package instanceops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// 取消/终止流程（契约 15.8）：
//
//	会员申请取消（POST /instances/:id/cancel）
//	  → 上游 POST /host/cancel（Immediate / Endofbilling，异步受理：status=202 表示待上游处理）
//	  → 本地记 cancel_status=pending（status 保持原值）
//	  → 上游处理完毕（主机被删除）→ 管理端同步或到期扫描回读 → 本地收敛 terminated。
//
// 上游没有「直接删除主机」的开放接口，删除走申请流程（可能需上游人工审核），
// 因此本包只保证「申请已被上游受理」，不保证立即终止（见 upstream.RequestCancel 注释）。

// CancelMaxReasonRunes 是取消申请原因的最大长度（按字符，与暂停原因同口径）。
const CancelMaxReasonRunes = 200

// 取消申请的默认原因（会员未填写时兜底；管理端与系统路径的原因由调用方给定）。
const defaultMemberCancelReason = "会员申请终止（未填写原因）"

// upstreamStatusCancelPending 是上游 /host/cancel 的「受理但待处理」业务状态码
// （实测 202 + data.pending=true，见契约 15.7 第 5 条）。
const upstreamStatusCancelPending = 202

// CancelResult 是一次取消申请的结果。
type CancelResult struct {
	// Instance 是申请后的实例快照（含最新取消标记）。
	Instance *model.Instance
	// Message 是结果说明（已脱敏，写入审计）。
	Message string
	// Duplicate 表示本次为幂等重复申请（已有在途申请，未重复提交上游）。
	Duplicate bool
	// UpstreamStatus 是上游返回的业务状态码（幂等分支为 0）。
	UpstreamStatus int
	// CancelRequestID 是上游回传的取消申请 ID（取不到为 0）。
	CancelRequestID int
}

// Cancel 提交取消（终止）申请：仅 active / suspended 可申请；已有在途申请时**幂等返回现状**，
// 不重复提交上游（契约 15.8.1）。
//
// 成功后本地记 cancel_status=pending（status 保持原值，收敛由 Sync / 到期扫描负责），
// 并写审计（actor 区分会员本人 / 管理员；幂等重复申请同样留痕）。
func (s *Service) Cancel(ctx context.Context, instance *model.Instance, actor Actor, cancelType, reason string) (*CancelResult, error) {
	cancelType = strings.TrimSpace(cancelType)
	if !model.IsValidCancelType(cancelType) {
		err := fmt.Errorf("%w：取消方式只能是 %s 或 %s", ErrNotOperable,
			model.CancelTypeImmediate, model.CancelTypeEndOfBilling)
		s.auditFail(ctx, instance, actor, model.ActionCancel, err)
		return nil, err
	}
	upstreamType := upstreamCancelType(cancelType)

	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = defaultMemberCancelReason
	}
	if len([]rune(reason)) > CancelMaxReasonRunes {
		err := fmt.Errorf("%w：取消原因过长（最多 %d 个字符）", ErrNotOperable, CancelMaxReasonRunes)
		s.auditFail(ctx, instance, actor, model.ActionCancel, err)
		return nil, err
	}

	// 幂等分支：已有在途申请 → 返回现状，不重复提交上游。
	if instance.CancelStatus == model.InstanceCancelPending {
		message := fmt.Sprintf("已有在途取消申请（%s，提交时间 %s），未重复提交上游（幂等）",
			describeCancelRequest(instance), formatCancelTime(instance.CancelRequestedAt))
		s.audit(ctx, instance.ID, actor, model.ActionCancel, model.InstanceOpSuccess, message)
		return &CancelResult{Instance: instance, Message: message, Duplicate: true,
			CancelRequestID: instance.CancelRequestID}, nil
	}

	if !model.IsInstanceCancellable(instance.Status) {
		err := fmt.Errorf("%w：实例当前状态为 %s，仅 active / suspended 可申请终止",
			ErrNotOperable, instance.Status)
		s.auditFail(ctx, instance, actor, model.ActionCancel, err)
		return nil, err
	}

	var upstreamResult *upstream.ProvisionResult
	err := s.upstreamCall(ctx, instance, actor, model.ActionCancel, func(client *upstream.Client) error {
		result, callErr := client.RequestCancel(ctx, instance.HostID, upstreamType, reason)
		upstreamResult = result
		return callErr
	})
	if err != nil {
		return nil, err
	}

	// 条件更新（cancel_status → pending）保证并发下只有一个申请在途。
	storeErr := s.store.UpdateInstanceCancelRequest(ctx, instance.ID, store.InstanceCancelInput{
		RequestID: upstreamResult.CancelRequestID,
		Type:      cancelType,
		Reason:    reason,
	})
	switch {
	case errors.Is(storeErr, store.ErrStateConflict):
		latest := s.reloadInstance(ctx, instance)
		message := fmt.Sprintf("已有在途取消申请（%s，并发重复提交），本次未重复记录（幂等）",
			describeCancelRequest(latest))
		s.audit(ctx, instance.ID, actor, model.ActionCancel, model.InstanceOpSuccess, message)
		return &CancelResult{Instance: latest, Message: message, Duplicate: true,
			CancelRequestID: latest.CancelRequestID}, nil
	case storeErr != nil:
		wrapped := fmt.Errorf("上游已受理取消申请但本地记录失败：%w", storeErr)
		s.auditFail(ctx, instance, actor, model.ActionCancel, wrapped)
		return nil, wrapped
	}

	message := cancelSubmittedMessage(upstreamResult, reason)
	s.audit(ctx, instance.ID, actor, model.ActionCancel, model.InstanceOpSuccess, message)
	return &CancelResult{
		Instance:        s.reloadInstance(ctx, instance),
		Message:         message,
		UpstreamStatus:  upstreamResult.Status,
		CancelRequestID: upstreamResult.CancelRequestID,
	}, nil
}

// upstreamCancelType 把本地取消方式映射为上游 /host/cancel 的 type 口径
// （上游取值实测为 Immediate / Endofbilling，见 upstream.CancelImmediate / CancelEndOfBilling）。
func upstreamCancelType(cancelType string) string {
	if cancelType == model.CancelTypeEndOfBilling {
		return upstream.CancelEndOfBilling
	}
	return upstream.CancelImmediate
}

// cancelSubmittedMessage 组装申请受理的结果文案（已含原因）。三种上游口径（契约 15.8.3）：
//
//   - 202（+ data.pending=true）：终止申请在途，等待上游处理（实测首次申请口径）；
//   - 200 + data.domainstatus=Deleted：上游主机已删除（实测对已被上游终止的主机再次申请的口径，
//     此时申请即时「生效」，本地随即进入 pending，等待同步/扫描收敛为 terminated）；
//   - 其余：如实标注「已提交上游」。
func cancelSubmittedMessage(result *upstream.ProvisionResult, reason string) string {
	prefix := "取消申请已提交上游"
	switch {
	case result.Status == upstreamStatusCancelPending:
		prefix = "取消申请已受理，等待上游处理"
	case IsTerminatedDomainStatus(result.DataField("domainstatus")):
		prefix = "上游主机已删除，终止即时生效（等待同步收敛）"
	}
	if result.CancelRequestID > 0 {
		prefix += fmt.Sprintf("（申请号 %d）", result.CancelRequestID)
	}
	return fmt.Sprintf("%s；原因：%s", prefix, reason)
}

// describeCancelRequest 描述在途申请（用于幂等分支的审计与提示文案）。
func describeCancelRequest(instance *model.Instance) string {
	label := "立即取消"
	if instance.CancelType == model.CancelTypeEndOfBilling {
		label = "到期取消"
	}
	if instance.CancelRequestID > 0 {
		return fmt.Sprintf("%s，申请号 %d", label, instance.CancelRequestID)
	}
	return label
}

// formatCancelTime 渲染申请时间（nil 输出 "-"）。
func formatCancelTime(at *time.Time) string {
	if at == nil {
		return "-"
	}
	return at.UTC().Format(time.RFC3339)
}

// reloadInstance 回读实例最新快照（失败时退回传入的快照，保证结果可用）。
func (s *Service) reloadInstance(ctx context.Context, instance *model.Instance) *model.Instance {
	latest, err := s.store.InstanceByID(ctx, instance.ID)
	if err != nil {
		return instance
	}
	return latest
}

// 终止判定用到的上游 domainstatus（WHMCS 口径；大小写不敏感）。
const (
	domainStatusDeleted    = "Deleted"
	domainStatusTerminated = "Terminated"
)

// IsTerminatedDomainStatus 判断上游 domainstatus 是否表示主机已终止。
func IsTerminatedDomainStatus(status string) bool {
	switch {
	case strings.EqualFold(strings.TrimSpace(status), domainStatusDeleted):
		return true
	case strings.EqualFold(strings.TrimSpace(status), domainStatusTerminated):
		return true
	default:
		return false
	}
}

// TerminationResult 是一次终止收敛的判定结果（契约 15.8.3）。
type TerminationResult struct {
	// Host 是上游回读的主机（上游已不存在时为 nil）。
	Host *upstream.Host
	// Terminated 表示上游已终止（主机不存在或 domainstatus ∈ {Deleted, Terminated}）。
	Terminated bool
	// StatusChanged 表示本次调用把本地状态收敛为 terminated（本就 terminated 时为 false，幂等）。
	StatusChanged bool
	// Message 是收敛说明（Terminated 时非空，已脱敏）。
	Message string
}

// ConvergeTermination 回读上游主机并收敛终止状态（管理端同步入口；到期扫描走同一实现）。
//
// 判定与动作（契约 15.8.3）：上游主机不存在（ErrHostNotFound）或 domainstatus ∈
// {Deleted, Terminated} → 本地 status=terminated + cancel_status=done（幂等条件更新），
// 并写一条 cancel_sync/success 审计；上游仍 Active/Suspended → 不动作、不写审计（返回 Terminated=false）。
//
// 回读失败（网络/鉴权等）返回 ErrUpstreamFailed 包装错误（由调用方按 sync 失败留痕）。
func (s *Service) ConvergeTermination(ctx context.Context, instance *model.Instance, actor Actor) (*TerminationResult, error) {
	client, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	host, hostErr := client.Host(ctx, instance.HostID)
	return s.converge(ctx, instance, actor, host, hostErr)
}

// converge 是收敛判定的实现：host / hostErr 为**已完成的回读结果**（二者互斥），
// 供 Sync（需要主机快照做字段回写）与 ConvergeTermination（自行回读）复用。
func (s *Service) converge(ctx context.Context, instance *model.Instance, actor Actor,
	host *upstream.Host, hostErr error) (*TerminationResult, error) {
	result := &TerminationResult{Host: host}

	upstreamStatus := ""
	switch {
	case hostErr != nil && errors.Is(hostErr, upstream.ErrHostNotFound):
		result.Terminated = true
	case hostErr != nil:
		return nil, fmt.Errorf("%w：回读上游主机失败：%v", ErrUpstreamFailed, hostErr)
	case host != nil && IsTerminatedDomainStatus(host.DomainStatus):
		result.Terminated = true
		upstreamStatus = host.DomainStatus
	default:
		return result, nil
	}

	changed, err := s.store.MarkInstanceTerminated(ctx, instance.ID, upstreamStatus)
	if err != nil {
		wrapped := fmt.Errorf("上游已终止但本地状态收敛失败：%w", err)
		s.audit(ctx, instance.ID, actor, model.ActionCancelSync, model.InstanceOpFail, wrapped.Error())
		return result, wrapped
	}
	result.StatusChanged = changed

	reason := "上游主机已删除"
	if upstreamStatus != "" {
		reason = "上游状态 " + upstreamStatus
	}
	if changed {
		result.Message = fmt.Sprintf("上游已终止（%s），本地状态已收敛为 terminated", reason)
	} else {
		result.Message = fmt.Sprintf("上游已终止（%s），本地状态已是 terminated（幂等）", reason)
	}
	s.audit(ctx, instance.ID, actor, model.ActionCancelSync, model.InstanceOpSuccess, result.Message)
	return result, nil
}
