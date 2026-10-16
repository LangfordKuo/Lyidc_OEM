# 部署指南

Lyidc_OEM 支持三种部署方式，**首次部署都用同一个「浏览器安装向导」收尾**：
服务启动后打开 `/install`，6 步填完数据库与管理员，全程不需要手写配置文件。

| 方式 | 适用 | 需要 | 入口 |
| --- | --- | --- | --- |
| ① compose（推荐） | 生产 / 快速试用 | Docker + Compose v2 | `http://<主机>:8088/install` |
| ② 分开 docker run | 已有容器环境或要自定网络 | Docker | `http://<主机>:<前端端口>/install` |
| ③ 本地二进制 | 无容器、要接已有 MySQL/nginx | 无（静态二进制） | `http://<主机>:8080/install` |

前置：MySQL 5.7、Redis 等**都不需要预先准备**——安装向导可以自动建库建表。

---

## 1. compose（推荐）

```bash
git clone https://github.com/LangfordKuo/Lyidc_OEM.git && cd Lyidc_OEM/deploy
cp .env.example .env          # 至少把 MYSQL_ROOT_PASSWORD 改成强口令
docker compose up -d
```

然后浏览器打开 **`http://<主机>:8088/install`**，按向导 6 步走完：

| 步骤 | 要点 |
| --- | --- |
| 1 环境检查 | 全绿即可继续；「配置写入点可写」指的是数据卷 `/app/data` |
| 2 数据库 | **主机填 `mysql`**（compose 服务名）、端口 `3306`、用户 `root`、口令填 `.env` 里的 `MYSQL_ROOT_PASSWORD`、数据库 `lyidc`、勾选「自动创建数据库」 |
| 3 初始化建表 | 点一下即可（后端内置迁移文件，无需外部工具） |
| 4 管理员账号 | 自定义用户名与强口令（不能再用默认 `admin/admin123456`） |
| 5 站点信息 | 站点名称、访问地址（填 `http://<主机>:8088`）、管理员邮箱 |
| 6 完成 | 自动生成随机 JWT 密钥并合并写入 `/app/data/config.yaml`，**免重启**直接生效；`/install` 永久关闭 |

装完即可用：前台 `http://<主机>:8088/`、会员中心 `/console`、管理后台 `/admin`（用第 4 步的账号登录）。

### 服务、端口与卷

| 服务 | 镜像 | 端口 | 说明 |
| --- | --- | --- | --- |
| `mysql` | mysql:5.7 | 仅 compose 网络内 `3306` | 默认不对宿主机暴露；调试用可放开 compose 里注释的 `ports` |
| `backend` | `ghcr.io/langfordkuo/lyidc-oem-backend` | 仅 compose 网络内 `8080` | 接口服务；数据卷挂 `/app/data` |
| `frontend` | `ghcr.io/langfordkuo/lyidc-oem-frontend` | `8088:80` | nginx 托管前端并反代 `/api/v1`、`/install` 到 backend |

| 卷 | 内容 | 删了就怎样 |
| --- | --- | --- |
| `mysql-data` | 全部业务数据 | 数据全丢 |
| `backend-data` | `config.yaml`（数据库 DSN + JWT 密钥） | 重启后回到安装向导 |
| `./backups`（绑定挂载 → 容器 `/app/backups`） | 后台「立即备份」产出的 SQL 与 sidecar | 备份文件被手工删除（不影响业务数据） |

### 备份

两种方式：`mysqldump`（手工）或后台「设置 → 数据库备份」（纯 Go 导出，不依赖 mysqldump；恢复是覆盖式的，需当前管理员密码）。

```bash
# 方式一：mysqldump 直接落宿主机
docker compose exec mysql sh -c 'exec mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" lyidc' > lyidc-$(date +%F).sql
```

```bash
# 方式二：后台点「立即备份」，文件落在 deploy/backups/（compose 已默认挂好，见下）
ls deploy/backups/                              # 宿主机视角（compose 文件旁边）
docker compose exec backend ls /app/backups     # 容器里看到的是同一份
```

- **备份目录默认已挂到宿主机**：compose 里 `backend` 服务的 `./backups:/app/backups` 绑定挂载，
  落点是 compose 文件同级的 `deploy/backups/`（容器内的安装目录子目录，与 `dist/` 同级、**不在 Web 根内**）。
  备份**没有下载入口**（含全库数据与管理员密码哈希，绝不从网站下发），运维取文件就是「直接去 `deploy/backups/` 拿」，
  异地留存用 rsync / scp 等运维手段。`deploy/backups/` 已被 `.gitignore` 忽略，不会进版本库。
- **权限 / 属主**：后端容器默认以 **root** 运行（见下文「数据卷权限」的取舍说明），所以宿主机上目录与文件的
  属主通常是 `root`（目录 0750、文件 0640）；非 root 账号取文件需要 `sudo`（或先 `chown` 再取）。
- 想改用**命名卷**（取文件需 `docker cp`）：把该行换成 `backend-backups:/app/backups`，
  并在 `deploy/docker-compose.yml` 末尾的顶层 `volumes:` 里声明 `backend-backups:`。
- 在后台「设置 → 数据库备份 → 备份目录」里改成**别的路径**时，那个路径**不在**本挂载里，
  需自行加挂载（或把目录改回 `/app/backups`），否则容器重建即丢；改目录**不会搬走已有备份**。
- 两种方式的落点都**绝不要**指向 Web 可访问的目录（如前端镜像里的 `/usr/share/nginx/html`）：
  备份文件含全库数据与管理员密码哈希。

### 升级

```bash
cd deploy
cp -r /path/to/backup .          # 可选：先备份
docker compose pull && docker compose up -d     # 同一数据卷，配置与数据都保留
```

> **容器里不要用后台的「立即升级」**（设置 → 系统更新）：一键升级是**主机直装**场景（替换安装目录里的
> 二进制与 `dist/`、再用分离的 helper 进程重启服务），容器里替换的只是容器可写层——`docker compose
> up -d` 重建容器或换镜像就没了。容器升级一律走上面这两条命令（换镜像 + 手动 `migrate up`，见下）。

注意两点：

- 安装向导只在**首次**执行迁移；若目标版本包含**新迁移**，升级后手动跑一次：
  `docker compose exec backend /app/lyidc-migrate -config /app/data/config.yaml up`
  （`version` 子命令可查看当前版本，`down [n]` 回滚）
- 生产建议把镜像固定到版本号而不是 `latest`（改 `.env` 里的 `LYIDC_BACKEND_IMAGE` / `LYIDC_FRONTEND_IMAGE`，例如 `...:v1.0.0`）。

回滚：把两个镜像地址改回旧版本 + `docker compose up -d`；数据卷不动。迁移已前滚的库回滚要谨慎（必要时先 `lyidc-migrate down N`，并配合数据库备份）。

### 常用改动

- 换端口：`.env` 里 `LYIDC_HTTP_PORT=80`
- 换时区：`.env` 里 `TZ=Asia/Shanghai`（影响日志时间戳与到期扫描）
- 只重建某个服务：`docker compose up -d --force-recreate frontend`

---

## 2. 分开 docker run

不用 compose 时，四条命令（`lyidc-net` 提供容器间主机名解析）：

```bash
docker network create lyidc-net

docker run -d --name lyidc-mysql --network lyidc-net --restart unless-stopped \
  -e MYSQL_ROOT_PASSWORD='<强口令>' -v lyidc-mysql-data:/var/lib/mysql mysql:5.7

docker run -d --name lyidc-backend --network lyidc-net --restart unless-stopped \
  -v lyidc-backend-data:/app/data \
  ghcr.io/langfordkuo/lyidc-oem-backend:latest

docker run -d --name lyidc-frontend --network lyidc-net --restart unless-stopped \
  -p 8088:80 -e BACKEND_UPSTREAM=http://lyidc-backend:8080 \
  ghcr.io/langfordkuo/lyidc-oem-frontend:latest
```

- 安装向导第 2 步的数据库主机填容器名 **`lyidc-mysql`**（不是 `mysql`）。
- `BACKEND_UPSTREAM` 必须指向**后端容器名:8080**；写错不会报错，但 `/api/v1` 与 `/install` 会返回 502。
- 后端容器会自动生成 `/app/data/config.yaml`（`server.addr: 0.0.0.0:8080`），安装向导把 DSN 与 JWT 密钥合并写回该文件。

### 数据卷权限（后端以 root 运行）

镜像默认以 **root** 启动：安装向导要往数据卷写 `config.yaml`，而命名卷默认是 root 属主，
非 root 启动会直接在向导第 1 步「配置写入点可写」报错。这是可用性优先的取舍。

想以非 root 运行（镜像内已建好 `lyidc` 用户，UID/GID 100:101）：

```bash
docker run --rm -v lyidc-backend-data:/app/data alpine chown -R 100:101 /app/data
# 再给容器加 --user lyidc（compose 里写 user: "lyidc"）
```

---

## 3. 本地二进制（Release 产物）

从 Releases 页面下载对应平台的 `lyidc-oem-<版本>-<os>-<arch>.tar.gz`（Windows 为 `.zip`），
并可用同名的 `.sha256` 校验完整性：`sha256sum -c *.sha256`。

### 包内结构

```
lyidc-oem-v1.0.0-linux-amd64/
  lyidc-server            后端可执行文件（CGO_ENABLED=0 静态编译，无 libc 依赖）
  lyidc-migrate           数据库迁移命令（升级含新迁移的版本时用）
  dist/                   前端静态产物（index.html + assets/）
  config.example.yaml     后端配置示例（走安装向导可以不写配置文件）
  deploy/docker-compose.yml / deploy/.env.example   容器编排（方式一/二用）
  docs/deploy.md          本文档
  README.md
  VERSION                 版本号 / 平台 / 构建时间
```

### 后端

```bash
tar -xzf lyidc-oem-v1.0.0-linux-amd64.tar.gz && cd lyidc-oem-v1.0.0-linux-amd64
./lyidc-server                      # 无需配置文件：缺省监听 127.0.0.1:8080 并进入安装向导模式
# 浏览器打开 http://127.0.0.1:8080/install 完成 6 步向导
```

- 配置文件写在**运行目录**的 `config.yaml`，运行账号需对该目录有写权限。
- 要监听公网/其它地址：先手工写一份最小 `config.yaml`（`server.addr: "0.0.0.0:8080"`），或改用
  `./lyidc-server -config /etc/lyidc/config.yaml`；也可用环境变量 `LYIDC_CONFIG` 指定路径（该文件必须已存在）。
- 手工配置（不想走向导）：
  ```bash
  cp config.example.yaml config.yaml     # 改 database.dsn 与 jwt.secret
  ./lyidc-migrate -config config.yaml up # 建表
  ./lyidc-server -config config.yaml
  ```

### 部署检查清单

| ☐ | 检查项 | 口径 |
| --- | --- | --- |
| ☐ | **`jwt.secret` 已非默认值** | `config.yaml` 里的 `jwt.secret` 不能是 `lyidc-dev-secret-change-me`、不能留空，**长度 ≥ 32 个字符**（会员与管理员 token 共用这一个密钥，弱密钥等于管理员身份可被伪造）。**已安装实例会自动轮换**：启动时若检测到默认/弱 `jwt.secret`，服务会生成随机强密钥、写回配置文件并打一条 WARN「已检测到默认/弱 jwt.secret，已自动轮换为随机密钥并写回配置文件」；**轮换会让此前签发的全部令牌立即失效**（用户需重新登录一次）。写回失败（配置只读/权限不足）时**服务照常启动**，只是密钥仅本次进程有效，日志会给出手工修改指引——此时请手工改成 ≥ 32 字符的随机值后重启。详见 `docs/api-contract.md` 第 25 节 |
| ☐ | 安装向导已关闭 | 访问一次 `/install` 应看到「系统已安装」；要重装需清空数据库后重启 |
| ☐ | 数据库口令已改 | `.env` 的 `MYSQL_ROOT_PASSWORD` 不是示例值，且与向导第 2 步填的一致 |

### 上游地址（构建期固定，运行时不可改）

上游「魔方财务系统」的**地址**在打包/构建镜像时烧进二进制（`-X .../internal/buildinfo.UpstreamBaseURL=<地址>`，
与版本号同一机制），运行时只读：后台「设置 → 上游对接」里能看到它并标记为不可修改，**任何界面与接口都改不了**。
这样即使后台账号被冒用，也无法把整站上游指向别处。

- **换地址 = 改构建期变量并重新发布**：改 GitHub 仓库变量 `UPSTREAM_BASE_URL`（Settings → Secrets and
  variables → Actions → Variables）后重跑 Release 流水线（或打新 tag），重新生成发布包与镜像；
  已部署的实例要落到新地址，须用新包/新镜像升级。
- **本地打包**必须显式给地址，缺失会直接报错退出（绝不产出连不上上游的包）：
  `UPSTREAM_BASE_URL=https://lyew.com scripts/package.sh -v v1.0.0`。
- **上游账号与 API 密钥**仍走后台设置（settings 表），改完立即生效、无需重启；库内旧版遗留的
  `base_url` 键会被忽略，属惰性残留，可不管。
- 构建时**未注入**地址的包：启动日志会提示「未注入上游地址」，上游相关功能（商品导入、开通交付、
  实例操作、探活）一律按「未配置」处理，其余功能不受影响——现场见到该提示就说明包打错了。

### 一键自动升级（后台「设置 → 系统更新」）

主机直装（本节的二进制部署）可以在后台**一键升级**，无需登录服务器手工解包：

1. 「设置 → 系统更新」→「检查更新」→ 有新版本时出现「**立即升级**」；
2. 二次确认后自动执行：下载本平台发布包 → **校验 sha256** → 解包 → 用分离的 helper 进程
   备份并替换 `lyidc-server` / `lyidc-migrate` / `dist/` → 跑 `lyidc-migrate up` →
   重启服务 → 健康检查；**失败会自动回滚**（还原二进制与 dist 并重新拉起旧版）。
3. 期间**服务会短暂中断**（接口不可用约数十秒），Nginx 无需 reload（每次请求读文件）。

要点与边界：

- **升级不修改 `config.yaml`**（含数据库连接），也**不备份数据库**；迁移不可逆，回滚只还原文件。
  回滚后页面会提示「数据库迁移已执行」，可用 `./lyidc-migrate version` 核对后重试。
- 安装目录内会新增 `upgrade-state.json`（任务状态）与 `upgrade.log`（升级日志）、`.upgrade/`（工作目录，
  成功后清理）；三者都不含任何密钥。
- 运行账号需要对**安装目录**有写权限（不是配置文件目录）。**容器部署不适用一键升级**（替换的是容器可写层，
  重建即失效）：请用「更新镜像」的方式（见方式一的「升级」小节）。
- 升级源默认 GitHub；镜像/内网环境可在更新源设置里填 `api_base` 与 `download_base`
  （只影响本机出网地址，**属于系统级信任**：指向哪里就从哪里下载二进制）。

### 前端静态托管

`dist/` 是纯静态产物，用任意静态服务器托管，并把 `/api/v1` 与 `/install` 反代到后端。
nginx 片段（与前端镜像内的模板同源）：

```nginx
server {
    listen 80;
    root /path/to/dist;
    index index.html;

    location /api/    { proxy_pass http://127.0.0.1:8080; proxy_set_header Host $host; }
    location /install { proxy_pass http://127.0.0.1:8080; proxy_set_header Host $host; }
    location /        { try_files $uri $uri/ /index.html; }   # SPA 路由回退
}
```

> 只起静态服务器而**不**反代 `/api/v1` 的话：页面能打开但所有接口 404——这是最常见的“装好了却用不了”。
> `/install` 走的也是后端（安装页由后端内嵌提供），同样需要反代。

---

## 4. 环境变量

容器支持的环境变量（后两个由本仓库镜像定义）：

| 变量 | 默认 | 作用 |
| --- | --- | --- |
| `TZ` | `Asia/Shanghai` | 容器时区（日志时间戳、到期扫描） |
| `BACKEND_UPSTREAM` | `http://backend:8080` | **前端镜像**反代目标；填错 = 502 |
| `LYIDC_CONFIG` | 空 | 指定后端配置文件路径（可选；容器内已默认 `/app/data/config.yaml`） |

`deploy/.env`（compose 读取）：

| 变量 | 默认 | 作用 |
| --- | --- | --- |
| `MYSQL_ROOT_PASSWORD` | 无（必填） | MySQL root 口令，安装向导第 2 步要填同一个 |
| `LYIDC_HTTP_PORT` | `8088` | 对外访问端口 |
| `TZ` | `Asia/Shanghai` | 传给后端容器 |
| `LYIDC_BACKEND_IMAGE` / `LYIDC_FRONTEND_IMAGE` | GHCR `latest` | 镜像地址（可换加速前缀或固定版本） |

数据库连接、JWT 密钥**不在环境变量里**：它们由安装向导写进 `/app/data/config.yaml`（0600 权限）。

## 5. 国内镜像加速

GHCR 直连有时很慢，可用公开的 ghcr 代理前缀（第三方服务，可用性自理；生产建议自建或使用云厂商的容器镜像服务）：

```bash
# 方式一：临时拉取后改 tag
docker pull docker.m.daocloud.io/ghcr.io/langfordkuo/lyidc-oem-backend:latest
docker tag  docker.m.daocloud.io/ghcr.io/langfordkuo/lyidc-oem-backend:latest \
            ghcr.io/langfordkuo/lyidc-oem-backend:latest
docker rmi  docker.m.daocloud.io/ghcr.io/langfordkuo/lyidc-oem-backend:latest

# 方式二：直接改 .env（compose 全程走代理前缀）
LYIDC_BACKEND_IMAGE=docker.m.daocloud.io/ghcr.io/langfordkuo/lyidc-oem-backend:latest
LYIDC_FRONTEND_IMAGE=docker.m.daocloud.io/ghcr.io/langfordkuo/lyidc-oem-frontend:latest
```

把 `docker.m.daocloud.io` 换成你手头可用的前缀即可（如 `ghcr.nju.edu.cn` 等）。Release 资产
（tar.gz/zip）下载慢时可挂代理；二进制包本身不含镜像，不受此影响。

## 6. 故障排查

| 现象 | 原因与处置 |
| --- | --- |
| 打开 8088 是 502，首页也可能打不开 | `BACKEND_UPSTREAM` 指向的地址不对/后端容器没起；`docker compose logs backend frontend` |
| 首页正常，接口全 404 | nginx 少了 `/api/` 反代（方式三最常见），或经多层反代丢了路径前缀 |
| `/install` 提示「系统已安装」 | 安装已完成（数据库里有 `installed` 标记）；要重装需清空数据库后重启服务 |
| 向导第 2 步连不上数据库 | 主机名写错（compose 里是 `mysql`，docker run 里是容器名）；口令与 `.env` 不一致；库未勾选自动建库 |
| 向导第 1 步「配置写入点可写」红 | 数据卷属主/权限问题，见上文「数据卷权限」 |
| 容器状态 `unhealthy` | 后端：健康检查要求「已安装且数据库 up」或「安装向导存活」；未安装且向导接口也不通才是真异常，看日志。前端：`unhealthy` 说明 nginx 自身没起来 |
| 升级后报错缺表 | 目标版本含新迁移，跑一次 `lyidc-migrate up`（见「升级」） |
| 后台「上游对接」里地址是空的 / 探活报「上游未配置」 | 包的构建期没注入上游地址（启动日志会提示「未注入上游地址」）：这不是设置问题，改后台无济于事——换用正确构建的包/镜像（见「上游地址」小节） |
| 启动日志出现「已检测到默认/弱 jwt.secret，已自动轮换为随机密钥并写回配置文件」 | **正常且必要**：该实例此前用着公开的开发默认密钥（任何人都能伪造管理员令牌），服务已自愈并把新密钥写回 `config.yaml`。副作用是**所有已登录用户需要重新登录一次**（旧令牌全部失效），配置文件的其它内容未被改动 |
| 启动日志出现「…但写回配置文件失败，已改用仅本次进程有效的随机密钥…」 | 部署账号对配置文件/父目录无写权限，本次密钥只存在内存里。按日志指引把 `config.yaml` 里的 `jwt.secret` 手工改成 ≥ 32 字符的随机值后重启；在此之前每次重启都会重新生成密钥、旧令牌每次失效 |
| 升级/重启后所有人被登出 | 上面两条的预期结果之一（密钥被轮换 → 旧令牌验签失败）。重新登录即可，无数据影响。若不想让用户被登出，请在维护窗口内手工改好 `jwt.secret` 再重启 |

日志：`docker compose logs -f backend`（后端为结构化文本日志，`log.format: json` 可切 JSON）。
