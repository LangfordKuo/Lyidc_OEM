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

// SuspendMaxReasonRunes 是暂停原因的最大长度（按字符）。
const SuspendMaxReasonRunes = 200

// Suspend 管理员暂停实例（仅 active）：调上游 Suspend 并把本地状态置 suspended。
//
// reason 必填（由 handler 校验非空），同时提交上游并写入审计（便于对账与追溯）。
// 上游受理成功但本地状态收敛失败（并发改动）时返回错误且审计记 fail（以本地状态为准）。
func (s *Service) Suspend(ctx context.Context, instance *model.Instance, actor Actor, reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		err := fmt.Errorf("%w：暂停原因不能为空", ErrNotOperable)
		s.auditFail(ctx, instance, actor, model.ActionSuspend, err)
		return "", err
	}
	if len([]rune(reason)) > SuspendMaxReasonRunes {
		err := fmt.Errorf("%w：暂停原因过长（最多 %d 个字符）", ErrNotOperable, SuspendMaxReasonRunes)
		s.auditFail(ctx, instance, actor, model.ActionSuspend, err)
		return "", err
	}
	if instance.Status != model.InstanceStatusActive {
		err := fmt.Errorf("%w：实例当前状态为 %s，仅 active 可暂停", ErrNotOperable, instance.Status)
		s.auditFail(ctx, instance, actor, model.ActionSuspend, err)
		return "", err
	}

	err := s.upstreamCall(ctx, instance, actor, model.ActionSuspend, func(client *upstream.Client) error {
		_, callErr := client.Suspend(ctx, instance.HostID, reason)
		return callErr
	})
	if err != nil {
		return "", err
	}

	if err := s.store.UpdateInstanceStatus(ctx, instance.ID, model.InstanceStatusActive, model.InstanceStatusSuspended); err != nil {
		err = fmt.Errorf("上游已暂停但本地状态更新失败：%w", err)
		s.auditFail(ctx, instance, actor, model.ActionSuspend, err)
		return "", err
	}

	message := "暂停成功，原因：" + reason
	s.audit(ctx, instance.ID, actor, model.ActionSuspend, model.InstanceOpSuccess, message)
	return message, nil
}

// Unsuspend 管理员恢复实例（仅 suspended）：调上游 Unsuspend 并把本地状态置 active。
func (s *Service) Unsuspend(ctx context.Context, instance *model.Instance, actor Actor) (string, error) {
	if instance.Status != model.InstanceStatusSuspended {
		err := fmt.Errorf("%w：实例当前状态为 %s，仅 suspended 可恢复", ErrNotOperable, instance.Status)
		s.auditFail(ctx, instance, actor, model.ActionUnsuspend, err)
		return "", err
	}

	err := s.upstreamCall(ctx, instance, actor, model.ActionUnsuspend, func(client *upstream.Client) error {
		_, callErr := client.Unsuspend(ctx, instance.HostID)
		return callErr
	})
	if err != nil {
		return "", err
	}

	if err := s.store.UpdateInstanceStatus(ctx, instance.ID, model.InstanceStatusSuspended, model.InstanceStatusActive); err != nil {
		err = fmt.Errorf("上游已恢复但本地状态更新失败：%w", err)
		s.auditFail(ctx, instance, actor, model.ActionUnsuspend, err)
		return "", err
	}

	message := "恢复成功（已解除暂停）"
	s.audit(ctx, instance.ID, actor, model.ActionUnsuspend, model.InstanceOpSuccess, message)
	return message, nil
}

// SyncResult 是一次上游同步的结果摘要（管理端接口返回）。
type SyncResult struct {
	// Instance 是同步后的实例快照（含最新同步字段）。
	Instance *model.Instance
	// PowerState 是上游 power status 的 data.status（on / off / process；取不到为空串）。
	PowerState string
	// PowerDesc 是上游对电源状态的中文描述（data.des；取不到为空串）。
	PowerDesc string
	// StatusChanged 表示本地实例状态是否被本次同步收敛（active ↔ suspended，或收敛为 terminated）。
	StatusChanged bool
	// Terminated 表示本次同步判定上游主机已终止（不存在或 domainstatus ∈ {Deleted, Terminated}），
	// 本地已收敛为 terminated（此时不再回写同步字段、不查电源状态；契约 15.8.3）。
	Terminated bool
	// Message 是同步摘要（已脱敏，写入审计）。
	Message string
}

// Sync 管理端同步：回读上游主机信息（hostinfo）并回写实例同步字段
// （next_due_date / upstream_status / IP / 端口 / 账号密码），附带查询电源状态；
// 并按上游 domainstatus 收敛本地状态（Suspended ↔ Active，仅在这两个状态之间）。
//
// 任意实例状态均可调用（含 suspended）。**上游主机已不存在或 domainstatus ∈ {Deleted, Terminated}
// 时走终止收敛**：本地 status=terminated + cancel_status=done（幂等），写 cancel_sync 审计，
// 本次不再回写同步字段与电源状态（契约 15.8.3）。
func (s *Service) Sync(ctx context.Context, instance *model.Instance, actor Actor) (*SyncResult, error) {
	client, err := s.client(ctx)
	if err != nil {
		s.auditFail(ctx, instance, actor, model.ActionSync, err)
		return nil, err
	}

	host, hostErr := client.Host(ctx, instance.HostID)
	if hostErr != nil && !errors.Is(hostErr, upstream.ErrHostNotFound) {
		wrapped := fmt.Errorf("%w：回读上游主机失败：%v", ErrUpstreamFailed, hostErr)
		s.auditFail(ctx, instance, actor, model.ActionSync, wrapped)
		return nil, wrapped
	}

	// 终止收敛（上游主机已消失 → terminated）；正常主机返回 Terminated=false 继续字段同步。
	termination, termErr := s.converge(ctx, instance, actor, host, hostErr)
	if termErr != nil {
		s.auditFail(ctx, instance, actor, model.ActionSync, termErr)
		return nil, termErr
	}
	if termination.Terminated {
		message := "同步完成：" + termination.Message
		s.audit(ctx, instance.ID, actor, model.ActionSync, model.InstanceOpSuccess, message)
		updated, err := s.store.InstanceByID(ctx, instance.ID)
		if err != nil {
			updated = instance
		}
		return &SyncResult{
			Instance:      updated,
			StatusChanged: termination.StatusChanged,
			Terminated:    true,
			Message:       message,
		}, nil
	}

	sync := store.InstanceSyncInput{
		NextDueDate:    instance.NextDueDate,
		UpstreamStatus: host.DomainStatus,
		DedicatedIP:    host.DedicatedIP,
		AssignedIPs:    strings.Join(nonEmptyStrings(host.AssignedIPs), ","),
		Port:           host.Port,
		Username:       instance.Username,
		Password:       instance.Password,
	}
	if due := dueDateOf(host); due != nil {
		sync.NextDueDate = due
	}
	if host.Username != "" {
		sync.Username = host.Username
	}
	if host.Password != "" {
		sync.Password = host.Password
	}

	if err := s.store.UpdateInstanceSync(ctx, instance.ID, sync); err != nil {
		wrapped := fmt.Errorf("同步字段落库失败：%w", err)
		s.auditFail(ctx, instance, actor, model.ActionSync, wrapped)
		return nil, wrapped
	}

	// 状态收敛：仅 active ↔ suspended 之间（终止收敛已在上方返回；已 terminated 的实例不再被此分支改动）。
	statusChanged := false
	localStatus := instance.Status
	switch {
	case strings.EqualFold(host.DomainStatus, "Suspended") && localStatus == model.InstanceStatusActive:
		if err := s.store.UpdateInstanceStatus(ctx, instance.ID, model.InstanceStatusActive, model.InstanceStatusSuspended); err == nil {
			statusChanged = true
			localStatus = model.InstanceStatusSuspended
		} else if !errors.Is(err, store.ErrStateConflict) {
			s.logger.Warn("同步时收敛实例状态失败", "error", err, "instance_id", instance.ID)
		}
	case strings.EqualFold(host.DomainStatus, "Active") && localStatus == model.InstanceStatusSuspended:
		if err := s.store.UpdateInstanceStatus(ctx, instance.ID, model.InstanceStatusSuspended, model.InstanceStatusActive); err == nil {
			statusChanged = true
			localStatus = model.InstanceStatusActive
		} else if !errors.Is(err, store.ErrStateConflict) {
			s.logger.Warn("同步时收敛实例状态失败", "error", err, "instance_id", instance.ID)
		}
	}

	// 电源状态是附加信息：失败不影响同步成功（如实记录在 message 中）。
	powerState, powerDesc := "", ""
	if result, err := client.Status(ctx, instance.HostID); err != nil {
		s.logger.Warn("同步时查询电源状态失败（不影响字段同步）", "error", err, "instance_id", instance.ID)
	} else {
		powerState = result.DataField("status")
		powerDesc = result.DataField("des")
	}

	message := fmt.Sprintf("同步完成：上游状态 %s，到期时间 %s", host.DomainStatus, formatDue(sync.NextDueDate))
	if statusChanged {
		message += fmt.Sprintf("；本地状态已收敛为 %s", localStatus)
	}
	if powerState != "" {
		message += fmt.Sprintf("；电源状态 %s(%s)", powerState, powerDesc)
	}
	s.audit(ctx, instance.ID, actor, model.ActionSync, model.InstanceOpSuccess, message)

	updated, err := s.store.InstanceByID(ctx, instance.ID)
	if err != nil {
		updated = instance
	}
	return &SyncResult{
		Instance:      updated,
		PowerState:    powerState,
		PowerDesc:     powerDesc,
		StatusChanged: statusChanged,
		Message:       message,
	}, nil
}

// dueDateOf 解析上游主机到期时间（unix 秒；非正数返回 nil）。
func dueDateOf(host *upstream.Host) *time.Time {
	if host == nil || host.NextDueDate <= 0 {
		return nil
	}
	due := time.Unix(host.NextDueDate, 0).UTC()
	return &due
}

// formatDue 渲染到期时间（nil 输出 "-"）。
func formatDue(due *time.Time) string {
	if due == nil {
		return "-"
	}
	return due.UTC().Format(time.RFC3339)
}

// nonEmptyStrings 过滤空串并去空白（上游 assignedips 可能含空项）。
func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
