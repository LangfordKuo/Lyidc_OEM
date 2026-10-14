package oplog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// testLogger 返回丢弃全部日志的 logger，避免测试输出噪音。
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// collector 是带锁的落库收集器（可注入 Writer.Insert）。
type collector struct {
	mu      sync.Mutex
	entries []store.OperationLogInput
	calls   int
}

// insert 实现 Options.Insert。
func (c *collector) insert(_ context.Context, entries []store.OperationLogInput) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.entries = append(c.entries, entries...)
	return nil
}

// snapshot 返回已收到的条目副本与调用次数。
func (c *collector) snapshot() ([]store.OperationLogInput, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]store.OperationLogInput, len(c.entries))
	copy(out, c.entries)
	return out, c.calls
}

// TestEnqueueDoesNotBlockAndFlushWaitsForWrite 验证：落库被卡住时 Enqueue 立即返回（不阻塞业务），
// 而 Flush 会一直等到这批真的落库（清空接口依赖它把在途缓冲冲出）。
func TestEnqueueDoesNotBlockAndFlushWaitsForWrite(t *testing.T) {
	release := make(chan struct{})
	blocked := make(chan struct{})
	var once sync.Once

	writer := NewWriter(Options{
		Logger:        testLogger(),
		FlushInterval: 10 * time.Millisecond,
		Insert: func(_ context.Context, _ []store.OperationLogInput) error {
			once.Do(func() { close(blocked) })
			<-release
			return nil
		},
	})

	writer.Enqueue(Entry{Channel: ChannelAdmin, Action: "第一条"})
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("后台写入未在预期时间内开始")
	}

	// 落库卡住期间连续入队：必须立即返回。
	done := make(chan struct{})
	go func() {
		for i := 0; i < 500; i++ {
			writer.Enqueue(Entry{Channel: ChannelAdmin, Action: "并发入队"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Enqueue 阻塞了调用方（要求：写日志不阻塞业务请求）")
	}

	// Flush 不能提前返回。
	flushed := make(chan error, 1)
	go func() { flushed <- writer.Flush(context.Background()) }()
	select {
	case err := <-flushed:
		t.Fatalf("Flush 在落库完成前就返回了: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-flushed:
		if err != nil {
			t.Fatalf("Flush 返回错误: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("放行落库后 Flush 仍未返回")
	}
	writer.Close()
}

// TestEnqueueDropsWhenBufferFull 验证：缓冲满时丢弃并立即返回（不阻塞、不 panic），
// 且丢弃的条目不会被落库。
func TestEnqueueDropsWhenBufferFull(t *testing.T) {
	release := make(chan struct{})
	blocked := make(chan struct{})
	var once sync.Once
	collector := &collector{}

	writer := NewWriter(Options{
		Logger:        testLogger(),
		QueueSize:     4,
		BatchSize:     1,         // 每条即触发一次落库，便于把队列填满
		FlushInterval: time.Hour, // 只由「攒满一批」触发落库
		Insert: func(ctx context.Context, entries []store.OperationLogInput) error {
			once.Do(func() { close(blocked) })
			<-release
			return collector.insert(ctx, entries)
		},
	})

	writer.Enqueue(Entry{Channel: ChannelMember, Action: "第 1 条"})
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("后台写入未在预期时间内开始")
	}

	// 队列容量 4：填满它。
	for i := 0; i < 4; i++ {
		writer.Enqueue(Entry{Channel: ChannelMember, Action: "填满缓冲"})
	}
	// 再多一条：缓冲已满 → 丢弃并立即返回。
	dropDone := make(chan struct{})
	go func() {
		writer.Enqueue(Entry{Channel: ChannelMember, Action: "应被丢弃"})
		close(dropDone)
	}()
	select {
	case <-dropDone:
	case <-time.After(2 * time.Second):
		t.Fatal("缓冲满时 Enqueue 阻塞了调用方")
	}

	close(release)
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatalf("Flush 返回错误: %v", err)
	}
	writer.Close()

	entries, _ := collector.snapshot()
	if len(entries) != 5 {
		t.Fatalf("落库条数 = %d，期望 5（1 条在途中 + 4 条缓冲；第 6 条被丢弃）", len(entries))
	}
	for _, entry := range entries {
		if entry.Action == "应被丢弃" {
			t.Fatal("缓冲满时的条目应被丢弃，不应落库")
		}
	}
}

// TestFlushWritesNormalizedEntries 验证：Flush 把缓冲条目按列宽截断 + 脱敏后落库，
// 结果由 HTTP 状态码推导。
func TestFlushWritesNormalizedEntries(t *testing.T) {
	collector := &collector{}
	writer := NewWriter(Options{
		Logger:        testLogger(),
		FlushInterval: time.Hour,
		Insert:        collector.insert,
	})
	t.Cleanup(writer.Close)

	createdAt := time.Date(2026, 10, 14, 7, 15, 0, 0, time.UTC)
	writer.Enqueue(Entry{
		Channel:    ChannelAdmin,
		ActorID:    7,
		ActorName:  strings.Repeat("长", 100),
		Action:     strings.Repeat("动", 200),
		Method:     "PUT",
		Path:       strings.Repeat("/p", 200),
		HTTPStatus: 403,
		IP:         "203.0.113.5",
		DurationMS: 12,
		Summary:    "password=hunter2 " + strings.Repeat("摘", 600),
		CreatedAt:  createdAt,
	})
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatalf("Flush 返回错误: %v", err)
	}

	entries, _ := collector.snapshot()
	if len(entries) != 1 {
		t.Fatalf("落库条数 = %d，期望 1", len(entries))
	}
	entry := entries[0]
	if entry.ActorID != 7 || entry.ActorName != strings.Repeat("长", maxActorNameRunes) {
		t.Fatalf("操作人字段异常: id=%d name=%q", entry.ActorID, entry.ActorName)
	}
	if len([]rune(entry.Action)) != maxActionRunes || len([]rune(entry.Path)) != maxPathRunes ||
		len([]rune(entry.Summary)) != maxSummaryRunes {
		t.Fatalf("字段未按列宽截断: action=%d path=%d summary=%d",
			len([]rune(entry.Action)), len([]rune(entry.Path)), len([]rune(entry.Summary)))
	}
	if strings.Contains(entry.Summary, "hunter2") || !strings.Contains(entry.Summary, "password=***") {
		t.Fatalf("摘要未脱敏: %q", entry.Summary)
	}
	if entry.Status != StatusFailed {
		t.Fatalf("status = %q，期望由 403 推导为 failed", entry.Status)
	}
	if !entry.CreatedAt.Equal(createdAt) {
		t.Fatalf("created_at = %v，期望保留入队时间 %v", entry.CreatedAt, createdAt)
	}
}

// TestWriteFailureIsSwallowed 验证：落库失败只记服务日志，Flush 不向调用方抛错（业务不受影响）。
func TestWriteFailureIsSwallowed(t *testing.T) {
	writer := NewWriter(Options{
		Logger:        testLogger(),
		FlushInterval: time.Hour,
		Insert: func(context.Context, []store.OperationLogInput) error {
			return errors.New("数据库故障")
		},
	})
	t.Cleanup(writer.Close)

	writer.Enqueue(Entry{Channel: ChannelMember, Action: "会失败"})
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatalf("Flush 不应向上抛落库错误: %v", err)
	}
}

// TestCloseFlushesRemainingAndDropsLateEntries 验证：Close 冲出剩余缓冲，
// 关闭后再 Enqueue 不 panic 且不再落库。
func TestCloseFlushesRemainingAndDropsLateEntries(t *testing.T) {
	collector := &collector{}
	writer := NewWriter(Options{
		Logger:        testLogger(),
		FlushInterval: time.Hour,
		Insert:        collector.insert,
	})

	writer.Enqueue(Entry{Channel: ChannelScheduler, Action: "关闭前"})
	writer.Close()

	entries, _ := collector.snapshot()
	if len(entries) != 1 || entries[0].Action != "关闭前" {
		t.Fatalf("Close 未冲出剩余缓冲: %+v", entries)
	}

	writer.Enqueue(Entry{Channel: ChannelScheduler, Action: "关闭后"})
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatalf("关闭后 Flush 返回错误: %v", err)
	}
	writer.Close() // 幂等

	entries, _ = collector.snapshot()
	if len(entries) != 1 {
		t.Fatalf("关闭后不应再落库任何条目: %+v", entries)
	}
}

// TestDiscardWriterIsNoop 验证：Discard 写入器丢弃全部条目（无数据库/测试默认路径），
// 且 nil 写入器上的调用同样安全。
func TestDiscardWriterIsNoop(t *testing.T) {
	writer := Discard(testLogger())
	writer.Enqueue(Entry{Channel: ChannelAdmin, Action: "丢弃"})
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatalf("Discard Flush 返回错误: %v", err)
	}
	writer.Close()

	var nilWriter *Writer
	nilWriter.Enqueue(Entry{Channel: ChannelAdmin})
	if err := nilWriter.Flush(context.Background()); err != nil {
		t.Fatalf("nil Flush 返回错误: %v", err)
	}
	nilWriter.Close()
}

// TestStatusFromHTTPStatus 验证结果推导口径。
func TestStatusFromHTTPStatus(t *testing.T) {
	cases := map[int]string{
		0:   StatusSuccess,
		200: StatusSuccess,
		202: StatusSuccess,
		302: StatusSuccess,
		400: StatusFailed,
		401: StatusFailed,
		403: StatusFailed,
		404: StatusFailed,
		500: StatusFailed,
	}
	for code, want := range cases {
		if got := StatusFromHTTPStatus(code); got != want {
			t.Errorf("StatusFromHTTPStatus(%d) = %q，期望 %q", code, got, want)
		}
	}
}

// TestNormalizeDerivesStatusAndKeepsDefaults 验证结果缺省推导与空 created_at 兜底。
func TestNormalizeDerivesStatusAndKeepsDefaults(t *testing.T) {
	entry := normalize(Entry{Channel: ChannelAdmin, HTTPStatus: 401})
	if entry.Status != StatusFailed {
		t.Fatalf("status = %q，期望按 401 推导为 failed", entry.Status)
	}
	if entry.CreatedAt.IsZero() {
		t.Fatal("created_at 不应为零值")
	}
	entry = normalize(Entry{Channel: ChannelAdmin, Status: StatusSuccess, HTTPStatus: 500})
	if entry.Status != StatusSuccess {
		t.Fatalf("显式 status 不应被 HTTP 码覆盖: %q", entry.Status)
	}
}

// TestSanitizeRedactsSecretAssignments 验证脱敏只作用于「密钥名=值」形式，不误伤中文摘要。
func TestSanitizeRedactsSecretAssignments(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"password=hunter2", "password=***"},
		{"Password: hunter2 登录失败", "Password=*** 登录失败"},
		{"api_key=abc123 已保存", "api_key=*** 已保存"},
		{"smtp_password=xyz", "smtp_password=***"},
		{"token=eyJhbGciOi", "token=***"},
		{"已保存易支付设置（更换密钥=true）", "已保存易支付设置（更换密钥=true）"},
		{"修改密码成功", "修改密码成功"},
		{"重置实例密码成功", "重置实例密码成功"},
		{"", ""},
	}
	for _, tt := range cases {
		if got := Sanitize(tt.in); got != tt.want {
			t.Errorf("Sanitize(%q) = %q，期望 %q", tt.in, got, tt.want)
		}
	}
}
