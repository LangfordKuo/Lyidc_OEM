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
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/buildinfo"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/cache"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/dbbackup"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/delivery"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/instanceops"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/notify"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/oplog"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/scheduler"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/updates"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upgrade"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// PingFunc 探测依赖组件（数据库）连通性；返回 nil 表示正常。
type PingFunc func(ctx context.Context) error

// DeliveryTrigger 是订单交付能力（契约 14.3）：支付成功后的自动触发与管理员重试。
//
// 生产实现是 internal/delivery.Service（自动触发默认异步）；测试可注入
// Async=false 的 Service 使交付同步完成，从而确定性地断言落库结果。
type DeliveryTrigger interface {
	// Trigger 是支付成功（在线回调 / 余额支付）后的自动交付入口，不阻塞调用方。
	// ctx 用于携带请求侧基地址（R16：`delivery.WithRequestBase`，异步交付时请求已结束）。
	Trigger(ctx context.Context, orderID uint64)
	// Deliver 同步执行一次交付（管理员重试接口）；allowFailed 为 true 时交付失败的订单可重试。
	Deliver(ctx context.Context, orderID uint64, allowFailed bool) (*model.Order, error)
}

// NotificationTrigger 是事件通知能力（阶段 6b，契约 17.4）：router 把同一个实现
// 分发给交付链路（delivery.Notifier）、扫描器（scheduler.Notifier）、工单 handler 与设置页。
//
// 生产实现是 internal/notify.Service（事件入口内部异步、失败只记日志）；测试可注入
// Async=false 的实现使通知同步完成，从而确定性地断言通知落库与邮件收件人。
type NotificationTrigger interface {
	// 交付事件（internal/delivery 的接线点）。
	delivery.Notifier
	// 扫描事件（internal/scheduler 的接线点：暂停、终止收敛、到期提醒）。
	scheduler.Notifier

	// TicketCreated 新工单 → 通知管理员/客服。
	TicketCreated(ticketID uint64)
	// TicketRepliedByMember 会员回复工单 → 通知管理员/客服。
	TicketRepliedByMember(ticketID uint64)
	// TicketRepliedByAdmin 客服公开回复工单 → 通知会员（内部备注不调用）。
	TicketRepliedByAdmin(ticketID uint64)
	// TicketClosedByAdmin 客服关闭工单 → 通知会员。
	TicketClosedByAdmin(ticketID uint64)
	// SendTestEmail 同步发送一封测试邮件（设置页验证 SMTP 参数）。
	SendTestEmail(ctx context.Context, to string) error
}

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
	// Upstream 是上游客户端提供者；**nil 时按后台设置（settings 表 upstream 键）动态构造**
	// （生产默认路径）。测试可注入 upstream.StaticProvider 指向假上游。
	Upstream upstream.Provider
	// UpstreamBaseURL 是上游地址（R17：构建期固定，不可通过任何接口或界面修改）。
	//
	// **生产由 cmd/server 用 buildinfo.UpstreamBaseURL 填充**（与版本号同一套 -ldflags 注入）；
	// 这里是给测试保留的唯一注入点：它来自进程内的构造参数，不来自任何 HTTP 入参，
	// 因此没有任何请求能让它改变。仅当 Options.Upstream 为 nil（走默认动态构造）时生效。
	UpstreamBaseURL string
	// UpstreamTimeout 是上游探活的整体超时，缺省 5s。
	UpstreamTimeout time.Duration
	// Delivery 是订单交付器；nil 时构造默认实现（自动交付异步执行、管理员重试用当前上游设置）。
	Delivery DeliveryTrigger
	// EnableDueScan 为 true 时构造并启动「到期暂停扫描」后台任务（阶段 5b，契约 15.5）：
	// 启动延迟 DefaultInitialDelay 后首次扫描，之后每 DefaultInterval 扫描一次。
	// 生产装配（cmd/server）传 true；集成测试默认 false（扫描逻辑由 scheduler.Scanner 单元/集成测试覆盖）。
	EnableDueScan bool
	// EnableUpdateCheck 为 true 时启动「检查更新」后台定时刷新（R13-A，契约 19.4）：
	// 启动后延迟 DefaultInitialDelay 检查一次，之后每 DefaultInterval 检查一次，失败只记日志。
	// 生产装配（cmd/server）传 true；测试默认 false，检查接口仍可用（测试直接触发）。
	EnableUpdateCheck bool
	// UpdateFetcher 是检查更新访问 GitHub 的能力（R13-A）；nil 时使用默认 GitHub 客户端。
	// 测试注入假实现，避免真实出网。
	UpdateFetcher updates.Fetcher
	// Upgrade 是一键升级服务（R13-B，契约 20）；nil 时按 Options.ServerAddr 与 os.Args 构造，
	// 无法定位安装目录则升级接口明确拒绝（其余功能不受影响）。测试注入假实现避免真实替换文件。
	Upgrade *upgrade.Service
	// ServerAddr 是监听地址（R13-B 用它推导新版本的健康检查地址）；空时取 config 缺省值。
	ServerAddr string
	// DBBackup 是数据库备份服务（R14，契约 21）；nil 时按 Options.DB / InstallDir / DatabaseDSN
	// 构造（备份目录缺省为 <安装目录>/backups）。测试注入以指定临时安装目录，避免写到可执行文件目录。
	DBBackup *dbbackup.Service
	// InstallDir 是安装目录（R14 备份目录缺省值的父目录）；空时取当前可执行文件所在目录。
	InstallDir string
	// DatabaseDSN 是数据库连接串（R14）：恢复后追迁移时用它新开连接（迁移文件含多语句，
	// 需要 DSN 上的 multiStatements=true）；为空时退回应用连接池。
	DatabaseDSN string
	// Notifier 是通知能力（阶段 6b）；nil 时构造默认实现（事件入口异步、按后台设置发信）。
	// 注入的实现同时被「交付/扫描（经 Options.Delivery 与调度器）」与「工单/设置页」使用；
	// 测试注入 Async=false 的 notify.Service 即可同步断言通知结果。
	Notifier NotificationTrigger
	// Oplog 是操作日志写入器（R18，契约第 23 节）：管理端/会员端写操作与定时任务都写它。
	//
	// **nil 时用丢弃写入器**（oplog.Discard，不开后台协程、不写库）：生产由 cmd/server
	// 显式构造并注入（关闭时 Close 冲出缓冲）；测试不关心日志时无需构造，
	// 需要断言日志的测试显式注入自己的写入器（可 Flush 定序）。
	Oplog *oplog.Writer
	// Cache 是缓存管理器（R19 批 1，契约第 24 节）：两种驱动（文件 / Redis）+ 统计 + 降级。
	//
	// **nil 时构造默认实现**（构造本身不读设置也不连 Redis，由接口按当前设置应用配置）；
	// 生产由 cmd/server 显式构造并注入（引擎热切换与进程退出时 Close，停掉文件缓存的清理协程）；
	// 测试可注入带假驱动工厂的管理器，不连真实 Redis 也能覆盖降级分支。
	//
	// R19 批 2 起业务读路径也用它（契约 24.6）：业务侧按设置节流应用配置（每分钟最多一次），
	// 管理端保存缓存设置时仍然当场生效。
	Cache *cache.Manager
	// CacheDir 是**文件缓存目录的进程内覆盖**：空串 = 系统临时目录下的 lyidc-cache
	// （契约 24.1 的缺省口径，生产一律留空）。
	//
	// 唯一用途是**测试隔离**：集成测试的每个引擎用各自的临时目录，避免共享缺省目录时
	// 不同用例之间的缓存互相污染（业务接线后缓存键真的会被写）。
	CacheDir string
}

// defaultUpstreamProbeTimeout 是上游探活缺省超时。
const defaultUpstreamProbeTimeout = 5 * time.Second

// backupDirCheckTimeout 是启动时校验备份目录的超时（一次主键读取，慢也不会拖住启动）。
const backupDirCheckTimeout = 3 * time.Second

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
	mw := &middleware{store: st, tokens: tokens}

	// R18：操作日志写入器（契约第 23 节）。未注入时用丢弃写入器——日志是尽力而为的留痕，
	// 绝不因为日志问题改变业务结果；生产与日志测试都显式注入自己的写入器。
	oplogWriter := opts.Oplog
	if oplogWriter == nil {
		oplogWriter = oplog.Discard(opts.Logger)
	}

	// 阶段 4：支付渠道与上游客户端都按后台设置动态构造（契约 12.1），
	// 管理员改完设置下一次调用即生效；未配置时对应功能返回明确业务错误，其余功能不受影响。
	settingsReader := settings.NewReader(st)
	payments := newPaymentRegistry(settingsReader, opts.Logger)
	upstreamProvider := opts.Upstream
	if upstreamProvider == nil {
		upstreamProvider = newUpstreamProvider(settingsReader, opts.UpstreamBaseURL, opts.Logger)
	}
	// 阶段 6b：通知服务（站内通知 + 通知邮件），供交付/扫描/工单/设置页共用。
	// 生产实现的事件入口是异步的（不阻塞业务，失败只记日志），发送器按后台 SMTP 设置动态重建。
	notifier := opts.Notifier
	if notifier == nil {
		notifier = notify.New(st, settingsReader, notify.Options{Async: true, Logger: opts.Logger})
	}
	// 阶段 5a：支付成功后的自动交付（异步 goroutine，不阻塞回调响应，契约 14.3）。
	// 阶段 5b：同一交付器按订单 type 分流新购（开通）/续费（RenewHost）。
	// 阶段 6b：交付结果提交后触发通知（交付成功/失败、续费成功）。
	deliveries := opts.Delivery
	if deliveries == nil {
		deliveries = delivery.New(st, upstreamProvider, delivery.Options{
			Async: true, Logger: opts.Logger, Notifier: notifier,
		})
	}
	// 阶段 5b：实例操作（电源/重装/改密/暂停/恢复/同步），审计写 instance_operation_logs。
	instanceOperator := instanceops.New(st, upstreamProvider, instanceops.Options{Logger: opts.Logger})

	// 阶段 5b：到期暂停扫描（应用内后台任务；生产由 main 显式开启，契约 15.5）。
	// 阶段 6b：同一轮扫描先跑到期提醒，并在暂停/终止收敛后投递通知（契约 17.5）。
	if opts.EnableDueScan {
		scanner := scheduler.New(st, upstreamProvider, scheduler.Options{
			InitialDelay: scheduler.DefaultInitialDelay,
			Logger:       opts.Logger,
			Notifier:     notifier,
			// R18：定时任务通道——每轮扫描一条汇总 + 有实际动作时的明细（契约 23.3）。
			Oplog: oplogWriter,
		})
		scanner.Start()
		opts.Logger.Info("到期暂停扫描已启动",
			"initial_delay", scheduler.DefaultInitialDelay, "interval", scheduler.DefaultInterval)
	}

	// R13-A：系统版本与更新检查（契约 19）。检查服务始终构造（管理端接口可用）；
	// 后台定时刷新只在生产装配开启（EnableUpdateCheck），与到期扫描同一套装配方式。
	// R13-B：检查访问器按当前设置里的 api_base 构造（更新源可注入，契约 19.5）；注入固定桩时优先用它。
	updateChecker := updates.NewService(updates.Options{
		Store:    st,
		Settings: settingsReader,
		Fetcher:  opts.UpdateFetcher,
		Logger:   opts.Logger,
		FetcherFor: func(apiBase string, timeout time.Duration) updates.Fetcher {
			return updates.NewClientFor(apiBase, timeout, opts.Logger, buildinfo.Version)
		},
	})
	if opts.EnableUpdateCheck {
		updateChecker.Start()
		opts.Logger.Info("检查更新后台任务已启动",
			"initial_delay", updates.DefaultInitialDelay, "interval", updates.DefaultInterval)
	}
	// R13-B：一键升级（契约 20）。安装目录由可执行文件定位；定位失败只让升级接口不可用，
	// 其余功能照常（集成测试的二进制在临时目录里，升级服务因此通常处于「不可用」态）。
	upgradeService := opts.Upgrade
	if upgradeService == nil {
		serverAddr := opts.ServerAddr
		if serverAddr == "" {
			serverAddr = config.DefaultAddr
		}
		service, err := upgrade.NewService(upgrade.Options{
			Addr:     serverAddr,
			Checker:  updateChecker,
			Settings: settingsReader,
			Logger:   opts.Logger,
		})
		if err != nil {
			opts.Logger.Warn("一键升级不可用（无法定位安装目录）", "error", err)
		} else {
			upgradeService = service
		}
	}
	updatesAdmin := &updatesHandler{service: updateChecker, upgrade: upgradeService, logger: opts.Logger}

	// R14：数据库备份与恢复（契约 21）。安装目录缺省取可执行文件所在目录（与一键升级同一口径），
	// 备份目录缺省 <安装目录>/backups；构造失败只让备份接口不可用，其余功能照常。
	// 启动时校验一次备份目录（契约 21.4：启动与保存时都校验），不可用只记警告、不阻断启动。
	backupService := opts.DBBackup
	if backupService == nil && opts.DB != nil {
		service, err := dbbackup.NewService(dbbackup.Options{
			InstallDir: opts.InstallDir,
			DB:         opts.DB,
			DSN:        opts.DatabaseDSN,
			Settings:   settingsReader,
			Logger:     opts.Logger,
		})
		if err != nil {
			opts.Logger.Warn("数据库备份功能不可用", "error", err)
		} else {
			backupService = service
			verifyCtx, cancelVerify := context.WithTimeout(context.Background(), backupDirCheckTimeout)
			service.VerifyDir(verifyCtx)
			cancelVerify()
		}
	}
	dbBackups := &dbBackupHandler{service: backupService, logger: opts.Logger}

	// R19 批 2：缓存管理器按设置应用到业务读路径（契约 24.6）。管理器始终存在（未注入时构造默认实现），
	// 由 bizCache 负责「节流应用配置 + 读写容错 + 按键族失效」；读设置失败只记日志，业务照常。
	cacheManager := opts.Cache
	if cacheManager == nil {
		cacheManager = cache.NewManager(cache.Options{Logger: opts.Logger})
	}
	bizCacheRuntime := newBizCache(cacheManager, settingsReader, opts.CacheDir, opts.Logger)

	members := &memberHandler{store: st, tokens: tokens, logger: opts.Logger}
	admins := &adminHandler{store: st, tokens: tokens, logger: opts.Logger}
	products := &productHandler{
		store:    st,
		upstream: upstreamProvider,
		cache:    bizCacheRuntime,
		logger:   opts.Logger,
	}
	categories := &categoryHandler{store: st, cache: bizCacheRuntime, logger: opts.Logger}
	groupMaps := &groupMapHandler{store: st, cache: bizCacheRuntime, logger: opts.Logger}
	coupons := &couponHandler{store: st, logger: opts.Logger}
	orders := &orderHandler{store: st, payments: payments, deliveries: deliveries, logger: opts.Logger}
	finance := &financeHandler{store: st, payments: payments, logger: opts.Logger}
	paymentCallbacks := &paymentHandler{
		store: st, payments: payments, reader: settingsReader, deliveries: deliveries, logger: opts.Logger,
	}
	instances := &instanceHandler{store: st, ops: instanceOperator, logger: opts.Logger}
	adminSettings := &settingsHandler{
		store: st, reader: settingsReader, notify: notifier,
		// R17：上游地址是构建期注入值——GET 只读回显它，PUT 出现 base_url 一律 40002。
		upstreamBaseURL: opts.UpstreamBaseURL,
		// R15：备份目录设置接口与备份运行时共用同一个服务实例（同一套目录校验口径）。
		backups: backupService,
		// R19 批 2：站点对外地址读取（settings:site）的缓存门面。
		cache:  bizCacheRuntime,
		logger: opts.Logger,
	}
	// 阶段 6a：工单（纯本地域，不调用上游；管理端为 admin + support 的客服域）。
	// 阶段 6b：工单事件（创建/会员回复/客服公开回复/客服关闭）在事务提交后触发通知。
	tickets := &ticketHandler{store: st, notify: notifier, logger: opts.Logger}
	// 阶段 6b：站内通知收件箱（会员端与管理端同构）。
	notifications := &notificationHandler{store: st, logger: opts.Logger}
	// R10-A：邀请返佣（会员端概览/开通/流水/邀请列表，契约 18）。
	referrals := &referralHandler{store: st, reader: settingsReader, logger: opts.Logger}
	// R10-B：邀请人自建券 + 提现/转余额（含审核），契约 18.5。
	referralCoupons := &referralCouponHandler{store: st, reader: settingsReader, logger: opts.Logger}
	referralPayouts := &referralPayoutHandler{store: st, logger: opts.Logger}
	// R18：操作日志（契约第 23 节）——列表（高级搜索）与清空；清空接口自写留痕并跳过中间件默认入队。
	operationLogs := &operationLogHandler{store: st, writer: oplogWriter, logger: opts.Logger}
	// R19 批 1：缓存基础设施（契约第 24 节）——设置驱动选择与 Redis 参数、查看状态与命中率、
	// 重置统计与清空缓存（管理端运维入口，与业务读路径共用同一个管理器）。
	caches := &cacheHandler{
		store: st, reader: settingsReader, manager: cacheManager,
		dir: opts.CacheDir, logger: opts.Logger,
	}

	// R16：上游状态回推接收端（契约 22.3）——**顶层路由**，不在 /api/v1 之下：
	// 路径由上游固定拼接（<downstream_url>/api/host/sync），响应格式由上游约定（不套统一响应包）。
	hostSync := &hostSyncHandler{store: st, logger: opts.Logger}
	engine.POST("/api/host/sync", hostSync.sync)

	apiV1 := engine.Group("/api/v1")
	{
		apiV1.GET("/health", healthHandler(opts))

		// 会员端：注册与登录开放，其余需要会员 token。
		// R18：注册/登录也是会员写操作，同样留痕（登录成败都记，失败只记尝试的用户名）。
		apiV1.POST("/auth/register", recordOperation(oplogWriter, oplog.ChannelMember), members.register)
		apiV1.POST("/auth/login", recordOperation(oplogWriter, oplog.ChannelMember), members.login)

		// R18（契约 23.3）：会员通道的写操作兜底埋点——记录中间件挂在鉴权**之前**，
		// 因此未登录/被禁用（401/403）的写请求同样留痕（操作人 0/空串）。
		memberGroup := apiV1.Group("/members", recordOperation(oplogWriter, oplog.ChannelMember), mw.requireMember())
		{
			memberGroup.GET("/me", members.me)
			memberGroup.PUT("/me", members.updateMe)
			memberGroup.POST("/me/password", members.changePassword)
		}

		// 商品目录（阶段 3a）：只读且无需鉴权——商品与价格对访客可见，
		// 阶段 4（下单/支付）才要求会员 token。只返回已上架商品。
		apiV1.GET("/products", products.listMemberProducts)
		apiV1.GET("/products/:id", products.getMemberProduct)

		// 优惠码校验（阶段 3b）：公开只读接口，供下单前试算折扣；
		// 折扣的应用（扣减金额、使用记账）阶段 4 已在下单/支付链路兑现（契约 12.5）。
		apiV1.GET("/coupons/:code/validate", coupons.validateCoupon)

		// 支付回调与同步跳转（阶段 4）：渠道 → 本服务，无需鉴权——验签与金额校验是唯一凭证。
		// notify_url 由管理员在后台设置里填写（推荐 <系统域名>/api/v1/payments/epay/notify）。
		apiV1.POST("/payments/epay/notify", paymentCallbacks.epayNotify)
		apiV1.GET("/payments/epay/notify", paymentCallbacks.epayNotify)
		apiV1.GET("/payments/epay/return", paymentCallbacks.epayReturn)

		// 订单与财务（阶段 4）：需要会员 token；全部只操作**本人**数据。
		// R18：同一会员通道埋点（鉴权之前挂载，被拒写请求同样留痕）。
		memberAPI := apiV1.Group("", recordOperation(oplogWriter, oplog.ChannelMember), mw.requireMember())
		{
			memberAPI.POST("/orders", orders.createOrder)
			memberAPI.GET("/orders", orders.listOrders)
			memberAPI.GET("/orders/:id", orders.getOrder)
			memberAPI.POST("/orders/:id/pay", orders.payOrder)
			memberAPI.POST("/orders/:id/cancel", orders.cancelOrder)

			// 实例（阶段 5a）：仅本人；列表不含敏感字段，详情含主机账号密码（含敏感字段仅本人可见）。
			memberAPI.GET("/instances", instances.listMyInstances)
			memberAPI.GET("/instances/:id", instances.getMyInstance)

			// 实例操作与续费（阶段 5b）：仅本人实例；电源/重装/改密仅 active，续费 active/suspended，
			// 操作记录含全部历史（成功与失败，message 已脱敏）。
			memberAPI.POST("/instances/:id/power", instances.powerInstance)
			memberAPI.POST("/instances/:id/reinstall", instances.reinstallInstance)
			memberAPI.GET("/instances/:id/reinstall-options", instances.reinstallOptions)
			memberAPI.POST("/instances/:id/reset-password", instances.resetPasswordInstance)
			memberAPI.POST("/instances/:id/renew", instances.renewInstance)
			memberAPI.GET("/instances/:id/logs", instances.listMyInstanceLogs)

			// 取消/终止申请（阶段 5c）：仅本人；active / suspended 可申请，已有在途申请幂等返回。
			memberAPI.POST("/instances/:id/cancel", instances.cancelInstance)

			memberAPI.POST("/recharges", finance.createRecharge)
			memberAPI.GET("/recharges", finance.listRecharges)
			memberAPI.GET("/finance/balance", finance.getBalance)
			memberAPI.GET("/finance/ledger", finance.listLedger)

			// 工单（阶段 6a，契约 16）：仅本人（他人工单与不存在统一 404）；
			// 内部备注（internal=true）的消息绝不出现在会员端响应里。
			memberAPI.POST("/tickets", tickets.createTicket)
			memberAPI.GET("/tickets", tickets.listMyTickets)
			memberAPI.GET("/tickets/:id", tickets.getMyTicket)
			memberAPI.POST("/tickets/:id/reply", tickets.replyMyTicket)
			memberAPI.POST("/tickets/:id/close", tickets.closeMyTicket)

			// 站内通知（阶段 6b，契约 17.2）：**仅本人**（他人的通知与不存在统一 404）；
			// 已读操作幂等（重复已读 already_read=true）。
			memberAPI.GET("/notifications", notifications.listMyNotifications)
			memberAPI.GET("/notifications/unread-count", notifications.myUnreadCount)
			memberAPI.POST("/notifications/:id/read", notifications.readMyNotification)
			memberAPI.POST("/notifications/read-all", notifications.readAllMyNotifications)

			// 邀请返佣（R10-A，契约 18.5）：仅本人数据；开通时惰性生成邀请码。
			memberAPI.GET("/referral", referrals.getReferral)
			memberAPI.PUT("/referral", referrals.updateReferral)
			memberAPI.GET("/referral/rebates", referrals.listRebates)
			memberAPI.GET("/referral/invitees", referrals.listInvitees)

			// 邀请人自建券（R10-B，契约 18.5）：仅本人；折扣上限 = min(设置, 生效返佣比例)。
			memberAPI.POST("/referral/coupons", referralCoupons.createReferralCoupon)
			memberAPI.GET("/referral/coupons", referralCoupons.listReferralCoupons)
			memberAPI.PUT("/referral/coupons/:id", referralCoupons.updateReferralCoupon)

			// 提现 / 转余额（R10-B，契约 18.1.1）：仅本人；负数余额一律拒。
			memberAPI.POST("/referral/withdrawals", referralPayouts.createWithdrawal)
			memberAPI.GET("/referral/withdrawals", referralPayouts.listMyWithdrawals)
			memberAPI.POST("/referral/conversions", referralPayouts.createConversion)
			memberAPI.GET("/referral/conversions", referralPayouts.listMyConversions)
		}

		// 管理端：登录开放，其余需要管理员 token；
		// 改状态类接口额外要求角色为 admin 或 finance（support 返回 403）。
		// R18：登录是管理端写操作，成功与失败都留痕（失败只记尝试的用户名，不记密码）。
		apiV1.POST("/admin/auth/login",
			recordOperation(oplogWriter, oplog.ChannelAdmin), admins.login)

		// R18（契约 23.3）：管理员通道的写操作兜底埋点——记录中间件挂在鉴权**之前**，
		// 因此未登录/无权限（401/403）的写请求同样留痕；未注册路径的 404 不经过本中间件。
		adminGroup := apiV1.Group("/admin", recordOperation(oplogWriter, oplog.ChannelAdmin), mw.requireAdmin())
		{
			adminGroup.GET("/profile", admins.profile)
			adminGroup.GET("/members", admins.listMembers)
			adminGroup.PUT("/members/:id/status",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), admins.updateMemberStatus)
			// 会员更新（R10-B，契约 6.3）：本批只承载会员级返佣特批比例，
			// 权限沿用 members.status 口径（admin / finance）。
			adminGroup.PUT("/members/:id",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), admins.updateMember)

			// 上游探活：按当前后台设置只读调用上游验证连通性（未配置时返回 connected=false）。
			upstreamGroup := adminGroup.Group("/upstream")
			{
				upstreamGroup.GET("/health",
					upstreamHealthHandler(upstreamProvider, opts.UpstreamTimeout, bizCacheRuntime))
			}

			// 后台设置（阶段 4）：承载 payment.epay 与 upstream 两个键；**仅 admin 角色，含读取**
			// （finance / support 一律 403）；密钥永不回显明文（契约 12.1）。
			settingsGroup := adminGroup.Group("/settings")
			{
				settingsGroup.GET("/payment/epay",
					requireAdminRole(model.RoleAdmin), adminSettings.getEpaySettings)
				settingsGroup.PUT("/payment/epay",
					requireAdminRole(model.RoleAdmin), adminSettings.updateEpaySettings)
				settingsGroup.GET("/upstream",
					requireAdminRole(model.RoleAdmin), adminSettings.getUpstreamSettings)
				settingsGroup.PUT("/upstream",
					requireAdminRole(model.RoleAdmin), adminSettings.updateUpstreamSettings)

				// 邮件与通知开关（阶段 6b，契约 17.3）：同样**仅 admin**；
				// 测试邮件同步发送，未配置 SMTP 时返回 40002 明确提示。
				settingsGroup.GET("/email/smtp",
					requireAdminRole(model.RoleAdmin), adminSettings.getEmailSMTPSettings)
				settingsGroup.PUT("/email/smtp",
					requireAdminRole(model.RoleAdmin), adminSettings.updateEmailSMTPSettings)
				settingsGroup.POST("/email/test",
					requireAdminRole(model.RoleAdmin), adminSettings.sendTestEmail)
				settingsGroup.GET("/notifications",
					requireAdminRole(model.RoleAdmin), adminSettings.getNotificationSettings)
				settingsGroup.PUT("/notifications",
					requireAdminRole(model.RoleAdmin), adminSettings.updateNotificationSettings)

				// 邀请返佣设置（R10-A，契约 18.6）：同样**仅 admin**。
				settingsGroup.GET("/referral",
					requireAdminRole(model.RoleAdmin), adminSettings.getReferralSettings)
				settingsGroup.PUT("/referral",
					requireAdminRole(model.RoleAdmin), adminSettings.updateReferralSettings)
				// 系统更新源（R13-A，契约 19.5）：仓库与可选 GitHub Token，同样仅 admin。
				settingsGroup.GET("/updates",
					requireAdminRole(model.RoleAdmin), adminSettings.getUpdatesSettings)
				settingsGroup.PUT("/updates",
					requireAdminRole(model.RoleAdmin), adminSettings.updateUpdatesSettings)

				// 备份目录（R15，契约 21.10）：db_backup 键的读写，**仅 admin**（含读取）。
				// 保存时就用备份服务的同一套校验（Web 根拒绝 + 0750 创建 + 写入探针），不过不落库。
				settingsGroup.GET("/db_backup",
					requireAdminRole(model.RoleAdmin), adminSettings.getDBBackupSettings)
				settingsGroup.PUT("/db_backup",
					requireAdminRole(model.RoleAdmin), adminSettings.updateDBBackupSettings)

				// 站点对外地址（R16，契约 22.4）：site 键里 url 一项的读写，**仅 admin**（含读取）。
				// 该地址是交付时登记给上游的回推地址（downstream_url）；只读写 url，
				// 向导写入的 name / admin_email 原样保留。非密钥，不做脱敏。
				settingsGroup.GET("/site",
					requireAdminRole(model.RoleAdmin), adminSettings.getSiteSettings)
				settingsGroup.PUT("/site",
					requireAdminRole(model.RoleAdmin), adminSettings.updateSiteSettings)

				// 缓存设置（R19 批 1，契约 24.3）：驱动选择（file / redis）+ Redis 参数，**仅 admin**
				// （含读取）；Redis 口令走三态与掩码回显，校验不过 40002 且不落库。
				settingsGroup.GET("/cache",
					requireAdminRole(model.RoleAdmin), caches.getCacheSettings)
				settingsGroup.PUT("/cache",
					requireAdminRole(model.RoleAdmin), caches.updateCacheSettings)
			}

			// 缓存状态与运维（R19 批 1，契约 24.4）：状态（含降级与原因）、重置命中统计、
			// 清空缓存——**三个接口都仅 admin**（finance / support 403）。
			adminGroup.GET("/cache/status",
				requireAdminRole(model.RoleAdmin), caches.getCacheStatus)
			adminGroup.POST("/cache/reset-stats",
				requireAdminRole(model.RoleAdmin), caches.resetCacheStats)
			adminGroup.POST("/cache/flush",
				requireAdminRole(model.RoleAdmin), caches.flushCache)

			// 系统版本与更新检查（R13-A，契约 19.3）：仅 admin（19.6）。读缓存不出网；检查更新强制出网。
			// 一键升级（R13-B，契约 20.3 / 20.4）：发起会替换二进制并重启进程，同样仅 admin。
			adminGroup.GET("/updates",
				requireAdminRole(model.RoleAdmin), updatesAdmin.getCachedUpdate)
			adminGroup.GET("/updates/check",
				requireAdminRole(model.RoleAdmin), updatesAdmin.checkUpdate)
			adminGroup.POST("/updates/apply",
				requireAdminRole(model.RoleAdmin), updatesAdmin.applyUpdate)
			adminGroup.GET("/updates/job",
				requireAdminRole(model.RoleAdmin), updatesAdmin.getUpdateJob)

			// 数据库备份与恢复（R14，契约 21.3）：四个接口**全部仅 admin**（含读取）。
			// 只输出元信息——**没有任何能读到备份文件内容的接口**（契约 21.2）；
			// 恢复与删除只接受备份 ID，服务在自己的白名单目录里解析路径（契约 21.4）。
			adminGroup.GET("/db-backups",
				requireAdminRole(model.RoleAdmin), dbBackups.listBackups)
			adminGroup.POST("/db-backups",
				requireAdminRole(model.RoleAdmin), dbBackups.createBackup)
			adminGroup.POST("/db-backups/:id/restore",
				requireAdminRole(model.RoleAdmin), dbBackups.restoreBackup)
			adminGroup.DELETE("/db-backups/:id",
				requireAdminRole(model.RoleAdmin), dbBackups.deleteBackup)

			// 充值单与流水对账（阶段 4）：admin / finance 可查（support 返回 403）。
			adminGroup.GET("/recharges",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), finance.listAdminRecharges)
			adminGroup.GET("/ledger",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), finance.listAdminLedger)

			// 实例（阶段 5a）：查看类所有角色可调用（列表不含敏感字段）；
			// 阶段 8：详情接口（`/instances/:id`）字段对齐会员端详情口径 + member_id，
			// 权限沿用列表口径（三角色均可读）。
			// 重试交付会真实调用上游开通（可能扣上游余额），仅 admin 角色（契约 14.4）。
			adminGroup.GET("/instances", instances.listAdminInstances)
			adminGroup.GET("/instances/:id", instances.getAdminInstance)

			// 订单（阶段 8b，契约 12.6，补阶段 8 缺口）：列表/详情为**查看类**接口，
			// admin / finance / support 均可读（客服协助会员查询是日常）；
			// 重试交付保持**仅 admin**（真实调用上游开通、可能扣上游余额，契约 14.4）。
			adminGroup.GET("/orders", orders.listAdminOrders)
			adminGroup.GET("/orders/:id", orders.getAdminOrder)
			adminGroup.POST("/orders/:id/retry-delivery",
				requireAdminRole(model.RoleAdmin), orders.retryDelivery)

			// 实例操作（阶段 5b）：suspend / unsuspend 影响服务状态，仅 admin 角色；
			// sync 为只读回读 + 状态收敛，所有角色可调用（对齐既有角色矩阵，契约 15.2）。
			adminGroup.POST("/instances/:id/suspend",
				requireAdminRole(model.RoleAdmin), instances.adminSuspendInstance)
			adminGroup.POST("/instances/:id/unsuspend",
				requireAdminRole(model.RoleAdmin), instances.adminUnsuspendInstance)
			adminGroup.POST("/instances/:id/sync", instances.adminSyncInstance)
			adminGroup.GET("/instances/:id/logs", instances.listAdminInstanceLogs)

			// 取消/终止申请（阶段 5c）：提交上游终止（代客/强制），仅 admin 角色且 reason 必填；
			// 状态收敛复用 sync（上游确认删除后本地转 terminated，契约 15.8.2/15.8.3）。
			adminGroup.POST("/instances/:id/cancel",
				requireAdminRole(model.RoleAdmin), instances.adminCancelInstance)

			// 商品与计费（阶段 3a）：所有角色可查看；导入/改定价/上下架/改分组要求 admin 或 finance。
			adminGroup.GET("/products", products.listProducts)
			adminGroup.GET("/products/:id", products.getProduct)
			adminGroup.POST("/products/import",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), products.importProducts)
			// 商品同步（R20，契约 10.7）：把**已存在**商品的上游来源字段刷成上游最新（不新增、不删除）。
			// 与导入不同，它不创建商品，因此权限收紧到**仅 admin**（10.5 矩阵）。
			adminGroup.POST("/products/sync",
				requireAdminRole(model.RoleAdmin), products.syncProducts)
			adminGroup.PUT("/products/:id",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), products.updateProduct)

			adminGroup.GET("/product-groups", products.listProductGroups)
			adminGroup.PUT("/product-groups/:id",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), products.updateProductGroup)

			// 上游分组 → 本地子分类映射（R12，契约 10.4）：列表三角色只读；
			// 写操作沿用 categories.write（改名/排序/改挂同口径）——**仅 admin**。
			adminGroup.GET("/product-group-maps", groupMaps.listGroupMaps)
			adminGroup.PUT("/product-group-maps/:upstream_group_id",
				requireAdminRole(model.RoleAdmin), groupMaps.upsertGroupMap)
			adminGroup.DELETE("/product-group-maps/:upstream_group_id",
				requireAdminRole(model.RoleAdmin), groupMaps.deleteGroupMap)

			// 商品两级分类（R7）：分类树三角色只读；改名 / 子分类改挂主分类仅 admin。
			adminGroup.GET("/categories", categories.listCategories)
			adminGroup.PUT("/categories/:id",
				requireAdminRole(model.RoleAdmin), categories.updateCategory)

			// 优惠码（阶段 3b）：查看类所有角色可调用；创建/修改要求 admin 或 finance。
			// 不提供 DELETE——停用即 status=off（契约写明）。
			adminGroup.GET("/coupons", coupons.listCoupons)
			adminGroup.GET("/coupons/:id", coupons.getCoupon)
			adminGroup.POST("/coupons",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), coupons.createCoupon)
			adminGroup.PUT("/coupons/:id",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), coupons.updateCoupon)

			// 邀请返佣出账申请（R10-B，契约 18.1.1 / 18.5）：
			// 查看类走财务域口径（admin / finance）；审核与打款**仅 admin**。
			adminGroup.GET("/referral/withdrawals",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), referralPayouts.listAdminWithdrawals)
			adminGroup.GET("/referral/conversions",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), referralPayouts.listAdminConversions)
			adminGroup.POST("/referral/withdrawals/:id/review",
				requireAdminRole(model.RoleAdmin), referralPayouts.reviewWithdrawal)
			adminGroup.POST("/referral/withdrawals/:id/mark-paid",
				requireAdminRole(model.RoleAdmin), referralPayouts.markWithdrawalPaid)
			adminGroup.POST("/referral/conversions/:id/review",
				requireAdminRole(model.RoleAdmin), referralPayouts.reviewConversion)

			// 工单（阶段 6a，契约 16.3）：工单域是**客服域**——admin 与 support 全权
			// （这是唯一给 support 写权限的域），finance 一律 403（含只读）。
			adminTickets := adminGroup.Group("/tickets",
				requireAdminRole(model.RoleAdmin, model.RoleSupport))
			{
				adminTickets.GET("", tickets.listAdminTickets)
				adminTickets.GET("/:id", tickets.getAdminTicket)
				adminTickets.POST("/:id/reply", tickets.adminReplyTicket)
				adminTickets.POST("/:id/close", tickets.adminCloseTicket)
			}

			// 站内通知（阶段 6b，契约 17.2）：**本人收件箱**，三类角色都可读自己的通知
			// （通知不属于工单域，finance 只是通常收不到事件——扇出只覆盖 admin + support）。
			adminNotifications := adminGroup.Group("/notifications")
			{
				adminNotifications.GET("", notifications.listAdminNotifications)
				adminNotifications.GET("/unread-count", notifications.adminUnreadCount)
				adminNotifications.POST("/:id/read", notifications.readAdminNotification)
				adminNotifications.POST("/read-all", notifications.readAllAdminNotifications)
			}

			// 操作日志（R18，契约 23.4）：列表（高级搜索）与清空，**两个接口都仅 admin**
			// （含读取；finance / support 一律 403）。**没有**按条删除或修改的接口——
			// 路由表断言测试保证不存在 DELETE /:id 之类的路径。
			adminGroup.GET("/operation-logs",
				requireAdminRole(model.RoleAdmin), operationLogs.listOperationLogs)
			adminGroup.DELETE("/operation-logs",
				requireAdminRole(model.RoleAdmin), operationLogs.clearOperationLogs)
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
