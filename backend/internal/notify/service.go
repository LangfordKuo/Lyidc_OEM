// Package notify 实现通知体系的事件分发（阶段 6b，契约 17.4）：
// 业务事件（交付/续费/到期/工单）提交后调用这里的入口，由本包完成
// 「站内通知落库 + 通知邮件发送」两件事。
//
// 契约（调用方必须遵守）：
//   - **事件提交后调用**：调用点位于业务事务提交之后（交付结果、工单消息已落库），
//     通知失败绝不回滚业务；
//   - **不阻塞主流程**：生产走 Option.Async=true（内部 goroutine + 独立超时 + panic 恢复），
//     测试注入 Async=false 同步执行以便确定性断言；
//   - **失败只记日志**：站内通知写库失败、邮件发送失败都只写服务日志（邮件另有 email_logs 留痕），
//     不向调用方返回错误（入口方法签名均为无返回值）；
//   - **开关生效**：站内/邮件总开关、SMTP 配置每次事件都重新读取（改完设置立即生效）。
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/email"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// defaultTimeout 是单个通知任务的超时（一次事件最多几次查询 + 一次邮件发送）。
const defaultTimeout = 30 * time.Second

// maxErrorRunes 是 email_logs.error 的字符上限（与列宽 500 对齐）。
const maxErrorRunes = 500

// ErrSMTPNotConfigured 表示 SMTP 未配置或未启用（测试邮件接口据此返回明确提示）。
var ErrSMTPNotConfigured = errors.New("邮件服务未配置或未启用（请先在后台设置中填写并启用 SMTP）")

// Options 是通知服务的构造参数。
type Options struct {
	// Async 为 true 时事件入口在后台 goroutine 执行（生产默认）；
	// false 时同步执行（集成测试用，入口返回前通知已落库）。
	Async bool
	// Logger 为 nil 时使用 slog.Default()。
	Logger *slog.Logger
	// Timeout 单次通知任务超时，缺省 30s。
	Timeout time.Duration
	// Sender 覆盖邮件发送器（测试注入假实现）；nil 时按后台 SMTP 设置动态构造。
	Sender email.Sender
}

// Service 是通知服务：各业务入口（交付、扫描、工单）通过它投递通知。
type Service struct {
	store  *store.Store
	reader *settings.Reader
	logger *slog.Logger

	async   bool
	timeout time.Duration

	// fixedSender 非 nil 时恒用它（测试注入），否则按设置指纹缓存 SMTP 发送器。
	fixedSender email.Sender

	mu                sync.Mutex
	cachedSender      email.Sender
	cachedFingerprint string
}

// New 构造通知服务。
func New(st *store.Store, reader *settings.Reader, opts Options) *Service {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Service{
		store:       st,
		reader:      reader,
		logger:      logger,
		async:       opts.Async,
		timeout:     timeout,
		fixedSender: opts.Sender,
	}
}

// ---------------------------------------------------------------------------
// 事件入口（契约 17.4 事件接线表）：全部「提交后触发、不阻塞、不返回错误」
// ---------------------------------------------------------------------------

// OrderDelivered 订单交付成功（新购开通完成）→ 通知会员。
// 调用点：internal/delivery 交付落库成功之后。
func (s *Service) OrderDelivered(orderID uint64) {
	s.dispatch(model.NotificationEventOrderDelivered, orderID, func(ctx context.Context) error {
		order, err := s.store.OrderByID(ctx, orderID)
		if err != nil {
			return fmt.Errorf("查询订单失败：%w", err)
		}
		instance, err := s.instanceOfOrder(ctx, orderID)
		if err != nil {
			return err
		}
		title, content := orderDeliveredText(order, instance)
		s.pushMember(ctx, order.MemberID, model.NotificationEventOrderDelivered, title, content)
		return nil
	})
}

// OrderFailed 订单交付失败（上游开通失败）→ 通知会员。
// 调用点：internal/delivery 置 failed 之后。
func (s *Service) OrderFailed(orderID uint64) {
	s.dispatch(model.NotificationEventOrderFailed, orderID, func(ctx context.Context) error {
		order, err := s.store.OrderByID(ctx, orderID)
		if err != nil {
			return fmt.Errorf("查询订单失败：%w", err)
		}
		title, content := orderFailedText(order)
		s.pushMember(ctx, order.MemberID, model.NotificationEventOrderFailed, title, content)
		return nil
	})
}

// RenewSucceeded 续费成功 → 通知会员。
// 调用点：internal/delivery 续费落库成功之后。
func (s *Service) RenewSucceeded(orderID uint64) {
	s.dispatch(model.NotificationEventRenewSucceeded, orderID, func(ctx context.Context) error {
		order, err := s.store.OrderByID(ctx, orderID)
		if err != nil {
			return fmt.Errorf("查询订单失败：%w", err)
		}
		instance, err := s.instanceOfOrder(ctx, orderID)
		if err != nil {
			return err
		}
		title, content := renewSucceededText(order, instance)
		s.pushMember(ctx, order.MemberID, model.NotificationEventRenewSucceeded, title, content)
		return nil
	})
}

// InstanceSuspended 到期自动暂停 → 通知会员。
// 调用点：internal/scheduler 到期暂停扫描成功之后。
func (s *Service) InstanceSuspended(instanceID uint64) {
	s.dispatch(model.NotificationEventInstanceSuspended, instanceID, func(ctx context.Context) error {
		instance, err := s.store.InstanceByID(ctx, instanceID)
		if err != nil {
			return fmt.Errorf("查询实例失败：%w", err)
		}
		title, content := instanceSuspendedText(instance)
		s.pushMember(ctx, instance.MemberID, model.NotificationEventInstanceSuspended, title, content)
		return nil
	})
}

// InstanceTerminated 终止收敛（上游主机已删除，本地转 terminated）→ 通知会员。
// 调用点：internal/scheduler 取消申请收敛成功之后。
func (s *Service) InstanceTerminated(instanceID uint64) {
	s.dispatch(model.NotificationEventInstanceTerminated, instanceID, func(ctx context.Context) error {
		instance, err := s.store.InstanceByID(ctx, instanceID)
		if err != nil {
			return fmt.Errorf("查询实例失败：%w", err)
		}
		title, content := instanceTerminatedText(instance)
		s.pushMember(ctx, instance.MemberID, model.NotificationEventInstanceTerminated, title, content)
		return nil
	})
}

// ExpiryReminder 到期前提醒 → 通知会员。
// 调用点：internal/scheduler 到期提醒扫描（**去重认领成功后**才调用，契约 17.5）。
func (s *Service) ExpiryReminder(instanceID uint64) {
	s.dispatch(model.NotificationEventExpiryReminder, instanceID, func(ctx context.Context) error {
		instance, err := s.store.InstanceByID(ctx, instanceID)
		if err != nil {
			return fmt.Errorf("查询实例失败：%w", err)
		}
		days := settings.DefaultExpiryReminderDays
		if state, err := s.reader.Notifications(ctx); err == nil {
			days = state.Value.ReminderDaysOr()
		}
		title, content := expiryReminderText(instance, days)
		s.pushMember(ctx, instance.MemberID, model.NotificationEventExpiryReminder, title, content)
		return nil
	})
}

// TicketCreated 新工单 → 通知管理员/客服（站内扇出给 admin + support；邮件发 settings.site.admin_email）。
// 调用点：router 工单创建落库之后。
func (s *Service) TicketCreated(ticketID uint64) {
	s.dispatch(model.NotificationEventTicketCreated, ticketID, func(ctx context.Context) error {
		ticket, err := s.store.TicketByID(ctx, ticketID)
		if err != nil {
			return fmt.Errorf("查询工单失败：%w", err)
		}
		title, content, err := s.ticketCreatedText(ctx, ticket)
		if err != nil {
			return err
		}
		s.pushAdmins(ctx, model.NotificationEventTicketCreated, title, content)
		return nil
	})
}

// TicketRepliedByMember 会员回复工单 → 通知管理员/客服。
// 调用点：router 会员回复落库之后。
func (s *Service) TicketRepliedByMember(ticketID uint64) {
	s.dispatch(model.NotificationEventTicketReplied, ticketID, func(ctx context.Context) error {
		ticket, err := s.store.TicketByID(ctx, ticketID)
		if err != nil {
			return fmt.Errorf("查询工单失败：%w", err)
		}
		title, content, err := s.ticketMemberRepliedText(ctx, ticket)
		if err != nil {
			return err
		}
		s.pushAdmins(ctx, model.NotificationEventTicketReplied, title, content)
		return nil
	})
}

// TicketRepliedByAdmin 客服**公开**回复工单 → 通知会员（内部备注不调用本入口）。
// 调用点：router 管理端公开回复落库之后。
func (s *Service) TicketRepliedByAdmin(ticketID uint64) {
	s.dispatch(model.NotificationEventTicketReplied, ticketID, func(ctx context.Context) error {
		ticket, err := s.store.TicketByID(ctx, ticketID)
		if err != nil {
			return fmt.Errorf("查询工单失败：%w", err)
		}
		title, content := ticketAdminRepliedText(ticket)
		s.pushMember(ctx, ticket.MemberID, model.NotificationEventTicketReplied, title, content)
		return nil
	})
}

// TicketClosedByAdmin 客服关闭工单 → 通知会员（会员自行关闭不通知客服，契约 17.4 定稿）。
// 调用点：router 管理端关闭成功（真的发生关闭）之后。
func (s *Service) TicketClosedByAdmin(ticketID uint64) {
	s.dispatch(model.NotificationEventTicketClosed, ticketID, func(ctx context.Context) error {
		ticket, err := s.store.TicketByID(ctx, ticketID)
		if err != nil {
			return fmt.Errorf("查询工单失败：%w", err)
		}
		title, content := ticketClosedText(ticket)
		s.pushMember(ctx, ticket.MemberID, model.NotificationEventTicketClosed, title, content)
		return nil
	})
}

// ---------------------------------------------------------------------------
// 测试邮件（管理端设置页）：与通知链路共用同一套 SMTP 解析、报文组装与留痕
// ---------------------------------------------------------------------------

// SendTestEmail 同步发送一封测试邮件到指定地址，供管理端设置页验证 SMTP 参数。
//
// 与通知链路的差异：**不受通知总开关约束**（它就是用来验证配置的），但要求
// SMTP 已启用且必填项齐全，否则返回 ErrSMTPNotConfigured（上层映射 40002）。
func (s *Service) SendTestEmail(ctx context.Context, to string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return errors.New("收件人地址不能为空")
	}
	state, err := s.reader.SMTP(ctx)
	if err != nil {
		return fmt.Errorf("读取 SMTP 设置失败：%w", err)
	}
	if !state.Value.Usable() {
		return ErrSMTPNotConfigured
	}
	site := s.siteName(ctx)
	subject := "【" + site + "】SMTP 测试邮件"
	body := "这是一封来自 " + site + " 后台的测试邮件。\n\n" +
		"收到本邮件说明 SMTP 参数配置正确，通知邮件已可正常投递。\n" +
		"发件服务器：" + state.Value.Endpoint() + "\n"
	return s.deliver(ctx, state, to, subject, composeBody(site, body))
}

// ---------------------------------------------------------------------------
// 内部实现
// ---------------------------------------------------------------------------

// dispatch 执行一次通知任务：生产走后台 goroutine（不阻塞业务），测试可同步执行。
// 任何错误（含 panic）都只记服务日志——通知不可拖垮业务。
func (s *Service) dispatch(event string, ref uint64, run func(ctx context.Context) error) {
	task := func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
		defer cancel()
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("通知任务发生 panic", "event", event, "ref", ref, "panic", recovered)
			}
		}()
		if err := run(ctx); err != nil {
			s.logger.Warn("通知任务执行失败（不影响业务）", "event", event, "ref", ref, "error", err)
		}
	}
	if s.async {
		go task()
		return
	}
	task()
}

// pushMember 给会员投递一条通知：站内通知落库 + 通知邮件（各受总开关约束）。
func (s *Service) pushMember(ctx context.Context, memberID uint64, event, title, content string) {
	cfg, err := s.switches(ctx)
	if err != nil {
		s.logger.Warn("读取通知开关失败（跳过本次通知）", "error", err, "event", event)
		return
	}
	if !cfg.InAppEnabled && !cfg.EmailEnabled {
		return
	}

	member, err := s.store.MemberByID(ctx, memberID)
	if err != nil {
		s.logger.Warn("通知收件会员查询失败（跳过本次通知）", "error", err, "event", event, "member_id", memberID)
		return
	}

	if cfg.InAppEnabled {
		if _, err := s.store.CreateNotification(ctx, store.NotificationInput{
			RecipientType: model.NotificationRecipientMember,
			RecipientID:   memberID,
			Event:         event,
			Title:         title,
			Content:       content,
		}); err != nil {
			s.logger.Warn("站内通知写入失败（不影响业务）", "error", err, "event", event, "member_id", memberID)
		}
	}
	if cfg.EmailEnabled {
		s.sendMail(ctx, member.Email, title, content, event)
	}
}

// pushAdmins 给管理端投递一条通知：站内扇出给**在职的 admin + support**；
// 邮件发 settings.site.admin_email（有值且 SMTP 可用时）。finance 不接收（契约 17.4 定稿）。
func (s *Service) pushAdmins(ctx context.Context, event, title, content string) {
	cfg, err := s.switches(ctx)
	if err != nil {
		s.logger.Warn("读取通知开关失败（跳过本次通知）", "error", err, "event", event)
		return
	}
	if !cfg.InAppEnabled && !cfg.EmailEnabled {
		return
	}

	if cfg.InAppEnabled {
		admins, err := s.store.ListAdminRecipients(ctx)
		if err != nil {
			s.logger.Warn("管理端通知收件人查询失败（跳过本次站内通知）", "error", err, "event", event)
		} else {
			rows := make([]store.NotificationInput, 0, len(admins))
			for i := range admins {
				rows = append(rows, store.NotificationInput{
					RecipientType: model.NotificationRecipientAdmin,
					RecipientID:   admins[i].ID,
					Event:         event,
					Title:         title,
					Content:       content,
				})
			}
			if _, err := s.store.CreateNotifications(ctx, rows); err != nil {
				s.logger.Warn("管理端站内通知写入失败（不影响业务）", "error", err, "event", event)
			}
		}
	}
	if cfg.EmailEnabled {
		s.sendMail(ctx, s.adminEmail(ctx), title, content, event)
	}
}

// sendMail 按当前 SMTP 设置发送通知邮件：地址为空或 SMTP 不可用时静默跳过
// （未配置不是故障，不写失败日志），失败写 email_logs（已脱敏）并记服务日志。
//
// 注：注入了固定发送器（测试）时不再要求后台 SMTP 可用——此时「发送器是否可用」
// 由注入方决定，SMTP 设置只用于发件人地址与显示名。
func (s *Service) sendMail(ctx context.Context, to, subject, body, event string) {
	to = strings.TrimSpace(to)
	if to == "" {
		return
	}
	state, err := s.reader.SMTP(ctx)
	if err != nil {
		s.logger.Warn("读取 SMTP 设置失败（跳过本次邮件）", "error", err, "event", event)
		return
	}
	if s.fixedSender == nil && !state.Value.Usable() {
		return
	}
	site := s.siteName(ctx)
	if err := s.deliver(ctx, state, to, subject, composeBody(site, body)); err != nil {
		s.logger.Warn("通知邮件发送失败（不影响业务）", "error", err, "event", event, "to", to)
	}
}

// deliver 发送一封邮件并留痕（成功与失败都写 email_logs）；返回发送错误供调用方记日志。
func (s *Service) deliver(ctx context.Context, state settings.SMTPState, to, subject, body string) error {
	sender := s.senderFor(state)
	if sender == nil {
		return ErrSMTPNotConfigured
	}

	message := email.Message{
		To:       to,
		Subject:  subject,
		Body:     body,
		From:     state.Value.From,
		FromName: state.Value.FromName,
	}
	sendErr := sender.Send(ctx, message)

	status, errorText := model.EmailStatusSuccess, ""
	if sendErr != nil {
		status = model.EmailStatusFail
		// 留痕前兜底脱敏：绝不把认证口令写进日志表。
		errorText = truncateRunes(redact(sendErr.Error(), state.Value.Password), maxErrorRunes)
	}
	if err := s.store.AppendEmailLog(ctx, to, subject, status, errorText); err != nil {
		s.logger.Warn("邮件日志写入失败", "error", err, "to", to)
	}
	return sendErr
}

// senderFor 返回当前设置对应的发送器：测试注入的固定发送器优先，
// 否则按设置指纹缓存（管理员改完 SMTP 参数下一次发送即用新值）。
func (s *Service) senderFor(state settings.SMTPState) email.Sender {
	if s.fixedSender != nil {
		return s.fixedSender
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cachedSender != nil && s.cachedFingerprint == state.Fingerprint {
		return s.cachedSender
	}
	s.cachedSender = email.NewSMTP(email.SMTPConfig{
		Host:       state.Value.Host,
		Port:       state.Value.PortOr(),
		Username:   state.Value.Username,
		Password:   state.Value.Password,
		From:       state.Value.From,
		FromName:   state.Value.FromName,
		Encryption: state.Value.NormalizedEncryption(),
	})
	s.cachedFingerprint = state.Fingerprint
	return s.cachedSender
}

// switches 读取通知开关（未配置时返回缺省：站内/邮件/到期提醒全开、提前 7 天）。
func (s *Service) switches(ctx context.Context) (settings.NotificationSettings, error) {
	state, err := s.reader.Notifications(ctx)
	if err != nil {
		return settings.NotificationSettings{}, err
	}
	return state.Value, nil
}

// siteName 读取站点名（尾注用；读取失败时回退占位符）。
func (s *Service) siteName(ctx context.Context) string {
	state, err := s.reader.Site(ctx)
	if err != nil || strings.TrimSpace(state.Value.Name) == "" {
		return "Lyidc OEM"
	}
	return state.Value.Name
}

// adminEmail 读取管理端通知邮箱（settings.site.admin_email，可空）。
func (s *Service) adminEmail(ctx context.Context) string {
	state, err := s.reader.Site(ctx)
	if err != nil {
		s.logger.Warn("读取站点设置失败（跳过管理端通知邮件）", "error", err)
		return ""
	}
	return strings.TrimSpace(state.Value.AdminEmail)
}

// instanceOfOrder 查询订单关联的实例；订单尚未落实例（异常路径）时返回 nil 而不报错。
func (s *Service) instanceOfOrder(ctx context.Context, orderID uint64) (*model.Instance, error) {
	instance, err := s.store.InstanceByOrderID(ctx, orderID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询实例失败：%w", err)
	}
	return instance, nil
}

// composeBody 给正文加站点尾注（契约 17.3：标题 + 正文 + 站点名尾注）。
func composeBody(siteName, body string) string {
	return strings.TrimRight(body, "\n") + "\n\n——\n本邮件由「" + siteName + "」自动发送，请勿直接回复。"
}

// redact 把文本中的口令替换为掩码（兜底：即便某个错误信息带了凭据也不落库）。
func redact(text, password string) string {
	if password == "" {
		return text
	}
	return strings.ReplaceAll(text, password, "****")
}

// truncateRunes 按 rune 截断（避免把多字节字符截成乱码）。
func truncateRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…"
}
