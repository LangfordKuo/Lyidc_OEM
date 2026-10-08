// Command server 启动 Lyidc_OEM 后端 HTTP 服务。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/db"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/router"
)

const shutdownTimeout = 10 * time.Second

func main() {
	configPath := flag.String("config", "", "配置文件路径（缺省时按 ./config.yaml、./backend/config.yaml 顺序查找）")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.Log)
	slog.SetDefault(logger)

	if cfg.SourcePath == "" {
		logger.Warn("未找到配置文件，使用缺省配置", "searched", config.DefaultSearchPaths)
	} else {
		logger.Info("配置加载完成", "path", cfg.SourcePath)
	}

	gin.SetMode(ginMode(cfg.Server.Mode))

	gdb, dbErr := db.Open(cfg.Database)
	if dbErr != nil {
		// 数据库不可用时不退出：/api/v1/health 会持续报告 db=down，便于定位环境问题。
		logger.Error("数据库连接失败", "error", dbErr, "dsn", config.MaskDSN(cfg.Database.DSN))
	} else {
		logger.Info("数据库连接成功", "dsn", config.MaskDSN(cfg.Database.DSN))
	}

	engine := router.New(router.Options{
		Logger: logger,
		Ping:   db.PingFunc(gdb),
	})

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("HTTP 服务启动", "addr", cfg.Server.Addr, "mode", cfg.Server.Mode)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP 服务异常退出", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("收到退出信号，开始优雅关闭")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("优雅关闭失败", "error", err)
			os.Exit(1)
		}
	}

	if gdb != nil {
		if err := db.Close(gdb); err != nil {
			logger.Warn("关闭数据库连接失败", "error", err)
		}
	}
	logger.Info("服务已退出")
}

// newLogger 按配置构造 slog 日志器，文本格式默认带时间与级别。
func newLogger(cfg config.LogConfig) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.SlogLevel()}
	if strings.EqualFold(cfg.Format, "json") {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func ginMode(mode string) string {
	switch strings.ToLower(mode) {
	case "debug":
		return gin.DebugMode
	case "test":
		return gin.TestMode
	default:
		return gin.ReleaseMode
	}
}
