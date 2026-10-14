package router

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 出账申请提示文案。
// msgPayoutMissing 同时用于「他人申请」与「不存在」：统一 404，防越权探测。
const (
	msgPayoutMissing       = "出账申请不存在"
	msgPayoutActionInvalid = "action 只能是 approve 或 reject"
)

// referralPayoutHandler 处理提现 / 转余额申请接口（契约 18.1.1 / 18.5，R10-B）。
type referralPayoutHandler struct {
	store  *store.Store
	logger *slog.Logger
}

// payoutView 是出账申请对外视图。
//
// **敏感字段**（account_name / account_no）一律脱敏回显（契约 18.5）；
// 会员端与管理端共用本视图，管理端额外带 member 概要。
type payoutView struct {
	ID          uint64  `json:"id"`
	Kind        string  `json:"kind"`
	MemberID    uint64  `json:"member_id"`
	Amount      string  `json:"amount"`
	Channel     string  `json:"channel"`
	AccountName string  `json:"account_name"`
	AccountNo   string  `json:"account_no"`
	Status      string  `json:"status"`
	Remark      string  `json:"remark"`
	ReviewNote  string  `json:"review_note"`
	ReviewedBy  uint64  `json:"reviewed_by"`
	ReviewedAt  *string `json:"reviewed_at"`
	PaidAt      *string `json:"paid_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	// Member 是申请人概要（仅管理端列表/详情下发；会员端恒为 nil）。
	Member *payoutMemberView `json:"member,omitempty"`
}

// payoutMemberView 是出账申请关联的会员概要（管理端对账用）。
type payoutMemberView struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
}

// payoutListView 是出账申请分页列表。
type payoutListView struct {
	Items    []payoutView `json:"items"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
	Total    int64        `json:"total"`
}

// createWithdrawalRequest 是 POST /api/v1/referral/withdrawals 请求体。
type createWithdrawalRequest struct {
	Amount      string  `json:"amount"`
	Channel     string  `json:"channel"`
	AccountName string  `json:"account_name"`
	AccountNo   string  `json:"account_no"`
	Remark      *string `json:"remark"`
}

// createConversionRequest 是 POST /api/v1/referral/conversions 请求体。
type createConversionRequest struct {
	Amount string  `json:"amount"`
	Remark *string `json:"remark"`
}

// reviewPayoutRequest 是 POST /api/v1/admin/referral/{withdrawals,conversions}/:id/review 请求体。
type reviewPayoutRequest struct {
	Action     string  `json:"action"`
	ReviewNote *string `json:"review_note"`
}

// markPaidRequest 是 POST /api/v1/admin/referral/withdrawals/:id/mark-paid 请求体
// （可为空体；review_note 可选，仅首次打款时写入）。
type markPaidRequest struct {
	ReviewNote *string `json:"review_note"`
}

// ── 会员端 ────────────────────────────────────────────────────────────────

// createWithdrawal 处理 POST /api/v1/referral/withdrawals：提交提现申请（提交即冻结）。
func (h *referralPayoutHandler) createWithdrawal(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req createWithdrawalRequest
	if !bindJSON(c, &req) {
		return
	}
	amountCents, err := validatePayoutAmount(strings.TrimSpace(req.Amount))
	if err != nil {
		failRuleError(c, err)
		return
	}
	channel, err := validatePayoutChannel(req.Channel)
	if err != nil {
		failRuleError(c, err)
		return
	}
	accountName, err := validatePayoutAccount("account_name", req.AccountName)
	if err != nil {
		failRuleError(c, err)
		return
	}
	accountNo, err := validatePayoutAccount("account_no", req.AccountNo)
	if err != nil {
		failRuleError(c, err)
		return
	}
	remark, err := validatePayoutRemark(derefString(req.Remark))
	if err != nil {
		failRuleError(c, err)
		return
	}

	payout, err := h.store.CreatePayout(c.Request.Context(), store.PayoutInput{
		Kind:        model.PayoutKindWithdraw,
		MemberID:    member.ID,
		AmountCents: amountCents,
		Channel:     channel,
		AccountName: accountName,
		AccountNo:   accountNo,
		Remark:      remark,
	})
	if err := h.mapCreateError(c, err); err != nil {
		return
	}

	// 日志只记脱敏信息，绝不含账号原文（契约 18.5）。
	h.logger.Info("提现申请已提交（已冻结返现余额）",
		"member_id", member.ID, "payout_id", payout.ID, "amount", string(payout.Amount),
		"detail", maskedAccountNote(channel, accountName, accountNo))
	// R18：操作日志同样只记金额与申请号，收款账号原文绝不落库（日志只记脱敏结果也不可能带出原文）。
	setOplogSummary(c, fmt.Sprintf("提交提现申请 #%d（金额 %s，余额已冻结）", payout.ID, string(payout.Amount)))
	response.Success(c, h.view(payout, nil))
}

// createConversion 处理 POST /api/v1/referral/conversions：申请把返现余额转为消费余额。
// 提交时**不动钱**；审核通过时才换算（契约 18.1.1）。
func (h *referralPayoutHandler) createConversion(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req createConversionRequest
	if !bindJSON(c, &req) {
		return
	}
	amountCents, err := validatePayoutAmount(strings.TrimSpace(req.Amount))
	if err != nil {
		failRuleError(c, err)
		return
	}
	remark, err := validatePayoutRemark(derefString(req.Remark))
	if err != nil {
		failRuleError(c, err)
		return
	}

	payout, err := h.store.CreatePayout(c.Request.Context(), store.PayoutInput{
		Kind:        model.PayoutKindConvert,
		MemberID:    member.ID,
		AmountCents: amountCents,
		Remark:      remark,
	})
	if err := h.mapCreateError(c, err); err != nil {
		return
	}

	h.logger.Info("转余额申请已提交", "member_id", member.ID, "payout_id", payout.ID, "amount", string(payout.Amount))
	setOplogSummary(c, fmt.Sprintf("提交转余额申请 #%d（金额 %s，审核通过才换算）", payout.ID, string(payout.Amount)))
	response.Success(c, h.view(payout, nil))
}

// mapCreateError 把 store 的创建错误映射为响应；成功时返回 nil。
// 负数余额与余额不足给出**明确 message**（契约 18.1.1 / 18.5：余额不足须含可用余额）。
func (h *referralPayoutHandler) mapCreateError(c *gin.Context, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrRebateNegative):
		response.Fail(c, response.CodeValidationFailed,
			"邀请返现余额为负，暂不能提现或转余额")
	case errors.Is(err, store.ErrInsufficientRebate):
		available := int64(0)
		var insufficient *store.InsufficientRebateError
		if errors.As(err, &insufficient) {
			available = insufficient.AvailableCents
		}
		response.Fail(c, response.CodeValidationFailed,
			"邀请返现余额不足：可用余额 "+pricing.FormatAmount(available)+
				"（在途转余额申请已占用额度）")
	case errors.Is(err, store.ErrInvalidAmount):
		response.Fail(c, response.CodeValidationFailed, "申请金额必须大于 0")
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgMemberMissing)
	default:
		failDB(c, h.logger, err)
	}
	return err
}

// listMyWithdrawals 处理 GET /api/v1/referral/withdrawals：我的提现申请（分页）。
func (h *referralPayoutHandler) listMyWithdrawals(c *gin.Context) {
	h.listMine(c, model.PayoutKindWithdraw)
}

// listMyConversions 处理 GET /api/v1/referral/conversions：我的转余额申请（分页）。
func (h *referralPayoutHandler) listMyConversions(c *gin.Context) {
	h.listMine(c, model.PayoutKindConvert)
}

// listMine 列出本人某类申请（仅本人，他人一律查不到）。
func (h *referralPayoutHandler) listMine(c *gin.Context, kind string) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	page, pageSize, ok := pagingParams(c)
	if !ok {
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !model.IsValidPayoutStatus(status) {
		response.Fail(c, response.CodeInvalidParam, "status 取值非法（pending / approved / rejected / paid）")
		return
	}

	items, total, err := h.store.ListPayouts(c.Request.Context(), store.PayoutFilter{
		Kind:     kind,
		MemberID: member.ID,
		Status:   status,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]payoutView, 0, len(items))
	for i := range items {
		views = append(views, h.view(&items[i].ReferralPayout, nil))
	}
	response.Success(c, payoutListView{Items: views, Page: page, PageSize: pageSize, Total: total})
}

// ── 管理端 ────────────────────────────────────────────────────────────────

// listAdminWithdrawals 处理 GET /api/v1/admin/referral/withdrawals。
// 默认只看 kind=withdraw；`kind=all` 两类都查（运营对账用）。
func (h *referralPayoutHandler) listAdminWithdrawals(c *gin.Context) {
	kind := strings.TrimSpace(c.Query("kind"))
	switch kind {
	case "":
		kind = model.PayoutKindWithdraw
	case "all":
		kind = ""
	case model.PayoutKindWithdraw, model.PayoutKindConvert:
	default:
		response.Fail(c, response.CodeInvalidParam, "kind 只能是 withdraw / convert / all")
		return
	}
	h.listAdmin(c, kind)
}

// listAdminConversions 处理 GET /api/v1/admin/referral/conversions（恒 kind=convert）。
func (h *referralPayoutHandler) listAdminConversions(c *gin.Context) {
	h.listAdmin(c, model.PayoutKindConvert)
}

// listAdmin 是管理端出账申请列表（admin / finance 可读，财务域口径）。
func (h *referralPayoutHandler) listAdmin(c *gin.Context, kind string) {
	page, pageSize, ok := pagingParams(c)
	if !ok {
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !model.IsValidPayoutStatus(status) {
		response.Fail(c, response.CodeInvalidParam, "status 取值非法（pending / approved / rejected / paid）")
		return
	}
	var memberID uint64
	if raw := strings.TrimSpace(c.Query("member_id")); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || parsed == 0 {
			response.Fail(c, response.CodeInvalidParam, "member_id 必须为正整数")
			return
		}
		memberID = parsed
	}

	items, total, err := h.store.ListPayouts(c.Request.Context(), store.PayoutFilter{
		Kind:     kind,
		MemberID: memberID,
		Status:   status,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]payoutView, 0, len(items))
	for i := range items {
		summary := &payoutMemberView{
			ID:       items[i].MemberID,
			Username: items[i].MemberUsername,
			Nickname: items[i].MemberNickname,
		}
		views = append(views, h.view(&items[i].ReferralPayout, summary))
	}
	response.Success(c, payoutListView{Items: views, Page: page, PageSize: pageSize, Total: total})
}

// reviewWithdrawal 处理 POST /api/v1/admin/referral/withdrawals/:id/review（仅 admin）。
func (h *referralPayoutHandler) reviewWithdrawal(c *gin.Context) {
	h.review(c, model.PayoutKindWithdraw, "审核")
}

// reviewConversion 处理 POST /api/v1/admin/referral/conversions/:id/review（仅 admin）。
func (h *referralPayoutHandler) reviewConversion(c *gin.Context) {
	h.review(c, model.PayoutKindConvert, "审核")
}

// review 是审核动作的共用实现（approve / reject + 可选 review_note）。
func (h *referralPayoutHandler) review(c *gin.Context, expectKind, attempted string) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	id, ok := payoutIDParam(c)
	if !ok {
		return
	}
	var req reviewPayoutRequest
	if !bindJSON(c, &req) {
		return
	}
	action, err := parsePayoutAction(req.Action)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, msgPayoutActionInvalid)
		return
	}
	note, err := validatePayoutRemark(derefString(req.ReviewNote))
	if err != nil {
		failRuleError(c, err)
		return
	}

	ctx := c.Request.Context()
	existing, err := h.store.PayoutByID(ctx, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgPayoutMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	if existing.Kind != expectKind {
		// 类型对不上（把转换单当提现单审）一律 404，不暴露跨类型单据的存在性。
		response.Fail(c, response.CodeNotFound, msgPayoutMissing)
		return
	}

	result, err := h.store.ReviewPayout(ctx, id, admin.ID, action == payoutActionApprove, note)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgPayoutMissing)
		return
	case errors.Is(err, store.ErrStateConflict):
		// 回读最新状态给出可读 message（含当前状态）。
		latest, readErr := h.store.PayoutByID(ctx, id)
		if readErr != nil {
			failDB(c, h.logger, readErr)
			return
		}
		response.Fail(c, response.CodeValidationFailed,
			payoutStateConflictMessage(latest.Status, attempted+string(action)))
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	h.logger.Info("出账申请已审核",
		"admin_id", admin.ID, "payout_id", id, "action", string(action),
		"already", result.Already, "status", result.Payout.Status)
	setOplogSummary(c, fmt.Sprintf("审核%s #%d：%s（重复审核=%t，当前状态 %s）",
		oplogPayoutKindLabel(existing.Kind), id, string(action), result.Already, result.Payout.Status))
	response.Success(c, reviewResultView{
		Payout:  h.view(result.Payout, nil),
		Already: result.Already,
	})
}

// markWithdrawalPaid 处理 POST /api/v1/admin/referral/withdrawals/:id/mark-paid（仅 admin）。
// 线下已打款 → paid，记录 paid_at；重复打款幂等（不覆盖首次 paid_at）。
func (h *referralPayoutHandler) markWithdrawalPaid(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	id, ok := payoutIDParam(c)
	if !ok {
		return
	}
	// 请求体可省略；有则解析 review_note。
	var req markPaidRequest
	if c.Request.ContentLength > 0 && !bindJSON(c, &req) {
		return
	}
	note, err := validatePayoutRemark(derefString(req.ReviewNote))
	if err != nil {
		failRuleError(c, err)
		return
	}

	ctx := c.Request.Context()
	existing, err := h.store.PayoutByID(ctx, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgPayoutMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	if existing.Kind != model.PayoutKindWithdraw {
		response.Fail(c, response.CodeNotFound, msgPayoutMissing)
		return
	}

	result, err := h.store.MarkPayoutPaid(ctx, id, admin.ID, note)
	switch {
	case errors.Is(err, store.ErrStateConflict):
		latest, readErr := h.store.PayoutByID(ctx, id)
		if readErr != nil {
			failDB(c, h.logger, readErr)
			return
		}
		response.Fail(c, response.CodeValidationFailed,
			payoutStateConflictMessage(latest.Status, "标记打款"))
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	h.logger.Info("提现申请已标记打款",
		"admin_id", admin.ID, "payout_id", id, "already", result.Already)
	setOplogSummary(c, fmt.Sprintf("提现申请 #%d 标记为已打款（重复操作=%t）", id, result.Already))
	response.Success(c, reviewResultView{
		Payout:  h.view(result.Payout, nil),
		Already: result.Already,
	})
}

// oplogPayoutKindLabel 把出账申请类型渲染成中文（供操作日志摘要）。
func oplogPayoutKindLabel(kind string) string {
	if kind == model.PayoutKindConvert {
		return "转余额申请"
	}
	return "提现申请"
}

// reviewResultView 是审核/打款响应：带 already 标记（幂等命中为 true）。
type reviewResultView struct {
	Payout  payoutView `json:"payout"`
	Already bool       `json:"already"`
}

// ── 视图 ──────────────────────────────────────────────────────────────────

// view 组装出账申请视图；**敏感字段一律脱敏**（契约 18.5）。
func (h *referralPayoutHandler) view(payout *model.ReferralPayout, member *payoutMemberView) payoutView {
	channel := payout.Channel
	accountName := payout.AccountName
	accountNo := payout.AccountNo
	if payout.Kind == model.PayoutKindConvert {
		// convert 不产生账号信息，恒为空串（模型侧也是空串，这里再兜一层）。
		channel, accountName, accountNo = "", "", ""
	}
	return payoutView{
		ID:          payout.ID,
		Kind:        payout.Kind,
		MemberID:    payout.MemberID,
		Amount:      string(payout.Amount),
		Channel:     channel,
		AccountName: maskAccountName(accountName),
		AccountNo:   maskAccountNo(accountNo),
		Status:      payout.Status,
		Remark:      payout.Remark,
		ReviewNote:  payout.ReviewNote,
		ReviewedBy:  payout.ReviewedBy,
		ReviewedAt:  formatTimePtr(payout.ReviewedAt),
		PaidAt:      formatTimePtr(payout.PaidAt),
		CreatedAt:   formatTime(payout.CreatedAt),
		UpdatedAt:   formatTime(payout.UpdatedAt),
		Member:      member,
	}
}

// payoutIDParam 解析路径参数 :id（正整数）。
func payoutIDParam(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, response.CodeInvalidParam, "申请 ID 必须为正整数")
		return 0, false
	}
	return id, true
}
