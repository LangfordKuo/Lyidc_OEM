// Package scheduler 提供应用内轻量后台任务（阶段 5b：到期暂停扫描；阶段 5c：终止收敛）。
//
// 到期暂停扫描（契约 15.5）：服务启动后延迟 InitialDelay 执行首次扫描，之后每 Interval
// 扫描一次；对「status=active 且 next_due_date < now」的实例调上游 Suspend、
// 本地置 suspended，并写审计（actor=system）。**有在途取消申请（cancel_status=pending）
// 的实例排除在外**（它们已进入终止流程，见契约 15.8.5）。
//
// 终止收敛（契约 15.8.5，阶段 5c）：同一轮扫描中对「取消申请在途且已到收敛时机」的实例
// 回读上游主机（immediate 提交后即回读；end_of_billing 到期后才回读），
// 上游已删除/已终止 → 本地收敛 terminated + 审计（actor=system，action=cancel_sync）。
//
// 幂等与失败重试：
//   - 上游 Suspend 业务失败时回读一次主机：若上游已是 Suspended，则只收敛本地状态
//     （幂等路径，审计记 success 并标注）；否则记 fail，**下一轮扫描自动重试**；
//   - 本地状态收敛用条件更新（active → suspended）：并发下只有一个生效；
//   - 终止收敛同样幂等（已是 terminated 不重复写），收敛失败（回读报错等）**只记服务日志**，
//     下一轮自动重试（不写 fail 审计，避免每日刷屏；契约 15.8.5）。
//
// 已知边界（契约 15.5 / 15.8.5）：上游自行暂停或自行删除主机（非本系统触发）不在扫描范围内，
// 由管理端同步接口收敛；扫描间隔窗口内到期/收敛的实例最迟在下一轮被处理；
// 本批不做到期提醒通知（阶段 6）。
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/instanceops"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// 扫描调度缺省参数（契约 15.5 的常量口径；测试可经 Options 注入覆盖）。
const (
	// DefaultInterval 是两次扫描之间的间隔。
	DefaultInterval = 24 * time.Hour
	// DefaultInitialDelay 是服务启动后首次扫描的延迟（避免启动风暴，给上游/DB 留缓冲）。
	DefaultInitialDelay = 1 * time.Minute
	// DefaultBatchSize 是单轮扫描每批处理的实例数。
	DefaultBatchSize = 50
	// scanTimeout 是单轮扫描的整体超时。
	scanTimeout = 5 * time.Minute
	// suspendReason 是自动暂停提交给上游的原因文案。
	suspendReason = "到期未续费，系统自动暂停"
)

// Options 是扫描器的构造参数（零值即生产默认）。
type Options struct {
	// Interval 扫描间隔；<=0 时取 DefaultInterval。
	Interval time.Duration
	// InitialDelay 启动后首次扫描延迟；零值表示不延迟（测试友好），
	// 生产装配（router）显式传入 DefaultInitialDelay。
	InitialDelay time.Duration
	// BatchSize 单批处理条数；<=0 时取 DefaultBatchSize。
	BatchSize int
	// Clock 可注入时钟（测试用）；nil 时用 time.Now。
	Clock func() time.Time
	// Logger 为 nil 时使用 slog.Default()。
	Logger *slog.Logger
}

// ScanReport 是单轮扫描的统计。
type ScanReport struct {
	// Scanned 本轮处理的到期实例数（含暂停成功与失败）。
	Scanned int
	// Suspended 本轮成功置为 suspended 的实例数。
	Suspended int
	// Failed 本轮处理失败（留待下轮重试）的实例数。
	Failed int
	// CancelScanned 本轮尝试收敛的「取消申请在途」实例数（阶段 5c）。
	CancelScanned int
	// CancelConverged 本轮成功收敛为 terminated 的实例数（阶段 5c）。
	CancelConverged int
	// CancelFailed 本轮收敛失败（回读报错等，留待下轮重试）的实例数（阶段 5c）。
	CancelFailed int
}

// Scanner 是到期暂停扫描器（Start/Stop 控制后台循环；ScanOnce 供测试与手动触发）。
type Scanner struct {
	store    *store.Store
	upstream upstream.Provider
	ops      *instanceops.Service
	logger   *slog.Logger

	interval     time.Duration
	initialDelay time.Duration
	batchSize    int
	clock        func() time.Time

	stop     chan struct{}
	stopOnce sync.Once
}

// New 构造扫描器。
func New(st *store.Store, provider upstream.Provider, opts Options) *Scanner {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	initialDelay := opts.InitialDelay
	if initialDelay < 0 {
		initialDelay = 0
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Scanner{
		store:        st,
		upstream:     provider,
		ops:          instanceops.New(st, provider, instanceops.Options{Logger: logger}),
		logger:       logger,
		interval:     interval,
		initialDelay: initialDelay,
		batchSize:    batchSize,
		clock:        clock,
		stop:         make(chan struct{}),
	}
}

// Start 启动后台循环（重复调用无效果；Stop 后不可再 Start）。
func (s *Scanner) Start() {
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("到期暂停扫描发生 panic", "panic", recovered)
			}
		}()

		if s.initialDelay > 0 {
			timer := time.NewTimer(s.initialDelay)
			defer timer.Stop()
			select {
			case <-s.stop:
				return
			case <-timer.C:
			}
		}

		s.runOnce()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				s.runOnce()
			}
		}
	}()
}

// Stop 停止后台循环（幂等）。
func (s *Scanner) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
}

// runOnce 执行一轮扫描并记录日志（超时保护）。
func (s *Scanner) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()

	report, err := s.ScanOnce(ctx)
	if err != nil {
		s.logger.Error("到期暂停扫描执行失败", "error", err)
		return
	}
	if report.Scanned == 0 && report.CancelScanned == 0 {
		s.logger.Info("到期暂停扫描完成：无到期实例、无待收敛的取消申请")
		return
	}
	s.logger.Info("到期暂停扫描完成", "scanned", report.Scanned,
		"suspended", report.Suspended, "failed", report.Failed,
		"cancel_scanned", report.CancelScanned, "cancel_converged", report.CancelConverged,
		"cancel_failed", report.CancelFailed)
}

// ScanOnce 执行一轮完整扫描（可重复调用；测试注入 Clock + 手动调用）。
//
// 返回的 error 只表示「扫描基础设施不可用」（数据库错误 / 上游未配置），
// 单个实例的处理失败计入 report.Failed 并不中断本轮（下轮自动重试）。
func (s *Scanner) ScanOnce(ctx context.Context) (ScanReport, error) {
	var report ScanReport

	client, enabled, err := s.upstream.Current(ctx)
	if err != nil {
		if errors.Is(err, upstream.ErrNotConfigured) {
			return report, fmt.Errorf("上游未配置（跳过本轮到期扫描）")
		}
		return report, fmt.Errorf("读取上游设置失败：%w", err)
	}
	if !enabled {
		return report, fmt.Errorf("上游未配置或未启用（跳过本轮到期扫描）")
	}

	now := s.clock().UTC()

	// 阶段一：到期暂停（active 且已过到期时间，排除有在途取消申请的实例）。
	if err := s.suspendDueInstances(ctx, client, now, &report); err != nil {
		return report, err
	}
	// 阶段二：取消申请收敛（回读上游，已删除/已终止 → 本地 terminated）。
	if err := s.convergeCancels(ctx, client, now, &report); err != nil {
		return report, err
	}
	return report, nil
}

// suspendDueInstances 循环处理到期实例直到取空（processed 去重避免状态未变时死循环）。
func (s *Scanner) suspendDueInstances(ctx context.Context, client *upstream.Client, now time.Time, report *ScanReport) error {
	processed := make(map[uint64]struct{})
	for {
		due, err := s.store.ListDueInstances(ctx, now, s.batchSize)
		if err != nil {
			return fmt.Errorf("查询到期实例失败：%w", err)
		}

		batch := make([]model.Instance, 0, len(due))
		for i := range due {
			if _, seen := processed[due[i].ID]; seen {
				continue
			}
			processed[due[i].ID] = struct{}{}
			batch = append(batch, due[i])
		}
		if len(batch) == 0 {
			return nil
		}

		for i := range batch {
			instance := &batch[i]
			report.Scanned++
			if err := s.suspendDue(ctx, client, instance); err != nil {
				report.Failed++
				s.logger.Warn("到期实例自动暂停失败（下一轮重试）",
					"error", err, "instance_id", instance.ID, "host_id", instance.HostID)
				continue
			}
			report.Suspended++
		}
	}
}

// convergeCancels 循环收敛「取消申请在途」的实例直到取空（契约 15.8.5）：
// 回读上游主机，上游已删除或已终止 → 本地 status=terminated + cancel_status=done + 审计（actor=system）；
// 上游仍在运行（申请尚未被上游处理）→ 本轮跳过，下一轮再看；
// 回读报错 → 记服务日志并计入 CancelFailed（不写 fail 审计），下一轮重试。
func (s *Scanner) convergeCancels(ctx context.Context, client *upstream.Client, now time.Time, report *ScanReport) error {
	processed := make(map[uint64]struct{})
	for {
		pending, err := s.store.ListPendingCancelInstances(ctx, now, s.batchSize)
		if err != nil {
			return fmt.Errorf("查询待收敛的取消申请失败：%w", err)
		}

		batch := make([]model.Instance, 0, len(pending))
		for i := range pending {
			if _, seen := processed[pending[i].ID]; seen {
				continue
			}
			processed[pending[i].ID] = struct{}{}
			batch = append(batch, pending[i])
		}
		if len(batch) == 0 {
			return nil
		}

		for i := range batch {
			instance := &batch[i]
			report.CancelScanned++
			result, err := s.ops.ConvergeTermination(ctx, instance, instanceops.SystemActor())
			if err != nil {
				report.CancelFailed++
				s.logger.Warn("取消申请收敛失败（下一轮重试）",
					"error", err, "instance_id", instance.ID, "host_id", instance.HostID)
				continue
			}
			if result.Terminated {
				report.CancelConverged++
				s.logger.Info("取消申请已收敛为 terminated",
					"instance_id", instance.ID, "host_id", instance.HostID,
					"status_changed", result.StatusChanged)
			}
		}
	}
}

// suspendDue 暂停单个到期实例：调上游 Suspend → 本地置 suspended → 审计。
//
// 上游业务失败时回读主机做幂等判定：上游已是 Suspended 则只收敛本地状态（视为成功）。
func (s *Scanner) suspendDue(ctx context.Context, client *upstream.Client, instance *model.Instance) error {
	if _, err := client.Suspend(ctx, instance.HostID, suspendReason); err != nil {
		// 幂等路径：上游可能已自行暂停（或本系统上轮已调成功但本地落库失败）。
		host, readErr := client.Host(ctx, instance.HostID)
		if readErr == nil && strings.EqualFold(strings.TrimSpace(host.DomainStatus), "Suspended") {
			if updateErr := s.store.UpdateInstanceStatus(ctx, instance.ID,
				model.InstanceStatusActive, model.InstanceStatusSuspended); updateErr != nil {
				s.audit(ctx, instance.ID, model.InstanceOpFail,
					fmt.Sprintf("上游已暂停但本地状态更新失败：%v", updateErr))
				return updateErr
			}
			s.audit(ctx, instance.ID, model.InstanceOpSuccess,
				"到期自动暂停：上游已是暂停状态（幂等），本地状态已同步为 suspended")
			return nil
		}
		s.audit(ctx, instance.ID, model.InstanceOpFail, fmt.Sprintf("到期自动暂停失败：%v", err))
		return err
	}

	if err := s.store.UpdateInstanceStatus(ctx, instance.ID,
		model.InstanceStatusActive, model.InstanceStatusSuspended); err != nil {
		// 上游已暂停成功但本地未落库：记 fail（本地状态仍是 active，下轮会重走幂等路径）。
		wrapped := fmt.Errorf("上游已暂停但本地状态更新失败：%w", err)
		s.audit(ctx, instance.ID, model.InstanceOpFail, wrapped.Error())
		return wrapped
	}

	s.audit(ctx, instance.ID, model.InstanceOpSuccess,
		fmt.Sprintf("到期自动暂停成功（到期时间 %s）", formatDue(instance.NextDueDate)))
	return nil
}

// audit 写一条系统操作审计（actor=system）。写失败只记日志，不影响扫描结果。
func (s *Scanner) audit(ctx context.Context, instanceID uint64, status, message string) {
	if err := s.store.AppendInstanceLog(ctx, store.InstanceLogInput{
		InstanceID: instanceID,
		ActorType:  model.ActorTypeSystem,
		Action:     model.ActionSuspend,
		Status:     status,
		Message:    message,
	}); err != nil {
		s.logger.Warn("到期扫描审计写入失败", "error", err, "instance_id", instanceID)
	}
}

// formatDue 渲染到期时间（nil 输出 "-"）。
func formatDue(due *time.Time) string {
	if due == nil {
		return "-"
	}
	return due.UTC().Format(time.RFC3339)
}
