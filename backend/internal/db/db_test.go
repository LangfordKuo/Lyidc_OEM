package db

import (
	"context"
	"strings"
	"testing"
	"time"

	gomysql "github.com/go-sql-driver/mysql"

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

func TestNormalizeDSNForcesParseTimeAndUTC(t *testing.T) {
	dsn, err := NormalizeDSN("root:lyidc123@tcp(127.0.0.1:3306)/lyidc?charset=utf8mb4&loc=Local")
	if err != nil {
		t.Fatalf("NormalizeDSN() 返回错误: %v", err)
	}
	if strings.Contains(dsn, "loc=Local") {
		t.Errorf("归一化 DSN 仍保留 loc=Local: %q", dsn)
	}

	parsed, err := gomysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("归一化 DSN 无法解析: %v", err)
	}
	if !parsed.ParseTime {
		t.Error("归一化 DSN 未开启 parseTime")
	}
	if parsed.Loc != time.UTC {
		t.Errorf("归一化 DSN loc = %v, 期望 UTC", parsed.Loc)
	}
	if parsed.DBName != "lyidc" || parsed.User != "root" {
		t.Errorf("归一化 DSN 丢失连接信息: %+v", parsed)
	}
}

func TestNormalizeDSNRejectsInvalidInput(t *testing.T) {
	if _, err := NormalizeDSN("这不是一个合法的 DSN"); err == nil {
		t.Fatal("NormalizeDSN() 期望返回错误，实际为 nil")
	}
}
