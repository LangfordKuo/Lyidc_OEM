package router

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/cache"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// ---------------------------------------------------------------------------
// 业务缓存接线（R19 批 2，契约 24.6）：命中/回源、主动失效、TTL、不串号、故障降级
//
// 用进程内的假驱动（map + 记录 TTL）替代文件驱动跑业务接线用例：
// 「写了哪些键、TTL 是多少、有没有打库」都可以确定性断言，失效与故障分支不依赖真实磁盘/Redis。
// ---------------------------------------------------------------------------

// fakeCacheStore 是记录型假缓存驱动。
type fakeCacheStore struct {
	mu      sync.Mutex
	entries map[string]string
	ttls    map[string]time.Duration
	// 故障注入：非 nil 时 Get / Set / DeletePrefix 分别返回该错误。
	getErr error
	setErr error
	delErr error
}

func newFakeCacheStore() *fakeCacheStore {
	return &fakeCacheStore{entries: map[string]string{}, ttls: map[string]time.Duration{}}
}

func (s *fakeCacheStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return nil, false, s.getErr
	}
	value, ok := s.entries[key]
	if !ok {
		return nil, false, nil
	}
	return []byte(value), true, nil
}

func (s *fakeCacheStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setErr != nil {
		return s.setErr
	}
	s.entries[key] = string(value)
	s.ttls[key] = ttl
	return nil
}

func (s *fakeCacheStore) Delete(_ context.Context, keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		delete(s.entries, key)
		delete(s.ttls, key)
	}
	return nil
}

func (s *fakeCacheStore) DeletePrefix(_ context.Context, prefix string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.delErr != nil {
		return s.delErr
	}
	for key := range s.entries {
		if strings.HasPrefix(key, prefix) {
			delete(s.entries, key)
			delete(s.ttls, key)
		}
	}
	return nil
}

func (s *fakeCacheStore) Flush(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = map[string]string{}
	s.ttls = map[string]time.Duration{}
	return nil
}

func (s *fakeCacheStore) Ping(context.Context) error          { return nil }
func (s *fakeCacheStore) Close() error                        { return nil }
func (s *fakeCacheStore) Keys(context.Context) (int64, error) { return int64(len(s.keys())), nil }

// keys 返回当前记录的全部键（升序，供断言「写了哪些键族」）。
func (s *fakeCacheStore) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.entries))
	for key := range s.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// has 判断某个键当前是否在缓存里。
func (s *fakeCacheStore) has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.entries[key]
	return ok
}

// valueOf 读取某个键的缓存内容。
func (s *fakeCacheStore) valueOf(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.entries[key]
	return value, ok
}

// ttlOf 返回某个键最近一次写入时用的 TTL。
func (s *fakeCacheStore) ttlOf(key string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ttls[key]
}

// fail 打开/关闭故障注入。
func (s *fakeCacheStore) fail(getErr, setErr, delErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getErr, s.setErr, s.delErr = getErr, setErr, delErr
}

// expireAll 模拟 TTL 到期（时间到了，缓存自然失效）。
func (s *fakeCacheStore) expireAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = map[string]string{}
}

// newFakeCacheManager 构造注入假驱动的缓存管理器（默认走文件驱动分支）。
func newFakeCacheManager(t *testing.T, store *fakeCacheStore) *cache.Manager {
	t.Helper()
	manager := cache.NewManager(cache.Options{
		Logger:          silentLogger(),
		CleanupInterval: time.Hour,
		FileFactory:     func(string) (cache.Store, error) { return store, nil },
	})
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}

// countingQueryLogger 统计经过 GORM 的语句条数（只计数、不写日志），
// 用于断言「命中缓存时不再打库」。
type countingQueryLogger struct{ count atomic.Int64 }

func (l *countingQueryLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }
func (l *countingQueryLogger) Info(context.Context, string, ...interface{})     {}
func (l *countingQueryLogger) Warn(context.Context, string, ...interface{})     {}
func (l *countingQueryLogger) Error(context.Context, string, ...interface{})    {}
func (l *countingQueryLogger) Trace(context.Context, time.Time, func() (string, int64), error) {
	l.count.Add(1)
}

// newCountingDB 包一层统计 SQL 条数的会话（不动共享的 testDatabase 句柄）。
func newCountingDB(t *testing.T, gdb *gorm.DB) (*gorm.DB, *countingQueryLogger) {
	t.Helper()
	logger := &countingQueryLogger{}
	return gdb.Session(&gorm.Session{Logger: logger}), logger
}

// newBizCacheEngine 构造带假缓存驱动与假上游目录客户端的测试引擎。
func newBizCacheEngine(t *testing.T, gdb *gorm.DB, manager *cache.Manager, client *upstream.Client) *gin.Engine {
	t.Helper()
	return New(Options{
		CacheDir:        t.TempDir(),
		Logger:          silentLogger(),
		DB:              gdb,
		JWT:             config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		Upstream:        upstream.StaticProvider{C: client},
		UpstreamTimeout: 5 * time.Second,
		Cache:           manager,
	})
}

// newBizCacheEngineWithUpstreamBase 同 newBizCacheEngine，但按构建期地址注入假上游
// （探活链路用：走真实的上游客户端，便于用调用计数断言缓存是否生效）。
func newBizCacheEngineWithUpstreamBase(t *testing.T, gdb *gorm.DB, manager *cache.Manager, baseURL string) *gin.Engine {
	t.Helper()
	return New(Options{
		CacheDir:        t.TempDir(),
		Logger:          silentLogger(),
		DB:              gdb,
		JWT:             config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		UpstreamBaseURL: baseURL,
		UpstreamTimeout: 5 * time.Second,
		Cache:           manager,
	})
}

// seedCatalog 导入标准目录、全部上架（会员端只出上架商品）并返回管理员 token。
// 上架用直接改库完成（绕开写接口）——用例在读之前做，因此不会与缓存失效语义纠缠。
func seedCatalog(t *testing.T, engine http.Handler, gdb *gorm.DB) string {
	t.Helper()
	token := adminTokenFor(t, engine, gdb, model.RoleAdmin)
	importProducts(t, engine, token)
	mustShelveAll(t, gdb)
	return token
}

// cacheStatus 读取 /admin/cache/status 的计数（键数量 / 命中 / 未命中）。
func cacheStatus(t *testing.T, engine http.Handler, token string) cacheStatusView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/cache/status", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("读取缓存状态失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[cacheStatusView](t, envelope)
}

// adminProductDetailGet 调 GET /admin/products/:id 并返回详情视图。
func adminProductDetailGet(t *testing.T, engine http.Handler, token string, id uint64) adminProductDetailView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/products/"+itoa(id), token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("管理端商品详情失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[adminProductDetailView](t, envelope)
}

// ---------------------------------------------------------------------------
// 一、命中 / 回源 / 回填
// ---------------------------------------------------------------------------

// TestBizCacheCatalogHitSkipsDatabaseAndReplaysSameBody 覆盖会员端商品目录：
// 首次未命中回源并回填（带 TTL）；第二次命中**不再打库**，且响应与未命中时**逐字节一致**；
// 命中/未命中与键数量真的记进统计（批 1「恒为 0」的口径作废）。
func TestBizCacheCatalogHitSkipsDatabaseAndReplaysSameBody(t *testing.T) {
	gdb := testDatabase(t)
	store := newFakeCacheStore()
	manager := newFakeCacheManager(t, store)
	counting, queries := newCountingDB(t, gdb)
	engine := newBizCacheEngine(t, counting, manager, newFakeCatalogClient(t, standardCatalog()))
	token := seedCatalog(t, engine, gdb)

	// 首次请求：未命中 → 回源 → 回填。
	base := queries.count.Load()
	rec1, envelope1 := doAPI(t, engine, http.MethodGet, "/api/v1/products", "", nil)
	if rec1.Code != http.StatusOK || envelope1.Code != response.CodeSuccess {
		t.Fatalf("商品目录请求失败: HTTP %d, body=%s", rec1.Code, rec1.Body.String())
	}
	if queries.count.Load() == base {
		t.Fatal("未命中时应回源打库")
	}
	if !store.has(cacheKeyProductsCatalog) {
		t.Fatalf("未命中回源后应回填 %q，实际键: %v", cacheKeyProductsCatalog, store.keys())
	}
	if ttl := store.ttlOf(cacheKeyProductsCatalog); ttl != cacheTTLProducts {
		t.Fatalf("商品目录 TTL = %v，期望 %v（契约 24.6 的兜底口径）", ttl, cacheTTLProducts)
	}

	// 第二次请求：命中 → 零 SQL，响应逐字节一致。
	base = queries.count.Load()
	rec2, _ := doAPI(t, engine, http.MethodGet, "/api/v1/products", "", nil)
	if got := queries.count.Load(); got != base {
		t.Fatalf("命中缓存不应再打库：新增 %d 条 SQL", got-base)
	}
	if rec2.Body.String() != rec1.Body.String() {
		t.Fatalf("命中与未命中的响应必须一致:\n未命中: %s\n命中:   %s", rec1.Body.String(), rec2.Body.String())
	}

	// 状态接口的键数量与命中率随业务流量变化。
	status := cacheStatus(t, engine, token)
	if status.Keys < 1 || status.Hits < 1 || status.Misses < 1 || status.HitRate <= 0 {
		t.Fatalf("键数量与命中/未命中应随业务变化: %+v", status)
	}
}

// TestBizCacheReadPathsBackfillTheirKeys 覆盖各键族的回填与 TTL：
// 会员端目录/详情、管理端列表/详情/分组树/分类树/映射列表、站点设置、上游探活各自入键。
func TestBizCacheReadPathsBackfillTheirKeys(t *testing.T) {
	gdb := testDatabase(t)
	store := newFakeCacheStore()
	manager := newFakeCacheManager(t, store)
	engine := newBizCacheEngine(t, gdb, manager, newFakeCatalogClient(t, standardCatalog()))
	token := seedCatalog(t, engine, gdb)
	productID := productIDByUpstreamPID(t, gdb, 101)

	// （1）会员端详情：products:detail:<id>，TTL 10 分钟。
	if rec, _ := doAPI(t, engine, http.MethodGet, "/api/v1/products/"+itoa(productID), "", nil); rec.Code != http.StatusOK {
		t.Fatalf("会员端详情失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	detailKey := cacheKeyProductDetailPrefix + itoa(productID)
	if !store.has(detailKey) || store.ttlOf(detailKey) != cacheTTLProducts {
		t.Fatalf("会员端详情应回填 %q（TTL %v），实际键: %v", detailKey, cacheTTLProducts, store.keys())
	}

	// （2）管理端商品列表：筛选指纹入键；不同筛选组合是不同键。
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/products?status=on", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("管理端商品列表失败: %s", toJSON(t, envelope))
	}
	listKey := cacheKeyAdminProductListPrefix + "p1/s20/st=on"
	if !store.has(listKey) || store.ttlOf(listKey) != cacheTTLProducts {
		t.Fatalf("管理端列表应回填 %q，实际键: %v", listKey, store.keys())
	}
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/products?page=2", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("管理端商品列表（第 2 页）失败: %s", toJSON(t, envelope))
	}
	if !store.has(cacheKeyAdminProductListPrefix + "p2/s20") {
		t.Fatalf("不同分页应各自入键，实际键: %v", store.keys())
	}

	// （3）管理端详情、分组树（商品统计）、分类树、映射列表。
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/products/"+itoa(productID), token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("管理端详情失败: %s", toJSON(t, envelope))
	}
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/product-groups", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("分组树失败: %s", toJSON(t, envelope))
	}
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/categories", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("分类树失败: %s", toJSON(t, envelope))
	}
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/product-group-maps", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("映射列表失败: %s", toJSON(t, envelope))
	}
	for key, ttl := range map[string]time.Duration{
		cacheKeyAdminProductDetailPrefix + itoa(productID): cacheTTLProducts,
		cacheKeyAdminProductGroups:                         cacheTTLProducts,
		cacheKeyAdminCategories:                            cacheTTLCategories,
		cacheKeyAdminGroupMaps:                             cacheTTLCategories,
	} {
		if !store.has(key) || store.ttlOf(key) != ttl {
			t.Fatalf("键 %q 应回填且 TTL=%v，实际键: %v", key, ttl, store.keys())
		}
	}

	// （4）站点设置读取：settings:site，TTL 10 分钟。
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/site", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("站点设置读取失败: %s", toJSON(t, envelope))
	}
	if !store.has(cacheKeySiteSettings) || store.ttlOf(cacheKeySiteSettings) != cacheTTLSiteSettings {
		t.Fatalf("站点设置应回填 %q，实际键: %v", cacheKeySiteSettings, store.keys())
	}

	// （5）上游探活：upstream:health，**短 TTL** 1 分钟。
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/upstream/health", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("上游探活失败: %s", toJSON(t, envelope))
	}
	if !store.has(cacheKeyUpstreamHealth) || store.ttlOf(cacheKeyUpstreamHealth) != cacheTTLUpstreamHealth {
		t.Fatalf("上游探活应回填 %q（TTL %v），实际键: %v",
			cacheKeyUpstreamHealth, cacheTTLUpstreamHealth, store.keys())
	}

	// 全部键都落在登记过的键族里（没有计划外的缓存键）。
	for _, key := range store.keys() {
		if !strings.HasPrefix(key, cachePrefixProducts) && !strings.HasPrefix(key, cachePrefixCategories) &&
			!strings.HasPrefix(key, cachePrefixSiteSettings) && !strings.HasPrefix(key, cachePrefixUpstreamHealth) {
			t.Fatalf("出现未登记的缓存键族: %q", key)
		}
	}
}

// TestBizCacheKeywordSearchIsNotCached 覆盖管理端关键词检索：基数无上限、不入缓存。
func TestBizCacheKeywordSearchIsNotCached(t *testing.T) {
	gdb := testDatabase(t)
	store := newFakeCacheStore()
	manager := newFakeCacheManager(t, store)
	engine := newBizCacheEngine(t, gdb, manager, newFakeCatalogClient(t, standardCatalog()))
	token := seedCatalog(t, engine, gdb)

	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/products?keyword=香港", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("关键词检索失败: %s", toJSON(t, envelope))
	}
	for _, key := range store.keys() {
		if strings.HasPrefix(key, cacheKeyAdminProductListPrefix) {
			t.Fatalf("带关键词的检索不应入缓存，实际键: %v", store.keys())
		}
	}
	// 同一筛选去掉关键词后才入键（指纹里不含关键词，故调用方必须先分流）。
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/products", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("无关键词列表失败: %s", toJSON(t, envelope))
	}
	if !store.has(cacheKeyAdminProductListPrefix + "p1/s20") {
		t.Fatalf("无关键词列表应入键，实际键: %v", store.keys())
	}
}

// TestAdminProductListFingerprint 覆盖筛选指纹的区分度（不含关键词）。
func TestAdminProductListFingerprint(t *testing.T) {
	group := uint64(7)
	category := uint64(9)
	base := adminProductListFingerprint(1, 20, "", nil, nil)
	cases := map[string]string{
		"分页":  adminProductListFingerprint(2, 20, "", nil, nil),
		"页大小": adminProductListFingerprint(1, 50, "", nil, nil),
		"状态":  adminProductListFingerprint(1, 20, "on", nil, nil),
		"分组":  adminProductListFingerprint(1, 20, "", &group, nil),
		"分类":  adminProductListFingerprint(1, 20, "", nil, &category),
		"未归类": adminProductListFingerprint(1, 20, "", nil, new(uint64)),
	}
	seen := map[string]string{base: "基础"}
	for name, fingerprint := range cases {
		if other, ok := seen[fingerprint]; ok {
			t.Fatalf("指纹应可区分：%s 与 %s 撞成 %q", name, other, fingerprint)
		}
		seen[fingerprint] = name
	}
}

// ---------------------------------------------------------------------------
// 二、主动失效（不等 TTL）
// ---------------------------------------------------------------------------

// TestBizCacheWriteInvalidationProduct 覆盖改商品后的主动失效：
// 改商品（这里改上下架）后下一次读**立刻**看到新值，而不是等 10 分钟 TTL。
func TestBizCacheWriteInvalidationProduct(t *testing.T) {
	gdb := testDatabase(t)
	store := newFakeCacheStore()
	manager := newFakeCacheManager(t, store)
	engine := newBizCacheEngine(t, gdb, manager, newFakeCatalogClient(t, standardCatalog()))
	token := seedCatalog(t, engine, gdb)
	productID := productIDByUpstreamPID(t, gdb, 101)

	// 预热（目录与分组计数都已入缓存），再改商品。
	memberCatalog(t, engine)
	listAdminGroups(t, engine, token)
	updateProductOK(t, engine, token, productID, map[string]any{"status": "off"})

	// 整个 products: 族都失效了：目录、分组计数、详情键一个不剩。
	if store.has(cacheKeyProductsCatalog) || store.has(cacheKeyAdminProductGroups) {
		t.Fatalf("改商品后 products: 族应整体失效，实际键: %v", store.keys())
	}

	// 目录：下架商品立刻不在（不等 TTL）。
	catalog, _ := memberCatalog(t, engine)
	for _, group := range catalog.Groups {
		for _, product := range group.Products {
			if product.ID == productID {
				t.Fatalf("下架后商品 %d 不应再出现在目录里", productID)
			}
		}
	}
	// 会员端详情：下架商品统一 404（立即生效）。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/products/"+itoa(productID), "", nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("下架商品详情应 404：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	// 管理端详情与分组计数（仪表盘统计口径）同样立刻刷新。
	if view := adminProductDetailGet(t, engine, token, productID); view.Status != "off" {
		t.Fatalf("管理端详情应立即反映下架状态: %+v", view)
	}
	for _, group := range listAdminGroups(t, engine, token).Items {
		if group.UpstreamGroupID == 1 && (group.Products.On != 1 || group.Products.Off != 1) {
			t.Fatalf("分组计数应立即刷新（上架 1 / 下架 1）: %+v", group.Products)
		}
	}
}

// TestBizCacheWriteInvalidationCategory 覆盖改分类后的主动失效：
// 分类名同时出现在分类树与商品视图里，两个键族一起失效、下一次读立刻是新值。
func TestBizCacheWriteInvalidationCategory(t *testing.T) {
	gdb := testDatabase(t)
	store := newFakeCacheStore()
	manager := newFakeCacheManager(t, store)
	engine := newBizCacheEngine(t, gdb, manager, newFakeCatalogClient(t, standardCatalog()))
	token := seedCatalog(t, engine, gdb)

	// 预热分类树与商品目录。
	categories := adminCategories(t, engine, token)
	memberCatalog(t, engine)
	if !store.has(cacheKeyAdminCategories) || !store.has(cacheKeyProductsCatalog) {
		t.Fatalf("前置：两族都应已入缓存，实际键: %v", store.keys())
	}
	if len(categories.Items) == 0 || len(categories.Items[0].Subcategories) == 0 {
		t.Fatalf("标准目录应至少有一个主分类与子分类: %+v", categories)
	}
	subID := categories.Items[0].Subcategories[0].ID

	putCategory(t, engine, token, subID, map[string]any{"name": "改名后的子分类"})
	if store.has(cacheKeyAdminCategories) || store.has(cacheKeyProductsCatalog) {
		t.Fatalf("改分类后分类树与商品目录都应失效，实际键: %v", store.keys())
	}

	after := adminCategories(t, engine, token)
	if after.Items[0].Subcategories[0].Name != "改名后的子分类" {
		t.Fatalf("分类树应立即反映改名: %+v", after.Items[0].Subcategories[0])
	}
	catalog, raw := memberCatalog(t, engine)
	if !strings.Contains(raw, "改名后的子分类") {
		t.Fatal("商品目录（会员端分类树）应立即反映改名")
	}
	if catalog.Total == 0 {
		t.Fatal("改名不应影响商品数量")
	}
}

// TestBizCacheWriteInvalidationGroupMapAndSiteSettings 覆盖映射与站点设置两条失效点。
func TestBizCacheWriteInvalidationGroupMapAndSiteSettings(t *testing.T) {
	gdb := testDatabase(t)
	store := newFakeCacheStore()
	manager := newFakeCacheManager(t, store)
	engine := newBizCacheEngine(t, gdb, manager, newFakeCatalogClient(t, standardCatalog()))
	token := seedCatalog(t, engine, gdb)

	// 站点设置：先读（缓存「未配置」），写新地址后立即生效。
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/site", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("站点设置读取失败: %s", toJSON(t, envelope))
	}
	if !store.has(cacheKeySiteSettings) {
		t.Fatal("站点设置应已入缓存")
	}
	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/site", token,
		map[string]any{"url": "https://cache.example.com"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("保存站点地址失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if store.has(cacheKeySiteSettings) {
		t.Fatal("改站点地址后读取缓存应失效")
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/site", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("站点设置重读失败: HTTP %d", rec.Code)
	}
	if view := decodeData[siteSettingsView](t, envelope); view.URL != "https://cache.example.com" {
		t.Fatalf("改地址后应立即读到新值: %+v", view)
	}

	// 分组映射：先读（缓存），upsert 后立即失效且新值立刻可见。
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/product-group-maps", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("映射列表失败: %s", toJSON(t, envelope))
	}
	if !store.has(cacheKeyAdminGroupMaps) {
		t.Fatal("映射列表应已入缓存")
	}
	categories := adminCategories(t, engine, token)
	subID := categories.Items[0].Subcategories[0].ID
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/product-group-maps/1", token,
		map[string]any{"category_id": subID})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("保存映射失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if store.has(cacheKeyAdminGroupMaps) {
		t.Fatal("改映射后映射列表缓存应失效")
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/product-group-maps", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("映射列表重读失败: HTTP %d", rec.Code)
	}
	list := decodeData[groupMapListView](t, envelope)
	found := false
	for _, item := range list.Items {
		if item.UpstreamGroupID == 1 && item.CategoryID == subID {
			found = true
		}
	}
	if !found {
		t.Fatalf("新映射应立即出现在列表里: %+v", list.Items)
	}
}

// TestBizCacheUpstreamHealthCachedAndInvalidatedBySettings 覆盖上游探活结果：
// 短 TTL 内命中**不再打上游**；改上游凭据后立即失效并重新探活（批次实测踩到的失效点）。
func TestBizCacheUpstreamHealthCachedAndInvalidatedBySettings(t *testing.T) {
	gdb := testDatabase(t)
	server := newFakeUpstreamServer(t, "jwt-cache")
	store := newFakeCacheStore()
	manager := newFakeCacheManager(t, store)
	engine := newBizCacheEngineWithUpstreamBase(t, gdb, manager, server.baseURL())
	token := stage4AdminToken(t, engine, gdb, "bizcachehealth", model.RoleAdmin)

	putUpstreamSettings(t, engine, token, map[string]any{
		"username": "13800000000", "api_key": "first-upstream-key", "timeout_seconds": 5,
	})
	health := upstreamHealth(t, engine, token)
	if !health.Connected || health.BaseURL != server.baseURL() {
		t.Fatalf("探活应连通: %+v", health)
	}
	if credits := server.creditCount(); credits != 1 {
		t.Fatalf("首次探活应真实打上游：credit 次数 = %d，期望 1", credits)
	}

	// TTL 内再次探活：命中缓存，不再打上游（上游只读数据的短 TTL 缓存）。
	if health = upstreamHealth(t, engine, token); !health.Connected {
		t.Fatalf("第二次探活应命中缓存: %+v", health)
	}
	if credits := server.creditCount(); credits != 1 {
		t.Fatalf("TTL 内再次探活不应打上游：credit 次数 = %d，期望 1", credits)
	}

	// 改凭据 → 立即失效（掩码与连通性都随设置变化），下一次探活重新打上游。
	putUpstreamSettings(t, engine, token, map[string]any{"api_key": "second-upstream-key"})
	if store.has(cacheKeyUpstreamHealth) {
		t.Fatal("改上游凭据后探活缓存应失效")
	}
	health = upstreamHealth(t, engine, token)
	if !health.Connected {
		t.Fatalf("改凭据后探活应立即生效: %+v", health)
	}
	if credits := server.creditCount(); credits != 2 {
		t.Fatalf("改凭据后应重新探活：credit 次数 = %d，期望 2", credits)
	}
}

// TestBizCacheInvalidationIsFamilyScoped 覆盖失效粒度：
// 改商品只清 products: 族，categories: / settings:site / upstream:health 一个都不动。
func TestBizCacheInvalidationIsFamilyScoped(t *testing.T) {
	gdb := testDatabase(t)
	store := newFakeCacheStore()
	manager := newFakeCacheManager(t, store)
	engine := newBizCacheEngine(t, gdb, manager, newFakeCatalogClient(t, standardCatalog()))
	token := seedCatalog(t, engine, gdb)
	productID := productIDByUpstreamPID(t, gdb, 101)

	// 预热四个键族。
	memberCatalog(t, engine)
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/categories", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("分类树失败: %s", toJSON(t, envelope))
	}
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/site", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("站点设置失败: %s", toJSON(t, envelope))
	}
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/upstream/health", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("上游探活失败: %s", toJSON(t, envelope))
	}

	updateProductOK(t, engine, token, productID, map[string]any{"sort": 5})

	if store.has(cacheKeyProductsCatalog) {
		t.Fatal("改商品应清掉 products: 族")
	}
	for _, key := range []string{cacheKeyAdminCategories, cacheKeySiteSettings, cacheKeyUpstreamHealth} {
		if !store.has(key) {
			t.Fatalf("改商品不应动别的键族：%q 被清掉了（当前键: %v）", key, store.keys())
		}
	}
}

// ---------------------------------------------------------------------------
// 三、TTL 兜底（过期后回源）
// ---------------------------------------------------------------------------

// TestBizCacheExpiredEntryRefetches 覆盖 TTL 兜底：条目过期后再读重新回源——
// 即使写操作没有经过接口（绕过主动失效，例如外部直接改库）。
func TestBizCacheExpiredEntryRefetches(t *testing.T) {
	gdb := testDatabase(t)
	store := newFakeCacheStore()
	manager := newFakeCacheManager(t, store)
	engine := newBizCacheEngine(t, gdb, manager, newFakeCatalogClient(t, standardCatalog()))
	seedCatalog(t, engine, gdb)

	before, _ := memberCatalog(t, engine)
	if before.Total != 3 {
		t.Fatalf("前置：标准目录应有 3 个商品，得到 %d", before.Total)
	}

	// 绕过接口直接改库（不触发主动失效）：TTL 未到时仍读缓存的旧值（这正是缓存生效的证据）。
	if err := gdb.Model(&model.Product{}).Where("upstream_pid = ?", 102).
		Update("status", model.ProductStatusOff).Error; err != nil {
		t.Fatalf("直接改库失败: %v", err)
	}
	if cached, _ := memberCatalog(t, engine); cached.Total != 3 {
		t.Fatalf("TTL 未到时应命中缓存（仍是 3 个商品），得到 %d", cached.Total)
	}

	// 时间到了（TTL 过期）：下一次读回源，拿到库里的新值。
	store.expireAll()
	refetched, _ := memberCatalog(t, engine)
	if refetched.Total != 2 {
		t.Fatalf("TTL 过期后应回源拿到新值（2 个商品），得到 %d", refetched.Total)
	}
}

// ---------------------------------------------------------------------------
// 四、安全红线：会员私有数据绝不进缓存（不串号）
// ---------------------------------------------------------------------------

// TestBizCacheNeverCachesMemberPrivateData 覆盖 24.6 的不缓存清单：
// 两个会员交替读自己的私有视图（资料 / 实例 / 订单 / 余额 / 未读通知 / 工单）时，
// 响应各自正确，且缓存里**一个键都不新增**——按 URL 入键就会串号，所以干脆不缓存这类数据。
func TestBizCacheNeverCachesMemberPrivateData(t *testing.T) {
	gdb := testDatabase(t)
	store := newFakeCacheStore()
	manager := newFakeCacheManager(t, store)
	engine := newBizCacheEngine(t, gdb, manager, newFakeCatalogClient(t, standardCatalog()))

	aliceToken, alice := memberTokenFor(t, engine, "bizcachealice")
	bobToken, bob := memberTokenFor(t, engine, "bizcachebob")
	if alice.ID == bob.ID {
		t.Fatalf("两个会员应是不同账号: %d/%d", alice.ID, bob.ID)
	}

	privateEndpoints := []string{
		"/api/v1/members/me",
		"/api/v1/instances",
		"/api/v1/orders",
		"/api/v1/finance/balance",
		"/api/v1/notifications/unread-count",
		"/api/v1/tickets",
	}
	// 交错请求：任何「按 URL 入键」的实现都会在这里把 alice 的数据发给 bob。
	for _, endpoint := range privateEndpoints {
		if rec, envelope := doAPI(t, engine, http.MethodGet, endpoint, aliceToken, nil); rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
			t.Fatalf("alice 读 %s 失败: HTTP %d, body=%s", endpoint, rec.Code, rec.Body.String())
		}
		if rec, envelope := doAPI(t, engine, http.MethodGet, endpoint, bobToken, nil); rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
			t.Fatalf("bob 读 %s 失败: HTTP %d, body=%s", endpoint, rec.Code, rec.Body.String())
		}
	}
	if keys := store.keys(); len(keys) != 0 {
		t.Fatalf("会员私有读路径不应写任何缓存键，实际键: %v", keys)
	}

	// 资料接口仍然各自返回本人（不因缓存串号）。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/members/me", bobToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("bob 资料读取失败: HTTP %d", rec.Code)
	}
	if me := decodeData[memberView](t, envelope); me.Username != bob.Username {
		t.Fatalf("bob 应拿到自己的资料: %+v", me)
	}

	// 公开目录（允许缓存的路径）的缓存体里绝不能出现会员维度内容。
	memberCatalog(t, engine)
	payload, ok := store.valueOf(cacheKeyProductsCatalog)
	if !ok {
		t.Fatalf("商品目录应已入缓存，实际键: %v", store.keys())
	}
	for _, forbidden := range []string{"member_id", "token", "password", alice.Username, bob.Username, "@"} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("公开目录缓存里不允许出现会员维度内容 %q: %s", forbidden, payload)
		}
	}
}

// ---------------------------------------------------------------------------
// 五、缓存故障不影响业务
// ---------------------------------------------------------------------------

// TestBizCacheDriverFailureDoesNotBreakBusiness 覆盖「驱动报错时接口照常返回正确数据」：
// 读失败按未命中处理、写失败与失效失败只记日志——响应内容与错误码都不受缓存影响。
func TestBizCacheDriverFailureDoesNotBreakBusiness(t *testing.T) {
	gdb := testDatabase(t)
	store := newFakeCacheStore()
	manager := newFakeCacheManager(t, store)
	engine := newBizCacheEngine(t, gdb, manager, newFakeCatalogClient(t, standardCatalog()))
	token := seedCatalog(t, engine, gdb)
	productID := productIDByUpstreamPID(t, gdb, 101)

	// 驱动全线报错：读失败按未命中、写失败与删除失败都被吞掉。
	store.fail(errors.New("驱动读失败"), errors.New("驱动写失败"), errors.New("驱动删除失败"))

	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/products", "", nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("驱动报错时商品目录应照常返回: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if catalog := decodeData[memberProductListView](t, envelope); catalog.Total != 3 {
		t.Fatalf("驱动报错时应回源拿到完整数据，得到 total=%d", catalog.Total)
	}
	if rec, _ = doAPI(t, engine, http.MethodGet, "/api/v1/products/"+itoa(productID), "", nil); rec.Code != http.StatusOK {
		t.Fatalf("驱动报错时商品详情应照常返回: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if _, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/site", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("驱动报错时站点设置读取应照常返回: %s", toJSON(t, envelope))
	}
	if _, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/upstream/health", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("驱动报错时探活应照常返回: %s", toJSON(t, envelope))
	}

	// 写操作（含主动失效）同样不受影响：返回成功，数据立即生效。
	updateProductOK(t, engine, token, productID, map[string]any{"status": model.ProductStatusOff})
	catalog, _ := memberCatalog(t, engine)
	for _, group := range catalog.Groups {
		for _, product := range group.Products {
			if product.ID == productID {
				t.Fatal("驱动报错也不应影响写操作的真实结果")
			}
		}
	}
}

// TestBizCacheManagerWithoutDriverStillServes 覆盖「缓存层完全不可用」：
// 文件缓存目录都建不出来时（Manager 停用），业务读路径照常回源返回正确数据。
func TestBizCacheManagerWithoutDriverStillServes(t *testing.T) {
	gdb := testDatabase(t)
	manager := cache.NewManager(cache.Options{
		Logger:      silentLogger(),
		FileFactory: func(string) (cache.Store, error) { return nil, errors.New("目录不可用") },
	})
	t.Cleanup(func() { _ = manager.Close() })
	engine := newBizCacheEngine(t, gdb, manager, newFakeCatalogClient(t, standardCatalog()))
	token := seedCatalog(t, engine, gdb)

	if rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/products", "", nil); rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("缓存层不可用时应回源并照常返回: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec, _ := doAPI(t, engine, http.MethodGet, "/api/v1/admin/product-groups", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("缓存层不可用时分组树应照常返回: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}
