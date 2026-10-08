package upstream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// stubLoader 是可切换的 Loader：记录调用次数，按当前状态返回配置与指纹。
type stubLoader struct {
	mu          sync.Mutex
	baseURL     string
	apiKey      string
	fingerprint string
	calls       int
	err         error
}

func (l *stubLoader) load(context.Context) (Config, string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.err != nil {
		return Config{}, "", l.err
	}
	return Config{
		BaseURL:      l.baseURL,
		Username:     testUsername,
		APIKey:       l.apiKey,
		Timeout:      time.Second,
		RetryBackoff: time.Millisecond,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, l.fingerprint, nil
}

func (l *stubLoader) update(baseURL, apiKey, fingerprint string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.baseURL, l.apiKey, l.fingerprint = baseURL, apiKey, fingerprint
}

// TestManagerReusesClientWhenFingerprintUnchanged 验证设置未变时复用同一个客户端
// （其 JWT 缓存保留），设置变化时重建并丢弃旧登录态（契约 12.1）。
func TestManagerReusesClientWhenFingerprintUnchanged(t *testing.T) {
	ctx := context.Background()
	fake := newFakeUpstream(t, nil)
	loader := &stubLoader{baseURL: fake.server.URL, apiKey: testAPIKey, fingerprint: `{"v":1}`}
	manager := NewManager(loader.load, nil)

	first, enabled, err := manager.Current(ctx)
	if err != nil || !enabled {
		t.Fatalf("Current() = %v, %v, %v", first, enabled, err)
	}
	if _, err := first.Login(ctx); err != nil {
		t.Fatalf("Login() 失败: %v", err)
	}
	if first.token() == "" {
		t.Fatal("登录后 JWT 缓存为空")
	}

	// 指纹未变：同一个客户端实例，JWT 缓存仍在（不会重复登录）。
	second, _, err := manager.Current(ctx)
	if err != nil {
		t.Fatalf("Current() 失败: %v", err)
	}
	if second != first {
		t.Fatal("指纹未变时应复用同一个客户端")
	}
	if second.token() == "" {
		t.Fatal("复用客户端时不应丢弃 JWT 缓存")
	}

	// 指纹变化（管理员改了设置）：重建客户端，旧 JWT 缓存失效。
	loader.update(fake.server.URL, testAPIKey, `{"v":2}`)
	third, _, err := manager.Current(ctx)
	if err != nil {
		t.Fatalf("Current() 失败: %v", err)
	}
	if third == first {
		t.Fatal("指纹变化时应重建客户端")
	}
	if third.token() != "" {
		t.Fatal("重建客户端后旧 JWT 缓存必须失效")
	}

	// 只改密钥（指纹随之变化）同样触发重建。
	loader.update(fake.server.URL, testAPIKey+"-new", `{"v":3}`)
	fourth, _, err := manager.Current(ctx)
	if err != nil {
		t.Fatalf("Current() 失败: %v", err)
	}
	if fourth == third {
		t.Fatal("改密钥后应重建客户端")
	}
}

// TestManagerUnconfigured 验证未配置齐全时返回 enabled=false 与可展示的客户端。
func TestManagerUnconfigured(t *testing.T) {
	ctx := context.Background()
	loader := &stubLoader{fingerprint: ""}
	manager := NewManager(loader.load, nil)

	client, enabled, err := manager.Current(ctx)
	if err != nil {
		t.Fatalf("未配置不应返回错误: %v", err)
	}
	if enabled || client.Enabled() {
		t.Fatal("未配置齐全时 enabled 应为 false")
	}

	// 空 BaseURL 也是未配置（不报错）。
	loader.update("https://lyew.example.com", testAPIKey, `{"v":1}`)
	client, enabled, err = manager.Current(ctx)
	if err != nil || !enabled || client.BaseURL() != "https://lyew.example.com" {
		t.Fatalf("Current() = %v, %v, %v", client, enabled, err)
	}
}

// TestManagerPropagatesLoaderError 验证设置读取失败原样上报。
func TestManagerPropagatesLoaderError(t *testing.T) {
	dbErr := errors.New("db down")
	manager := NewManager((&stubLoader{err: dbErr}).load, nil)

	if _, _, err := manager.Current(context.Background()); !errors.Is(err, dbErr) {
		t.Fatalf("err = %v，期望 %v", err, dbErr)
	}
}

// TestManagerRejectsInvalidBaseURL 验证设置里的地址非法时报错（不返回半可用客户端）。
func TestManagerRejectsInvalidBaseURL(t *testing.T) {
	loader := &stubLoader{baseURL: "lyew.example.com", apiKey: testAPIKey, fingerprint: `{"v":1}`}
	manager := NewManager(loader.load, nil)

	if _, _, err := manager.Current(context.Background()); err == nil {
		t.Fatal("非法 base_url 应返回错误")
	}
}

// TestStaticProvider 验证固定客户端提供者与 nil 语义。
func TestStaticProvider(t *testing.T) {
	ctx := context.Background()
	if _, _, err := (StaticProvider{}).Current(ctx); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil 客户端 err = %v，期望 ErrNotConfigured", err)
	}

	fake := newFakeUpstream(t, nil)
	client := fake.client(t, nil)
	got, enabled, err := StaticProvider{C: client}.Current(ctx)
	if err != nil || !enabled || got != client {
		t.Fatalf("StaticProvider.Current() = %v, %v, %v", got, enabled, err)
	}
}
