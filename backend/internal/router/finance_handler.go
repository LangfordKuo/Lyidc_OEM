package router

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/payment"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 充值单支付标题（渠道侧展示）。
const rechargeSubject = "余额充值"

// financeHandler 处理财务接口：会员端充值/余额/流水，管理端对账列表。
type financeHandler struct {
	store    *store.Store
	payments *payment.Registry
	logger   *slog.Logger
}

// rechargeView 是充值单对外视图（金额为定点小数字符串，时间 RFC3339 UTC）。
type rechargeView struct {
	ID             uint64  `json:"id"`
	TradeNo        string  `json:"trade_no"`
	MemberID       uint64  `json:"member_id"`
	Amount         string  `json:"amount"`
	Channel        string  `json:"channel"`
	Status         string  `json:"status"`
	ChannelTradeNo *string `json:"channel_trade_no"`
	CreatedAt      string  `json:"created_at"`
	PaidAt         *string `json:"paid_at"`
	ExpiresAt      *string `json:"expires_at"`
}

// rechargeListView 是充值单分页列表。
type rechargeListView struct {
	Items    []rechargeView `json:"items"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Total    int64          `json:"total"`
}

// createRechargeView 是创建充值单的返回：充值单 + 渠道支付参数。
type createRechargeView struct {
	Recharge rechargeView `json:"recharge"`
	Pay      payView      `json:"pay"`
}

// ledgerView 是余额流水对外视图（amount 为有符号金额：入账为正、出账为负）。
type ledgerView struct {
	ID            uint64 `json:"id"`
	MemberID      uint64 `json:"member_id"`
	Type          string `json:"type"`
	Amount        string `json:"amount"`
	BalanceBefore string `json:"balance_before"`
	BalanceAfter  string `json:"balance_after"`
	RefType       string `json:"ref_type"`
	RefID         uint64 `json:"ref_id"`
	Note          string `json:"note"`
	CreatedAt     string `json:"created_at"`
}

// ledgerListView 是余额流水分页列表。
type ledgerListView struct {
	Items    []ledgerView `json:"items"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
	Total    int64        `json:"total"`
}

// balanceView 是余额查询结果。
type balanceView struct {
	MemberID uint64 `json:"member_id"`
	Balance  string `json:"balance"`
}

// createRechargeRequest 是 POST /api/v1/recharges 请求体。
type createRechargeRequest struct {
	Amount  string `json:"amount"`
	Channel string `json:"channel"`
	PayType string `json:"pay_type"`
}

// createRecharge 处理 POST /api/v1/recharges：创建充值单并跳转渠道支付。
//
// 流程（契约 12.4）：校验金额（1.00 ~ 50000.00）与渠道 → 创建 pending 充值单 →
// 渠道下单 → 返回 {recharge, pay}。渠道下单失败时充值单保持 pending（可通过回调或后续重试入账），
// 不做回滚——单号唯一，重复发起不会重复入账（回调按 paid 状态幂等）。
func (h *financeHandler) createRecharge(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req createRechargeRequest
	if !bindJSON(c, &req) {
		return
	}
	amountCents, err := parseMoneyBounds("amount", req.Amount, minRechargeCents, maxRechargeCents)
	if err != nil {
		failRuleError(c, err)
		return
	}
	channel := strings.ToLower(strings.TrimSpace(req.Channel))
	if channel != payment.ProviderEpay {
		response.Fail(c, response.CodeValidationFailed,
			fmt.Sprintf("channel 只能是 %s（本批仅支持易支付）", payment.ProviderEpay))
		return
	}

	ctx := c.Request.Context()
	provider, err := h.payments.Get(ctx, payment.ProviderEpay)
	switch {
	case errors.Is(err, payment.ErrUnknownChannel), errors.Is(err, payment.ErrNotConfigured):
		response.Fail(c, response.CodeValidationFailed, msgPaymentNotConfigured)
		return
	case err != nil:
		failInternal(c, h.logger, err)
		return
	}

	amount := pricing.FormatAmount(amountCents)
	recharge, err := createWithTradeNo(model.RechargeTradeNoPrefix, func(tradeNo string) (*model.Recharge, error) {
		return h.store.CreateRecharge(ctx, store.RechargeInput{
			TradeNo:  tradeNo,
			MemberID: member.ID,
			Amount:   amount,
			Channel:  channel,
		})
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	result, err := provider.CreateOrder(ctx, payment.CreateRequest{
		OutTradeNo: recharge.TradeNo,
		Amount:     amount,
		Subject:    rechargeSubject,
		PayType:    strings.TrimSpace(req.PayType),
	})
	switch {
	case errors.Is(err, payment.ErrPayTypeUnsupported):
		response.Fail(c, response.CodeValidationFailed, err.Error())
		return
	case err != nil:
		h.logger.Error("充值单渠道下单失败", "error", err, "recharge_id", recharge.ID, "trade_no", recharge.TradeNo)
		response.Fail(c, response.CodePaymentGateway, "支付渠道下单失败："+err.Error())
		return
	}

	h.logger.Info("充值单已创建", "recharge_id", recharge.ID, "trade_no", recharge.TradeNo,
		"member_id", member.ID, "amount", amount, "pay_type", result.PayType)
	response.Success(c, createRechargeView{
		Recharge: newRechargeView(recharge),
		Pay: payView{
			Channel:        payment.ProviderEpay,
			PayType:        result.PayType,
			ChannelTradeNo: result.TradeNo,
			PayURL:         result.PayURL,
			Extra:          result.Extra,
		},
	})
}

// listRecharges 处理 GET /api/v1/recharges：本人充值单分页（新建在前）。
func (h *financeHandler) listRecharges(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	filter, ok := rechargeFilter(c, member.ID)
	if !ok {
		return
	}

	items, total, err := h.store.ListRecharges(c.Request.Context(), filter)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	views := make([]rechargeView, 0, len(items))
	for i := range items {
		views = append(views, newRechargeView(&items[i]))
	}
	response.Success(c, rechargeListView{Items: views, Page: filter.Page, PageSize: filter.PageSize, Total: total})
}

// listAdminRecharges 处理 GET /api/v1/admin/recharges：全站充值单分页 + 会员/状态筛选（对账用）。
func (h *financeHandler) listAdminRecharges(c *gin.Context) {
	memberID, err := parseUint64Query("member_id", c.Query("member_id"))
	if err != nil {
		failRuleError(c, err)
		return
	}
	filter, ok := rechargeFilter(c, memberID)
	if !ok {
		return
	}

	items, total, err := h.store.ListRecharges(c.Request.Context(), filter)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	views := make([]rechargeView, 0, len(items))
	for i := range items {
		views = append(views, newRechargeView(&items[i]))
	}
	response.Success(c, rechargeListView{Items: views, Page: filter.Page, PageSize: filter.PageSize, Total: total})
}

// getBalance 处理 GET /api/v1/finance/balance：当前余额（实时读库，不用中间件里的快照）。
func (h *financeHandler) getBalance(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	current, err := h.store.MemberByID(c.Request.Context(), member.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	response.Success(c, balanceView{MemberID: current.ID, Balance: string(current.Balance)})
}

// listLedger 处理 GET /api/v1/finance/ledger：本人余额流水分页（新建在前）。
func (h *financeHandler) listLedger(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	filter, ok := ledgerFilter(c, member.ID)
	if !ok {
		return
	}

	items, total, err := h.store.ListLedger(c.Request.Context(), filter)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	views := make([]ledgerView, 0, len(items))
	for i := range items {
		views = append(views, newLedgerView(&items[i]))
	}
	response.Success(c, ledgerListView{Items: views, Page: filter.Page, PageSize: filter.PageSize, Total: total})
}

// listAdminLedger 处理 GET /api/v1/admin/ledger：全站流水分页 + 会员/类型筛选（对账用）。
func (h *financeHandler) listAdminLedger(c *gin.Context) {
	memberID, err := parseUint64Query("member_id", c.Query("member_id"))
	if err != nil {
		failRuleError(c, err)
		return
	}
	filter, ok := ledgerFilter(c, memberID)
	if !ok {
		return
	}

	items, total, err := h.store.ListLedger(c.Request.Context(), filter)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	views := make([]ledgerView, 0, len(items))
	for i := range items {
		views = append(views, newLedgerView(&items[i]))
	}
	response.Success(c, ledgerListView{Items: views, Page: filter.Page, PageSize: filter.PageSize, Total: total})
}

// rechargeFilter 解析充值单分页参数（memberID 为 0 表示不过滤会员，管理端用）。
func rechargeFilter(c *gin.Context, memberID uint64) (store.RechargeFilter, bool) {
	page, pageSize, ok := pageParams(c)
	if !ok {
		return store.RechargeFilter{}, false
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !model.IsValidRechargeStatus(status) {
		response.Fail(c, response.CodeInvalidParam, "status 只能是 pending / paid / closed")
		return store.RechargeFilter{}, false
	}
	return store.RechargeFilter{MemberID: memberID, Status: status, Page: page, PageSize: pageSize}, true
}

// ledgerFilter 解析流水分页参数（memberID 为 0 表示不过滤会员，管理端用）。
func ledgerFilter(c *gin.Context, memberID uint64) (store.LedgerFilter, bool) {
	page, pageSize, ok := pageParams(c)
	if !ok {
		return store.LedgerFilter{}, false
	}
	kind := strings.TrimSpace(c.Query("type"))
	if kind != "" && !model.IsValidLedgerType(kind) {
		response.Fail(c, response.CodeInvalidParam, "type 只能是 recharge / order_pay / refund / adjust")
		return store.LedgerFilter{}, false
	}
	return store.LedgerFilter{MemberID: memberID, Type: kind, Page: page, PageSize: pageSize}, true
}

// pageParams 解析 page / page_size（缺省 1 / 20，越界 40001）。
func pageParams(c *gin.Context) (int, int, bool) {
	page, err := intQuery(c, "page", defaultPage, 1, maxPage)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return 0, 0, false
	}
	pageSize, err := intQuery(c, "page_size", defaultPageSize, 1, maxPageSize)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return 0, 0, false
	}
	return page, pageSize, true
}

// newRechargeView 组装充值单视图。
func newRechargeView(recharge *model.Recharge) rechargeView {
	return rechargeView{
		ID:             recharge.ID,
		TradeNo:        recharge.TradeNo,
		MemberID:       recharge.MemberID,
		Amount:         string(recharge.Amount),
		Channel:        recharge.Channel,
		Status:         recharge.Status,
		ChannelTradeNo: recharge.ChannelTradeNo,
		CreatedAt:      formatTime(recharge.CreatedAt),
		PaidAt:         formatTimePtr(recharge.PaidAt),
		ExpiresAt:      formatTimePtr(recharge.ExpiresAt),
	}
}

// newLedgerView 组装流水视图。
func newLedgerView(entry *model.Ledger) ledgerView {
	return ledgerView{
		ID:            entry.ID,
		MemberID:      entry.MemberID,
		Type:          entry.Type,
		Amount:        string(entry.Amount),
		BalanceBefore: string(entry.BalanceBefore),
		BalanceAfter:  string(entry.BalanceAfter),
		RefType:       entry.RefType,
		RefID:         entry.RefID,
		Note:          entry.Note,
		CreatedAt:     formatTime(entry.CreatedAt),
	}
}
