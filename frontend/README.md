# Lyidc_OEM 前端

岭云互联官网前端（第一期：官网 + 认证 + 商品 + 下单支付）。

技术栈：**Vite + React 19 + TypeScript + Tailwind CSS v4 + shadcn/ui**（radix 基座）+ react-router-dom v7
+ react-hook-form + zod + sonner + lucide-react；测试为 vitest + @testing-library/react（jsdom）。

## 快速开始

```bash
npm install
npm run dev     # http://127.0.0.1:5173（/api 代理到 http://127.0.0.1:8080）
```

后端需在 `127.0.0.1:8080` 运行（仓库根 `backend/`）；`/api` 的代理在 `vite.config.ts` 的 `server.proxy`。

## 脚本

| 命令 | 说明 |
| --- | --- |
| `npm run dev` | 开发服务器（端口 **5173**，`strictPort`，仅监听 `127.0.0.1`） |
| `npm run build` | `tsc -b` 类型检查 + 构建到 `frontend/dist/`（Dockerfile.frontend 依赖该路径） |
| `npm test` / `npm run test:watch` | vitest（run / watch） |
| `npm run lint` | oxlint |

## 目录结构

```
src/
  api/           # typed fetch 封装（契约统一响应包）、各域接口与类型
  app/           # 路由表（routes.tsx）/ 浏览器路由（router.tsx）/ Provider / 路径常量
  auth/          # 会员登录态（AuthProvider）、路由守卫（RequireAuth）
  components/
    ui/          # shadcn/ui 组件（由官方 CLI 生成，勿手改）
    common/      # 页面状态占位、状态徽标、区块标题
    layout/      # 顶栏 / 页脚 / 官网布局 / 认证卡片壳 / 用户菜单
    product/     # 商品卡片、六周期选择器与价格表、配置项选择
    checkout/    # 支付弹窗（在线支付 / 余额支付 / 二维码）
  hooks/         # useAsync（加载/错误/竞态取消/重试）
  lib/           # 周期、金额与时间格式化、下单草稿、支付口径、校验 zod schema 等
  pages/         # 页面组件（Home / Products / ProductDetail / Login / Register / Checkout / PayResult / NotFound）
  test/          # 测试 setup、夹具与渲染 harness
```

## 硬性约定（与仓库其余部分一致）

- **构建产物**：`npm run build` → `frontend/dist/`；dev 端口 **5173**；`/api` → `127.0.0.1:8080`。
- **会员 token**：登录后写入 `localStorage['lyidc.member.token']`；任意请求返回 `401` 时清 token 并回到登录页（带 `redirect` 回跳）。
- **接口契约**：`docs/api-contract.md`。统一响应包 `{code, message, data}`，`code === 0` 为成功；拒绝路径一律以契约为准。
- **shadcn/ui 组件一律用官方 CLI 生成**（`npx shadcn@latest add <name>`），不手写替代实现。

## 本期范围

已实现：全站布局、首页、商品列表（分组/搜索/分页）、商品详情（六周期价格 + 配置项）、注册/登录（zod 校验 + 回跳）、
结算（优惠码校验 + 金额明细 + 支付弹窗）、支付结果页、404。

未实现（后续期）：会员区（服务器/订单/充值/工单/通知）、管理后台、站点安装向导前端。
