package store

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
)

// ── 邀请码 ──────────────────────────────────────────────────────────────

// inviteCodeAlphabet 是邀请码字符表：32 字符 base32 口径，去掉易混淆的 0/O/1/I。
const inviteCodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// inviteCodeLength 是邀请码长度（8 位 × 5 bit = 40 bit 熵，约 1.1e12 种组合）。
//
// 取舍：会员只有「首次开通返现」才生成邀请码（惰性），总量远小于组合数；
// 8 位比 6 位更抗碰撞但仍短到可以直接念/贴链接，冲突由唯一键 + 换码重试兜底。
const inviteCodeLength = 8

// inviteCodeMaxRetries 是邀请码唯一键冲突的换码重试上限（与本地单号口径一致）。
const inviteCodeMaxRetries = 5

// generateInviteCode 生成一个 8 位邀请码（crypto/rand 均匀取自 32 字符表）。
func generateInviteCode() (string, error) {
	buf := make([]byte, inviteCodeLength)
	limit := big.NewInt(int64(len(inviteCodeAlphabet)))
	for i := range buf {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("生成邀请码失败: %w", err)
		}
		buf[i] = inviteCodeAlphabet[n.Int64()]
	}
	return string(buf), nil
}

// EnsureInviteCode 惰性生成邀请码：已有邀请码则原样返回，从未生成过才生成（契约 18.5）。
// 唯一键冲突换码重试，最多 inviteCodeMaxRetries 次。
func (s *Store) EnsureInviteCode(ctx context.Context, memberID uint64) (string, error) {
	db, err := s.session(ctx)
	if err != nil {
		return "", err
	}

	member, err := memberByID(db, memberID)
	if err != nil {
		return "", err
	}
	if member.InviteCode != nil && *member.InviteCode != "" {
		return *member.InviteCode, nil
	}

	now := time.Now().UTC()
	for attempt := 0; attempt < inviteCodeMaxRetries; attempt++ {
		code, err := generateInviteCode()
		if err != nil {
			return "", err
		}
		result := db.Model(&model.Member{}).
			Where("id = ? AND (invite_code IS NULL OR invite_code = '')", memberID).
			Updates(map[string]any{"invite_code": code, "updated_at": now})
		if result.Error != nil {
			if errors.Is(mapDuplicateError(result.Error), ErrDuplicate) {
				continue
			}
			return "", result.Error
		}
		if result.RowsAffected > 0 {
			return code, nil
		}
		// 并发下别人已生成：回读已有邀请码。
		member, err := memberByID(db, memberID)
		if err != nil {
			return "", err
		}
		if member.InviteCode != nil && *member.InviteCode != "" {
			return *member.InviteCode, nil
		}
	}
	return "", fmt.Errorf("%w: 邀请码生成冲突重试超限", ErrDuplicate)
}

// MemberByInviteCode 按邀请码精确匹配查询邀请人（列 utf8mb4_general_ci，大小写不敏感）。
func (s *Store) MemberByInviteCode(ctx context.Context, code string) (*model.Member, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return memberBy(db, "invite_code = ?", code)
}

// SetMemberReferralEnabled 开通/关闭邀请返现（契约 18.5）。
func (s *Store) SetMemberReferralEnabled(ctx context.Context, memberID uint64, enabled bool) (*model.Member, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := memberByID(db, memberID); err != nil {
		return nil, err
	}
	if err := db.Model(&model.Member{}).Where("id = ?", memberID).
		Updates(map[string]any{"referral_enabled": enabled, "updated_at": time.Now().UTC()}).Error; err != nil {
		return nil, err
	}
	return memberByID(db, memberID)
}

// BindInviter 首绑邀请人（仅当当前 inviter_id=0 时生效）。
// 返回本次是否真的完成绑定；已绑定过一律 false（首绑不改，契约 18.4）。
func (s *Store) BindInviter(ctx context.Context, memberID, inviterID uint64) (bool, error) {
	db, err := s.session(ctx)
	if err != nil {
		return false, err
	}
	result := db.Model(&model.Member{}).
		Where("id = ? AND inviter_id = 0", memberID).
		Updates(map[string]any{"inviter_id": inviterID, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// BindInviterTx 与 BindInviter 同语义，但走调用方给定的事务句柄。
func BindInviterTx(tx *gorm.DB, memberID, inviterID uint64) (bool, error) {
	result := tx.Model(&model.Member{}).
		Where("id = ? AND inviter_id = 0", memberID).
		Updates(map[string]any{"inviter_id": inviterID, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// ── 返佣结算 ────────────────────────────────────────────────────────────

// referralSettingsJSON 是 settings.referral 值结构的**发放所需**镜像。
//
// settings 包依赖 store（Source 接口），store 不能反向依赖 settings（会成环），
// 因此这里只镜像发放所需的两个字段；完整结构（含 max_coupon_percent）见 settings.Referral。
type referralSettingsJSON struct {
	Enabled        bool   `json:"enabled"`
	DefaultPercent string `json:"default_percent"`
}

// loadReferralPolicy 读取发放所需的返佣策略（总开关 + 全局默认比例）。
// 未配置或取值非法时按「总开关关闭」处理（宁可不发，也不乱发钱）。
func loadReferralPolicy(tx *gorm.DB) (enabled bool, defaultHundredths int64) {
	var setting model.Setting
	if err := tx.Where("`key` = ?", "referral").Take(&setting).Error; err != nil {
		return false, 0
	}
	var raw referralSettingsJSON
	if err := json.Unmarshal([]byte(setting.Value), &raw); err != nil {
		return false, 0
	}
	hundredths, err := pricing.ParsePercent(raw.DefaultPercent)
	if err != nil {
		// 比例非法按 0 处理：总开关开着也只会按 0% 计，不会乱发。
		hundredths = 0
	}
	return raw.Enabled, hundredths
}

// resolveRebateRatio 按契约 18.2 三级优先级取**第一个非空命中**：
// 商品级 products.rebate_percent → 会员级 邀请人 members.rebate_percent_override →
// 全局默认 settings.referral.default_percent。返回比例（百分之 ×100 的整数）与来源。
func resolveRebateRatio(product *model.Product, inviter *model.Member, globalHundredths int64) (int64, string) {
	if product != nil && product.RebatePercent != nil {
		if hundredths, err := pricing.ParsePercent(*product.RebatePercent); err == nil {
			return hundredths, model.RebateSourceProduct
		}
	}
	if inviter != nil && inviter.RebatePercentOverride != nil {
		if hundredths, err := pricing.ParsePercent(*inviter.RebatePercentOverride); err == nil {
			return hundredths, model.RebateSourceMember
		}
	}
	return globalHundredths, model.RebateSourceGlobal
}

// grantReferralRebate 在**支付入账事务内**发放返佣（契约 18.4）：
// 写 referral_rebates + ledger(referral_rebate) + 邀请人 rebate_balance 自增。
//
// 不发放的情形（一律返回 nil，不阻断入账）：
//   - 总开关关闭（settings.referral.enabled=false）；
//   - 买家未绑定邀请人（inviter_id=0）；
//   - 商品禁用返佣（products.rebate_disabled=1，压过一切比例）；
//   - 商品不存在或邀请人不存在；
//   - 计算结果为 0（公式自带地板，如券减免吃满比例）。
//
// 幂等：referral_rebates.order_id 唯一；本函数先查已有发放，存在即跳过
// （正常路径下重复入账已被 model.IsOrderSettled 闸门挡住，这里是第二道防线）。
func grantReferralRebate(tx *gorm.DB, order *model.Order, now time.Time) (*model.ReferralRebate, error) {
	var existing model.ReferralRebate
	err := tx.Where("order_id = ?", order.ID).Take(&existing).Error
	switch {
	case err == nil:
		return &existing, nil // 已发放过，幂等跳过
	case errors.Is(err, gorm.ErrRecordNotFound):
		// 继续发放
	default:
		return nil, err
	}

	buyer, err := memberByID(tx, order.MemberID)
	if err != nil {
		return nil, err
	}
	if buyer.InviterID == 0 {
		return nil, nil // 未绑定邀请人 → 不返
	}

	var product model.Product
	productErr := tx.Where("id = ?", order.ProductID).Take(&product).Error
	switch {
	case productErr == nil && product.RebateDisabled:
		return nil, nil // 商品禁用返佣 → 完全不返
	case errors.Is(productErr, gorm.ErrRecordNotFound):
		productErr = nil
	case productErr != nil:
		return nil, productErr
	}

	inviter, err := memberByID(tx, buyer.InviterID)
	if err != nil {
		return nil, err // 邀请人不存在（脏数据）→ 不返
	}

	enabled, defaultHundredths := loadReferralPolicy(tx)
	if !enabled {
		return nil, nil
	}

	var productPtr *model.Product
	if productErr == nil && product.ID != 0 {
		productPtr = &product
	}
	ratioHundredths, source := resolveRebateRatio(productPtr, inviter, defaultHundredths)

	baseCents, err := pricing.ParseAmount(string(order.Amount))
	if err != nil {
		return nil, fmt.Errorf("订单 %s 原价数据异常: %w", order.TradeNo, err)
	}
	discountCents, err := pricing.ParseAmount(string(order.DiscountAmount))
	if err != nil {
		return nil, fmt.Errorf("订单 %s 优惠减免数据异常: %w", order.TradeNo, err)
	}

	rebateCents := pricing.ComputeRebateCents(baseCents, discountCents, ratioHundredths)
	if rebateCents <= 0 {
		return nil, nil // 返佣为 0：不发放、不写流水
	}

	rebate := model.ReferralRebate{
		OrderID:        order.ID,
		InviterID:      buyer.InviterID,
		BuyerID:        buyer.ID,
		OrderType:      order.Type,
		BaseAmount:     order.Amount,
		DiscountAmount: order.DiscountAmount,
		RatioPercent:   model.Money(pricing.FormatPercent(ratioHundredths)),
		RatioSource:    source,
		RebateAmount:   model.Money(pricing.FormatAmount(rebateCents)),
		Status:         model.RebateStatusGranted,
		Note:           "订单 " + order.TradeNo + " 返佣",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := tx.Create(&rebate).Error; err != nil {
		// 唯一键冲突 = 并发下已被别的入账路径发放，按幂等跳过。
		if errors.Is(mapDuplicateError(err), ErrDuplicate) {
			return nil, nil
		}
		return nil, err
	}

	if err := applyRebateBalanceChange(tx, buyer.InviterID, rebateCents, rebate.ID, rebate.Note, now); err != nil {
		return nil, err
	}
	return &rebate, nil
}

// applyRebateBalanceChange 在事务内锁定邀请人行并按 deltaCents（有符号整数分）调整
// **邀请返现余额** rebate_balance，同时写一条 ledger(type=referral_rebate)。
//
// 与 applyBalanceChange 的差别：rebate_balance **可为负**（已提现后客户退款 → 扣成负），
// 不做余额不足校验；流水的 balance_before / balance_after 记的是 rebate_balance 的前后值。
func applyRebateBalanceChange(tx *gorm.DB, memberID uint64, deltaCents int64, refID uint64, note string, now time.Time) error {
	var member model.Member
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", memberID).Take(&member).Error; err != nil {
		return notFoundIfNeeded(err)
	}

	beforeCents, err := pricing.ParseSignedAmount(string(member.RebateBalance))
	if err != nil {
		return fmt.Errorf("会员 %d 返现余额数据异常: %w", memberID, err)
	}
	afterCents := beforeCents + deltaCents
	after := pricing.FormatAmount(afterCents)

	if err := tx.Model(&model.Member{}).Where("id = ?", memberID).Updates(map[string]any{
		"rebate_balance": after,
		"updated_at":     now,
	}).Error; err != nil {
		return err
	}

	ledger := model.Ledger{
		MemberID:      memberID,
		Type:          model.LedgerTypeReferralRebate,
		Amount:        model.Money(pricing.FormatAmount(deltaCents)),
		BalanceBefore: member.RebateBalance,
		BalanceAfter:  model.Money(after),
		RefType:       model.LedgerRefReferralRebate,
		RefID:         refID,
		Note:          note,
		CreatedAt:     now,
	}
	return tx.Create(&ledger).Error
}

// RollbackReferralRebate 回滚某订单的返佣（订单转 cancelled 时调用，契约 18.4）：
// 写负向 ledger、在返现余额上冲回，referral_rebates.status 置 rolled_back。
//
// 幂等：本就没有返佣、或已回滚过，一律直接返回 nil，不重复冲回。
func (s *Store) RollbackReferralRebate(ctx context.Context, orderID uint64) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		return rollbackReferralRebateInTx(tx, orderID, time.Now().UTC())
	})
}

// rollbackReferralRebateInTx 是 RollbackReferralRebate 的事务内实现（供订单取消同事务调用）。
func rollbackReferralRebateInTx(tx *gorm.DB, orderID uint64, now time.Time) error {
	var rebate model.ReferralRebate
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("order_id = ?", orderID).Take(&rebate).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil // 本就没有返佣
	case err != nil:
		return err
	}
	if rebate.Status == model.RebateStatusRolledBack {
		return nil // 已回滚，幂等
	}

	rebateCents, err := pricing.ParseAmount(string(rebate.RebateAmount))
	if err != nil {
		return fmt.Errorf("返佣流水 %d 金额数据异常: %w", rebate.ID, err)
	}
	if rebateCents > 0 {
		if err := applyRebateBalanceChange(tx, rebate.InviterID, -rebateCents, rebate.ID,
			"订单 "+rebate.Note+" 冲回", now); err != nil {
			return err
		}
	}

	if err := tx.Model(&model.ReferralRebate{}).Where("id = ?", rebate.ID).Updates(map[string]any{
		"status":     model.RebateStatusRolledBack,
		"note":       "订单 " + rebate.Note + " 已取消，返佣冲回",
		"updated_at": now,
	}).Error; err != nil {
		return err
	}
	return nil
}

// ── 查询 ────────────────────────────────────────────────────────────────

// ReferralRebateFilter 是返佣流水分页查询条件（page 从 1 开始）。
type ReferralRebateFilter struct {
	// InviterID 为 0 表示不按邀请人过滤（会员端一律传本人 ID；管理端可传 0 查全站）。
	InviterID uint64
	Status    string
	Page      int
	PageSize  int
}

// ReferralRebateItem 是返佣流水的对外行（含订单号与买家用户名）。
type ReferralRebateItem struct {
	model.ReferralRebate
	// OrderTradeNo 是订单号（orders.trade_no）；订单行缺失时为空串。
	OrderTradeNo string `gorm:"column:order_trade_no"`
	// BuyerUsername 是买家用户名；买家行缺失时为空串。
	BuyerUsername string `gorm:"column:buyer_username"`
}

// ListReferralRebates 分页查询返佣流水（新建在前），附带订单号与买家用户名。
func (s *Store) ListReferralRebates(ctx context.Context, filter ReferralRebateFilter) ([]ReferralRebateItem, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.ReferralRebate{})
	if filter.InviterID != 0 {
		query = query.Where("referral_rebates.inviter_id = ?", filter.InviterID)
	}
	if filter.Status != "" {
		query = query.Where("referral_rebates.status = ?", filter.Status)
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]ReferralRebateItem, 0)
	if err := query.Session(&gorm.Session{}).
		Select("referral_rebates.*, orders.trade_no AS order_trade_no, members.username AS buyer_username").
		Joins("LEFT JOIN orders ON orders.id = referral_rebates.order_id").
		Joins("LEFT JOIN members ON members.id = referral_rebates.buyer_id").
		Order("referral_rebates.id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Scan(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// InviteeFilter 是「我邀请的会员」分页查询条件（page 从 1 开始）。
type InviteeFilter struct {
	InviterID uint64
	Page      int
	PageSize  int
}

// InviteeStat 是单个被邀请人的消费/贡献返佣统计。
//
// 消费口径：该会员**已入账**订单（paid/provisioning/active/failed）的 final_amount 之和（实付）。
// 贡献返佣口径：给邀请人带来且仍处 granted 的返佣之和（rolled_back 不计）。
type InviteeStat struct {
	MemberID    uint64    `gorm:"column:id"`
	Username    string    `gorm:"column:username"`
	JoinedAt    time.Time `gorm:"column:created_at"`
	OrderCount  int64     `gorm:"column:order_count"`
	TotalSpent  int64     `gorm:"column:total_spent"`
	TotalRebate int64     `gorm:"column:total_rebate"`
}

// ListInvitees 分页查询我邀请的会员（新加入在前）及各自消费/贡献返佣统计。
func (s *Store) ListInvitees(ctx context.Context, filter InviteeFilter) ([]InviteeStat, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.Member{}).Where("inviter_id = ?", filter.InviterID)
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	bases := make([]model.Member, 0)
	if err := query.Session(&gorm.Session{}).
		Order("id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&bases).Error; err != nil {
		return nil, 0, err
	}
	if len(bases) == 0 {
		return []InviteeStat{}, total, nil
	}

	ids := make([]uint64, 0, len(bases))
	for i := range bases {
		ids = append(ids, bases[i].ID)
	}

	// 订单聚合：只统计已入账订单（与 IsOrderSettled 一致）。
	// 金额走 SUM(decimal) + ROUND(…, 2) 后用 model.Money 接住，再在 Go 侧换算成整数分——
	// 直接在 SQL 里乘 100 会让 MySQL 返回 DECIMAL，驱动扫进 int64 会报错。
	type spendRow struct {
		MemberID   uint64      `gorm:"column:member_id"`
		OrderCount int64       `gorm:"column:order_count"`
		TotalSpent model.Money `gorm:"column:total_spent"`
	}
	spends := make([]spendRow, 0)
	if err := db.Model(&model.Order{}).
		Select("member_id, COUNT(*) AS order_count, COALESCE(ROUND(SUM(final_amount), 2), 0.00) AS total_spent").
		Where("member_id IN ? AND status IN ?", ids,
			[]string{model.OrderStatusPaid, model.OrderStatusProvisioning,
				model.OrderStatusActive, model.OrderStatusFailed}).
		Group("member_id").
		Scan(&spends).Error; err != nil {
		return nil, 0, err
	}

	// 返佣聚合：只计已发放（rolled_back 不算贡献）。
	type rebateRow struct {
		BuyerID     uint64      `gorm:"column:buyer_id"`
		TotalRebate model.Money `gorm:"column:total_rebate"`
	}
	rebates := make([]rebateRow, 0)
	if err := db.Model(&model.ReferralRebate{}).
		Select("buyer_id, COALESCE(ROUND(SUM(rebate_amount), 2), 0.00) AS total_rebate").
		Where("inviter_id = ? AND buyer_id IN ? AND status = ?", filter.InviterID, ids, model.RebateStatusGranted).
		Group("buyer_id").
		Scan(&rebates).Error; err != nil {
		return nil, 0, err
	}

	spendByMember := make(map[uint64]spendRow, len(spends))
	for _, row := range spends {
		spendByMember[row.MemberID] = row
	}
	rebateByMember := make(map[uint64]int64, len(rebates))
	for _, row := range rebates {
		cents, err := pricing.ParseAmount(string(row.TotalRebate))
		if err != nil {
			return nil, 0, fmt.Errorf("会员 %d 返佣合计数据异常: %w", row.BuyerID, err)
		}
		rebateByMember[row.BuyerID] = cents
	}

	items := make([]InviteeStat, 0, len(bases))
	for _, base := range bases {
		stat := InviteeStat{
			MemberID:    base.ID,
			Username:    base.Username,
			JoinedAt:    base.CreatedAt,
			TotalRebate: rebateByMember[base.ID],
		}
		if spend, ok := spendByMember[base.ID]; ok {
			spentCents, err := pricing.ParseAmount(string(spend.TotalSpent))
			if err != nil {
				return nil, 0, fmt.Errorf("会员 %d 消费合计数据异常: %w", spend.MemberID, err)
			}
			stat.OrderCount = spend.OrderCount
			stat.TotalSpent = spentCents
		}
		items = append(items, stat)
	}
	return items, total, nil
}

// ReferralSummary 是我的返佣概览聚合（GET /api/v1/referral 的数据部分）。
type ReferralSummary struct {
	// InviteeCount 已邀请人数（members.inviter_id = 本人 的会员数）。
	InviteeCount int64
	// TotalRebateCents 累计返佣（已发放且未回滚之和）。
	TotalRebateCents int64
}

// ReferralSummary 查询我的返佣概览聚合。
func (s *Store) ReferralSummary(ctx context.Context, memberID uint64) (ReferralSummary, error) {
	db, err := s.session(ctx)
	if err != nil {
		return ReferralSummary{}, err
	}

	var summary ReferralSummary
	if err := db.Model(&model.Member{}).Where("inviter_id = ?", memberID).
		Count(&summary.InviteeCount).Error; err != nil {
		return ReferralSummary{}, err
	}

	// 累计返佣：SUM 出 DECIMAL，用 model.Money 接住再换算整数分（SQL 里乘 100 扫 int64 会失败）。
	var row struct {
		Total model.Money `gorm:"column:total"`
	}
	if err := db.Model(&model.ReferralRebate{}).
		Select("COALESCE(ROUND(SUM(rebate_amount), 2), 0.00) AS total").
		Where("inviter_id = ? AND status = ?", memberID, model.RebateStatusGranted).
		Scan(&row).Error; err != nil {
		return ReferralSummary{}, err
	}
	totalCents, err := pricing.ParseAmount(string(row.Total))
	if err != nil {
		return ReferralSummary{}, fmt.Errorf("会员 %d 累计返佣数据异常: %w", memberID, err)
	}
	summary.TotalRebateCents = totalCents
	return summary, nil
}
