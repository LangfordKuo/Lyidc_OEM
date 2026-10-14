package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 邀请人自建券提示文案（契约 18.5，R10-B）。
const (
	msgReferralNotEnabled = "请先开通邀请返现"
	msgSelfCouponOnly     = "优惠码不存在" // 他人券 / 平台券统一 404，防越权探测
)

// referralCouponHandler 处理邀请人自建优惠码接口（契约 18.5，R10-B）。
//
// 口径：
//   - owner_member_id 强制 = 调用者（请求体不接受该字段）；
//   - type 本批只允许 percent（fixed 是绝对金额减免，无法用比例卡倒挂）；
//   - value 必须 ≤ 折扣上限 = min(settings.referral.max_coupon_percent, 邀请人的生效返佣比例)；
//   - 仅本人可读可改，且只能改 status 与 expires_at（value / code / owner 不可改）。
type referralCouponHandler struct {
	store  *store.Store
	reader *settings.Reader
	logger *slog.Logger
}

// createReferralCouponRequest 是 POST /api/v1/referral/coupons 请求体。
// 沿用 11.1 的字段与校验口径；type 本批只接受 percent。
type createReferralCouponRequest struct {
	Code      string    `json:"code"`
	Type      string    `json:"type"`
	Value     string    `json:"value"`
	Cycles    *[]string `json:"cycles"`
	StartsAt  *string   `json:"starts_at"`
	ExpiresAt *string   `json:"expires_at"`
	MaxUses   *int      `json:"max_uses"`
	Comment   *string   `json:"comment"`
}

// updateReferralCouponRequest 是 PUT /api/v1/referral/coupons/:id 请求体。
// **只允许 status 与 expires_at**；出现其它字段一律 40001 明确拒绝（不静默忽略）。
type updateReferralCouponRequest struct {
	Status    *string         `json:"status"`
	ExpiresAt json.RawMessage `json:"expires_at"`
	// 以下字段仅用于「出现即拒绝」的探测，不参与更新。
	Code      *string         `json:"code"`
	Type      *string         `json:"type"`
	Value     *string         `json:"value"`
	Cycles    json.RawMessage `json:"cycles"`
	StartsAt  json.RawMessage `json:"starts_at"`
	MaxUses   *int            `json:"max_uses"`
	Comment   *string         `json:"comment"`
	OwnerHint json.RawMessage `json:"owner_member_id"`
}

// createReferralCoupon 处理 POST /api/v1/referral/coupons：邀请人创建属于自己的券。
func (h *referralCouponHandler) createReferralCoupon(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	if !member.ReferralEnabled {
		response.Fail(c, response.CodeValidationFailed, msgReferralNotEnabled)
		return
	}

	var req createReferralCouponRequest
	if !bindJSON(c, &req) {
		return
	}

	code := strings.TrimSpace(req.Code)
	if err := validateCouponCode(code); err != nil {
		failRuleError(c, err)
		return
	}

	// 本批只允许 percent：fixed 无法用一个比例卡住倒挂（契约 18.5 取舍）。
	couponType := strings.ToLower(strings.TrimSpace(req.Type))
	if couponType != pricing.CouponTypePercent {
		response.Fail(c, response.CodeValidationFailed,
			"邀请人自建券只支持 percent（按比例折扣）；固定额减免无法用比例约束折扣上限")
		return
	}

	value := strings.TrimSpace(req.Value)
	valueHundredths, err := validateCouponValue(couponType, value)
	if err != nil {
		failRuleError(c, err)
		return
	}

	// 折扣上限：min(设置 max_coupon_percent, 邀请人的生效返佣比例)。
	ctx := c.Request.Context()
	referralState, ok := h.referralSettings(c)
	if !ok {
		return
	}
	cap := resolveCouponCap(referralState, member)
	if valueHundredths > cap.Cap {
		response.Fail(c, response.CodeValidationFailed, cap.describe())
		return
	}

	var cycles []string
	if req.Cycles != nil {
		cycles = *req.Cycles
	}
	cyclesJSON, err := normalizeCouponCycles(cycles)
	if err != nil {
		failRuleError(c, err)
		return
	}
	startsAt, err := parseCouponTime(derefString(req.StartsAt))
	if err != nil {
		failRuleError(c, err)
		return
	}
	expiresAt, err := parseCouponTime(derefString(req.ExpiresAt))
	if err != nil {
		failRuleError(c, err)
		return
	}
	if err := validateCouponWindow(startsAt, expiresAt); err != nil {
		failRuleError(c, err)
		return
	}
	maxUses := 0
	if req.MaxUses != nil {
		maxUses = *req.MaxUses
	}
	if err := validateCouponMaxUses(maxUses); err != nil {
		failRuleError(c, err)
		return
	}
	comment := ""
	if req.Comment != nil {
		comment = strings.TrimSpace(*req.Comment)
	}
	if err := validateCouponComment(comment); err != nil {
		failRuleError(c, err)
		return
	}

	// owner_member_id 强制 = 调用者；状态恒为 on（建一张停用的自建券没有意义）。
	coupon, err := h.store.CreateCoupon(ctx, store.CouponInput{
		Code:          code,
		Type:          couponType,
		Value:         value,
		CyclesJSON:    cyclesJSON,
		StartsAt:      startsAt,
		ExpiresAt:     expiresAt,
		MaxUses:       maxUses,
		Status:        model.CouponStatusOn,
		Comment:       comment,
		OwnerMemberID: member.ID,
	})
	switch {
	case errors.Is(err, store.ErrCouponCodeTaken):
		response.Fail(c, response.CodeConflict, "优惠码已存在（code 比较不区分大小写）")
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	h.logger.Info("邀请人自建券已创建",
		"member_id", member.ID, "coupon_id", coupon.ID, "value", value,
		"cap", pricing.FormatPercent(cap.Cap))
	setOplogSummary(c, "已创建自建券 "+coupon.Code+"（折扣 "+string(coupon.Value)+"%）")
	response.Success(c, newCouponView(coupon))
}

// listReferralCoupons 处理 GET /api/v1/referral/coupons：我创建的券（分页，仅本人）。
func (h *referralCouponHandler) listReferralCoupons(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	page, pageSize, ok := pagingParams(c)
	if !ok {
		return
	}

	items, total, err := h.store.ListCoupons(c.Request.Context(), store.CouponFilter{
		Page:          page,
		PageSize:      pageSize,
		OwnerMemberID: member.ID,
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]couponView, 0, len(items))
	for i := range items {
		views = append(views, newCouponView(&items[i]))
	}
	response.Success(c, couponListView{
		Items: views, Page: page, PageSize: pageSize, Total: total,
	})
}

// updateReferralCoupon 处理 PUT /api/v1/referral/coupons/:id：只能改自己的券，
// 且只允许改 status 与 expires_at。
func (h *referralCouponHandler) updateReferralCoupon(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	id, ok := couponIDParam(c)
	if !ok {
		return
	}

	var req updateReferralCouponRequest
	if !bindJSON(c, &req) {
		return
	}

	// 不可改字段：出现即拒绝（不静默忽略），杜绝绕过上限与挖客（契约 18.5）。
	if req.Value != nil || req.Code != nil || req.Type != nil || req.OwnerHint != nil {
		response.Fail(c, response.CodeInvalidParam,
			"自建券只允许修改 status 与 expires_at；value / code / type / owner_member_id 不可修改")
		return
	}
	if len(req.Cycles) > 0 || len(req.StartsAt) > 0 || req.MaxUses != nil || req.Comment != nil {
		response.Fail(c, response.CodeInvalidParam,
			"自建券只允许修改 status 与 expires_at")
		return
	}
	if req.Status == nil && len(req.ExpiresAt) == 0 {
		response.Fail(c, response.CodeInvalidParam,
			"至少提供一个字段：status / expires_at")
		return
	}

	ctx := c.Request.Context()
	// 只能改自己的券：他人券与平台券统一 404（防越权探测）。
	current, err := h.store.CouponByIDForMember(ctx, id, member.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgSelfCouponOnly)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	update := store.CouponUpdate{}
	if req.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*req.Status))
		if !model.IsValidCouponStatus(status) {
			response.Fail(c, response.CodeInvalidParam, msgCouponStatusInvalid)
			return
		}
		update.Status = &status
	}
	expiresAt := current.ExpiresAt
	if len(req.ExpiresAt) > 0 {
		parsed, err := parseCouponTimeRaw(req.ExpiresAt)
		if err != nil {
			failRuleError(c, err)
			return
		}
		update.ExpiresAt = store.CouponTimeUpdate{Set: true, Value: parsed}
		expiresAt = parsed
	}
	if err := validateCouponWindow(current.StartsAt, expiresAt); err != nil {
		failRuleError(c, err)
		return
	}

	updated, err := h.store.UpdateCoupon(ctx, id, update)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgSelfCouponOnly)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	setOplogSummary(c, fmt.Sprintf("已更新自建券 %s（当前状态 %s）", updated.Code, updated.Status))
	response.Success(c, newCouponView(updated))
}

// referralSettings 读取 referral 设置（计算折扣上限用）。
// 读取失败时写出错误响应并返回 ok=false。
func (h *referralCouponHandler) referralSettings(c *gin.Context) (settings.Referral, bool) {
	if h.reader == nil {
		failInternal(c, h.logger, fmt.Errorf("referral 设置读取器未装配"))
		return settings.Referral{}, false
	}
	state, err := h.reader.Referral(c.Request.Context())
	if err != nil {
		failDB(c, h.logger, err)
		return settings.Referral{}, false
	}
	return state.Value, true
}
