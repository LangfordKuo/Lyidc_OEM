package router

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/cache"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 缓存基础设施的接口（R19 批 1，契约 24.3 / 24.4）——**全部仅 admin**：
//
//	GET  /api/v1/admin/settings/cache   缓存设置（驱动选择 + Redis 参数 + 启用状态）
//	PUT  /api/v1/admin/settings/cache   保存缓存设置（校验不过 40002 且**不落库**）
//	GET  /api/v1/admin/cache/status     运行状态（驱动 / 降级与原因 / 键数量 / 命中率 / 连接信息）
//	POST /api/v1/admin/cache/reset-stats 重置命中统计（进程内，重启本就归零）
//	POST /api/v1/admin/cache/flush      清空缓存（排障用，**只清缓存、不碰业务数据**）
//
// 生效方式与仓库既有设置一致：每次读写设置都读一次 settings 表并应用到缓存管理器
// （配置未变且未降级时无操作），**改完立即生效、不用重启**；
// 状态接口顺带是 Redis 的「下次探活」自愈时机（降级期间重新 PING，恢复可用即切回，契约 24.5）。
//
// 密钥口径：Redis 口令与其他密钥一致——GET 只回配置标志与掩码，日志与响应不含明文。

// cacheHandler 处理缓存设置与状态接口。
type cacheHandler struct {
	store   *store.Store
	reader  *settings.Reader
	manager *cache.Manager
	logger  *slog.Logger
}

// cacheRedisView 是 Redis 参数的对外视图（口令只输出配置标志与掩码）。
type cacheRedisView struct {
	Host               string `json:"host"`
	Port               int    `json:"port"`
	PasswordConfigured bool   `json:"password_configured"`
	PasswordMasked     string `json:"password_masked"`
	DB                 int    `json:"db"`
	KeyPrefix          string `json:"key_prefix"`
	TimeoutSeconds     int    `json:"timeout_seconds"`
}

// cacheSettingsUpdateRequest 是 PUT /admin/settings/cache 请求体（契约 24.3）：
// 字段均可选但至少一个；redis.password 三态（省略 = 保持、新值 = 替换、空串 = 清空）。
type cacheSettingsUpdateRequest struct {
	Driver *string                  `json:"driver"`
	Redis  *cacheRedisUpdateRequest `json:"redis"`
}

// cacheRedisUpdateRequest 是 Redis 参数的局部更新（nil = 不修改）。
type cacheRedisUpdateRequest struct {
	Host           *string `json:"host"`
	Port           *int    `json:"port"`
	Password       *string `json:"password"`
	DB             *int    `json:"db"`
	KeyPrefix      *string `json:"key_prefix"`
	TimeoutSeconds *int    `json:"timeout_seconds"`
}

// provided 判断请求是否至少提供了一个字段（redis 对象在场即算提供了字段）。
func (r cacheSettingsUpdateRequest) provided() bool {
	return r.Driver != nil || r.Redis != nil
}

// cacheSettingsView 是缓存设置的对外视图（契约 24.3）：
// 配置值（driver / redis / 启用状态）+ 当前运行状态（生效驱动 / 降级与原因 / 文件目录）。
type cacheSettingsView struct {
	Driver string `json:"driver"`
	// RedisEnabled 是「启用状态」：driver=redis 时为 true（Redis 默认关闭）。
	RedisEnabled    bool   `json:"redis_enabled"`
	EffectiveDriver string `json:"effective_driver"`
	Degraded        bool   `json:"degraded"`
	DegradedReason  string `json:"degraded_reason"`
	// FileDir 是文件缓存目录（无论当前是否生效都给出，便于排障）。
	FileDir   string         `json:"file_dir"`
	Redis     cacheRedisView `json:"redis"`
	UpdatedBy *uint64        `json:"updated_by"`
	UpdatedAt *string        `json:"updated_at"`
}

// cacheStatusView 是 GET /admin/cache/status 的 data（契约 24.4）。
type cacheStatusView struct {
	Driver          string `json:"driver"`
	EffectiveDriver string `json:"effective_driver"`
	Degraded        bool   `json:"degraded"`
	DegradedReason  string `json:"degraded_reason"`
	Keys            int64  `json:"keys"`
	Hits            int64  `json:"hits"`
	Misses          int64  `json:"misses"`
	// HitRate 是进程内累计命中率（百分比，两位小数）；重启归零、可重置。
	HitRate    float64              `json:"hit_rate"`
	StatsSince string               `json:"stats_since"`
	File       cacheFileView        `json:"file"`
	Redis      cacheStatusRedisView `json:"redis"`
}

// cacheFileView 是文件缓存信息（目录）。
type cacheFileView struct {
	Dir string `json:"dir"`
}

// cacheStatusRedisView 是 Redis 连接信息（**不含口令**；版本与连接耗时来自最近一次成功探活）。
type cacheStatusRedisView struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	DB             int    `json:"db"`
	KeyPrefix      string `json:"key_prefix"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Version        string `json:"version"`
	LatencyMS      int64  `json:"latency_ms"`
}

// cacheStatsView 是重置统计的响应（契约 24.4）：返回归零后的计数。
type cacheStatsView struct {
	Hits       int64   `json:"hits"`
	Misses     int64   `json:"misses"`
	HitRate    float64 `json:"hit_rate"`
	StatsSince string  `json:"stats_since"`
}

// cacheFlushView 是清空缓存的响应（契约 24.4）：返回清空后的键数量（0）与生效驱动。
type cacheFlushView struct {
	Driver  string `json:"driver"`
	Flushed bool   `json:"flushed"`
	Keys    int64  `json:"keys"`
}

// getCacheSettings 处理 GET /api/v1/admin/settings/cache（契约 24.3）：
// 配置值 + 当前运行状态；同时是一次自愈时机（降级期间重试 Redis）。
func (h *cacheHandler) getCacheSettings(c *gin.Context) {
	if !h.configured(c) {
		return
	}
	state, err := h.reader.Cache(c.Request.Context())
	if err != nil {
		h.failSettings(c, err)
		return
	}
	response.Success(c, cacheSettingsViewOf(state.Value, h.manager.Status(c.Request.Context()), state.Meta))
}

// updateCacheSettings 处理 PUT /api/v1/admin/settings/cache（契约 24.3）：
// 校验（驱动取值 / host / 端口 / 数据库序号 / 键前缀 / 超时）不过一律 40002 且**不落库**；
// 保存成功即应用到缓存管理器（立即生效，Redis 会先探活，不可用则标红降级）。
func (h *cacheHandler) updateCacheSettings(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req cacheSettingsUpdateRequest
	if !bindJSON(c, &req) {
		return
	}
	if !req.provided() {
		response.Fail(c, response.CodeInvalidParam,
			"至少提供一个字段：driver / redis（redis 内可含 host / port / password / db / key_prefix / timeout_seconds）")
		return
	}

	ctx := c.Request.Context()
	current, err := h.reader.Cache(ctx)
	if err != nil {
		h.failSettings(c, err)
		return
	}
	merged, err := current.Value.Apply(settings.CacheUpdate{
		Driver: req.Driver,
		Redis:  cacheRedisUpdateOf(req.Redis),
	})
	if err != nil {
		// 契约 24.3：缓存设置的一切校验失败都是 40002 + 可读原因，且不落库。
		response.Fail(c, response.CodeValidationFailed, err.Error())
		return
	}

	encoded, err := merged.Encode()
	if err != nil {
		failInternal(c, h.logger, err)
		return
	}
	setting, err := h.store.UpsertSetting(ctx, settings.KeyCache, encoded, admin.ID)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	// 立即生效：配置变化时重建驱动（Redis 探活失败即降级到文件缓存，业务不中断）。
	h.manager.Configure(ctx, cacheConfigOf(merged))
	status := h.manager.Status(ctx)

	h.logger.Info("后台设置已更新", "key", settings.KeyCache, "admin_id", admin.ID,
		"driver", merged.NormalizedDriver(), "redis_host", merged.Redis.Host,
		"redis_port", merged.PortOr(), "password_changed", req.passwordChanged(),
		"degraded", status.Degraded, "degraded_reason", status.DegradedReason)
	// R18：操作日志摘要——Redis 口令明文绝不落库（只记是否更换）。
	setOplogSummary(c, fmt.Sprintf("已保存缓存设置（driver=%s，Redis=%s:%d，更换口令=%t）",
		merged.NormalizedDriver(), merged.Redis.Host, merged.PortOr(), req.passwordChanged()))
	response.Success(c, cacheSettingsViewOf(merged, status, settingMeta(setting)))
}

// getCacheStatus 处理 GET /api/v1/admin/cache/status（契约 24.4）：
// 先探活（降级期间重试 Redis = 自愈），再返回驱动、降级与原因、键数量、命中率与连接信息。
func (h *cacheHandler) getCacheStatus(c *gin.Context) {
	if !h.configured(c) {
		return
	}
	response.Success(c, cacheStatusViewOf(h.manager.Status(c.Request.Context())))
}

// resetCacheStats 处理 POST /api/v1/admin/cache/reset-stats（契约 24.4）：
// 归零进程内命中统计（重启本就归零），返回归零后的计数。
func (h *cacheHandler) resetCacheStats(c *gin.Context) {
	h.manager.ResetStats()
	stats := h.manager.Stats()
	h.logger.Info("缓存命中统计已重置")
	setOplogSummary(c, "已重置缓存命中统计")
	response.Success(c, cacheStatsView{
		Hits:       stats.Hits,
		Misses:     stats.Misses,
		HitRate:    stats.HitRate,
		StatsSince: formatTime(stats.Since),
	})
}

// flushCache 处理 POST /api/v1/admin/cache/flush（契约 24.4）：
// 清空缓存（只清缓存、不碰业务数据；Redis 只清本系统键前缀下的键），返回清空后的键数量。
func (h *cacheHandler) flushCache(c *gin.Context) {
	if !h.configured(c) {
		return
	}
	ctx := c.Request.Context()
	if err := h.manager.Flush(ctx); err != nil {
		failInternal(c, h.logger, err)
		return
	}
	status := h.manager.Status(ctx)
	h.logger.Info("缓存已清空", "driver", status.EffectiveDriver)
	setOplogSummary(c, fmt.Sprintf("已清空缓存（驱动：%s）", status.EffectiveDriver))
	response.Success(c, cacheFlushView{
		Driver:  status.EffectiveDriver,
		Flushed: true,
		Keys:    status.Keys,
	})
}

// configured 读一次缓存设置并应用到管理器（读时校验、按内容失效，与仓库既有设置同口径）：
// 配置未变且未降级时内部无操作；失败时写出错误响应并返回 false。
func (h *cacheHandler) configured(c *gin.Context) bool {
	state, err := h.reader.Cache(c.Request.Context())
	if err != nil {
		h.failSettings(c, err)
		return false
	}
	h.manager.Configure(c.Request.Context(), cacheConfigOf(state.Value))
	return true
}

// failSettings 处理设置读取失败：库内值损坏按内部错误上报，其余按数据库错误。
func (h *cacheHandler) failSettings(c *gin.Context, err error) {
	if errors.Is(err, settings.ErrCorrupt) {
		failInternal(c, h.logger, err)
		return
	}
	failDB(c, h.logger, err)
}

// passwordChanged 判断本次请求是否涉及口令变更（三态：省略 = 没动，给值 / 空串 = 变更）。
func (r cacheSettingsUpdateRequest) passwordChanged() bool {
	return r.Redis != nil && r.Redis.Password != nil
}

// cacheRedisUpdateOf 把请求视图转成设置层的更新意图（nil 一律保留）。
func cacheRedisUpdateOf(req *cacheRedisUpdateRequest) *settings.CacheRedisUpdate {
	if req == nil {
		return nil
	}
	return &settings.CacheRedisUpdate{
		Host:           req.Host,
		Port:           req.Port,
		Password:       req.Password,
		DB:             req.DB,
		KeyPrefix:      req.KeyPrefix,
		TimeoutSeconds: req.TimeoutSeconds,
	}
}

// cacheConfigOf 把设置值映射成缓存运行时配置。
// 与 upstream 的映射同口径：映射放在路由层，避免 settings 包反向依赖 cache 包。
func cacheConfigOf(value settings.Cache) cache.Config {
	return cache.Config{
		Driver: value.NormalizedDriver(),
		Redis: cache.RedisConfig{
			Host:      value.Redis.Host,
			Port:      value.PortOr(),
			Password:  value.Redis.Password,
			DB:        value.Redis.DB,
			KeyPrefix: value.KeyPrefixOr(),
			Timeout:   time.Duration(value.TimeoutSecondsOr()) * time.Second,
		},
	}
}

// cacheSettingsViewOf 组装缓存设置视图（口令只输出配置标志与掩码）。
func cacheSettingsViewOf(value settings.Cache, status cache.Status, meta settings.Meta) cacheSettingsView {
	view := cacheSettingsView{
		Driver:          value.NormalizedDriver(),
		RedisEnabled:    value.RedisEnabled(),
		EffectiveDriver: status.EffectiveDriver,
		Degraded:        status.Degraded,
		DegradedReason:  status.DegradedReason,
		FileDir:         status.FileDir,
		Redis: cacheRedisView{
			Host:               value.Redis.Host,
			Port:               value.PortOr(),
			PasswordConfigured: value.Redis.Password != "",
			PasswordMasked:     settings.MaskSecret(value.Redis.Password),
			DB:                 value.Redis.DB,
			KeyPrefix:          value.KeyPrefixOr(),
			TimeoutSeconds:     value.TimeoutSecondsOr(),
		},
		UpdatedBy: meta.UpdatedBy,
	}
	if meta.UpdatedAt != nil {
		formatted := formatTime(*meta.UpdatedAt)
		view.UpdatedAt = &formatted
	}
	return view
}

// cacheStatusViewOf 组装状态视图（Redis 信息不含口令）。
func cacheStatusViewOf(status cache.Status) cacheStatusView {
	return cacheStatusView{
		Driver:          status.Driver,
		EffectiveDriver: status.EffectiveDriver,
		Degraded:        status.Degraded,
		DegradedReason:  status.DegradedReason,
		Keys:            status.Keys,
		Hits:            status.Hits,
		Misses:          status.Misses,
		HitRate:         status.HitRate,
		StatsSince:      formatTime(status.StatsSince),
		File:            cacheFileView{Dir: status.FileDir},
		Redis: cacheStatusRedisView{
			Host:           status.Redis.Host,
			Port:           status.Redis.Port,
			DB:             status.Redis.DB,
			KeyPrefix:      status.Redis.KeyPrefix,
			TimeoutSeconds: int(status.Redis.Timeout / time.Second),
			Version:        status.Redis.Version,
			LatencyMS:      status.Redis.Latency.Milliseconds(),
		},
	}
}
