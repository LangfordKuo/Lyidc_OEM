package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// KeyCache 是缓存设置的键（R19，契约 24.3）：驱动选择 + Redis 参数。
//
// 取值与校验在本文件；运行时（构造驱动、降级、统计）在 internal/cache，
// 由路由层把本结构映射成 cache.Config（避免本包反向依赖 cache 包）。
const KeyCache = "cache"

// 缓存驱动取值（cache.driver）。
const (
	// CacheDriverFile 文件缓存（**默认**，无需配置，目录在系统临时目录下）。
	CacheDriverFile = "file"
	// CacheDriverRedis Redis 缓存（默认关闭，由管理员在后台配置启用）。
	CacheDriverRedis = "redis"
)

// CacheDrivers 是驱动全集（顺序即文档与错误提示的展示顺序）。
var CacheDrivers = []string{CacheDriverFile, CacheDriverRedis}

// Redis 参数缺省值与边界（契约 24.3）。
const (
	// DefaultCacheRedisPort Redis 缺省端口（与 internal/cache.DefaultRedisPort 保持一致）。
	DefaultCacheRedisPort = 6379
	// DefaultCacheRedisDB 缺省数据库序号。
	DefaultCacheRedisDB = 0
	// MaxCacheRedisDB 数据库序号上限（Redis 缺省 databases=16）。
	MaxCacheRedisDB = 15
	// DefaultCacheKeyPrefix 缺省键前缀（只作用于本系统写入的键）。
	DefaultCacheKeyPrefix = "lyidc:"
	// MaxCacheKeyPrefixLength 键前缀长度上限（字符数）。
	MaxCacheKeyPrefixLength = 64
	// DefaultCacheTimeoutSeconds 缺省超时（秒）：短超时，避免把请求拖住。
	DefaultCacheTimeoutSeconds = 1
	// MinCacheTimeoutSeconds 超时下限（秒）。
	MinCacheTimeoutSeconds = 1
	// MaxCacheTimeoutSeconds 超时上限（秒）。
	MaxCacheTimeoutSeconds = 10
	// MaxCacheRedisHostLength 主机名长度上限（字符数）。
	MaxCacheRedisHostLength = 255
)

// Cache 是 cache 设置的取值结构（缓存基础设施，R19 批 1，契约 24.3）。
//
// 默认口径：**文件缓存**（driver=file，无需任何参数）；Redis **默认关闭**，
// 由管理员在后台选择 driver=redis 并填 host 等参数后启用。
//
// Password 是 Redis 认证口令，只进数据库与内存，对外一律掩码（MaskSecret），
// 三态语义与其他密钥一致（省略 = 保持、新值 = 替换、空串 = 清空）。
type Cache struct {
	// Driver 取 file / redis；空值按 file 处理。
	Driver string `json:"driver"`
	// Redis 是 Redis 驱动参数（driver=file 时也原样保存，只是不生效）。
	Redis CacheRedis `json:"redis"`
}

// CacheRedis 是 Redis 驱动参数。
type CacheRedis struct {
	// Host 主机（不含端口）；driver=redis 时必填。
	Host string `json:"host"`
	// Port 端口，1-65535；0 表示缺省 6379。
	Port int `json:"port"`
	// Password 认证口令（可空表示不认证）。
	Password string `json:"password"`
	// DB 数据库序号，0-15。
	DB int `json:"db"`
	// KeyPrefix 键前缀（只作用于本系统写入的键）；空值按缺省 lyidc: 处理。
	KeyPrefix string `json:"key_prefix"`
	// TimeoutSeconds 连接 / 读写超时（秒），1-10，缺省 1。
	TimeoutSeconds int `json:"timeout_seconds"`
}

// CacheState 是 cache 的读取结果。
type CacheState struct {
	Value Cache
	Meta  Meta
}

// CacheRedisUpdate 是 Redis 参数的局部更新意图：nil 表示不修改；
// Password 三态（省略 = 保持、新值 = 替换、空串 = 清空）。
type CacheRedisUpdate struct {
	Host           *string
	Port           *int
	Password       *string
	DB             *int
	KeyPrefix      *string
	TimeoutSeconds *int
}

// CacheUpdate 是 PUT /api/v1/admin/settings/cache 的更新意图：nil 表示不修改。
type CacheUpdate struct {
	Driver *string
	Redis  *CacheRedisUpdate
}

// NormalizedDriver 返回归一化后的驱动（大小写与空白不敏感；空值按 file）。
func (c Cache) NormalizedDriver() string {
	value := strings.ToLower(strings.TrimSpace(c.Driver))
	if value == "" {
		return CacheDriverFile
	}
	return value
}

// RedisEnabled 表示当前是否选择启用 Redis（等价于 driver=redis，供界面展示「启用状态」）。
func (c Cache) RedisEnabled() bool { return c.NormalizedDriver() == CacheDriverRedis }

// PortOr 返回生效的 Redis 端口（未填时取缺省 6379）。
func (c Cache) PortOr() int {
	if c.Redis.Port > 0 {
		return c.Redis.Port
	}
	return DefaultCacheRedisPort
}

// KeyPrefixOr 返回生效的键前缀（空值取缺省 lyidc:）。
func (c Cache) KeyPrefixOr() string {
	if c.Redis.KeyPrefix == "" {
		return DefaultCacheKeyPrefix
	}
	return c.Redis.KeyPrefix
}

// TimeoutSecondsOr 返回生效的超时秒数（<=0 时取缺省 1）。
func (c Cache) TimeoutSecondsOr() int {
	if c.Redis.TimeoutSeconds <= 0 {
		return DefaultCacheTimeoutSeconds
	}
	return c.Redis.TimeoutSeconds
}

// Apply 把更新意图合并到当前值，并对合并结果做整体校验。
// 校验失败返回 ErrRule（上层映射 40002，契约 24.3），此时设置保持不变。
func (c Cache) Apply(upd CacheUpdate) (Cache, error) {
	merged := c
	if upd.Driver != nil {
		merged.Driver = strings.ToLower(strings.TrimSpace(*upd.Driver))
	}
	if upd.Redis != nil {
		if upd.Redis.Host != nil {
			merged.Redis.Host = strings.TrimSpace(*upd.Redis.Host)
		}
		if upd.Redis.Port != nil {
			merged.Redis.Port = *upd.Redis.Port
		}
		if upd.Redis.Password != nil {
			// 口令不做 TrimSpace：首尾空白可能是口令的一部分（与支付密钥同口径）。
			merged.Redis.Password = *upd.Redis.Password
		}
		if upd.Redis.DB != nil {
			merged.Redis.DB = *upd.Redis.DB
		}
		if upd.Redis.KeyPrefix != nil {
			merged.Redis.KeyPrefix = strings.TrimSpace(*upd.Redis.KeyPrefix)
		}
		if upd.Redis.TimeoutSeconds != nil {
			merged.Redis.TimeoutSeconds = *upd.Redis.TimeoutSeconds
		}
	}
	if err := merged.Validate(); err != nil {
		return Cache{}, err
	}
	return merged, nil
}

// Validate 校验取值（契约 24.3：非法一律拒绝且不落库）。
//
// 校验项：驱动取值、Redis 主机（启用 Redis 时必填）、端口范围、数据库序号范围、
// 键前缀形状、超时范围。全部按 **ErrRule（40002）** 返回——这是后台设置项的规则校验，
// 不是请求体类型错误（类型错误在绑定阶段就以 40001 拒绝）。
func (c Cache) Validate() error {
	driver := c.NormalizedDriver()
	if driver != CacheDriverFile && driver != CacheDriverRedis {
		return fmt.Errorf("%w: driver 只能是 %s，收到 %q",
			ErrRule, strings.Join(CacheDrivers, " / "), c.Driver)
	}
	host := c.Redis.Host
	if len([]rune(host)) > MaxCacheRedisHostLength {
		return fmt.Errorf("%w: redis.host 不能超过 %d 个字符", ErrRule, MaxCacheRedisHostLength)
	}
	if strings.ContainsFunc(host, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("%w: redis.host 不能包含控制字符", ErrRule)
	}
	if c.Redis.Port < 0 || c.Redis.Port > 65535 {
		return fmt.Errorf("%w: redis.port 需在 1-65535 之间（留空取缺省 %d），收到 %d",
			ErrRule, DefaultCacheRedisPort, c.Redis.Port)
	}
	if c.Redis.DB < 0 || c.Redis.DB > MaxCacheRedisDB {
		return fmt.Errorf("%w: redis.db 需在 0-%d 之间，收到 %d", ErrRule, MaxCacheRedisDB, c.Redis.DB)
	}
	if prefix := c.Redis.KeyPrefix; len([]rune(prefix)) > MaxCacheKeyPrefixLength {
		return fmt.Errorf("%w: redis.key_prefix 不能超过 %d 个字符", ErrRule, MaxCacheKeyPrefixLength)
	}
	if strings.ContainsFunc(c.Redis.KeyPrefix, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("%w: redis.key_prefix 不能包含控制字符", ErrRule)
	}
	if timeout := c.Redis.TimeoutSeconds; timeout != 0 &&
		(timeout < MinCacheTimeoutSeconds || timeout > MaxCacheTimeoutSeconds) {
		return fmt.Errorf("%w: redis.timeout_seconds 需在 %d-%d 之间（留空取缺省 %d），收到 %d",
			ErrRule, MinCacheTimeoutSeconds, MaxCacheTimeoutSeconds, DefaultCacheTimeoutSeconds, timeout)
	}
	if driver == CacheDriverRedis && host == "" {
		return fmt.Errorf("%w: 启用 Redis 时必须填写 redis.host", ErrRule)
	}
	return nil
}

// Encode 返回落库用的 JSON 文本。
func (c Cache) Encode() (string, error) { return encode(c) }

// Cache 读取 cache 设置；未配置时返回缺省值（文件缓存 + Redis 参数缺省）。
func (r *Reader) Cache(ctx context.Context) (CacheState, error) {
	raw, meta, err := r.raw(ctx, KeyCache)
	if err != nil {
		return CacheState{}, err
	}
	value, err := decodeCache(raw)
	if err != nil {
		return CacheState{}, err
	}
	return CacheState{Value: value, Meta: meta}, nil
}

// decodeCache 解析库内 JSON；空值按未配置处理（全部取缺省）。
func decodeCache(raw string) (Cache, error) {
	value := defaultCache()
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return value, nil
	}
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return Cache{}, fmt.Errorf("%w: %s 的值不是合法 JSON：%v", ErrCorrupt, KeyCache, err)
	}
	value.Driver = value.NormalizedDriver()
	value.Redis.Host = strings.TrimSpace(value.Redis.Host)
	value.Redis.KeyPrefix = strings.TrimSpace(value.Redis.KeyPrefix)
	if value.Redis.Port == 0 {
		value.Redis.Port = DefaultCacheRedisPort
	}
	if value.Redis.TimeoutSeconds == 0 {
		value.Redis.TimeoutSeconds = DefaultCacheTimeoutSeconds
	}
	if err := value.Validate(); err != nil {
		return Cache{}, fmt.Errorf("%w: %s 的取值不合法：%v", ErrCorrupt, KeyCache, err)
	}
	return value, nil
}

// defaultCache 返回未配置时的缺省值：文件缓存 + Redis 参数缺省（Redis 默认关闭）。
func defaultCache() Cache {
	return Cache{
		Driver: CacheDriverFile,
		Redis: CacheRedis{
			Port:           DefaultCacheRedisPort,
			DB:             DefaultCacheRedisDB,
			KeyPrefix:      DefaultCacheKeyPrefix,
			TimeoutSeconds: DefaultCacheTimeoutSeconds,
		},
	}
}
