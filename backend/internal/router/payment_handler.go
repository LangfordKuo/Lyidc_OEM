package router

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/payment"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// epayReturnPage 是未配置 return_url 时的同步跳转提示页（契约 12.2.4）。
const epayReturnPage = `<!DOCTYPE html>
<html lang="zh-CN">
<head><meta charset="utf-8"><title>支付已完成</title></head>
<body style="font-family:sans-serif;text-align:center;margin-top:80px">
<h2>支付已完成</h2>
<p>请返回商户页面查看订单状态。</p>
</body>
</html>`

// paymentHandler 处理支付回调与同步跳转（渠道 → 本服务）。
//
// 职责边界（契约 12.2.3 / 14.3）：回调只做「验签 → 金额校验 → 单事务入账 → 应答纯文本
// success / fail」；订单交付（上游开通）在**入账事务提交后**触发（异步，不阻塞应答）。
type paymentHandler struct {
	store      *store.Store
	payments   *payment.Registry
	reader     *settings.Reader
	deliveries DeliveryTrigger
	logger     *slog.Logger
}

// epayNotify 处理 POST/GET /api/v1/payments/epay/notify（易支付异步通知）。
//
// 处理顺序与应答口径（契约 12.2.3）：
//  1. 渠道不可用（未启用/配置不完整）→ fail；
//  2. 验签或必要参数校验失败 → fail + WARN；
//  3. trade_status != TRADE_SUCCESS → success + WARN（非成功状态无需处理，也不是错误）；
//  4. 单号前缀决定命中订单（O）还是充值单（R）；找不到本地单 → fail + WARN；
//  5. 金额与本地单不一致 → fail + WARN（防篡改与错配）；
//  6. 单事务入账：已 paid 幂等直接 success；已 cancelled/closed 记 WARN 后 success（不处理）；
//  7. 落库异常 → fail（渠道会按自己的策略重试）。
func (h *paymentHandler) epayNotify(c *gin.Context) {
	ctx := c.Request.Context()

	provider, err := h.payments.Get(ctx, payment.ProviderEpay)
	if err != nil {
		h.logger.Warn("易支付回调：渠道不可用", "error", err, "client_ip", c.ClientIP())
		h.ackEpay(c, false)
		return
	}

	notification, err := provider.ParseNotify(c.Request)
	if err != nil {
		h.logger.Warn("易支付回调：验签或参数校验失败", "error", err, "client_ip", c.ClientIP())
		h.ackEpay(c, false)
		return
	}

	if !notification.Paid {
		h.logger.Warn("易支付回调：非支付成功状态，忽略",
			"out_trade_no", notification.OutTradeNo, "trade_status", notification.TradeStatus)
		h.ackEpay(c, true)
		return
	}

	switch {
	case strings.HasPrefix(notification.OutTradeNo, model.OrderTradeNoPrefix):
		h.ackEpay(c, h.applyOrderPayment(ctx, notification))
	case strings.HasPrefix(notification.OutTradeNo, model.RechargeTradeNoPrefix):
		h.ackEpay(c, h.applyRechargePayment(ctx, notification))
	default:
		h.logger.Warn("易支付回调：单号前缀无法识别", "out_trade_no", notification.OutTradeNo)
		h.ackEpay(c, false)
	}
}

// applyOrderPayment 处理订单回调入账，返回是否应答 success。
func (h *paymentHandler) applyOrderPayment(ctx context.Context, n *payment.Notification) bool {
	order, err := h.store.OrderByTradeNo(ctx, n.OutTradeNo)
	switch {
	case errors.Is(err, store.ErrNotFound):
		h.logger.Warn("易支付回调：订单不存在", "out_trade_no", n.OutTradeNo, "channel_trade_no", n.TradeNo)
		return false
	case err != nil:
		h.logger.Error("易支付回调：查询订单失败", "error", err, "out_trade_no", n.OutTradeNo)
		return false
	}

	if !amountMatches(n.Amount, string(order.FinalAmount)) {
		h.logger.Warn("易支付回调：金额与本地订单不一致，拒绝入账",
			"out_trade_no", n.OutTradeNo, "notify_money", n.Amount, "order_amount", string(order.FinalAmount))
		return false
	}

	result, err := h.store.CompleteOrderPayment(ctx, store.OrderPaymentInput{
		OutTradeNo:     order.TradeNo,
		Channel:        payment.ProviderEpay,
		ChannelTradeNo: n.TradeNo,
		PaidAt:         time.Now().UTC(),
	})
	if err != nil {
		h.logger.Error("易支付回调：订单入账失败", "error", err, "out_trade_no", n.OutTradeNo)
		return false
	}

	switch result.Outcome {
	case store.OutcomeApplied:
		h.logger.Info("易支付回调：订单已支付", "order_id", result.Order.ID, "trade_no", result.Order.TradeNo,
			"channel_trade_no", n.TradeNo, "amount", string(result.Order.FinalAmount))
		// 阶段 5a：入账事务已提交，触发交付（不阻塞回调应答，契约 14.3）。
		h.deliveries.Trigger(result.Order.ID)
	case store.OutcomeAlreadyPaid:
		h.logger.Info("易支付回调：订单已是已支付状态（幂等，不重复处理）",
			"order_id", result.Order.ID, "trade_no", result.Order.TradeNo)
	case store.OutcomeSkipped:
		h.logger.Warn("易支付回调：订单已取消，回调不处理（按成功应答，不再重试）",
			"order_id", result.Order.ID, "trade_no", result.Order.TradeNo,
			"channel_trade_no", n.TradeNo, "amount", string(result.Order.FinalAmount))
	}
	if result.CouponNotCounted {
		h.logger.Warn("优惠码使用次数已达上限，本次未计数（并发边界，不阻断入账）",
			"order_id", result.Order.ID, "trade_no", result.Order.TradeNo)
	}
	return true
}

// applyRechargePayment 处理充值单回调入账（充值单 → 余额 + 流水），返回是否应答 success。
func (h *paymentHandler) applyRechargePayment(ctx context.Context, n *payment.Notification) bool {
	recharge, err := h.store.RechargeByTradeNo(ctx, n.OutTradeNo)
	switch {
	case errors.Is(err, store.ErrNotFound):
		h.logger.Warn("易支付回调：充值单不存在", "out_trade_no", n.OutTradeNo, "channel_trade_no", n.TradeNo)
		return false
	case err != nil:
		h.logger.Error("易支付回调：查询充值单失败", "error", err, "out_trade_no", n.OutTradeNo)
		return false
	}

	if !amountMatches(n.Amount, string(recharge.Amount)) {
		h.logger.Warn("易支付回调：金额与本地充值单不一致，拒绝入账",
			"out_trade_no", n.OutTradeNo, "notify_money", n.Amount, "recharge_amount", string(recharge.Amount))
		return false
	}

	result, err := h.store.CompleteRechargePayment(ctx, store.RechargePaymentInput{
		OutTradeNo:     recharge.TradeNo,
		ChannelTradeNo: n.TradeNo,
		PaidAt:         time.Now().UTC(),
	})
	if err != nil {
		h.logger.Error("易支付回调：充值入账失败", "error", err, "out_trade_no", n.OutTradeNo)
		return false
	}

	switch result.Outcome {
	case store.OutcomeApplied:
		h.logger.Info("易支付回调：充值已入账", "recharge_id", result.Recharge.ID,
			"trade_no", result.Recharge.TradeNo, "channel_trade_no", n.TradeNo,
			"member_id", result.Recharge.MemberID, "amount", string(result.Recharge.Amount))
	case store.OutcomeAlreadyPaid:
		h.logger.Info("易支付回调：充值单已入账（幂等，不重复加款）",
			"recharge_id", result.Recharge.ID, "trade_no", result.Recharge.TradeNo)
	case store.OutcomeSkipped:
		h.logger.Warn("易支付回调：充值单已关闭，回调不处理（按成功应答，不再重试）",
			"recharge_id", result.Recharge.ID, "trade_no", result.Recharge.TradeNo)
	}
	return true
}

// epayReturn 处理 GET /api/v1/payments/epay/return（同步跳转）。
//
// 最简实现（契约 12.2.4）：302 到后台设置里的 return_url；未配置时输出简单提示页。
// 同步跳转不参与入账（以异步通知为准），因此这里不验签、不改状态，
// 也不依赖渠道是否启用——跳转目标属于部署设置，而不是渠道能力。
func (h *paymentHandler) epayReturn(c *gin.Context) {
	if target, ok := h.returnURL(c.Request.Context()); ok {
		c.Redirect(http.StatusFound, target)
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, epayReturnPage)
}

// returnURL 读取后台设置里的同步跳转目标；未配置或读取失败时返回 false（输出提示页）。
func (h *paymentHandler) returnURL(ctx context.Context) (string, bool) {
	if h.reader == nil {
		return "", false
	}
	state, err := h.reader.Epay(ctx)
	if err != nil {
		h.logger.Warn("读取同步跳转设置失败，输出默认提示页", "error", err)
		return "", false
	}
	target := strings.TrimSpace(state.Value.ReturnURL)
	return target, target != ""
}

// ackEpay 按易支付协议应答纯文本（HTTP 200 + success / fail）。
func (h *paymentHandler) ackEpay(c *gin.Context, ok bool) {
	if ok {
		c.String(http.StatusOK, payment.AckSuccess)
		return
	}
	c.String(http.StatusOK, payment.AckFail)
}

// amountMatches 比较渠道回传金额与本地单金额（都按整数分比较）；
// 任一解析失败都视为不一致（拒绝入账）。
func amountMatches(notifyAmount, localAmount string) bool {
	notifyCents, err := pricing.ParseAmount(notifyAmount)
	if err != nil {
		return false
	}
	localCents, err := pricing.ParseAmount(localAmount)
	if err != nil {
		return false
	}
	return notifyCents == localCents
}
