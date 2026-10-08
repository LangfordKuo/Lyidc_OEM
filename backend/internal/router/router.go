// Package router 集中注册 HTTP 路由与中间件。
package router

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
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
}

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

	apiV1 := engine.Group("/api/v1")
	{
		apiV1.GET("/health", healthHandler(opts))
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
