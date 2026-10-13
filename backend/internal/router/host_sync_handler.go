package router

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 上游状态回推接收端（R16，契约 22.3）：`POST /api/host/sync`。
//
// 这是本项目**唯一一个由上游（魔方财务）主动调用的接口**，因此有三条与站内接口不同的口径：
//
//  1. **路径在顶层**（`/api/host/sync`，不在 `/api/v1` 之下）：路径由上游固定拼接；
//  2. **请求是 form 表单**、**响应是上游约定的 `{"status":200|400,"msg":"…"}`**——
//     不套本站统一响应包（契约第 2 节第 5 条例外，外部对接格式由上游定）；
//  3. **鉴权只有签名**（`rand_str` + `signature` + 该实例的 `downstream_token`，见 host_sync_rule.go），
//     没有 token / 头鉴权；不区分「实例不存在」与「未登记」，避免泄露实例是否存在。
//
// 同步语义：只写 `instances`（字段 + 状态）与**一条**审计，**幂等**（无变化不写库不写审计），
// **静默**（不通知会员、不发站内信、不发邮件），**密码只落库不进日志/审计**（契约 22.3）。
//
// 失败一律 HTTP 200 + body `status=400`（与上游自身口径一致：成败看 body 的 status）。

// 回推接口的固定文案与状态码。
const (
	// hostSyncOK 是成功应答的 body.status。
	hostSyncOK = 200
	// hostSyncFail 是失败应答的 body.status。
	hostSyncFail = 400

	// msgHostSyncOK 成功文案。
	msgHostSyncOK = "更新成功"
	// msgHostSyncIncomplete 缺少必要参数（signature / rand_str）。
	msgHostSyncIncomplete = "参数不完整"
	// msgHostSyncGeneric 实例不可用（id 非法 / 实例不存在 / 未登记 downstream_token）的统一文案：
	// 刻意不区分具体原因，不泄露实例是否存在（契约 22.3）。
	msgHostSyncGeneric = "该产品不能使用该接口"
	// msgHostSyncBadSignature 验签失败。
	msgHostSyncBadSignature = "签名验证失败"
	// msgHostSyncFailed 通过验签后的落库/内部失败。
	msgHostSyncFailed = "更新失败"
)

// hostSyncResponse 是回推接口的响应体（**上游约定，不套统一响应包**，契约 22.3）。
type hostSyncResponse struct {
	Status int    `json:"status"`
	Msg    string `json:"msg"`
}

// hostSyncHandler 处理上游状态回推。
type hostSyncHandler struct {
	store  *store.Store
	logger *slog.Logger
}

// reply 写出回推应答：HTTP 状态码恒为 200，成败看 body 的 status（契约 22.3）。
func (h *hostSyncHandler) reply(c *gin.Context, status int, msg string) {
	c.JSON(http.StatusOK, hostSyncResponse{Status: status, Msg: msg})
}

// fail 写出失败应答。
func (h *hostSyncHandler) fail(c *gin.Context, msg string) {
	h.reply(c, hostSyncFail, msg)
}

// sync 处理 `POST /api/host/sync`：解析 form → 取实例与 token → 验签 → 按映射同步字段与状态。
func (h *hostSyncHandler) sync(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		h.fail(c, msgHostSyncIncomplete)
		return
	}
	form := c.Request.PostForm

	// 1) 必要参数：rand_str 与 signature（id 单独走「实例不可用」的统一文案）。
	idText := form.Get("id")
	randStr := form.Get("rand_str")
	signature := strings.TrimSpace(form.Get("signature"))
	if signature == "" || strings.TrimSpace(randStr) == "" {
		h.fail(c, msgHostSyncIncomplete)
		return
	}

	// 2) 按 id 查实例并取该实例的回推鉴权串：不存在 / 未登记一律统一文案。
	instanceID, err := strconv.ParseUint(strings.TrimSpace(idText), 10, 64)
	if err != nil || instanceID == 0 {
		h.fail(c, msgHostSyncGeneric)
		return
	}
	ctx := c.Request.Context()
	instance, err := h.store.InstanceByID(ctx, instanceID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		h.fail(c, msgHostSyncGeneric)
		return
	case err != nil:
		h.logger.Error("上游状态回推：查询实例失败", "error", err, "instance_id", instanceID)
		h.fail(c, msgHostSyncFailed)
		return
	}
	if instance.DownstreamToken == "" {
		// 存量老实例（接入前开通，未登记下游地址）：不参与回推。
		h.fail(c, msgHostSyncGeneric)
		return
	}

	// 3) 验签：id 用**收到的原始字符串**参与签名（与上游 PHP 的字符串口径一致）。
	expected := hostSyncSignature(idText, randStr, instance.DownstreamToken)
	if !strings.EqualFold(expected, signature) {
		h.logger.Warn("上游状态回推：签名验证失败", "instance_id", instanceID)
		h.fail(c, msgHostSyncBadSignature)
		return
	}

	// 4) 落库：只写发生变化的列；无变化是完全正常的幂等结果（不写库、不写审计）。
	in, targetStatus := parseHostSyncInput(form)
	outcome, err := h.store.ApplyHostSync(ctx, instance.ID, in, targetStatus)
	if err != nil {
		h.logger.Error("上游状态回推：落库失败", "error", err, "instance_id", instanceID)
		h.fail(c, msgHostSyncFailed)
		return
	}
	if outcome.Changed {
		h.audit(ctx, instance.ID, outcome)
		h.logger.Info("上游状态回推已同步", "instance_id", instance.ID,
			"fields", outcome.Fields, "status", outcome.Status)
	}
	h.reply(c, hostSyncOK, msgHostSyncOK)
}

// parseHostSyncInput 把回推表单解析为落库输入与目标状态（契约 22.3）。
//
// 字段口径：**只有出现在表单里的字段才写**（未出现的字段不改动，空值即清空）；
// `nextduedate` 额外要求「非空」（上游未回读到时会给空值，此时保留本地值）。
// 密码字段按原文进 `in.Password`，**绝不进日志与审计描述**。
func parseHostSyncInput(form url.Values) (store.HostSyncInput, string) {
	var in store.HostSyncInput

	domainStatus, hasDomainStatus := formField(form, "domainstatus")
	if hasDomainStatus {
		in.UpstreamStatus = &domainStatus
	}
	if value, ok := formField(form, "nextduedate"); ok && strings.TrimSpace(value) != "" {
		// 上游以 unix 秒给出到期时间（契约 8.5 第 11 条）；非正数/非法值视为未提供。
		if stamp, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && stamp > 0 {
			due := time.Unix(stamp, 0).UTC()
			in.NextDueDate = &due
		}
	}
	if value, ok := formField(form, "dedicatedip"); ok {
		in.DedicatedIP = &value
	}
	if value, ok := formField(form, "assignedips"); ok {
		// 上游以逗号分隔原文给出；库内同样是逗号分隔原文（列表接口才展开为数组）。
		in.AssignedIPs = &value
	}
	if value, ok := formField(form, "port"); ok {
		if port, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && port >= 0 {
			in.Port = &port
		}
	}
	if value, ok := formField(form, "username"); ok {
		in.Username = &value
	}
	if value, ok := formField(form, "password"); ok {
		in.Password = &value
	}

	targetStatus := ""
	if hasDomainStatus {
		if mapped, ok := hostSyncStatus(domainStatus); ok {
			targetStatus = mapped
		}
	}
	return in, targetStatus
}

// formField 取表单字段，**区分「没有这个键」与「键存在但为空」**（url.Values 保留该区分，
// 前者不改动本地值、后者按空值写入）。
func formField(form url.Values, key string) (string, bool) {
	values, ok := form[key]
	if !ok || len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// audit 写一条「上游推送同步」审计（契约 22.3）：actor_type=system、actor_id=0、action=host_sync。
//
// message **只列发生变化的栏目名**（如「上游状态、到期时间」），不含任何字段值——
// 密码绝不进入审计描述。审计写入不参与业务事务：写失败只记日志，不影响本次同步结果。
func (h *hostSyncHandler) audit(ctx context.Context, instanceID uint64, outcome *store.HostSyncOutcome) {
	message := "上游推送同步：更新 " + strings.Join(outcome.Fields, "、")
	if outcome.Status != "" {
		message += "（当前状态 " + outcome.Status + "）"
	}
	if err := h.store.AppendInstanceLog(ctx, store.InstanceLogInput{
		InstanceID: instanceID,
		ActorType:  model.ActorTypeSystem,
		ActorID:    0,
		Action:     model.ActionHostSync,
		Status:     model.InstanceOpSuccess,
		Message:    message,
	}); err != nil {
		h.logger.Warn("上游推送同步审计写入失败", "error", err, "instance_id", instanceID)
	}
}
