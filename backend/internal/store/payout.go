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

// ── 出账申请（提现 / 转余额，契约 18.1.1）────────────────────────────────
//
// 账务口径（两类不同）：
//
//	withdraw「提交即冻结」：提交时 rebate_balance 立即扣减 + 写返现侧 ledger（负），
//	  approve 只改状态，reject 退回冻结额（加回 + 正向 ledger），mark-paid 只记 paid_at；
//	convert「审核通过时才换算」：提交只登记不动钱，approve 同一事务里
//	  rebate_balance 减 + balance 加 + 两端各一条 ledger，reject 不动余额。
//
// 可用余额 = rebate_balance − Σ(kind=convert 且 status=pending 的 amount)；
// 在途 withdraw 不必再减（已冻结）。全部校验在**行锁**下进行。

// PayoutInput 是创建出账申请的落库字段（全部已由 handler 校验与归一化）。
type PayoutInput struct {
	Kind        string
	MemberID    uint64
	AmountCents int64
	// Channel / AccountName / AccountNo 仅 kind=withdraw 有值（convert 恒为空串）。
	// AccountName / AccountNo 是敏感字段：库内存原文，展示边界才脱敏。
	Channel     string
	AccountName string
	AccountNo   string
	Remark      string
}

// PayoutFilter 是出账申请分页查询条件（page 从 1 开始）。
type PayoutFilter struct {
	// Kind 为空表示不按类型过滤（管理端可传空查两类）。
	Kind string
	// MemberID 为 0 表示不按申请人过滤（会员端一律传本人 ID）。
	MemberID uint64
	Status   string
	Page     int
	PageSize int
}

// PayoutItem 是出账申请的对外行（含申请人概要）。
type PayoutItem struct {
	model.ReferralPayout
	// MemberUsername / MemberNickname 是申请人概要；会员行缺失时为空串。
	MemberUsername string `gorm:"column:member_username"`
	MemberNickname string `gorm:"column:member_nickname"`
}

// PayoutReviewResult 描述一次审核/打款的结果：Already=true 表示幂等命中
// （状态已是目标态，本次**不产生**任何资金动作，也不覆盖首次时间戳）。
type PayoutReviewResult struct {
	Payout  *model.ReferralPayout
	Already bool
}

// InsufficientRebateError 表示申请金额超过可用返现余额，并携带**可用余额**
// （整数分），供 handler 组装明确的拒绝 message（契约 18.5 要求含可用余额）。
type InsufficientRebateError struct {
	AvailableCents int64
}

// Error 实现 error；与 ErrInsufficientRebate 保持 errors.Is 可判定。
func (e *InsufficientRebateError) Error() string {
	return fmt.Sprintf("%s：可用余额 %s", ErrInsufficientRebate.Error(),
		pricing.FormatAmount(e.AvailableCents))
}

// Is 让 errors.Is(err, ErrInsufficientRebate) 命中本类型。
func (e *InsufficientRebateError) Is(target error) bool { return target == ErrInsufficientRebate }

// availableRebateCents 计算可用返现余额（整数分）：
//
//	可用 = rebate_balance − Σ(kind=convert 且 status=pending 的 amount)
//
// 调用方必须已经持有会员行锁；在途 withdraw 不必再减（提交时已冻结）。
func availableRebateCents(tx *gorm.DB, member *model.Member) (int64, error) {
	rebateCents, err := pricing.ParseSignedAmount(string(member.RebateBalance))
	if err != nil {
		return 0, fmt.Errorf("会员 %d 返现余额数据异常: %w", member.ID, err)
	}

	var row struct {
		Total model.Money `gorm:"column:total"`
	}
	if err := tx.Model(&model.ReferralPayout{}).
		Select("COALESCE(ROUND(SUM(amount), 2), 0.00) AS total").
		Where("member_id = ? AND kind = ? AND status = ?",
			member.ID, model.PayoutKindConvert, model.PayoutStatusPending).
		Scan(&row).Error; err != nil {
		return 0, err
	}
	pendingCents, err := pricing.ParseAmount(string(row.Total))
	if err != nil {
		return 0, fmt.Errorf("会员 %d 在途转换申请额数据异常: %w", member.ID, err)
	}
	return rebateCents - pendingCents, nil
}

// CreatePayout 创建一笔出账申请。
//
//   - kind=withdraw：**提交即冻结**——同一事务内扣减 rebate_balance 并写返现侧 ledger（负）；
//   - kind=convert：**只登记不动钱**——审核通过时才换算。
//
// 返回哨兵错误（handler 映射 40002）：
//   - ErrRebateNegative：rebate_balance < 0（负数口径，禁提现与转换）；
//   - ErrInsufficientRebate：amount 超过可用余额；
//   - ErrInvalidAmount：amount ≤ 0。
func (s *Store) CreatePayout(ctx context.Context, in PayoutInput) (*model.ReferralPayout, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var created *model.ReferralPayout
	err = db.Transaction(func(tx *gorm.DB) error {
		var member model.Member
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", in.MemberID).Take(&member).Error; err != nil {
			return notFoundIfNeeded(err)
		}

		rebateCents, err := pricing.ParseSignedAmount(string(member.RebateBalance))
		if err != nil {
			return fmt.Errorf("会员 %d 返现余额数据异常: %w", in.MemberID, err)
		}
		if rebateCents < 0 {
			return ErrRebateNegative
		}
		if in.AmountCents <= 0 {
			return ErrInvalidAmount
		}
		available, err := availableRebateCents(tx, &member)
		if err != nil {
			return err
		}
		if in.AmountCents > available {
			return &InsufficientRebateError{AvailableCents: available}
		}

		payout := model.ReferralPayout{
			Kind:        in.Kind,
			MemberID:    in.MemberID,
			Amount:      model.Money(pricing.FormatAmount(in.AmountCents)),
			Channel:     in.Channel,
			AccountName: in.AccountName,
			AccountNo:   in.AccountNo,
			Status:      model.PayoutStatusPending,
			Remark:      in.Remark,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := tx.Create(&payout).Error; err != nil {
			return err
		}
		created = &payout

		if in.Kind != model.PayoutKindWithdraw {
			return nil // convert：提交时不动钱
		}
		// withdraw：提交即冻结——扣减 rebate_balance 并写返现侧 ledger（负）。
		return applyRebateBalanceChange(tx, in.MemberID, -in.AmountCents,
			model.LedgerRefReferralWithdrawal, payout.ID,
			"提现申请 #"+fmt.Sprint(payout.ID)+" 冻结", now)
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// ListPayouts 分页查询出账申请（新建在前），附带申请人用户名与昵称。
func (s *Store) ListPayouts(ctx context.Context, filter PayoutFilter) ([]PayoutItem, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.ReferralPayout{})
	if filter.Kind != "" {
		query = query.Where("referral_payouts.kind = ?", filter.Kind)
	}
	if filter.MemberID != 0 {
		query = query.Where("referral_payouts.member_id = ?", filter.MemberID)
	}
	if filter.Status != "" {
		query = query.Where("referral_payouts.status = ?", filter.Status)
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]PayoutItem, 0)
	if err := query.Session(&gorm.Session{}).
		Select("referral_payouts.*, members.username AS member_username, members.nickname AS member_nickname").
		Joins("LEFT JOIN members ON members.id = referral_payouts.member_id").
		Order("referral_payouts.id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Scan(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// PayoutByID 按主键查询出账申请（管理端用）。
func (s *Store) PayoutByID(ctx context.Context, id uint64) (*model.ReferralPayout, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return payoutBy(db, "id = ?", id)
}

// PayoutByIDForMember 查询**本人**出账申请；他人申请与不存在的统一 ErrNotFound（对外 404）。
func (s *Store) PayoutByIDForMember(ctx context.Context, id, memberID uint64) (*model.ReferralPayout, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return payoutBy(db, "id = ? AND member_id = ?", id, memberID)
}

// ReviewPayout 审核出账申请（approve / reject）。
//
// 状态机（契约 18.1.1）：
//
//	pending + approve → approved（convert 在此执行换算）
//	pending + reject  → rejected（withdraw 在此退回冻结额）
//	对 approved 再 approve、对 rejected 再 reject → Already=true，零资金动作、不覆盖时间戳
//	其余组合（含 paid 的一切、approved→reject、rejected→approve）→ ErrStateConflict
//
// 审核幂等锚点是 `status: pending→<目标>` 的**条件更新**：影响行数为 0 即已被并发审核过，
// 直接按 Already 处理，绝不重复产生资金动作。
func (s *Store) ReviewPayout(ctx context.Context, payoutID, adminID uint64, approve bool, reviewNote string) (PayoutReviewResult, error) {
	db, err := s.session(ctx)
	if err != nil {
		return PayoutReviewResult{}, err
	}

	now := time.Now().UTC()
	var result PayoutReviewResult
	err = db.Transaction(func(tx *gorm.DB) error {
		var payout model.ReferralPayout
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", payoutID).Take(&payout).Error; err != nil {
			return notFoundIfNeeded(err)
		}

		target := model.PayoutStatusApproved
		sameState := payout.Status == model.PayoutStatusApproved
		if !approve {
			target = model.PayoutStatusRejected
			sameState = payout.Status == model.PayoutStatusRejected
		}

		// 终态或非法流转一律拒绝（paid 是终态，approved/rejected 之间不可互转）。
		if payout.Status == model.PayoutStatusPaid ||
			(payout.Status == model.PayoutStatusApproved && !approve) ||
			(payout.Status == model.PayoutStatusRejected && approve) {
			return ErrStateConflict
		}
		if sameState {
			result = PayoutReviewResult{Payout: &payout, Already: true}
			return nil
		}

		// pending → 目标态：条件更新作幂等锚点。
		update := map[string]any{
			"status":      target,
			"reviewed_by": adminID,
			"reviewed_at": now,
			"updated_at":  now,
		}
		if reviewNote != "" {
			update["review_note"] = reviewNote
		}
		claimed := tx.Model(&model.ReferralPayout{}).
			Where("id = ? AND status = ?", payoutID, model.PayoutStatusPending).
			Updates(update)
		if claimed.Error != nil {
			return claimed.Error
		}
		if claimed.RowsAffected == 0 {
			// 并发下已被别的管理员审掉：回读现状，按幂等处理，不重复产生资金动作。
			var latest model.ReferralPayout
			if err := tx.Where("id = ?", payoutID).Take(&latest).Error; err != nil {
				return notFoundIfNeeded(err)
			}
			result = PayoutReviewResult{Payout: &latest, Already: true}
			return nil
		}

		if err := applyPayoutReviewEffect(tx, &payout, approve, now); err != nil {
			return err
		}

		payout.Status = target
		payout.ReviewedBy = adminID
		payout.ReviewedAt = &now
		if reviewNote != "" {
			payout.ReviewNote = reviewNote
		}
		payout.UpdatedAt = now
		result = PayoutReviewResult{Payout: &payout}
		return nil
	})
	if err != nil {
		return PayoutReviewResult{}, err
	}
	return result, nil
}

// applyPayoutReviewEffect 执行审核带来的资金动作（仅在状态真的翻转时调用一次）。
//
//   - withdraw + approve：钱已在提交时冻结，这里只改状态，不动余额；
//   - withdraw + reject：退回冻结额（rebate_balance 加回 + 正向 ledger）；
//   - convert + approve：换算——rebate_balance 减、balance 加，两端各一条 ledger；
//   - convert + reject：提交时就没扣过钱，不动余额。
func applyPayoutReviewEffect(tx *gorm.DB, payout *model.ReferralPayout, approve bool, now time.Time) error {
	amountCents, err := pricing.ParseAmount(string(payout.Amount))
	if err != nil {
		return fmt.Errorf("出账申请 %d 金额数据异常: %w", payout.ID, err)
	}
	if amountCents <= 0 {
		return ErrInvalidAmount
	}

	note := fmt.Sprintf("出账申请 #%d", payout.ID)
	switch {
	case payout.Kind == model.PayoutKindWithdraw && !approve:
		// 驳回提现：退回冻结额。
		return applyRebateBalanceChange(tx, payout.MemberID, amountCents,
			model.LedgerRefReferralWithdrawal, payout.ID,
			note+" 驳回退回", now)

	case payout.Kind == model.PayoutKindConvert && approve:
		// 换算：返现侧减（type=referral_rebate 负向）+ 消费侧加（type=adjust 正向），同一事务。
		if err := applyRebateBalanceChange(tx, payout.MemberID, -amountCents,
			model.LedgerRefReferralConversion, payout.ID,
			note+" 转消费余额（返现侧）", now); err != nil {
			return err
		}
		_, err := applyBalanceChange(tx, payout.MemberID, amountCents,
			model.LedgerTypeAdjust, model.LedgerRefReferralConversion, payout.ID,
			note+" 转消费余额（消费侧）", now)
		return err

	default:
		// withdraw+approve（钱已冻结）与 convert+reject（从未扣过）都不动余额。
		return nil
	}
}

// MarkPayoutPaid 把已通过的提现申请标记为线下已打款（kind=convert 不适用）。
//
// 幂等：对已 paid 的申请重复调用返回 Already=true，**不覆盖**首次 paid_at。
// 非 approved（含 pending / rejected / convert 的 approved）一律 ErrStateConflict。
func (s *Store) MarkPayoutPaid(ctx context.Context, payoutID, adminID uint64, reviewNote string) (PayoutReviewResult, error) {
	db, err := s.session(ctx)
	if err != nil {
		return PayoutReviewResult{}, err
	}

	now := time.Now().UTC()
	var result PayoutReviewResult
	err = db.Transaction(func(tx *gorm.DB) error {
		var payout model.ReferralPayout
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", payoutID).Take(&payout).Error; err != nil {
			return notFoundIfNeeded(err)
		}
		if payout.Kind != model.PayoutKindWithdraw {
			return ErrStateConflict
		}
		if payout.Status == model.PayoutStatusPaid {
			result = PayoutReviewResult{Payout: &payout, Already: true}
			return nil
		}
		if payout.Status != model.PayoutStatusApproved {
			return ErrStateConflict
		}

		update := map[string]any{
			"status":     model.PayoutStatusPaid,
			"paid_at":    now,
			"updated_at": now,
		}
		if reviewNote != "" {
			update["review_note"] = reviewNote
		}
		claimed := tx.Model(&model.ReferralPayout{}).
			Where("id = ? AND status = ?", payoutID, model.PayoutStatusApproved).
			Updates(update)
		if claimed.Error != nil {
			return claimed.Error
		}
		if claimed.RowsAffected == 0 {
			var latest model.ReferralPayout
			if err := tx.Where("id = ?", payoutID).Take(&latest).Error; err != nil {
				return notFoundIfNeeded(err)
			}
			result = PayoutReviewResult{Payout: &latest, Already: true}
			return nil
		}

		payout.Status = model.PayoutStatusPaid
		payout.PaidAt = &now
		if reviewNote != "" {
			payout.ReviewNote = reviewNote
		}
		payout.UpdatedAt = now
		result = PayoutReviewResult{Payout: &payout}
		return nil
	})
	if err != nil {
		return PayoutReviewResult{}, err
	}
	return result, nil
}

// SetMemberRebateOverride 写会员级返佣特批比例（契约 6.3 三态）：
// value=nil 清空（落 NULL = 不覆盖）；value!=nil 设值（0.00-100.00，由调用方校验）。
func (s *Store) SetMemberRebateOverride(ctx context.Context, memberID uint64, value *string) (*model.Member, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := memberByID(db, memberID); err != nil {
		return nil, err
	}

	columns := map[string]any{"updated_at": time.Now().UTC()}
	if value == nil {
		columns["rebate_percent_override"] = nil
	} else {
		columns["rebate_percent_override"] = *value
	}
	if err := db.Model(&model.Member{}).Where("id = ?", memberID).
		Updates(columns).Error; err != nil {
		return nil, err
	}
	return memberByID(db, memberID)
}

// payoutBy 是内部按条件查询单条出账申请。
func payoutBy(db *gorm.DB, query string, args ...any) (*model.ReferralPayout, error) {
	var payout model.ReferralPayout
	if err := db.Where(query, args...).Take(&payout).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &payout, nil
}
