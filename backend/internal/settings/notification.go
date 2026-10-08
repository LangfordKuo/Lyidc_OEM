package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// KeyNotifications 是通知开关设置的键（阶段 6b，契约 17.3）。
const KeyNotifications = "notifications"

// 到期提醒天数缺省值与范围（契约 17.5）。
const (
	// DefaultExpiryReminderDays 是「到期前 N 天提醒」的缺省值。
	DefaultExpiryReminderDays = 7
	// MinExpiryReminderDays / MaxExpiryReminderDays 是允许的提醒天数范围。
	MinExpiryReminderDays = 1
	// MaxExpiryReminderDays 上限：超过一个月没有业务意义（且会把提醒窗口拉得比账单周期还长）。
	MaxExpiryReminderDays = 30
)

// NotificationSettings 是 notifications 设置的取值结构（契约 17.3）。
//
// 两个总开关分别控制**站内通知**与**邮件通知**（邮件还额外要求 email.smtp 已配置并启用）；
// 到期提醒开关只影响 scheduler 的到期提醒扫描，不影响其它事件。
type NotificationSettings struct {
	// InAppEnabled 站内通知总开关（关：不写 notifications 行）。
	InAppEnabled bool `json:"inapp_enabled"`
	// EmailEnabled 邮件通知总开关（关：不发送任何通知邮件；邮件另需 email.smtp 可用）。
	EmailEnabled bool `json:"email_enabled"`
	// ExpiryReminderEnabled 到期提醒开关（关：到期扫描不产生任何提醒）。
	ExpiryReminderEnabled bool `json:"expiry_reminder_enabled"`
	// ExpiryReminderDays 到期前多少天提醒（1-30，缺省 7）。
	ExpiryReminderDays int `json:"expiry_reminder_days"`
}

// DefaultNotificationSettings 返回未配置时的缺省开关（站内开 / 邮件开 / 到期提醒开 / 提前 7 天）。
//
// 邮件总开关缺省为开：真正发信还取决于 email.smtp 是否已配置并启用，
// 未配置时通知链路静默跳过邮件（不写失败日志，避免无配置时刷屏）。
func DefaultNotificationSettings() NotificationSettings {
	return NotificationSettings{
		InAppEnabled:          true,
		EmailEnabled:          true,
		ExpiryReminderEnabled: true,
		ExpiryReminderDays:    DefaultExpiryReminderDays,
	}
}

// NotificationUpdate 是 PUT /api/v1/admin/settings/notifications 的更新意图：nil 表示不修改。
type NotificationUpdate struct {
	InAppEnabled          *bool
	EmailEnabled          *bool
	ExpiryReminderEnabled *bool
	ExpiryReminderDays    *int
}

// Apply 把更新意图合并到当前值，并对合并结果做整体校验。
func (n NotificationSettings) Apply(upd NotificationUpdate) (NotificationSettings, error) {
	merged := n
	if upd.InAppEnabled != nil {
		merged.InAppEnabled = *upd.InAppEnabled
	}
	if upd.EmailEnabled != nil {
		merged.EmailEnabled = *upd.EmailEnabled
	}
	if upd.ExpiryReminderEnabled != nil {
		merged.ExpiryReminderEnabled = *upd.ExpiryReminderEnabled
	}
	if upd.ExpiryReminderDays != nil {
		merged.ExpiryReminderDays = *upd.ExpiryReminderDays
	}
	if err := merged.Validate(); err != nil {
		return NotificationSettings{}, err
	}
	return merged, nil
}

// Validate 校验取值：提醒天数必须在 1-30 之间。
func (n NotificationSettings) Validate() error {
	if n.ExpiryReminderDays < MinExpiryReminderDays || n.ExpiryReminderDays > MaxExpiryReminderDays {
		return fmt.Errorf("%w: expiry_reminder_days 需在 %d-%d 之间，收到 %d",
			ErrFormat, MinExpiryReminderDays, MaxExpiryReminderDays, n.ExpiryReminderDays)
	}
	return nil
}

// ReminderDaysOr 返回实际使用的提醒天数（<=0 时取缺省 7 天）。
func (n NotificationSettings) ReminderDaysOr() int {
	if n.ExpiryReminderDays <= 0 {
		return DefaultExpiryReminderDays
	}
	return n.ExpiryReminderDays
}

// Encode 返回落库用的 JSON 文本。
func (n NotificationSettings) Encode() (string, error) { return encode(n) }

// NotificationState 是 notifications 设置的读取结果。
type NotificationState struct {
	Value NotificationSettings
	Meta  Meta
	// Fingerprint 是库内 JSON 原文（未配置为空串）。
	Fingerprint string
}

// Notifications 读取 notifications 设置；**未配置时返回缺省值**（站内/邮件/到期提醒全开，提前 7 天）。
func (r *Reader) Notifications(ctx context.Context) (NotificationState, error) {
	raw, meta, err := r.raw(ctx, KeyNotifications)
	if err != nil {
		return NotificationState{}, err
	}
	value, err := decodeNotifications(raw)
	if err != nil {
		return NotificationState{}, err
	}
	return NotificationState{Value: value, Meta: meta, Fingerprint: raw}, nil
}

// decodeNotifications 解析库内 JSON；空值按缺省开关处理（与其它设置键的「空=零值」不同，
// 通知开关缺省必须是「开」，否则未配置的新站点会静默不发通知）。
func decodeNotifications(raw string) (NotificationSettings, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return DefaultNotificationSettings(), nil
	}
	value := DefaultNotificationSettings()
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return NotificationSettings{}, fmt.Errorf("%w: %s 的值不是合法 JSON：%v", ErrCorrupt, KeyNotifications, err)
	}
	if value.ExpiryReminderDays <= 0 {
		value.ExpiryReminderDays = DefaultExpiryReminderDays
	}
	return value, nil
}
