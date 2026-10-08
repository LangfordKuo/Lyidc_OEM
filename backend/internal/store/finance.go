package store

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
)

// RechargeInput 是创建充值单的落库字段（金额已由 handler 校验）。
type RechargeInput struct {
	TradeNo   string
	MemberID  uint64
	Amount    string
	Channel   string
	ExpiresAt *time.Time
}

// RechargeFilter 是充值单分页查询条件（page 从 1 开始）。
type RechargeFilter struct {
	// MemberID 为 0 表示不按会员过滤（管理端传 0 时不加条件）。
	MemberID uint64
	Status   string
	Page     int
	PageSize int
}

// LedgerFilter 是余额流水分页查询条件（page 从 1 开始）。
type LedgerFilter struct {
	// MemberID 为 0 表示不按会员过滤（管理端传 0 时不加条件）。
	MemberID uint64
	Type     string
	Page     int
	PageSize int
}

// RechargePaymentInput 是渠道回调触发的充值入账参数。
type RechargePaymentInput struct {
	OutTradeNo     string
	ChannelTradeNo string
	PaidAt         time.Time
}

// RechargePaymentResult 是充值入账的结果。
type RechargePaymentResult struct {
	Recharge *model.Recharge
	Member   *model.Member
	Outcome  PaymentOutcome
}

// CreateRecharge 创建 pending 充值单；trade_no 冲突时返回 ErrTradeNoTaken（调用方换号重试）。
func (s *Store) CreateRecharge(ctx context.Context, in RechargeInput) (*model.Recharge, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	recharge := model.Recharge{
		TradeNo:   in.TradeNo,
		MemberID:  in.MemberID,
		Amount:    model.Money(in.Amount),
		Channel:   in.Channel,
		Status:    model.RechargeStatusPending,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: in.ExpiresAt,
	}
	if err := db.Create(&recharge).Error; err != nil {
		return nil, mapDuplicateError(err)
	}
	return &recharge, nil
}

// RechargeByID 按本地主键查询充值单。
func (s *Store) RechargeByID(ctx context.Context, id uint64) (*model.Recharge, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return rechargeBy(db, "id = ?", id)
}

// RechargeByTradeNo 按本地单号查询充值单（支付回调用）。
func (s *Store) RechargeByTradeNo(ctx context.Context, tradeNo string) (*model.Recharge, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return rechargeBy(db, "trade_no = ?", tradeNo)
}

// ListRecharges 按条件分页查询充值单（新建在前），返回当页数据与总数。
func (s *Store) ListRecharges(ctx context.Context, filter RechargeFilter) ([]model.Recharge, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.Recharge{})
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

	items := make([]model.Recharge, 0)
	if err := query.Session(&gorm.Session{}).
		Order("id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ListLedger 按条件分页查询余额流水（新建在前），返回当页数据与总数。
func (s *Store) ListLedger(ctx context.Context, filter LedgerFilter) ([]model.Ledger, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.Ledger{})
	if filter.MemberID != 0 {
		query = query.Where("member_id = ?", filter.MemberID)
	}
	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]model.Ledger, 0)
	if err := query.Session(&gorm.Session{}).
		Order("id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// CompleteRechargePayment 处理渠道回调的充值入账：单事务内锁充值单 → pending 转 paid →
// 锁会员行加款 → 写流水（type=recharge，amount 为正）。
//
// 幂等：已 paid 返回 OutcomeAlreadyPaid（不重复入账）；已 closed 返回 OutcomeSkipped
// （不处理，调用方记 WARN 并答复成功）。金额一致性由调用方在进入本方法前校验。
func (s *Store) CompleteRechargePayment(ctx context.Context, in RechargePaymentInput) (RechargePaymentResult, error) {
	db, err := s.session(ctx)
	if err != nil {
		return RechargePaymentResult{}, err
	}

	var result RechargePaymentResult
	err = db.Transaction(func(tx *gorm.DB) error {
		recharge, err := lockRecharge(tx, "trade_no = ?", in.OutTradeNo)
		if err != nil {
			return err
		}
		if recharge.Status == model.RechargeStatusPaid {
			result = RechargePaymentResult{Recharge: recharge, Outcome: OutcomeAlreadyPaid}
			return nil
		}
		if recharge.Status == model.RechargeStatusClosed {
			result = RechargePaymentResult{Recharge: recharge, Outcome: OutcomeSkipped}
			return nil
		}

		amountCents, err := pricing.ParseAmount(string(recharge.Amount))
		if err != nil {
			return err
		}
		paidAt := in.PaidAt
		if paidAt.IsZero() {
			paidAt = time.Now().UTC()
		}

		channelTradeNo := in.ChannelTradeNo
		if err := tx.Model(&model.Recharge{}).Where("id = ?", recharge.ID).Updates(map[string]any{
			"status":           model.RechargeStatusPaid,
			"channel_trade_no": channelTradeNo,
			"paid_at":          paidAt,
		}).Error; err != nil {
			return err
		}

		member, err := applyBalanceChange(tx, recharge.MemberID, amountCents,
			model.LedgerTypeRecharge, model.LedgerRefRecharge, recharge.ID, "充值 "+recharge.TradeNo, paidAt)
		if err != nil {
			return err
		}

		recharge.Status = model.RechargeStatusPaid
		recharge.ChannelTradeNo = &channelTradeNo
		recharge.PaidAt = &paidAt
		result = RechargePaymentResult{Recharge: recharge, Member: member, Outcome: OutcomeApplied}
		return nil
	})
	if err != nil {
		return RechargePaymentResult{}, err
	}
	return result, nil
}

// rechargeBy 是内部按条件查询单条充值单。
func rechargeBy(db *gorm.DB, query string, args ...any) (*model.Recharge, error) {
	var recharge model.Recharge
	if err := db.Where(query, args...).Take(&recharge).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &recharge, nil
}

// lockRecharge 在事务内以 SELECT ... FOR UPDATE 锁定充值单行（并发回调的幂等锚点）。
func lockRecharge(tx *gorm.DB, query string, args ...any) (*model.Recharge, error) {
	var recharge model.Recharge
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(query, args...).Take(&recharge).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &recharge, nil
}
