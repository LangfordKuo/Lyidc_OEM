package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
)

// PaymentOutcome 是支付入账的结果分类（幂等与状态冲突的统一判定）。
type PaymentOutcome int

const (
	// OutcomeApplied 本次调用完成了入账（订单转 paid / 充值单转 paid + 余额到账）。
	OutcomeApplied PaymentOutcome = iota
	// OutcomeAlreadyPaid 记录已是已支付（重复回调或重复发起支付），幂等：直接按成功答复。
	OutcomeAlreadyPaid
	// OutcomeSkipped 记录处于不可入账的终态（订单已取消 / 充值单已关闭）：
	// 调用方记 WARN 并按成功答复（避免渠道重试），但不做任何入账。
	OutcomeSkipped
)

// OrderInput 是创建订单的落库字段（金额与配置已由 handler 校验、计算）。
type OrderInput struct {
	TradeNo        string
	MemberID       uint64
	ProductID      uint64
	ProductName    string
	Cycle          string
	Qty            int
	ConfigJSON     string
	Amount         string
	DiscountAmount string
	FinalAmount    string
	// PriceBreakdownJSON 是成交价差价明细快照（R7，契约 10.2.1）；空串落 NULL。
	PriceBreakdownJSON string
	CouponID           *uint64
	CouponCode         string
	// Type 是订单类型（阶段 5b）：空串按 new 处理；renew 单必须带 InstanceID。
	Type string
	// InstanceID 是续费单对应的实例（renew 单必填；new 单为 nil）。
	InstanceID *uint64
	// BindInviterID 非 0 时，创建订单的同事务内把买家首绑到该邀请人
	// （用于邀请人自建券，契约 18.5）。买家已绑定邀请人时返回 ErrInviterAlreadyBound、
	// 订单不落库——自建券只允许未绑定邀请关系的客户使用。
	BindInviterID uint64
}

// OrderFilter 是订单分页查询条件（page 从 1 开始）。
type OrderFilter struct {
	// MemberID 为 0 表示不按会员过滤（会员端一律传本人 ID；管理端传 0 表示全站）。
	MemberID uint64
	Status   string
	// TradeNo 为**模糊**匹配（不区分大小写的 LIKE 包含匹配，与会员/工单搜索口径一致）；
	// 空串表示不过滤。单号前缀 `O` 由调用方保证不参与匹配口径。
	TradeNo string
	// Type 为订单类型精确匹配（new / renew，阶段 8b）；空串表示不过滤。
	Type     string
	Page     int
	PageSize int
}

// OrderPaymentInput 是渠道回调触发的订单入账参数。
type OrderPaymentInput struct {
	OutTradeNo     string
	Channel        string
	ChannelTradeNo string
	PaidAt         time.Time
}

// OrderPaymentResult 是订单支付入账的结果。
type OrderPaymentResult struct {
	Order   *model.Order
	Outcome PaymentOutcome
}

// CreateOrder 创建 pending 订单；trade_no 冲突时返回 ErrTradeNoTaken（调用方换号重试）。
//
// in.BindInviterID 非 0 时（邀请人自建券，契约 18.5）在同一事务内完成邀请人首绑；
// 买家已绑定邀请人时返回 ErrInviterAlreadyBound，订单不落库。
//
// 优惠码占用（P4 安全整改 finding#09，契约 12.5）：in.CouponID 非 nil 时，在同一事务内先做
// **条件自增**占用（`max_uses = 0 OR used_count < max_uses`）——影响行数为 0（券已用尽，含并发
// 抢占）直接返回 ErrCouponExhausted，**订单不落库**（不是「先落单再回滚」）；影响行数为 1 才写
// 订单行并把 coupon_counted 置 1。占用与订单行同事务，任一步失败整体回滚（含单号冲突重试：
// 重试是一次新事务，上一次的占用已随回滚释放）。
func (s *Store) CreateOrder(ctx context.Context, in OrderInput) (*model.Order, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	orderType := in.Type
	if orderType == "" {
		orderType = model.OrderTypeNew
	}

	now := time.Now().UTC()
	order := model.Order{
		TradeNo:            in.TradeNo,
		MemberID:           in.MemberID,
		ProductID:          in.ProductID,
		ProductName:        in.ProductName,
		Cycle:              in.Cycle,
		Qty:                in.Qty,
		ConfigJSON:         in.ConfigJSON,
		Amount:             model.Money(in.Amount),
		DiscountAmount:     model.Money(in.DiscountAmount),
		FinalAmount:        model.Money(in.FinalAmount),
		PriceBreakdownJSON: in.PriceBreakdownJSON,
		CouponID:           in.CouponID,
		CouponCode:         in.CouponCode,
		Status:             model.OrderStatusPending,
		Type:               orderType,
		InstanceID:         in.InstanceID,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		if in.BindInviterID != 0 {
			bound, err := BindInviterTx(tx, in.MemberID, in.BindInviterID)
			if err != nil {
				return err
			}
			if !bound {
				return ErrInviterAlreadyBound
			}
		}
		// P4：下单即原子占用（同事务、条件自增）。占用失败 → 拒绝下单，订单行不写入。
		occupied, err := occupyCouponUsage(tx, in.CouponID)
		if err != nil {
			return err
		}
		if !occupied {
			return ErrCouponExhausted
		}
		order.CouponCounted = in.CouponID != nil
		if err := tx.Create(&order).Error; err != nil {
			return mapDuplicateError(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// OrderByID 按本地主键查询订单。
func (s *Store) OrderByID(ctx context.Context, id uint64) (*model.Order, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return orderBy(db, "id = ?", id)
}

// OrderByTradeNo 按本地单号查询订单（支付回调用）。
func (s *Store) OrderByTradeNo(ctx context.Context, tradeNo string) (*model.Order, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return orderBy(db, "trade_no = ?", tradeNo)
}

// OrderByIDForMember 查询**本人**订单；他人订单与不存在的订单统一返回 ErrNotFound（对外 404）。
func (s *Store) OrderByIDForMember(ctx context.Context, id, memberID uint64) (*model.Order, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return orderBy(db, "id = ? AND member_id = ?", id, memberID)
}

// ListOrders 按条件分页查询订单（新建在前），返回当页数据与总数。
func (s *Store) ListOrders(ctx context.Context, filter OrderFilter) ([]model.Order, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.Order{})
	if filter.MemberID != 0 {
		query = query.Where("member_id = ?", filter.MemberID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.TradeNo != "" {
		query = query.Where("trade_no LIKE ?", containsKeyword(filter.TradeNo))
	}
	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]model.Order, 0)
	if err := query.Session(&gorm.Session{}).
		Order("id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// CancelOrder 取消本人 pending 订单（条件 UPDATE 保证并发下只有一个请求生效）。
// 状态不允许取消时返回最新记录 + ErrStateConflict；订单不存在或非本人返回 ErrNotFound。
//
// 取消成功后在同一事务内做两件释放（任一失败整体回滚）：
//   - 优惠码占用（P4 安全整改 finding#09，契约 12.5）：本单 coupon_counted=1 时把券 used_count 减 1
//     （`WHERE used_count > 0`，不低于 0）并把 coupon_counted 置 0；paid 及之后的订单不可取消，
//     因此「成交即消耗、取消才释放」由状态机本身保证；
//   - 该笔返佣回滚（契约 18.4）：当前只有 pending 可取消、pending 从未发放返佣，因此这条钩子在
//     现有取消路径上是防御性的——一旦未来出现「已支付订单取消/退款」路径，复用同一钩子即可保证
//     返佣被冲回；回滚本身幂等（再次回滚不重复）。
func (s *Store) CancelOrder(ctx context.Context, id, memberID uint64) (*model.Order, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var order *model.Order
	err = db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Order{}).
			Where("id = ? AND member_id = ? AND status = ?", id, memberID, model.OrderStatusPending).
			Updates(map[string]any{"status": model.OrderStatusCancelled, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}

		latest, err := orderBy(tx, "id = ? AND member_id = ?", id, memberID)
		if err != nil {
			return err
		}
		order = latest
		if result.RowsAffected == 0 {
			return ErrStateConflict
		}
		if err := releaseCouponUsage(tx, latest); err != nil {
			return err
		}
		return rollbackReferralRebateInTx(tx, id, now)
	})
	if err != nil {
		if errors.Is(err, ErrStateConflict) && order != nil {
			return order, ErrStateConflict
		}
		return nil, err
	}
	return order, nil
}

// PayOrderWithBalance 用余额支付订单：单事务内锁订单与会员行 → 扣款 → 订单转 paid →
// 写流水（type=order_pay，amount 为负）→ 历史遗留单补记优惠码占用（见下）。
//
// 优惠码（P4 安全整改 finding#09，契约 12.5）：占用在**下单**时已完成，本方法**不再自增**；
// 只有迁移 0020 之前下单、coupon_counted 仍为 0 的历史单才在此按老口径补记一次，
// 券已用尽则返回 ErrCouponExhausted（整个事务回滚：不扣余额、订单保持 pending）。
//
// 余额不足返回 ErrInsufficientBalance；订单已入账（paid 及其后的交付态）返回
// OutcomeAlreadyPaid、其余非 pending 状态返回 OutcomeSkipped（都不视为错误，调用方按状态给提示）。
func (s *Store) PayOrderWithBalance(ctx context.Context, orderID, memberID uint64) (OrderPaymentResult, error) {
	db, err := s.session(ctx)
	if err != nil {
		return OrderPaymentResult{}, err
	}

	var result OrderPaymentResult
	err = db.Transaction(func(tx *gorm.DB) error {
		order, err := lockOrder(tx, "id = ? AND member_id = ?", orderID, memberID)
		if err != nil {
			return err
		}
		// 已入账（paid 及其后的交付态）→ 幂等；非 pending 的其余状态（如已取消）→ 不处理。
		if model.IsOrderSettled(order.Status) {
			result = OrderPaymentResult{Order: order, Outcome: OutcomeAlreadyPaid}
			return nil
		}
		if order.Status != model.OrderStatusPending {
			result = OrderPaymentResult{Order: order, Outcome: OutcomeSkipped}
			return nil
		}

		amountCents, err := pricing.ParseAmount(string(order.FinalAmount))
		if err != nil {
			return fmt.Errorf("订单 %s 金额数据异常: %w", order.TradeNo, err)
		}

		now := time.Now().UTC()
		if _, err := applyBalanceChange(tx, memberID, -amountCents,
			model.LedgerTypeOrderPay, model.LedgerRefOrder, order.ID, "订单支付 "+order.TradeNo, now); err != nil {
			return err
		}

		// P4：历史遗留单（迁移 0020 之前下单，coupon_counted 仍为 0）按老口径补记一次占用；
		// 券已用尽 → 拒绝入账（返回 ErrCouponExhausted，事务整体回滚）。
		if err := countLegacyCouponUsage(tx, order); err != nil {
			return err
		}

		columns := map[string]any{
			"status":      model.OrderStatusPaid,
			"pay_channel": model.PayChannelBalance,
			"pay_time":    now,
			"updated_at":  now,
		}
		if order.CouponCounted {
			columns["coupon_counted"] = true
		}
		if err := tx.Model(&model.Order{}).Where("id = ?", order.ID).Updates(columns).Error; err != nil {
			return err
		}

		// 邀请返佣：与入账同事务发放（契约 18.4）。发放条件与幂等见 grantReferralRebate；
		// 未绑定/禁用/关闸/为 0 都不发放，也绝不阻断入账。
		if _, err := grantReferralRebate(tx, order, now); err != nil {
			return err
		}
		// 阶段 5 交付触发点：订单转 paid 后在此发起上游开通/交付；本批不做。

		order.Status = model.OrderStatusPaid
		order.PayChannel = model.PayChannelBalance
		order.PayTime = &now
		order.UpdatedAt = now
		result = OrderPaymentResult{Order: order, Outcome: OutcomeApplied}
		return nil
	})
	if err != nil {
		return OrderPaymentResult{}, err
	}
	return result, nil
}

// CompleteOrderPayment 处理渠道回调的订单入账：单事务内锁订单 → pending 转 paid →
// 记录 pay_channel / channel_trade_no / pay_time → 历史遗留单补记优惠码占用（见下）。
//
// 优惠码（P4 安全整改 finding#09，契约 12.5）：占用在**下单**时已完成，本方法**不再自增**；
// 只有迁移 0020 之前下单、coupon_counted 仍为 0 的历史单才在此按老口径补记一次，
// 券已用尽则返回 ErrCouponExhausted（事务回滚、订单保持 pending；调用方应答渠道 fail）。
//
// 幂等：已入账（paid 及其后的交付态，model.IsOrderSettled）返回 OutcomeAlreadyPaid——
// 重复回调不重复入账、不重复计数、不重复触发交付（契约 14.1）；
// 其余非 pending 状态（如已取消）返回 OutcomeSkipped（不处理，调用方按契约记 WARN 并答复成功）。
// 金额一致性由调用方在进入本方法前校验。
func (s *Store) CompleteOrderPayment(ctx context.Context, in OrderPaymentInput) (OrderPaymentResult, error) {
	db, err := s.session(ctx)
	if err != nil {
		return OrderPaymentResult{}, err
	}

	var result OrderPaymentResult
	err = db.Transaction(func(tx *gorm.DB) error {
		order, err := lockOrder(tx, "trade_no = ?", in.OutTradeNo)
		if err != nil {
			return err
		}
		// 已入账（paid 及其后的交付态）→ 幂等；非 pending 的其余状态（如已取消）→ 不处理。
		if model.IsOrderSettled(order.Status) {
			result = OrderPaymentResult{Order: order, Outcome: OutcomeAlreadyPaid}
			return nil
		}
		if order.Status != model.OrderStatusPending {
			result = OrderPaymentResult{Order: order, Outcome: OutcomeSkipped}
			return nil
		}

		paidAt := in.PaidAt
		if paidAt.IsZero() {
			paidAt = time.Now().UTC()
		}

		// P4：历史遗留单（迁移 0020 之前下单，coupon_counted 仍为 0）按老口径补记一次占用；
		// 券已用尽 → 拒绝入账（返回 ErrCouponExhausted，事务整体回滚）。
		if err := countLegacyCouponUsage(tx, order); err != nil {
			return err
		}

		columns := map[string]any{
			"status":           model.OrderStatusPaid,
			"pay_channel":      in.Channel,
			"channel_trade_no": in.ChannelTradeNo,
			"pay_time":         paidAt,
			"updated_at":       paidAt,
		}
		if order.CouponCounted {
			columns["coupon_counted"] = true
		}
		if err := tx.Model(&model.Order{}).Where("id = ?", order.ID).Updates(columns).Error; err != nil {
			return err
		}

		// 邀请返佣：与入账同事务发放（契约 18.4）。发放条件与幂等见 grantReferralRebate。
		if _, err := grantReferralRebate(tx, order, paidAt); err != nil {
			return err
		}
		// 阶段 5 交付触发点：订单转 paid 后在此发起上游开通/交付；本批不做。

		order.Status = model.OrderStatusPaid
		order.PayChannel = in.Channel
		order.ChannelTradeNo = in.ChannelTradeNo
		order.PayTime = &paidAt
		order.UpdatedAt = paidAt
		result = OrderPaymentResult{Order: order, Outcome: OutcomeApplied}
		return nil
	})
	if err != nil {
		return OrderPaymentResult{}, err
	}
	return result, nil
}

// orderBy 是内部按条件查询单条订单。
func orderBy(db *gorm.DB, query string, args ...any) (*model.Order, error) {
	var order model.Order
	if err := db.Where(query, args...).Take(&order).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &order, nil
}

// lockOrder 在事务内以 SELECT ... FOR UPDATE 锁定订单行（并发回调的幂等锚点）。
func lockOrder(tx *gorm.DB, query string, args ...any) (*model.Order, error) {
	var order model.Order
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(query, args...).Take(&order).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &order, nil
}

// applyBalanceChange 在事务内锁定会员行并按 deltaCents（有符号整数分）调整余额，
// 同时写一条流水（BalanceAfter = BalanceBefore + amount）。
// 出账（delta < 0）时余额不足返回 ErrInsufficientBalance。
func applyBalanceChange(tx *gorm.DB, memberID uint64, deltaCents int64, kind, refType string, refID uint64, note string, now time.Time) (*model.Member, error) {
	var member model.Member
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", memberID).Take(&member).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}

	balanceCents, err := pricing.ParseAmount(string(member.Balance))
	if err != nil {
		return nil, fmt.Errorf("会员 %d 余额数据异常: %w", memberID, err)
	}
	newBalanceCents := balanceCents + deltaCents
	if newBalanceCents < 0 {
		return nil, ErrInsufficientBalance
	}
	newBalance := pricing.FormatAmount(newBalanceCents)

	if err := tx.Model(&model.Member{}).Where("id = ?", memberID).Updates(map[string]any{
		"balance":    newBalance,
		"updated_at": now,
	}).Error; err != nil {
		return nil, err
	}

	ledger := model.Ledger{
		MemberID:      memberID,
		Type:          kind,
		Amount:        model.Money(pricing.FormatAmount(deltaCents)),
		BalanceBefore: member.Balance,
		BalanceAfter:  model.Money(newBalance),
		RefType:       refType,
		RefID:         refID,
		Note:          note,
		CreatedAt:     now,
	}
	if err := tx.Create(&ledger).Error; err != nil {
		return nil, err
	}

	member.Balance = model.Money(newBalance)
	member.UpdatedAt = now
	return &member, nil
}

// occupyCouponUsage 条件自增优惠码使用次数（原子占用，P4 安全整改 finding#09，契约 12.5）：
// 仅在 max_uses=0（不限）或 used_count<max_uses 时 +1，由 InnoDB 行锁 + 当前读保证互斥——
// 因此两个并发请求抢同一张 max_uses=1 的券时，只有一个的影响行数为 1。
//
// 返回 occupied=false 表示券已用尽（含并发抢占），调用方必须拒绝下单（ErrCouponExhausted）
// 或拒绝入账；couponID 为 nil（未用码）时恒返回 true，不做任何写入。
//
// 刻意不做「先 SELECT 再 UPDATE」：那样两个请求都读到 used_count=0 时会双双放行。
func occupyCouponUsage(tx *gorm.DB, couponID *uint64) (occupied bool, err error) {
	if couponID == nil {
		return true, nil
	}
	result := tx.Model(&model.Coupon{}).
		Where("id = ? AND (max_uses = 0 OR used_count < max_uses)", *couponID).
		UpdateColumn("used_count", gorm.Expr("used_count + 1"))
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// countLegacyCouponUsage 为**迁移 0020 之前下单的历史单**按老口径补记一次优惠码占用。
//
// 迁移刻意不回填 coupon_counted，因此「未用码」与「历史单」都表现为 0；两者的区分依据是
// CouponID 是否为 nil——未用码的订单 CouponID 恒为 nil，历史单则有值。
// 补记成功时把 order.CouponCounted 置 true（调用方据此把该列一并写回，幂等锚点同时成立）。
// 券已用尽返回 ErrCouponExhausted：调用方必须拒绝入账，订单保持 pending（契约 12.5 第 5 行）。
func countLegacyCouponUsage(tx *gorm.DB, order *model.Order) error {
	if order.CouponID == nil || order.CouponCounted {
		return nil
	}
	occupied, err := occupyCouponUsage(tx, order.CouponID)
	if err != nil {
		return err
	}
	if !occupied {
		return ErrCouponExhausted
	}
	order.CouponCounted = true
	return nil
}

// releaseCouponUsage 在取消订单的同一事务内释放本单占用的优惠码次数（P4，契约 12.5）：
// 券 used_count 减 1 并把订单 coupon_counted 置 0。
//
// `WHERE used_count > 0` 是「不低于 0」的兜底：INT UNSIGNED 列在 0 上再减会报越界错误，
// 而且历史脏数据（占用标记与计数不一致）也不该把取消流程卡死。
// 未占用（coupon_counted=0 或未用码）时不做任何写入，因此重复取消不会重复释放
// （重复取消本就被状态条件挡住）。
func releaseCouponUsage(tx *gorm.DB, order *model.Order) error {
	if !order.CouponCounted || order.CouponID == nil {
		return nil
	}
	if err := tx.Model(&model.Coupon{}).
		Where("id = ? AND used_count > 0", *order.CouponID).
		UpdateColumn("used_count", gorm.Expr("used_count - 1")).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.Order{}).Where("id = ?", order.ID).
		UpdateColumn("coupon_counted", false).Error; err != nil {
		return err
	}
	order.CouponCounted = false
	return nil
}
