package router

// oplogActionNames 是「方法 + 路由模板 → 中文动作名」的映射表（R18 契约 23.3）。
//
// **单一维护点**：新增写路由时在这里补一条中文动作名；漏补不会漏日志——
// 兜底动作名是「METHOD /path」，日志照写（契约 23.3 的兜底口径）。
// 键里的路由模板与 gin 的 c.FullPath() 完全一致（含 :id / :upstream_group_id 等参数名）。
var oplogActionNames = map[string]string{
	// —— 会员通道（23.3：会员端所有写操作）——
	"POST /api/v1/auth/register":                "会员注册",
	"POST /api/v1/auth/login":                   "会员登录",
	"PUT /api/v1/members/me":                    "修改会员资料",
	"POST /api/v1/members/me/password":          "修改会员密码",
	"POST /api/v1/orders":                       "创建订单",
	"POST /api/v1/orders/:id/pay":               "支付订单",
	"POST /api/v1/orders/:id/cancel":            "取消订单",
	"POST /api/v1/instances/:id/power":          "实例电源操作",
	"POST /api/v1/instances/:id/reinstall":      "重装系统",
	"POST /api/v1/instances/:id/reset-password": "重置实例密码",
	"POST /api/v1/instances/:id/renew":          "续费实例",
	"POST /api/v1/instances/:id/cancel":         "申请终止实例",
	"POST /api/v1/recharges":                    "发起充值",
	"POST /api/v1/tickets":                      "提交工单",
	"POST /api/v1/tickets/:id/reply":            "回复工单",
	"POST /api/v1/tickets/:id/close":            "关闭工单",
	"POST /api/v1/notifications/:id/read":       "标记通知已读",
	"POST /api/v1/notifications/read-all":       "通知全部已读",
	"PUT /api/v1/referral":                      "更新返现设置",
	"POST /api/v1/referral/coupons":             "创建自建券",
	"PUT /api/v1/referral/coupons/:id":          "更新自建券",
	"POST /api/v1/referral/withdrawals":         "提交提现申请",
	"POST /api/v1/referral/conversions":         "提交转余额申请",

	// —— 管理员通道（23.3：管理端所有写操作，含登录成败）——
	"POST /api/v1/admin/auth/login":                              "管理员登录",
	"PUT /api/v1/admin/members/:id/status":                       "修改会员状态",
	"PUT /api/v1/admin/members/:id":                              "修改会员返佣特批",
	"PUT /api/v1/admin/settings/payment/epay":                    "修改易支付设置",
	"PUT /api/v1/admin/settings/upstream":                        "修改上游凭据设置",
	"PUT /api/v1/admin/settings/email/smtp":                      "修改 SMTP 设置",
	"POST /api/v1/admin/settings/email/test":                     "发送测试邮件",
	"PUT /api/v1/admin/settings/notifications":                   "修改通知设置",
	"PUT /api/v1/admin/settings/referral":                        "修改返佣设置",
	"PUT /api/v1/admin/settings/updates":                         "修改更新源设置",
	"PUT /api/v1/admin/settings/db_backup":                       "修改备份目录设置",
	"PUT /api/v1/admin/settings/site":                            "修改站点地址设置",
	"POST /api/v1/admin/updates/apply":                           "发起一键升级",
	"POST /api/v1/admin/db-backups":                              "创建数据库备份",
	"POST /api/v1/admin/db-backups/:id/restore":                  "恢复数据库备份",
	"DELETE /api/v1/admin/db-backups/:id":                        "删除数据库备份",
	"POST /api/v1/admin/orders/:id/retry-delivery":               "重试订单交付",
	"POST /api/v1/admin/instances/:id/suspend":                   "暂停实例",
	"POST /api/v1/admin/instances/:id/unsuspend":                 "恢复实例",
	"POST /api/v1/admin/instances/:id/sync":                      "同步实例状态",
	"POST /api/v1/admin/instances/:id/cancel":                    "终止实例申请",
	"POST /api/v1/admin/products/import":                         "导入上游商品",
	"PUT /api/v1/admin/products/:id":                             "修改商品",
	"PUT /api/v1/admin/product-groups/:id":                       "修改商品分组",
	"PUT /api/v1/admin/product-group-maps/:upstream_group_id":    "保存分组映射",
	"DELETE /api/v1/admin/product-group-maps/:upstream_group_id": "删除分组映射",
	"PUT /api/v1/admin/categories/:id":                           "修改商品分类",
	"POST /api/v1/admin/coupons":                                 "创建优惠码",
	"PUT /api/v1/admin/coupons/:id":                              "修改优惠码",
	"POST /api/v1/admin/referral/withdrawals/:id/review":         "审核提现申请",
	"POST /api/v1/admin/referral/withdrawals/:id/mark-paid":      "提现打款",
	"POST /api/v1/admin/referral/conversions/:id/review":         "审核转余额申请",
	"POST /api/v1/admin/tickets/:id/reply":                       "回复工单",
	"POST /api/v1/admin/tickets/:id/close":                       "关闭工单",
	"POST /api/v1/admin/notifications/:id/read":                  "标记通知已读",
	"POST /api/v1/admin/notifications/read-all":                  "通知全部已读",
	"DELETE /api/v1/admin/operation-logs":                        "清空操作日志",
}

// oplogActionName 返回路由的中文动作名；未登记的路由回落为「METHOD /path」（兜底，绝不留空）。
func oplogActionName(method, path string) string {
	if name, ok := oplogActionNames[method+" "+path]; ok {
		return name
	}
	if path == "" {
		return method
	}
	return method + " " + path
}

// changedLabel 把「是否发生变化」渲染成中文（供操作日志摘要复用）。
func changedLabel(changed bool) string {
	if changed {
		return "已变更"
	}
	return "无变化"
}
