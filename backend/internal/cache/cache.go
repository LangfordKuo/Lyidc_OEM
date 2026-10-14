// Package cache 是缓存基础设施（契约第 24 节，R19 批 1）：
// 驱动抽象（Store）+ 两种驱动（文件 / Redis）+ 按后台设置构造与切换的 Manager（含统计与降级）。
//
// 本批**只做基础设施**：不接入任何业务读路径、不改变任何既有业务行为（契约 24.7 先把
// 「允许缓存 / 不缓存」与失效策略定稿，接线留批 2）。
//
// 三条硬边界（契约 24.6）：
//  1. **读写失败绝不让业务接口报错**：Get 把一切错误按「未命中」处理并记服务日志；
//     Set / Delete 返回错误只供调用方（批 2 的接线处）记录，调用方不得因此改变业务结果。
//  2. **统计不参与业务事务**：命中 / 未命中是进程内累加（重启归零、可重置），
//     统计读取失败不影响任何业务。
//  3. **只清自己的缓存**：Redis 驱动的 Flush 只删本 key_prefix 下的键，**绝不 FLUSHDB**
//     （同一个 DB 里可能有别的应用的数据）。
//
// 生效方式：Manager 按后台设置（settings 键 `cache`）构造驱动，设置改动由调用方（管理端接口）
// 翻译成 Config 后经 Configure 应用——**立即生效，不用重启**；配置未变且未降级时 Configure 无操作。
package cache

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// 驱动取值（与设置项 cache.driver 一一对应）。
const (
	// DriverFile 文件缓存（**默认**）：值按字节存文件，目录在系统临时目录下，无需任何配置。
	DriverFile = "file"
	// DriverRedis Redis 缓存（默认关闭，由管理员在后台配置启用）。
	DriverRedis = "redis"
)

// DefaultDirName 是文件缓存的缺省目录名（落在 os.TempDir() 下，如 <临时目录>/lyidc-cache）。
const DefaultDirName = "lyidc-cache"

// DefaultKeyPrefix 是 Redis 键前缀的缺省值（只作用于本系统写入的键）。
const DefaultKeyPrefix = "lyidc:"

// DefaultRedisPort 是 Redis 缺省端口（设置层未填端口时使用，与 settings.DefaultCacheRedisPort 一致）。
const DefaultRedisPort = 6379

// DefaultTimeout 是 Redis 连接 / 读写超时的缺省值：**短超时**，避免把请求拖住。
const DefaultTimeout = time.Second

// Store 是缓存驱动接口。两种驱动（文件 / Redis）都实现它。
//
// 约定：
//   - Get 的第三返回值是「驱动层错误」；Manager 会把错误按未命中处理并自行降级，
//     因此**业务层拿到的一定是 (value, found) 两个事实**；
//   - Set 的 ttl <= 0 表示**不过期**；ttl > 0 表示相对当前时间的过期时长；
//   - Flush 只清本驱动的缓存（Redis 只清 key_prefix 下的键）；
//   - Keys 返回当前键数量（驱动自己的口径：Redis 用 SCAN 计数、文件用目录计数）；
//   - Close 释放资源（文件驱动停清理协程、Redis 关连接池），可重复调用。
type Store interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	Flush(ctx context.Context) error
	Ping(ctx context.Context) error
	Keys(ctx context.Context) (int64, error)
	Close() error
}

// ServerVersioner 是可选接口：驱动能报告服务端版本时（Redis）由状态接口回显，如 `7.2.5`。
type ServerVersioner interface {
	ServerVersion(ctx context.Context) (string, error)
}

// Config 是缓存的运行时配置（由设置项 `cache` 翻译而来，见契约 24.3）。
type Config struct {
	// Driver 取 DriverFile / DriverRedis；空值按 DriverFile 处理。
	Driver string
	// Redis 是 Redis 驱动参数（Driver 为 file 时也能保存，只是不生效）。
	Redis RedisConfig
	// Dir 是文件缓存目录；空串 = <os.TempDir()>/lyidc-cache。
	Dir string
}

// RedisConfig 是 Redis 驱动参数（不含任何默认值推导，缺省值由设置层补齐）。
type RedisConfig struct {
	// Host 主机（不含端口；启用 Redis 时必填）。
	Host string
	// Port 端口。
	Port int
	// Password 认证口令（可空；只进内存与设置表，绝不落日志）。
	Password string
	// DB 数据库序号。
	DB int
	// KeyPrefix 键前缀（只作用于本系统写入的键，Flush 也只清该前缀）。
	KeyPrefix string
	// Timeout 连接 / 读写超时（短超时）。
	Timeout time.Duration
}

// withDefaults 补齐缺省值：驱动缺省 file、前缀缺省 lyidc:、超时缺省 1 秒、端口缺省 6379。
func (c Config) withDefaults() Config {
	if c.Driver == "" {
		c.Driver = DriverFile
	}
	if c.Redis.KeyPrefix == "" {
		c.Redis.KeyPrefix = DefaultKeyPrefix
	}
	if c.Redis.Timeout <= 0 {
		c.Redis.Timeout = DefaultTimeout
	}
	if c.Redis.Port == 0 {
		c.Redis.Port = DefaultRedisPort
	}
	return c
}

// DirOr 返回生效的文件缓存目录（未配置时为系统临时目录下的缺省目录）。
func (c Config) DirOr() string {
	if c.Dir != "" {
		return c.Dir
	}
	return filepath.Join(os.TempDir(), DefaultDirName)
}

// Addr 返回 Redis 的连接地址 `host:port`（仅用于日志与状态展示，不含口令）。
func (r RedisConfig) Addr() string { return net.JoinHostPort(r.Host, strconv.Itoa(r.Port)) }

// Stats 是进程内累计的命中统计（契约 24.4：重启归零、可由管理端重置）。
type Stats struct {
	// Hits / Misses 是累计命中与未命中次数（未命中含「不存在」「已过期」与「读失败按未命中处理」）。
	Hits   int64
	Misses int64
	// HitRate 是命中率（百分比，保留两位小数；无请求时为 0）。
	HitRate float64
	// Since 是统计起点（进程内首次累计或最近一次重置的时刻）。
	Since time.Time
}

// Status 是缓存的运行状态（契约 24.4 的状态接口数据面）。
type Status struct {
	// Driver 是**配置选择**的驱动；EffectiveDriver 是**实际生效**的驱动（降级时为 file）。
	Driver          string
	EffectiveDriver string
	// Degraded 表示已从 Redis 降级到文件缓存；Reason 是可读原因（如「连接被拒绝」）。
	Degraded       bool
	DegradedReason string
	// Keys 是当前生效驱动的键数量（统计口径见 Store.Keys）。
	Keys int64
	// Hits / Misses / HitRate / StatsSince 见 Stats。
	Hits       int64
	Misses     int64
	HitRate    float64
	StatsSince time.Time
	// FileDir 是文件缓存目录（无论当前是否生效都给出，便于管理员排障）。
	FileDir string
	// Redis 是 Redis 连接信息（不含口令）。
	Redis RedisStatus
}

// RedisStatus 是状态接口里的 Redis 连接信息（**不含口令**）。
type RedisStatus struct {
	Host      string
	Port      int
	DB        int
	KeyPrefix string
	Timeout   time.Duration
	// Version 是服务端版本（最近一次成功探活时取得）；空串表示未探到。
	Version string
	// Latency 是最近一次成功 PING 的往返耗时；0 表示未探到。
	Latency time.Duration
}

// Options 是 Manager 的构造参数。
type Options struct {
	Logger *slog.Logger
	// FileFactory / RedisFactory 是驱动工厂，**仅供测试注入**（默认构造真实的文件 / Redis 驱动）。
	// 注入后不连真实 Redis 也能覆盖降级、自愈与错误分类等分支。
	FileFactory  func(dir string) (Store, error)
	RedisFactory func(cfg RedisConfig) (Store, error)
	// CleanupInterval 是文件缓存清理过期文件的后台间隔，缺省 5 分钟（测试可缩短）。
	CleanupInterval time.Duration
}

// Manager 按设置构造与切换驱动，并承载降级、自愈与统计（契约 24.3 / 24.5）。
//
// 生命周期：Configure 应用配置（Redis 配置会做一次 PING 探活，失败即降级到文件缓存）；
// 运行期 Redis 出错时自动降级到文件缓存；Status 会重试 Redis（自愈），恢复可用即切回。
type Manager struct {
	logger *slog.Logger

	fileFactory  func(dir string) (Store, error)
	redisFactory func(cfg RedisConfig) (Store, error)

	// mu 保护下列字段；**驱动 I/O 不持锁**（Configure / Status 除外，它们本就是管理端低频路径）。
	mu         sync.RWMutex
	cfg        Config
	configured bool
	store      Store
	effective  string
	degraded   bool
	reason     string

	hits    atomic.Int64
	misses  atomic.Int64
	sinceMu sync.Mutex
	since   time.Time
}

// NewManager 构造缓存管理器。注意：构造本身不读设置也不连 Redis——
// 由调用方在读取设置后调用 Configure（管理端接口每次都读一次设置，与仓库既有的
// 「读时校验、按内容失效」口径一致，见契约 12.1）。
func NewManager(opts Options) *Manager {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	fileFactory := opts.FileFactory
	if fileFactory == nil {
		interval := opts.CleanupInterval
		if interval <= 0 {
			interval = DefaultCleanupInterval
		}
		fileFactory = func(dir string) (Store, error) { return newFileStore(dir, interval, logger) }
	}
	redisFactory := opts.RedisFactory
	if redisFactory == nil {
		redisFactory = func(cfg RedisConfig) (Store, error) { return newRedisStore(cfg) }
	}
	return &Manager{
		logger:       logger,
		fileFactory:  fileFactory,
		redisFactory: redisFactory,
		effective:    DriverFile,
		since:        time.Now(),
	}
}

// Configure 应用（或重试）配置：配置未变且未降级时**无操作**；
// 配置变化时重建驱动（Redis 会先做一次 PING 探活，失败即降级到文件缓存）；
// 处于降级且配置为 Redis 时重新构造 —— 这就是「下次读取设置」的自愈时机（契约 24.5）。
func (m *Manager) Configure(ctx context.Context, cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg = cfg.withDefaults()
	if !m.configured || cfg != m.cfg || m.degraded {
		m.applyLocked(ctx, cfg)
	}
}

// Get 读取缓存。**任何驱动错误都按「未命中」处理**（记服务日志，必要时降级），绝不向上抛错。
func (m *Manager) Get(ctx context.Context, key string) ([]byte, bool) {
	store := m.activeStore()
	if store == nil {
		m.record(false)
		return nil, false
	}
	value, found, err := store.Get(ctx, key)
	if err != nil {
		m.handleFailure(store, err)
		m.record(false)
		return nil, false
	}
	m.record(found)
	if !found {
		return nil, false
	}
	return value, true
}

// Set 写入缓存（ttl <= 0 表示不过期）。失败只记服务日志并返回错误；
// **调用方不得因缓存写失败改变业务结果**（契约 24.6）。
func (m *Manager) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	store := m.activeStore()
	if store == nil {
		return errNoStore
	}
	if err := store.Set(ctx, key, value, ttl); err != nil {
		m.handleFailure(store, err)
		return err
	}
	return nil
}

// Delete 删除缓存键（幂等：键不存在不算失败）。失败只记服务日志并返回错误。
func (m *Manager) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	store := m.activeStore()
	if store == nil {
		return errNoStore
	}
	if err := store.Delete(ctx, keys...); err != nil {
		m.handleFailure(store, err)
		return err
	}
	return nil
}

// Flush 清空缓存（管理端排障用，契约 24.4）：
//   - 清当前生效驱动；Redis 只清本 key_prefix 下的键，**绝不 FLUSHDB**；
//   - 降级期间（生效驱动是文件缓存）**额外尽力清一次配置的 Redis**，避免恢复后命中残留旧值；
//     这一步失败只记日志，不影响文件缓存的清空结果。
func (m *Manager) Flush(ctx context.Context) error {
	store := m.activeStore()
	if store == nil {
		return errNoStore
	}
	err := store.Flush(ctx)
	if err != nil {
		m.handleFailure(store, err)
	}

	m.mu.RLock()
	cfg := m.cfg
	effective := m.effective
	m.mu.RUnlock()
	if cfg.Driver == DriverRedis && effective != DriverRedis {
		if redis, buildErr := m.redisFactory(cfg.Redis); buildErr == nil {
			if flushErr := redis.Flush(ctx); flushErr != nil {
				m.logger.Warn("降级期间清空 Redis 缓存失败（不影响文件缓存）", "error", flushErr)
			}
			_ = redis.Close()
		}
	}
	return err
}

// Stats 返回进程内累计的命中统计（契约 24.4：重启归零、可重置）。
func (m *Manager) Stats() Stats {
	hits := m.hits.Load()
	misses := m.misses.Load()
	m.sinceMu.Lock()
	since := m.since
	m.sinceMu.Unlock()
	return Stats{Hits: hits, Misses: misses, HitRate: hitRate(hits, misses), Since: since}
}

// ResetStats 归零命中统计（管理端接口，契约 24.4）。
func (m *Manager) ResetStats() {
	m.hits.Store(0)
	m.misses.Store(0)
	m.sinceMu.Lock()
	m.since = time.Now()
	m.sinceMu.Unlock()
}

// Status 返回运行状态：先做一次探活——降级期间**重试 Redis（自愈）**，正常时 PING 校验当前驱动
// （失败即降级，契约 24.5）。因此本方法只用于管理端状态接口，**不要在业务热路径上调用**。
func (m *Manager) Status(ctx context.Context) Status {
	m.mu.Lock()
	switch {
	case !m.configured:
		m.applyLocked(ctx, Config{})
	case m.degraded && m.cfg.Driver == DriverRedis:
		// 自愈：降级期间再次探活 Redis，恢复可用就切回去。
		m.applyLocked(ctx, m.cfg)
	case m.store != nil:
		if err := m.store.Ping(ctx); err != nil {
			m.failoverLocked(err)
		}
	}
	status := m.statusLocked(ctx)
	m.mu.Unlock()
	return status
}

// Close 关闭当前驱动（文件驱动停清理协程、Redis 关连接池）；可重复调用。
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var err error
	if m.store != nil {
		err = m.store.Close()
		m.store = nil
	}
	m.configured = false
	return err
}

// errNoStore 表示当前没有任何可用驱动（文件缓存目录都建不出来时的极端情形）。
var errNoStore = errors.New("缓存驱动不可用")

// activeStore 取当前生效的驱动（可能因降级是文件缓存；未配置时返回 nil）。
func (m *Manager) activeStore() Store {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store
}

// record 累计一次命中 / 未命中。
func (m *Manager) record(hit bool) {
	if hit {
		m.hits.Add(1)
	} else {
		m.misses.Add(1)
	}
}

// handleFailure 处理运行期驱动错误（幂等，先到先得）：
//   - 调用方主动取消（如 HTTP 请求断开）不是驱动故障，不降级；
//   - **只有 Redis 驱动出错才降级到文件缓存**（契约 24.5 的降级口径）；
//     文件缓存自身出错只记服务日志（按未命中处理，业务不受影响，也没有可退的地方）。
func (m *Manager) handleFailure(failed Store, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store != failed {
		return // 已被其它并发请求降级过
	}
	if m.effective != DriverRedis {
		m.logger.Warn("文件缓存操作失败（按未命中处理，业务不受影响）", "error", err)
		return
	}
	m.failoverLocked(err)
}

// failoverLocked 降级到文件缓存并记录可读原因（须持有写锁）。
func (m *Manager) failoverLocked(err error) {
	m.degraded = true
	m.reason = describeStoreError(err, m.cfg)
	m.logger.Warn("缓存驱动不可用，已降级到文件缓存",
		"driver", m.cfg.Driver, "reason", m.reason, "error", err)
	if m.store != nil {
		_ = m.store.Close()
		m.store = nil
	}
	m.effective = DriverFile
	store, buildErr := m.fileFactory(m.cfg.DirOr())
	if buildErr != nil {
		m.logger.Error("文件缓存同样不可用，缓存层停用（业务不受影响）", "dir", m.cfg.DirOr(), "error", buildErr)
		return
	}
	m.store = store
}

// applyLocked 按配置重建驱动（须持有写锁）：Redis 先构造再 PING，探活失败即降级到文件缓存。
func (m *Manager) applyLocked(ctx context.Context, cfg Config) {
	if m.store != nil {
		_ = m.store.Close()
		m.store = nil
	}
	m.cfg = cfg
	m.configured = true
	m.degraded = false
	m.reason = ""
	m.effective = cfg.Driver

	if cfg.Driver != DriverRedis {
		store, err := m.fileFactory(cfg.DirOr())
		if err != nil {
			// 文件缓存都建不起来：缓存层停用（Get 一律未命中、Set 返回错误），业务不受影响。
			m.logger.Error("文件缓存不可用，缓存层停用（业务不受影响）", "dir", cfg.DirOr(), "error", err)
			m.degraded = true
			m.reason = "文件缓存目录不可用"
			return
		}
		m.store = store
		return
	}

	store, err := m.redisFactory(cfg.Redis)
	if err == nil {
		probeCtx, cancel := context.WithTimeout(ctx, cfg.Redis.Timeout)
		err = store.Ping(probeCtx)
		cancel()
	}
	if err == nil {
		m.store = store
		return
	}
	// Redis 不可用：标红 + 自动降级到文件缓存（业务不中断），原因可读（契约 24.5）。
	if store != nil {
		_ = store.Close()
	}
	m.degraded = true
	m.reason = describeStoreError(err, cfg)
	m.logger.Warn("Redis 缓存不可用，已自动降级到文件缓存",
		"addr", cfg.Redis.Addr(), "reason", m.reason, "error", err)
	m.effective = DriverFile
	fileStore, buildErr := m.fileFactory(cfg.DirOr())
	if buildErr != nil {
		m.logger.Error("文件缓存同样不可用，缓存层停用（业务不受影响）", "dir", cfg.DirOr(), "error", buildErr)
		return
	}
	m.store = fileStore
}

// statusLocked 组装状态视图（须持有写锁；会做键数量与 Redis 版本探测）。
func (m *Manager) statusLocked(ctx context.Context) Status {
	stats := m.Stats()
	status := Status{
		Driver:          m.cfg.Driver,
		EffectiveDriver: m.effective,
		Degraded:        m.degraded,
		DegradedReason:  m.reason,
		Hits:            stats.Hits,
		Misses:          stats.Misses,
		HitRate:         stats.HitRate,
		StatsSince:      stats.Since,
		FileDir:         m.cfg.DirOr(),
		Redis: RedisStatus{
			Host:      m.cfg.Redis.Host,
			Port:      m.cfg.Redis.Port,
			DB:        m.cfg.Redis.DB,
			KeyPrefix: m.cfg.Redis.KeyPrefix,
			Timeout:   m.cfg.Redis.Timeout,
		},
	}
	if m.store != nil {
		keys, err := m.store.Keys(ctx)
		if err != nil {
			// 统计不参与业务：键数量取不到按 0 计并记日志（状态接口本身仍然成功）。
			m.logger.Warn("读取缓存键数量失败", "error", err)
		} else {
			status.Keys = keys
		}
	}
	if m.effective == DriverRedis && m.store != nil {
		start := time.Now()
		if err := m.store.Ping(ctx); err == nil {
			status.Redis.Latency = time.Since(start)
			if versioner, ok := m.store.(ServerVersioner); ok {
				if version, err := versioner.ServerVersion(ctx); err == nil {
					status.Redis.Version = version
				}
			}
		}
	}
	return status
}

// hitRate 计算命中率（百分比，保留两位小数）；没有请求时返回 0。
func hitRate(hits, misses int64) float64 {
	total := hits + misses
	if total <= 0 {
		return 0
	}
	return math.Round(float64(hits)/float64(total)*10000) / 100
}

// describeStoreError 把驱动错误翻译成可读的中文降级原因（契约 24.5：如「连接被拒绝」「认证失败」「超时」）。
// 分类只对 Redis 驱动做（降级本就是 Redis 的口径）；其它情况直接给原始错误文本。
func describeStoreError(err error, cfg Config) string {
	if cfg.Driver != DriverRedis {
		return err.Error()
	}
	reason := describeRedisError(err)
	if cfg.Redis.Host == "" {
		return reason
	}
	return reason + "（" + cfg.Redis.Addr() + "）"
}
