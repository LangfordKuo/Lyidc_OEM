package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/delivery"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/payment"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 订单相关提示文案。
const (
	msgOrderMissing         = "订单不存在"
	msgPaymentNotConfigured = "支付渠道未配置或未启用（请联系管理员在后台设置中填写并启用）"
)

// orderHandler 处理订单接口：创建（含优惠码抵扣）/ 列表 / 详情 / 发起支付 / 取消 /
// 管理员重试交付（阶段 5a）。
//
// 交付边界（契约 14.3）：下单与支付链路只在**入账事务提交后**触发交付（异步，不阻塞响应）；
// 上游开通由 internal/delivery 完成，本 handler 不做任何上游调用（重试交付除外，见 retryDelivery）。
type orderHandler struct {
	store      *store.Store
	payments   *payment.Registry
	deliveries DeliveryTrigger
	logger     *slog.Logger
}

// createOrderRequest 是 POST /api/v1/orders 请求体。
//
// Config 的键与值都是**上游配置项的本地 ID**：键为该商品可配置项的 id（`options[].id`），
// 值为所选可选值的 id（`values[].id`）；值接受 JSON 字符串或整数写法。
// （口径依据见 order_rule.go validateOrderConfig 的说明与契约 14.5。）
type createOrderRequest struct {
	ProductID  uint64                     `json:"product_id"`
	Cycle      string                     `json:"cycle"`
	Config     map[string]json.RawMessage `json:"config"`
	CouponCode string                     `json:"coupon_code"`
}

// payOrderRequest 是 POST /api/v1/orders/:id/pay 请求体。
type payOrderRequest struct {
	Channel string `json:"channel"`
	PayType string `json:"pay_type"`
}

// orderView 是订单对外视图（金额均为定点小数字符串，时间为 RFC3339 UTC）。
type orderView struct {
	ID             uint64            `json:"id"`
	TradeNo        string            `json:"trade_no"`
	MemberID       uint64            `json:"member_id"`
	ProductID      uint64            `json:"product_id"`
	ProductName    string            `json:"product_name"`
	Cycle          string            `json:"cycle"`
	Qty            int               `json:"qty"`
	Config         map[string]string `json:"config"`
	Amount         string            `json:"amount"`
	DiscountAmount string            `json:"discount_amount"`
	FinalAmount    string            `json:"final_amount"`
	CouponCode     string            `json:"coupon_code"`
	Status         string            `json:"status"`
	Type           string            `json:"type"`
	PayChannel     string            `json:"pay_channel"`
	ChannelTradeNo string            `json:"channel_trade_no"`
	PayTime        *string           `json:"pay_time"`
	HostID         *int              `json:"host_id"`
	InstanceID     *uint64           `json:"instance_id"`
	ProvisionError string            `json:"provision_error"`
	DeliveredAt    *string           `json:"delivered_at"`
	CreatedAt      string            `json:"created_at"`
	UpdatedAt      string            `json:"updated_at"`
}

// orderListView 是订单分页列表。
type orderListView struct {
	Items    []orderView `json:"items"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
	Total    int64       `json:"total"`
}

// payView 是发起支付的返回：epay 渠道给支付链接，余额支付给扣款结果。
type payView struct {
	Channel        string            `json:"channel"`
	PayType        string            `json:"pay_type,omitempty"`
	ChannelTradeNo string            `json:"channel_trade_no,omitempty"`
	PayURL         string            `json:"payurl,omitempty"`
	Extra          map[string]string `json:"extra,omitempty"`
	Paid           bool              `json:"paid,omitempty"`
	BalanceAfter   string            `json:"balance_after,omitempty"`
}

// payOrderView 是 POST /orders/:id/pay 的数据体。
type payOrderView struct {
	Order orderView `json:"order"`
	Pay   payView   `json:"pay"`
}

// createOrder 处理 POST /api/v1/orders：校验商品/周期/配置项/优惠码 → 计算金额 → 创建 pending 订单。
//
// 校验顺序（契约 12.4）：参数格式（40001）→ 商品存在且上架（404）→ 周期可售（40002）→
// 配置项（40002）→ 优惠码（40002）→ 落库（50001）。
// 库存不做强校验：导入库存是上游快照，防超卖由上游开通环节最终保证（契约 12.9）。
func (h *orderHandler) createOrder(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req createOrderRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.ProductID == 0 {
		response.Fail(c, response.CodeInvalidParam, "product_id 必须为正整数（本地商品 ID）")
		return
	}
	cycle := strings.TrimSpace(req.Cycle)
	if !pricing.IsValidCycle(cycle) {
		response.Fail(c, response.CodeInvalidParam,
			fmt.Sprintf("cycle 需为以下之一：%s", strings.Join(pricing.Cycles, " / ")))
		return
	}

	ctx := c.Request.Context()
	// 下架商品与不存在的商品统一 404（与会员端商品接口、优惠码校验接口语义一致）。
	product, err := h.store.MemberProductByID(ctx, req.ProductID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgProductMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	// 该周期须有本地售价（null 表示不可售）。定价数据异常时按不可售处理，与会员端一致。
	_, _, prices, priceErr := productPrices(product)
	if priceErr != nil {
		h.logger.Warn("商品定价数据异常，按不可售处理", "error", priceErr, "product_id", product.ID)
		prices = nil
	}
	price := prices[cycle]
	if price == "" {
		response.Fail(c, response.CodeValidationFailed,
			fmt.Sprintf("该商品在 %s 周期不可售（无本地售价）", cycle))
		return
	}

	config, err := normalizeOrderConfig(req.Config)
	if err != nil {
		failRuleError(c, err)
		return
	}
	cache, err := parseProductConfigCache(product.ConfigJSON)
	if err != nil {
		h.logger.Warn("商品配置缓存解析失败，按无可配置项处理", "error", err, "product_id", product.ID)
	}
	if err := validateOrderConfig(cache, config); err != nil {
		failRuleError(c, err)
		return
	}
	configJSON, err := encodeOrderConfig(config)
	if err != nil {
		failRuleError(c, err)
		return
	}

	// 优惠码：不存在 / 无效 / 不适用于该周期一律 40002 + 明确提示（契约 12.5）。
	discount := pricing.FormatAmount(0)
	final := price
	var coupon *model.Coupon
	if code := strings.TrimSpace(req.CouponCode); code != "" {
		coupon, err = h.store.CouponByCode(ctx, code)
		switch {
		case errors.Is(err, store.ErrNotFound):
			response.Fail(c, response.CodeValidationFailed, "优惠码不存在")
			return
		case err != nil:
			failDB(c, h.logger, err)
			return
		}
		cycles, err := parseCouponCycles(coupon.CyclesJSON)
		if err != nil {
			h.logger.Warn("优惠码适用周期解析失败，按全部周期处理", "error", err, "coupon_id", coupon.ID)
			cycles = nil
		}
		discount, final, err = applyCoupon(coupon, cycles, cycle, price, time.Now().UTC())
		if err != nil {
			failRuleError(c, err)
			return
		}
	}

	input := store.OrderInput{
		MemberID:       member.ID,
		ProductID:      product.ID,
		ProductName:    product.Name,
		Cycle:          cycle,
		Qty:            1,
		ConfigJSON:     configJSON,
		Amount:         price,
		DiscountAmount: discount,
		FinalAmount:    final,
		CouponCode:     strings.TrimSpace(req.CouponCode),
	}
	if coupon != nil {
		input.CouponID = &coupon.ID
		input.CouponCode = coupon.Code
	}

	order, err := createWithTradeNo(model.OrderTradeNoPrefix, func(tradeNo string) (*model.Order, error) {
		input.TradeNo = tradeNo
		return h.store.CreateOrder(ctx, input)
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	h.logger.Info("订单已创建", "order_id", order.ID, "trade_no", order.TradeNo,
		"member_id", member.ID, "product_id", product.ID, "cycle", cycle,
		"final_amount", string(order.FinalAmount), "coupon", stringOrDash(order.CouponCode))
	response.Success(c, newOrderView(order, h.logger))
}

// listOrders 处理 GET /api/v1/orders：本人订单分页（新建在前），可按 status 过滤。
func (h *orderHandler) listOrders(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	page, err := intQuery(c, "page", defaultPage, 1, maxPage)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	}
	pageSize, err := intQuery(c, "page_size", defaultPageSize, 1, maxPageSize)
	if err != nil {
		response.Fail(c, response.CodeInvalidParam, err.Error())
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !model.IsValidOrderStatus(status) {
		response.Fail(c, response.CodeInvalidParam,
			"status 只能是 pending / paid / provisioning / active / failed / cancelled")
		return
	}

	items, total, err := h.store.ListOrders(c.Request.Context(), store.OrderFilter{
		MemberID: member.ID,
		Status:   status,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]orderView, 0, len(items))
	for i := range items {
		views = append(views, newOrderView(&items[i], h.logger))
	}
	response.Success(c, orderListView{Items: views, Page: page, PageSize: pageSize, Total: total})
}

// getOrder 处理 GET /api/v1/orders/:id：仅本人可见，他人订单与不存在的订单统一 404。
func (h *orderHandler) getOrder(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	id, ok := orderIDParam(c)
	if !ok {
		return
	}

	order, err := h.store.OrderByIDForMember(c.Request.Context(), id, member.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgOrderMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	response.Success(c, newOrderView(order, h.logger))
}

// payOrder 处理 POST /api/v1/orders/:id/pay：按 channel 发起在线支付或余额支付。
//
// 重复发起支付：pending 订单可多次发起（渠道 out_trade_no 固定复用订单本地单号，
// 契约 12.2.5）；已支付 / 已取消订单再次发起返回 40002。
func (h *orderHandler) payOrder(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	id, ok := orderIDParam(c)
	if !ok {
		return
	}

	var req payOrderRequest
	if !bindJSON(c, &req) {
		return
	}
	channel := strings.ToLower(strings.TrimSpace(req.Channel))
	if channel != payment.ProviderEpay && channel != model.PayChannelBalance {
		response.Fail(c, response.CodeValidationFailed,
			fmt.Sprintf("channel 只能是 %s（在线支付）或 %s（余额支付）", payment.ProviderEpay, model.PayChannelBalance))
		return
	}

	ctx := c.Request.Context()
	order, err := h.store.OrderByIDForMember(ctx, id, member.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgOrderMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	if order.Status != model.OrderStatusPending {
		response.Fail(c, response.CodeValidationFailed, orderStatusMessage(order.Status)+"，无法发起支付")
		return
	}

	if channel == model.PayChannelBalance {
		h.payOrderWithBalance(c, order)
		return
	}
	h.payOrderWithChannel(c, order, strings.TrimSpace(req.PayType))
}

// payOrderWithBalance 余额支付：本地单事务扣款 + 写流水 + 订单转 paid + 优惠码计数。
func (h *orderHandler) payOrderWithBalance(c *gin.Context, order *model.Order) {
	result, err := h.store.PayOrderWithBalance(c.Request.Context(), order.ID, order.MemberID)
	switch {
	case errors.Is(err, store.ErrInsufficientBalance):
		response.Fail(c, response.CodeValidationFailed, "余额不足，请先充值或改用在线支付")
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	switch result.Outcome {
	case store.OutcomeAlreadyPaid:
		response.Fail(c, response.CodeValidationFailed, "订单已支付")
		return
	case store.OutcomeSkipped:
		response.Fail(c, response.CodeValidationFailed, "订单已取消，无法支付")
		return
	}
	if result.CouponNotCounted {
		h.logger.Warn("优惠码使用次数已达上限，本次未计数（并发边界，不阻断支付）",
			"order_id", result.Order.ID, "trade_no", result.Order.TradeNo)
	}

	balanceAfter := ""
	if member, err := h.store.MemberByID(c.Request.Context(), order.MemberID); err == nil {
		balanceAfter = string(member.Balance)
	} else {
		h.logger.Warn("余额支付后回读会员余额失败", "error", err, "member_id", order.MemberID)
	}

	h.logger.Info("订单余额支付成功", "order_id", result.Order.ID, "trade_no", result.Order.TradeNo,
		"member_id", order.MemberID, "amount", string(result.Order.FinalAmount))
	// 阶段 5a：入账事务已提交，在此触发交付（不阻塞本次响应，契约 14.3）。
	h.deliveries.Trigger(result.Order.ID)
	response.Success(c, payOrderView{
		Order: newOrderView(result.Order, h.logger),
		Pay:   payView{Channel: model.PayChannelBalance, Paid: true, BalanceAfter: balanceAfter},
	})
}

// payOrderWithChannel 在线支付：向渠道下单并返回支付链接（本批仅易支付）。
func (h *orderHandler) payOrderWithChannel(c *gin.Context, order *model.Order, payType string) {
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

	result, err := provider.CreateOrder(ctx, payment.CreateRequest{
		OutTradeNo: order.TradeNo,
		Amount:     string(order.FinalAmount),
		Subject:    orderSubject(order),
		PayType:    payType,
	})
	switch {
	case errors.Is(err, payment.ErrPayTypeUnsupported):
		response.Fail(c, response.CodeValidationFailed, err.Error())
		return
	case err != nil:
		// 渠道侧故障：已脱敏（错误里只有渠道返回的 msg 摘要），按 50002 上报便于运营定位。
		h.logger.Error("支付渠道下单失败", "error", err, "order_id", order.ID, "trade_no", order.TradeNo)
		response.Fail(c, response.CodePaymentGateway, "支付渠道下单失败："+err.Error())
		return
	}

	h.logger.Info("订单发起在线支付", "order_id", order.ID, "trade_no", order.TradeNo,
		"channel", payment.ProviderEpay, "pay_type", result.PayType, "amount", string(order.FinalAmount))
	response.Success(c, payOrderView{
		Order: newOrderView(order, h.logger),
		Pay: payView{
			Channel:        payment.ProviderEpay,
			PayType:        result.PayType,
			ChannelTradeNo: result.TradeNo,
			PayURL:         result.PayURL,
			Extra:          result.Extra,
		},
	})
}

// cancelOrder 处理 POST /api/v1/orders/:id/cancel：仅本人 + 仅 pending。
//
// 取消不涉及优惠码回退：下单不计数，只有支付成功才计数（契约 12.5）。
func (h *orderHandler) cancelOrder(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	id, ok := orderIDParam(c)
	if !ok {
		return
	}

	order, err := h.store.CancelOrder(c.Request.Context(), id, member.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgOrderMissing)
		return
	case errors.Is(err, store.ErrStateConflict):
		response.Fail(c, response.CodeValidationFailed, orderStatusMessage(order.Status)+"，无法取消")
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	h.logger.Info("订单已取消", "order_id", order.ID, "trade_no", order.TradeNo, "member_id", member.ID)
	response.Success(c, newOrderView(order, h.logger))
}

// retryDelivery 处理 POST /api/v1/admin/orders/:id/retry-delivery：管理员重试交付（仅 admin 角色）。
//
// 同步执行一次完整交付（契约 14.3）：成功返回 active 订单；交付执行失败时订单已置
// failed 并记录脱敏原因，仍按 200 返回最新订单视图（供管理员据此处置或再次重试）。
// 订单状态不允许交付（未支付 / 交付中 / 已交付 / 已取消）返回 40002。
func (h *orderHandler) retryDelivery(c *gin.Context) {
	id, ok := orderIDParam(c)
	if !ok {
		return
	}

	order, err := h.deliveries.Deliver(c.Request.Context(), id, true)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgOrderMissing)
		return
	case errors.Is(err, delivery.ErrNotClaimable):
		response.Fail(c, response.CodeValidationFailed,
			retryDeliveryMessage(order.Status)+"，无法重试交付")
		return
	case errors.Is(err, delivery.ErrProvisionFailed):
		h.logger.Warn("管理员重试交付失败（订单已置 failed，可再次重试）",
			"order_id", order.ID, "trade_no", order.TradeNo, "error", err)
		response.Success(c, newOrderView(order, h.logger))
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	h.logger.Info("管理员重试交付成功", "order_id", order.ID, "trade_no", order.TradeNo,
		"member_id", order.MemberID, "host_id", order.HostID)
	response.Success(c, newOrderView(order, h.logger))
}

// normalizeOrderConfig 归一化下单配置：值接受 JSON 字符串或整数（配置项 id 的数字写法）。
func normalizeOrderConfig(raw map[string]json.RawMessage) (map[string]string, error) {
	config := make(map[string]string, len(raw))
	for key, value := range raw {
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			config[strings.TrimSpace(key)] = strings.TrimSpace(text)
			continue
		}
		var number json.Number
		if err := json.Unmarshal(value, &number); err == nil {
			if _, err := strconv.ParseInt(number.String(), 10, 64); err == nil {
				config[strings.TrimSpace(key)] = number.String()
				continue
			}
		}
		return nil, fmt.Errorf("%w: config 的键与值都必须是配置项 id（字符串或整数写法）",
			errFinanceFormat)
	}
	return config, nil
}

// orderIDParam 解析路径参数 :id。
func orderIDParam(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, response.CodeInvalidParam, "订单 ID 必须为正整数")
		return 0, false
	}
	return id, true
}

// newOrderView 组装订单视图。
func newOrderView(order *model.Order, logger *slog.Logger) orderView {
	return orderView{
		ID:             order.ID,
		TradeNo:        order.TradeNo,
		MemberID:       order.MemberID,
		ProductID:      order.ProductID,
		ProductName:    order.ProductName,
		Cycle:          order.Cycle,
		Qty:            order.Qty,
		Config:         decodeOrderConfig(order.ConfigJSON, logger),
		Amount:         string(order.Amount),
		DiscountAmount: string(order.DiscountAmount),
		FinalAmount:    string(order.FinalAmount),
		CouponCode:     order.CouponCode,
		Status:         order.Status,
		Type:           order.Type,
		PayChannel:     order.PayChannel,
		ChannelTradeNo: order.ChannelTradeNo,
		PayTime:        formatTimePtr(order.PayTime),
		HostID:         order.HostID,
		InstanceID:     order.InstanceID,
		ProvisionError: order.ProvisionError,
		DeliveredAt:    formatTimePtr(order.DeliveredAt),
		CreatedAt:      formatTime(order.CreatedAt),
		UpdatedAt:      formatTime(order.UpdatedAt),
	}
}

// orderSubject 组装订单支付标题（商品名 + 周期），按 rune 截断到渠道友好长度。
func orderSubject(order *model.Order) string {
	return truncateRunes(order.ProductName+" "+order.Cycle, maxPaymentSubjectRunes)
}

// stringOrDash 把空串渲染成 "-"（仅用于日志，避免空字段）。
func stringOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
