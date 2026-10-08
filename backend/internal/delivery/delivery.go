// Package delivery 实现订单交付链路：支付成功 → 上游开通（CreateHost）→ 实例落库。
//
// 触发方式（契约 14.3）：
//   - 自动：支付入账成功（在线回调 / 余额支付）后由 router 调用 Trigger——
//     默认在后台 goroutine 执行，不阻塞回调响应；Options.Async=false 时同步执行
//     （集成测试用，触发点返回前交付已完成，可确定断言）。
//   - 手动：管理员重试交付接口调用 Deliver（同步执行，结果落在订单状态上）。
//
// 幂等与防并发：所有交付入口都先经 store.BeginDelivery 的行锁认领
// （仅 paid / 允许重试的 failed 可认领，认领即置 provisioning），
// 同一订单同一时刻只有一个交付在跑，重复回调、重复触发不会重复开通。
//
// 失败处理：认领后的任何失败（上游未配置、上游报错、配置快照损坏等）都会把订单置
// failed 并记录已脱敏的原因（provision_error），可由管理员重试；成功则插实例并置 active。
package delivery

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// 交付链路错误（router 据此区分提示；交付执行失败已落库到订单 provision_error）。
var (
	// ErrNotClaimable 表示订单当前状态不允许交付（provisioning / active / pending / cancelled，
	// 或 failed 且未允许重试）：未执行任何动作，订单保持现状。
	ErrNotClaimable = errors.New("订单当前状态不允许交付")
	// ErrProvisionFailed 表示交付已执行但失败，订单已置 failed（原因见订单 provision_error）。
	ErrProvisionFailed = errors.New("交付执行失败")
)

// 交付超时（契约 14.3）：整体上限覆盖「下单 + 余额支付 + 回读」多次上游调用；
// writeTimeout 是交付结果落库的超时（上游已开通时即使主超时耗尽也必须落库）。
const (
	defaultTimeout = 120 * time.Second
	writeTimeout   = 10 * time.Second
)

// PasswordLength 是交付时自动生成的主机密码长度。
const PasswordLength = 16

// passwordClasses 是密码字符池，按「大写 / 小写 / 数字 / 特殊」四类分组：
// 生成时每类至少取 1 个，满足上游与主机系统的常见复杂度要求；去掉易混淆字符（I/O/l/0/1）。
var passwordClasses = []string{
	"ABCDEFGHJKLMNPQRSTUVWXYZ",
	"abcdefghijkmnopqrstuvwxyz",
	"23456789",
	"!@#$%^&*",
}

// Notifier 接收交付事件的通知（阶段 6b，契约 17.4）。
//
// 契约：**交付结果提交后**调用（订单/实例已落库），实现必须不阻塞且不返回错误——
// 生产实现是 internal/notify.Service（内部异步 + 失败只记日志）；nil 表示不接线（测试默认）。
type Notifier interface {
	// OrderDelivered 新购订单交付成功（开通完成）。
	OrderDelivered(orderID uint64)
	// OrderFailed 新购订单交付失败（订单已置 failed）。
	OrderFailed(orderID uint64)
	// RenewSucceeded 续费订单交付成功。
	RenewSucceeded(orderID uint64)
}

// Options 是交付服务的构造参数。
type Options struct {
	// Async 为 true 时 Trigger 在后台 goroutine 中执行（生产默认）；
	// false 时同步执行（集成测试用）。
	Async bool
	// Timeout 是单次交付的整体超时（含多次上游调用），缺省 120s。
	Timeout time.Duration
	// Logger 为 nil 时使用 slog.Default()。
	Logger *slog.Logger
	// Notifier 是交付事件的通知回调（阶段 6b）；nil 时不发通知。
	Notifier Notifier
}

// Service 是交付服务：router 通过它触发自动交付与管理员重试。
type Service struct {
	store    *store.Store
	upstream upstream.Provider
	logger   *slog.Logger
	notifier Notifier

	async   bool
	timeout time.Duration
}

// New 构造交付服务。
func New(st *store.Store, provider upstream.Provider, opts Options) *Service {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Service{
		store:    st,
		upstream: provider,
		logger:   logger,
		notifier: opts.Notifier,
		async:    opts.Async,
		timeout:  timeout,
	}
}

// notifyDelivered / notifyFailed / notifyRenewed 是交付结果提交后的通知触发点（契约 17.4）。
// 调用方（实现须为 notify.Service）自行保证异步与失败隔离：这里只做 nil 保护。
func (s *Service) notifyDelivered(orderID uint64) {
	if s.notifier != nil {
		s.notifier.OrderDelivered(orderID)
	}
}

// notifyFailed 交付失败通知。
func (s *Service) notifyFailed(orderID uint64) {
	if s.notifier != nil {
		s.notifier.OrderFailed(orderID)
	}
}

// notifyRenewed 续费成功通知。
func (s *Service) notifyRenewed(orderID uint64) {
	if s.notifier != nil {
		s.notifier.RenewSucceeded(orderID)
	}
}

// Trigger 是支付成功后的自动交付入口（契约 14.3）：在入账事务提交后调用，
// 不阻塞调用方（默认异步；同步模式用于测试确定性）。重复触发是安全的（认领失败即返回）。
func (s *Service) Trigger(orderID uint64) {
	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
		defer cancel()
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("自动交付发生 panic", "panic", recovered, "order_id", orderID)
			}
		}()

		if _, err := s.Deliver(ctx, orderID, false); err != nil {
			if errors.Is(err, ErrNotClaimable) {
				// 重复触发（订单已在交付中/已交付）：正常幂等结果，不重复开通。
				s.logger.Info("自动交付跳过：订单当前状态不允许交付（重复触发）", "order_id", orderID)
				return
			}
			// ErrProvisionFailed 已由 Deliver 记过明细日志；其余为系统级错误。
			s.logger.Error("自动交付未完成", "error", err, "order_id", orderID)
		}
	}

	if s.async {
		go run()
		return
	}
	run()
}

// Deliver 同步执行一次交付（认领 → 上游开通 → 回读 → 落库）。
//
// 返回值与错误的对应关系：
//   - 成功：订单（status=active）+ nil；
//   - 订单不存在 / 数据库错误：nil + 原错误；
//   - 状态不允许认领：现状订单 + ErrNotClaimable（未执行任何动作）；
//   - 交付执行失败：已置 failed 的订单 + ErrProvisionFailed（原因已写入 provision_error）。
//
// allowFailed 为 true 时 failed 订单可再次认领（管理员重试）；自动触发一律传 false。
//
// 按订单类型分流（阶段 5b）：new → 上游开通（5a 骨架）；renew → 上游续费（renew.go）。
func (s *Service) Deliver(ctx context.Context, orderID uint64, allowFailed bool) (*model.Order, error) {
	claimed, order, err := s.store.BeginDelivery(ctx, orderID, allowFailed)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return order, ErrNotClaimable
	}

	if order.Type == model.OrderTypeRenew {
		return s.deliverRenew(ctx, order)
	}
	return s.deliverNew(ctx, order)
}

// deliverNew 执行一次新购开通交付（阶段 5a 骨架：上游开通 → 回读 → 落库）。
func (s *Service) deliverNew(ctx context.Context, order *model.Order) (*model.Order, error) {
	s.logger.Info("开始交付订单", "order_id", order.ID, "trade_no", order.TradeNo,
		"member_id", order.MemberID, "product_id", order.ProductID, "cycle", order.Cycle)

	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	instance, err := s.provision(callCtx, order)
	if err == nil {
		updated, completeErr := s.complete(order.ID, *instance)
		if completeErr != nil {
			// 上游已开通（可能已扣费）但落库失败：订单停在 provisioning，留痕待人工/重试核对。
			s.logger.Error("交付落库失败（上游已开通，订单停在 provisioning 需人工核对）",
				"error", completeErr, "order_id", order.ID, "trade_no", order.TradeNo, "host_id", instance.HostID)
			return nil, completeErr
		}
		s.logger.Info("订单交付完成", "order_id", updated.ID, "trade_no", updated.TradeNo,
			"host_id", instance.HostID, "product_id", instance.ProductID)
		s.auditCreate(ctx, updated, instance.HostID, true, "开通成功，主机已交付")
		// 6b 通知挂点：交付成功 → 通知会员（异步、失败不影响交付结果）。
		s.notifyDelivered(updated.ID)
		return updated, nil
	}

	failed, failErr := s.fail(order.ID, err.Error())
	if failErr != nil {
		return nil, failErr
	}
	s.logger.Error("订单交付失败", "error", err, "order_id", order.ID, "trade_no", order.TradeNo)
	s.auditCreate(ctx, failed, 0, false, "开通失败："+err.Error())
	// 6b 通知挂点：交付失败 → 通知会员（含已脱敏的失败原因）。
	s.notifyFailed(failed.ID)
	return failed, fmt.Errorf("%w: %v", ErrProvisionFailed, err)
}

// auditCreate 写开通审计：成功时用订单已落库的 host_id 反查实例；失败时尚无实例，
// 用 host_id=0（不存在）跳过——失败留痕落在订单 provision_error 上（契约 15.3 边界）。
func (s *Service) auditCreate(ctx context.Context, order *model.Order, hostID int, ok bool, message string) {
	if order == nil || !ok || hostID <= 0 {
		return
	}
	instance, err := s.store.InstanceByHostID(ctx, hostID)
	if err != nil {
		s.logger.Warn("开通审计写入前查询实例失败", "error", err, "order_id", order.ID, "host_id", hostID)
		return
	}
	s.audit(ctx, instance.ID, model.ActionCreate, model.InstanceOpSuccess, message)
}

// provision 执行上游开通并回读主机信息，返回实例落库字段。
// 返回的错误均已脱敏（不含密码等敏感字段）。
func (s *Service) provision(ctx context.Context, order *model.Order) (*store.InstanceInput, error) {
	client, err := s.client(ctx)
	if err != nil {
		return nil, err
	}

	product, err := s.store.ProductByID(ctx, order.ProductID)
	if err != nil {
		return nil, fmt.Errorf("订单快照商品 %d 查询失败：%v", order.ProductID, err)
	}

	configOptions, err := ParseConfigOptions(order.ConfigJSON)
	if err != nil {
		return nil, err
	}

	hostname := HostnameFor(order.TradeNo)
	password, err := GeneratePassword()
	if err != nil {
		return nil, fmt.Errorf("生成主机密码失败：%v", err)
	}

	result, err := client.CreateHost(ctx, upstream.CreateHostRequest{
		ProductID:     product.UpstreamPID,
		BillingCycle:  pricing.UpstreamCycle(order.Cycle),
		Hostname:      hostname,
		Password:      password,
		Quantity:      order.Qty,
		ConfigOptions: configOptions,
	})
	if err != nil {
		return nil, fmt.Errorf("上游开通失败：%v", err)
	}
	if result.HostID <= 0 {
		return nil, errors.New("上游开通成功但未返回主机 ID，无法确认交付结果")
	}

	instance := &store.InstanceInput{
		HostID:       result.HostID,
		ProductID:    order.ProductID,
		ProductName:  order.ProductName,
		Name:         hostname,
		BillingCycle: order.Cycle,
		Password:     password,
	}

	// 回读上游主机信息（到期时间/状态/IP/端口/账号密码）。
	// 回读失败不影响交付成立（主机已开通并扣费），同步字段留空，避免重复开通。
	host, err := client.Host(ctx, result.HostID)
	if err != nil {
		s.logger.Warn("开通成功但回读上游主机信息失败（实例照常落库，同步字段留空）",
			"error", err, "order_id", order.ID, "host_id", result.HostID)
		return instance, nil
	}
	instance.NextDueDate = DueDateOf(host)
	instance.UpstreamStatus = host.DomainStatus
	instance.DedicatedIP = host.DedicatedIP
	instance.AssignedIPs = strings.Join(nonEmpty(host.AssignedIPs), ",")
	instance.Port = host.Port
	if host.Username != "" {
		instance.Username = host.Username
	}
	if host.Password != "" {
		instance.Password = host.Password
	}
	return instance, nil
}

// complete 用独立超时落库交付结果（不随调用方 ctx 取消而丢失）。
func (s *Service) complete(orderID uint64, in store.InstanceInput) (*model.Order, error) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	return s.store.CompleteDelivery(ctx, orderID, in)
}

// fail 用独立超时记录交付失败（不随调用方 ctx 取消而丢失）。
func (s *Service) fail(orderID uint64, reason string) (*model.Order, error) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	return s.store.FailDelivery(ctx, orderID, reason)
}

// HostnameFor 生成开通时提交给上游的主机名（host 字段）：
// "oem-" + 本地单号小写（如 oem-o20261008143015k7q2zp，全局唯一且可回溯订单）。
func HostnameFor(tradeNo string) string {
	return "oem-" + strings.ToLower(tradeNo)
}

// ParseConfigOptions 把订单配置项快照 {"<配置项 id>": "<所选值 id>"}
// 解析为上游下单参数 configoption[<配置项 id>]=<所选值 id>（契约 8.3 / 12.3 / 14.5）。
// 取值接受 JSON 字符串或整数写法（与下单接口的归一化口径一致）。
// 空快照返回 nil；键不是正整数或值为空时返回错误（快照损坏，交付置 failed）。
func ParseConfigOptions(raw string) (map[int]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" || trimmed == "null" {
		return nil, nil
	}

	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &snapshot); err != nil {
		return nil, fmt.Errorf("订单配置项快照解析失败：%v", err)
	}

	options := make(map[int]string, len(snapshot))
	for key, rawValue := range snapshot {
		id, err := strconv.Atoi(strings.TrimSpace(key))
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("订单配置项快照含非法的配置项 ID %q", key)
		}
		value, err := configValueText(rawValue)
		if err != nil {
			return nil, fmt.Errorf("订单配置项快照的配置项 %d %v", id, err)
		}
		options[id] = value
	}
	return options, nil
}

// configValueText 解析配置项取值（接受 JSON 字符串或整数写法）。
func configValueText(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if strings.TrimSpace(text) == "" {
			return "", errors.New("取值为空")
		}
		return strings.TrimSpace(text), nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		if _, err := strconv.ParseInt(number.String(), 10, 64); err == nil {
			return number.String(), nil
		}
	}
	return "", errors.New("取值必须是字符串或整数的 upstream_id")
}

// GeneratePassword 生成主机密码：PasswordLength 位，
// 保证含大写/小写/数字/特殊四类字符（crypto/rand）。
func GeneratePassword() (string, error) {
	chars := make([]byte, 0, PasswordLength)
	for _, class := range passwordClasses {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(class))))
		if err != nil {
			return "", err
		}
		chars = append(chars, class[index.Int64()])
	}

	all := strings.Join(passwordClasses, "")
	for len(chars) < PasswordLength {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(all))))
		if err != nil {
			return "", err
		}
		chars = append(chars, all[index.Int64()])
	}

	// Fisher-Yates 洗牌，避免前四位的类别固定。
	for i := len(chars) - 1; i > 0; i-- {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		j := int(index.Int64())
		chars[i], chars[j] = chars[j], chars[i]
	}
	return string(chars), nil
}

// DueDateOf 解析上游主机的到期时间（nextduedate 是 unix 秒，契约 8.5 第 11 条）；
// 缺省或非正数时返回 nil。
func DueDateOf(host *upstream.Host) *time.Time {
	if host == nil || host.NextDueDate <= 0 {
		return nil
	}
	due := time.Unix(host.NextDueDate, 0).UTC()
	return &due
}

// nonEmpty 过滤空串（上游 assignedips 可能含空项）。
func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}
