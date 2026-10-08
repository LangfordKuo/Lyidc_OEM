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
    types.ts   #   契约对象类型（会员/商品/优惠码/订单/支付/实例/工单/通知/充值/流水）
  app/         # 路由表（routes.tsx）、Browser Router（router.tsx）、路径常量（paths.ts）
  auth/        # 会员登录态 Provider、路由守卫 RequireAuth、useAuth
  components/  # 布局（layout/）、通用件（common/）、商品件（product/）、
               # 实例操作件（instance/）、订单支付件（order/）
  hooks/       # useAsync（轻量数据加载）、useUnreadCount（未读通知计数，跨页面共享）
  lib/         # 与框架无关的工具：周期、格式化、校验、到期口径、支付口径、
               # 实例/工单/通知/财务文案、分页、剪贴板、下单草稿
  pages/       # 页面（console/ 为会员区实页）
  test/        # vitest 测试环境初始化与会员区测试脚手架（consoleHarness.tsx）
```

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
