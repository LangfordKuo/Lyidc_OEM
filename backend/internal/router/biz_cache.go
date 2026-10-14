package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/cache"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
)

// 业务缓存接线（R19 批 2，契约 24.6）：把批 1 的缓存基础设施接到**与身份无关**的读路径上，
// 并在写操作成功后**按键族主动失效**（TTL 只是兜底）。
//
// 硬红线：**只缓存与用户无关的数据**。缓存键里没有任何身份维度（不带 member_id / 不带 token），
// 因此命中即复用；反过来，任何「会员私有视图」（余额、订单、实例、工单、通知、个人资料、
// 未读数）一律**不进缓存**——按 URL 入键会让不同会员之间串号（契约 24.6 的不缓存清单）。
//
// 键族（业务键；Redis 驱动会再叠加设置里的 key_prefix 命名空间）：
//
//	products:catalog                 会员端商品目录（分类树 + 子分类分组 + 已上架商品）  10 分钟
//	products:detail:<商品ID>          会员端商品详情                                   10 分钟
//	products:admin:list:<筛选指纹>    管理端商品列表（无关键词时）                       10 分钟
//	products:admin:detail:<商品ID>    管理端商品详情                                   10 分钟
//	products:admin:groups            管理端分组树 + 每个分组的商品计数（仪表盘统计口径） 10 分钟
//	categories:list                  管理端分类树 + 每个子分类的商品计数                10 分钟
//	categories:group-maps            管理端「上游分组 → 本地子分类」映射列表            10 分钟
//	settings:site                    站点设置读取（site 键，URL/名称/管理员邮箱）       10 分钟
//	upstream:health                  上游探活结果（dashboard 的连通性卡片）              1 分钟
//
// 失效点（写操作成功后整族删除，见各 handler 的 invalidate 调用）：
//
//	改商品 / 改分组 / 导入商品 → products:（含列表、详情与分组计数）
//	改分类 / 增删分组映射       → products: 与 categories:
//	改站点对外地址             → settings:site
//
// 取舍（哪些读路径**不接**）：管理端仪表盘与会员中心概览的「计数」来自会员 / 订单 / 实例 / 工单 /
// 充值 / 流水接口——这些正是契约 24.6 明确不缓存的数据（会员私有或随操作即时变化），
// 因此不为它们接线；仪表盘真正被缓存的是**商品统计**（products:admin:groups）与**上游探活**
// （upstream:health）。关键词检索（管理端商品列表带 keyword 时）基数无上限、命中率极低，同样不缓存。
//
// 两条工程口径：
//  1. **读失败 / 写失败都不改变业务结果**：Get 把驱动错误按未命中处理（Manager 内部已兜底并降级），
//     Set / DeletePrefix 的错误只记服务日志；TTL 是最后一道保险。
//  2. **配置节流应用**：业务路径不做「每个请求读一次 settings 表」（那会把省下的库查询又花回去），
//     而是最多每分钟按设置 Apply 一次（管理端保存缓存设置时仍然当场生效，见 cache_handler.go）。

// 业务缓存键。
const (
	// cacheKeyProductsCatalog 是会员端商品目录（公开读、无参数）。
	cacheKeyProductsCatalog = "products:catalog"
	// cacheKeyProductDetailPrefix 是会员端商品详情键前缀（后接商品 ID）。
	cacheKeyProductDetailPrefix = "products:detail:"
	// cacheKeyAdminProductListPrefix 是管理端商品列表键前缀（后接筛选指纹）。
	cacheKeyAdminProductListPrefix = "products:admin:list:"
	// cacheKeyAdminProductDetailPrefix 是管理端商品详情键前缀（后接商品 ID）。
	cacheKeyAdminProductDetailPrefix = "products:admin:detail:"
	// cacheKeyAdminProductGroups 是管理端分组树 + 商品计数（商品统计的仪表盘口径）。
	cacheKeyAdminProductGroups = "products:admin:groups"
	// cacheKeyAdminCategories 是管理端分类树 + 子分类商品计数。
	cacheKeyAdminCategories = "categories:list"
	// cacheKeyAdminGroupMaps 是管理端「上游分组 → 本地子分类」映射列表。
	cacheKeyAdminGroupMaps = "categories:group-maps"
	// cacheKeySiteSettings 是站点设置（site 键）的读取结果。
	cacheKeySiteSettings = "settings:site"
	// cacheKeyUpstreamHealth 是上游探活结果（短 TTL）。
	cacheKeyUpstreamHealth = "upstream:health"
)

// 按键族失效用的前缀（覆盖该族的全部键，含列表指纹键）。
const (
	cachePrefixProducts       = "products:"
	cachePrefixCategories     = "categories:"
	cachePrefixSiteSettings   = "settings:site"
	cachePrefixUpstreamHealth = "upstream:health"
)

// 各键族的 TTL（契约 24.6 的兜底口径；主动失效漏删时最坏多存活这么久）。
const (
	cacheTTLProducts       = 10 * time.Minute
	cacheTTLCategories     = 10 * time.Minute
	cacheTTLSiteSettings   = 10 * time.Minute
	cacheTTLUpstreamHealth = time.Minute
)

// cacheConfigRefreshInterval 是业务路径按设置 Apply 缓存配置的最小间隔（见文件头第 2 条）。
const cacheConfigRefreshInterval = time.Minute

// cacheInvalidateTimeout 是主动失效的独立超时：写操作已经成功，失效不该被客户端断连掐断，
// 也不该拖住响应（超时就放弃，等 TTL 兜底）。
const cacheInvalidateTimeout = 3 * time.Second

// bizCache 是业务读路径使用的缓存门面：包一层 Manager，负责按设置节流应用配置、
// 读写容错与按键族失效。字段可为 nil（测试直接构造 handler 时不接线，行为与接线前一致）。
type bizCache struct {
	manager *cache.Manager
	reader  *settings.Reader
	logger  *slog.Logger
	// dir 是文件缓存目录的进程内覆盖（**仅测试隔离用**，见 Options.CacheDir）；空串 = 缺省目录。
	dir      string
	interval time.Duration

	mu          sync.Mutex
	refreshedAt time.Time
}

// newBizCache 构造业务缓存门面。
func newBizCache(manager *cache.Manager, reader *settings.Reader, dir string, logger *slog.Logger) *bizCache {
	if logger == nil {
		logger = slog.Default()
	}
	return &bizCache{
		manager:  manager,
		reader:   reader,
		logger:   logger,
		dir:      dir,
		interval: cacheConfigRefreshInterval,
	}
}

// log 返回可用 logger（字段缺失时退回默认 logger，保证调用方无需判空）。
func (b *bizCache) log() *slog.Logger {
	if b == nil || b.logger == nil {
		return slog.Default()
	}
	return b.logger
}

// configure 按当前设置应用缓存配置，**节流到最少每隔 interval 一次**：
// 命中缓存不该再为读设置去打一次数据库；管理端保存设置时由 cache_handler 当场生效，
// 这里的节流只影响「进程外改了库 / 多实例部署」时的生效延迟（最坏一个 interval）。
// 读设置失败同样按节流记账：数据库故障时不至于每个请求都重试一次。
func (b *bizCache) configure(ctx context.Context) {
	if b == nil || b.manager == nil || b.reader == nil {
		return
	}
	b.mu.Lock()
	interval := b.interval
	if interval <= 0 {
		interval = cacheConfigRefreshInterval
	}
	if !b.refreshedAt.IsZero() && time.Since(b.refreshedAt) < interval {
		b.mu.Unlock()
		return
	}
	b.refreshedAt = time.Now()
	b.mu.Unlock()

	state, err := b.reader.Cache(ctx)
	if err != nil {
		b.log().Warn("读取缓存设置失败，本次沿用当前缓存配置", "error", err)
		return
	}
	b.manager.Configure(ctx, cacheConfigOf(state.Value, b.dir))
}

// get 读缓存：驱动错误一律按未命中处理（Manager 内部已记日志并按需降级）。
func (b *bizCache) get(ctx context.Context, key string) ([]byte, bool) {
	if b == nil || b.manager == nil {
		return nil, false
	}
	return b.manager.Get(ctx, key)
}

// set 回填缓存：失败只记服务日志（**绝不因此改变业务结果**）。
func (b *bizCache) set(ctx context.Context, key string, value []byte, ttl time.Duration) {
	if b == nil || b.manager == nil {
		return
	}
	if err := b.manager.Set(ctx, key, value, ttl); err != nil {
		b.log().Warn("缓存回填失败（不影响业务结果，等待下次回源）", "key", key, "error", err)
	}
}

// invalidate 按键族前缀主动失效（写操作成功后调用）：**尽力而为**——失败只记服务日志，
// 绝不改变业务结果（TTL 兜底）。用一个不被请求取消的上下文，避免客户端断连让失效半途而废。
func (b *bizCache) invalidate(ctx context.Context, prefixes ...string) {
	if b == nil || b.manager == nil {
		return
	}
	b.configure(ctx)
	delCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cacheInvalidateTimeout)
	defer cancel()
	for _, prefix := range prefixes {
		if err := b.manager.DeletePrefix(delCtx, prefix); err != nil {
			b.log().Warn("缓存主动失效失败（不影响业务结果，等待 TTL 过期）", "prefix", prefix, "error", err)
		}
	}
}

// cachedJSON 是业务读路径的统一接线：命中则回放缓存的 data（不再打库）；未命中执行 build，
// 成功（build 写出 code=0 响应）时把 data 原样回填。
//
// 响应形状与接线前一致：缓存里存的就是 `data` 的 JSON 字节，命中时按 json.RawMessage 原样嵌入
// 统一响应包，与 `response.Success(c, view)` 的字节一致（字段顺序与转义都由 encoding/json 固定）。
func cachedJSON[T any](c *gin.Context, b *bizCache, key string, ttl time.Duration, build func() (T, bool)) {
	ctx := c.Request.Context()
	if b != nil {
		b.configure(ctx)
		if raw, ok := b.get(ctx, key); ok && json.Valid(raw) {
			response.Success(c, json.RawMessage(raw))
			return
		}
	}

	view, ok := build()
	if !ok {
		// build 已写出失败响应（400/404/500）——失败结果**不入缓存**。
		return
	}
	payload, err := json.Marshal(view)
	if err != nil {
		failInternal(c, b.log(), err)
		return
	}
	if b != nil {
		b.set(ctx, key, payload, ttl)
	}
	response.Success(c, json.RawMessage(payload))
}

// adminProductListFingerprint 生成管理端商品列表的「筛选参数指纹」：
// 分页与全部筛选维度都进键（不含 keyword——带关键词的检索基数无上限、命中率极低，调用方不缓存）。
// 指纹是纯字符串拼接（各取值都已在处理器里校验过形状），不参与任何业务语义。
func adminProductListFingerprint(page, pageSize int, status string, groupID, categoryID *uint64) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "p%d/s%d", page, pageSize)
	if status != "" {
		builder.WriteString("/st=")
		builder.WriteString(status)
	}
	if groupID != nil {
		fmt.Fprintf(&builder, "/g=%d", *groupID)
	}
	if categoryID != nil {
		fmt.Fprintf(&builder, "/c=%d", *categoryID)
	}
	return builder.String()
}

// siteSettingsEntry 是站点设置读取结果的缓存体（契约 22.4 的读取口径）：
// 只缓存**与请求无关**的取值与审计字段；`url_recommended` 依赖当前请求 Host，不进缓存、每次现算。
type siteSettingsEntry struct {
	Value     settings.Site `json:"value"`
	Exists    bool          `json:"exists"`
	UpdatedBy *uint64       `json:"updated_by"`
	UpdatedAt *string       `json:"updated_at"`
}

// entryOf 把设置读取结果转成缓存体（UpdatedAt 按 RFC3339 预格式化，与 formatTime 同口径）。
func siteSettingsEntryOf(state settings.SiteState) siteSettingsEntry {
	entry := siteSettingsEntry{Value: state.Value, Exists: state.Meta.Exists, UpdatedBy: state.Meta.UpdatedBy}
	if state.Meta.UpdatedAt != nil {
		formatted := formatTime(*state.Meta.UpdatedAt)
		entry.UpdatedAt = &formatted
	}
	return entry
}

// viewOf 用缓存体重建对外视图（url_recommended 按当前请求现算）。
func (e siteSettingsEntry) viewOf(c *gin.Context) siteSettingsView {
	return siteSettingsView{
		URL:            e.Value.URL,
		Configured:     e.Value.URL != "",
		URLRecommended: requestBaseURL(c),
		UpdatedBy:      e.UpdatedBy,
		UpdatedAt:      e.UpdatedAt,
	}
}
