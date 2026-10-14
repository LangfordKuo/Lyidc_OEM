package cache

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// ---------------------------------------------------------------------------
// Manager（契约 24.3 / 24.4 / 24.5）：驱动切换、降级与自愈、统计、清空口径、Redis 分支
//
// Redis 的「可用」分支用 github.com/alicebob/miniredis/v2（进程内 RESP 服务，不装真 Redis）；
// 「不可用」分支用真实不可达地址（连接被拒绝）与注入的假驱动（超时 / 认证类错误）。
// ---------------------------------------------------------------------------

// stubStore 是可注入的假驱动：用于覆盖真实驱动难以稳定构造的失败形态。
type stubStore struct {
	pingErr   error
	getErr    error
	setErr    error
	closeCall int
}

func (s *stubStore) Get(context.Context, string) ([]byte, bool, error) {
	if s.getErr != nil {
		return nil, false, s.getErr
	}
	return nil, false, nil
}

func (s *stubStore) Set(context.Context, string, []byte, time.Duration) error { return s.setErr }
func (s *stubStore) Delete(context.Context, ...string) error                  { return nil }
func (s *stubStore) Flush(context.Context) error                              { return nil }
func (s *stubStore) Ping(context.Context) error                               { return s.pingErr }
func (s *stubStore) Keys(context.Context) (int64, error)                      { return 0, nil }
func (s *stubStore) Close() error                                             { s.closeCall++; return nil }

// redisConfigFor 把 miniredis 的监听地址翻译成 RedisConfig（默认前缀）。
func redisConfigFor(t *testing.T, server *miniredis.Miniredis) RedisConfig {
	t.Helper()
	host, port := hostPortOf(t, server.Addr())
	return RedisConfig{Host: host, Port: port, KeyPrefix: DefaultKeyPrefix, Timeout: 500 * time.Millisecond}
}

// hostPortOf 拆分 host:port。
func hostPortOf(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("拆分地址 %q 失败: %v", addr, err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatalf("解析端口 %q 失败: %v", portText, err)
	}
	return host, port
}

// mustPort 取地址里的端口。
func mustPort(t *testing.T, addr string) int {
	t.Helper()
	_, port := hostPortOf(t, addr)
	return port
}

// freePort 占一个临时端口后立刻释放：该端口上没有任何服务（连接会立即被拒绝）。
// 注意：释放到再次使用之间有极小的被抢占窗口，测试里可接受。
func freePort(t *testing.T) (string, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("占位监听失败: %v", err)
	}
	host, port := hostPortOf(t, listener.Addr().String())
	_ = listener.Close()
	return host, port
}

// startMiniRedis 起一个进程内 Redis（测试结束自动关闭）。
func startMiniRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	return miniredis.RunT(t)
}

// newTestManager 构造带静默日志的管理器（测试结束自动关闭）。
func newTestManager(t *testing.T, opts Options) *Manager {
	t.Helper()
	if opts.Logger == nil {
		opts.Logger = silentTestLogger()
	}
	if opts.CleanupInterval == 0 {
		opts.CleanupInterval = time.Hour
	}
	manager := NewManager(opts)
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}

// TestManagerFileDriverReadWriteAndStats 覆盖默认文件驱动的读写与命中统计。
func TestManagerFileDriverReadWriteAndStats(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	manager := newTestManager(t, Options{})
	manager.Configure(ctx, Config{Driver: DriverFile, Dir: dir})

	status := manager.Status(ctx)
	if status.Driver != DriverFile || status.EffectiveDriver != DriverFile || status.Degraded {
		t.Fatalf("默认应为文件缓存且不降级: %+v", status)
	}
	if status.FileDir != dir {
		t.Fatalf("文件目录应为配置值 %q，得到 %q", dir, status.FileDir)
	}

	// 未命中一次 → misses=1
	if _, found := manager.Get(ctx, "missing"); found {
		t.Fatal("未写入的键不应命中")
	}
	if err := manager.Set(ctx, "key", []byte("值"), time.Minute); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	// 命中两次 → hits=2
	for i := 0; i < 2; i++ {
		value, found := manager.Get(ctx, "key")
		if !found || string(value) != "值" {
			t.Fatalf("读回失败: found=%v, value=%q", found, value)
		}
	}

	stats := manager.Stats()
	if stats.Hits != 2 || stats.Misses != 1 {
		t.Fatalf("统计应为 2 命中 / 1 未命中，得到 %+v", stats)
	}
	if stats.HitRate != 66.67 {
		t.Fatalf("命中率应为 66.67，得到 %v", stats.HitRate)
	}
	if status = manager.Status(ctx); status.Keys != 1 {
		t.Fatalf("键数量应为 1，得到 %d", status.Keys)
	}

	// 删除后读不到（并再记一次未命中）。
	if err := manager.Delete(ctx, "key"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, found := manager.Get(ctx, "key"); found {
		t.Fatal("删除后不应命中")
	}
	if stats = manager.Stats(); stats.Misses != 2 {
		t.Fatalf("未命中应为 2，得到 %+v", stats)
	}
}

// TestManagerResetStats 覆盖统计重置（契约 24.4）。
func TestManagerResetStats(t *testing.T) {
	ctx := context.Background()
	manager := newTestManager(t, Options{})
	manager.Configure(ctx, Config{Driver: DriverFile, Dir: t.TempDir()})

	if err := manager.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	manager.Get(ctx, "k")
	manager.Get(ctx, "nope")
	before := manager.Stats()
	if before.Hits != 1 || before.Misses != 1 {
		t.Fatalf("重置前统计错误: %+v", before)
	}

	manager.ResetStats()
	after := manager.Stats()
	if after.Hits != 0 || after.Misses != 0 || after.HitRate != 0 {
		t.Fatalf("重置后应全部归零: %+v", after)
	}
	if after.Since.Before(before.Since) {
		t.Fatalf("重置应刷新统计起点: before=%v, after=%v", before.Since, after.Since)
	}
	if manager.Status(ctx).Keys != 1 {
		t.Fatal("重置统计不应影响缓存内容")
	}
}

// TestHitRateCalculation 覆盖命中率计算（含四舍五入与 0 请求）。
func TestHitRateCalculation(t *testing.T) {
	cases := []struct {
		hits, misses int64
		want         float64
	}{
		{0, 0, 0},
		{1, 0, 100},
		{0, 1, 0},
		{1, 2, 33.33},
		{2, 1, 66.67},
		{7, 3, 70},
	}
	for _, tc := range cases {
		if got := hitRate(tc.hits, tc.misses); got != tc.want {
			t.Fatalf("hitRate(%d, %d) = %v，期望 %v", tc.hits, tc.misses, got, tc.want)
		}
	}
}

// TestManagerSwitchToRedisAndBack 覆盖运行时切换（契约 24.1：改完立即生效、不用重启）。
func TestManagerSwitchToRedisAndBack(t *testing.T) {
	ctx := context.Background()
	server := startMiniRedis(t)
	dir := t.TempDir()
	manager := newTestManager(t, Options{})

	manager.Configure(ctx, Config{Driver: DriverFile, Dir: dir})
	if err := manager.Set(ctx, "shared", []byte("文件值"), 0); err != nil {
		t.Fatalf("文件驱动写入失败: %v", err)
	}

	manager.Configure(ctx, Config{Driver: DriverRedis, Dir: dir, Redis: redisConfigFor(t, server)})
	status := manager.Status(ctx)
	if status.EffectiveDriver != DriverRedis || status.Degraded {
		t.Fatalf("切换后应生效 Redis: %+v", status)
	}
	if _, found := manager.Get(ctx, "shared"); found {
		t.Fatal("两套驱动的数据互不相通：Redis 里不应有文件驱动写的键")
	}
	if err := manager.Set(ctx, "shared", []byte("Redis 值"), 0); err != nil {
		t.Fatalf("Redis 写入失败: %v", err)
	}
	// 值确实落在 miniredis 里，且带上了键前缀。
	if got, err := server.Get(DefaultKeyPrefix + "shared"); err != nil || got != "Redis 值" {
		t.Fatalf("miniredis 里应存在带前缀的键: value=%q, err=%v", got, err)
	}
	if keys := server.Keys(); len(keys) != 1 {
		t.Fatalf("miniredis 应只有 1 个键，得到 %v", keys)
	}

	// 切回文件驱动：读到的是文件里那份值（再次证明两套互不相通）。
	manager.Configure(ctx, Config{Driver: DriverFile, Dir: dir})
	if value, found := manager.Get(ctx, "shared"); !found || string(value) != "文件值" {
		t.Fatalf("切回文件驱动应读到文件里的值: found=%v, value=%q", found, value)
	}
}

// TestManagerDegradesWhenRedisUnreachable 覆盖降级主路径（契约 24.5 第 1/2 条）：
// 配置 Redis 但地址不可达 → 降级到文件缓存、degraded=true、原因可读、业务读写照常成功。
func TestManagerDegradesWhenRedisUnreachable(t *testing.T) {
	ctx := context.Background()
	host, port := freePort(t)

	dir := t.TempDir()
	manager := newTestManager(t, Options{})
	manager.Configure(ctx, Config{
		Driver: DriverRedis,
		Dir:    dir,
		Redis:  RedisConfig{Host: host, Port: port, KeyPrefix: DefaultKeyPrefix, Timeout: 500 * time.Millisecond},
	})

	status := manager.Status(ctx)
	if !status.Degraded {
		t.Fatalf("Redis 不可达应标红降级: %+v", status)
	}
	if status.EffectiveDriver != DriverFile {
		t.Fatalf("降级后应生效文件缓存: %+v", status)
	}
	if status.Driver != DriverRedis {
		t.Fatalf("配置选择的驱动仍是 redis: %+v", status)
	}
	if status.DegradedReason == "" {
		t.Fatal("降级原因必须可读（非空）")
	}
	if !strings.Contains(status.DegradedReason, "连接被拒绝") {
		t.Fatalf("不可达的原因应为「连接被拒绝」，得到 %q", status.DegradedReason)
	}
	if !strings.Contains(status.DegradedReason, fmt.Sprintf("%d", port)) {
		t.Fatalf("原因里应带上目标地址，得到 %q", status.DegradedReason)
	}

	// 业务读写照常成功（缓存层不因降级报错）。
	if err := manager.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("降级期间写入应成功: %v", err)
	}
	if value, found := manager.Get(ctx, "k"); !found || string(value) != "v" {
		t.Fatalf("降级期间读取应成功: found=%v, value=%q", found, value)
	}
	if status = manager.Status(ctx); status.Keys != 1 {
		t.Fatalf("降级期间键数量应可读: %+v", status)
	}
}

// TestManagerDegradeReasonAuthFailure 覆盖认证失败的可读原因（契约 24.5 第 1 条）。
func TestManagerDegradeReasonAuthFailure(t *testing.T) {
	ctx := context.Background()
	server := startMiniRedis(t)
	server.RequireAuth("correct-password")

	cfg := redisConfigFor(t, server)
	cfg.Password = "wrong-password"
	manager := newTestManager(t, Options{})
	manager.Configure(ctx, Config{Driver: DriverRedis, Dir: t.TempDir(), Redis: cfg})

	status := manager.Status(ctx)
	if !status.Degraded || status.EffectiveDriver != DriverFile {
		t.Fatalf("口令错误应降级: %+v", status)
	}
	if !strings.Contains(status.DegradedReason, "认证失败") {
		t.Fatalf("原因应为「认证失败」，得到 %q", status.DegradedReason)
	}
}

// TestManagerDegradeReasonTimeout 覆盖超时分类（注入探活超时的假驱动，不依赖真实网络）。
func TestManagerDegradeReasonTimeout(t *testing.T) {
	ctx := context.Background()
	manager := newTestManager(t, Options{
		RedisFactory: func(RedisConfig) (Store, error) {
			return &stubStore{pingErr: fmt.Errorf("dial tcp: %w", context.DeadlineExceeded)}, nil
		},
	})
	manager.Configure(ctx, Config{
		Driver: DriverRedis,
		Dir:    t.TempDir(),
		Redis:  RedisConfig{Host: "redis.example.com", Port: 6379, Timeout: time.Second},
	})

	status := manager.Status(ctx)
	if !status.Degraded || !strings.Contains(status.DegradedReason, "连接超时") {
		t.Fatalf("超时原因应为「连接超时」: %+v", status)
	}
	if !strings.Contains(status.DegradedReason, "redis.example.com:6379") {
		t.Fatalf("原因里应带上目标地址: %q", status.DegradedReason)
	}
}

// TestManagerSelfHealsWhenRedisComesBack 覆盖自愈（契约 24.5 第 3 条）：
// 降级期间再次探活（这里就是状态接口）时 Redis 已恢复 → 自动切回 Redis，不用重启。
func TestManagerSelfHealsWhenRedisComesBack(t *testing.T) {
	ctx := context.Background()
	host, port := freePort(t)
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	manager := newTestManager(t, Options{})
	manager.Configure(ctx, Config{
		Driver: DriverRedis,
		Dir:    t.TempDir(),
		Redis:  RedisConfig{Host: host, Port: port, KeyPrefix: DefaultKeyPrefix, Timeout: 500 * time.Millisecond},
	})
	if !manager.Status(ctx).Degraded {
		t.Fatal("服务未启动时应处于降级")
	}

	// Redis 起来了：下一次探活（状态接口）就切回去。
	server := miniredis.NewMiniRedis()
	if err := server.StartAddr(addr); err != nil {
		t.Fatalf("在该端口启动 miniredis 失败: %v", err)
	}
	t.Cleanup(server.Close)

	status := manager.Status(ctx)
	if status.Degraded || status.EffectiveDriver != DriverRedis {
		t.Fatalf("Redis 恢复后应自动切回: %+v", status)
	}
	if err := manager.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("自愈后写入失败: %v", err)
	}
	if got, err := server.Get(DefaultKeyPrefix + "k"); err != nil || got != "v" {
		t.Fatalf("自愈后应写入 Redis: value=%q, err=%v", got, err)
	}
}

// TestManagerRuntimeFailoverAndContinue 覆盖运行期降级：Redis 中途挂掉后读按未命中、
// 业务读写继续在文件缓存上成功，且原因可读。
func TestManagerRuntimeFailoverAndContinue(t *testing.T) {
	ctx := context.Background()
	server := startMiniRedis(t)
	manager := newTestManager(t, Options{})
	manager.Configure(ctx, Config{Driver: DriverRedis, Dir: t.TempDir(), Redis: redisConfigFor(t, server)})

	if err := manager.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if _, found := manager.Get(ctx, "k"); !found {
		t.Fatal("Redis 正常时应命中")
	}

	server.Close() // Redis 消失

	if _, found := manager.Get(ctx, "k"); found {
		t.Fatal("Redis 挂掉后读应按未命中处理")
	}
	status := manager.Status(ctx)
	if !status.Degraded || status.EffectiveDriver != DriverFile || status.DegradedReason == "" {
		t.Fatalf("运行期故障应降级并有原因: %+v", status)
	}
	if err := manager.Set(ctx, "k", []byte("file v"), 0); err != nil {
		t.Fatalf("降级后写入应成功: %v", err)
	}
	if value, found := manager.Get(ctx, "k"); !found || string(value) != "file v" {
		t.Fatalf("降级后应在文件缓存上读写: found=%v, value=%q", found, value)
	}
}

// TestManagerFlushOnlyOwnNamespace 覆盖清空口径（契约 24.6 第 3 条）：
// 只清本系统 key_prefix 下的键，同一个 DB 里别人的键绝不动。
func TestManagerFlushOnlyOwnNamespace(t *testing.T) {
	ctx := context.Background()
	server := startMiniRedis(t)
	if err := server.Set("other-app:key", "别人的数据"); err != nil {
		t.Fatalf("写入外部键失败: %v", err)
	}

	manager := newTestManager(t, Options{})
	manager.Configure(ctx, Config{Driver: DriverRedis, Dir: t.TempDir(), Redis: redisConfigFor(t, server)})
	for i := 0; i < 3; i++ {
		if err := manager.Set(ctx, fmt.Sprintf("k%d", i), []byte("v"), 0); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}
	if status := manager.Status(ctx); status.Keys != 3 {
		t.Fatalf("键数量应为 3（不含别人的键）: %+v", status)
	}

	if err := manager.Flush(ctx); err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	if keys := server.Keys(); len(keys) != 1 || keys[0] != "other-app:key" {
		t.Fatalf("清空后只应剩下别人的键: %v", keys)
	}
	if status := manager.Status(ctx); status.Keys != 0 {
		t.Fatalf("清空后本命名空间键数量应为 0: %+v", status)
	}
}

// TestManagerFlushDuringDegradeAlsoClearsRedis 覆盖降级期间的清空：
// 生效驱动是文件缓存，但配置的 Redis 也尽力清一次（避免恢复后命中残留旧值），失败不报错。
func TestManagerFlushDuringDegradeAlsoClearsRedis(t *testing.T) {
	ctx := context.Background()
	host, port := freePort(t)
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	manager := newTestManager(t, Options{})
	manager.Configure(ctx, Config{
		Driver: DriverRedis,
		Dir:    t.TempDir(),
		Redis:  RedisConfig{Host: host, Port: port, KeyPrefix: DefaultKeyPrefix, Timeout: 500 * time.Millisecond},
	})
	if !manager.Status(ctx).Degraded {
		t.Fatal("服务未启动时应处于降级")
	}
	// 降级期间 Redis 不可达：清空不应报错（文件缓存清空了，Redis 那一步失败只记日志）。
	if err := manager.Flush(ctx); err != nil {
		t.Fatalf("降级期间清空不应报错: %v", err)
	}

	// Redis 起来了（还带着一份残留键）：降级期间再清一次，Redis 命名空间也要被清掉。
	server := miniredis.NewMiniRedis()
	if err := server.StartAddr(addr); err != nil {
		t.Fatalf("在该端口启动 miniredis 失败: %v", err)
	}
	t.Cleanup(server.Close)
	if err := server.Set(DefaultKeyPrefix+"stale", "残留值"); err != nil {
		t.Fatalf("写入残留键失败: %v", err)
	}
	if err := manager.Flush(ctx); err != nil {
		t.Fatalf("降级期间清空不应报错: %v", err)
	}
	if _, err := server.Get(DefaultKeyPrefix + "stale"); !errors.Is(err, miniredis.ErrKeyNotFound) {
		t.Fatalf("降级期间的清空也应清掉 Redis 命名空间里的残留键: err=%v", err)
	}
}

// TestManagerStatusRedisConnectionInfo 覆盖 Redis 连接信息回显（不含口令）与键数量。
func TestManagerStatusRedisConnectionInfo(t *testing.T) {
	ctx := context.Background()
	server := startMiniRedis(t)
	cfg := redisConfigFor(t, server)
	cfg.DB = 3
	cfg.KeyPrefix = "app-cache:"

	manager := newTestManager(t, Options{})
	manager.Configure(ctx, Config{Driver: DriverRedis, Dir: t.TempDir(), Redis: cfg})
	if err := manager.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	status := manager.Status(ctx)
	if status.Redis.Host != cfg.Host || status.Redis.Port != cfg.Port || status.Redis.DB != cfg.DB {
		t.Fatalf("连接信息应回显配置（不含口令）: %+v", status.Redis)
	}
	if status.Redis.KeyPrefix != "app-cache:" {
		t.Fatalf("键前缀应回显配置: %+v", status.Redis)
	}
	if status.Degraded {
		t.Fatalf("miniredis 可用时不应降级: %+v", status)
	}
	if status.Keys != 1 {
		t.Fatalf("键数量应为 1: %+v", status)
	}
	// 注意：miniredis 的 INFO 只实现 clients / stats 两个分区，不报 redis_version，
	// 因此这里 Version 允许为空（真 Redis 会返回版本号，解析逻辑另有单测覆盖）。
	if status.Redis.Version != "" && !strings.Contains(status.Redis.Version, ".") {
		t.Fatalf("若拿到版本号，应是形如 7.2.5 的字符串: %q", status.Redis.Version)
	}
}

// TestParseRedisVersion 覆盖 INFO 文本里的版本解析（用真实 Redis 的 INFO 片段）。
func TestParseRedisVersion(t *testing.T) {
	info := "# Server\r\nredis_version:7.2.5\r\nredis_git_sha1:00000000\r\nredis_mode:standalone\r\n"
	if got := parseRedisVersion(info); got != "7.2.5" {
		t.Fatalf("版本解析错误: %q", got)
	}
	if got := parseRedisVersion("# Server\r\nredis_mode:standalone\r\n"); got != "" {
		t.Fatalf("没有版本字段时应返回空串，得到 %q", got)
	}
}

// TestManagerConfigureIdempotent 覆盖「配置未变且未降级时无操作」：
// 不重复构造驱动（避免每次读设置都重建连接池 / 清理协程）。
func TestManagerConfigureIdempotent(t *testing.T) {
	ctx := context.Background()
	fileCalls := 0
	manager := newTestManager(t, Options{
		FileFactory: func(dir string) (Store, error) {
			fileCalls++
			return newFileStore(dir, time.Hour, silentTestLogger())
		},
	})
	cfg := Config{Driver: DriverFile, Dir: t.TempDir()}

	manager.Configure(ctx, cfg)
	manager.Configure(ctx, cfg)
	manager.Configure(ctx, cfg)
	if fileCalls != 1 {
		t.Fatalf("配置未变时不应重建驱动，构造次数=%d", fileCalls)
	}
	// 配置变化 → 重建一次。
	manager.Configure(ctx, Config{Driver: DriverFile, Dir: t.TempDir()})
	if fileCalls != 2 {
		t.Fatalf("配置变化应重建驱动，构造次数=%d", fileCalls)
	}
}

// TestManagerUnconfiguredManagerIsSafe 覆盖未配置时的安全行为：
// 读按未命中、写返回错误（业务侧只记日志），不 panic。
func TestManagerUnconfiguredManagerIsSafe(t *testing.T) {
	ctx := context.Background()
	manager := newTestManager(t, Options{})

	if _, found := manager.Get(ctx, "k"); found {
		t.Fatal("未配置时读应按未命中")
	}
	if err := manager.Set(ctx, "k", []byte("v"), 0); !errors.Is(err, errNoStore) {
		t.Fatalf("未配置时写应返回 errNoStore，得到 %v", err)
	}
	if err := manager.Delete(ctx, "k"); !errors.Is(err, errNoStore) {
		t.Fatalf("未配置时删除应返回 errNoStore，得到 %v", err)
	}
	if stats := manager.Stats(); stats.Hits != 0 || stats.Misses != 1 {
		t.Fatalf("未配置时的读应记一次未命中: %+v", stats)
	}
}

// TestManagerFileFactoryFailureDisablesCacheSafely 覆盖文件缓存都不可用的极端情形：
// 缓存层停用但业务不受影响（读未命中、写报错、状态可读）。
func TestManagerFileFactoryFailureDisablesCacheSafely(t *testing.T) {
	ctx := context.Background()
	manager := newTestManager(t, Options{
		FileFactory: func(string) (Store, error) { return nil, errors.New("磁盘写满") },
	})
	manager.Configure(ctx, Config{Driver: DriverFile, Dir: t.TempDir()})

	if _, found := manager.Get(ctx, "k"); found {
		t.Fatal("停用时应按未命中")
	}
	if err := manager.Set(ctx, "k", []byte("v"), 0); err == nil {
		t.Fatal("停用时写应报错（由调用方记录，不影响业务）")
	}
	status := manager.Status(ctx)
	if status.Keys != 0 || status.EffectiveDriver == "" {
		t.Fatalf("停用时状态仍应可读: %+v", status)
	}
}

// TestManagerFileDriverErrorDoesNotDegrade 覆盖「文件驱动出错不标红降级」：
// 降级是 Redis 的口径；文件缓存出错只记日志并继续按未命中处理。
func TestManagerFileDriverErrorDoesNotDegrade(t *testing.T) {
	ctx := context.Background()
	manager := newTestManager(t, Options{
		FileFactory: func(string) (Store, error) {
			return &stubStore{getErr: errors.New("读盘失败")}, nil
		},
	})
	manager.Configure(ctx, Config{Driver: DriverFile, Dir: t.TempDir()})

	if _, found := manager.Get(ctx, "k"); found {
		t.Fatal("文件驱动出错应按未命中")
	}
	status := manager.Status(ctx)
	if status.Degraded {
		t.Fatalf("文件驱动出错不应标红降级: %+v", status)
	}
	if status.EffectiveDriver != DriverFile {
		t.Fatalf("生效驱动仍是 file: %+v", status)
	}
}

// TestDescribeRedisError 覆盖降级原因的错误分类（契约 24.5：连接被拒绝 / 认证失败 / 超时）。
func TestDescribeRedisError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"未分类的连接错误仍可读", errors.New("dial tcp 127.0.0.1:6379: something odd"), "连接失败"},
		{"连接被拒绝", fmt.Errorf("拨号失败: %w", syscall.ECONNREFUSED), "连接被拒绝"},
		{"上下文超时", fmt.Errorf("ping: %w", context.DeadlineExceeded), "连接超时"},
		{"net 层超时", &net.DNSError{IsTimeout: true, Err: "i/o timeout"}, "连接超时"},
		{"认证失败", errors.New("WRONGPASS invalid username-password pair"), "认证失败"},
		{"未认证", errors.New("NOAUTH Authentication required."), "认证失败"},
		{"主机名解析失败", &net.DNSError{Err: "no such host", Name: "nope.invalid"}, "无法解析主机名"},
	}
	for _, tc := range cases {
		if got := describeRedisError(tc.err); !strings.Contains(got, tc.want) {
			t.Fatalf("%s：describeRedisError(%v) = %q，应包含 %q", tc.name, tc.err, got, tc.want)
		}
	}
	if describeRedisError(nil) != "" {
		t.Fatal("nil 错误应映射为空串")
	}
}
