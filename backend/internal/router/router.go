// Package router 集中注册 HTTP 路由与中间件。
package router

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/auth"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// PingFunc 探测依赖组件（数据库）连通性；返回 nil 表示正常。
type PingFunc func(ctx context.Context) error

// Options 是路由构造参数。
type Options struct {
	Logger *slog.Logger
	// Ping 为 nil 时视为数据库未配置，健康检查返回 db=down。
	Ping PingFunc
	// HealthTimeout 是健康检查探测数据库的超时，缺省 2s。
	HealthTimeout time.Duration
	// DB 是账号体系（阶段 1）使用的数据库句柄；nil 表示启动时数据库不可用，
	// 此时账号接口返回 code=50001（数据库错误）。
	DB *gorm.DB
	// JWT 是 token 签发/校验配置；零值回退到开发默认密钥与 7 天有效期。
	JWT config.JWTConfig
	// Upstream 是上游客户端（阶段 2）；nil 表示未配置上游，
	// /api/v1/admin/upstream/health 会返回 connected=false + error="上游未配置"。
	Upstream *upstream.Client
	// UpstreamTimeout 是上游探活的整体超时，缺省 5s。
	UpstreamTimeout time.Duration
}

// defaultUpstreamProbeTimeout 是上游探活缺省超时。
const defaultUpstreamProbeTimeout = 5 * time.Second

// New 创建 gin 引擎并注册全部路由。
func New(opts Options) *gin.Engine {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.HealthTimeout <= 0 {
		opts.HealthTimeout = 2 * time.Second
	}

	engine := gin.New()
	engine.HandleMethodNotAllowed = true
	engine.Use(gin.Recovery(), requestLogger(opts.Logger))

	st := store.New(opts.DB)
	tokens := auth.NewTokenManager(opts.JWT)
	members := &memberHandler{store: st, tokens: tokens, logger: opts.Logger}
	admins := &adminHandler{store: st, tokens: tokens, logger: opts.Logger}
	products := &productHandler{
		store:    st,
		upstream: opts.Upstream,
		logger:   opts.Logger,
	}
	mw := &middleware{store: st, tokens: tokens}

	apiV1 := engine.Group("/api/v1")
	{
		apiV1.GET("/health", healthHandler(opts))

		// 会员端：注册与登录开放，其余需要会员 token。
		apiV1.POST("/auth/register", members.register)
		apiV1.POST("/auth/login", members.login)

		memberGroup := apiV1.Group("/members", mw.requireMember())
		{
			memberGroup.GET("/me", members.me)
			memberGroup.PUT("/me", members.updateMe)
			memberGroup.POST("/me/password", members.changePassword)
		}

		// 商品目录（阶段 3a）：只读且无需鉴权——商品与价格对访客可见，
		// 阶段 4（下单/支付）才要求会员 token。只返回已上架商品。
		apiV1.GET("/products", products.listMemberProducts)
		apiV1.GET("/products/:id", products.getMemberProduct)

		// 管理端：登录开放，其余需要管理员 token；
		// 改状态类接口额外要求角色为 admin 或 finance（support 返回 403）。
		apiV1.POST("/admin/auth/login", admins.login)

		adminGroup := apiV1.Group("/admin", mw.requireAdmin())
		{
			adminGroup.GET("/profile", admins.profile)
			adminGroup.GET("/members", admins.listMembers)
			adminGroup.PUT("/members/:id/status",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), admins.updateMemberStatus)

			// 上游探活：只读调用上游验证连通性（未配置上游时返回 connected=false）。
			upstreamGroup := adminGroup.Group("/upstream")
			{
				upstreamGroup.GET("/health",
					upstreamHealthHandler(opts.Upstream, opts.UpstreamTimeout))
			}

			// 商品与计费（阶段 3a）：所有角色可查看；导入/改定价/上下架/改分组要求 admin 或 finance。
			adminGroup.GET("/products", products.listProducts)
			adminGroup.GET("/products/:id", products.getProduct)
			adminGroup.POST("/products/import",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), products.importProducts)
			adminGroup.PUT("/products/:id",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), products.updateProduct)

			adminGroup.GET("/product-groups", products.listProductGroups)
			adminGroup.PUT("/product-groups/:id",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), products.updateProductGroup)
		}
	}

	engine.NoRoute(notFoundHandler)
	engine.NoMethod(notFoundHandler)
	return engine
}

// requestLogger 记录访问日志（slog）。
func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		logger.Log(c.Request.Context(), levelForStatus(c.Writer.Status()), "http 请求",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		)
	}
}

func levelForStatus(status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case status >= http.StatusBadRequest:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

func notFoundHandler(c *gin.Context) {
	response.Fail(c, response.CodeNotFound, "接口不存在")
}
