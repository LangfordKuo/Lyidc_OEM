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
    auth.ts products.ts coupons.ts orders.ts finance.ts health.ts
    types.ts   #   契约对象类型（会员/商品/优惠码/订单/支付/分页）
  app/         # 路由表（routes.tsx）、Browser Router（router.tsx）、路径常量（paths.ts）
  auth/        # 会员登录态 Provider、路由守卫 RequireAuth、useAuth
  components/  # 布局（layout/）、通用件（common/）、商品件（product/）
  hooks/       # useAsync（轻量数据加载：加载态/错误态/竞态取消/重试）
  lib/         # 与框架无关的工具：周期、金额/时间格式化、表单校验、下单草稿、回跳安全
  pages/       # 页面（console/ 为会员区骨架页）
  test/        # vitest 测试环境初始化
```

## 阶段 7a 范围

已交付：官网首页、商品列表/详情、登录注册、下单确认、支付结果、会员区骨架（概览 + 占位路由）。
会员区各功能页（我的服务器/订单/充值/工单/通知）为 **7b** 范围，本批仅保留路由与占位说明。

### 主题与 HeroUI 约定

- HeroUI v3 **不使用** `HeroUIProvider`，样式由 `src/index.css` 顶部的 `@import "@heroui/styles"` 提供；
  该导入必须在 `@import "tailwindcss"` **之前**（HeroUI 的入口文件已声明 layer 顺序并引入 Tailwind 基础层）。
- 品牌色板通过覆盖 HeroUI 主题变量定制（深靛蓝主色 `--accent`、圆角 `--radius`），
  亮色变量用 `:root:not(.dark)` 守卫，避免影响潜暗色主题。见 `src/index.css`。
- 站点名目前是前端常量（`src/lib/site.ts`）：契约中没有面向会员端的公开站点信息接口
  （`settings.site` 只经安装向导接口回带），代码中已用 TODO 标记待接。
