package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// 通知文案模板（契约 17.4）。正文**不含密码、IP 等敏感信息**：
// 只出现单号、商品名、实例名、到期时间与既有审计文案（失败原因已由交付链路脱敏）。
const (
	// 到期提醒的天数兜底（实例到期时间已过或取不到时）。
	unknownDueText = "未同步"
)

// orderDeliveredText 组装「交付成功」通知文案。
func orderDeliveredText(order *model.Order, instance *model.Instance) (string, string) {
	title := "订单交付成功"
	due := unknownDueText
	if instance != nil && instance.NextDueDate != nil {
		due = formatTime(*instance.NextDueDate)
	}
	content := fmt.Sprintf("您的订单 %s（%s）已开通完成，实例 %s 已交付，到期时间 %s。",
		order.TradeNo, order.ProductName, instanceName(order, instance), due)
	return title, content
}

// orderFailedText 组装「交付失败」通知文案（失败原因来自订单 provision_error，已脱敏）。
func orderFailedText(order *model.Order) (string, string) {
	title := "订单交付失败"
	reason := strings.TrimSpace(order.ProvisionError)
	if reason == "" {
		reason = "上游开通未成功，请稍后重试或联系客服"
	}
	content := fmt.Sprintf("您的订单 %s（%s）开通失败：%s。\n\n"+
		"我们已将失败原因记录在订单上，可稍后由管理员重试交付；如有疑问请提交工单。",
		order.TradeNo, order.ProductName, reason)
	return title, content
}

// renewSucceededText 组装「续费成功」通知文案。
func renewSucceededText(order *model.Order, instance *model.Instance) (string, string) {
	title := "续费成功"
	due := unknownDueText
	if instance != nil && instance.NextDueDate != nil {
		due = formatTime(*instance.NextDueDate)
	}
	content := fmt.Sprintf("您的续费订单 %s（%s）已完成，实例 %s 新的到期时间为 %s。",
		order.TradeNo, order.ProductName, instanceName(order, instance), due)
	return title, content
}

// instanceSuspendedText 组装「到期自动暂停」通知文案。
func instanceSuspendedText(instance *model.Instance) (string, string) {
	title := "实例已暂停（到期未续费）"
	content := fmt.Sprintf("您的实例 %s（%s）已于到期时间 %s 后自动暂停。\n\n"+
		"续费成功后系统会自动恢复主机；如不再需要，可在实例详情中申请终止。",
		instance.Name, instance.ProductName, formatDue(instance.NextDueDate))
	return title, content
}

// instanceTerminatedText 组装「终止收敛」通知文案。
func instanceTerminatedText(instance *model.Instance) (string, string) {
	title := "实例已终止"
	content := fmt.Sprintf("您的实例 %s（%s）已完成终止：上游主机已删除，本系统状态已收敛为 terminated。",
		instance.Name, instance.ProductName)
	if reason := strings.TrimSpace(instance.CancelReason); reason != "" {
		content += "\n\n终止申请原因：" + reason
	}
	return title, content
}

// expiryReminderText 组装「到期前提醒」通知文案。
func expiryReminderText(instance *model.Instance, days int) (string, string) {
	title := fmt.Sprintf("实例即将到期（%d 天内）", days)
	content := fmt.Sprintf("您的实例 %s（%s）将于 %s 到期（提前 %d 天提醒）。\n\n"+
		"请及时续费，以免到期后主机被自动暂停。",
		instance.Name, instance.ProductName, formatDue(instance.NextDueDate), days)
	return title, content
}

// ticketCreatedText 组装「新工单」通知文案（管理端）。
func (s *Service) ticketCreatedText(ctx context.Context, ticket *model.Ticket) (string, string, error) {
	username, err := s.memberName(ctx, ticket.MemberID)
	if err != nil {
		return "", "", err
	}
	title := "新工单：" + ticket.Subject
	content := fmt.Sprintf("会员 %s 提交了工单 %s（分类 %s）。\n\n标题：%s\n请及时在后台工单列表处理。",
		username, ticket.TradeNo, ticket.Category, ticket.Subject)
	return title, content, nil
}

// ticketMemberRepliedText 组装「会员回复工单」通知文案（管理端）。
func (s *Service) ticketMemberRepliedText(ctx context.Context, ticket *model.Ticket) (string, string, error) {
	username, err := s.memberName(ctx, ticket.MemberID)
	if err != nil {
		return "", "", err
	}
	title := "工单收到会员回复：" + ticket.Subject
	content := fmt.Sprintf("会员 %s 回复了工单 %s（%s）。\n\n请及时在后台工单列表查看并处理。",
		username, ticket.TradeNo, ticket.Subject)
	return title, content, nil
}

// ticketAdminRepliedText 组装「客服回复工单」通知文案（会员侧）。
func ticketAdminRepliedText(ticket *model.Ticket) (string, string) {
	title := "工单已回复：" + ticket.Subject
	content := fmt.Sprintf("客服已回复您的工单 %s（%s）。\n\n请登录会员中心查看回复内容。",
		ticket.TradeNo, ticket.Subject)
	return title, content
}

// ticketClosedText 组装「工单被客服关闭」通知文案（会员侧）。
func ticketClosedText(ticket *model.Ticket) (string, string) {
	title := "工单已关闭：" + ticket.Subject
	content := fmt.Sprintf("您的工单 %s（%s）已被客服关闭。如仍有问题，请新开工单。",
		ticket.TradeNo, ticket.Subject)
	return title, content
}

// memberName 取会员账号名（用于管理端通知文案；会员不存在时回退占位符）。
func (s *Service) memberName(ctx context.Context, memberID uint64) (string, error) {
	member, err := s.store.MemberByID(ctx, memberID)
	if err != nil {
		// 会员被删除属于历史数据边界：文案里用占位符，通知照常投递。
		return unknownMemberName, nil
	}
	return member.Username, nil
}

// unknownMemberName 是会员账号查不到时的占位符。
const unknownMemberName = "-"

// instanceName 取实例名（实例查不到时回退订单上的商品名）。
func instanceName(order *model.Order, instance *model.Instance) string {
	if instance != nil && strings.TrimSpace(instance.Name) != "" {
		return instance.Name
	}
	return order.ProductName
}

// formatDue 渲染到期时间（nil 输出占位符）。
func formatDue(due *time.Time) string {
	if due == nil {
		return unknownDueText
	}
	return formatTime(*due)
}

// formatTime 渲染 UTC 时间（通知文案统一口径：yyyy-MM-dd HH:mm UTC）。
func formatTime(at time.Time) string {
	return at.UTC().Format("2006-01-02 15:04") + " UTC"
}
