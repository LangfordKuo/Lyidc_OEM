# Lyidc_OEM Frontend

React 19 + Vite + TypeScript + HeroUI v3 + Tailwind CSS v4 + react-router-dom。
完整的本地启动说明见仓库根目录 [README.md](../README.md)，接口契约见 [docs/api-contract.md](../docs/api-contract.md)。

## 常用命令

```bash
npm install        # 安装依赖
npm run dev        # 开发服务器（http://127.0.0.1:5173，/api 代理到 127.0.0.1:8080）
npm run typecheck  # tsc --noEmit
npm run build      # 类型检查 + 生产构建
npm test           # vitest run
npm run lint       # oxlint
```

> `vite.config.ts` 显式把 dev server 绑定到 IPv4 `127.0.0.1`：部分 Windows 环境下默认的
> `localhost` 只监听 `[::1]`，会导致 README/契约里记录的 `http://127.0.0.1:5173` 打不开。

## 目录结构

```
src/
  api/         # fetch 封装与按域接口（统一解析 {code,message,data} 响应包）
    client.ts  #   request/http、ApiError、鉴权头注入、401 通知、超时
    tokens.ts  #   会员 token 与管理端 token 分离存储（localStorage）
    auth.ts products.ts coupons.ts orders.ts finance.ts
    instances.ts tickets.ts notifications.ts   # 7b：实例操作 / 工单 / 站内通知
    adminAuth.ts adminMembers.ts adminProducts.ts adminOrders.ts
    adminInstances.ts adminTickets.ts adminSettings.ts adminNotifications.ts   # 8：管理端各域
    types.ts   #   契约对象类型（会员/商品/优惠码/订单/支付/实例/工单/通知/充值/流水/管理端）
  app/         # 路由表（routes.tsx，含 /admin 分支）、路径与导航常量（paths.ts）
  auth/        # 会员登录态 Provider + RequireAuth；管理端 AdminAuthProvider + RequireAdmin
  components/  # 布局（layout/）、通用件（common/）、商品件（product/）、
               # 实例操作件（instance/）、订单支付件（order/）、后台件（admin/）
  hooks/       # useAsync（轻量数据加载）、useUnreadCount / useAdminUnreadCount（未读计数）
  lib/         # 与框架无关的工具：周期、格式化、校验、到期口径、支付口径、
               # 实例/工单/通知/财务文案、后台角色矩阵（adminRoles）、分页、剪贴板、草稿
  pages/       # 页面（console/ 会员区实页；admin/ 管理后台实页）
  test/        # vitest 环境初始化与测试脚手架（consoleHarness / adminHarness）
```

## 阶段 8 范围（管理后台 `/admin/*`）

| 路由 | 页面 | 关键交互 | 角色可见性（契约 6.4 / 10.5 / 12.7 / 15.3 / 16.3 / 17.3） |
| --- | --- | --- | --- |
| `/admin/login` | 管理员登录 | 错误提示直出后端 message；成功回跳 `redirect` | 公开 |
| `/admin` | 仪表盘 | 会员/实例/工单/充值指标 + 最近工单与充值 + 上游探活；**订单类指标标注「接口缺失」不造假** | 三角色；工单卡仅 admin/support、财务卡仅 admin/finance、探活三角色 |
| `/admin/products` | 商品 | 搜索/分组/状态筛选 + 分页；定价三模式编辑；上下架；**上游导入（进度与结果反馈）**；分组重命名/排序 | 只读三角色；导入/定价/上下架/分组写 = admin/finance |
| `/admin/orders` | 订单 | **交付处置工作台**：按订单 ID 重试交付（二次确认 + 结果订单展示）；列表接口缺失的说明 | 重试交付 = 仅 admin |
| `/admin/members` | 会员 | 用户名/邮箱搜索 + 状态筛选；详情弹窗（资料 + 余额 + 流水 + 充值单）；禁用/启用（二次确认） | 列表三角色；流水/充值单 = admin/finance；禁用/启用 = admin/finance |
| `/admin/instances` | 实例 | 状态/会员筛选 + 分页；详情（含主机凭据与操作记录）；暂停/恢复/终止申请/同步 | 只读 + 同步三角色；暂停/恢复/终止 = 仅 admin |
| `/admin/tickets` | 工单 | 状态/分类/关键词/会员筛选 + 分页；详情含**内部备注**（开关）、回复、关闭 | admin/support；**finance 整页无权** |
| `/admin/settings` | 设置 | 易支付 / 上游（+探活）/ SMTP（+测试邮件）/ 通知开关；密钥三态（保持·替换·清空）与掩码回显 | **仅 admin**（含读取） |
| `/admin/notifications` | 通知 | 管理员本人收件箱（未读筛选、单条/全部已读、侧栏徽章） | 三角色（个人收件箱） |

实现约定：

- **角色矩阵集中在 `src/lib/adminRoles.ts`**（权限点 → 角色白名单 + 禁用提示文案），页面只做
  「隐藏整页入口 / 禁用按钮 + 说明」，服务端仍独立校验（403 统一由错误提示兜底）。
- 管理端 token 存 `lyidc.admin.token`，与会员 token 完全隔离；管理端 401 会清本地登录态并回登录页。
- 列表页统一用 `components/admin/AdminTable.tsx`（列定义 + 空态）与 `FilterSelect`（下拉筛选）、
  `StatCard`（仪表盘指标）、`NoPermission`（整页无权占位）；分页/筛选/空态与会员区保持同一套组件。
- **HeroUI v3 两个坑（已在代码注释中标注）**：
  1. `Table` 的根是普通 div，真正的表格元素是 `Table.Content`（react-aria 集合上下文）——
     `Header/Body/Row/Cell` 必须放在 `Table.Content` 内，否则抛 `cannot be rendered outside a collection`；
  2. `Radio` 必须用 `Radio.Content` 包裹（内含 `Radio.Control`/`Radio.Indicator`）才是可交互控件，
     只写文本会渲染成不可选中的纯文本节点。

## 阶段 7b 范围（会员区）

| 路由 | 页面 | 关键交互 |
| --- | --- | --- |
| `/console/servers` | 我的服务器 | 状态筛选 + 分页；状态 chip、到期临期高亮、取消在途标记 |
| `/console/servers/:id` | 实例详情 | 账号密码「显示/复制」（默认遮蔽）；电源/重装/改密/续费/终止申请（**全部 HeroUI 弹窗二次确认**）；操作记录时间线 |
| `/console/orders` | 我的订单 | 状态筛选 + 行内展开详情（配置快照、金额明细、时间线、交付失败原因 + 提交工单）；继续支付 / 取消订单 |
| `/console/recharge` | 余额充值 | 余额实时卡片 + 金额校验（1.00~50000.00）+ 易支付跳转；充值记录与余额流水两个页签 |
| `/console/tickets` | 工单 | 状态筛选 + 提交工单（分类/关联实例/内容校验）；支持从订单页带参预填 |
| `/console/tickets/:id` | 工单详情 | 会员/客服分侧气泡（内部备注不下发）+ 回复 + 关闭（二次确认）；已关闭不可回复 |
| `/console/notifications` | 通知 | 未读筛选 + 点击即读 + 全部已读；侧栏「通知」带未读徽章 |

首次交付（7a）已包含：官网首页、商品列表/详情、登录注册、下单确认、支付结果、会员区概览。

### 主题与 HeroUI 约定

- HeroUI v3 **不使用** `HeroUIProvider`，样式由 `src/index.css` 顶部的 `@import "@heroui/styles"` 提供；
  该导入必须在 `@import "tailwindcss"` **之前**（HeroUI 的入口文件已声明 layer 顺序并引入 Tailwind 基础层）。
- 品牌色板通过覆盖 HeroUI 主题变量定制（深靛蓝主色 `--accent`、圆角 `--radius`），
  亮色变量用 `:root:not(.dark)` 守卫，避免影响潜暗色主题。见 `src/index.css`。
- 站点名目前是前端常量（`src/lib/site.ts`）：契约中没有面向会员端的公开站点信息接口
  （`settings.site` 只经安装向导接口回带），代码中已用 TODO 标记待接。
