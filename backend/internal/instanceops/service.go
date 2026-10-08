// Package instanceops 实现实例操作链路（阶段 5b）：会员端电源/重装/改密、管理端暂停/恢复/同步；
// 阶段 5c 追加：取消/终止申请（会员端与管理端）与上游确认终止后的状态收敛。
//
// 设计要点（契约见 docs/api-contract.md 第 15 节）：
//   - 上游调用只走 internal/upstream 既有封装（On/Off/Reboot/HardOff/HardReboot/Reinstall/
//     ResetPassword/Suspend/Unsuspend/Status/CloudOS/Host/RequestCancel），本包不做任何裸 HTTP 调用；
//   - **所有操作无论成功失败都写 instance_operation_logs**（actor 区分 member/admin/system），
//     审计写入不参与业务事务：写失败只记日志，不改变操作结果；
//   - 审计与对外错误**不含密码与密钥**：上游错误原样透传前先做敏感串替换（见 sanitize）；
//   - 状态矩阵（契约 15.2 / 15.8）：会员端操作仅 active 可执行；管理端 suspend 仅 active、
//     unsuspend 仅 suspended；取消申请 active / suspended 均可（有在途申请时幂等返回）；
//     sync 任意状态可执行（由上游回答，含上游已删除时的终止收敛）。
package instanceops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// 实例操作错误分类（handler 据此映射错误码）。
var (
	// ErrNotOperable 实例当前状态不允许该操作（未执行任何上游调用，已写审计 fail）。
	ErrNotOperable = errors.New("实例当前状态不允许该操作")
	// ErrUpstreamNotConfigured 上游未配置或未启用。
	ErrUpstreamNotConfigured = errors.New("上游未配置或未启用（请管理员在后台设置中填写并启用）")
	// ErrUpstreamFailed 上游调用失败（错误信息已脱敏，可安全透传给用户）。
	ErrUpstreamFailed = errors.New("上游调用失败")
	// ErrNoOSOption 该商品未配置操作系统选项（无法提供重装系统列表）。
	ErrNoOSOption = errors.New("该商品未配置操作系统可选项")
)

// Actor 是一次实例操作的操作者（审计用）。
type Actor struct {
	// Type 取 model.ActorTypeMember / ActorTypeAdmin；系统自动操作用 ActorTypeSystem。
	Type string
	// ID 为 member_id / admin_id；系统操作为 0。
	ID uint64
}

// MemberActor 构造会员操作者。
func MemberActor(memberID uint64) Actor {
	return Actor{Type: model.ActorTypeMember, ID: memberID}
}

// AdminActor 构造管理员操作者。
func AdminActor(adminID uint64) Actor {
	return Actor{Type: model.ActorTypeAdmin, ID: adminID}
}

// SystemActor 构造系统操作者（后台扫描/自动任务；审计 actor_id 恒为 0）。
func SystemActor() Actor {
	return Actor{Type: model.ActorTypeSystem}
}

// Options 是实例操作服务的构造参数。
type Options struct {
	// Logger 为 nil 时使用 slog.Default()。
	Logger *slog.Logger
}

// Service 是实例操作服务。
type Service struct {
	store    *store.Store
	upstream upstream.Provider
	logger   *slog.Logger
}

// New 构造实例操作服务。
func New(st *store.Store, provider upstream.Provider, opts Options) *Service {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: st, upstream: provider, logger: logger}
}

// Store 暴露底层 Store（router 用于查询实例与审计列表）。
func (s *Service) Store() *store.Store { return s.store }

// client 返回当前设置下的上游客户端；未配置/未启用时返回 ErrUpstreamNotConfigured。
func (s *Service) client(ctx context.Context) (*upstream.Client, error) {
	client, enabled, err := s.upstream.Current(ctx)
	if err != nil {
		if errors.Is(err, upstream.ErrNotConfigured) {
			return nil, ErrUpstreamNotConfigured
		}
		return nil, fmt.Errorf("读取上游设置失败：%v", err)
	}
	if !enabled {
		return nil, ErrUpstreamNotConfigured
	}
	return client, nil
}

// audit 写一条实例操作审计（message 先脱敏）。写失败只记日志，不影响业务结果。
func (s *Service) audit(ctx context.Context, instanceID uint64, actor Actor, action, status, message string, secrets ...string) {
	if err := s.store.AppendInstanceLog(ctx, store.InstanceLogInput{
		InstanceID: instanceID,
		ActorType:  actor.Type,
		ActorID:    actor.ID,
		Action:     action,
		Status:     status,
		Message:    sanitize(message, secrets...),
	}); err != nil {
		s.logger.Warn("实例操作审计写入失败", "error", err, "instance_id", instanceID, "action", action)
	}
}

// auditFail 记录失败审计（供各操作的失败分支统一调用）。
func (s *Service) auditFail(ctx context.Context, instance *model.Instance, actor Actor, action string, err error, secrets ...string) {
	s.audit(ctx, instance.ID, actor, action, model.InstanceOpFail, err.Error(), secrets...)
}

// requireOperable 校验实例处于可操作状态（active）；不满足时写审计并返回 ErrNotOperable。
func (s *Service) requireOperable(ctx context.Context, instance *model.Instance, actor Actor, action, label string) error {
	if model.IsInstanceOperable(instance.Status) {
		return nil
	}
	err := fmt.Errorf("%w：实例当前状态为 %s，%s仅在 active 状态可用（暂停/终止中不可操作）",
		ErrNotOperable, instance.Status, label)
	s.auditFail(ctx, instance, actor, action, err)
	return err
}

// upstreamCall 统一执行「取客户端 + 调用」的失败处理：上游未配置或调用失败时写审计 fail 并返回分类错误。
func (s *Service) upstreamCall(ctx context.Context, instance *model.Instance, actor Actor, action string,
	call func(client *upstream.Client) error, secrets ...string) error {
	client, err := s.client(ctx)
	if err != nil {
		s.auditFail(ctx, instance, actor, action, err, secrets...)
		return err
	}
	if err := call(client); err != nil {
		wrapped := fmt.Errorf("%w：%v", ErrUpstreamFailed, err)
		s.auditFail(ctx, instance, actor, action, wrapped, secrets...)
		return wrapped
	}
	return nil
}

// sanitize 把消息中的敏感串（密码等）替换为 ***，保证审计与对外错误不含明文密码。
// 空串与过短的敏感串（<4 字符）不参与替换，避免误伤正常文案。
func sanitize(message string, secrets ...string) string {
	out := message
	for _, secret := range secrets {
		if len([]rune(secret)) < 4 {
			continue
		}
		out = strings.ReplaceAll(out, secret, "***")
	}
	return out
}
