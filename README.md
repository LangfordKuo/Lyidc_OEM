# Lyidc_OEM

岭云互联 IDC 财务系统，代理对接专用：对标魔方财务 / WHMCS 的独立部署 IDC 财务与计费系统。

后端 Go + Gin + GORM + MySQL 5.7，前端 React + Vite + TypeScript + Tailwind CSS v4 + shadcn/ui。
接口契约见 [docs/api-contract.md](docs/api-contract.md)（当前 v14）。

## 快速部署

```bash
git clone https://github.com/LangfordKuo/Lyidc_OEM.git && cd Lyidc_OEM/deploy
cp .env.example .env          # 至少改掉 MYSQL_ROOT_PASSWORD
docker compose up -d
```

浏览器打开 **`http://<主机>:8088/install`**，按向导 6 步完成安装
（第 2 步数据库主机填 `mysql`，勾选自动建库）。装完即用：

- 前台商城 `/`、会员中心 `/console`
- 管理后台 `/admin`（用向导第 4 步设的账号登录）
- 升级：`docker compose pull && docker compose up -d`

> 首次部署全程不需要编辑配置文件：数据库连接与随机 JWT 密钥由安装向导写入数据卷。

其它部署方式（分开 `docker run`、本地二进制、已有 nginx 反代）、环境变量表、国内镜像加速与
故障排查，见 **[docs/deploy.md](docs/deploy.md)**；发版与 tag 规则见 [docs/RELEASE.md](docs/RELEASE.md)。

## 功能概览

- **账号体系**：会员/管理员双受众 JWT、RBAC 角色（admin / finance / cs）、注册登录与资料
- **商品与计费**：上游商品导入、本地定价、6 种计费周期、优惠码规则与校验
- **下单与支付**：购物车下单、易支付在线支付、充值余额与流水、支付回调对账
- **交付与实例**：支付成功自动开通（上游 CreateHost）、实例落库与查询、电源/重装/改密、
  管理端暂停恢复同步、续费下单与自动续费、到期暂停扫描、操作审计
- **运营**：站内通知 + 邮件（SMTP）、到期提醒、工单、后台设置（支付/上游/邮件等全部进库，改完即生效）
- **部署**：浏览器安装向导、容器化交付（GHCR 双镜像）、Release 三平台包

## 目录结构

```
backend/                 Go 后端（module: github.com/LangfordKuo/Lyidc_OEM/backend）
  cmd/server/            HTTP 服务入口（含安装向导模式）
  cmd/migrate/           迁移命令（up / down n / version）
  internal/              config / db / install / auth / model / store / router / upstream /
                         payment / pricing / delivery / scheduler / notify / email / settings
  migrations/            编号 SQL 迁移（go:embed 内嵌进二进制）
frontend/                React 前端（src/api、src/app、src/pages、src/components…）
deploy/                  compose 编排 + .env 示例 + 镜像内的 nginx/入口脚本模板
scripts/                 package.sh（三平台打包）、check_release_workflow.py（交付资产自检）
docs/                    部署指南、发版说明、接口契约
Dockerfile.backend/frontend + .dockerignore
```

## 本地开发

环境：Go 1.25+、Node 20.19+、MySQL 5.7（**无需预先建库建表**，安装向导可代劳）。

**后端**

```bash
cd backend
go run ./cmd/server          # 无配置即进入安装向导模式，浏览器打开 http://127.0.0.1:8080/install
```

手工配置（开发调试常用）：`cp config.example.yaml config.yaml` 后改 `database.dsn`，
再 `go run ./cmd/migrate up` 建表、`go run ./cmd/server` 启动。
配置读取顺序：`-config` → `LYIDC_CONFIG` → `./config.yaml` → `./backend/config.yaml` → 内置缺省值。

**前端**

```bash
cd frontend
npm install
npm run dev                  # http://127.0.0.1:5173，/api 代理到 127.0.0.1:8080
```

**测试与构建**

```bash
cd backend  && gofmt -l . && go vet ./... && go test ./...
cd frontend && npm run typecheck && npm test && npm run build
```

后端集成测试（httptest + 真实 MySQL 5.7）用独立测试库，默认
`root:lyidc123@tcp(127.0.0.1:3306)/lyidc_test`（自动建库并执行迁移），可用 `LYIDC_TEST_DSN` 覆盖；
MySQL 不可达时整体跳过。

## 文档

- [docs/deploy.md](docs/deploy.md)：三种部署方式、环境变量、升级、镜像加速、故障排查
- [docs/RELEASE.md](docs/RELEASE.md)：发版流程、tag 命名规则、发布后核对清单
- [docs/api-contract.md](docs/api-contract.md)：统一响应包、错误码表、各接口契约（**改接口先改这里**）
- [frontend/README.md](frontend/README.md)：前端目录与命令速查

## 阶段进度

已完成阶段 0–9：脚手架与 CI → 账号体系 → 上游对接 → 商品计费（含优惠码）→ 支付财务 →
安装向导 → 订单交付与自动开通 → 实例操作与续费 → 通知体系 → 管理后台 → 交付自动化
（Release 包 + GHCR 镜像 + 部署文档）。前端（官网/会员区/管理后台）已在阶段 9 后整体
重写为 shadcn/ui（R1–R3）。
后续：实例终止流程收敛、`provisioning` 悬挂订单对账等。
