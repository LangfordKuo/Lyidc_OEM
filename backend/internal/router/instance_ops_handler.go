package router

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/instanceops"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 实例操作接口（阶段 5b）：会员端电源/重装/改密/续费/操作记录，管理端暂停/恢复/同步/操作记录。
//
// 鉴权与状态约束（契约 15.2 / 15.4）：会员端一律「仅本人实例」；电源/重装/改密仅 active；
// 续费 active / suspended 均可；管理端 suspend / unsuspend 仅 admin 角色、
// sync 所有角色，均不限实例归属。

// powerRequest 是 POST /api/v1/instances/:id/power 请求体。
type powerRequest struct {
	Op string `json:"op"`
}

// reinstallRequest 是 POST /api/v1/instances/:id/reinstall 请求体。
type reinstallRequest struct {
	OSID int `json:"os_id"`
	Port int `json:"port"`
}

// resetPasswordRequest 是 POST /api/v1/instances/:id/reset-password 请求体。
type resetPasswordRequest struct {
	// Password 省略或空串时由服务端生成 16 位强密码。
	Password string `json:"password"`
}

// renewRequest 是 POST /api/v1/instances/:id/renew 请求体。
type renewRequest struct {
	Cycle string `json:"cycle"`
}

// cancelRequest 是 POST /api/v1/instances/:id/cancel 与
// POST /api/v1/admin/instances/:id/cancel 的请求体（阶段 5c，契约 15.8.2）。
type cancelRequest struct {
	// Type 必填：immediate（立即取消）/ end_of_billing（到期取消，等到账单周期结束）。
	Type string `json:"type"`
	// Reason 会员端可空（服务端兜底默认文案）；管理端必填（强制终止需记录原因）。
	Reason string `json:"reason"`
}

// suspendRequest 是 POST /api/v1/admin/instances/:id/suspend 请求体。
type suspendRequest struct {
	Reason string `json:"reason"`
}

// operationResultView 是实例操作类接口的统一返回（message 已脱敏）。
type operationResultView struct {
	InstanceID uint64 `json:"instance_id"`
	Action     string `json:"action"`
	Message    string `json:"message"`
	// Status 是操作后的本地实例状态（同步/暂停/恢复等会改变状态的操作返回）。
	Status string `json:"status"`
	// Password 仅在重置密码成功时返回（会员本人可见，与详情接口一致）。
	Password string `json:"password,omitempty"`
}

// instanceLogView 是一条实例操作记录（message 已脱敏，不含密码与密钥）。
type instanceLogView struct {
	ID         uint64 `json:"id"`
	InstanceID uint64 `json:"instance_id"`
	ActorType  string `json:"actor_type"`
	ActorID    uint64 `json:"actor_id"`
	Action     string `json:"action"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	CreatedAt  string `json:"created_at"`
}

// instanceLogListView 是实例操作记录分页列表。
type instanceLogListView struct {
	Items    []instanceLogView `json:"items"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Total    int64             `json:"total"`
}

// reinstallOptionsView 是重装可选系统列表（上游 /host/cloudos）。
type reinstallOptionsView struct {
	InstanceID uint64                     `json:"instance_id"`
	OS         []reinstallOptionItemView  `json:"os"`
	Groups     []reinstallOptionGroupView `json:"groups"`
}

// reinstallOptionItemView 是一个可选操作系统；id 用于重装接口的 os_id。
type reinstallOptionItemView struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

// reinstallOptionGroupView 是系统分组（上游可能是分组名，可能为空）。
type reinstallOptionGroupView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// syncResultView 是管理端同步接口的返回。
type syncResultView struct {
	operationResultView
	PowerState    string              `json:"power_state"`
	PowerDesc     string              `json:"power_desc"`
	StatusChanged bool                `json:"status_changed"`
	Terminated    bool                `json:"terminated"`
	Instance      instanceSummaryView `json:"instance"`
	NextDueDate   *string             `json:"next_due_date"`
	UpstreamState string              `json:"upstream_status"`
}

// cancelResultView 是取消申请接口的返回（阶段 5c）。
type cancelResultView struct {
	operationResultView
	CancelRequestID   int     `json:"cancel_request_id"`
	CancelType        string  `json:"cancel_type"`
	CancelStatus      string  `json:"cancel_status"`
	CancelRequestedAt *string `json:"cancel_requested_at"`
	// Duplicate 为 true 表示已有在途申请，本次未重复提交上游（幂等返回现状）。
	Duplicate bool `json:"duplicate"`
}

// powerInstance 处理 POST /api/v1/instances/:id/power：soft_on / soft_off / reboot /
// hard_off / hard_reboot（硬操作为高风险，契约 15.2 已标注）。
func (h *instanceHandler) powerInstance(c *gin.Context) {
	member, instance, ok := h.memberInstance(c)
	if !ok {
		return
	}

	var req powerRequest
	if !bindJSON(c, &req) {
		return
	}
	op := strings.TrimSpace(req.Op)
	if !instanceops.IsValidPowerOp(op) {
		response.Fail(c, response.CodeInvalidParam,
			"op 只能是 soft_on / soft_off / reboot / hard_off / hard_reboot（hard_* 为强制断电，风险自负）")
		return
	}

	message, err := h.ops.Power(c.Request.Context(), instance, instanceops.MemberActor(member.ID), instanceops.PowerOp(op))
	if err != nil {
		h.failInstanceOp(c, instance.ID, err)
		return
	}
	h.logger.Info("会员执行实例电源操作", "instance_id", instance.ID, "host_id", instance.HostID,
		"member_id", member.ID, "op", op)
	response.Success(c, operationResultView{
		InstanceID: instance.ID, Action: instanceops.PowerOp(op).AuditAction(), Message: message, Status: instance.Status,
	})
}

// reinstallOptions 处理 GET /api/v1/instances/:id/reinstall-options：重装可选系统列表。
func (h *instanceHandler) reinstallOptions(c *gin.Context) {
	_, instance, ok := h.memberInstance(c)
	if !ok {
		return
	}

	list, err := h.ops.ReinstallOptions(c.Request.Context(), instance)
	if err != nil {
		h.failInstanceOp(c, instance.ID, err)
		return
	}

	items := make([]reinstallOptionItemView, 0, len(list.OS))
	for _, item := range list.OS {
		items = append(items, reinstallOptionItemView{ID: item.ID, Name: item.Name, Group: item.Group})
	}
	groups := make([]reinstallOptionGroupView, 0, len(list.OSGroup))
	for _, group := range list.OSGroup {
		groups = append(groups, reinstallOptionGroupView{ID: group.ID, Name: group.Name})
	}
	response.Success(c, reinstallOptionsView{InstanceID: instance.ID, OS: items, Groups: groups})
}

// reinstallInstance 处理 POST /api/v1/instances/:id/reinstall：发起重装（异步，上游受理即成功）。
func (h *instanceHandler) reinstallInstance(c *gin.Context) {
	member, instance, ok := h.memberInstance(c)
	if !ok {
		return
	}

	var req reinstallRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.OSID <= 0 {
		response.Fail(c, response.CodeInvalidParam, "os_id 必须为正整数（取自重装系统列表）")
		return
	}
	if req.Port < 0 || req.Port > 65535 {
		response.Fail(c, response.CodeInvalidParam, "port 需为 0-65535（0 表示不指定）")
		return
	}

	message, err := h.ops.Reinstall(c.Request.Context(), instance, instanceops.MemberActor(member.ID), req.OSID, req.Port)
	if err != nil {
		h.failInstanceOp(c, instance.ID, err)
		return
	}
	h.logger.Info("会员发起实例重装", "instance_id", instance.ID, "host_id", instance.HostID,
		"member_id", member.ID, "os_id", req.OSID)
	response.Success(c, operationResultView{
		InstanceID: instance.ID, Action: model.ActionReinstall, Message: message, Status: instance.Status,
	})
}

// resetPasswordInstance 处理 POST /api/v1/instances/:id/reset-password：
// 不传密码时自动生成 16 位强密码；传入时校验强度；新密码落库并在响应中返回（仅本人可见）。
func (h *instanceHandler) resetPasswordInstance(c *gin.Context) {
	member, instance, ok := h.memberInstance(c)
	if !ok {
		return
	}

	var req resetPasswordRequest
	if !bindJSON(c, &req) {
		return
	}

	password, err := h.ops.ResetPassword(c.Request.Context(), instance,
		instanceops.MemberActor(member.ID), strings.TrimSpace(req.Password))
	if err != nil {
		h.failInstanceOp(c, instance.ID, err)
		return
	}
	h.logger.Info("会员重置实例密码", "instance_id", instance.ID, "host_id", instance.HostID, "member_id", member.ID)
	response.Success(c, operationResultView{
		InstanceID: instance.ID, Action: model.ActionResetPassword,
		Message: "密码已重置并落库（仅本人可见）", Status: instance.Status, Password: password,
	})
}

// renewInstance 处理 POST /api/v1/instances/:id/renew：按月/季/年等周期创建续费订单（pending）。
//
// 金额取**当前商品**该周期售价（不校验商品上架状态：已购实例允许续费）；
// 本批续费单**不支持优惠码**（不接受 coupon_code，契约 15.4）。
// 支付成功后由交付链路自动续费（RenewHost → 到期时间顺延），与 5a 的新购交付同一骨架。
func (h *instanceHandler) renewInstance(c *gin.Context) {
	member, instance, ok := h.memberInstance(c)
	if !ok {
		return
	}

	var req renewRequest
	if !bindJSON(c, &req) {
		return
	}
	cycle := strings.TrimSpace(req.Cycle)
	if !pricing.IsValidCycle(cycle) {
		response.Fail(c, response.CodeInvalidParam,
			fmt.Sprintf("cycle 需为以下之一：%s", strings.Join(pricing.Cycles, " / ")))
		return
	}
	if !model.IsInstanceRenewable(instance.Status) {
		response.Fail(c, response.CodeValidationFailed,
			fmt.Sprintf("实例当前状态为 %s，仅 active / suspended 可续费", instance.Status))
		return
	}
	// 阶段 5c：有在途取消申请时不可续费（上游可能按申请终止主机，续费会白付；
	// 上游的撤销申请能力（DELETE /host/cancel）留后续批次，契约 15.8.4）。
	if instance.CancelStatus == model.InstanceCancelPending {
		response.Fail(c, response.CodeValidationFailed,
			"实例已有在途取消申请，无法续费（如需继续使用请联系管理员）")
		return
	}

	ctx := c.Request.Context()
	product, err := h.store.ProductByID(ctx, instance.ProductID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeValidationFailed, "实例关联的商品已不存在，无法续费（请联系管理员）")
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	_, _, prices, priceErr := productPrices(product)
	if priceErr != nil {
		h.logger.Warn("商品定价数据异常，按不可售处理", "error", priceErr, "product_id", product.ID)
		prices = nil
	}
	price := prices[cycle]
	if price == "" {
		response.Fail(c, response.CodeValidationFailed,
			fmt.Sprintf("该商品在 %s 周期不可售（无本地售价），无法续费", cycle))
		return
	}

	instanceID := instance.ID
	order, err := createWithTradeNo(model.OrderTradeNoPrefix, func(tradeNo string) (*model.Order, error) {
		return h.store.CreateOrder(ctx, store.OrderInput{
			TradeNo:        tradeNo,
			MemberID:       member.ID,
			ProductID:      instance.ProductID,
			ProductName:    instance.ProductName,
			Cycle:          cycle,
			Qty:            1,
			ConfigJSON:     "{}",
			Amount:         price,
			DiscountAmount: pricing.FormatAmount(0),
			FinalAmount:    price,
			Type:           model.OrderTypeRenew,
			InstanceID:     &instanceID,
		})
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	h.logger.Info("续费订单已创建", "order_id", order.ID, "trade_no", order.TradeNo,
		"member_id", member.ID, "instance_id", instance.ID, "cycle", cycle, "amount", price)
	response.Success(c, newOrderView(order, h.logger))
}

// cancelInstance 处理 POST /api/v1/instances/:id/cancel：会员本人提交取消（终止）申请。
//
// 允许状态 active / suspended（契约 15.8.1）；已有在途申请时**幂等返回现状**（`duplicate=true`，
// 不重复提交上游）；成功后本地记 cancel_status=pending（status 保持原值），
// 上游处理完毕后由管理端同步或到期扫描收敛为 terminated。
func (h *instanceHandler) cancelInstance(c *gin.Context) {
	member, instance, ok := h.memberInstance(c)
	if !ok {
		return
	}

	var req cancelRequest
	if !bindJSON(c, &req) {
		return
	}
	cancelType := strings.TrimSpace(req.Type)
	if !model.IsValidCancelType(cancelType) {
		response.Fail(c, response.CodeInvalidParam, msgCancelTypeInvalid)
		return
	}

	result, err := h.ops.Cancel(c.Request.Context(), instance,
		instanceops.MemberActor(member.ID), cancelType, req.Reason)
	if err != nil {
		h.failInstanceOp(c, instance.ID, err)
		return
	}
	h.logger.Info("会员提交实例取消申请", "instance_id", instance.ID, "host_id", instance.HostID,
		"member_id", member.ID, "cancel_type", cancelType, "duplicate", result.Duplicate,
		"cancel_request_id", result.CancelRequestID)
	response.Success(c, newCancelResultView(result))
}

// listMyInstanceLogs 处理 GET /api/v1/instances/:id/logs：本人实例的操作记录。
func (h *instanceHandler) listMyInstanceLogs(c *gin.Context) {
	_, instance, ok := h.memberInstance(c)
	if !ok {
		return
	}
	h.respondInstanceLogs(c, instance.ID)
}

// adminSuspendInstance 处理 POST /api/v1/admin/instances/:id/suspend（仅 admin 角色，reason 必填）。
func (h *instanceHandler) adminSuspendInstance(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	instance, ok := h.adminInstance(c)
	if !ok {
		return
	}

	var req suspendRequest
	if !bindJSON(c, &req) {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		response.Fail(c, response.CodeInvalidParam, "reason 不能为空（暂停需记录原因）")
		return
	}

	message, err := h.ops.Suspend(c.Request.Context(), instance, instanceops.AdminActor(admin.ID), reason)
	if err != nil {
		h.failInstanceOp(c, instance.ID, err)
		return
	}
	h.logger.Info("管理员暂停实例", "instance_id", instance.ID, "host_id", instance.HostID,
		"admin_id", admin.ID, "reason", reason)
	response.Success(c, operationResultView{
		InstanceID: instance.ID, Action: model.ActionSuspend, Message: message, Status: model.InstanceStatusSuspended,
	})
}

// adminUnsuspendInstance 处理 POST /api/v1/admin/instances/:id/unsuspend（仅 admin 角色）。
func (h *instanceHandler) adminUnsuspendInstance(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	instance, ok := h.adminInstance(c)
	if !ok {
		return
	}

	message, err := h.ops.Unsuspend(c.Request.Context(), instance, instanceops.AdminActor(admin.ID))
	if err != nil {
		h.failInstanceOp(c, instance.ID, err)
		return
	}
	h.logger.Info("管理员恢复实例", "instance_id", instance.ID, "host_id", instance.HostID, "admin_id", admin.ID)
	response.Success(c, operationResultView{
		InstanceID: instance.ID, Action: model.ActionUnsuspend, Message: message, Status: model.InstanceStatusActive,
	})
}

// adminCancelInstance 处理 POST /api/v1/admin/instances/:id/cancel：管理员代客/强制提交终止申请
// （仅 admin 角色；reason 必填）。允许状态与幂等口径同会员端，审计 actor=admin（契约 15.8.2）。
func (h *instanceHandler) adminCancelInstance(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	instance, ok := h.adminInstance(c)
	if !ok {
		return
	}

	var req cancelRequest
	if !bindJSON(c, &req) {
		return
	}
	cancelType := strings.TrimSpace(req.Type)
	if !model.IsValidCancelType(cancelType) {
		response.Fail(c, response.CodeInvalidParam, msgCancelTypeInvalid)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		response.Fail(c, response.CodeInvalidParam, "reason 不能为空（管理端终止需记录原因）")
		return
	}

	result, err := h.ops.Cancel(c.Request.Context(), instance,
		instanceops.AdminActor(admin.ID), cancelType, reason)
	if err != nil {
		h.failInstanceOp(c, instance.ID, err)
		return
	}
	h.logger.Info("管理员提交实例取消申请", "instance_id", instance.ID, "host_id", instance.HostID,
		"admin_id", admin.ID, "cancel_type", cancelType, "duplicate", result.Duplicate,
		"cancel_request_id", result.CancelRequestID)
	response.Success(c, newCancelResultView(result))
}

// adminSyncInstance 处理 POST /api/v1/admin/instances/:id/sync：
// 回读上游 hostinfo 回写同步字段（到期时间/状态/IP/端口/账号密码），并查询电源状态；
// 按上游 domainstatus 收敛本地状态（Suspended ↔ Active）。所有角色可调用（契约 15.2）。
func (h *instanceHandler) adminSyncInstance(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	instance, ok := h.adminInstance(c)
	if !ok {
		return
	}

	result, err := h.ops.Sync(c.Request.Context(), instance, instanceops.AdminActor(admin.ID))
	if err != nil {
		h.failInstanceOp(c, instance.ID, err)
		return
	}
	h.logger.Info("管理员同步实例", "instance_id", instance.ID, "host_id", instance.HostID,
		"admin_id", admin.ID, "status_changed", result.StatusChanged, "power_state", result.PowerState)
	response.Success(c, syncResultView{
		operationResultView: operationResultView{
			InstanceID: instance.ID, Action: model.ActionSync, Message: result.Message, Status: result.Instance.Status,
		},
		PowerState:    result.PowerState,
		PowerDesc:     result.PowerDesc,
		StatusChanged: result.StatusChanged,
		Terminated:    result.Terminated,
		Instance:      newInstanceSummaryView(result.Instance),
		NextDueDate:   formatTimePtr(result.Instance.NextDueDate),
		UpstreamState: result.Instance.UpstreamStatus,
	})
}

// listAdminInstanceLogs 处理 GET /api/v1/admin/instances/:id/logs：管理端操作记录（所有角色）。
func (h *instanceHandler) listAdminInstanceLogs(c *gin.Context) {
	instance, ok := h.adminInstance(c)
	if !ok {
		return
	}
	h.respondInstanceLogs(c, instance.ID)
}

// memberInstance 取当前会员并解析**本人**实例（他人/不存在统一 404）。
func (h *instanceHandler) memberInstance(c *gin.Context) (*model.Member, *model.Instance, bool) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return nil, nil, false
	}
	id, ok := instanceIDParam(c)
	if !ok {
		return nil, nil, false
	}

	instance, err := h.store.InstanceByIDForMember(c.Request.Context(), id, member.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgInstanceMissing)
		return nil, nil, false
	case err != nil:
		failDB(c, h.logger, err)
		return nil, nil, false
	}
	return member, instance, true
}

// adminInstance 按路径 ID 查询实例（管理端，不限会员；不存在 404）。
func (h *instanceHandler) adminInstance(c *gin.Context) (*model.Instance, bool) {
	id, ok := instanceIDParam(c)
	if !ok {
		return nil, false
	}
	instance, err := h.store.InstanceByID(c.Request.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgInstanceMissing)
		return nil, false
	case err != nil:
		failDB(c, h.logger, err)
		return nil, false
	}
	return instance, true
}

// respondInstanceLogs 输出实例操作记录分页（会员端与管理端共用；鉴权已由调用方完成）。
func (h *instanceHandler) respondInstanceLogs(c *gin.Context, instanceID uint64) {
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

	items, total, err := h.store.ListInstanceLogs(c.Request.Context(), store.InstanceLogFilter{
		InstanceID: instanceID,
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]instanceLogView, 0, len(items))
	for i := range items {
		views = append(views, instanceLogView{
			ID:         items[i].ID,
			InstanceID: items[i].InstanceID,
			ActorType:  items[i].ActorType,
			ActorID:    items[i].ActorID,
			Action:     items[i].Action,
			Status:     items[i].Status,
			Message:    items[i].Message,
			CreatedAt:  formatTime(items[i].CreatedAt),
		})
	}
	response.Success(c, instanceLogListView{Items: views, Page: page, PageSize: pageSize, Total: total})
}

// msgCancelTypeInvalid 是取消方式非法的统一提示（会员端与管理端一致）。
const msgCancelTypeInvalid = "type 只能是 immediate（立即取消）或 end_of_billing（到期取消，等到账单周期结束）"

// newCancelResultView 组装取消申请接口的返回（会员端与管理端一致；message 已脱敏）。
func newCancelResultView(result *instanceops.CancelResult) cancelResultView {
	instance := result.Instance
	return cancelResultView{
		operationResultView: operationResultView{
			InstanceID: instance.ID, Action: model.ActionCancel,
			Message: result.Message, Status: instance.Status,
		},
		CancelRequestID:   instance.CancelRequestID,
		CancelType:        instance.CancelType,
		CancelStatus:      instance.CancelStatus,
		CancelRequestedAt: formatTimePtr(instance.CancelRequestedAt),
		Duplicate:         result.Duplicate,
	}
}

// failInstanceOp 把实例操作错误映射为对外错误码（契约 15.6）：
// 状态不允许 / 上游未配置 / 参数口径 → 40002；上游调用失败 → 50003；其余 → 50001。
func (h *instanceHandler) failInstanceOp(c *gin.Context, instanceID uint64, err error) {
	switch {
	case errors.Is(err, instanceops.ErrNotOperable),
		errors.Is(err, instanceops.ErrUpstreamNotConfigured),
		errors.Is(err, instanceops.ErrWeakPassword),
		errors.Is(err, instanceops.ErrNoOSOption):
		response.Fail(c, response.CodeValidationFailed, err.Error())
	case errors.Is(err, instanceops.ErrUpstreamFailed):
		h.logger.Warn("实例操作上游调用失败", "error", err, "instance_id", instanceID)
		response.Fail(c, response.CodeUpstreamFailed, err.Error())
	default:
		failDB(c, h.logger, err)
	}
}
