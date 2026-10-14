package router

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/cache"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/oplog"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// ---------------------------------------------------------------------------
// 缓存基础设施（R19 批 1，契约 24.3 / 24.4）：设置读写、状态、重置、清空与权限
// ---------------------------------------------------------------------------

// cacheEndpoints 是本节新增的五个接口（方法 + 路径）。
var cacheEndpoints = []struct {
	method string
	path   string
}{
	{http.MethodGet, "/api/v1/admin/settings/cache"},
	{http.MethodPut, "/api/v1/admin/settings/cache"},
	{http.MethodGet, "/api/v1/admin/cache/status"},
	{http.MethodPost, "/api/v1/admin/cache/reset-stats"},
	{http.MethodPost, "/api/v1/admin/cache/flush"},
}

// newCacheEngine 构造带缓存管理器的集成测试引擎（管理器可注入，便于确定性断言统计）。
// 未注入时构造默认管理器（文件缓存落在系统临时目录的 lyidc-cache 下）。
func newCacheEngine(t *testing.T, gdb *gorm.DB, manager *cache.Manager) *gin.Engine {
	t.Helper()
	return newCacheEngineWith(t, gdb, manager, nil)
}

// newCacheEngineWith 同上，可额外注入操作日志写入器（断言留痕用）。
// CacheDir 取本用例专属目录（R19 批 2 的测试隔离：业务读路径会真的往文件缓存里写键，
// 共享缺省目录会让用例之间互相读到对方的旧值）。
func newCacheEngineWith(t *testing.T, gdb *gorm.DB, manager *cache.Manager, writer *oplog.Writer) *gin.Engine {
	t.Helper()
	return newCacheEngineWithDir(t, gdb, manager, writer, t.TempDir())
}

// newCacheEngineWithDir 同 newCacheEngineWith，但显式指定文件缓存目录（断言目录口径用）。
func newCacheEngineWithDir(t *testing.T, gdb *gorm.DB, manager *cache.Manager, writer *oplog.Writer, dir string) *gin.Engine {
	t.Helper()
	if manager == nil {
		manager = cache.NewManager(cache.Options{Logger: silentLogger(), CleanupInterval: time.Hour})
	}
	t.Cleanup(func() { _ = manager.Close() })
	return New(Options{
		CacheDir: dir,
		Logger:   silentLogger(),
		DB:       gdb,
		JWT:      config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		Upstream: upstream.StaticProvider{},
		Cache:    manager,
		Oplog:    writer,
	})
}

// freeLocalPort 占一个临时端口后释放：该端口上没有任何服务（连接立即被拒）。
func freeLocalPort(t *testing.T) (string, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("占位监听失败: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)
	_ = listener.Close()
	return addr.IP.String(), addr.Port
}

// TestCacheEndpointsPermissions 验证五个接口都仅 admin（含读取）：
// finance / support 403、会员 token 401、未登录 401。
func TestCacheEndpointsPermissions(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCacheEngine(t, gdb, nil)

	memberToken, _ := memberTokenFor(t, engine, "cacheperm")
	finance := stage4AdminToken(t, engine, gdb, "cachefin", model.RoleFinance)
	support := stage4AdminToken(t, engine, gdb, "cachesup", model.RoleSupport)

	for name, token := range map[string]string{"会员": memberToken, "finance": finance, "support": support} {
		wantStatus, wantCode := http.StatusForbidden, response.CodeForbidden
		if name == "会员" {
			wantStatus, wantCode = http.StatusUnauthorized, response.CodeUnauthorized
		}
		for _, endpoint := range cacheEndpoints {
			rec, envelope := doAPI(t, engine, endpoint.method, endpoint.path, token, map[string]any{})
			if rec.Code != wantStatus || envelope.Code != wantCode {
				t.Fatalf("%s %s %s 应 %d/%d：HTTP %d, body=%s",
					name, endpoint.method, endpoint.path, wantStatus, wantCode, rec.Code, rec.Body.String())
			}
		}
	}

	// 未登录：401。
	for _, endpoint := range cacheEndpoints {
		rec, envelope := doAPI(t, engine, endpoint.method, endpoint.path, "", nil)
		if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
			t.Fatalf("未登录 %s %s 应 401：HTTP %d, body=%s",
				endpoint.method, endpoint.path, rec.Code, rec.Body.String())
		}
	}
}

// TestCacheSettingsReadWrite 验证设置读写口径（契约 24.3）：默认文件缓存、Redis 参数与掩码、
// 三态口令、明文绝不回显。
func TestCacheSettingsReadWrite(t *testing.T) {
	gdb := testDatabase(t)
	// 显式指定目录：断言设置接口回显的就是进程内生效的那个文件缓存目录
	// （缺省口径 = 系统临时目录下的 lyidc-cache，见契约 24.1；测试用隔离目录，见 Options.CacheDir）。
	dir := filepath.Join(t.TempDir(), cache.DefaultDirName)
	engine := newCacheEngineWithDir(t, gdb, nil, nil, dir)
	admin := stage4AdminToken(t, engine, gdb, "cache", model.RoleAdmin)

	// 1) 默认：文件缓存（Redis 未启用），Redis 参数取缺省。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/cache", admin, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("读取缓存设置失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	view := decodeData[cacheSettingsView](t, envelope)
	if view.Driver != "file" || view.RedisEnabled || view.EffectiveDriver != "file" || view.Degraded {
		t.Fatalf("默认应为文件缓存且未启用 Redis: %+v", view)
	}
	if view.Redis.Port != 6379 || view.Redis.DB != 0 || view.Redis.KeyPrefix != "lyidc:" || view.Redis.TimeoutSeconds != 1 {
		t.Fatalf("Redis 参数应取缺省: %+v", view.Redis)
	}
	if view.Redis.PasswordConfigured || view.Redis.PasswordMasked != "" {
		t.Fatalf("口令应未配置: %+v", view.Redis)
	}
	if !strings.HasSuffix(view.FileDir, "lyidc-cache") || view.FileDir != dir {
		t.Fatalf("文件缓存目录应是进程内生效的那个目录（缺省名 lyidc-cache）: %q", view.FileDir)
	}

	// 2) 启用 Redis 并保存参数：口令落库但接口只回掩码。
	const password = "super-secret-pass"
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/cache", admin, map[string]any{
		"driver": "redis",
		"redis": map[string]any{
			"host": "127.0.0.1", "port": 6380, "password": password,
			"db": 2, "key_prefix": "oem:", "timeout_seconds": 3,
		},
	})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("保存缓存设置失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), password) {
		t.Fatalf("响应绝不回显口令明文: %s", rec.Body.String())
	}
	view = decodeData[cacheSettingsView](t, envelope)
	if view.Driver != "redis" || !view.RedisEnabled {
		t.Fatalf("保存后应启用 Redis: %+v", view)
	}
	if !view.Redis.PasswordConfigured || view.Redis.PasswordMasked != "supe****" {
		t.Fatalf("口令应回配置标志与掩码: %+v", view.Redis)
	}
	if view.Redis.Host != "127.0.0.1" || view.Redis.Port != 6380 || view.Redis.DB != 2 ||
		view.Redis.KeyPrefix != "oem:" || view.Redis.TimeoutSeconds != 3 {
		t.Fatalf("Redis 参数应原样落库并回显: %+v", view.Redis)
	}
	if view.UpdatedBy == nil || view.UpdatedAt == nil {
		t.Fatalf("审计字段应回带: %+v", view)
	}
	stored, ok := settingByKey(t, gdb, settings.KeyCache)
	if !ok || !strings.Contains(stored, password) || !strings.Contains(stored, `"driver":"redis"`) {
		t.Fatalf("落库内容不符: %s", stored)
	}

	// 3) 三态口令：省略 = 保持；空串 = 清空；其余字段不受影响。
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/cache", admin,
		map[string]any{"redis": map[string]any{"port": 6381}})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("局部更新失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if stored, _ = settingByKey(t, gdb, settings.KeyCache); !strings.Contains(stored, password) {
		t.Fatalf("省略口令应保持不变: %s", stored)
	}
	if view = decodeData[cacheSettingsView](t, envelope); view.Redis.Port != 6381 || view.Driver != "redis" {
		t.Fatalf("局部更新只改提供的字段: %+v", view)
	}

	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/cache", admin,
		map[string]any{"redis": map[string]any{"password": ""}})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("清空口令失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if stored, _ = settingByKey(t, gdb, settings.KeyCache); strings.Contains(stored, password) {
		t.Fatalf("空串应清空口令: %s", stored)
	}
	if view = decodeData[cacheSettingsView](t, envelope); view.Redis.PasswordConfigured || view.Redis.PasswordMasked != "" {
		t.Fatalf("清空后应回到未配置: %+v", view.Redis)
	}
}

// TestCacheSettingsValidation 验证保存校验（契约 24.3）：非法值一律 40002 + 可读原因且**不落库**。
func TestCacheSettingsValidation(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCacheEngine(t, gdb, nil)
	admin := stage4AdminToken(t, engine, gdb, "cachebad", model.RoleAdmin)

	cases := []struct {
		name string
		body map[string]any
		hint string
	}{
		{"空请求体", map[string]any{}, "至少提供一个字段"},
		{"驱动非法", map[string]any{"driver": "memcached"}, "driver"},
		{"启用 Redis 但缺 host", map[string]any{"driver": "redis"}, "host"},
		{"端口越界", map[string]any{"driver": "redis", "redis": map[string]any{"host": "h", "port": 70000}}, "port"},
		{"超时越界", map[string]any{"driver": "redis", "redis": map[string]any{"host": "h", "timeout_seconds": 99}}, "timeout_seconds"},
		{"数据库序号越界", map[string]any{"driver": "redis", "redis": map[string]any{"host": "h", "db": 16}}, "db"},
	}
	for _, tc := range cases {
		rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/cache", admin, tc.body)
		wantCode := response.CodeValidationFailed
		if tc.name == "空请求体" {
			wantCode = response.CodeInvalidParam // 缺字段是 40001（与全站「至少一个字段」口径一致）
		}
		if rec.Code != http.StatusBadRequest || envelope.Code != wantCode {
			t.Fatalf("%s 应 %d：HTTP %d, body=%s", tc.name, wantCode, rec.Code, rec.Body.String())
		}
		if tc.hint != "" && !strings.Contains(envelope.Message, tc.hint) {
			t.Fatalf("%s 的原因应包含 %q，得到 %q", tc.name, tc.hint, envelope.Message)
		}
		if _, ok := settingByKey(t, gdb, settings.KeyCache); ok {
			t.Fatalf("%s：校验失败不应落库", tc.name)
		}
	}
}

// TestCacheStatusResetAndFlush 验证状态、重置与清空（契约 24.4）：
// 键数量、命中/未命中与命中率、重置归零、清空后键数量归零。
func TestCacheStatusResetAndFlush(t *testing.T) {
	gdb := testDatabase(t)
	manager := cache.NewManager(cache.Options{Logger: silentLogger(), CleanupInterval: time.Hour})
	engine := newCacheEngine(t, gdb, manager)
	admin := stage4AdminToken(t, engine, gdb, "cachestatus", model.RoleAdmin)
	ctx := context.Background()

	// 1) 初始状态：文件驱动、全零。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/cache/status", admin, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("读取缓存状态失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	status := decodeData[cacheStatusView](t, envelope)
	if status.Driver != "file" || status.EffectiveDriver != "file" || status.Degraded {
		t.Fatalf("默认状态应为文件缓存: %+v", status)
	}
	if status.Keys != 0 || status.Hits != 0 || status.Misses != 0 || status.HitRate != 0 {
		t.Fatalf("初始计数应全零: %+v", status)
	}
	if status.StatsSince == "" || status.File.Dir == "" {
		t.Fatalf("应回统计起点与文件目录: %+v", status)
	}

	// 2) 通过管理器制造真实流量（批 2 接线后由业务读路径产生，这里直接调以确定性断言）。
	if err := manager.Set(ctx, "product:1", []byte("{}"), time.Minute); err != nil {
		t.Fatalf("写入缓存失败: %v", err)
	}
	if err := manager.Set(ctx, "product:2", []byte("{}"), time.Minute); err != nil {
		t.Fatalf("写入缓存失败: %v", err)
	}
	manager.Get(ctx, "product:1") // 命中
	manager.Get(ctx, "product:1") // 命中
	manager.Get(ctx, "product:9") // 未命中

	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/cache/status", admin, nil)
	status = decodeData[cacheStatusView](t, envelope)
	if status.Keys != 2 {
		t.Fatalf("键数量应为 2: %+v", status)
	}
	if status.Hits != 2 || status.Misses != 1 || status.HitRate != 66.67 {
		t.Fatalf("命中统计应为 2/1/66.67: %+v", status)
	}

	// 3) 重置统计：计数归零，但缓存内容不动。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/admin/cache/reset-stats", admin, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("重置统计失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	stats := decodeData[cacheStatsView](t, envelope)
	if stats.Hits != 0 || stats.Misses != 0 || stats.HitRate != 0 || stats.StatsSince == "" {
		t.Fatalf("重置后应归零: %+v", stats)
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/cache/status", admin, nil)
	status = decodeData[cacheStatusView](t, envelope)
	if status.Hits != 0 || status.Misses != 0 || status.Keys != 2 {
		t.Fatalf("重置只清统计、不清缓存: %+v", status)
	}

	// 4) 清空缓存：键数量归零，缓存内容清掉。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/admin/cache/flush", admin, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("清空缓存失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	flush := decodeData[cacheFlushView](t, envelope)
	if !flush.Flushed || flush.Keys != 0 || flush.Driver != "file" {
		t.Fatalf("清空后应报 0 键: %+v", flush)
	}
	if _, found := manager.Get(ctx, "product:1"); found {
		t.Fatal("清空后不应再命中")
	}
}

// TestCacheDegradedStatusAndBusinessContinues 验证降级主路径（契约 24.5）：
// 切到 Redis + 不可达地址 → 状态 degraded=true + 原因可读，**业务接口照常可用**；
// Redis 恢复后下一次探活（状态接口）自动切回（自愈）。
func TestCacheDegradedStatusAndBusinessContinues(t *testing.T) {
	gdb := testDatabase(t)
	manager := cache.NewManager(cache.Options{Logger: silentLogger(), CleanupInterval: time.Hour})
	engine := newCacheEngine(t, gdb, manager)
	admin := stage4AdminToken(t, engine, gdb, "cachedeg", model.RoleAdmin)
	host, port := freeLocalPort(t)

	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/cache", admin, map[string]any{
		"driver": "redis",
		"redis":  map[string]any{"host": host, "port": port, "timeout_seconds": 1},
	})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("保存 Redis 设置失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	// 保存响应里就应标红（保存即生效并探活）。
	if view := decodeData[cacheSettingsView](t, envelope); !view.Degraded || view.EffectiveDriver != "file" {
		t.Fatalf("保存后应已标记降级: %+v", view)
	}

	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/cache/status", admin, nil)
	status := decodeData[cacheStatusView](t, envelope)
	if !status.Degraded || status.EffectiveDriver != "file" || status.Driver != "redis" {
		t.Fatalf("状态应显示降级到文件缓存: %+v", status)
	}
	if !strings.Contains(status.DegradedReason, "连接被拒绝") ||
		!strings.Contains(status.DegradedReason, fmt.Sprintf("%d", port)) {
		t.Fatalf("降级原因应可读（含目标地址）: %q", status.DegradedReason)
	}

	// 业务接口照常可用（降级只发生在缓存层，且本批还没有任何业务接线）。
	rec, _ = doAPI(t, engine, http.MethodGet, "/api/v1/products", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("降级期间业务接口应照常可用: HTTP %d", rec.Code)
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/profile", admin, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("降级期间管理端接口应照常可用: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	// 降级期间缓存读写依旧可用（落在文件缓存上）。
	if err := manager.Set(context.Background(), "k", []byte("v"), 0); err != nil {
		t.Fatalf("降级期间缓存写入失败: %v", err)
	}
	if _, found := manager.Get(context.Background(), "k"); !found {
		t.Fatal("降级期间缓存读取失败")
	}

	// 自愈：Redis 在同一个地址起来了，下一次状态请求（=探活）应切回 Redis。
	server := miniredis.NewMiniRedis()
	if err := server.StartAddr(net.JoinHostPort(host, fmt.Sprintf("%d", port))); err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	t.Cleanup(server.Close)

	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/cache/status", admin, nil)
	status = decodeData[cacheStatusView](t, envelope)
	if status.Degraded || status.EffectiveDriver != "redis" {
		t.Fatalf("Redis 恢复后应自愈切回: %+v", status)
	}
	if status.Keys != 0 {
		t.Fatalf("切回 Redis 后键数量来自 Redis（此时为空）: %+v", status)
	}
}

// TestCacheRedisDriverEndToEnd 验证 Redis 可用分支（miniredis，进程内 RESP 服务）：
// 保存即生效、读写走 Redis 且带键前缀、键数量与统计随使用增长、清空只清本命名空间。
func TestCacheRedisDriverEndToEnd(t *testing.T) {
	gdb := testDatabase(t)
	server := miniredis.RunT(t)
	host, port, err := net.SplitHostPort(server.Addr())
	if err != nil {
		t.Fatalf("拆分 miniredis 地址失败: %v", err)
	}
	if err := server.Set("other-app:key", "别人的数据"); err != nil {
		t.Fatalf("写入外部键失败: %v", err)
	}

	manager := cache.NewManager(cache.Options{Logger: silentLogger(), CleanupInterval: time.Hour})
	engine := newCacheEngine(t, gdb, manager)
	admin := stage4AdminToken(t, engine, gdb, "cacheredis", model.RoleAdmin)

	var portNumber int
	if _, err := fmt.Sscanf(port, "%d", &portNumber); err != nil {
		t.Fatalf("解析端口失败: %v", err)
	}
	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/cache", admin, map[string]any{
		"driver": "redis",
		"redis": map[string]any{
			"host": host, "port": portNumber, "key_prefix": "oem:", "timeout_seconds": 1,
		},
	})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("保存 Redis 设置失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if view := decodeData[cacheSettingsView](t, envelope); view.Degraded || view.EffectiveDriver != "redis" {
		t.Fatalf("Redis 可达时不应降级: %+v", view)
	}

	ctx := context.Background()
	if err := manager.Set(ctx, "product:1", []byte("{}"), time.Minute); err != nil {
		t.Fatalf("写入 Redis 失败: %v", err)
	}
	if got, err := server.Get("oem:product:1"); err != nil || got != "{}" {
		t.Fatalf("键应带前缀写入 Redis: value=%q, err=%v", got, err)
	}
	manager.Get(ctx, "product:1") // 命中
	manager.Get(ctx, "product:2") // 未命中

	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/cache/status", admin, nil)
	status := decodeData[cacheStatusView](t, envelope)
	if status.EffectiveDriver != "redis" || status.Degraded {
		t.Fatalf("状态应显示生效 Redis: %+v", status)
	}
	if status.Keys != 1 {
		t.Fatalf("键数量应只数本命名空间（1，不含别人的键）: %+v", status)
	}
	if status.Hits != 1 || status.Misses != 1 || status.HitRate != 50 {
		t.Fatalf("统计应随读写增长: %+v", status)
	}
	if status.Redis.Host != host || status.Redis.Port != portNumber || status.Redis.KeyPrefix != "oem:" {
		t.Fatalf("状态应回显 Redis 连接信息: %+v", status.Redis)
	}

	// 清空：只清本命名空间，别人的键不动。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/admin/cache/flush", admin, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("清空 Redis 缓存失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if flush := decodeData[cacheFlushView](t, envelope); flush.Driver != "redis" || flush.Keys != 0 {
		t.Fatalf("清空后应报 0 键: %+v", flush)
	}
	if keys := server.Keys(); len(keys) != 1 || keys[0] != "other-app:key" {
		t.Fatalf("清空只应清本命名空间: %v", keys)
	}
}

// TestCacheWritesAreOpLogged 验证三个写接口的操作日志动作名（契约 23.3 的映射表补充）。
func TestCacheWritesAreOpLogged(t *testing.T) {
	gdb := testDatabase(t)
	engine, writer := newOplogEngineWithWriter(t, gdb)
	admin := stage4AdminToken(t, engine, gdb, "cachelog", model.RoleAdmin)

	doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/cache", admin,
		map[string]any{"driver": "file"})
	doAPI(t, engine, http.MethodPost, "/api/v1/admin/cache/reset-stats", admin, nil)
	doAPI(t, engine, http.MethodPost, "/api/v1/admin/cache/flush", admin, nil)
	mustFlushOplog(t, writer)

	actions := map[string]bool{}
	for _, row := range oplogRows(t, gdb, oplog.ChannelAdmin) {
		actions[row.Action] = true
	}
	for _, want := range []string{"修改缓存设置", "重置缓存统计", "清空缓存"} {
		if !actions[want] {
			t.Fatalf("操作日志应记录动作 %q，实际动作集合: %v", want, actions)
		}
	}
}

// TestCacheRoutesRegistered 断言路由表形状：未注册的方法与路径都落 NoRoute 兜底（404，
// 与仓库既有口径一致，见 TestMethodNotAllowedReturnsEnvelope），不留可误用的入口。
func TestCacheRoutesRegistered(t *testing.T) {
	gdb := testDatabase(t)
	engine := newCacheEngine(t, gdb, nil)
	admin := stage4AdminToken(t, engine, gdb, "cacheroute", model.RoleAdmin)

	// 只读接口用写方法访问（未注册方法）→ NoRoute 兜底 404，绝不落到处理器上。
	for _, path := range []string{"/api/v1/admin/settings/cache", "/api/v1/admin/cache/status"} {
		rec, envelope := doAPI(t, engine, http.MethodDelete, path, admin, nil)
		if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
			t.Fatalf("DELETE %s 应 404：HTTP %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}
	// 未注册路径 → 404（登录鉴权通过后仍不存在）。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/cache/keys", admin, nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("未注册路径应 404：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}
