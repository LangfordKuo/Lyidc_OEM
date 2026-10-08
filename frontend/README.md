# Lyidc_OEM Frontend

React 19 + Vite + TypeScript + HeroUI v3 + Tailwind CSS v4。
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

## 目录结构

```
src/
  api/        # fetch 封装与接口调用（统一解析 {code,message,data} 响应包）
  components/ # 可复用展示组件
  lib/        # 与框架无关的工具函数
  pages/      # 页面级组件
  test/       # vitest 测试环境初始化
```
