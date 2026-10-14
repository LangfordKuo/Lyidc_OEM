package settings

import (
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// cache 设置（R19 批 1，契约 24.3）：缺省口径、三态口令、校验边界
// ---------------------------------------------------------------------------

// TestDecodeCacheDefaults 覆盖缺省口径：未配置 = 文件缓存（Redis 默认关闭）+ Redis 参数缺省。
func TestDecodeCacheDefaults(t *testing.T) {
	for _, raw := range []string{"", "   ", "null"} {
		value, err := decodeCache(raw)
		if err != nil {
			t.Fatalf("decodeCache(%q) 失败: %v", raw, err)
		}
		if value.NormalizedDriver() != CacheDriverFile || value.RedisEnabled() {
			t.Fatalf("缺省应是文件缓存且 Redis 未启用: %+v", value)
		}
		if value.Redis.Host != "" || value.PortOr() != DefaultCacheRedisPort {
			t.Fatalf("Redis 参数应取缺省（空 host + 端口 6379）: %+v", value.Redis)
		}
		if value.KeyPrefixOr() != DefaultCacheKeyPrefix || value.TimeoutSecondsOr() != DefaultCacheTimeoutSeconds {
			t.Fatalf("键前缀与超时应取缺省: %+v", value.Redis)
		}
	}
}

// TestCacheApplyAndThreeStatePassword 覆盖合并语义（含口令三态）。
func TestCacheApplyAndThreeStatePassword(t *testing.T) {
	current := Cache{
		Driver: CacheDriverRedis,
		Redis: CacheRedis{
			Host: "127.0.0.1", Port: 6380, Password: "old-pass", DB: 2,
			KeyPrefix: "app:", TimeoutSeconds: 3,
		},
	}

	// 省略 = 全部保持不变。
	kept, err := current.Apply(CacheUpdate{})
	if err != nil || kept != current {
		t.Fatalf("省略字段应保持不变: %+v / %v", kept, err)
	}

	// 给新值 = 替换（口令不做 TrimSpace；host / 前缀做 TrimSpace）。
	newPassword := " new-pass "
	host := " redis.internal "
	prefix := " cache: "
	merged, err := current.Apply(CacheUpdate{
		Driver: strPtr(CacheDriverFile),
		Redis:  &CacheRedisUpdate{Host: &host, Password: &newPassword, KeyPrefix: &prefix},
	})
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	if merged.NormalizedDriver() != CacheDriverFile {
		t.Fatalf("driver 应被替换为 file: %+v", merged)
	}
	if merged.Redis.Password != " new-pass " {
		t.Fatalf("口令不应被 TrimSpace: %q", merged.Redis.Password)
	}
	if merged.Redis.Host != "redis.internal" || merged.Redis.KeyPrefix != "cache:" {
		t.Fatalf("host / key_prefix 应去首尾空白: %+v", merged.Redis)
	}
	if merged.Redis.Port != 6380 || merged.Redis.DB != 2 || merged.Redis.TimeoutSeconds != 3 {
		t.Fatalf("未提供的字段应保持原值: %+v", merged.Redis)
	}

	// 空串 = 清空口令（其余字段不动）。
	empty := ""
	cleared, err := current.Apply(CacheUpdate{Redis: &CacheRedisUpdate{Password: &empty}})
	if err != nil || cleared.Redis.Password != "" {
		t.Fatalf("空串应清空口令: %+v / %v", cleared.Redis, err)
	}
	if cleared.Redis.Host != "127.0.0.1" {
		t.Fatalf("清空口令不应影响其它字段: %+v", cleared.Redis)
	}
}

// TestCacheValidateRejects 覆盖校验边界（契约 24.3：一切非法值都拒绝且不落库）。
func TestCacheValidateRejects(t *testing.T) {
	base := defaultCache()
	base.Driver = CacheDriverRedis
	base.Redis.Host = "127.0.0.1"
	if err := base.Validate(); err != nil {
		t.Fatalf("合法配置不应被拒: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Cache)
		hint   string // 可读原因里应包含的关键字
	}{
		{"驱动取值非法", func(c *Cache) { c.Driver = "memcached" }, "driver"},
		{"启用 Redis 但 host 为空", func(c *Cache) { c.Redis.Host = "" }, "host"},
		{"端口越界（0 以上）", func(c *Cache) { c.Redis.Port = 70000 }, "port"},
		{"端口为负", func(c *Cache) { c.Redis.Port = -1 }, "port"},
		{"数据库序号越界", func(c *Cache) { c.Redis.DB = 16 }, "db"},
		{"数据库序号为负", func(c *Cache) { c.Redis.DB = -1 }, "db"},
		{"超时越界（上限）", func(c *Cache) { c.Redis.TimeoutSeconds = 11 }, "timeout_seconds"},
		{"超时为负", func(c *Cache) { c.Redis.TimeoutSeconds = -1 }, "timeout_seconds"},
		{"键前缀过长", func(c *Cache) { c.Redis.KeyPrefix = strings.Repeat("p", 65) }, "key_prefix"},
		{"键前缀含控制字符", func(c *Cache) { c.Redis.KeyPrefix = "bad\nprefix" }, "key_prefix"},
		{"host 含控制字符", func(c *Cache) { c.Redis.Host = "bad\x00host" }, "host"},
	}
	for _, tc := range cases {
		value := defaultCache()
		value.Driver = CacheDriverRedis
		value.Redis.Host = "127.0.0.1"
		tc.mutate(&value)
		err := value.Validate()
		if !errors.Is(err, ErrRule) {
			t.Fatalf("%s：应返回 ErrRule（40002），实际 %v", tc.name, err)
		}
		if !strings.Contains(err.Error(), tc.hint) {
			t.Fatalf("%s：原因应包含 %q，实际 %q", tc.name, tc.hint, err.Error())
		}
	}
}

// TestCacheValidateAllowsFileDriverWithoutHost 覆盖「文件驱动不必填 Redis 参数」：
// driver=file 时 host 可以为空（Redis 参数只是原样保存）。
func TestCacheValidateAllowsFileDriverWithoutHost(t *testing.T) {
	value := Cache{Driver: CacheDriverFile}
	if err := value.Validate(); err != nil {
		t.Fatalf("文件驱动的空 Redis 参数应合法: %v", err)
	}
}

// TestCacheEncodeRoundTrip 覆盖落库 JSON 的往返（字段顺序固定、内容可比较）。
func TestCacheEncodeRoundTrip(t *testing.T) {
	value := Cache{
		Driver: CacheDriverRedis,
		Redis: CacheRedis{
			Host: "redis.example.com", Port: 6380, Password: "p@ss", DB: 4,
			KeyPrefix: "oem:", TimeoutSeconds: 2,
		},
	}
	encoded, err := value.Encode()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	decoded, err := decodeCache(encoded)
	if err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if decoded != value {
		t.Fatalf("往返不一致: %+v != %+v", decoded, value)
	}
}

// TestDecodeCacheRejectsCorrupt 覆盖库内值损坏（只可能是绕过接口手工改库）。
func TestDecodeCacheRejectsCorrupt(t *testing.T) {
	for _, raw := range []string{
		"{不是 JSON",
		`{"driver":"memcached"}`,
		`{"driver":"redis","redis":{"host":"h","port":70000}}`,
		`{"driver":"redis","redis":{"host":"h","timeout_seconds":99}}`,
	} {
		if _, err := decodeCache(raw); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("decodeCache(%q) 应返回 ErrCorrupt，实际 %v", raw, err)
		}
	}
}

// TestCacheDecodeNormalizesStoredValues 覆盖库内旧值的归一化（缺字段补缺省、大小写归一）。
func TestCacheDecodeNormalizesStoredValues(t *testing.T) {
	value, err := decodeCache(`{"driver":"REDIS","redis":{"host":" 127.0.0.1 "}}`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if value.NormalizedDriver() != CacheDriverRedis || !value.RedisEnabled() {
		t.Fatalf("驱动应大小写不敏感地归一为 redis: %+v", value)
	}
	if value.Redis.Host != "127.0.0.1" {
		t.Fatalf("host 应去首尾空白: %q", value.Redis.Host)
	}
	if value.PortOr() != DefaultCacheRedisPort || value.TimeoutSecondsOr() != DefaultCacheTimeoutSeconds {
		t.Fatalf("缺省参数应补齐: %+v", value.Redis)
	}
}

// TestCacheHelpers 覆盖取值辅助函数。
func TestCacheHelpers(t *testing.T) {
	value := Cache{Driver: "  ", Redis: CacheRedis{Port: 0, KeyPrefix: "", TimeoutSeconds: 0}}
	if value.NormalizedDriver() != CacheDriverFile || value.RedisEnabled() {
		t.Fatalf("空驱动应按文件缓存: %+v", value)
	}
	if value.PortOr() != 6379 || value.KeyPrefixOr() != "lyidc:" || value.TimeoutSecondsOr() != 1 {
		t.Fatalf("缺省取值错误: %+v", value)
	}
}

// strPtr 返回字符串指针（构造更新意图用）。
func strPtr(value string) *string { return &value }
