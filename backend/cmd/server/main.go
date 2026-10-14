// Command server 启动 Lyidc_OEM 后端 HTTP 服务。
//
// 启动装配（阶段 4+）：
//   - 配置加载 → 日志 → 交给 install.Supervisor 探测安装状态；
//   - **未安装**（无配置/库连不上/未建表/无管理员）时进入安装向导模式：进程照常启动，
//     浏览器访问 /install 完成数据库、建表、管理员、站点信息，全程无需编辑配置文件；
//   - 已安装时构建正常模式引擎；安装完成时 Supervisor 在进程内热切换（免重启）。
//
// 契约见 docs/api-contract.md 第 13 节。
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
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/buildinfo"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/db"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/install"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/oplog"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/router"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upgrade"
)

const (
	shutdownTimeout = 10 * time.Second
	// startupProbeTimeout 是启动时探测安装状态（含连接数据库）的超时。
	startupProbeTimeout = 15 * time.Second
)

func main() {
	// R13-B：一键升级的辅助进程入口（契约 20.6）。它在解析 flag 之前分流：helper 不读配置、
	// 不连数据库、不监听端口，只按交接文件替换文件、跑迁移、拉起新服务并做健康检查。
	if len(os.Args) > 1 && os.Args[1] == upgrade.CommandName {
		os.Exit(upgrade.RunHelperCommand(os.Args[2:]))
	}

	configPath := flag.String("config", "", "配置文件路径（缺省时按 ./config.yaml、./backend/config.yaml 顺序查找）")
	showVersion := flag.Bool("version", false, "打印版本信息后退出（不读取配置、不连接数据库）")
	flag.Parse()

	if *showVersion {
		fmt.Println("lyidc-oem server", buildinfo.Summary())
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.Log)
	slog.SetDefault(logger)
	logger.Info("版本信息", "version", buildinfo.Version, "commit", buildinfo.Commit, "built_at", buildinfo.BuiltAt)

	// R17：上游地址是构建期固定项（与版本号同一套 -ldflags 注入），运行时只读、不可改。
	// 未注入时明确提示：上游相关功能（导入/交付/实例操作/探活）都会按「未配置」处理。
	if buildinfo.UpstreamBaseURL == "" {
		logger.Warn("未注入上游地址（构建期变量 UpstreamBaseURL 为空），上游相关功能不可用",
			"how_to_fix", "构建时注入 -X <模块>/internal/buildinfo.UpstreamBaseURL=<上游地址>")
	} else {
		logger.Info("上游地址（构建期固定，不可修改）", "base_url", buildinfo.UpstreamBaseURL)
	}

	if cfg.SourcePath == "" {
		logger.Warn("未找到配置文件，使用缺省配置（将进入安装向导模式）", "searched", config.DefaultSearchPaths)
	} else {
		logger.Info("配置加载完成", "path", cfg.SourcePath)
	}
	if cfg.JWT.UsesDefaultSecret() {
		logger.Warn("jwt.secret 正在使用开发默认密钥，生产环境必须修改（安装向导完成时会自动替换为随机密钥）",
			"config_key", "jwt.secret")
	}

	gin.SetMode(ginMode(cfg.Server.Mode))

	// R18：操作日志写入器（契约 23.6：异步写入、缓冲满丢弃并记服务日志、关闭时 flush）。
	// 它绑定当前数据库句柄，因此与正常模式引擎在同一处构造；关闭服务时由下方 closeOplog 冲出缓冲。
	var oplogMu sync.Mutex
	var oplogWriter *oplog.Writer
	closeOplog := func() {
		oplogMu.Lock()
		writer := oplogWriter
		oplogWriter = nil
		oplogMu.Unlock()
		if writer != nil {
			writer.Close()
		}
	}

	// Supervisor 同时负责：安装状态机、安装页/安装 API、正常模式引擎的构建与热切换。
	supervisor, err := install.NewSupervisor(install.Options{
		Logger:     logger,
		Config:     cfg,
		ConfigPath: cfg.SourcePath,
		BuildEngine: func(gdb *gorm.DB, jwt config.JWTConfig) http.Handler {
			writer := oplog.NewWriter(oplog.Options{Store: store.New(gdb), Logger: logger})
			oplogMu.Lock()
			previous := oplogWriter
			oplogWriter = writer
			oplogMu.Unlock()
			if previous != nil {
				// 热切换重建引擎时关掉上一个写入器（其数据库句柄也会被 Supervisor 退役）。
				previous.Close()
			}
			return router.New(router.Options{
				Logger: logger,
				Ping:   db.PingFunc(gdb),
				DB:     gdb,
				JWT:    jwt,
				// R18：操作日志（管理端/会员端写操作 + 定时任务）。
				Oplog: writer,
				// R17：上游地址由构建期注入（任何界面/接口都改不了），设置里只剩账号与密钥。
				UpstreamBaseURL: buildinfo.UpstreamBaseURL,
				// 阶段 5b：生产开启「到期暂停扫描」后台任务（启动延迟 + 每日一次，契约 15.5）。
				EnableDueScan: true,
				// R13-A：生产开启「检查更新」后台定时刷新（契约 19.4）。
				EnableUpdateCheck: true,
				// R13-B：一键升级按监听地址推导健康检查地址（契约 20.6.2）。
				ServerAddr: cfg.Server.Addr,
				// R14：数据库备份恢复后需要追迁移，迁移文件含多语句，必须用带
				// multiStatements 的 DSN 新开连接（契约 21.5）。
				DatabaseDSN: cfg.Database.DSN,
			})
		},
	})
	if err != nil {
		logger.Error("初始化安装监督器失败", "error", err)
		os.Exit(1)
	}
	defer supervisor.Shutdown()

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), startupProbeTimeout)
	supervisor.Init(startupCtx)
	cancelStartup()

	// 支付渠道参数与上游对接**凭据**（账号 / 密钥 / 超时）都在后台设置（settings 表）里，
	// 由 router 按当前设置动态构造：启动时不做读取（数据库可能不可用），管理员改完设置下一次调用
	// 即生效（契约 12.1）。R17 起上游**地址**不在设置里，改走构建期注入（见上方的启动日志）。
	logger.Info("支付与上游凭据由后台设置承载", "settings_api", "/api/v1/admin/settings",
		"keys", []string{"payment.epay", "upstream"})

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           supervisor,
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

	// R18：先冲出操作日志缓冲（在 Supervisor 关闭数据库句柄之前）。
	closeOplog()

	// 数据库连接由 Supervisor 统一管理（可能与启动时不同：安装向导会热切换）。
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
