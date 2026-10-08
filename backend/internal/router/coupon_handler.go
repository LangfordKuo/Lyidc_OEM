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

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 优惠码相关提示文案。
const (
	msgCouponMissing       = "优惠码不存在"
	msgCouponStatusInvalid = "status 只能是 on 或 off"
)

// couponHandler 处理优惠码接口：管理端 CRUD（阶段 3b）+ 公开校验接口。
// 折扣的**应用**（下单抵扣、使用记账）留到订单/支付阶段，本阶段只做规则与校验。
type couponHandler struct {
	store  *store.Store
	logger *slog.Logger
}

// couponView 是优惠码对外视图。value 为定点小数字符串（percent 时是百分比）；
// cycles 为空数组表示全部 6 周期；starts_at / expires_at 为 null 表示不限制。
type couponView struct {
	ID        uint64   `json:"id"`
	Code      string   `json:"code"`
	Type      string   `json:"type"`
	Value     string   `json:"value"`
	Cycles    []string `json:"cycles"`
	StartsAt  *string  `json:"starts_at"`
	ExpiresAt *string  `json:"expires_at"`
	MaxUses   int      `json:"max_uses"`
	UsedCount int      `json:"used_count"`
	Status    string   `json:"status"`
	Comment   string   `json:"comment"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// couponListView 是管理端优惠码分页列表。
type couponListView struct {
	Items    []couponView `json:"items"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
	Total    int64        `json:"total"`
}

// couponValidateView 是公开校验接口的数据体。valid=true 时带折扣明细；
// valid=false 时只带 reason（枚举见 coupon_rule.go）。
type couponValidateView struct {
	Valid          bool   `json:"valid"`
	Reason         string `json:"reason,omitempty"`
	Code           string `json:"code,omitempty"`
	Type           string `json:"type,omitempty"`
	Value          string `json:"value,omitempty"`
	Price          string `json:"price,omitempty"`
	DiscountAmount string `json:"discount_amount,omitempty"`
	FinalAmount    string `json:"final_amount,omitempty"`
}

// createCouponRequest 是 POST /api/v1/admin/coupons 请求体。
type createCouponRequest struct {
	Code      string    `json:"code"`
	Type      string    `json:"type"`
	Value     string    `json:"value"`
	Cycles    *[]string `json:"cycles"`
	StartsAt  *string   `json:"starts_at"`
	ExpiresAt *string   `json:"expires_at"`
	MaxUses   *int      `json:"max_uses"`
	Status    *string   `json:"status"`
	Comment   *string   `json:"comment"`
}

// couponUpdateRequest 是 PUT /api/v1/admin/coupons/:id 请求体，字段均可选但至少提供一个。
// cycles / starts_at / expires_at 用 json.RawMessage 承接三态：未提供（不修改）/
// null（清空——时间写 NULL、周期恢复全部）/ 具体值。type 创建后不可修改。
type couponUpdateRequest struct {
	Code      *string         `json:"code"`
	Value     *string         `json:"value"`
	Cycles    json.RawMessage `json:"cycles"`
	StartsAt  json.RawMessage `json:"starts_at"`
	ExpiresAt json.RawMessage `json:"expires_at"`
	MaxUses   *int            `json:"max_uses"`
	Status    *string         `json:"status"`
	Comment   *string         `json:"comment"`
}

// provided 判断更新请求是否至少提供了一个字段。
func (r *couponUpdateRequest) provided() bool {
	return r.Code != nil || r.Value != nil || len(r.Cycles) > 0 || len(r.StartsAt) > 0 ||
		len(r.ExpiresAt) > 0 || r.MaxUses != nil || r.Status != nil || r.Comment != nil
}

// createCoupon 处理 POST /api/v1/admin/coupons：创建优惠码。
// 角色守卫（admin/finance）在路由注册处挂载。
func (h *couponHandler) createCoupon(c *gin.Context) {
	var req createCouponRequest
	if !bindJSON(c, &req) {
		return
	}

	code := strings.TrimSpace(req.Code)
	if err := validateCouponCode(code); err != nil {
		failRuleError(c, err)
		return
	}
	couponType := strings.ToLower(strings.TrimSpace(req.Type))
	if err := validateCouponType(couponType); err != nil {
		failRuleError(c, err)
		return
	}
	value := strings.TrimSpace(req.Value)
	if _, err := validateCouponValue(couponType, value); err != nil {
		failRuleError(c, err)
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

	status := model.CouponStatusOn
	if req.Status != nil {
		status = strings.ToLower(strings.TrimSpace(*req.Status))
		if !model.IsValidCouponStatus(status) {
			response.Fail(c, response.CodeInvalidParam, msgCouponStatusInvalid)
			return
		}
	}

	comment := ""
	if req.Comment != nil {
		comment = strings.TrimSpace(*req.Comment)
	}
	if err := validateCouponComment(comment); err != nil {
		failRuleError(c, err)
		return
	}

	coupon, err := h.store.CreateCoupon(c.Request.Context(), store.CouponInput{
		Code:       code,
		Type:       couponType,
		Value:      value,
		CyclesJSON: cyclesJSON,
		StartsAt:   startsAt,
		ExpiresAt:  expiresAt,
		MaxUses:    maxUses,
		Status:     status,
		Comment:    comment,
	})
	switch {
	case errors.Is(err, store.ErrCouponCodeTaken):
		response.Fail(c, response.CodeConflict, "优惠码已存在（code 比较不区分大小写）")
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	response.Success(c, h.couponView(coupon))
}

// listCoupons 处理 GET /api/v1/admin/coupons：分页 + status / keyword 过滤（新建在前）。
func (h *couponHandler) listCoupons(c *gin.Context) {
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
	if status != "" && !model.IsValidCouponStatus(status) {
		response.Fail(c, response.CodeInvalidParam, msgCouponStatusInvalid)
		return
	}

	items, total, err := h.store.ListCoupons(c.Request.Context(), store.CouponFilter{
		Page:     page,
		PageSize: pageSize,
		Status:   status,
		Keyword:  strings.TrimSpace(c.Query("keyword")),
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]couponView, 0, len(items))
	for i := range items {
		views = append(views, h.couponView(&items[i]))
	}
	response.Success(c, couponListView{
		Items:    views,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	})
}

// getCoupon 处理 GET /api/v1/admin/coupons/:id。
func (h *couponHandler) getCoupon(c *gin.Context) {
	id, ok := couponIDParam(c)
	if !ok {
		return
	}

	coupon, err := h.store.CouponByID(c.Request.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgCouponMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	response.Success(c, h.couponView(coupon))
}

// updateCoupon 处理 PUT /api/v1/admin/coupons/:id：改 code / value / 适用周期 / 时间 /
// 次数上限 / 状态 / 备注；type 不可修改（请求中的 type 字段被忽略）。
// 角色守卫（admin/finance）在路由注册处挂载。
func (h *couponHandler) updateCoupon(c *gin.Context) {
	id, ok := couponIDParam(c)
	if !ok {
		return
	}

	var req couponUpdateRequest
	if !bindJSON(c, &req) {
		return
	}
	if !req.provided() {
		response.Fail(c, response.CodeInvalidParam,
			"至少提供一个字段：code / value / cycles / starts_at / expires_at / max_uses / status / comment")
		return
	}

	ctx := c.Request.Context()
	current, err := h.store.CouponByID(ctx, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgCouponMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	update := store.CouponUpdate{}
	if req.Code != nil {
		code := strings.TrimSpace(*req.Code)
		if err := validateCouponCode(code); err != nil {
			failRuleError(c, err)
			return
		}
		update.Code = &code
	}
	if req.Value != nil {
		value := strings.TrimSpace(*req.Value)
		if _, err := validateCouponValue(current.Type, value); err != nil {
			failRuleError(c, err)
			return
		}
		update.Value = &value
	}
	if len(req.Cycles) > 0 {
		cyclesJSON, err := updateCyclesJSON(req.Cycles)
		if err != nil {
			failRuleError(c, err)
			return
		}
		update.CyclesJSON = &cyclesJSON
	}

	// 时间字段按「更新后的最终值」校验先后关系（未提供的沿用库内现值）。
	startsAt := current.StartsAt
	if len(req.StartsAt) > 0 {
		parsed, err := parseCouponTimeRaw(req.StartsAt)
		if err != nil {
			failRuleError(c, err)
			return
		}
		update.StartsAt = store.CouponTimeUpdate{Set: true, Value: parsed}
		startsAt = parsed
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
	if err := validateCouponWindow(startsAt, expiresAt); err != nil {
		failRuleError(c, err)
		return
	}

	if req.MaxUses != nil {
		if err := validateCouponMaxUses(*req.MaxUses); err != nil {
			failRuleError(c, err)
			return
		}
		update.MaxUses = req.MaxUses
	}
	if req.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*req.Status))
		if !model.IsValidCouponStatus(status) {
			response.Fail(c, response.CodeInvalidParam, msgCouponStatusInvalid)
			return
		}
		update.Status = &status
	}
	if req.Comment != nil {
		comment := strings.TrimSpace(*req.Comment)
		if err := validateCouponComment(comment); err != nil {
			failRuleError(c, err)
			return
		}
		update.Comment = &comment
	}

	updated, err := h.store.UpdateCoupon(ctx, id, update)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgCouponMissing)
		return
	case errors.Is(err, store.ErrCouponCodeTaken):
		response.Fail(c, response.CodeConflict, "优惠码已存在（code 比较不区分大小写）")
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	response.Success(c, h.couponView(updated))
}

// validateCoupon 处理 GET /api/v1/coupons/:code/validate?product_id=&cycle=（公开，无需 token）。
//
// 语义（契约见 docs/api-contract.md 优惠码章节）：
//   - code 不存在 → 404；product_id / cycle 格式非法 → 40001；商品不存在或已下架 → 404；
//   - 业务无效 → HTTP 200 + {valid:false, reason}（reason 枚举见 coupon_rule.go）；
//   - 有效 → HTTP 200 + 折扣明细（price 为该商品该周期的本地售价）。
func (h *couponHandler) validateCoupon(c *gin.Context) {
	rawProductID := strings.TrimSpace(c.Query("product_id"))
	productID, err := strconv.ParseUint(rawProductID, 10, 64)
	if err != nil || productID == 0 {
		response.Fail(c, response.CodeInvalidParam, "product_id 必须为正整数（本地商品 ID）")
		return
	}
	cycle := strings.TrimSpace(c.Query("cycle"))
	if !pricing.IsValidCycle(cycle) {
		response.Fail(c, response.CodeInvalidParam,
			fmt.Sprintf("cycle 需为以下之一：%s", strings.Join(pricing.Cycles, " / ")))
		return
	}

	ctx := c.Request.Context()
	coupon, err := h.store.CouponByCode(ctx, strings.TrimSpace(c.Param("code")))
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgCouponMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	// 下架商品与不存在的商品统一 404（与会员端商品接口语义一致）。
	product, err := h.store.MemberProductByID(ctx, productID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgProductMissing)
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
	if reason := couponInvalidReason(coupon, cycles, cycle, time.Now().UTC()); reason != "" {
		response.Success(c, couponValidateView{Valid: false, Reason: reason})
		return
	}

	// 该商品该周期本地不可售（无售价）时无折扣可算，归入 cycle_not_applicable。
	_, _, prices, err := productPrices(product)
	if err != nil {
		h.logger.Warn("商品定价数据异常，按不可售处理", "error", err, "product_id", product.ID)
		prices = nil
	}
	price := prices[cycle]
	if price == "" {
		response.Success(c, couponValidateView{Valid: false, Reason: ReasonCycleNotApplicable})
		return
	}

	discount, final, err := pricing.CouponDiscount(price, coupon.Type, string(coupon.Value))
	if err != nil {
		failInternal(c, h.logger, fmt.Errorf("优惠码 %d 折扣计算失败: %w", coupon.ID, err))
		return
	}
	response.Success(c, couponValidateView{
		Valid:          true,
		Code:           coupon.Code,
		Type:           coupon.Type,
		Value:          formatCouponValue(coupon.Value),
		Price:          price,
		DiscountAmount: discount,
		FinalAmount:    final,
	})
}

// couponView 组装优惠码视图（cycles 解析异常时按空数组输出并告警）。
func (h *couponHandler) couponView(coupon *model.Coupon) couponView {
	cycles, err := parseCouponCycles(coupon.CyclesJSON)
	if err != nil {
		h.logger.Warn("优惠码适用周期解析失败，按全部周期输出", "error", err, "coupon_id", coupon.ID)
		cycles = nil
	}
	if cycles == nil {
		cycles = []string{}
	}
	return couponView{
		ID:        coupon.ID,
		Code:      coupon.Code,
		Type:      coupon.Type,
		Value:     formatCouponValue(coupon.Value),
		Cycles:    cycles,
		StartsAt:  formatTimePtr(coupon.StartsAt),
		ExpiresAt: formatTimePtr(coupon.ExpiresAt),
		MaxUses:   coupon.MaxUses,
		UsedCount: coupon.UsedCount,
		Status:    coupon.Status,
		Comment:   coupon.Comment,
		CreatedAt: formatTime(coupon.CreatedAt),
		UpdatedAt: formatTime(coupon.UpdatedAt),
	}
}

// updateCyclesJSON 处理更新请求里的 cycles 字段：null 表示恢复为全部周期（"[]"），
// 数组则校验并规范化。
func updateCyclesJSON(raw json.RawMessage) (string, error) {
	if strings.TrimSpace(string(raw)) == "null" {
		return normalizeCouponCycles(nil)
	}
	var cycles []string
	if err := json.Unmarshal(raw, &cycles); err != nil {
		return "", fmt.Errorf("%w: cycles 需为周期名数组或 null", errCouponFormat)
	}
	return normalizeCouponCycles(cycles)
}

// couponIDParam 解析路径参数 :id。
func couponIDParam(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, response.CodeInvalidParam, "优惠码 ID 必须为正整数")
		return 0, false
	}
	return id, true
}

// derefString 解引用可空字符串（nil 返回空串）。
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// formatCouponValue 把折扣值归一为两位小数的定点字符串（如 "10" → "10.00"），
// 与 price / discount_amount / final_amount 的输出格式保持一致。
// 解析失败时原样返回（值只能由管理端接口写入，正常不会出现）。
func formatCouponValue(value model.Money) string {
	cents, err := pricing.ParseCouponValue(string(value))
	if err != nil {
		return string(value)
	}
	return pricing.FormatAmount(cents)
}
