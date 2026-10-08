# Lyidc_OEM

岭云互联 IDC 财务系统，代理对接专用。

一个独立部署的 IDC 财务/计费系统（对标魔方财务、WHMCS），后端 Go + Gin + GORM + MySQL 5.7，前端 React + Vite + TypeScript + HeroUI（Tailwind CSS v4）。当前处于**阶段 0：脚手架**，只包含工程结构与基建（配置、数据库连接、迁移、统一响应包、健康检查），不含任何业务功能。

## 目录结构

```
backend/                 Go 后端（module: github.com/LangfordKuo/Lyidc_OEM/backend）
  cmd/server/            HTTP 服务入口（默认监听 127.0.0.1:8080）
  cmd/migrate/           数据库迁移命令（up / down / version）
  internal/config/       配置加载与校验（backend/config.yaml）
  internal/db/           MySQL 连接与连接池
  internal/router/       路由注册与健康检查
  internal/response/     统一响应包与错误码表
  migrations/            编号 SQL 迁移文件（golang-migrate 风格）
frontend/                React 前端（Vite + TS + HeroUI v3 + Tailwind v4）
  src/api/               fetch 封装 / 接口调用
  src/components/        复用组件
  src/lib/               工具函数
  src/pages/             页面
docs/api-contract.md     接口契约（改接口先改这里）
.github/workflows/ci.yml 持续集成
```

## 环境要求

- Go 1.25+（开发机为 1.27.1）
- Node.js 20.19+ / npm 11（开发机为 Node 26 + npm 11）
- MySQL 5.7（本地 127.0.0.1:3306，需要预先建好 utf8mb4 空库，如 `lyidc`）

## 本地开发

### 1. 后端

```bash
cd backend
cp config.example.yaml config.yaml     # config.yaml 已被 .gitignore 忽略，按需修改 DSN
go mod download

go run ./cmd/migrate up                # 建表（首次执行），version 可查看当前版本
go run ./cmd/migrate version

go run ./cmd/server                    # 启动服务，监听 127.0.0.1:8080
curl http://127.0.0.1:8080/api/v1/health
```

迁移命令：

```bash
go run ./cmd/migrate up        # 应用全部未执行的迁移
go run ./cmd/migrate down [n]  # 回滚 n 步（缺省 1 步）
go run ./cmd/migrate version   # 查看当前版本与 dirty 状态
```

配置读取顺序：`-config` 参数 → 环境变量 `LYIDC_CONFIG` → `./config.yaml` → `./backend/config.yaml` → 内置缺省值。数据库不可用时服务仍会启动，`/api/v1/health` 会返回 `db: "down"`，方便定位环境问题。

后端测试：

```bash
cd backend
gofmt -l .        # 应无输出
go vet ./...
go test ./...
```

### 2. 前端

```bash
cd frontend
npm install
npm run dev       # http://127.0.0.1:5173，/api 代理到 127.0.0.1:8080
```

前端测试与构建：

```bash
cd frontend
npm run typecheck   # tsc --noEmit
npm test            # vitest run
npm run build       # 类型检查 + 生产构建
```

## 文档

- [docs/api-contract.md](docs/api-contract.md)：统一响应包、错误码表、`/api/v1/health` 契约。**后续阶段改接口必须先改本文档再改代码。**
- [frontend/README.md](frontend/README.md)：前端目录与命令速查。

## 阶段规划

- 阶段 0（已完成）：脚手架、配置、数据库连接、迁移、统一响应包、健康检查、CI。
- 阶段 1 及以后：客户、产品、订单、账单、支付、代理对接等业务表与接口（业务代码一律不得写占位假数据）。
