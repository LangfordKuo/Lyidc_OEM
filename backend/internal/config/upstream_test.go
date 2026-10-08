package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpstreamDefaultsToDisabled(t *testing.T) {
	cfg := Default()

	if cfg.Upstream.Enabled() {
		t.Errorf("缺省配置不应启用上游对接，实际 = %+v", cfg.Upstream)
	}
	if cfg.Upstream.TimeoutSeconds != DefaultUpstreamTimeoutSeconds {
		t.Errorf("缺省 timeout_seconds = %d, 期望 %d",
			cfg.Upstream.TimeoutSeconds, DefaultUpstreamTimeoutSeconds)
	}
	if got := cfg.Upstream.Timeout(); got != time.Duration(DefaultUpstreamTimeoutSeconds)*time.Second {
		t.Errorf("Timeout() = %s, 期望 %s", got, time.Duration(DefaultUpstreamTimeoutSeconds)*time.Second)
	}
	if missing := cfg.Upstream.MissingFields(); missing != nil {
		t.Errorf("完全未配置时 MissingFields() = %v, 期望 nil", missing)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("缺省配置应通过校验，实际: %v", err)
	}
}

func TestUpstreamValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     UpstreamConfig
		wantErr string
	}{
		{
			name: "合法配置",
			cfg:  UpstreamConfig{BaseURL: "https://lyew.com", Username: "13800000000", APIKey: "Key1234567", TimeoutSeconds: 5},
		},
		{
			name:    "地址缺 scheme",
			cfg:     UpstreamConfig{BaseURL: "lyew.com", Username: "13800000000", APIKey: "Key1234567"},
			wantErr: "必须以 http:// 或 https:// 开头",
		},
		{
			name:    "超时越界",
			cfg:     UpstreamConfig{BaseURL: "https://lyew.com", TimeoutSeconds: MaxUpstreamTimeoutSeconds + 1},
			wantErr: "upstream.timeout_seconds",
		},
		{
			name:    "有密钥没账号",
			cfg:     UpstreamConfig{BaseURL: "https://lyew.com", APIKey: "Key1234567"},
			wantErr: "upstream.username 为空",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.validate()
			if tc.wantErr == "" {
				if len(err) != 0 {
					t.Fatalf("期望校验通过，实际: %v", err)
				}
				return
			}
			if len(err) == 0 {
				t.Fatalf("期望报错包含 %q，实际无错误", tc.wantErr)
			}
			if !strings.Contains(err[0].Error(), tc.wantErr) {
				t.Fatalf("错误 = %v, 期望包含 %q", err[0], tc.wantErr)
			}
		})
	}
}

func TestUpstreamMissingFieldsReportsPartialConfig(t *testing.T) {
	upstream := UpstreamConfig{BaseURL: "https://lyew.com"}
	missing := upstream.MissingFields()
	if len(missing) != 2 || missing[0] != "upstream.username" || missing[1] != "upstream.api_key" {
		t.Errorf("MissingFields() = %v, 期望 [upstream.username upstream.api_key]", missing)
	}
}

func TestLoadReadsUpstreamSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `server:
  addr: "127.0.0.1:9090"
  mode: "test"
database:
  dsn: "root:pass@tcp(127.0.0.1:3306)/lyidc?parseTime=true"
jwt:
  secret: "unit-test-secret"
  expire_hours: 24
log:
  level: "warn"
  format: "json"
upstream:
  base_url: "https://lyew.example.com/"
  username: "13800000000"
  api_key: "UnitTestKey12"
  timeout_seconds: 8
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入临时配置失败: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}

	if cfg.Upstream.BaseURL != "https://lyew.example.com/" {
		t.Errorf("base_url = %q, 期望原样读取（结尾斜杠由客户端自行裁剪）", cfg.Upstream.BaseURL)
	}
	if !cfg.Upstream.Enabled() {
		t.Errorf("完整配置应视为已启用，实际 = %+v", cfg.Upstream)
	}
	if got := cfg.Upstream.Timeout(); got != 8*time.Second {
		t.Errorf("Timeout() = %s, 期望 8s", got)
	}
}
