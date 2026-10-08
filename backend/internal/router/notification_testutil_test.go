package router

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/delivery"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/email"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/notify"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/scheduler"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// ---------------------------------------------------------------------------
// 阶段 6b 测试基建：假邮件发送器、同步通知引擎、通知/邮件日志读取
// ---------------------------------------------------------------------------

// fakeSender 是 email.Sender 的内存实现：记录每封邮件（断言收件人/主题/正文），可注入发送失败。
type fakeSender struct {
	mu   sync.Mutex
	sent []email.Message
	fail error
}

// Send 记录一封邮件；注入了 fail 时返回该错误（模拟 SMTP 故障）。
func (f *fakeSender) Send(_ context.Context, msg email.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, msg)
	return nil
}

// messages 返回已发送的邮件副本。
func (f *fakeSender) messages() []email.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]email.Message, len(f.sent))
	copy(out, f.sent)
	return out
}

// count 返回已发送邮件数。
func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// setFail 注入发送失败（nil 表示恢复）。
func (f *fakeSender) setFail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = err
}

// reset 清空已发送记录与注入的失败。
func (f *fakeSender) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = nil
	f.fail = nil
}

// newTestNotifier 构造**同步**通知服务（Async=false：入口返回前通知已落库/已发信），
// 注入假发送器使邮件可断言而不真的连 SMTP。
func newTestNotifier(t *testing.T, gdb *gorm.DB) (*notify.Service, *fakeSender) {
	t.Helper()
	sender := &fakeSender{}
	service := notify.New(store.New(gdb), settings.NewReader(store.New(gdb)), notify.Options{
		Async:  false,
		Logger: silentLogger(),
		Sender: sender,
	})
	return service, sender
}

// newTicketNotifyEngine 构造工单用例引擎（纯本地域 + 同步通知接线）。
func newTicketNotifyEngine(t *testing.T, gdb *gorm.DB) (*gin.Engine, *notify.Service, *fakeSender) {
	t.Helper()
	service, sender := newTestNotifier(t, gdb)
	engine := New(Options{
		Logger:   silentLogger(),
		DB:       gdb,
		JWT:      config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		Notifier: service,
	})
	return engine, service, sender
}

// newStage6Engine 构造「真实数据库 + 假上游 + 同步交付 + 同步通知」的引擎：
// 交付/续费事件在接口返回前完成通知投递，可确定性断言通知落库与邮件。
func newStage6Engine(t *testing.T, gdb *gorm.DB, client *upstream.Client) (*gin.Engine, *notify.Service, *fakeSender) {
	t.Helper()
	logger := silentLogger()
	service, sender := newTestNotifier(t, gdb)
	provider := upstream.StaticProvider{C: client}
	engine := New(Options{
		Logger:   logger,
		DB:       gdb,
		JWT:      config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		Upstream: provider,
		Delivery: delivery.New(store.New(gdb), provider, delivery.Options{
			Async: false, Logger: logger, Notifier: service,
		}),
		Notifier: service,
	})
	return engine, service, sender
}

// newNotifierScanner 构造注入了通知的扫描器（clock 注入 + 手动 ScanOnce，通知同步完成）。
func newNotifierScanner(t *testing.T, gdb *gorm.DB, host *fakeHostServer, now time.Time,
	service *notify.Service) *scheduler.Scanner {
	t.Helper()
	return scheduler.New(store.New(gdb), upstream.StaticProvider{C: host.client(t)}, scheduler.Options{
		Clock:    func() time.Time { return now },
		Logger:   silentLogger(),
		Notifier: service,
	})
}

// seedSiteSetting 写站点设置（admin_email 是管理端通知邮件的收件人）。
func seedSiteSetting(t *testing.T, gdb *gorm.DB, name, adminEmail string) {
	t.Helper()
	seedSetting(t, gdb, settings.KeySite,
		fmt.Sprintf(`{"name":%q,"url":"","admin_email":%q}`, name, adminEmail))
}

// seedNotificationSettings 写通知开关设置（站内 / 邮件 / 到期提醒 + 天数）。
func seedNotificationSettings(t *testing.T, gdb *gorm.DB, inapp, mail, expiry bool, days int) {
	t.Helper()
	seedSetting(t, gdb, settings.KeyNotifications, fmt.Sprintf(
		`{"inapp_enabled":%t,"email_enabled":%t,"expiry_reminder_enabled":%t,"expiry_reminder_days":%d}`,
		inapp, mail, expiry, days))
}

// seedNotification 直接写库造一条通知（构造收件箱前置数据）。
func seedNotification(t *testing.T, gdb *gorm.DB, recipientType string, recipientID uint64,
	event, title, content string, read bool) *model.Notification {
	t.Helper()
	notification := model.Notification{
		RecipientType: recipientType,
		RecipientID:   recipientID,
		Event:         event,
		Title:         title,
		Content:       content,
		CreatedAt:     time.Now().UTC(),
	}
	if read {
		readAt := time.Now().UTC()
		notification.ReadAt = &readAt
	}
	if err := gdb.Create(&notification).Error; err != nil {
		t.Fatalf("写入测试通知失败: %v", err)
	}
	return &notification
}

// notificationsOf 读库取某接收方的全部通知（新建在前）。
func notificationsOf(t *testing.T, gdb *gorm.DB, recipientType string, recipientID uint64) []model.Notification {
	t.Helper()
	items := make([]model.Notification, 0)
	if err := gdb.Where("recipient_type = ? AND recipient_id = ?", recipientType, recipientID).
		Order("id DESC").Find(&items).Error; err != nil {
		t.Fatalf("查询通知失败: %v", err)
	}
	return items
}

// notificationsByEvent 读库取某接收方某事件的通知（新建在前）。
func notificationsByEvent(t *testing.T, gdb *gorm.DB, recipientType string, recipientID uint64,
	event string) []model.Notification {
	t.Helper()
	items := make([]model.Notification, 0)
	if err := gdb.Where("recipient_type = ? AND recipient_id = ? AND event = ?",
		recipientType, recipientID, event).Order("id DESC").Find(&items).Error; err != nil {
		t.Fatalf("查询通知失败: %v", err)
	}
	return items
}

// countNotifications 读库统计通知总数（按接收方；recipientID 为 0 表示不按接收方过滤）。
func countNotifications(t *testing.T, gdb *gorm.DB, recipientType string, recipientID uint64) int64 {
	t.Helper()
	var count int64
	query := gdb.Model(&model.Notification{})
	if recipientType != "" {
		query = query.Where("recipient_type = ?", recipientType)
	}
	if recipientID != 0 {
		query = query.Where("recipient_id = ?", recipientID)
	}
	if err := query.Count(&count).Error; err != nil {
		t.Fatalf("统计通知失败: %v", err)
	}
	return count
}

// emailLogsFromDB 读库取邮件留痕（新建在前）。
func emailLogsFromDB(t *testing.T, gdb *gorm.DB) []model.EmailLog {
	t.Helper()
	items := make([]model.EmailLog, 0)
	if err := gdb.Order("id DESC").Find(&items).Error; err != nil {
		t.Fatalf("查询邮件日志失败: %v", err)
	}
	return items
}
