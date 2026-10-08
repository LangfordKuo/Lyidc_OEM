package payment

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
)

// stubProvider 是最小 Provider 实现，用于注册表测试。
type stubProvider struct{ name string }

func (s *stubProvider) Name() string                   { return s.name }
func (s *stubProvider) PayTypes() []string             { return []string{"alipay"} }
func (s *stubProvider) Ack(bool) string                { return "ok" }
func (s *stubProvider) ReturnRedirect() (string, bool) { return "", false }
func (s *stubProvider) CreateOrder(context.Context, CreateRequest) (*CreateResult, error) {
	return &CreateResult{}, nil
}
func (s *stubProvider) ParseNotify(*http.Request) (*Notification, error) { return &Notification{}, nil }

func TestRegistry(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()
	registry.Register("a", Static(&stubProvider{name: "a"}))
	registry.Register("b", Static(&stubProvider{name: "b"}))

	if names := registry.Names(); !reflect.DeepEqual(names, []string{"a", "b"}) {
		t.Fatalf("Names = %v, 期望 [a b]", names)
	}
	if !registry.Has("a") || registry.Has("epay") {
		t.Fatalf("Has 判定错误：a=%v epay=%v", registry.Has("a"), registry.Has("epay"))
	}

	provider, err := registry.Get(ctx, "a")
	if err != nil || provider.Name() != "a" {
		t.Fatalf("Get(a) = %v, %v", provider, err)
	}

	// 未登记的渠道返回 ErrUnknownChannel。
	if _, err := registry.Get(ctx, "epay"); !errors.Is(err, ErrUnknownChannel) {
		t.Fatalf("未登记渠道的 err = %v，期望 ErrUnknownChannel", err)
	}

	// 重复登记以最后一次为准。
	registry.Register("a", Static(&stubProvider{name: "a2"}))
	if provider, _ := registry.Get(ctx, "a"); provider.Name() != "a2" {
		t.Fatalf("重复登记后渠道 = %v，期望 a2", provider.Name())
	}

	// 空注册表、nil 注册表都要安全。
	empty := NewRegistry()
	if _, err := empty.Get(ctx, "epay"); !errors.Is(err, ErrUnknownChannel) {
		t.Fatalf("空注册表 err = %v", err)
	}
	if names := empty.Names(); len(names) != 0 {
		t.Fatalf("空注册表 Names = %v", names)
	}

	var nilRegistry *Registry
	if _, err := nilRegistry.Get(ctx, "epay"); !errors.Is(err, ErrUnknownChannel) {
		t.Fatalf("nil 注册表 err = %v", err)
	}
	if names := nilRegistry.Names(); names != nil {
		t.Fatalf("nil 注册表 Names = %v", names)
	}
	nilRegistry.Register("x", Static(&stubProvider{name: "x"})) // 不应 panic
}

// TestRegistryFactoryErrors 验证工厂错误原样回传、工厂返回 nil 时按未配置处理。
func TestRegistryFactoryErrors(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()

	registry.Register("broken", func(context.Context) (Provider, error) {
		return nil, ErrNotConfigured
	})
	if _, err := registry.Get(ctx, "broken"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("工厂错误 err = %v，期望 ErrNotConfigured", err)
	}

	registry.Register("empty", func(context.Context) (Provider, error) { return nil, nil })
	if _, err := registry.Get(ctx, "empty"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("工厂返回 nil 的 err = %v，期望 ErrNotConfigured", err)
	}
}

// TestStaticFactory 验证 Static 包装器（固定实例）。
func TestStaticFactory(t *testing.T) {
	provider := &stubProvider{name: "static"}
	got, err := Static(provider)(context.Background())
	if err != nil || got != provider {
		t.Fatalf("Static() = %v, %v", got, err)
	}
}
