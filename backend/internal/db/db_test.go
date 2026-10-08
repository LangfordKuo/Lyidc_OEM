package db

import (
	"context"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
)

func TestPingFuncFailsWithoutConnection(t *testing.T) {
	ping := PingFunc(nil)

	if err := ping(context.Background()); err == nil {
		t.Fatal("PingFunc(nil) 期望返回错误，实际为 nil")
	}
}

func TestOpenRejectsInvalidDSN(t *testing.T) {
	cfg := config.Default().Database
	cfg.DSN = "这不是一个合法的 DSN"

	gdb, err := Open(cfg)
	if err == nil {
		t.Fatal("Open() 期望返回错误，实际为 nil")
	}
	if gdb != nil {
		t.Errorf("Open() 失败时应返回 nil 句柄，实际 %v", gdb)
	}
}

func TestCloseAcceptsNil(t *testing.T) {
	if err := Close(nil); err != nil {
		t.Errorf("Close(nil) = %v, 期望 nil", err)
	}
}
