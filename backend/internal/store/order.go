package store

import (
	"context"
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
	CouponID       *uint64
	CouponCode     string
}

// OrderFilter 是订单分页查询条件（page 从 1 开始）。
type OrderFilter struct {
	// MemberID 为 0 表示不按会员过滤（管理端预留；本批会员端一律传本人 ID）。
	MemberID uint64
	Status   string
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
	// CouponNotCounted 为 true 表示优惠码条件自增影响行数为 0
	// （极端并发下已超用）——按契约只告警、不阻断入账。
	CouponNotCounted bool
}

// CreateOrder 创建 pending 订单；trade_no 冲突时返回 ErrTradeNoTaken（调用方换号重试）。
func (s *Store) CreateOrder(ctx context.Context, in OrderInput) (*model.Order, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	order := model.Order{
		TradeNo:        in.TradeNo,
		MemberID:       in.MemberID,
		ProductID:      in.ProductID,
		ProductName:    in.ProductName,
		Cycle:          in.Cycle,
		Qty:            in.Qty,
		ConfigJSON:     in.ConfigJSON,
		Amount:         model.Money(in.Amount),
		DiscountAmount: model.Money(in.DiscountAmount),
		FinalAmount:    model.Money(in.FinalAmount),
		CouponID:       in.CouponID,
		CouponCode:     in.CouponCode,
		Status:         model.OrderStatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := db.Create(&order).Error; err != nil {
		return nil, mapDuplicateError(err)
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
func (s *Store) CancelOrder(ctx context.Context, id, memberID uint64) (*model.Order, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	result := db.Model(&model.Order{}).
		Where("id = ? AND member_id = ? AND status = ?", id, memberID, model.OrderStatusPending).
		Updates(map[string]any{"status": model.OrderStatusCancelled, "updated_at": now})
	if result.Error != nil {
		return nil, result.Error
	}

	order, err := orderBy(db, "id = ? AND member_id = ?", id, memberID)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected == 0 {
		return order, ErrStateConflict
	}
	return order, nil
}

// PayOrderWithBalance 用余额支付订单：单事务内锁订单与会员行 → 扣款 → 订单转 paid →
// 写流水（type=order_pay，amount 为负）→ 优惠码条件自增。
//
// 余额不足返回 ErrInsufficientBalance；订单已支付/已取消分别返回
// OutcomeAlreadyPaid / OutcomeSkipped（不视为错误，调用方按状态给提示）。
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
		if order.Status == model.OrderStatusPaid {
			result = OrderPaymentResult{Order: order, Outcome: OutcomeAlreadyPaid}
			return nil
		}
		if order.Status == model.OrderStatusCancelled {
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
		if err := tx.Model(&model.Order{}).Where("id = ?", order.ID).Updates(map[string]any{
			"status":      model.OrderStatusPaid,
			"pay_channel": model.PayChannelBalance,
			"pay_time":    now,
			"updated_at":  now,
		}).Error; err != nil {
			return err
		}

		notCounted, err := incrementCouponUsage(tx, order.CouponID)
		if err != nil {
			return err
		}
		// 阶段 5 交付触发点：订单转 paid 后在此发起上游开通/交付；本批不做。

		order.Status = model.OrderStatusPaid
		order.PayChannel = model.PayChannelBalance
		order.PayTime = &now
		order.UpdatedAt = now
		result = OrderPaymentResult{Order: order, Outcome: OutcomeApplied, CouponNotCounted: notCounted}
		return nil
	})
	if err != nil {
		return OrderPaymentResult{}, err
	}
	return result, nil
}

// CompleteOrderPayment 处理渠道回调的订单入账：单事务内锁订单 → pending 转 paid →
// 记录 pay_channel / channel_trade_no / pay_time → 优惠码条件自增。
//
// 幂等：已 paid 返回 OutcomeAlreadyPaid（不再处理）；已 cancelled 返回 OutcomeSkipped
// （不处理，调用方按契约记 WARN 并答复成功）。金额一致性由调用方在进入本方法前校验。
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
		if order.Status == model.OrderStatusPaid {
			result = OrderPaymentResult{Order: order, Outcome: OutcomeAlreadyPaid}
			return nil
		}
		if order.Status == model.OrderStatusCancelled {
			result = OrderPaymentResult{Order: order, Outcome: OutcomeSkipped}
			return nil
		}

		paidAt := in.PaidAt
		if paidAt.IsZero() {
			paidAt = time.Now().UTC()
		}
		if err := tx.Model(&model.Order{}).Where("id = ?", order.ID).Updates(map[string]any{
			"status":           model.OrderStatusPaid,
			"pay_channel":      in.Channel,
			"channel_trade_no": in.ChannelTradeNo,
			"pay_time":         paidAt,
			"updated_at":       paidAt,
		}).Error; err != nil {
			return err
		}

		notCounted, err := incrementCouponUsage(tx, order.CouponID)
		if err != nil {
			return err
		}
		// 阶段 5 交付触发点：订单转 paid 后在此发起上游开通/交付；本批不做。

		order.Status = model.OrderStatusPaid
		order.PayChannel = in.Channel
		order.ChannelTradeNo = in.ChannelTradeNo
		order.PayTime = &paidAt
		order.UpdatedAt = paidAt
		result = OrderPaymentResult{Order: order, Outcome: OutcomeApplied, CouponNotCounted: notCounted}
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

// incrementCouponUsage 条件自增优惠码使用次数（原子防超用）：
// 仅在 max_uses=0（不限）或 used_count<max_uses 时 +1。
// 影响行数为 0 表示极端并发下已超用，返回 notCounted=true（调用方记 WARN，不阻断入账）。
func incrementCouponUsage(tx *gorm.DB, couponID *uint64) (notCounted bool, err error) {
	if couponID == nil {
		return false, nil
	}
	result := tx.Model(&model.Coupon{}).
		Where("id = ? AND (max_uses = 0 OR used_count < max_uses)", *couponID).
		UpdateColumn("used_count", gorm.Expr("used_count + 1"))
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 0, nil
}
