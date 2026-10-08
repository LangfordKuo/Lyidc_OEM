// Package scheduler 提供应用内轻量后台任务（阶段 5b：到期暂停扫描）。
//
// 到期暂停扫描（契约 15.5）：服务启动后延迟 InitialDelay 执行首次扫描，之后每 Interval
// 扫描一次；对「status=active 且 next_due_date < now」的实例调上游 Suspend、
// 本地置 suspended，并写审计（actor=system）。
//
// 幂等与失败重试：
//   - 上游 Suspend 业务失败时回读一次主机：若上游已是 Suspended，则只收敛本地状态
//     （幂等路径，审计记 success 并标注）；否则记 fail，**下一轮扫描自动重试**；
//   - 本地状态收敛用条件更新（active → suspended）：并发下只有一个生效。
//
// 已知边界（契约 15.5）：上游自行暂停（非本系统触发）不在扫描范围内，由管理端同步接口收敛；
// 扫描间隔窗口内到期的实例最迟在下一轮被处理；本批不做到期提醒通知（阶段 6）。
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

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
}

// Scanner 是到期暂停扫描器（Start/Stop 控制后台循环；ScanOnce 供测试与手动触发）。
type Scanner struct {
	store    *store.Store
	upstream upstream.Provider
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
	if report.Scanned == 0 {
		s.logger.Info("到期暂停扫描完成：无到期实例")
		return
	}
	s.logger.Info("到期暂停扫描完成", "scanned", report.Scanned,
		"suspended", report.Suspended, "failed", report.Failed)
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
	processed := make(map[uint64]struct{})

	for {
		due, err := s.store.ListDueInstances(ctx, now, s.batchSize)
		if err != nil {
			return report, fmt.Errorf("查询到期实例失败：%w", err)
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
			return report, nil
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
