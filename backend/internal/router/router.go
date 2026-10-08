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
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/delivery"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/instanceops"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/notify"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/scheduler"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
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
	Trigger(orderID uint64)
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
	// UpstreamTimeout 是上游探活的整体超时，缺省 5s。
	UpstreamTimeout time.Duration
	// Delivery 是订单交付器；nil 时构造默认实现（自动交付异步执行、管理员重试用当前上游设置）。
	Delivery DeliveryTrigger
	// EnableDueScan 为 true 时构造并启动「到期暂停扫描」后台任务（阶段 5b，契约 15.5）：
	// 启动延迟 DefaultInitialDelay 后首次扫描，之后每 DefaultInterval 扫描一次。
	// 生产装配（cmd/server）传 true；集成测试默认 false（扫描逻辑由 scheduler.Scanner 单元/集成测试覆盖）。
	EnableDueScan bool
	// Notifier 是通知能力（阶段 6b）；nil 时构造默认实现（事件入口异步、按后台设置发信）。
	// 注入的实现同时被「交付/扫描（经 Options.Delivery 与调度器）」与「工单/设置页」使用；
	// 测试注入 Async=false 的 notify.Service 即可同步断言通知结果。
	Notifier NotificationTrigger
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
	mw := &middleware{store: st, tokens: tokens}

	// 阶段 4：支付渠道与上游客户端都按后台设置动态构造（契约 12.1），
	// 管理员改完设置下一次调用即生效；未配置时对应功能返回明确业务错误，其余功能不受影响。
	settingsReader := settings.NewReader(st)
	payments := newPaymentRegistry(settingsReader, opts.Logger)
	upstreamProvider := opts.Upstream
	if upstreamProvider == nil {
		upstreamProvider = newUpstreamProvider(settingsReader, opts.Logger)
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
		})
		scanner.Start()
		opts.Logger.Info("到期暂停扫描已启动",
			"initial_delay", scheduler.DefaultInitialDelay, "interval", scheduler.DefaultInterval)
	}

	members := &memberHandler{store: st, tokens: tokens, logger: opts.Logger}
	admins := &adminHandler{store: st, tokens: tokens, logger: opts.Logger}
	products := &productHandler{
		store:    st,
		upstream: upstreamProvider,
		logger:   opts.Logger,
	}
	coupons := &couponHandler{store: st, logger: opts.Logger}
	orders := &orderHandler{store: st, payments: payments, deliveries: deliveries, logger: opts.Logger}
	finance := &financeHandler{store: st, payments: payments, logger: opts.Logger}
	paymentCallbacks := &paymentHandler{
		store: st, payments: payments, reader: settingsReader, deliveries: deliveries, logger: opts.Logger,
	}
	instances := &instanceHandler{store: st, ops: instanceOperator, logger: opts.Logger}
	adminSettings := &settingsHandler{
		store: st, reader: settingsReader, notify: notifier, logger: opts.Logger,
	}
	// 阶段 6a：工单（纯本地域，不调用上游；管理端为 admin + support 的客服域）。
	// 阶段 6b：工单事件（创建/会员回复/客服公开回复/客服关闭）在事务提交后触发通知。
	tickets := &ticketHandler{store: st, notify: notifier, logger: opts.Logger}
	// 阶段 6b：站内通知收件箱（会员端与管理端同构）。
	notifications := &notificationHandler{store: st, logger: opts.Logger}

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

		// 优惠码校验（阶段 3b）：公开只读接口，供下单前试算折扣；
		// 折扣的应用（扣减金额、使用记账）阶段 4 已在下单/支付链路兑现（契约 12.5）。
		apiV1.GET("/coupons/:code/validate", coupons.validateCoupon)

		// 支付回调与同步跳转（阶段 4）：渠道 → 本服务，无需鉴权——验签与金额校验是唯一凭证。
		// notify_url 由管理员在后台设置里填写（推荐 <系统域名>/api/v1/payments/epay/notify）。
		apiV1.POST("/payments/epay/notify", paymentCallbacks.epayNotify)
		apiV1.GET("/payments/epay/notify", paymentCallbacks.epayNotify)
		apiV1.GET("/payments/epay/return", paymentCallbacks.epayReturn)

		// 订单与财务（阶段 4）：需要会员 token；全部只操作**本人**数据。
		memberAPI := apiV1.Group("", mw.requireMember())
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
		}

		// 管理端：登录开放，其余需要管理员 token；
		// 改状态类接口额外要求角色为 admin 或 finance（support 返回 403）。
		apiV1.POST("/admin/auth/login", admins.login)

		adminGroup := apiV1.Group("/admin", mw.requireAdmin())
		{
			adminGroup.GET("/profile", admins.profile)
			adminGroup.GET("/members", admins.listMembers)
			adminGroup.PUT("/members/:id/status",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), admins.updateMemberStatus)

			// 上游探活：按当前后台设置只读调用上游验证连通性（未配置时返回 connected=false）。
			upstreamGroup := adminGroup.Group("/upstream")
			{
				upstreamGroup.GET("/health",
					upstreamHealthHandler(upstreamProvider, opts.UpstreamTimeout))
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
			}

			// 充值单与流水对账（阶段 4）：admin / finance 可查（support 返回 403）。
			adminGroup.GET("/recharges",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), finance.listAdminRecharges)
			adminGroup.GET("/ledger",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), finance.listAdminLedger)

			// 实例（阶段 5a）：查看类所有角色可调用（不含敏感字段）；
			// 重试交付会真实调用上游开通（可能扣上游余额），仅 admin 角色（契约 14.4）。
			adminGroup.GET("/instances", instances.listAdminInstances)
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
			adminGroup.PUT("/products/:id",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), products.updateProduct)

			adminGroup.GET("/product-groups", products.listProductGroups)
			adminGroup.PUT("/product-groups/:id",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), products.updateProductGroup)

			// 优惠码（阶段 3b）：查看类所有角色可调用；创建/修改要求 admin 或 finance。
			// 不提供 DELETE——停用即 status=off（契约写明）。
			adminGroup.GET("/coupons", coupons.listCoupons)
			adminGroup.GET("/coupons/:id", coupons.getCoupon)
			adminGroup.POST("/coupons",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), coupons.createCoupon)
			adminGroup.PUT("/coupons/:id",
				requireAdminRole(model.RoleAdmin, model.RoleFinance), coupons.updateCoupon)

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
