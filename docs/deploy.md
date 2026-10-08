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

备份：`docker compose exec mysql sh -c 'exec mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" lyidc' > lyidc-$(date +%F).sql`

### 升级

```bash
cd deploy
cp -r /path/to/backup .          # 可选：先备份
docker compose pull && docker compose up -d     # 同一数据卷，配置与数据都保留
```

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

日志：`docker compose logs -f backend`（后端为结构化文本日志，`log.format: json` 可切 JSON）。
