# Lyidc_OEM API 契约

> **变更流程**：任何接口变更都必须先修改本文档，再修改后端与前端代码。评审时以本文档为准。
>
> 版本：v31（阶段 0 建立；阶段 1 认证与账号；阶段 2 上游对接与探活；阶段 3a 商品与计费；
> 阶段 3b 六周期 + 优惠码；阶段 4 设置机制 + 易支付 + 充值/余额/流水 + 下单与在线支付；
> 阶段 4+ 站点安装向导：首次访问浏览器完成部署，全程零文件编辑；阶段 5a/5b/5c 订单交付与自动开通、
> 实例操作与续费、取消/终止流程；阶段 6a 工单系统；阶段 6b 通知体系：站内通知 + 邮件 SMTP + 到期提醒 + 事件接线；
> 阶段 8 管理端订单接口；R5 商品简介：上游 description 拉取落库 + 商品卡配置列表（`description_lines`）；
> R6 配置控件矩阵 + 数量型取值口径 + 系统二级选择；
> R7 配置差价联动计价 + 商品两级分类（主分类 / 子分类）；R9-A 商品分类管理；R9-B 商品与分类合并页；
> R10-A 邀请返佣：契约 + 迁移 + 绑定与返佣结算核心；
> R10-B 邀请返佣（二）：自建券 + 提现/转余额（含审核）+ 管理端返佣管理；
> R11 数量型档位区间下放（`tiers`）+ 商品详情页展示调整；
> **R12 导入时按上游分组筛选 + 「上游分组 → 本地子分类」映射（迁移 0017）**；
> **R13-A 系统版本与更新检查：版本号构建期注入 + GitHub Releases 检查更新 + 管理端「系统更新」（第 19 节；无迁移）**；
> **R13-B 一键自动升级：下载 → 校验 → 替换 → 迁移 → 重启 → 失败回滚（第 20 节；无迁移）**；
> **R14 数据库备份与恢复：纯 Go 导出 / 覆盖式恢复 + 管理端「数据库备份」分区（第 21 节；无迁移）**；
> **R15 备份目录设置界面 + compose 默认挂载备份目录（21.10 新增；无迁移）**；
> **R16 上游状态回推：开通登记下游地址 + `POST /api/host/sync` 接收端 + 「站点对外地址」设置（第 22 节；迁移 0018）**；
> **R17 上游地址改为构建期固定、后台只读（8.6 / 12.1 改写；无迁移）**；
> **R18 操作日志：管理员 / 会员 / 定时任务三通道留痕 + 后台查询与清空（第 23 节；迁移 0019）**；
> **R18 批 2 操作日志前端 + 关键字检索执行超时保护（23.4.1 / 23.5 / 23.10 / 新增 23.11；无迁移）**；
> **R19 批 1 缓存基础设施：抽象层 + 文件 / Redis 两种驱动 + 设置与状态 / 重置 / 清空接口 + 管理端缓存卡片
> （第 24 节；不接线任何业务读路径、不改变既有行为；无迁移）**）

## 1. 通用约定

| 项目 | 约定 |
| --- | --- |
| 基础路径 | `/api/v1`（所有业务接口都在该前缀下） |
| 请求方法 | REST 风格，使用 GET / POST / PUT / PATCH / DELETE |
| 请求体 | JSON（`Content-Type: application/json`），字段使用小驼峰或下划线需在具体接口中写明 |
| 响应体 | 统一响应包，见第 2 节，`Content-Type: application/json; charset=utf-8` |
| 时间格式 | RFC3339；数据库时间列存 UTC，账号类接口输出 UTC（`2026-10-08T06:18:31Z`），见 1.2 节；`/health` 的 `time` 保持阶段 0 契约（本地时区） |
| 认证 | 请求头 `Authorization: Bearer <token>`；会员 token 与管理员 token 用 `aud` 区分，见 1.1 节 |
| 开发环境地址 | 后端 `http://127.0.0.1:8080`；前端 `http://127.0.0.1:5173`（`/api` 由 Vite 代理到后端） |

### 1.1 认证方式（阶段 1）

账号体系使用 JWT（HS256）自包含 token，密钥与有效期来自配置 `jwt.secret` / `jwt.expire_hours`（缺省 168 小时 = 7 天）。

| 项目 | 会员 token | 管理员 token |
| --- | --- | --- |
| 签发接口 | `POST /api/v1/auth/login` | `POST /api/v1/admin/auth/login` |
| `aud`（受众） | `member` | `admin` |
| 业务 claims | `member_id`、`username` | `admin_id`、`username`、`role` |
| `iss` / `sub` | `lyidc-oem` / `member:<id>` | `lyidc-oem` / `admin:<id>` |
| 携带方式 | `Authorization: Bearer <token>` | 同左 |

**两类 token 的区分设计**：共用 `jwt.secret`（HS256），用 `aud` 区分受众；校验时按接口要求断言 `aud`，因此会员 token 不能访问管理端接口，管理员 token 也不能访问会员接口，越权访问统一返回 `401`。

其他约定：

1. 令牌算法固定 HS256；校验同时强制 `iss=lyidc-oem`、`exp` 存在且未过期。
2. 每次鉴权都会回查 `members` / `admins` 表：账号被管理员禁用后，已签发的 token 立即失效（返回 `403`），无需等待过期。
3. 缺少 `Authorization` 头、格式不是 `Bearer <token>`、签名/受众/有效期不合法，统一返回 `401`（`code=401`）。
4. 本阶段不提供 token 主动失效（改密码不会踢下线已登录会话），如后续需要可引入 token 版本号。
5. 生产环境必须修改 `jwt.secret`（缺省值是开发默认值，服务启动时会打印告警）。

### 1.2 时间与时区

| 层次 | 约定 |
| --- | --- |
| 数据库 | `members` / `admins` 的 `created_at` / `updated_at` / `last_login_at` 均为 `DATETIME`，统一存 **UTC**；后端启动时强制 DSN `parseTime=true&loc=UTC`，不随开发机时区变化 |
| 接口 | 账号类接口的时间字段输出 RFC3339 **UTC**（`2026-10-08T06:18:31Z`），与 `health` 的 RFC3339 风格一致 |
| 例外 | `GET /api/v1/health` 的 `data.time` 保持阶段 0 契约：服务端当前时间（RFC3339，本地时区） |

## 2. 统一响应包

所有接口（包括错误响应）都返回同一结构：

```json
{
  "code": 0,
  "message": "ok",
  "data": {}
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `code` | int | 业务错误码，`0` 表示成功，非 `0` 表示失败，取值见错误码表 |
| `message` | string | 面向调用方的提示文案，成功时为 `ok` |
| `data` | object \| array \| null | 业务数据；无数据时为 `null`，失败时可能携带上下文信息 |

约定：

1. 只要 `code != 0`，前端一律按失败处理，`data` 中的内容不保证存在。
2. HTTP 状态码与业务错误码同时表达语义：`code` 用于业务分支，HTTP 状态码用于网关/监控，映射规则见第 4 节。
3. 后端实现：`backend/internal/response`（`Success` / `SuccessMessage` / `Fail` / `FailCode` / `FailWithData` / `Abort`）。
4. 前端实现：`frontend/src/api/client.ts` 中 `request()` 负责统一解包，返回 `data` 或抛出 `ApiError`（携带 `code`、`message`、`data`）。
5. **唯一例外：`POST /api/host/sync`（上游状态回推，第 22 节）不套本响应包**。它是**上游（魔方财务）调用本系统**的外部对接接口，
   请求与响应格式由上游约定：请求是 form 表单、响应是 `{"status":200|400,"msg":"…"}`。
   该接口因此**不在 `/api/v1` 前缀下**，前端也不消费它（详见 22.3）。

## 3. 错误码表

| code | 含义 | 典型场景 |
| --- | --- | --- |
| `0` | 成功 | 正常返回 |
| `40001` | 参数错误 | 参数类型/格式不合法（如 JSON 解析失败、密码少于 8 字节、邮箱格式错误、分页参数越界） |
| `40002` | 参数校验失败 | 满足语法但不满足业务规则 |
| `40003` | 缺少必要参数 | 必填字段缺失 |
| `401` | 未认证或凭证无效 | 未登录、Token 过期/受众不符/签名错误、登录密码错误、旧密码校验失败 |
| `403` | 无权访问 | 账号已被禁用（登录或已签发 token 均被拒）、角色权限不足（如 support 改状态） |
| `404` | 资源不存在 | 路由不存在、对象查不到 |
| `409` | 资源冲突 | 唯一键重复（用户名、邮箱）、状态冲突 |
| `500` | 服务器内部错误 | 未预期的服务端异常 |
| `50001` | 数据库错误 | 连接失败、SQL 执行失败 |
| `50002` | 支付渠道调用失败 | 渠道不可达、渠道拒绝下单、渠道响应异常（阶段 4，见 12.2） |
| `50003` | 上游调用失败 | 实例操作/续费回读等上游调用失败（阶段 5b，见 15.6）；message 为已脱敏的上游原因 |
| `50004` | 邮件发送失败 | 管理端「发送测试邮件」发送失败（阶段 6b，见 17.3）；message 为已脱敏的 SMTP 原因，不含认证口令 |

预留区间：`40001-40099` 为参数/校验类错误，`50001-50099` 为服务端具体故障；新增错误码必须同步更新本表与 `backend/internal/response/codes.go`。

## 4. HTTP 状态码映射

| 业务 code | HTTP 状态码 |
| --- | --- |
| `0` | `200` |
| `40001` / `40002` / `40003` | `400` |
| `401` | `401` |
| `403` | `403` |
| `404` | `404` |
| `409` | `409` |
| 其他（含 `500`、`50001`） | `500` |

## 5. 健康检查

### `GET /api/v1/health`

探测服务与数据库连通性，用于本地联调、前端状态展示与后续容器/负载均衡探针。

| 项目 | 说明 |
| --- | --- |
| 方法 | `GET` |
| 路径 | `/api/v1/health` |
| 认证 | 不需要 |
| 请求参数 | 无 |
| 超时 | 服务端探测数据库最多等待 2 秒 |

**成功响应（HTTP 200）**

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "status": "ok",
    "db": "up",
    "time": "2026-10-08T14:18:31+08:00"
  }
}
```

**降级响应（HTTP 503，数据库不可达）**

```json
{
  "code": 500,
  "message": "服务器内部错误",
  "data": {
    "status": "degraded",
    "db": "down",
    "time": "2026-10-08T14:18:31+08:00"
  }
}
```

`data` 字段说明：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `status` | string | `ok`（服务与数据库均正常）；`degraded`（服务在线但数据库不可用） |
| `db` | string | `up` / `down` |
| `time` | string | 服务端当前时间（RFC3339，本地时区） |

**验证方式**

```bash
curl -s http://127.0.0.1:8080/api/v1/health
curl -s http://127.0.0.1:5173/api/v1/health   # 经 Vite 代理，等价于上一行
```

## 6. 认证与账号（阶段 1）

阶段 1 提供会员账号、管理员账号与 RBAC 最小集合。所有接口都在 `/api/v1` 前缀下，统一返回第 2 节的响应包。

### 6.1 对象定义

**会员对象（member）**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int | 会员 ID |
| `username` | string | 用户名（唯一，大小写不敏感） |
| `email` | string | 邮箱（唯一，大小写不敏感） |
| `nickname` | string | 昵称 |
| `phone` | string \| null | 手机号，可空 |
| `status` | string | `active` / `disabled` |
| `balance` | string | 余额，定点小数字符串（如 `"0.00"`，避免浮点误差；流水表留待阶段 4） |
| `created_at` / `updated_at` | string | RFC3339（UTC） |
| `last_login_at` | string \| null | 最后登录时间（RFC3339 UTC），从未登录为 `null` |

```json
{
  "id": 1,
  "username": "alice",
  "email": "alice@example.com",
  "nickname": "爱丽丝",
  "phone": null,
  "status": "active",
  "balance": "0.00",
  "created_at": "2026-10-08T06:18:31Z",
  "updated_at": "2026-10-08T06:18:31Z",
  "last_login_at": "2026-10-08T06:20:02Z"
}
```

**管理员对象（admin）**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int | 管理员 ID |
| `username` | string | 用户名（唯一，大小写不敏感） |
| `nickname` | string | 昵称 |
| `role` | string | `admin`（超级管理员）/ `finance`（财务）/ `support`（客服） |
| `status` | string | `active` / `disabled` |
| `created_at` / `updated_at` / `last_login_at` | string | 同会员对象 |

**密码与敏感字段约定**：密码使用 bcrypt（cost=10）存储，密码规则为 8-72 字节（bcrypt 上限）。任何接口都不得返回 `password_hash`，本阶段所有响应均不包含该字段。

### 6.2 会员端接口

#### `POST /api/v1/auth/register`

注册会员账号。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 不需要 |
| 请求体 | `username`（3-32 位字母/数字/下划线）、`email`（≤128 字节，需符合邮箱格式）、`password`（8-72 字节）、`invite_code`（**可选**，邀请码，见 18.5） |
| 成功 | HTTP 200，`data` 为会员对象（`nickname` 缺省等于 `username`，`status=active`，`balance="0.00"`） |

**`invite_code`（R10-A，契约 18.5）**：字段缺省或为 `null` 表示不走邀请注册（照常注册、不绑定）；
**一旦提供值就必须解析成功**——按 `members.invite_code` 精确匹配（列 `utf8mb4_general_ci`，大小写不敏感）：
写法非法、指向不存在会员、或指向「自己」（同用户名/同邮箱）一律 `40002` 明确报错，**不静默忽略**。
解析成功则写入 `members.inviter_id`，且**首绑不改**：注册时绑定的邀请人永久生效，后续任何路径都不改绑。

```bash
curl -s -X POST http://127.0.0.1:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","email":"alice@example.com","password":"alice123456"}'
```

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "id": 1,
    "username": "alice",
    "email": "alice@example.com",
    "nickname": "alice",
    "phone": null,
    "status": "active",
    "balance": "0.00",
    "created_at": "2026-10-08T06:18:31Z",
    "updated_at": "2026-10-08T06:18:31Z",
    "last_login_at": null
  }
}
```

错误码：

| code | HTTP | 场景与 message |
| --- | --- | --- |
| `40001` | 400 | 请求体非法 JSON、用户名/邮箱格式错误、密码少于 8 字节或超过 72 字节（message 说明具体原因，如「密码至少 8 个字节」） |
| `409` | 409 | 用户名已被占用（`用户名已被占用`）、邮箱已被占用（`邮箱已被占用`）；唯一性大小写不敏感 |

#### `POST /api/v1/auth/login`

会员登录，签发会员 token。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 不需要 |
| 请求体 | `username`、`password` |
| 成功 | HTTP 200，`data` 为 `{token, expires_at, member}`；`expires_at` 为 RFC3339 UTC |

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9....",
    "expires_at": "2026-10-15T06:20:02Z",
    "member": { "id": 1, "username": "alice", "status": "active" }
  }
}
```

错误码：

| code | HTTP | 场景与 message |
| --- | --- | --- |
| `40001` | 400 | 请求体非法、`username` 或 `password` 为空 |
| `401` | 401 | 用户名不存在或密码错误（统一返回 `用户名或密码错误`，不区分两种情况） |
| `403` | 403 | 账号已被禁用（`账号已被禁用`）：凭证正确但账号不可用 |

#### `GET /api/v1/members/me`

查询当前登录会员。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token（`aud=member`） |
| 成功 | HTTP 200，`data` 为会员对象 |
| 错误码 | `401`（未携带/无效/过期 token）、`403`（账号已被禁用） |

#### `PUT /api/v1/members/me`

修改当前会员资料，字段均可选，至少提供一个。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token（`aud=member`） |
| 请求体 | `nickname`（1-32 字符）、`phone`（5-32 字节，可含数字 `+ - ( )` 与空格；传空字符串表示清空）、`email`（规则同注册） |
| 成功 | HTTP 200，`data` 为更新后的会员对象 |
| 错误码 | `40001`（无任何字段、字段格式错误）、`401`、`403`（禁用）、`409`（`邮箱已被占用`） |

```json
{ "nickname": "爱丽丝", "phone": "+86 138-0013-8000", "email": "alice.new@example.com" }
```

#### `POST /api/v1/members/me/password`

修改当前会员密码。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token（`aud=member`） |
| 请求体 | `old_password`、`new_password`（8-72 字节） |
| 成功 | HTTP 200，`data` 为 `null` |
| 错误码 | `40001`（新密码长度不合法）、`401`（旧密码错误，message=`旧密码不正确`；或 token 无效）、`403`（禁用） |

说明：阶段 1 不提供 token 主动失效，改密码后此前签发的 token 在有效期内仍可用（见 1.1 节第 4 条）。

### 6.3 管理端接口

#### `POST /api/v1/admin/auth/login`

管理员登录，签发管理员 token。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 不需要 |
| 请求体 | `username`、`password` |
| 成功 | HTTP 200，`data` 为 `{token, expires_at, admin}` |
| 错误码 | `40001`（参数缺失/非法）、`401`（用户名或密码错误）、`403`（管理员账号已被禁用） |

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9....",
    "expires_at": "2026-10-15T06:22:10Z",
    "admin": { "id": 1, "username": "admin", "role": "admin", "status": "active" }
  }
}
```

#### `GET /api/v1/admin/profile`

查询当前登录管理员。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token（`aud=admin`） |
| 成功 | HTTP 200，`data` 为管理员对象 |
| 错误码 | `401`、`403`（账号已被禁用） |

#### `GET /api/v1/admin/members`

分页查询会员列表（所有管理员角色均可调用）。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token（`aud=admin`） |
| 查询参数 | `page`（默认 1，范围 1-1000000）、`page_size`（默认 20，范围 1-100）、`username`（模糊匹配）、`email`（模糊匹配）、`status`（`active` / `disabled`） |
| 成功 | HTTP 200，`data` 为 `{items, page, page_size, total}`，按 `id` 倒序 |
| 错误码 | `40001`（分页参数越界/非数字、`status` 取值非法）、`401`、`403` |

```bash
curl -s "http://127.0.0.1:8080/api/v1/admin/members?page=1&page_size=20&status=active&username=ali" \
  -H 'Authorization: Bearer <admin-token>'
```

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "items": [ { "id": 1, "username": "alice", "email": "alice@example.com", "status": "active" } ],
    "page": 1,
    "page_size": 20,
    "total": 1
  }
}
```

补充说明：模糊匹配大小写不敏感（utf8mb4_general_ci），`%`、`_`、`\` 按字面量匹配，不作为通配符。

#### `PUT /api/v1/admin/members/:id/status`

启用/禁用会员（改状态类接口）。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token（`aud=admin`），且角色必须是 `admin` 或 `finance` |
| 路径参数 | `id`：会员 ID（正整数） |
| 请求体 | `status`：`active`（启用）/ `disabled`（禁用） |
| 成功 | HTTP 200，`data` 为更新后的会员对象 |
| 错误码 | `40001`（ID 非正整数、`status` 取值非法）、`401`、`403`（角色为 `support`，message=`当前角色无权执行该操作`；或管理员被禁用）、`404`（`会员不存在`） |

行为约定：会员被禁用后，登录返回 `403`（`账号已被禁用`），此前签发的会员 token 访问任何会员接口也返回 `403`；重新启用后恢复。

#### `PUT /api/v1/admin/members/:id`

管理端会员更新接口（R10-B，契约 18.5）。本批只承载**会员级返佣特批比例**；
后续管理端会员可写字段一律走本接口，不再新增 `/members/:id/xxx` 子路径。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token（`aud=admin`），且角色必须是 `admin` 或 `finance`（与 `members.status` 写权限同口径） |
| 路径参数 | `id`：会员 ID（正整数） |
| 请求体 | `rebate_percent_override`（**三态**，见下）；本批不接受其它字段 |
| 成功 | HTTP 200，`data` 为更新后的会员对象 |
| 错误码 | `40001`（ID 非正整数、请求体为空、`rebate_percent_override` 类型既非字符串也非 `null`、比例格式非法）、`40002`（比例越界，需 `0.00`–`100.00`）、`401`、`403`、`404`（`会员不存在`）、`50001` |

**`rebate_percent_override` 三态**（与 12.1 密钥三态、`PUT /admin/products/:id` 的 `rebate_percent` 三态同一套写法）：

| 取值 | 语义 |
| --- | --- |
| 键缺席（不传） | **不改**该字段 |
| `null` | **清空**（落 `NULL`，回到「不覆盖」，生效比例落到全局默认） |
| 字符串 | **设值**，`0.00`–`100.00` 两位小数；非法格式 `40001`、越界 `40002` |

设值后立即生效：下一笔支付结算返佣时按「会员级 > 全局默认」取值（三级优先级的第二级，
见 18.2）；已发放的返佣流水不受影响（`ratio_percent` 是发放时快照）。
管理端会员视图（`GET /admin/members` 列表与本接口响应）输出 `rebate_percent_override`
（`string \| null`），供页面显示「已特批 x%」。

### 6.4 角色权限矩阵

| 接口 | admin | finance | support |
| --- | --- | --- | --- |
| `POST /api/v1/admin/auth/login` | ✓ | ✓ | ✓ |
| `GET /api/v1/admin/profile` | ✓ | ✓ | ✓ |
| `GET /api/v1/admin/members` | ✓ | ✓ | ✓ |
| `PUT /api/v1/admin/members/:id/status` | ✓ | ✓ | ✗（`403`） |
| `PUT /api/v1/admin/members/:id`（R10-B，会员级特批比例） | ✓ | ✓ | ✗（`403`） |

会员端接口（`/api/v1/members/*`）只接受会员 token；管理员 token 访问返回 `401`。角色守卫挂在路由层（`internal/router`）：`requireAdmin` 负责 token 与账号状态，`requireAdminRole` 负责角色白名单。

### 6.5 开发默认管理员

迁移 `0003` 会写入一个开发默认管理员，便于本地与 CI 联调：

| 用户名 | 密码 | 角色 |
| --- | --- | --- |
| `admin` | `admin123456` | `admin` |

**警告**：生产环境部署后必须立即修改该密码；阶段 1 没有管理员管理接口（改密码需直接执行 SQL 或等待后续阶段接口），也不提供管理员注册接口。

### 6.6 暂不支持的能力

| 能力 | 说明 |
| --- | --- |
| 忘记密码 / 重置密码 | **留待阶段 6（邮件通知体系）**：需要邮件验证码或重置链接，本阶段不提供 |
| 管理员账号管理（增删改、改密） | 留待后续阶段 |
| refresh token / token 主动失效 | 阶段 1 使用 7 天有效期的一次性签发 token |
| 余额流水 | `members.balance` 仅落字段，流水表 `ledger` 留待阶段 4（支付财务） |

## 7. 错误响应示例

```bash
curl -s http://127.0.0.1:8080/api/v1/nope
```

```json
{
  "code": 404,
  "message": "接口不存在",
  "data": null
}
```

## 8. 上游对接（阶段 2）

本节描述 Lyidc_OEM 作为**下游**对接上游「魔方财务系统」（智简魔方）时的真实接口契约。
结论来自上游 v3.7.5 源码 + 对生产站 `https://lyew.com` 的实测（2026-10-08）。

### 8.1 上游系统与鉴权机制

| 项目 | 结论 |
| --- | --- |
| 上游系统 | 智简魔方「魔方财务系统」v3.7.5（生产站 `https://lyew.com`） |
| 版本判定依据 | `GET /doc` 返回内置接口文档站；业务响应的 `is_aff` 字段来自 v3.7.5 `jsons()` 注入；实测路由 `/zjmf_api_login`、`/cart/*`、`/api/product/*` 均按 v3.7.5 源码定义响应 |
| 鉴权方式 | **两步：先换 JWT，再带 Bearer 头**（不是签名，也不是 Query 参数） |
| 凭证 | 「用户名」+「API 密钥」（上游前台 安全中心 → API 生成的 12 位随机串，**不是登录密码**）。**用户名必须是注册手机号**：上游对 `username` 有 4–20 字符硬校验，长邮箱会被拒（见 8.5 第 1 条） |
| 密钥传输 | 仅在 `POST /zjmf_api_login` 的表单体中传输一次，之后所有请求只用 JWT |

**第一步：换取 JWT**

```http
POST {base_url}/zjmf_api_login
Content-Type: application/x-www-form-urlencoded

username=<上游注册手机号，必须 4-20 字符>&password=<API 密钥>
```

实测（2026-10-08，密钥与 JWT 已脱敏）：

```bash
curl -s -X POST https://lyew.com/zjmf_api_login \
  -d 'username=<注册手机号，11 位>' -d 'password=<API 密钥>'
```

```json
{"jwt":"eyJ0****Er8","status":200,"msg":"鉴权成功","is_aff":"1"}
```

**第二步：携带 JWT 调用业务接口**

```http
POST {base_url}/cart/hostinfo
Authorization: Bearer <jwt>
```

JWT 由上游服务端缓存（键 `client_user_login_token_<jwt>`）二次校验：未登录、JWT 失效或已过期时，上游返回 `{"status":405,"msg":"请登陆后再试"}`，此时必须**重新登录并原样重放一次请求**（上游官方下游实现即为此策略）。

前置条件（缺一不可，否则上游返回业务错误而不是鉴权通过）：

1. 上游站点开启「资源 API」总开关（配置项 `allow_resource_api`）；
2. 该账号已开通 API（`clients.api_open = 1`），未开通时只读接口返回 `{"status":400,"msg":"暂未开通API功能"}`。

上游还存在另外两套鉴权，本阶段**不使用**，仅记录以免混淆：

| 体系 | 位置 | 方式 |
| --- | --- | --- |
| 开放 API 账号 | `/api/host_server`、`/api/host`、`/api/host/free` | `Authorization: Basic base64(username:password)`，账号来自上游后台「API」（`api` 表），并校验 **IP 白名单**（不在白名单返回 `当前IP不允许访问`） |
| 前台登录会话 | 全部前台路由（`/provision/*`、`/dcim/*`、`/host/*`） | 前台登录 Cookie 中的同款 JWT，或 `Authorization: JWT <jwt>` |

### 8.2 请求/响应包格式与状态码映射

上游响应统一为：

```json
{ "status": 200, "msg": "请求成功", "data": {}, "is_aff": "1" }
```

| 上游字段 | 说明 |
| --- | --- |
| `status` | **业务状态码，不是 HTTP 状态码**；成功为 `200` |
| `msg` | 提示文案（中文） |
| `data` | 业务数据；注意部分接口把结果直接挂在 `data` 下，部分接口直接挂在顶层（如 `jwt`） |
| `is_aff` | 上游 v3.7.5 注入的推广开关，与本项目无关，解析时忽略 |

**重要**：上游对业务错误同样返回 **HTTP 200**，因此**必须以 body 中的 `status` 判定成败**，不能看 HTTP 状态码。

| 上游 status | 含义 | 我方映射（`backend/internal/upstream`） |
| --- | --- | --- |
| `200` | 成功 | 正常返回 |
| `202` | 已受理、异步待处理（实测 `/host/cancel` 终止申请） | 视为已受理，`Resp.Status` 原样回传给调用方判断 |
| `1001` | 特例：`/apply_credit` 余额支付成功 | 视为成功（与 `200` 等价） |
| `400` | 业务失败（账号或密码错误、API 未开通、操作被拒） | `ErrBusiness`（`/zjmf_api_login` 场景为 `ErrAuth`） |
| `405` | 未登录或 JWT 失效（`请登陆后再试`） | 内部触发一次重新登录并重放；仍失败则 `ErrNotLoggedIn` |
| `406` | 业务校验失败（ID 错误、不支持的模块方法等） | `ErrBusiness` |
| 其他 | 上游未定义的错误码 | `ErrUpstream` |
| — | 网络错误 / 超时 / 非 JSON 响应 | `ErrNetwork`（可 `errors.Is` 判定） |

### 8.3 本阶段封装的上游接口

| 我方方法（`internal/upstream`） | 上游接口 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| `Login` | `POST /zjmf_api_login` | `username`、`password` | 换 JWT，自动缓存 |
| `Products` | `GET /cart/all` | — | 商品（产品组 → 商品）列表 |
| `ProductConfig` | `GET /cart/get_product_config` | `pid` | 可配置选项与价格周期 |
| `Stock` | `GET /cart/stock_control` | `pid` | 库存/是否控库存 |
| `OntrialMax` | `GET /cart/ontrialmax` | `pid` | 试用数量与最大购买数 |
| `Hosts` / `Host` | `GET /cart/hostinfo` | `hostid[]`、`all` | 已购产品（主机）列表 / 单主机详情 |
| `Credit` | `GET /cart/credit` | — | 账号余额与货币 |
| `CloudOS` | `GET /host/cloudos` | `productid`、`os_config_option_id` | 重装可选的操作系统列表 |
| `Summary` | `GET /cart/summary` | — | API 概览；**API 登录（机器凭证）调用返回 403**，见 8.5 第 2 条 |
| `RequestCancel` | `POST /host/cancel` | `id`、`type`(Immediate/Endofbilling)、`reason` | 提交取消（终止）申请；实测返回 `status=202` + `pending=true`，见 8.5 第 6 条 |
| `On` / `Off` / `Reboot` / `HardOff` / `HardReboot` / `Status` / `VNC` | `POST /provision/default` | `func`、`id` | 开关机、重启、电源状态 |
| `Reinstall` | `POST /provision/default` | `func=reinstall`、`id`、`os`、`port` | 重装系统 |
| `ResetPassword` | `POST /provision/default` | `func=crack_pass`、`id`、`password` | 重置密码 |
| `RescueSystem` | `POST /provision/default` | `func=rescue_system`、`id`、`system` | 救援系统 |
| `Suspend` / `Unsuspend` | `POST /provision/default` | `func=suspend/unsuspend`、`id`、`reason` | 暂停 / 恢复 |
| `CustomButton` | `POST /provision/button` | `id`、`func` | 模块自定义按钮方法 |
| `CreateHost`（开通） | `POST /cart/clear` → `POST /cart/add_to_shop` → `POST /cart/settle` → `POST /apply_credit` | 见下 | 上游下单并用余额付款，成功后 `data.hostid[]` 为上游主机 ID |
| `RenewHost`（续费） | `POST /host/renew` → `POST /apply_credit` | `hostid`、`billingcycles`；`invoiceid`、`use_credit=1` | 生成续费账单并余额支付 |

开通参数（对齐上游 v3.7.5 下游实现 `app/common/logic/Host.php`）：

| 参数 | 说明 |
| --- | --- |
| `pid` | 上游商品 ID（`Products` 返回的 `id`） |
| `billingcycle` | 计费周期（`monthly` / `quarterly` / `annually` …） |
| `host` | 主机名 |
| `password` | 主机密码 |
| `currencyid` | 上游货币 ID（来自 `/cart/clear` 的 `user.currency`） |
| `qty` | 数量（本阶段固定 1） |
| `configoption[<上游配置项 ID>]` | 可配置项取值（数量型传 `qty`，选项型传上游选项 ID） |
| `customfield[<上游自定义字段 ID>]` | 自定义字段值 |

`/apply_credit` 的 `status=1001` 表示余额支付成功（`data.hostid[]` 为开通出的主机 ID）；`status=200` 表示支付失败（余额不足等）。

### 8.4 实测示例（密钥与 JWT 已脱敏）

未鉴权调用（HTTP 200，业务 400；`/cart/summary` 在未登录时因取不到 `uid` 直接判为未开通 API）：

```bash
curl -s -X POST https://lyew.com/cart/summary
```

```json
{"status":400,"msg":"暂未开通API功能","is_aff":"1"}
```

带上 JWT 后的只读接口（实测主机 10919 的真实返回，字段已裁剪）：

```bash
curl -s 'https://lyew.com/cart/hostinfo?hostid%5B%5D=10919' \
  -H 'Authorization: Bearer <JWT>'
```

```json
{"status":200,"msg":"请求成功","data":{"hosts":[
  {"id":10919,"productid":1,"domain":"lyidc-it-48201.example.com",
   "dedicatedip":"156.233.235.183","assignedips":[""],"create_time":1791448202,
   "nextduedate":1796718606,"billingcycle":"monthly","billingcycle_zh":"月付",
   "firstpaymentamount":"20.00","amount":"20.00","port":0,"username":"root",
   "initiative_renew":0,"domainstatus":"Active",
   "domainstatus_zh":{"name":"已激活","color":"#3fbf70"}}],
 "currency":"¥"},"is_aff":"1"}
```

开通（`/cart/clear` → `/cart/add_to_shop` → `/cart/settle` → `/apply_credit` 四步全成功后的返回）：

```json
开通: 成功 —— 上游 hostid=10919 invoiceid=840727 paid=true
```

生命周期调用（`POST /provision/default`，实测返回）：

```json
{"status":200,"msg":"","data":{"status":"on","des":"开机"}}     // func=status
{"status":200,"msg":"关机成功","data":null}                      // func=off
{"status":200,"msg":"发起重启成功","data":null}                   // func=reboot
{"status":200,"msg":"重装发起成功","data":null}                   // func=reinstall（异步，随后 status 查得 process/重装中）
{"status":200,"msg":"暂停成功","data":null}                       // func=suspend
{"status":200,"msg":"解除暂停成功","data":null}                    // func=unsuspend
```

续费（`/host/renew` + `/apply_credit`）：

```json
续费: 成功 —— invoiceid=840728 paid=true
```

只读接口（无需鉴权即可返回商品目录，实测 2026-10-08 返回 200）：

```bash
curl -s https://lyew.com/api/product/list
```

```json
{"status":200,"msg":"请求成功","data":{"list":[{"id":1,"type":"dcimcloud","gid":1,
"name":"香港二区 CN2 A型","pay_type":"recurring_prepayment","stock_control":1,"qty":71,
"product_price":"20.00","billingcycle":"monthly","billingcycle_zh":"月"}, ...]}}
```

余额接口（未携带 JWT 时 `credit` 为 `null`，携带有效 JWT 后为账号余额）：

```bash
curl -s https://lyew.com/cart/credit
```

```json
{"status":200,"msg":"请求成功","data":{"credit":null,
"currency":{"id":1,"code":"CNY","prefix":"¥","suffix":"元"}},"is_aff":"1"}
```

携带有效 JWT 时（该测试账号实测余额）：

```json
{"status":200,"msg":"请求成功","data":{"credit":"10000.00",
"currency":{"id":1,"code":"CNY","prefix":"¥","suffix":"元"}},"is_aff":"1"}
```

通过 `/api/*` 的路由还会返回 `{"status":400,"msg":"请输入用户名密码"}`（缺 Basic 头）或 `{"status":400,"msg":"当前IP不允许访问"}`（IP 未加白）。

### 8.5 实测与文档不符之处 / 已知限制

1. **`username` 必须是注册手机号，长邮箱过不了长度校验（已解决）**：上游 `app/home/controller/LoginController.php` 的 `zjmfApiLogin` / `resourceLogin` 均使用 `require|length:4,20`，**超过 20 字符的邮箱一律在校验阶段失败并统一返回 `鉴权失败`**（与密码错误同文案，无法区分）。上游文档标注 username 为「用户名(手机号+区号)」——手机号 ≤ 20 字符可用，长邮箱不可用。

   定位过程（2026-10-08，可复跑脚本 `scratch/auth_evidence.sh`）：

   | # | 请求 | 响应 | 说明 |
   | --- | --- | --- | --- |
   | 1 | `POST /zjmf_api_login`，username=`langfordkuo@foxmail.com`（23 字符）+ 正确 API 密钥 | `{"status":400,"msg":"鉴权失败"}` | 换 JWT 失败 |
   | 2 | `POST /zjmf_api_login`，同账号 + **错误**密码 | `{"status":400,"msg":"鉴权失败"}` | 与 1 文案相同 ⇒ 无法用文案区分「长度不合法」与「密码错误」 |
   | 3 | `POST /resource_login_supplier`（同一套 4–20 校验），同账号 + 错误密码 | `{"status":400,"msg":"鉴权失败"}` | 该接口密码错时本应返回 `账号或密码错误` ⇒ **用户查询根本没执行**，卡在长度校验 |
   | 4 | `POST /login_pass_email`，同一邮箱 | `{"status":400,"msg":"账号或密码错误"}` | 该邮箱在上游**已注册** |
   | 5 | `POST /login_pass_email`，不存在的邮箱 | `{"status":400,"msg":"邮箱未注册"}` | 负对照，证明 4 的判读成立 |
   | 6 | `POST /zjmf_api_login`，username=**注册手机号**（11 字符）+ API 密钥 | `{"jwt":"eyJ0****Er8","status":200,"msg":"鉴权成功"}` | **改用手机号后鉴权成功**，全部需鉴权接口可用 |

   结论：`upstream.username` 填该账号在上游绑定的手机号（`backend/config.yaml` 已如此配置），邮箱仅作为账号标识，不能用于 API 登录。

2. **`/cart/summary` 对 API 登录返回 403**：携带 JWT 调用时上游返回
   `{"status":403,"msg":"Machine credentials cannot access member security operations"}`——
   上游把 API 登录的 JWT 视为「机器凭证」，禁止其访问会员安全类操作。因此 API 概览接口在本阶段**不可用**（`internal/upstream.Client.Summary` 保留但会返回 `ErrBusiness`），账号/主机统计改用 `Hosts` + `Credit` 组合获得。该 403 用英文返回，与上游其它中文文案风格不一致，疑似新增的权限层。
3. **上游官方文档站不含资源 API 定义**：`http://w2.test.idcsmart.com/doc?name=...`（已抓取于 `docs/references/upstream/`）只覆盖前台/后台管理控制器，`/zjmf_api_login`、`/cart/*`、`/provision/default` 等资源 API 均无文档；本节清单来自 v3.7.5 源码与实测。
4. **响应包风格不同**：上游 `{status,msg,data}` vs 我方 `{code,message,data}`。上游包**不进入**我方对外接口，只在上游客户端内部映射（`backend/internal/upstream/errors.go`）。
5. **业务错误也返回 HTTP 200**：不可用 HTTP 状态码判成败（本阶段实测全部命中该行为）。
6. **`/cart/all` 未鉴权也返回数据**：上游未对该只读接口强制登录；我方客户端仍要求先登录，避免误用匿名态数据（实测该接口匿名态返回 157 479 字节 / 29 个分组 / 159 个商品，已用于验证解析层）。
7. **开通/续费是「下单 + 余额支付」两步**：上游没有单独的「开通」接口，必须 `add_to_shop`/`settle` 后再 `apply_credit` 扣上游余额；余额不足时 `apply_credit` 返回业务失败（非异常）。
8. **`/apply_credit` 的支付成功码是 `1001` 而不是 `200`**：上游返回 `status=200` 表示「已生成账单但未支付」（上游官方下游实现即把 200 当作失败处理），只有 `1001` 才是余额支付成功且回带 `data.hostid[]`。
9. **业务状态码与内层 `data.status` 同名不同义**：`/provision/default` 的外层 `status=200` 是业务成功码，内层 `data.status` 是电源状态文案（`"on"`/`"off"`），解析时不能合并成同一个字段（`internal/upstream.ProvisionResult.DataField` 负责取内层值）。
10. **部分接口把结果放在顶层**：`/zjmf_api_login` 的 `jwt`、`/cart/clear` 的 `hostid`/`invoiceid`/`user` 都在响应顶层而非 `data` 内；`/cart/settle`、`/apply_credit` 的 `hostid` 又出现在 `data` 内。客户端对两处都做兼容（`Response.HostIDs`）。
11. **字段类型与「文档/直觉」不符（实测踩坑，已按真实类型建模）**：

    | 字段 | 直觉/文档 | 实测真实类型 | 备注 |
    | --- | --- | --- | --- |
    | `hosts[].nextduedate` | 日期字符串 `2026-11-08` | **unix 秒（int）** `1796718606` | 按字符串建模会导致整条主机列表解析失败 |
    | `hosts[].create_time` | — | unix 秒（int） | 同上 |
    | `hosts[].domainstatus_zh` | 字符串 | **对象** `{"name":"已激活","color":"#3fbf70"}` | 用 `any` 承接 |
    | `hosts[].billingcycle_zh` | 字符串 | 字符串 `月付` | — |
    | `hosts[].assignedips` | 字符串 | 数组（可能含空字符串） | 已展开为 `[]string` |
    | `cloud_os[].group` | 分组 ID（int） | **分组名字符串** `CentOS` | — |
    | `cloud_os_group[].id` | 分组 ID（int） | **字符串**（即分组名） | — |
    | `/host/cloudos` 响应 | 含 `msg` | 只有 `{status,data}`，无 `msg` | 客户端 `Msg` 为空属正常 |

12. **`/provision/default` 的成功语义与文案**（实测 2026-10-08，主机 10919）：
    - 外层 `status=200` + `msg` 为中文成功文案：`关机成功`、`开机成功`、`发起重启成功`、`重装发起成功`、`暂停成功`、`解除暂停成功`；
    - `func=status` 返回内层 `data.status`：`on`（开机）/`off`（关机）/`process`（重装等异步进行中），另有 `data.des` 中文描述；
    - 重装是**异步**的：`重装发起成功` 之后 `status` 查询返回 `{"status":"process","des":"重装中"}`，接入方不能把「发起成功」当作「重装完成」。
13. **取消（终止）申请是异步且幂等的**：`POST /host/cancel` 实测返回

    ```json
    {"status":202,"msg":"mf_cloud_finance_termination_pending","pending":true,
     "data":{"cancel_request_id":431,"domainstatus":"Active","id":"10919","idempotent":1}}
    ```

    要点：`status=202` 表示**已受理、待上游处理**（不是失败，客户端按已受理处理）；`pending=true`；同一主机重复提交返回 `idempotent=1` 且不产生重复申请；主机状态**不会立即变**（仍为 `Active`），实际终止由上游财务/魔方云流程执行。
14. **前端路由的 `is_api` 分支**：上游用 JWT 中的 `is_api=1` 跳过二次验证（`secondVerifyResultHome`），因此 API 登录可免 2FA 调用 `/provision/default` 等前台接口；同时也带来上面第 2 条的 403 限制（机器凭证不得访问会员安全操作）。
15. **`/cart/hostinfo` 必须带 `hostid[]`**：实测不带该参数时上游返回**空列表**（不是「账号下全部主机」），带 `hostid[]=<id>` 才返回对应主机。上游文档把该参数标为必填，但未说明缺省时返回空——接入方需自行维护上游主机 ID 清单（本阶段由我方订单表承担，见后续阶段）。
16. **资金消耗核对**：测试账号在上游的余额从 `10000.00` 降到 `9960.00`，与「开通 20.00 + 续费 20.00」一致，说明 `apply_credit` 的扣费链路真实生效。

### 8.6 配置项（阶段 4 起：**由后台设置承载**；R17 起**地址改为构建期注入**）

上游对接的**凭据**（账号 / API 密钥 / 超时）自阶段 4 起由「后台管理设置」承载（settings 表 + 管理端接口，见 12.1），
**改完立即生效、无需重启**；`backend/config.yaml` 的 `upstream` 段已停用并删除（`config` 包不再解析该段）。
本文件只保留部署级参数（监听地址、数据库连接、JWT 密钥、日志等级）。

**R17（上游地址构建期固定）**：上游**地址**不再是可以后台修改的设置，改为**构建期注入**——与版本号同一套
`-ldflags -X` 机制写入 `internal/buildinfo.UpstreamBaseURL`（打包脚本 `scripts/package.sh` 要求环境变量
`UPSTREAM_BASE_URL` 非空，否则直接失败；发布流水线校验仓库变量后注入发布包与镜像），运行时只读。
动机：地址可由后台修改等于「后台账号被冒用就能把整站上游指向同行」，构建期固定后**任何界面与接口都改不了**。

| 设置字段（键 `upstream`） | 类型 | 说明 |
| --- | --- | --- |
| `username` | string | 上游账号（**必须是注册手机号**，上游对 username 有 4-20 字符硬校验，见 8.5 第 1 条） |
| `api_key` | string | 上游 API 密钥（12 位随机串，不是登录密码）；写入后接口只回 `api_key_configured` 与掩码，**绝不回显明文** |
| `timeout_seconds` | int | 单次上游请求超时，缺省 5，取值 1-120 |

- **上游地址不在本表内**：它来自构建期（`base_url` 只作为 `GET` 的**只读**回显字段 + `base_url_fixed=true`，
  见 12.1）；`backend/config.yaml` 同样不承载它。
- **库内遗留的 `base_url` 键被忽略**：阶段 4 起旧版曾把它写进 `upstream` 键的 JSON；R17 后代码不再读、不再写该键，
  它属**惰性残留**（存留原样不影响任何行为，清理与否都不影响）。PUT 请求体里出现 `base_url` 一律被拒（见 12.1）。

「未配置」口径（R17 改写）：**构建期地址为空 或 `username` / `api_key` 为空**，即视为未配置——
服务照常启动，仅上游相关功能按「未配置」处理（探活返回 `connected=false` + `error="上游未配置"`，
商品导入返回明确错误），**其余功能不受影响**。只填了一部分凭据时同样按未配置处理，
但设置接口仍能看到已填字段（便于管理员分步填写）。

读取/写入接口：`GET` / `PUT /api/v1/admin/settings/upstream`（仅 `admin` 角色，见 12.1）。
**注意 `GET` 返回的地址恒为构建期值，`PUT` 无法修改它。**

**生效方式**：上游客户端按当前设置**动态构造**——每次使用都读一次 settings（主键查询，开销极小），
设置内容未变时复用现有客户端（**保留其 JWT 缓存**，不重复登录），内容变化时立即重建客户端并丢弃旧 JWT
缓存（契约 12.1）。因此不存在「启动时读一次」的陈旧配置；地址则恒为构建期注入值。

## 9. 管理端上游探活接口（阶段 2）

### `GET /api/v1/admin/upstream/health`

管理员鉴权（`Authorization: Bearer <admin token>`）后探测上游连通性：客户端会向上游发起一次只读调用（`POST /zjmf_api_login` 换取 JWT + `GET /cart/credit`）。

| 项目 | 说明 |
| --- | --- |
| 方法与路径 | `GET /api/v1/admin/upstream/health` |
| 鉴权 | 管理员 token（`aud=admin`），与 `/api/v1/admin/profile` 一致 |
| 请求参数 | 无 |
| 上游调用 | 登录 + 读取余额（只读，不产生写操作） |
| 上游参数 | 凭据（`username` / `api_key` / `timeout_seconds`）**按当前后台设置动态读取**（键 `upstream`，见 8.6 / 12.1）；**地址是构建期注入值**（8.6，R17，不可修改）；未配置时不发起请求 |
| 超时 | 探活整体超时缺省 5s（客户端单次请求超时取设置里的 `timeout_seconds`，缺省 5s） |

成功（HTTP 200，下为 2026-10-08 对生产上游的实测响应）：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "connected": true,
    "base_url": "https://lyew.com",
    "latency_ms": 354,
    "api_key_masked": "1sXR****ZG5",
    "checked_at": "2026-10-08T08:32:44Z"
  }
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `connected` | bool | 上游只读调用是否成功 |
| `base_url` | string | 上游地址（构建期注入值，非密；R17 起不由任何接口修改） |
| `latency_ms` | int | 本次探活耗时（毫秒） |
| `api_key_masked` | string | 脱敏后的 API 密钥，**仅保留首 4 位与后 3 位**（如 `1sXR****ZG5`）；未配置时为空串。注意与设置接口的掩码口径不同（12.1），两者都不含明文 |
| `checked_at` | string | 探测完成时间（RFC3339，UTC） |

失败（HTTP 200，`connected=false`；探活失败属业务结果而非接口错误）：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "connected": false,
    "base_url": "https://lyew.com",
    "latency_ms": 680,
    "api_key_masked": "1sXR****ZG5",
    "checked_at": "2026-10-08T07:31:02Z",
    "error": "上游返回业务失败: 鉴权失败 (status=400)"
  }
}
```

约定：

1. 上游未配置时不发起请求，直接返回 `connected=false` 与 `error="上游未配置"`；
   R17 起「未配置」口径 = **构建期地址为空 或 `username` / `api_key` 为空**（见 8.6）。
   此时响应仍回带构建期地址（若非空）与掩码，便于管理员排查。
2. 探活失败**不返回 5xx**，便于前端把它当作状态展示而不是错误弹窗；管理员 token 无效仍按第 1.1 节返回 `401`。
3. 密钥只以脱敏形式出现在响应与日志中（`1sXR****ZG5`），完整密钥只存在于 settings 表与本进程内存
   （阶段 4 起不再写配置文件）。
4. 探活每次都主动登录一次（验证鉴权链路可用），因此它**不反映**客户端的 JWT 缓存状态；普通业务调用
   （如商品导入）才复用缓存中的 JWT。

## 10. 商品与计费（阶段 3a 交付；阶段 3b 起计费周期扩为 6 个）

本节描述商品目录的导入、本地定价与上下架。数据来源是上游「魔方财务系统」的**只读**接口
（`GET /cart/all` + `GET /cart/get_product_config`，见第 8 节），本阶段对上游不做任何写操作。

### 10.1 数据模型与字段归属

**商品分组（product_group，本地表 `product_groups`）**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int | 本地分组 ID（管理端过滤/排序用，也是会员端分组 ID） |
| `upstream_group_id` | int | 上游分组 ID（唯一键） |
| `name` | string | 分组名（**本地字段**：首次导入取自上游，之后只能在管理端改） |
| `sort` | int | 排序值（**本地字段**：首次导入按上游顺序写入，升序） |
| `created_at` / `updated_at` | string | RFC3339（UTC） |

**商品（product，本地表 `products`）**

| 字段 | 类型 | 归属 | 说明 |
| --- | --- | --- | --- |
| `id` | int | 本地 | 本地商品 ID（对外主键，会员端只用它） |
| `upstream_pid` | int | 上游 | 上游商品 ID（唯一键；阶段 4 下单时作为 `pid`） |
| `upstream_group_id` | int | 上游 | 所属上游分组 ID |
| `name` / `description` / `type` / `module` | string | 上游 | 商品名/描述/类型/模块；`description` 存**上游原文经一次 HTML 实体反转义后的原始 HTML**（R5 口径：`&lt;li&gt;CPU:2核心&lt;/li&gt;` → `<li>CPU:2核心</li>`，见迁移 0012），接口层再解析成行数组下发，JSON 里敏感字符按常规转义 |
| `config_json` | object | 上游 | `GET /cart/get_product_config` 的 `data` 原文缓存（可配置项、自定义字段、价格行） |
| `upstream_prices_json` | object | 上游 | 上游周期价格原文与挑选结果，见 10.2 |
| `pricing_json` | object | **本地** | 本地定价规则，见 10.2 |
| `stock_qty` | int | 上游 | 库存数量（`stock_control=1` 时有效） |
| `ontrial_max` | int | 上游 | 可试用数量（`0` 表示不提供试用） |
| `status` | string | **本地** | `on` 上架 / `off` 下架（导入的新商品默认 `off`） |
| `sort` | int | **本地** | 排序值（首次导入按上游顺序写入，之后由管理端控制，升序） |
| `created_at` / `updated_at` | string | — | RFC3339（UTC） |

**导入的写入边界（幂等的关键）**：导入只覆盖**上游字段**（`name`、`description`、`type`、`module`、
`config_json`、`upstream_prices_json`、`stock_qty`、`ontrial_max`，以及商品的 `upstream_group_id`）；
**本地字段**（`pricing_json`、`status`、`sort`，分组的 `name`、`sort`）在更新分支**永不覆盖**——
重复导入不会冲掉本地定价与上下架状态。因此「重复导入仅计数变化」：第二次导入同一批数据必然返回
`created=0, updated=0, unchanged=<商品总数>`。
R5 起 `description` 落库前额外做一次 HTML 实体反转义（口径见 10.1 字段表与迁移 0012）：
该转换是**确定性**的——R5 前的转义存量在首次导入时计一次 `updated`，之后重复导入仍为 `unchanged`。

**商品两级分类（R7，迁移 0014）**

会员端要「选择香港分类 → 香港二区 / 香港三区」两级筛选，因此在 `product_groups`（上游产品组的本地映射）
**之外**新增独立的分类树。取舍说明：

- **不选「给 `product_groups` 加 `parent_id`」**：子分类名与上游组名不是一一对应——
  子分类「香港二区」由**商品名**派生，而它的 6 个商品分属上游组「二区 CN2标准性能」；
  子分类「美国一区」更是横跨 3 个上游组（「一区 Kurun精品」「传家宝」「美国一区 CERA高防」）。
  一个上游组也可能需要拆到多个子分类，单表父子列表达不了这种多对多。
- **选「新表 `product_categories` + `products.category_id`」**：分类是**商品级**归属，
  与上游组映射正交；`product_groups` 继续只服务导入幂等与管理端按上游组过滤，互不干扰。

**`product_categories`（本地表，导入只创建不覆盖）**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int | 本地分类 ID |
| `parent_id` | int \| null | **null = 主分类**；非空 = 子分类，指向主分类的 `id`（两级，不再往下嵌套） |
| `key` | string | **派生稳定键**（唯一，见下），导入按它查找，管理员改名不漂移 |
| `name` | string | 展示名（**本地字段**：首次派生写入，之后只能在管理端改） |
| `sort` | int | 排序（升序；主分类按地域固定序，子分类按派生出现序） |
| `created_at` / `updated_at` | string | RFC3339（UTC） |

`products` 新增 `category_id`（int，指向**子分类**的 `id`；`0` = 尚未归类）与
`category_source`（string：`derived` / `manual`，缺省 `derived`）。
`category_source=manual` 表示管理员已手工改挂，**导入不再覆盖**；`derived` 时导入按派生结果刷新。

**派生规则（导入时自动，管理端可后改）**

1. **主分类**：按地域关键词在**商品名**中查找，找不到再在**上游组名**中查找，仍找不到归「其他」。
   关键词（按此优先序匹配，取第一个命中）：`香港` / `日本` / `美国` / `韩国` / `新加坡` / `台湾` /
   `马来` / `德国` / `国内` / `亚太`。
   另按实测数据把大陆机房地域并入 `国内`：`西安` / `襄阳` / `内蒙` / `内蒙古` / `绍兴` / `十堰`
   （这 5 个地域共 40 个商品，关键词表本身不含它们，不并入则会全部落进「其他」）。
2. **子分类**：商品名匹配「地域 + 区段」形态（`^(香港|日本|美国|韩国|新加坡|台湾|马来|德国|国内|亚太)[0-9一二三四五六七八九十]+区`）
   时取该形态原文（`香港二区 CN2 A型` → `香港二区`）；否则取**上游组名原文**（`香港精品网`、`传家宝` 等）。
3. **派生稳定键 `key`**（保证重复导入不漂移、管理员改名后不重建）：
   主分类 `m:<地域>`；子分类命中区段形态时 `z:<区段>`（如 `z:香港二区`），
   未命中时 `g:<upstream_group_id>`（按上游组 id 而非组名，组名被改名也不漂移）。
   `name` 只在**首次创建**时写入，之后导入永不改写。

**导入幂等**：`key` 唯一，find-or-create；派生结果与当前 `category_id` 一致时零写入。
连续两次导入必然 `created=0, updated=0, unchanged=<商品数>`（分类行不新建、不改名、不漂移）。
R7 首次导入会因 `category_id` 从 `0` 变为派生值而计一次 `updated=<商品数>`，属预期（同 10.4 第 5 条），
**再导入一次必回到 `unchanged`**。`category_source=manual` 的商品不参与派生比较与写入。

**「上游分组 → 本地子分类」映射（R12，迁移 0017）**

导入时可只挑部分上游分组（见 10.4 的 `upstream_group_ids`），也可把某个上游分组**固定映射**到一个
本地**子分类**：此后每次导入该分组的商品都自动套用该归类，不再依赖商品名/组名派生。

**`product_group_category_maps`（本地表，与 `product_groups` / `product_categories` 均不建外键）**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int | 本地主键 |
| `upstream_group_id` | int | 上游分组 ID（**唯一键**，对应 `product_groups.upstream_group_id`） |
| `category_id` | int | 目标**子分类** ID（`product_categories.id`，必须 `parent_id` 非空）；主分类不可作为目标 |
| `created_at` / `updated_at` | string | RFC3339（UTC） |

**导入归类优先级（R12，对每个导入的商品依次判定）**

1. `products.category_source=manual`（管理员手工改挂）→ **一律不覆盖**（与 R7 一致，最高优先）。
2. 该商品所属**上游分组**命中映射 → 直接归到映射指向的子分类，写入时 `category_source=derived`
   （取舍：映射是**导入规则的一部分**，改映射后重新导入会刷新这些商品的归属；
   只有管理端 `PUT /admin/products/:id` 的 `category_id` 才写 `manual`）。
3. 无映射（或映射目标已失效，见下）→ 走 R7 的 `category.Derive` 自动派生（行为不变）。

**映射目标失效的降级**：映射指向的分类已被删除、或（被外部改坏）指向了主分类时，导入**不报错**，
仅对该分组回落自动派生，并在导入响应 `map_fallback_group_ids` 与导入日志中体现（不 500、不静默）。

说明：映射只作用于**导入写入路径**（`store.ImportCatalog`），与读时兜底 `EnsureProductCategories`
（只补 `category_id=0` 的商品）无关；无映射的老数据行为完全不变。映射的查看 / 修改 / 删除见 10.4。

### 10.2 定价模型

**计费周期（6 个）与中文显示名**

| 周期名（本地 / 接口） | 上游字段名（`product_pricings`） | 中文显示名 |
| --- | --- | --- |
| `monthly` | `monthly` | 月付 |
| `quarterly` | `quarterly` | 季付 |
| `semiannual` | `semiannually` | 半年付 |
| `annual` | `annually` | 年付 |
| `biennial` | `biennially` | 两年付 |
| `triennial` | `triennially` | 三年付 |

本地周期名沿用「去 -ly」规范（`semiannually` → `semiannual`、`annually` → `annual`、
`biennially` → `biennial`、`triennially` → `triennial`）。接口输出、`pricing_json`、优惠码
适用周期一律使用**本地周期名**；上游字段名只出现在上游原文（`upstream_prices_json.rows`）中。
中文显示名本期只做文案约定（供前端展示），接口不下发。

**上游价格缓存 `upstream_prices_json`**

```json
{
  "code": "CNY",
  "prices": {
    "monthly": "20.00", "quarterly": "60.00", "semiannual": "-1.00", "annual": "200.00",
    "biennial": "-1.00", "triennial": "-1.00"
  },
  "rows": [ { "id": 1, "type": "product", "currency": 1, "code": "CNY", "monthly": "20.00", "…": "上游原文" } ]
}
```

| 字段 | 说明 |
| --- | --- |
| `code` | 选中价格行的货币代码（优先选 `CNY` 行，其次首行「月付价可用」的行，最后回退首行） |
| `prices` | 六个周期的上游原值；键是**周期名**（`monthly` / `quarterly` / `semiannual` / `annual` / `biennial` / `triennial`，注意不是上游字段名 `annually` / `semiannually` / `biennially` / `triennially`）；`biennial` / `triennial` 分别取自上游 `biennially` / `triennially` 字段 |
| `rows` | 上游 `product_pricings` 原文（仅管理端详情接口返回，便于与上游对账） |

约定：**负数表示该周期不售**（上游实测用 `-1.00` 标记未开通的周期，见 10.6 第 2 条）。
上游价不可用（缺省、非法、负数）时该周期视为「无上游价」。
阶段 3a 时期写入的旧 4 键缓存（缺 `biennial` / `triennial`）仍可被正常解析——缺失键视为无上游价；
重新导入一次即会整体刷新为 6 键（解析升级导致首次导入出现 `updated>0` 属预期，见 10.4 导入接口说明）。

**本地定价规则 `pricing_json`**（三种模式，可组合）

```json
{"mode":"upstream"}
{"mode":"markup","markup_percent":10}
{"mode":"fixed","fixed":{"monthly":"25.00","annual":"200.00"}}
{"mode":"markup","markup_percent":10,"fixed":{"monthly":"25.00"}}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `mode` | string | `upstream`（直接用上游价）/ `markup`（加价率）/ `fixed`（固定覆盖价）；缺省按 `upstream` |
| `markup_percent` | number | 加价率（百分比，最多两位小数，取值范围 `-100` ~ `1000`，负数即折扣）。`mode=markup` 时必填；`mode=fixed` 时可选（作用于未被固定价覆盖的周期）；`mode=upstream` 时不允许出现 |
| `fixed` | object | 固定覆盖价：周期名 → 金额字符串。键只能是 6 个本地周期名（`monthly` / `quarterly` / `semiannual` / `annual` / `biennial` / `triennial`）。`mode=fixed` 时至少一项；`mode=upstream` 时不允许出现。旧 4 周期子集（只写前 4 个键）继续合法 |

**单周期计算顺序**：上游价 →（若配置了 `markup_percent`）按上游价加价 →（若该周期有固定价）用固定价覆盖。

1. 加价：`上游价 × (1 + markup_percent/100)`，**四舍五入到分**（half-up，例：`20.05 × 1.10 = 22.055 → 22.06`）。
2. 固定价**不依赖上游价**：上游该周期不售时，固定价照样生效。
3. 上游价不可用且无固定价覆盖 → 该周期**不可售**，接口输出 `null`（不回退成 `0.00`）；
   未被 `fixed` 覆盖的周期照常回退上游价（含 `biennial` / `triennial`）。
4. 金额一律用定点小数字符串（如 `"22.00"`），全链路整数分计算，避免浮点误差；金额格式为非负十进制、最多两位小数、上限 `999999999999.99`。
5. 规则校验是**严格模式**：出现未知字段（如把 `markup_percent` 拼成 `markup_percentt`）直接报错，避免「少写一个字母 → 静默不加价」造成资金损失。

**校验错误码划分**：格式类（非法 JSON、未知字段、未知周期、金额写法非法、`mode` 取值非法、
加价率小数超两位）→ `40001`；规则类（`mode=markup` 缺 `markup_percent`、`mode=fixed` 的 `fixed` 为空、
`mode=upstream` 携带 `markup_percent`/`fixed`、加价率越界）→ `40002`。

#### 10.2.1 配置差价（R7）

**成交价 = 基础价 + Σ 配置差价**。基础价是上面 10.2 的单周期计算结果（上游价 → 加价 → 固定价覆盖）；
配置差价是用户所选每个配置项在该周期的加价，**跟随商品定价模式**：

| 定价模式 | 基础价 | 配置差价 |
| --- | --- | --- |
| `upstream` | 上游价 | 差价原样 |
| `markup` | 上游价 ×(1+markup_percent%) | **差价 ×(1+markup_percent%)**（同一加价率，四舍五入到分） |
| `fixed` | 固定价覆盖基础价 | **差价原样叠加在固定价之上**（固定价只覆盖基础价，不吞差价） |

组合模式（`markup` + `fixed`）：基础价按 `fixed` 覆盖，差价按 `markup` 加价后叠加。

**差价数据来源**：上游 `GET /cart/get_product_config` 的 `data.config_groups[].options[].sub[].pricings[]`
（`type:"configoptions"`，`relid` = 子项 id），已在导入时随 `products.config_json` 原文落库，
**无需新增列**。单行含 6 个周期字段，但键名是**上游副词形态**（`semiannually`/`annually`/`biennially`/`triennially`），
按 10.2 的周期映射表转成本地周期名；`code` 取 CNY 行（无则取首行）。

**数量型（拉条型）的差价口径**：一个数量型配置项下有**多个子项 = 价格档位**，各档位 `qty_minimum~qty_maximum`
互不重叠且**上下衔接**（实测 153 个单档位 + 133 个多档位，多档位 100% 衔接无空洞）。
选中数量落入哪一档，就用**该档位的单位差价**；**可计费数量 = 数量 − 低一档的 `qty_maximum`**（无低一档时为 0）：

```
数量型差价 = 档位单位差价 × 可计费数量
```

实测依据（2026-10-13 全量核对 159 个商品的 `config_json`）：

- 单档位（如商品 1「香港二区 CN2 A型」`data_disk_size`）：`[0-256 @0.50]`，可计费数量 = 数量，
  退化为**单位差价 × 数量**（20GB → `10.00`，50GB → `25.00`）。
- 多档位（如商品 2「香港二区 CN2 B型」`data_disk_size`）：`[30-30 @0.00] [31-256 @0.50]`，
  30GB 落在免费档差价 `0.00`，31GB 起每 GB `0.50`（31GB → `0.50`，256GB → `113.00`）。
- 多档位（如商品 74 `ip_num`）：`[1-1 @0.00] [2-10 @38.00]`，首个 IP 免费，第 2 个起每个 `38.00`
  （2 个 → `38.00`，10 个 → `342.00`）。若按「单位差价 × 数量」会把免费档也计费，故以档位为准。

**提交口径不变**：`config` / `configoption` 仍提交**数量**（`qty`），不提交子项 id（见 12.4）；
档位只影响计价，不影响提交。数量的合法区间取该配置项**全部可见档位的并集**
（`min(档位.qty_minimum)` ~ `max(档位.qty_maximum)`），不得只取首个档位。

**档位区间怎么拿到（v21 / R11）**：服务端**不改写**各档位的 `qty_minimum`/`qty_maximum` 语义来源——
会员端视图里 `values[].qty_minimum`/`qty_maximum` 仍是**并集**（只用于数量控件取值范围），
每档的**真实区间**由 `options[].tiers[]` 下发（`qty_min`/`qty_max` + 该档 `pricings`，见 10.3 第 6 条）。
前端挑档位、算 `billable` 必须用 `tiers`，**不得**用并集字段反推档位（并集后每档区间都相同，
按区间挑会永远命中第一档——正是 v21 修复的联动 bug）。

**无差价数据的降级**：子项无 `pricings`、无 CNY/首行、或该周期字段缺失/非法/为负
（上游以 `-1.00` 表示不适用）时，该配置项差价按 **`0.00`** 计，明细里 `note:"无差价数据"`，
**不阻断下单**。

**金额口径**：差价与成交价一律整数分计算，接口输出两位小数字符串。**优惠码对成交总额抵扣**
（即对 `基础价 + Σ配置差价` 抵扣），既有 12.5 口径不变。

**明细快照**：订单落库时把差价明细写入 `orders.price_breakdown_json`（迁移 0013），对账可查：
`{"base":"20.00","deltas":[{"option_id":10,"option_name":"data_disk_size|数据盘","kind":"quantity",
"qty":50,"unit":"0.50","billable":50,"amount":"25.00","note":""}],"total":"45.00"}`。
`kind` 取 `select`（选项型，`value_id` 有值）/ `quantity`（数量型，`qty` 有值）。

### 10.3 会员端接口（只读）

商品目录**无需鉴权**：商品与价格对访客可见（阶段 4 的下单/支付接口才要求会员 token）。
会员端只暴露**已上架**（`status=on`）商品，且**不返回任何上游 ID**（`upstream_pid` / `upstream_group_id` 都不出现）。

#### `GET /api/v1/products`

按**子分类**组织的上架商品目录（不含空分类；分类与商品均按 `sort` 升序）。
R7 起 `groups[]` 的语义从「上游产品组」演进为「**子分类**」，字段骨架 `id` / `name` / `sort` / `products` **保持不变**，
只新增 `parent`（所属主分类）——旧客户端读 `groups[].products` 仍可用；新增 `categories[]` 供两级筛选。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 不需要 |
| 查询参数 | 无 |
| 成功 | HTTP 200，`data` 为 `{categories, groups, total}` |

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "categories": [
      {
        "id": 101,
        "name": "香港",
        "sort": 0,
        "subcategories": [
          { "id": 201, "name": "香港二区", "sort": 0, "product_count": 6 },
          { "id": 202, "name": "香港三区", "sort": 1, "product_count": 6 }
        ]
      }
    ],
    "groups": [
      {
        "id": 201,
        "name": "香港二区",
        "sort": 0,
        "parent": { "id": 101, "name": "香港" },
        "products": [
          {
            "id": 1,
            "name": "香港二区 CN2 A型",
            "type": "dcimcloud",
            "sort": 0,
            "prices": {
              "monthly": "22.00", "quarterly": "66.00", "semiannual": "132.00", "annual": "220.00",
              "biennial": null, "triennial": null
            },
            "stock_qty": 70,
            "stock_control": 1,
            "ontrial_max": 0,
            "description_lines": ["CPU:2核心", "内存:1G", "带宽:20M", "流量:不限"]
          }
        ]
      }
    ],
    "total": 1
  }
}
```

| 字段 | 说明 |
| --- | --- |
| `categories` | 主分类树（R7）：`subcategories` 只含**有上架商品**的子分类；`product_count` 是该子分类下上架商品数 |
| `groups[].parent` | 所属主分类 `{id, name}`（R7 新增） |
| `prices` | 六周期本地**基础价**（不含配置差价，见 10.2.1）；六个键**始终存在**，`null` 表示该周期不可售 |
| `stock_qty` / `stock_control` | `stock_control=1` 时 `stock_qty` 才是有效库存；`0` 表示上游不限库存 |
| `ontrial_max` | 可试用数量（`0` 表示不提供试用） |
| `description_lines` | 商品简介解析出的**展示行数组**（R5，商品卡配置列表的数据源）：从 `description` 提取 `<li>` 行文本、去 HTML 标签与空行，无 `<li>` 时按 `<br>`/段落/换行切行；**空简介恒为空数组 `[]`**（不是 `null`，也不是省略键）；列表与详情都下发同一份数组，列表**不下发** `description` 原文 |

#### `GET /api/v1/products/:id`

商品详情：配置项 + 六周期价格 + 库存/试用信息。`:id` 是**本地商品 ID**。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 不需要 |
| 路径参数 | `id`：本地商品 ID（正整数） |
| 成功 | HTTP 200，`data` 为商品详情 |
| 错误码 | `40001`（ID 非正整数）、`404`（商品不存在**或已下架**——下架商品对会员端等同于不存在） |

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "id": 1,
    "name": "香港二区 CN2 A型",
    "type": "dcimcloud",
    "sort": 0,
    "prices": {
      "monthly": "22.00", "quarterly": "66.00", "semiannual": "132.00", "annual": "220.00",
      "biennial": null, "triennial": null
    },
    "stock_qty": 70,
    "stock_control": 1,
    "ontrial_max": 0,
    "description_lines": ["CPU:2核心", "内存:1G"],
    "description": "<li>CPU:2核心</li>\n<li>内存:1G</li>",
    "group": { "id": 201, "name": "香港二区", "parent": { "id": 101, "name": "香港" } },
    "config_groups": [
      {
        "id": 1,
        "name": "区域",
        "options": [
          {
            "id": 1, "name": "area|区域", "type": 12, "upstream_id": 0,
            "values": [
              { "id": 1, "name": "1|HK^香港", "upstream_id": 0, "qty_minimum": 0, "qty_maximum": 0,
                "pricings": { "monthly": "0.00", "quarterly": "0.00", "semiannual": "0.00", "annual": "0.00", "biennial": null, "triennial": null } }
            ]
          }
        ]
      },
      {
        "id": 2,
        "name": "系统",
        "options": [
          {
            "id": 2, "name": "os|操作系统", "type": 5, "upstream_id": 0,
            "values": [
              { "id": 3, "name": "15|Debian^Debian-10.3.3-x64", "upstream_id": 0, "qty_minimum": 0, "qty_maximum": 0,
                "pricings": { "monthly": "0.00", "quarterly": "0.00", "semiannual": "0.00", "annual": "0.00", "biennial": null, "triennial": null } },
              { "id": 9, "name": "66|CentOS^CentOS-7.9.2111-x64", "upstream_id": 0, "qty_minimum": 0, "qty_maximum": 0,
                "pricings": { "monthly": "0.00", "quarterly": "0.00", "semiannual": "0.00", "annual": "0.00", "biennial": null, "triennial": null } }
            ]
          },
          {
            "id": 7, "name": "bw|带宽", "type": 11, "upstream_id": 0,
            "values": [ { "id": 34, "name": "带宽", "upstream_id": 0, "qty_minimum": 20, "qty_maximum": 100,
              "pricings": { "monthly": "0.50", "quarterly": "1.50", "semiannual": "3.00", "annual": "6.00", "biennial": "0.00", "triennial": "0.00" } } ],
            "tiers": [ { "value_id": 34, "qty_min": 20, "qty_max": 100,
              "pricings": { "monthly": "0.50", "quarterly": "1.50", "semiannual": "3.00", "annual": "6.00", "biennial": "0.00", "triennial": "0.00" } } ]
          }
        ]
      }
    ],
    "custom_fields": [],
    "updated_at": "2026-10-08T09:12:03Z"
  }
}
```

约定：

1. `config_groups[].options[].id` / `values[].id` 是下单 `config`（进而交付 `configoption`）的
   键与值——**阶段 5a 真机实测口径**（见 14.5 第 1 条：上游直连下单按本地 id 取配置）；
   `upstream_id` 为上游「代理模式」映射字段（本项目真实数据恒为 0），会员端**保留下发**
   仅作透传展示（商品级的上游 ID 仍不下发）。
2. 会员端**过滤掉上游标记为隐藏**（`hidden != 0`）的可配置项与可选值；管理端不做过滤。
3. `name` 是上游文案原文（形如 `area|区域`、`1|HK^香港`），阶段 3a 不做文案清洗。
4. `description` 是**解码后的原始 HTML**（R5 起导入落库时做一次实体反转义，如 `<li>CPU:2核心</li>` 与换行），
   仅**详情**下发、列表不下发；展示配置列表请直接用 `description_lines`（服务端已去标签、去空行），
   前端无需再解析 HTML，也不应把 `description` 当 HTML 注入渲染。
5. `type` 是上游 `option_type`（选择方式编码），前端按它渲染与上游前台一致的控件
   （**R6 实测映射**，2026-10-09 对上游 159 个商品全量拉取 + 前台配置页
   `GET /cart?action=configureproduct&pid=N` 渲染对照）：

   | `option_type` | 上游形态 | 实测键名样例 | 本项目控件 |
   | --- | --- | --- | --- |
   | 1 | 下拉 | `network_type` / 快照数量 / 备份数量 | 下拉 |
   | 4 | 滑条 + 数字输入 | `ip_num` | 数量 |
   | 5 | 两级（大类 → 版本） | `os` | 二级系统选择 |
   | 6 | 单选横条 | `cpu` / 网络类型 / 系统盘 | 单选横条 |
   | 8 | 单选横条 | `memory` | 单选横条 |
   | 10 | 单选横条 | 带宽（档位）/ 流入带宽 | 单选横条 |
   | 11 | 滑条 + 数字输入 | `bw` | 数量 |
   | 12 | 单选横条（带图标） | `area` / 数据中心 | 单选横条 |
   | 13 | 单选横条 | `system_disk_size` / `data_disk_size` | 单选横条 |
   | 14 | 滑条 + 数字输入 | `data_disk_size` | 数量 |
   | 15 | 滑条 + 数字输入 | `ip_num` / 流量 | 数量 |
   | 19 | 滑条 + 数字输入 | 系统盘 / 数据盘 | 数量 |

   未收录的 `option_type` 前端按值形态兜底：值含 `^` 且存在多个大类 → 二级；
   单值且值名不是 `id|名` 形态 → 数量型；选项数 > 8 → 下拉；其余 → 单选横条。
   （实测 159 个商品未出现多选/复选型配置项；上游若新增编码，按上述兜底渲染。）
6. **数量型（上表「数量」行）的取值口径**：提交的是**数量**而不是值 id
   （契约 8.3 的 `configoption` 行：数量型传 `qty`）。**取值范围取该配置项全部可见值（档位）的并集**：
   `min(值.qty_minimum)` ~ `max(值.qty_maximum)`，默认值取该下界，步进 1；选项型的这两个字段恒为 0。
   （R7 修正：此前只取**首个**值的范围，遇到多档位数量型——如商品 2 数据盘 `[30-30]`/`[31-256]`——
   会被锁死在 30 无法加盘；档位结构见 10.2.1。）下单校验见 12.4。
   **v21 / R11 起数量型另下发 `options[].tiers[]`**（仅数量型有此项，选项型不出现该键）：

   ```json
   "tiers": [
     { "value_id": 88,    "qty_min": 30, "qty_max": 30,  "pricings": { "monthly": "0.00", "...": "..." } },
     { "value_id": 10664, "qty_min": 31, "qty_max": 256, "pricings": { "monthly": "0.50", "...": "..." } }
   ]
   ```

   | 字段 | 说明 |
   | --- | --- |
   | `value_id` | 对应 `values[].id`（同一子项 = 同一价格档位），便于回查值名 |
   | `qty_min` / `qty_max` | **该档真实区间**（上游 `sub.qty_minimum`/`qty_maximum` 原值，**不是**并集） |
   | `pricings` | 该档六周期**单位差价**，形状与口径同 `values[].pricings`（第 8 条） |

   `tiers[]` 按 `qty_min` 升序，与 `values[]` 同源同过滤（会员端同样剔除 `hidden != 0`）。
   **语义分工（向后兼容）**：`values[].qty_minimum`/`qty_maximum` **继续是并集**（数量控件的
   min/max 与默认值，旧客户端行为不变）；`tiers[]` **才是挑档位的唯一依据**——数量落在哪一档
   就用哪一档的 `pricings`，`billable = qty − 低一档的 qty_max`（见 10.2.1）。
   用并集字段反推档位会永远命中第一档（每档区间被并集拉平），v21 之前的价格联动 bug 即源于此。
7. 值名里的 `^` 前是「大类」（如 `15|Debian^Debian-10.3.3-x64` 的 `Debian`、
   `1|HK^香港` 的 `HK`），二级选择以它为大类分组依据（按值出现顺序去重）；
   `^` 后是具体显示名。
8. **`pricings`（R7）**：该值在六个周期的**配置差价**，已按 10.2.1 的定价模式换算
   （`markup` 已乘加价率，`fixed` 下仍为原差价），前端直接累加即可，**不必也不应**在前端再套定价规则。
   六个键**始终存在**；`null` 表示该周期**无差价数据**（按 `0.00` 计，明细标注「无差价数据」，见 10.2.1）。
   数量型的 `pricings` 是**单位差价**（成交时乘可计费数量，见 10.2.1）。
   `base` 类价格字段不在值上——基础价仍看商品级 `prices`。
   （v21 / R11：数量型的 `tiers[].pricings` 与本条同形同口径，是**按档位**取单价的权威来源；
   `values[].pricings` 对数量型而言与 `tiers` 同源，保留是为了旧客户端不破。）
9. **成交价实时重算**：前端在配置/周期变化时按 `基础价(cycle) + Σ 配置差价(cycle)` 重算
   （数量型乘可计费数量），并展示可展开明细；**服务端下单时仍以同一公式为准**，
   前端展示价只是预览（10.3 结尾既有声明「最终以提交订单时的金额为准」继续有效）。

### 10.4 管理端接口

管理端接口全部挂在管理员 token 下（`aud=admin`）。**导入 / 改定价 / 上下架 / 改分组仅 `admin`、`finance` 可调用，`support` 返回 `403`**；查看类接口（列表、详情）所有角色可调用。

#### `POST /api/v1/admin/products/import`

从上游拉取商品目录与逐个商品详情，批量 upsert 到本地（幂等）。**上游侧全程只读**。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，角色 `admin` / `finance` |
| 请求体 | **可选**（R12）：`{ "upstream_group_ids": [1, 3, 5] }`；省略请求体 / `{}` / `[]` / `null` = **全量导入** |
| 上游调用 | `GET /cart/all` 一次 + `GET /cart/get_product_config` 每个（选中分组的）商品一次（4 并发） |
| 超时 | 单次上游请求取 `upstream.timeout_seconds`；整次导入上限 5 分钟 |
| 成功 | HTTP 200，`data` 为 `{created, updated, unchanged, groups, failed, failed_pids?, selected_groups?, map_fallback_group_ids?}` |

```bash
# 全量导入（不带请求体，与 R11 前完全一致）
curl -s -X POST http://127.0.0.1:8080/api/v1/admin/products/import \
  -H 'Authorization: Bearer <admin-token>'

# 只导入选中的上游分组（R12）
curl -s -X POST http://127.0.0.1:8080/api/v1/admin/products/import \
  -H 'Authorization: Bearer <admin-token>' -H 'Content-Type: application/json' \
  -d '{"upstream_group_ids":[1,3,5]}'
```

```json
{ "code": 0, "message": "ok", "data": { "created": 159, "updated": 0, "unchanged": 0, "groups": 29, "failed": 0 } }
```

| 字段 | 说明 |
| --- | --- |
| `created` | 新建商品数（新商品默认 `status=off`、`pricing_json={"mode":"upstream"}`，须人工定价后上架） |
| `updated` | 上游字段发生变化的商品数 |
| `unchanged` | 与库内完全一致的商品数 |
| `groups` | 本次处理的分组数（分组只创建不覆盖；**只计选中的分组**） |
| `failed` / `failed_pids` | 详情抓取失败而被跳过的商品数与 ID 列表（`failed_pids` 最多 20 个，`failed=0` 时不出现） |
| `selected_groups` | R12：本次选中的上游分组 ID（升序去重）；**全量导入时不出现** |
| `map_fallback_group_ids` | R12：因映射目标失效而回落自动派生的上游分组 ID（见 10.1）；无失效映射时不出现 |

行为约定：

1. 同一批数据重复导入：`created=0, updated=0, unchanged=<商品数>, groups=<分组数>`（幂等）。
2. 单个商品的详情抓取失败不中断整次导入，计入 `failed` 并跳过该商品；**目录里有商品但全部失败**时返回 `500`。
3. 首次导入按上游目录顺序写入 `sort`（分组的 `sort` = 其在上游目录中的位置；商品的 `sort` = 本次导入的顺序编号）。
   选中子集时同样稳定（重复导入不会改 `sort`：它是本地字段，只创建不覆盖）。
4. 对上游已删除的商品不做处理（本地保留，由管理端下架）。
5. **周期解析升级后的首次导入会出现 `updated>0`，属预期**：阶段 3b 把 `upstream_prices_json`
   从 4 键扩为 6 键（新增 `biennial` / `triennial`），而导入的「上游变化检测」按字段整串比较，
   因此升级后首次导入会刷新所有商品的该字段（`updated=<商品数>`）；**再导入一次必回到
   `unchanged=<商品数>`**（幂等保持）。
6. **按分组筛选（R12）**：传了非空 `upstream_group_ids` 时只抓取这些上游分组的商品，`product_groups`
   的 upsert 也只处理选中的分组（与全量导入同一套幂等口径）。全量导入（缺省/空/`null`）行为与 R11 前
   完全一致，既有调用方不受影响。
7. **归类优先级（R12）**：manual 商品不覆盖 > 上游分组映射 > 自动派生，详见 10.1。

错误码：

| code | HTTP | 场景与 message |
| --- | --- | --- |
| `40001` | 400 | 请求体不是合法 JSON、`upstream_group_ids` 不是数组、或含 `0` / 负数 |
| `40002` | 400 | `upstream_group_ids` 里有不在本次上游目录中的分组（`上游分组不存在：…`；不静默导入 0 个商品还报成功） |
| `403` | 403 | 角色为 `support` |
| `500` | 500 | 上游未配置（`上游未配置，无法导入商品`）、上游目录拉取失败（`上游商品目录拉取失败：…`）、全部商品详情抓取失败 |
| `50001` | 500 | 写入本地库失败 |

#### `GET /api/v1/admin/products`

分页查询商品（含下架商品）。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理端 token（所有角色） |
| 查询参数 | `page`（默认 1）、`page_size`（默认 20，1-100）、`group_id`（**本地分组 ID**）、`status`（`on` / `off`）、`keyword`（商品名模糊匹配）、`category_id`（R9，**本地子分类 ID**；`0` = 只看未归类） |
| 成功 | HTTP 200，`data` 为 `{items, page, page_size, total}`，按 `sort` 升序、`id` 升序 |

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "items": [
      {
        "id": 1,
        "upstream_pid": 1,
        "upstream_group_id": 1,
        "group_id": 1,
        "group_name": "香港二区",
        "category_id": 201,
        "category_name": "香港二区",
        "category_parent": { "id": 101, "name": "香港" },
        "name": "香港二区 CN2 A型",
        "type": "dcimcloud",
        "module": "idcsmart_common",
        "status": "on",
        "sort": 0,
        "stock_qty": 70,
        "stock_control": 1,
        "ontrial_max": 0,
        "pricing": { "mode": "markup", "markup_percent": 10 },
        "prices": {
          "monthly": "22.00", "quarterly": "66.00", "semiannual": "132.00", "annual": "220.00",
          "biennial": null, "triennial": null
        },
        "created_at": "2026-10-08T09:00:00Z",
        "updated_at": "2026-10-08T09:12:03Z"
      }
    ],
    "page": 1,
    "page_size": 20,
    "total": 159
  }
}
```

**分类字段（R9）**——列表项与详情（`GET /admin/products/:id`，详情 = 列表项字段 + 描述/配置项）都带这三个字段，取值口径：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `category_id` | int | 所属**子分类** ID；`0` = 未归类 |
| `category_name` | string | 所属子分类名；未归类为空串 |
| `category_parent` | object | 所属**主分类**引用 `{id, name}`（形状同会员端 `group.parent`）；未归类为 `{id:0,name:""}` |

三个键**始终存在**（未归类输出 0 / 空串 / 零值对象，不省略键，便于前端稳定渲染）。
与 `group_id` / `group_name`（上游产品组）**并列下发、互不替代**：分组是上游目录结构，分类是本地两级目录。
未归类时**不回退**成组名（会员端 `GET /products/:id` 的 `group` 有组名兜底，管理端这三个字段是诚实口径，供「分类」列显示占位）。

错误码：

| code | HTTP | 场景与 message |
| --- | --- | --- |
| `40001` | 400 | 分页参数越界/非数字、`status` 取值非法、`group_id` 非正整数、`category_id` 非整数或为负数 |
| `404` | 404 | `group_id` 指向不存在的分组（`分组不存在`）、`category_id` 指向不存在的子分类（`分类不存在`） |
| `401` / `403` | 401/403 | token 无效 / 管理员被禁用 |

#### `GET /api/v1/admin/products/:id`

商品详情 = 列表项字段 + `description` + `description_lines` + `config_groups` + `custom_fields` + `upstream_prices`（上游价格缓存原文，含 `rows`）。`:id` 为本地商品 ID。管理端**列表**（`GET /admin/products`）不带 `description` / `description_lines`（避免列表响应膨胀），仅在详情下发。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token（所有角色） |
| 成功 | HTTP 200，`data` 为商品详情（字段见上；`config_groups` 不做 hidden 过滤） |
| 错误码 | `40001`（ID 非正整数）、`404`（`商品不存在`） |

#### `PUT /api/v1/admin/products/:id`

修改本地字段：定价规则、上下架、排序、改挂子分类。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，角色 `admin` / `finance` |
| 请求体 | `pricing_json`（定价规则，见 10.2）、`status`（`on` / `off`）、`sort`（`-999999` ~ `999999`）、`category_id`（改挂到**子分类**，`0` = 取消归类，见下）；四个字段均可选但**至少提供一个** |
| 成功 | HTTP 200，`data` 为更新后的商品详情（与 `GET /admin/products/:id` 同构） |

```bash
curl -s -X PUT http://127.0.0.1:8080/api/v1/admin/products/1 \
  -H 'Authorization: Bearer <admin-token>' -H 'Content-Type: application/json' \
  -d '{"pricing_json":{"mode":"markup","markup_percent":10},"status":"on","sort":3}'
```

注意：`pricing_json` 既接受**对象**（`{"mode":"markup","markup_percent":10}`）也接受**字符串**
（`"{\"mode\":\"markup\",\"markup_percent\":10}"`），传 `null` 表示重置为缺省规则 `{"mode":"upstream"}`。
`status` / `sort` / `category_id` 只在提供时才修改；未提供的字段保持原值。

错误码：

| code | HTTP | 场景与 message |
| --- | --- | --- |
| `40001` | 400 | 未提供任何字段、定价规则格式非法（含未知字段/未知周期/金额写法非法）、`status` 取值非法、`sort` 越界、ID 非正整数 |
| `40002` | 400 | 定价规则不成立（缺 `markup_percent`、`fixed` 为空、`mode=upstream` 携带加价率/固定价等） |
| `403` | 403 | 角色为 `support` |
| `404` | 404 | `商品不存在` |
| `409` | 409 | **更新后为上架状态，但六个周期都没有可用价格**（`商品没有任何可用周期的价格，无法上架（请先配置固定价或确认上游价格可用）`）：避免上架一个买不到的商品 |
| `50001` | 500 | 写入本地库失败 |

#### `GET /api/v1/admin/product-groups`

列出全部分组（含空分组）与分组下的商品计数。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token（所有角色） |
| 成功 | HTTP 200，`data` 为 `{items}`，按 `sort` 升序 |

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "items": [
      {
        "id": 1,
        "upstream_group_id": 1,
        "name": "香港二区",
        "sort": 0,
        "products": { "total": 12, "on": 3, "off": 9 },
        "created_at": "2026-10-08T09:00:00Z",
        "updated_at": "2026-10-08T09:12:03Z"
      }
    ]
  }
}
```

#### `PUT /api/v1/admin/product-groups/:id`

分组重命名 / 改排序。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，角色 `admin` / `finance` |
| 请求体 | `name`（1-128 字符，自动去首尾空格）、`sort`（`-999999` ~ `999999`）；至少提供一个 |
| 成功 | HTTP 200，`data` 为更新后的分组（同列表项结构，`products` 计数为 0 值） |
| 错误码 | `40001`（无字段、空名称、名称超长、`sort` 越界、ID 非正整数）、`403`（support）、`404`（`分组不存在`） |

说明：分组名与排序是**本地字段**，导入不会覆盖（见 10.1），因此可以放心按自己的品牌重命名。

#### `GET /api/v1/admin/product-group-maps`（R12）

列出全部「上游分组 → 本地子分类」映射（数量级 = 上游分组数，几十条，**不分页**）。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token（**所有角色**，只读，与分类树同口径） |
| 成功 | HTTP 200，`data` 为 `{items}`，按 `upstream_group_id` 升序 |

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "items": [
      {
        "upstream_group_id": 9,
        "upstream_group_name": "香港二区",
        "category_id": 201,
        "category_name": "香港二区",
        "category_parent_name": "香港"
      }
    ]
  }
}
```

| 字段 | 说明 |
| --- | --- |
| `upstream_group_id` / `upstream_group_name` | 上游分组 ID 与其本地组名（组名缺失时为空串） |
| `category_id` | 目标子分类 ID（存储值） |
| `category_name` / `category_parent_name` | 目标子分类名与其主分类名；**目标分类已被删除时为两个空串**（页面据此提示「目标分类已失效」） |

只返回**已建立映射**的分组；未映射的分组由前端与 `GET /admin/product-groups` 对照展示为「未映射（按规则自动归类）」。

#### `PUT /api/v1/admin/product-group-maps/:upstream_group_id`（R12）

新增或更新某上游分组的映射（upsert，同一分组只有一条）。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，**仅 `admin`**（沿用 `categories.write`，与分类写一致） |
| 路径参数 | `upstream_group_id`：**上游分组 ID**（不是本地分组 ID） |
| 请求体 | `{ "category_id": 31 }`（必填，正整数） |
| 成功 | HTTP 200，`data` 为更新后的映射（结构同上，`upstream_group_name` / 分类名已解析） |

```bash
curl -s -X PUT http://127.0.0.1:8080/api/v1/admin/product-group-maps/9 \
  -H 'Authorization: Bearer <admin-token>' -H 'Content-Type: application/json' \
  -d '{"category_id": 201}'
```

错误码：

| code | HTTP | 场景与 message |
| --- | --- | --- |
| `40001` | 400 | `upstream_group_id` 非正整数、`category_id` 缺失或非正整数、请求体不是合法 JSON |
| `40002` | 400 | `category_id` 指向的分类不存在（`分类不存在`）、指向的是**主分类**（`只能映射到子分类`）、`upstream_group_id` 对应的上游分组尚未导入本地（`上游分组不存在`） |
| `403` | 403 | 角色为 `finance` / `support` |
| `50001` | 500 | 写入本地库失败 |

#### `DELETE /api/v1/admin/product-group-maps/:upstream_group_id`（R12）

删除某上游分组的映射（不存在的映射返回 `404`）。删除后该分组回到自动派生。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，**仅 `admin`** |
| 路径参数 | `upstream_group_id`：上游分组 ID |
| 成功 | HTTP 200，`data` 为 `null` |
| 错误码 | `40001`（ID 非正整数）、`403`（finance / support）、`404`（`该分组没有映射`）、`50001` |

说明：映射只影响**导入**时的归类（见 10.1），不会就地改写任何商品当前的 `category_id`；
要让映射生效需重新导入该分组（或全量导入）。删除映射同样不就地回退，
需重新导入才会回落自动派生——这是为了不与 `category_source=manual` 的手工归属混淆。

#### `GET /api/v1/admin/categories`

列出全部分类树（主分类 → 子分类）与每个子分类下的商品计数。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token（**所有角色**，只读） |
| 成功 | HTTP 200，`data` 为 `{items}`，主分类按 `sort` 升序、子分类按 `sort` 升序 |

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "items": [
      {
        "id": 101,
        "key": "m:香港",
        "name": "香港",
        "sort": 0,
        "created_at": "2026-10-13T00:00:00Z",
        "updated_at": "2026-10-13T00:00:00Z",
        "subcategories": [
          {
            "id": 201,
            "key": "z:香港二区",
            "name": "香港二区",
            "sort": 0,
            "products": { "total": 6, "on": 3, "off": 3 },
            "created_at": "2026-10-13T00:00:00Z",
            "updated_at": "2026-10-13T00:00:00Z"
          }
        ]
      }
    ]
  }
}
```

`key` 是派生稳定键（见 10.1），仅供排查与导入对照，管理端**不可修改**。

#### `PUT /api/v1/admin/categories/:id`

分类改名（主/子通用）与子分类改挂主分类。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，**仅 `admin`**（分类管理属目录结构，财务/客服只读） |
| 请求体 | `name`（1-128 字符，自动去首尾空格）、`parent_id`（**仅子分类**可传：改挂到另一个主分类；主分类传 `parent_id` 一律 `40002`）、`sort`（`-999999` ~ `999999`）；至少提供一个 |
| 成功 | HTTP 200，`data` 为更新后的分类（主分类带 `subcategories`，子分类带 `products` 计数） |
| 错误码 | `40001`（无字段、空名称、名称超长、`sort` 越界、ID 非正整数）、`40002`（主分类传 `parent_id`、`parent_id` 指向自身/子分类/不存在、子分类改成主分类）、`403`（`finance` / `support`）、`404`（`分类不存在`） |

**改挂子分类**（把子分类移到别的主分类）走本接口的 `parent_id`；
**商品改挂子分类**走 `PUT /api/v1/admin/products/:id` 的 `category_id`（见下）。

说明：分类的 `name` 是**本地字段**，导入不会覆盖（见 10.1 的 `key` 稳定键），改名后不漂移、不重建。

#### `PUT /api/v1/admin/products/:id` 的 `category_id`（R7 扩展，R9 管理界面）

请求体新增可选 `category_id`：把商品改挂到某个**子分类**（传 `0` 表示取消归类）。
设置后该商品的 `category_source` 置为 `manual`，**后续导入不再自动派生覆盖**（见 10.1）。
非法/不存在的子分类 id → `40002`；传主分类 id（`parent_id` 为空的行）→ `40002`（必须挂到子分类）。
成功响应即更新后的商品详情，`category_id` / `category_name` / `category_parent` 三个字段同步反映新归属。

#### 管理端页面（R9：分类管理 + 商品改挂分类；R9-B 并为一页）

界面只消费上面已有的端点，**不新增接口**：

1. **`/admin/products` 商品与分类页**（侧栏唯一入口「商品与分类」；R9-B 把原「分类」页并入）
   页内两个页签，由查询参数驱动：默认「商品」，`?tab=categories` 打开「分类」（分享 / 刷新保持在同一页签）。

   - **商品页签** = 商品列表 + 上游导入 + 分组面板 + 「分类」列与「改分类」操作：
     - 列展示 `category_name`（未归类显示占位，依据 `category_id == 0`）；
     - 弹窗选目标**子分类**（数据同 `GET /admin/categories`，含「未归类」= `0`），
       提交 `PUT /admin/products/:id` 的 `category_id`；成功后提示「已改挂，后续导入不再覆盖该商品分类」；
     - 写权限沿用商品写（`admin` / `finance`），与分类页签的 `categories.write`（仅 `admin`）**不是同一个权限点**；
     - 列表另支持按分类筛选（`GET /admin/products` 的 `category_id` 查询参数，`0` = 只看未归类）。
     - **上游导入（R12）**为弹窗：列出上游分组（数据来源 `GET /admin/product-groups`，多选、默认全选），
       结果区沿用 `created / updated / unchanged / groups / failed` 展示；
       **全选（或一个分组都没有）时省略 `upstream_group_ids`**（保持「缺省=全量」语义），
       部分选择时提交选中的上游分组 ID；一个都不选时禁用确认并提示原因。
   - **分类页签**（原 `/admin/categories` 分类管理页）：
     - 数据来源 `GET /admin/categories`：主分类 → 子分类两级树，展示 `name` / `sort` 与子分类的
       `products{total,on,off}` 商品计数；
     - 改名（主/子通用）→ `PUT /admin/categories/:id` 的 `name`；改排序 → `sort`；
     - 子分类改挂主分类 → `parent_id`（下拉目标为主分类，不能选自身；主分类不提供此项）；
     - 写操作**仅 `admin`**（`finance` / `support` 进来是只读视图：写入口禁用并给出原因，且不发无权限请求）。
     - **上游分组映射（R12）**区块：表格展示每个上游分组 → 目标子分类（**下拉只列已有子分类**，
       两级展示如「香港 / 香港二区」）；支持新增 / 修改（同一个下拉 + 保存）与删除；
       未映射的分组显示「未映射（按规则自动归类）」；数据来源 `GET /admin/product-group-maps`
       与 `GET /admin/product-groups` 对照。写操作沿用 `categories.write`（**仅 `admin`**），
       与分类写同一闸门。**不做**弹窗内联新建分类（要新建分类先去分类分区建）。
2. **旧地址 `/admin/categories`**：重定向到 `/admin/products?tab=categories`（收藏 / 外链 / 文档里的旧地址不断，
   不再有独立分类页）。

### 10.5 角色权限矩阵（商品部分）

| 接口 | admin | finance | support |
| --- | --- | --- | --- |
| `GET /api/v1/products`、`GET /api/v1/products/:id` | 公开（无需 token） | 公开 | 公开 |
| `GET /api/v1/admin/products`、`GET /api/v1/admin/products/:id` | ✓ | ✓ | ✓ |
| `GET /api/v1/admin/product-groups` | ✓ | ✓ | ✓ |
| `GET /api/v1/admin/categories`（R7，只读） | ✓ | ✓ | ✓ |
| `GET /api/v1/admin/product-group-maps`（R12，只读） | ✓ | ✓ | ✓ |
| `PUT /api/v1/admin/product-group-maps/:upstream_group_id`（R12，仅 admin） | ✓ | ✗（`403`） | ✗（`403`） |
| `DELETE /api/v1/admin/product-group-maps/:upstream_group_id`（R12，仅 admin） | ✓ | ✗（`403`） | ✗（`403`） |
| `POST /api/v1/admin/products/import` | ✓ | ✓ | ✗（`403`） |
| `PUT /api/v1/admin/products/:id`（改定价/上下架/排序/改挂子分类/商品级返佣 `rebate_percent`+`rebate_disabled`，R10-A） | ✓ | ✓ | ✗（`403`） |
| `PUT /api/v1/admin/product-groups/:id` | ✓ | ✓ | ✗（`403`） |
| `PUT /api/v1/admin/categories/:id`（R7，改名/改挂主分类，仅 admin） | ✓ | ✗（`403`） | ✗（`403`） |

### 10.6 实测差异与约定（2026-10-08，生产上游）

1. **`/cart/all` 的库存与试用字段恒为 0，不可用**：实测 159 个商品的 `stock_control` / `qty` / `ontrial`
   全为 0，而 `GET /cart/get_product_config` 的 `products.qty` / `stock_control` 才是真实值
   （例：商品 1 `stock_control=1, qty=70`）。因此导入的库存/试用一律取自**商品详情接口**，
   `stock_control` 随 `config_json` 缓存下发到视图，`stock_qty` 落 `products.stock_qty`。
2. **负数价格表示「该周期不售」**：实测商品 1 的 `biennially` / `triennially` 为 `-1.00`；
   159 个商品里 `monthly` 有 5 个、`quarterly` 33 个、`semiannually` 54 个、`annually` 32 个、
   `biennially` / `triennially` 各 158 个为负值或缺省。阶段 3b 复核：两年付/三年付仅
   「襄阳云服务器-A型」（本地 `id=112`，`upstream` 模式）有价——上游 `biennially=800.00` /
   `triennially=1200.00`，本地 `prices` 对应输出 `800.00` / `1200.00`。
   本地定价据此把该周期判为「无上游价」（输出 `null`），**不会**退化成 `0.00` 或负数。
3. **目录与详情两个接口的字段不一致**：`/cart/all` 的商品 `gid` 为 0（分组关系由目录的嵌套结构给出），
   详情接口的 `products.gid` 才是真实分组；`/cart/all` 有 `module`（如 `idcsmart_common`）而详情接口为空。
   导入时分组一律取**目录里的父分组 ID**，其余字段「目录优先、详情兜底」。
4. **单商品详情体量最大约 22KB**（159 个商品实测，`product_pricings` + `config_groups` + `sub` 价格行），
   故 `config_json` 用 `MEDIUMTEXT` 缓存原文。
5. **可配置项文案是上游原文**：`option_name` 形如 `area|区域`、`1|HK^香港`（`code|名称` / `序号|代码^名称`），
   阶段 3a 不清洗，会员端原样下发（见 10.3）。
6. **`description` 的形态与 R5 口径变更（2026-10-09 复核）**：上游把 `<li>` 存成 `&lt;li&gt;`
   （实测 159 个商品均是，行间为换行 `\n`）。R5 起**导入落库时做一次 HTML 实体反转义**
   （迁移 0012），库内与接口统一为解码后的原始 HTML；存量转义数据在下一次商品导入时自动刷新
   （第一次导入 `updated=159`，其后恢复幂等）。行数组展示请统一走 `description_lines`，
   解析对「转义/未转义」两种输入都兼容（见 10.3）。
7. **并发抓取安全**：导入对上游是纯只读（`/cart/all` + `/cart/get_product_config`），
   4 并发抓 159 个商品实测约 6.5 秒（串行约 40 秒以上）；客户端自带 405 重登与幂等 GET 重试。
8. **六周期解析升级的实测表现（阶段 3b，2026-10-08 生产上游复核）**：升级后第一次导入
   `updated=159`（`upstream_prices_json` 从 4 键整体刷新为 6 键），紧接着第二次导入
   `created=0, updated=0, unchanged=159, groups=29`（幂等保持）；商品 1（本地 `id=1`，
   `markup` 10%）回读 `prices.biennial` / `prices.triennial` 均为 `null`（上游 `-1.00`），
   `upstream_prices.prices` 六键齐备。优惠码真机用例：`annual` + `PERCENT10`（percent 10%、限 annual）
   → `price=220.00, discount_amount=22.00, final_amount=198.00`；`monthly` + 同码 →
   `cycle_not_applicable`；`annual` + `CASH20`（fixed 20、全周期）→ `20.00 / 200.00`；
   不存在码 → `404 优惠码不存在`；`cash20` 重复创建 → `409`（大小写不敏感）。

## 11. 优惠码（阶段 3b）

优惠码的**规则与校验**在阶段 3b 交付：管理端 CRUD + 会员端公开校验接口。
折扣的**应用**在阶段 4 交付：下单时抵扣并入金额（`POST /api/v1/orders`），**支付成功时**才计入
`used_count`（原子条件自增，防超用）。完整口径见 11.6 与 12.5。

### 11.1 数据模型（coupons 表）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int | 本地优惠码 ID（对外主键） |
| `code` | string | 优惠码：3-32 位 `[A-Za-z0-9_-]`；**唯一且大小写不敏感**（列排序规则 `utf8mb4_general_ci`——`welcome10` 与 `WELCOME10` 视为同一个码，冲突返回 `409`） |
| `type` | string | 折扣类型：`percent`（按比例）/ `fixed`（固定减免）；**创建后不可修改** |
| `value` | string | 折扣值（DECIMAL(12,2) 定点字符串）：`percent` 时是百分比（`0 < x ≤ 100`）；`fixed` 时是减免金额（`> 0`）；上限 `9999999999.99` |
| `cycles_json` | string[] | 适用周期：空数组 `[]` 表示**全部 6 周期**；元素必须是本地周期名（`monthly` / `quarterly` / `semiannual` / `annual` / `biennial` / `triennial`）；落库时去重并按标准周期顺序排列 |
| `starts_at` | string \| null | 生效时间（RFC3339 UTC）；`null` = 立即生效 |
| `expires_at` | string \| null | 过期时间（RFC3339 UTC）；`null` = 永不过期；**`now > expires_at` 判过期**（边界为闭区间：等于过期时间时仍有效） |
| `max_uses` | int | 最大使用次数，`0` = 不限 |
| `used_count` | int | 已使用次数（默认 0）；**阶段 4 起由支付成功触发原子条件自增**（下单不计数、取消不回退，防超用，见 12.5） |
| `status` | string | `on` 启用 / `off` 停用（默认 `on`）；**不提供 DELETE —— 停用即 `status=off`** |
| `comment` | string | 备注（管理端可见，≤ 255 字符，可空） |
| `owner_member_id` | int | **自建券归属邀请人 ID**（R10-A 落列，R10-B 提供建券接口）：`0` = 平台券（任何人可用）；非 `0` = 邀请人自建券（仅限**未绑定**邀请关系的客户使用，绑定规则见 18.5） |
| `source` | string | **派生字段**（不落库，只在管理端视图输出）：`owner_member_id == 0` → `platform`（平台券）；非 `0` → `member`（邀请人自建券），便于运营区分 |
| `created_at` / `updated_at` | string | RFC3339（UTC） |

### 11.2 折扣计算与校验语义

**折扣计算**（全部整数分计算，percent 四舍五入到分 half-up）：

| type | 折扣额 | 说明 |
| --- | --- | --- |
| `percent` | `price × value%` | 例：`220.00 × 10% = 22.00`；`20.05 × 10% = 2.005 → 2.01` |
| `fixed` | `min(value, price)` | 超额封顶：减免大于售价时按售价计 |

`final_amount = price − discount_amount`（不会为负）；`price` 是该商品该周期的**本地售价**
（与会员端 `prices` 同周期的值一致）。金额一律为定点小数字符串（两位小数）。

**校验判定顺序**（依次判断，命中即返回）：

| 顺序 | 条件 | reason |
| --- | --- | --- |
| 1 | `status != on` | `disabled` |
| 2 | `now < starts_at` | `not_started` |
| 3 | `now > expires_at` | `expired` |
| 4 | `max_uses > 0` 且 `used_count >= max_uses` | `used_up` |
| 5 | 适用周期不含该周期，**或该商品该周期本地不可售**（`price = null`） | `cycle_not_applicable` |

`reason` 枚举**固定为这 5 个**。第 5 条的两种情形归入同一枚举：不可售的周期没有折扣可算，
前端拿到 `cycle_not_applicable` 即视为「该周期不可用此码」。

### 11.3 管理端接口

#### `POST /api/v1/admin/coupons`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，角色 `admin` / `finance`（`support` 返回 `403`） |
| 请求体 | `code`（必填）、`type`（必填）、`value`（必填）、`cycles`（可选，省略/null/空数组 = 全部周期）、`starts_at` / `expires_at`（可选，RFC3339，null = 不限制）、`max_uses`（可选，缺省 0）、`status`（可选，缺省 `on`）、`comment`（可选） |
| 成功 | HTTP 200，`data` 为优惠码对象（结构见下） |

```bash
curl -s -X POST http://127.0.0.1:8080/api/v1/admin/coupons \
  -H 'Authorization: Bearer <admin-token>' -H 'Content-Type: application/json' \
  -d '{"code":"WELCOME10","type":"percent","value":"10","cycles":["annual"],"max_uses":100,"comment":"新人首年优惠"}'
```

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "id": 1,
    "code": "WELCOME10",
    "type": "percent",
    "value": "10.00",
    "cycles": ["annual"],
    "starts_at": null,
    "expires_at": null,
    "max_uses": 100,
    "used_count": 0,
    "status": "on",
    "comment": "新人首年优惠",
    "created_at": "2026-10-08T09:30:00Z",
    "updated_at": "2026-10-08T09:30:00Z"
  }
}
```

校验规则与错误码：

| code | HTTP | 场景 |
| --- | --- | --- |
| `40001` | 400 | `code` 非 3-32 位 `[A-Za-z0-9_-]`、`type` 取值非法、`value` 写法非法（非十进制/超两位小数/超 `9999999999.99`）、`cycles` 含非法周期、时间非 RFC3339、`status` 非法、`comment` 超 255 字符 |
| `40002` | 400 | `percent` 的 `value` 不在 `(0, 100]`、`fixed` 的 `value ≤ 0`、`starts_at` 不早于 `expires_at`、`max_uses` 超出 `0 ~ 4294967295` |
| `403` | 403 | 角色为 `support` |
| `409` | 409 | `code` 已存在（**比较不区分大小写**） |
| `50001` | 500 | 写入本地库失败 |

#### `GET /api/v1/admin/coupons`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token（所有角色） |
| 查询参数 | `page`（默认 1）、`page_size`（默认 20，1-100）、`status`（`on` / `off`）、`keyword`（按 `code` 模糊匹配，不区分大小写） |
| 成功 | HTTP 200，`data` 为 `{items, page, page_size, total}`，**新建在前**（`id` 降序） |
| 错误码 | `40001`（分页参数越界/非数字、`status` 取值非法） |

#### `GET /api/v1/admin/coupons/:id`

按本地 ID 查询单条优惠码（结构同创建响应）。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token（所有角色） |
| 错误码 | `40001`（ID 非正整数）、`404`（`优惠码不存在`） |

#### `PUT /api/v1/admin/coupons/:id`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，角色 `admin` / `finance` |
| 请求体 | `code` / `value` / `cycles` / `starts_at` / `expires_at` / `max_uses` / `status` / `comment`；字段均可选但**至少提供一个**。`type` 不可修改（请求中的 `type` 字段被忽略） |
| 成功 | HTTP 200，`data` 为更新后的优惠码对象 |

三态说明：`cycles` / `starts_at` / `expires_at` 传 `null` 表示**清空**——时间清为 `NULL`
（不限制），`cycles` 恢复为全部周期；不传（键缺席）表示保持原值。`starts_at` / `expires_at`
的先后关系按**更新后的最终值**校验（未提供的沿用库内现值）。

**自建券的额外约束（R10-B）**：`owner_member_id` 非 `0` 的优惠码是**邀请人自建券**，
其折扣上限由邀请人的生效返佣比例决定（见 18.5「折扣上限」）。为杜绝「管理员代改大 `value`
绕过上限」，**管理端 `PUT` 对自建券一律拒绝修改 `value`**（请求体含 `value` 即 `40002`，
message=`自建券的折扣值不可修改`）；平台券（`owner_member_id = 0`）不受此限。
`type` 仍创建后不可修改（对平台券与自建券一致）。

```bash
# 停用（替代删除）并放宽为全部周期
curl -s -X PUT http://127.0.0.1:8080/api/v1/admin/coupons/1 \
  -H 'Authorization: Bearer <admin-token>' -H 'Content-Type: application/json' \
  -d '{"status":"off","cycles":null,"expires_at":null}'
```

错误码：与创建一致（`40001` / `40002` / `403` / `404` 优惠码不存在 / `409` code 冲突 / `50001`）。

### 11.4 公开校验接口

#### `GET /api/v1/coupons/:code/validate`

下单前的折扣试算：校验优惠码并计算某商品某周期的折扣明细。**无需鉴权**，
`:code` 匹配不区分大小写。

| 项目 | 说明 |
| --- | --- |
| 查询参数 | `product_id`（必填，**本地商品 ID**，正整数）、`cycle`（必填，本地周期名） |
| 成功 | HTTP 200，`data` 为 `{valid, ...}` |

判定顺序：**先参数格式（40001）→ 再 code 存在性（404）→ 再商品存在且上架（404）→ 最后业务判定**。

```bash
curl -s 'http://127.0.0.1:8080/api/v1/coupons/welcome10/validate?product_id=1&cycle=annual'
```

有效（HTTP 200）：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "valid": true,
    "code": "WELCOME10",
    "type": "percent",
    "value": "10.00",
    "price": "220.00",
    "discount_amount": "22.00",
    "final_amount": "198.00"
  }
}
```

无效（HTTP 200，只带 `valid` 与 `reason`）：

```json
{ "code": 0, "message": "ok", "data": { "valid": false, "reason": "cycle_not_applicable" } }
```

| 字段 | 说明 |
| --- | --- |
| `code` | 库内优惠码原文（回带规范化后的写法，便于前端展示） |
| `price` | 该商品该周期的本地售价（会员端 `prices` 中的同周期值） |
| `discount_amount` / `final_amount` | 折扣额与折后价（定点小数字符串，见 11.2） |

错误码：

| code | HTTP | 场景与 message |
| --- | --- | --- |
| `40001` | 400 | `product_id` 缺失/非正整数（`product_id 必须为正整数（本地商品 ID）`）、`cycle` 缺失或非本地周期名（`cycle 需为以下之一：…`） |
| `404` | 404 | code 不存在（`优惠码不存在`）、商品不存在**或已下架**（`商品不存在`，与会员端商品接口语义一致） |
| `50001` | 500 | 查询本地库失败 |

### 11.5 角色权限矩阵（优惠码部分）

| 接口 | admin | finance | support |
| --- | --- | --- | --- |
| `GET /api/v1/coupons/:code/validate` | 公开（无需 token） | 公开 | 公开 |
| `GET /api/v1/admin/coupons`、`GET /api/v1/admin/coupons/:id` | ✓ | ✓ | ✓ |
| `POST /api/v1/admin/coupons` | ✓ | ✓ | ✗（`403`） |
| `PUT /api/v1/admin/coupons/:id` | ✓ | ✓ | ✗（`403`） |

### 11.6 折扣应用口径（阶段 4 已实现）与仍不支持的能力

**折扣应用（阶段 4 交付，实现口径见 12.5）**：

| 环节 | 行为 |
| --- | --- |
| 下单（`POST /api/v1/orders`） | 复用本节 11.2 的判定顺序与折扣计算；不通过 → **统一 40002 + 明确 message**（`优惠码不存在` / `优惠码已停用` / `优惠码尚未生效` / `优惠码已过期` / `优惠码使用次数已用尽` / `优惠码不适用于该周期`）。通过 → 折扣并入订单金额，并快照 `coupon_id` / `coupon_code` / `discount_amount` |
| 支付成功（在线支付回调 / 余额支付） | 在同一事务内对 `used_count` 做**原子条件自增**：`UPDATE coupons SET used_count = used_count + 1 WHERE id = ? AND (max_uses = 0 OR used_count < max_uses)`；影响行数为 0（极端并发下已超用）时**只记 WARN 日志、不阻断入账** |
| 下单未支付 / 取消订单 | **不占用次数**（只有支付成功才计数），因此取消订单无需回退 `used_count` |

`used_count` 是**全站总次数**口径：不同会员共用同一个计数器；单笔订单只计 1 次（本批 `qty` 固定 1）。

**仍不支持的能力**：

1. **商品范围限定**：优惠码只按「周期」限定适用范围，不区分商品/分组（全站通用）。
2. **每人限用**：没有「每个会员限用 N 次」的约束（`max_uses` 是全站总次数上限）。
3. **核销明细**：`used_count` 只有计数，没有「哪张订单用了哪个码」的独立核销表
   （订单表本身有 `coupon_id` / `coupon_code` 快照，可按订单追溯）。
4. **新购/续费区分**：优惠码不区分首购与续费场景（本批只能新购——见 12.10）。

## 12. 支付与财务（阶段 4）

本节描述：后台设置机制（12.1）、易支付渠道协议（12.2）、订单/充值单/流水的数据模型与状态机（12.3）、
会员端接口（12.4）、优惠码应用口径（12.5）、管理端对账接口（12.6）、角色矩阵（12.7）、错误码（12.8）、
业务边界（12.9）、暂不支持清单（12.10）。

> **支付链路与交付链路的边界（阶段 5a 起）**：下单与支付入账只操作本地库（创建订单 / 落支付状态 / 记账），
> 交付（上游开通）在**入账事务提交后**触发、**不阻塞回调应答**，时序与幂等见**第 14 节**。
> 本节订单相关的状态机与接口表述已按阶段 5a 同步更新（订单状态扩为 6 态、视图新增交付字段）。

### 12.1 设置机制（后台管理设置）

**总原则**：所有「用户可设置」的内容一律做进**后台管理设置**（`settings` 表 + 管理端接口），
**不让用户改任何配置文件**；`backend/config.yaml` 只保留部署级参数
（数据库连接、监听端口、JWT 密钥、日志等级）。阶段 4 把**易支付参数**与**上游对接参数**一并迁入本机制。

**数据模型（`settings` 表，迁移 0006）**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `key` | string | 设置键（主键）；本批两个：`payment.epay`、`upstream`；阶段 4+ 新增 `site`（站点信息）、`installed`（安装标记）、`install.progress`（安装进行中标记，临时），见第 13 节；**阶段 6b 新增 `email.smtp`（邮件通道）与 `notifications`（通知开关与到期提醒天数），见第 17 节**；**R10-A 新增 `referral`（邀请返佣：总开关 + 全局默认比例 + 自建券折扣上限），见第 18 节**；**R13-A 新增 `updates.github`（更新源：仓库与可选 GitHub Token）与 `updates.check`（更新检查缓存，非用户设置），见第 19 节**；**R14 新增 `db_backup`（数据库备份目录，见 21.4 / 21.10；R15 起提供读写接口）**；**R16 为既有的 `site` 键（站点信息，安装向导第 5 步写入）补上「站点对外地址」的读写接口（见 22.4）——该地址同时用作上游回推的下游地址**；**R19 批 1 新增 `cache`（缓存基础设施：驱动选择 file/redis + Redis 参数，见第 24 节）** |
| `value` | text | 设置值（JSON 文本），结构与键一一对应 |
| `updated_by` | int \| null | 最后修改的管理员 ID（审计用；NULL = 没有接口写入记录） |
| `created_at` / `updated_at` | string | RFC3339（UTC） |

表是**通用设计**：后续阶段的站点/邮件等设置直接复用，无需再建表。

**生效方式（最终选型）：读时校验、按内容失效。** 不采用「启动时读一次」，也不采用固定 TTL 缓存：

1. 每次使用都读一次 settings（单一主键查询，开销可忽略）；
2. 调用方（支付渠道工厂 / 上游客户端管理器）以**设置 JSON 原文**为内容指纹：
   指纹未变 → 复用现有实例（上游客户端据此**保留 JWT 缓存**，避免每次调用都重新登录）；
   指纹变化 → 立即重建实例（上游客户端**丢弃旧配置与旧 JWT 缓存**）。

因此后台改完设置**下一次调用即生效**：既不需要重启，也没有「短缓存窗口」。
集成测试对此有断言（设置不变时不重复登录；只改密钥后业务调用会在新密钥下重新登录）。
R17 起**上游地址不参与该机制**：它由构建期注入（8.6），指纹只覆盖 `username` / `api_key` / `timeout_seconds`。

**渠道可插拔（扩展方式）**：新增支付渠道 = ①新增一个 `payment.Provider` 实现（下单/验签/应答）；
②在注册表登记工厂 `Register("<渠道名>", factory)`（工厂按当前设置构造实例）；
③新增设置键与管理端接口（复用 12.1 的机制）；
④注册回调路由（如 `/api/v1/payments/<渠道>/notify`）。业务代码只依赖 `Provider` 抽象与
`Registry.Get(ctx, name)`，不直接依赖具体渠道实现。

**密钥安全**

1. `key`（易支付商户密钥）与 `api_key`（上游密钥）只存 settings 表与本进程内存；
2. **任何接口响应都不回显明文**，只给 `*_configured`（布尔）与掩码；
3. 掩码有两处口径（都只暴露前缀，不含完整密钥）：
   - **设置接口（本节）**：`MaskSecret` = 首 4 位 + `****`（如 `1sXR****`）；不足 8 位时只给 `****`；
   - **上游探活接口（第 9 节，沿用阶段 2 口径）**：首 4 位 + `****` + 末 3 位（如 `1sXR****ZG5`）。
4. 日志与错误信息同样不含明文（集成测试对响应与日志均有断言）。

#### `GET /api/v1/admin/settings/payment/epay`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，**仅 `admin` 角色**（`finance` / `support` 返回 `403`，**读取同样受限**） |
| 成功 | HTTP 200，`data` 为易支付设置视图 |

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "enabled": true,
    "gateway": "https://pay.example.com",
    "pid": "1001",
    "key_configured": true,
    "key_masked": "1sXR****",
    "notify_url": "https://oem.example.com/api/v1/payments/epay/notify",
    "notify_url_recommended": "http://127.0.0.1:8080/api/v1/payments/epay/notify",
    "return_url": "https://oem.example.com/pay/result",
    "updated_by": 1,
    "updated_at": "2026-10-08T09:42:33Z"
  }
}
```

| 字段 | 说明 |
| --- | --- |
| `enabled` | 渠道是否启用；启用时 `gateway` / `pid` / `key` / `notify_url` 必须齐全 |
| `key_configured` / `key_masked` | 是否已配置商户密钥 / 密钥掩码（**绝不回显明文**；未配置时 `false` / 空串） |
| `notify_url_recommended` | 按当前请求 Host 推导的推荐回调地址（只做提示，不校验可达性） |
| `updated_by` / `updated_at` | 最后修改的管理员 ID 与时间；从未写入时为 `null` |

推荐值：`notify_url` = `http(s)://<系统域名>/api/v1/payments/epay/notify`；
`return_url` = 前端支付结果页（本服务在其上做最简 302，见 12.2.4）。

#### `PUT /api/v1/admin/settings/payment/epay`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 同上（仅 `admin`） |
| 请求体 | `enabled` / `gateway` / `pid` / `key` / `notify_url` / `return_url`，**均可选但至少提供一个**；键缺席或 `null` = 不修改 |
| 成功 | HTTP 200，`data` 为更新后的设置视图（含审计字段） |

**密钥三态语义**（`key`，`upstream.api_key` 同款）：**省略 = 保持不变、提供新值 = 替换、空串 = 清空**。

```bash
# 1) 首次启用（含密钥）
curl -s -X PUT http://127.0.0.1:8080/api/v1/admin/settings/payment/epay \
  -H 'Authorization: Bearer <admin-token>' -H 'Content-Type: application/json' \
  -d '{"enabled":true,"gateway":"https://pay.example.com","pid":"1001","key":"<商户密钥>",
       "notify_url":"https://oem.example.com/api/v1/payments/epay/notify"}'

# 2) 只改 return_url（省略 key → 密钥保持不变）
curl -s -X PUT http://127.0.0.1:8080/api/v1/admin/settings/payment/epay \
  -H 'Authorization: Bearer <admin-token>' -H 'Content-Type: application/json' \
  -d '{"return_url":"https://oem.example.com/pay/result"}'

# 3) 清空密钥并停用（只清 key 而保留 enabled=true 会被拒绝）
curl -s -X PUT http://127.0.0.1:8080/api/v1/admin/settings/payment/epay \
  -H 'Authorization: Bearer <admin-token>' -H 'Content-Type: application/json' \
  -d '{"enabled":false,"key":""}'
```

校验按**合并后的最终值**执行：

| code | HTTP | 场景与 message |
| --- | --- | --- |
| `40001` | 400 | 未提供任何字段（`至少提供一个字段：enabled / gateway / pid / key / notify_url / return_url`）；URL 字段不是合法 `http(s)` 或缺少主机名（`gateway 必须以 http:// 或 https:// 开头，收到 "..."`） |
| `40002` | 400 | `enabled=true` 时必填项缺失，message 列出缺失项：`enabled=true 时以下字段不能为空：gateway、pid、key、notify_url` |
| `403` | 403 | 角色不是 `admin` |
| `500` | 500 | 库内该键的值损坏（只可能由手工改库导致） |
| `50001` | 500 | 读写本地库失败 |

#### `GET /api/v1/admin/settings/upstream`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，仅 `admin` 角色 |
| 成功 | HTTP 200，`data` 为上游设置视图 |

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "base_url": "https://lyew.com",
    "base_url_fixed": true,
    "username": "16650492239",
    "api_key_configured": true,
    "api_key_masked": "1sXR****",
    "timeout_seconds": 10,
    "updated_by": 1,
    "updated_at": "2026-10-08T09:42:33Z"
  }
}
```

- `base_url` 是**构建期注入值**（8.6，R17），**只读**：它不由本接口写入、也不在任何地方可改；
  构建时未注入时输出空串。
- `base_url_fixed` 恒为 `true`，供界面直接把该字段渲染成不可编辑项（不依赖前端硬编码假设）。
- `api_key_configured` / `api_key_masked` 语义同支付 `key`；`timeout_seconds` 未配置时输出缺省 5。
- `updated_by` / `updated_at` 反映的是**凭据设置**（settings 行的审计），与构建期地址无关。
- 字段语义与「未配置」口径见 8.6。

#### `PUT /api/v1/admin/settings/upstream`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 同上（仅 `admin`） |
| 请求体 | `username` / `api_key` / `timeout_seconds`，**均可选但至少提供一个**；`api_key` 三态语义同支付 `key`。**上游地址不在内**：请求体里出现 `base_url`（任意取值，含 `null` 与空串）一律拒绝，见下表 |
| 成功 | HTTP 200，`data` 为更新后的设置视图（`base_url` 恒为构建期值） |

| code | HTTP | 场景与 message |
| --- | --- | --- |
| `40002` | 400 | **请求体出现 `base_url`**：`上游地址由系统固定（构建期注入），不可修改：请从请求体中移除 base_url`。该请求**不落库**——连同一请求里带的 `username` / `api_key` 也不改动（校验发生在合并与写库之前） |
| `40001` | 400 | 未提供任何字段（`username` / `api_key` / `timeout_seconds` 全缺） |
| `40002` | 400 | `timeout_seconds` 不在 1-120：`timeout_seconds 需为 1-120 之间的整数，收到 0` |
| `403` | 403 | 角色不是 `admin` |
| `500` / `50001` | 500 | 值损坏 / 库操作失败 |

**地址不可修改的承诺（R17）**：只有上述四个字段参与更新，`base_url` 只存在于 `GET` 的只读回显中；
**没有任何 HTTP 路径能改上游地址**（后台设置接口、探活接口、其余管理端接口都不行），
库内遗留的 `base_url` 键被忽略（8.6）。要换地址只能改构建期变量并重新发布。

**生效承诺**：PUT 之后，下一次上游调用必须使用新凭据——**包括只改 `api_key`**（地址固定、密钥变了）的情况：
此时客户端被重建、旧 JWT 缓存失效，会在新参数下重新登录。探活接口按当前设置取参数（8.6 / 第 9 节）。

### 12.2 易支付渠道（epay）

按**彩虹易支付标准协议**实现。本批对 **mock 网关**完成全链路联调（下单/验签/回调/幂等/金额校验），
真机联调另行安排（12.10）。如遇平台变体差异，按阶段 2「实测差异」的模式记入 12.2.6。

渠道参数全部来自 12.1 的 `payment.epay` 设置：`gateway` / `pid` / `key` / `notify_url` / `return_url`。

#### 12.2.1 下单（`POST {gateway}/mapi.php`）

form 参数：

| 参数 | 取值 |
| --- | --- |
| `pid` | 设置里的商户 ID |
| `type` | 支付方式：`alipay` / `wxpay`；取请求的 `pay_type`，缺省 `alipay`；**其他取值返回 `40002`** |
| `out_trade_no` | **本地单号**（`O…` 订单 / `R…` 充值单，见 12.2.5） |
| `notify_url` | 设置里的 `notify_url` |
| `return_url` | **本服务的 return 端点**：由 `notify_url` 推导（结尾 `/notify` → `/return`；否则取 scheme+host + `/api/v1/payments/epay/return`）；`notify_url` 为空时不传 |
| `name` | 支付标题：订单为「商品名 + 周期」（按字符截断到 64），充值为 `余额充值` |
| `money` | 应付金额（定点小数字符串）：订单为 `final_amount`（已扣优惠码），充值为充值金额 |
| `sign` | 见 12.2.2 |
| `sign_type` | 固定 `MD5` |

成功响应（渠道 JSON）：

```json
{"code":1,"trade_no":"2026100822001","payurl":"https://pay.example.com/pay/xxx"}
```

- `code` 兼容数字 `1` 与字符串 `"1"`；`code != 1` → 渠道拒绝 → **`50002`**（message 带渠道 `msg` 摘要）；
- 缺 `payurl` → 渠道响应异常 → **`50002`**；
- 响应体读取上限 64 KiB、日志只记前 200 字节（异常保护，且都是脱敏内容）；
- `qrcode` / `urlscheme` 等额外字段原样放进响应的 `pay.extra`。

#### 12.2.2 签名（下单与回调验签**共用同一实现**）

1. 取全部业务参数，**剔除 `sign` 与 `sign_type`**，跳过值为空的参数；
2. 按参数名 **ASCII 升序**排序，拼成 `a=1&b=2` 形式（原值直接拼接，不做 URL 编码）；
3. 末尾**直接拼接商户 KEY**（不加密钥名），MD5 后取**小写**十六进制。

验签用常量时间比较、大小写不敏感（签名统一输出小写）。

#### 12.2.3 异步回调

#### `POST|GET /api/v1/payments/epay/notify`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | **无需 token**——验签 + 金额校验是唯一凭证（渠道无法携带我方 token） |
| 请求参数 | `pid`、`trade_no`、`out_trade_no`、`type`、`name`、`money`、`trade_status`、`sign`、`sign_type` |
| 成功应答 | HTTP 200 + **纯文本** `success` |
| 失败应答 | HTTP 200 + **纯文本** `fail`（渠道按其策略重试） |

处理顺序与应答口径（**逐条对齐，实现与测试均以此为准**）：

| # | 条件 | 应答 | 副作用 |
| --- | --- | --- | --- |
| 1 | 渠道不可用（`payment.epay` 未启用 / 不完整 / 值损坏） | `fail` | 记 WARN（无密钥无法验签） |
| 2 | 缺少必要参数、`pid` 与本地设置不一致、`sign_type` 非 MD5、**验签失败** | `fail` | 记 WARN |
| 3 | `trade_status != TRADE_SUCCESS` | `success` | 记 WARN，不处理（非成功状态不是错误，也无需重试；状态变化时渠道会再通知一次） |
| 4 | `out_trade_no` 前缀不可识别（既非 `O` 也非 `R`）、或本地查不到对应订单/充值单 | `fail` | 记 WARN（本地单在发起支付前就已创建，查不到属异常，保留重试以留下痕迹） |
| 5 | `money` 与本地单金额**不一致**（按整数分比较；任一侧解析失败也算不一致） | `fail` | 记 WARN，**不入账** |
| 6 | 订单已是 `paid` | `success` | 幂等：不重复处理、不重复计数 |
| 7 | 订单已 `cancelled` | `success` | 记 WARN，**不处理**、不改状态（避免渠道无限重试；已付款但订单被取消的极端情况由运营对账处理） |
| 8 | 订单 `pending` | `success` | **单事务**：`pending→paid`、记 `pay_channel=epay`/`channel_trade_no`/`pay_time`、优惠码条件自增 |
| 9 | 充值单已是 `paid` | `success` | 幂等：不重复加款 |
| 10 | 充值单已 `closed`（本批不产生该状态） | `success` | 记 WARN，不处理 |
| 11 | 充值单 `pending` | `success` | **单事务**：`pending→paid`、记 `channel_trade_no`/`paid_at`、会员余额加款、写流水（`recharge`，余额前后准确） |
| 12 | 落库异常 | `fail` | 记 ERROR（渠道重试） |

金额比较对象：订单 = `final_amount`（应付金额，已扣优惠码）；充值单 = `amount`。
并发安全：入账事务对订单/充值单行 `SELECT ... FOR UPDATE`，回调重复到达不会重复入账。

#### 12.2.4 同步跳转（return）

#### `GET /api/v1/payments/epay/return`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 无需 token |
| 行为 | 最简实现：**302 到设置里的 `return_url`**；未配置（或设置读取失败）时 HTTP 200 + 简单提示页（`支付已完成，请返回商户页面查看订单状态。`） |
| 说明 | 同步跳转**不参与入账**（以异步通知为准），因此不验签、不改状态，也不依赖渠道是否启用 |

#### 12.2.5 out_trade_no 策略

- 本地单号 = 渠道 `out_trade_no`，格式 `<前缀><UTC 时间 yyyyMMddHHmmss><6 位随机大写字母/数字>`
  （如 `O20261008143015K7Q2ZP`）；前缀 `O` = 订单、`R` = 充值单，回调据此分派（12.2.3 第 4 条）。
- 生成时若唯一键冲突自动换号重试（最多 5 次）。
- **重复发起支付沿用同一 `out_trade_no`**（不换号）：即使渠道对同号重复下单有限制，也不会造成重复入账
  （本地按 `pending→paid` 幂等，回调只可能命中同一张本地单）。
  **若真机联调发现渠道对同号重复下单报错**，则改为「复用已建渠道单的 payurl」或「换新号 + 旧号作废」，
  并把差异记入 12.2.6。

#### 12.2.6 与标准协议的差异（本批为空）

| # | 现象 | 处理 |
| --- | --- | --- |
| — | 本批对 mock 网关完成全链路联调，与彩虹易支付标准协议一致；真机联调待安排（12.10） | — |

### 12.3 数据模型与状态机

**订单（`orders`，迁移 0006）**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` / `trade_no` | int / string | 本地主键与单号（`O…`，唯一，同时是渠道 `out_trade_no`） |
| `member_id` / `product_id` | int | 下单会员与本地商品 |
| `product_name` | string | 商品名快照（商品改名后订单仍显示下单时的名称） |
| `cycle` / `qty` | string / int | 计费周期（6 周期之一）与数量（**本批固定 1**） |
| `config_json` | string | 所选配置项快照 `{"<配置项 id>": "<所选值 id>"}`（数量型配置存**数量**字符串，R6 起；阶段 5a 起交付时原样拼 `configoption`，口径见 10.3 第 6 条与 14.5 第 1 条） |
| `amount` / `discount_amount` / `final_amount` | string | 原价 / 优惠码折扣额 / 应付金额（定点小数字符串，`final = amount − discount`） |
| `coupon_id` / `coupon_code` | int \| null / string | 所用优惠码快照（未用码：NULL / 空串） |
| `status` | string | `pending` / `paid` / `provisioning` / `active` / `failed` / `cancelled`（阶段 5a 扩为 6 态，完整状态机见 14.1） |
| `type` | string | `new`（新购，默认）/ `renew`（续费；阶段 5b 迁移 0008 新增） |
| `pay_channel` / `channel_trade_no` / `pay_time` | string / string / string \| null | 支付渠道（`epay` / `balance`）、渠道单号、支付时间（UTC） |
| `host_id` | int \| null | 上游主机 ID（阶段 5a 交付成功后写入；未交付为 `null`；续费单交付成功后同填实例主机 ID） |
| `instance_id` | int \| null | 续费单对应的实例（阶段 5b 迁移 0008 新增；`new` 单为 `null`） |
| `provision_error` | string | 最近一次交付失败原因（脱敏，最多 500 字符；成功时清空，空串表示无错误） |
| `delivered_at` | string \| null | 交付完成时间（UTC；未交付为 `null`） |
| `created_at` / `updated_at` | string | RFC3339（UTC） |

状态机（阶段 5a 扩为 6 态，流转规则与幂等锚点见 14.1）：`pending → paid`（在线支付回调 / 余额支付）、
`pending → cancelled`（本人取消）、`paid → provisioning → active / failed`（自动交付）、
`failed → provisioning`（管理员重试）。**没有** `paid → refunded`，**没有**自动超时关闭（12.10）。
阶段 5b 起交付分支按订单 `type` 分流：`new` 走上游开通、`renew` 走上游续费
（`RenewHost`，链路与幂等见 15.5）；两者共用同一状态机与认领行锁锚点。

**充值单（`recharges`）**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` / `trade_no` | int / string | 本地主键与单号（`R…`，唯一，同时是渠道 `out_trade_no`） |
| `member_id` / `amount` | int / string | 会员与充值金额（1.00 ~ 50000.00） |
| `channel` | string | 支付渠道（本批仅 `epay`） |
| `status` | string | `pending` / `paid` / `closed`（`closed` 本批不产生，留后续批次） |
| `channel_trade_no` / `created_at` / `paid_at` / `expires_at` | string \| null | 渠道单号 / 创建时间 / 到账时间 / 过期时间（**本批不自动关闭**，`expires_at` 恒为 null） |

**余额流水（`ledger`，只增不改）**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` / `member_id` | int | 主键与会员 |
| `type` | string | `recharge`（充值入账，正）/ `order_pay`（余额支付扣款，负）/ `refund`、`adjust`（预留，本批不产生） |
| `amount` | string | **有符号**金额：入账为正、出账为负 |
| `balance_before` / `balance_after` | string | 变动前后余额，恒满足 `balance_after = balance_before + amount` |
| `ref_type` / `ref_id` | string / int | 关联单据类型（`recharge` / `order`）与 ID |
| `note` / `created_at` | string | 备注（如「充值 R2026…」「订单支付 O2026…」）与时间（UTC） |

**金额与时间口径**：数据库 `DECIMAL(14,2)`，接口一律输出定点小数字符串（两位小数），
内部计算全部走**整数分**（无浮点误差）；时间列统一 UTC，接口输出 RFC3339 UTC。

### 12.4 会员端接口（订单与财务）

全部要求**会员 token**（`aud=member`），且只操作**本人**数据；未携带/无效 token → `401`。

**订单对象字段**（下单/详情/列表/支付返回统一结构，字段语义见 12.3 的 orders 表）：
`id` / `trade_no` / `member_id` / `product_id` / `product_name` / `cycle` / `qty` / `config`（对象）/
`amount` / `discount_amount` / `final_amount` / `coupon_code` / **`price_breakdown`** / `status` /
**`type`** / `pay_channel` / `channel_trade_no` / `pay_time` / `host_id` / **`instance_id`** /
`provision_error` / `delivered_at` / `created_at` / `updated_at`
（加粗为历次新增：阶段 5b 的 `type` 区分新购 `new` / 续费 `renew`、`instance_id` 为续费单对应的实例 ID；
R7 的 `price_breakdown` 为配置差价明细快照，见 10.2.1 与下方算例）。
`amount` 自 R7 起为**基础价 + Σ 配置差价**（此前为基础价）。

#### `POST /api/v1/orders`

| 项目 | 说明 |
| --- | --- |
| 请求体 | `product_id`（必填）、`cycle`（必填，6 周期之一）、`config`（可选）、`coupon_code`（可选，空串 = 不用码） |
| 成功 | HTTP 200，`data` 为订单对象（含金额明细） |

`config` 的键是**上游配置项的本地 ID**（`options[].id`），值为所选可选值的 `id`（`values[].id`）；
值接受 **JSON 字符串或整数**写法（服务端统一按字符串快照）。**数量型配置项**（`type` ∈
{4, 11, 14, 15, 19}，见 10.3 第 5/6 条）的值是**数量**（非负整数，须在该值的
`qty_minimum`~`qty_maximum` 内），如 `{"7": "50"}` 表示带宽 50Mbps。
只接受该商品**会员可见**的项与值（上游标记 `hidden` 的项/值按「未知」处理）。
（键值口径的阶段 5a 真机实测依据见 14.5 第 1 条；`upstream_id` 不再作为下单键值。）

```bash
curl -s -X POST http://127.0.0.1:8080/api/v1/orders \
  -H 'Authorization: Bearer <member-token>' -H 'Content-Type: application/json' \
  -d '{"product_id":1,"cycle":"monthly","config":{"10":"50"}}'
```

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "id": 1,
    "trade_no": "O20261008143015K7Q2ZP",
    "member_id": 1,
    "product_id": 1,
    "product_name": "香港二区 CN2 A型",
    "cycle": "monthly",
    "qty": 1,
    "config": {"10": "50"},
    "amount": "45.00",
    "discount_amount": "0.00",
    "final_amount": "45.00",
    "coupon_code": "",
    "price_breakdown": {
      "base": "20.00",
      "deltas": [
        { "option_id": 10, "option_name": "data_disk_size|数据盘", "kind": "quantity",
          "value_id": 37, "qty": 50, "unit": "0.50", "billable": 50,
          "amount": "25.00", "note": "" }
      ],
      "total": "45.00"
    },
    "status": "pending",
    "pay_channel": "",
    "channel_trade_no": "",
    "pay_time": null,
    "host_id": null,
    "provision_error": "",
    "delivered_at": null,
    "created_at": "2026-10-08T14:30:15Z",
    "updated_at": "2026-10-08T14:30:15Z"
  }
}
```

算例：商品 1「香港二区 CN2 A型」月付基础价 `20.00`，`data_disk_size`（配置项 id `10`）单档位
`[0-256 @0.50]`，提交数量 `50` → 可计费数量 `50`，差价 `0.50 × 50 = 25.00`，
`amount = 20.00 + 25.00 = 45.00`。

**金额口径（R7，见 10.2.1）**：`amount` = **基础价 + Σ 配置差价**（成交总额），
`discount_amount` 为优惠码对**该总额**的抵扣，`final_amount = amount − discount_amount`。
`price_breakdown` 是差价明细快照（同内容写入 `orders.price_breakdown_json`，迁移 0013，对账可查）：
`base` = 基础价，`deltas[]` 为逐配置项差价，`total` = `base + Σ deltas.amount`（= `amount`）。
`deltas[].note` 为 `"无差价数据"` 时该项按 `0.00` 计（见 10.2.1）。`kind=quantity` 时 `qty`/`billable` 有值，
`kind=select` 时 `value_id` 有值。

校验顺序（依次判断，命中即返回）：

| # | 条件 | code / HTTP | message |
| --- | --- | --- | --- |
| 1 | 请求体非合法 JSON、`product_id` 非正整数、`cycle` 非本地周期、`config` 键值类型不符 | `40001` / 400 | 见 12.8 |
| 2 | 商品不存在**或已下架**（统一 404，不泄露存在性） | `404` / 404 | `商品不存在` |
| 3 | 该周期无本地售价（不可售） | `40002` / 400 | `该商品在 <cycle> 周期不可售（无本地售价）` |
| 4 | `config` 含未知配置项/未知取值（含隐藏项与隐藏值）、数量型数量非整数或超出 `qty_minimum`~`qty_maximum`，或快照超 1024 字节 | `40002` / 400 | `未知的配置项 "..."（不在该商品的可配置项内）` / `配置项 "..." 不支持所选值 "..."` / `配置项 "..." 的数量必须在 20~100 之间（收到 999）` |
| 5 | 优惠码不存在 / 无效 / 不适用于该周期 | `40002` / 400 | `优惠码不存在`、`优惠码已停用`、`优惠码尚未生效`、`优惠码已过期`、`优惠码使用次数已用尽`、`优惠码不适用于该周期` |
| 6 | 落库失败（含单号冲突重试耗尽） | `50001` / 500 | — |

**库存不做强校验**：导入库存是上游快照，防超卖由上游开通环节最终保证（12.9）。

#### `GET /api/v1/orders`

| 项目 | 说明 |
| --- | --- |
| 查询参数 | `page`（缺省 1）、`page_size`（缺省 20，1-100）、`status`（可选：`pending` / `paid` / `provisioning` / `active` / `failed` / `cancelled`） |
| 成功 | HTTP 200，`data` = `{items, page, page_size, total}`，**新建在前**（id 降序），只含本人订单 |
| 错误码 | `40001`（分页越界、`status` 取值非法） |

#### `GET /api/v1/orders/:id`

本人订单详情（结构同下单返回）。
**他人订单与不存在的订单统一返回 `404 订单不存在`**（不泄露存在性）；`:id` 非正整数 → `40001`。

#### `POST /api/v1/orders/:id/pay`

| 项目 | 说明 |
| --- | --- |
| 请求体 | `channel`（必填：`epay` 在线支付 / `balance` 余额支付）、`pay_type`（可选：`alipay` / `wxpay`，仅 `epay` 用） |
| 成功 | HTTP 200，`data` = `{order, pay}` |

在线支付 `{"channel":"epay","pay_type":"alipay"}`：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "order": { "...同下单返回，status 仍为 pending..." },
    "pay": {
      "channel": "epay",
      "pay_type": "alipay",
      "channel_trade_no": "2026100822001",
      "payurl": "https://pay.example.com/pay/xxx"
    }
  }
}
```

余额支付 `{"channel":"balance"}`：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "order": { "...status: paid, pay_channel: balance, pay_time: ..." },
    "pay": { "channel": "balance", "paid": true, "balance_after": "50.00" }
  }
}
```

| 场景 | code / HTTP | message |
| --- | --- | --- |
| `:id` 非正整数 | `40001` / 400 | `订单 ID 必须为正整数` |
| 订单不存在 / 非本人 | `404` / 404 | `订单不存在` |
| `channel` 取值非法 | `40002` / 400 | `channel 只能是 epay（在线支付）或 balance（余额支付）` |
| 订单已入账（`paid` 及交付态 `provisioning` / `active` / `failed`）/ `cancelled` | `40002` / 400 | `订单已支付，无法发起支付` / `订单正在交付中，无法发起支付` / `订单已交付，无法发起支付` / `订单交付失败，无法发起支付` / `订单已取消，无法发起支付` |
| 渠道未启用或配置不完整 | `40002` / 400 | `支付渠道未配置或未启用（请联系管理员在后台设置中填写并启用）` |
| 渠道不支持该 `pay_type` | `40002` / 400 | `支付方式不受支持: epay 支持 alipay / wxpay` |
| 渠道拒绝 / 响应异常 / 网络失败 | `50002` / 500 | `支付渠道下单失败：<渠道 msg 摘要>`（不含商户密钥） |
| 余额不足 | `40002` / 400 | `余额不足，请先充值或改用在线支付` |

余额支付为**本地单事务**：余额扣款 → 写流水（`order_pay`，金额为负）→ 订单转 `paid` → 优惠码条件自增
（余额不足时不产生任何写入）。**重复发起支付**：`pending` 订单可多次发起，`out_trade_no` 固定复用订单号。

#### `POST /api/v1/orders/:id/cancel`

| 项目 | 说明 |
| --- | --- |
| 请求体 | 无 |
| 成功 | HTTP 200，`data` 为更新后的订单对象（`status=cancelled`） |
| 错误码 | `40001`（ID 非法）、`404`（不存在/非本人）、`40002`（`订单已支付，无法取消` / `订单已取消，无法取消`） |
| 说明 | 仅本人 + 仅 `pending`；取消**不涉及优惠码回退**（下单不计数，见 12.5）；取消后再收到回调只告警、不改状态（12.2.3 第 7 条） |

#### `POST /api/v1/recharges`

| 项目 | 说明 |
| --- | --- |
| 请求体 | `amount`（必填，1.00 ~ 50000.00，两位小数）、`channel`（必填，本批仅 `epay`）、`pay_type`（可选） |
| 成功 | HTTP 200，`data` = `{recharge, pay}` |

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "recharge": {
      "id": 1, "trade_no": "R20261008143500M3P8QT", "member_id": 1, "amount": "100.00",
      "channel": "epay", "status": "pending", "channel_trade_no": null,
      "created_at": "2026-10-08T14:35:00Z", "paid_at": null, "expires_at": null
    },
    "pay": {
      "channel": "epay", "pay_type": "alipay",
      "channel_trade_no": "2026100822002", "payurl": "https://pay.example.com/pay/yyy"
    }
  }
}
```

| 场景 | code / HTTP | message |
| --- | --- | --- |
| `amount` 写法非法（非十进制 / 超两位小数 / 超上限） | `40001` / 400 | `amount 金额格式不正确（需为非负十进制数，最多两位小数）："..."` |
| `amount` 越界 | `40002` / 400 | `amount 需在 1.00 ~ 50000.00 之间（两位小数），收到 "..."` |
| `channel` 非 `epay` | `40002` / 400 | `channel 只能是 epay（本批仅支持易支付）` |
| 渠道未配置 / 渠道故障 | `40002` / `50002` | 同订单支付 |

**渠道下单失败时充值单保持 `pending`**（不回滚、不删除）：单号唯一且回调按 `paid` 幂等，因此
①若渠道其实已建单，后续回调仍能正确入账；②若渠道未建单，该单不会被自动关闭（12.10），用户重新发起
充值即可（生成新单号）。本批**不提供**「继续支付既有充值单」的接口。

#### `GET /api/v1/recharges`

本人充值单分页（新建在前）：查询参数 `page` / `page_size` / `status`（`pending` / `paid` / `closed`）；
`data` = `{items, page, page_size, total}`，`items` 元素为充值单对象（结构同创建返回的 `recharge`）。

#### `GET /api/v1/finance/balance`

实时读库返回当前余额（不取鉴权中间件里的快照）：

```json
{ "code": 0, "message": "ok", "data": { "member_id": 1, "balance": "50.00" } }
```

#### `GET /api/v1/finance/ledger`

本人余额流水分页（新建在前）：查询参数 `page` / `page_size` / `type`（可选：`recharge` / `order_pay` /
`refund` / `adjust`）；`data` = `{items, page, page_size, total}`。

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "items": [{
      "id": 2, "member_id": 1, "type": "order_pay", "amount": "-200.00",
      "balance_before": "250.00", "balance_after": "50.00",
      "ref_type": "order", "ref_id": 1, "note": "订单支付 O20261008143015K7Q2ZP",
      "created_at": "2026-10-08T14:40:00Z"
    }],
    "page": 1, "page_size": 20, "total": 1
  }
}
```

### 12.5 优惠码应用口径（兑现阶段 3b 遗留）

| 环节 | 行为 |
| --- | --- |
| 下单 | 复用 11.2 的判定顺序与折扣计算（percent 四舍五入到分、fixed 封顶售价）；不通过 → **统一 40002 + 明确 message**；通过 → 折扣并入 `final_amount` 并快照 `coupon_id` / `coupon_code` / `discount_amount` |
| 支付成功（在线回调 / 余额支付） | **同一事务内**对 `used_count` 做原子条件自增：`UPDATE coupons SET used_count = used_count + 1 WHERE id = ? AND (max_uses = 0 OR used_count < max_uses)`；影响行数为 0（极端并发超用）时**只记 WARN、不阻断入账**（订单照常转 `paid`） |
| 下单未支付 / 已取消 | **不占用次数**（只有支付成功才计数），因此取消订单无需回退 `used_count` |

口径说明：

1. `used_count` 是**全站总次数**（所有会员共用），单笔订单只计 1 次（本批 `qty` 固定 1）。
2. 优惠码大小写不敏感（沿用 3b 的列级排序规则）；订单快照保存库内原文。
3. 已支付订单的金额不可变：优惠码后续被停用/删除不影响历史订单（订单只存快照）。
4. **极端并发超用**是已知边界：两个并发回调都读到 `used_count < max_uses` 时，条件更新保证只有一个
   成功自增，另一个「超用但已付款」——按契约照常交付，只记 WARN（不退款、不阻断）。

### 12.6 管理端接口（订单查看与对账）

**订单列表 / 详情**（阶段 8b 新增）为**查看类**接口：`admin` / `finance` / `support` **均可读**
（客服协助会员查询是日常）；**重试交付仍仅 `admin`**（契约 14.4）。
**充值单 / 流水**两个只读列表用于财务对账：`admin` 与 `finance` 可查，`support` 返回 `403`。

#### `GET /api/v1/admin/orders`（阶段 8b）

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理端 token（admin / finance / support 均可） |
| 查询参数 | `page`（缺省 1）、`page_size`（缺省 20，1-100）、`status`（可选：6 态枚举）、`type`（可选：`new` / `renew`）、`member_id`（可选，本地会员 ID）、`trade_no`（可选，**模糊匹配**：不区分大小写的包含匹配，与会员/工单搜索口径一致） |
| 成功 | HTTP 200，`data` = `{items, page, page_size, total}`（新建在前 = id 降序，**含全站数据**） |
| 错误码 | `40001`（分页越界、`status` / `type` 取值非法、`member_id` 非正整数）、`401` |

`items` 元素 = **会员端订单对象（12.4，字段完全相同）** + `member`（会员概要）：

```json
{
  "items": [{
    "id": 103, "trade_no": "O20261008141000DEMO7B", "member_id": 11,
    "product_id": 7, "product_name": "香港二区 CN2 A型", "cycle": "monthly", "qty": 1,
    "config": {"11": "111"}, "amount": "22.00", "discount_amount": "0.00", "final_amount": "22.00",
    "coupon_code": "", "status": "active", "type": "renew", "pay_channel": "balance",
    "channel_trade_no": "", "pay_time": "2026-10-08T06:10:40Z", "host_id": 40011,
    "instance_id": 101, "provision_error": "", "delivered_at": "2026-10-08T06:10:45Z",
    "created_at": "2026-10-08T06:10:35Z", "updated_at": "2026-10-08T06:10:45Z",
    "member": {"id": 11, "username": "demo7a", "nickname": "demo7a",
               "email": "demo7a@example.com", "status": "active"}
  }],
  "page": 1, "page_size": 20, "total": 12
}
```

`member` 是**会员概要**（身份类字段：`id` / `username` / `nickname` / `email` / `status`，
不含余额等与订单无关的信息）；会员行缺失（异常数据）时输出 `null`，订单其余字段照常返回。
会员概要按当页订单的会员 ID **一次批量查出**（不产生 N+1）。

#### `GET /api/v1/admin/orders/:id`（阶段 8b）

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理端 token（admin / finance / support 均可） |
| 成功 | HTTP 200，`data` = **单个订单对象**（结构同列表元素：订单全字段 + `member`），含交付信息 `host_id` / `provision_error` / `delivered_at` 与关联实例 `instance_id` |
| 错误码 | `40001`（`:id` 非正整数）、`401`、`404 订单不存在`（管理端**不按归属**过滤：不存在即 404） |

#### `GET /api/v1/admin/recharges`

| 项目 | 说明 |
| --- | --- |
| 查询参数 | `page` / `page_size`、`member_id`（可选，本地会员 ID）、`status`（可选） |
| 成功 | HTTP 200，`data` = `{items, page, page_size, total}`（新建在前，含全站数据） |
| 错误码 | `40001`（分页越界、`member_id` 非正整数、`status` 非法）、`403`（support） |

#### `GET /api/v1/admin/ledger`

| 项目 | 说明 |
| --- | --- |
| 查询参数 | `page` / `page_size`、`member_id`（可选）、`type`（可选：4 种取值） |
| 成功 | HTTP 200，`data` = `{items, page, page_size, total}` |
| 错误码 | `40001`（分页越界、`member_id` 非正整数、`type` 非法）、`403`（support） |

### 12.7 角色权限矩阵（支付与财务部分）

| 接口 | admin | finance | support | 会员 |
| --- | --- | --- | --- | --- |
| `GET/PUT /api/v1/admin/settings/payment/epay` | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `GET/PUT /api/v1/admin/settings/upstream` | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `GET /api/v1/admin/orders`、`GET /api/v1/admin/orders/:id`（阶段 8b，查看类） | ✓ | ✓ | ✓ | ✗（`401`） |
| `GET /api/v1/admin/categories`（R7，分类树只读） | ✓ | ✓ | ✓ | ✗（`401`） |
| `PUT /api/v1/admin/categories/:id`（R7，分类管理：改名 / 子分类改挂主分类，**仅 admin**） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `GET /api/v1/admin/recharges`、`GET /api/v1/admin/ledger` | ✓ | ✓ | ✗（`403`） | ✗（`401`） |
| `GET /api/v1/admin/instances`（阶段 5a）、`GET /api/v1/admin/instances/:id`（阶段 8 新增） | ✓ | ✓ | ✓ | ✗（`401`） |
| `POST /api/v1/admin/orders/:id/retry-delivery`（阶段 5a，仅 admin） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `POST/GET /api/v1/orders`、`GET /api/v1/orders/:id`、`POST /api/v1/orders/:id/pay`、`POST /api/v1/orders/:id/cancel` | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `GET /api/v1/instances`、`GET /api/v1/instances/:id`（阶段 5a） | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `POST /api/v1/admin/instances/:id/suspend`、`/unsuspend`（阶段 5b，仅 admin） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `POST /api/v1/admin/instances/:id/sync`、`GET /api/v1/admin/instances/:id/logs`（阶段 5b） | ✓ | ✓ | ✓ | ✗（`401`） |
| `POST /api/v1/instances/:id/power`、`/reinstall`、`/reset-password`、`/renew`、`GET /api/v1/instances/:id/reinstall-options`、`/logs`（阶段 5b） | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `POST/GET /api/v1/recharges`、`GET /api/v1/finance/balance`、`GET /api/v1/finance/ledger` | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `POST\|GET /api/v1/payments/epay/notify`、`GET /api/v1/payments/epay/return` | 公开（无鉴权，验签是凭证） | 公开 | 公开 | 公开 |
| `POST/GET /api/v1/tickets`、`GET /api/v1/tickets/:id`、`POST /api/v1/tickets/:id/reply`、`/close`（阶段 6a） | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `GET/POST /api/v1/admin/tickets`、`GET /api/v1/admin/tickets/:id`、`POST /api/v1/admin/tickets/:id/reply`、`/close`（阶段 6a，客服域） | ✓ | ✓ | ✗（`403`） | ✗（`401`） |
| `GET/PUT /api/v1/admin/settings/email/smtp`、`POST /api/v1/admin/settings/email/test`、`GET/PUT /api/v1/admin/settings/notifications`（阶段 6b，仅 admin） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `GET/POST /api/v1/notifications`、`/unread-count`、`/:id/read`、`/read-all`（阶段 6b） | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `GET/POST /api/v1/admin/notifications`、`/unread-count`、`/:id/read`、`/read-all`（阶段 6b，个人收件箱） | ✓ | ✓ | ✓ | ✗（`401`） |
| `GET/PUT /api/v1/admin/settings/referral`（R10-A，邀请返佣设置，仅 admin） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `GET/PUT /api/v1/referral`、`GET /api/v1/referral/rebates`、`GET /api/v1/referral/invitees`（R10-A，仅本人） | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `POST/GET /api/v1/referral/coupons`、`PUT /api/v1/referral/coupons/:id`（R10-B，邀请人自建券，仅本人） | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `POST/GET /api/v1/referral/withdrawals`、`POST/GET /api/v1/referral/conversions`（R10-B，提现与转余额申请，仅本人） | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `GET /api/v1/admin/referral/withdrawals`、`GET /api/v1/admin/referral/conversions`（R10-B，查看类，财务域口径） | ✓ | ✓ | ✗（`403`） | ✗（`401`） |
| `POST /api/v1/admin/referral/withdrawals/:id/review`、`/:id/mark-paid`、`POST /api/v1/admin/referral/conversions/:id/review`（R10-B，**仅 admin**） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `PUT /api/v1/admin/members/:id`（R10-B，会员级特批比例，与 `members.status` 同口径） | ✓ | ✓ | ✗（`403`） | ✗（`401`） |

### 12.8 错误码汇总（阶段 4 新增场景）

| code | 含义与典型场景 |
| --- | --- |
| `40001` | 参数错误：分页越界、`status`/`type` 取值非法、`member_id` 非正整数、金额写法非法、URL 非法、设置请求体为空、订单/商品 ID 非法、`config` 键值类型不符 |
| `40002` | 参数校验失败：周期不可售、未知配置项/取值、优惠码不存在或无效、订单状态不允许支付/取消/**重试交付**、余额不足、渠道未启用或配置不完整、`pay_type` 不受支持、`enabled=true` 缺必填设置项、超时越界 |
| `401` | 未携带/无效 token（会员接口用管理员 token 访问同样 401） |
| `403` | 角色不足（设置接口非 admin；财务对账接口非 admin/finance；**重试交付接口非 admin；实例 suspend / unsuspend 非 admin（阶段 5b）**） |
| `404` | 商品不存在或已下架、订单不存在或非本人（会员端；管理端订单详情只看 ID，不存在即 `订单不存在`，阶段 8b）、**实例不存在或非本人**、优惠码不存在（校验接口） |
| `409` | （阶段 4 未新用） |
| `500` | 库内设置值损坏等内部错误 |
| `50001` | 本地库读写失败 |
| `50002` | 支付渠道调用失败（渠道拒绝下单、响应异常、网络失败）；message 为脱敏后的渠道提示 |
| `50003` | **上游（IDC 面板）调用失败（阶段 5b）**：实例操作/重装/改密/暂停/恢复/同步/续费回读等场景；message 为已脱敏的上游原因（不含密码与密钥） |

### 12.9 业务边界（本批实现约定）

1. **下单与支付入账只操作本地库；交付在入账事务提交后触发**（阶段 5a 起）：在线回调与余额支付
   在应答前不调用上游，上游开通在**提交后**异步执行（时序见 14.3），回调应答不被上游耗时拖慢。
2. **库存不做强校验**：导入库存是上游快照（`stock_qty`），本批下单不校验库存、不预占；
   防超卖由**交付环节**（上游开通接口）最终保证。
3. **数量固定 1**：`qty` 恒为 1，请求体不接受 `qty`。
4. **时间**：`pay_time` / `paid_at` 取服务端处理回调的时间（UTC），渠道通知不含可靠支付时间；
   `delivered_at` 取交付落库时间（UTC）。
5. **幂等锚点**：订单/充值单的 `status` 转换在行级锁（`SELECT … FOR UPDATE`）下进行，
   重复回调、并发回调都不会重复入账或重复计数；订单的「已入账」集合为
   `paid` / `provisioning` / `active` / `failed`（`model.IsOrderSettled`），这些状态下重复回调
   一律幂等返回，**不会重复触发交付**（14.1 / 14.3）。
6. **未配置即明确报错**：渠道未配置时支付类接口返回 `40002` 并给出可操作提示；
   上游未配置时探活返回 `connected=false`、导入返回明确错误；**其余功能不受影响**。

### 12.10 暂不支持的能力（留后续批次）

1. **自动超时关闭**：`pending` 订单/充值单不会自动过期关闭（`orders` 无过期列，`recharges.expires_at`
   恒为 null）；用户需手动取消订单。
2. **主动查单补偿**：不主动向渠道查询订单状态，只依赖异步通知（渠道不回调时需人工对账）。
3. **退款流程**：没有退款接口，流水类型 `refund` / `adjust` 为预留；`paid` 订单无终态出口。
4. **其他支付渠道**：仅易支付（支付宝/微信两种 `pay_type`）；渠道抽象已就位（12.1 的扩展方式），
   后续新增只需加 Provider + 设置键 + 路由。
5. **易支付真机联调**：本批以 mock 网关完成全链路验证，真机联调与差异记录另行安排（12.2.6）。
6. **充值单继续支付**：不提供「对既有 `pending` 充值单再次下单」的接口（失败时重新创建）。
7. **订单交付的后续能力**：上游开通与主机绑定已在阶段 5a 落地（第 14 节）；
   续费、开关机/重装/暂停、到期处理与上游主机状态同步留**阶段 5b**（instances 已预留状态枚举与字段）。
8. **前台支付页**：本批返回渠道 `payurl` 与二维码等字段，前端展示页面由后续前端批次实现。

## 13. 站点安装向导（阶段 4+）

本节描述：安装状态机（13.1）、安装模式下的请求分发与页面（13.2）、安装 API（13.3）、
配置文件写入规范（13.4）、并发与一次性保护（13.5）、免重启热切换（13.6）、
安全边界（13.7）、错误码（13.8）、边界与暂不支持（13.9）。

> **动因**：代理商部署时**不允许改任何配置文件**——首次访问用浏览器完成：
> 数据库信息 → 初始化建表 → 管理员账号 → 站点信息，装完即用（阶段 4 已把「用户可设置项」
> 全部做进后台设置，本批补齐「部署级参数由安装向导代写」）。

### 13.1 安装状态机

判定的输入全部来自**实际环境**（配置文件、数据库连通性、核心表、`installed` 标记、`admins` 表内容），
不依赖任何本地标志文件，因此换机器、换进程、重启后结论一致。判定顺序与结论：

| # | 场景 | 判定条件 | 结论 |
| --- | --- | --- | --- |
| 1 | 无数据库 DSN | 未找到配置文件，**或**配置文件未显式提供非空 `database.dsn`（`config.DatabaseConfigured=false`） | **安装模式**，从第 1 步起 |
| 2 | 库不可达 | DSN 已配置，但连接/Ping 失败 | **安装/修复模式**：页面提示连接失败，允许修改数据库参数重试；进程照常启动，`/api/v1/health` 报 `db=down`；从第 2 步起 |
| 3 | 表缺失 | 库可达，但核心表（`settings`、`admins`）不全 | **安装模式**，从第 3 步（初始化建表）起 |
| 4 | 无管理员 | 库可达、核心表齐、`settings` 无 `installed` 标记，且**没有安装者管理员**（`admins` 为空，或仅剩未修改的默认管理员） | **安装模式**，从第 4 步（管理员账号）起 |
| 5 | 存量库 | 库可达、核心表齐、无 `installed` 标记，但 `admins` 非空 | **自动补写 `installed` 标记**（`source=auto`）并进入正常模式，同时写日志——避免打断既有开发/升级库 |

补充规则：

1. **场景 4 的“安装者管理员”判据**：`admins` 表中存在密码哈希**不等于**迁移 0003 默认哈希的行
   （默认哈希见 13.3 的默认管理员策略）。按契约字面语义，场景 5 的判据取「`admins` 表非空」，
   即：迁移 0003 写入的默认管理员同样算「存量部署的既成事实」，不会把既有部署打回安装模式。
2. **`install.progress` 进度标记优先于场景 5**：向导改动过数据库（建表 / 建管理员 / 写站点信息）后
   会写入 `settings.install.progress`，**存在该键时不执行场景 5 的自动补标记**——否则刚建完表的库
   （表齐、只有默认管理员、无标记）会被误判为存量库、向导提前关闭并把默认管理员留在库里。
   该标记在安装完成时删除。
3. **续装定位（向导任意步刷新 / 进程重启后的恢复）**：无 `installed` 标记且不属于场景 5 时，
   按「安装者管理员 → 站点信息 → 完成」逐级定位续装点，共 5 个状态：

   | 状态 | 判据 | 标签 | first_step |
   | --- | --- | --- | --- |
   | `admin_missing` | 不存在安装者管理员（`admins` 为空，或**全部**是未修改的默认管理员） | 等待创建管理员账号 | 4 |
   | `site_missing` | 已存在安装者管理员（`total - unmodified > 0`，与 `progress.admin_ready` 同口径），但 `settings.site` 不存在 | 等待站点信息 | 5 |
   | `pending` | 安装者管理员与 `settings.site` 都已就绪 | 等待完成安装 | 6 |

   因此：建完管理员后刷新 → 停在第 5 步；存完站点后刷新 → 停在第 6 步；
   中途重启进程（库内留有 `install.progress`）→ 启动探测**落在正确的续装步骤**（不会一律退回第 4 步，
   也不会被场景 5 误判为存量库）。
4. 场景 5 的补标记写入失败**不阻断**服务：照常进入正常模式，仅记录告警。
5. `installed` 标记的值结构：`{"at":"<RFC3339 UTC>","version":1,"source":"wizard|auto"}`；
   标记存在但 JSON 内容损坏时同样按「已安装」处理（存在即已安装，避免内容问题把线上服务打回安装模式）。

### 13.2 安装模式下的请求分发与页面

`install.Supervisor` 是 HTTP 入口，按状态分发（契约级行为，与实现解耦）：

| 请求 | 未安装（安装模式） | 已安装 |
| --- | --- | --- |
| `/install`、`/install/**` | 安装向导页 / 安装 API | 「系统已安装」提示页 / 全部安装 API 返回 `50302` |
| `/api/v1/health` | 正常返回（库可达 `200 db=up`；不可达 `503 db=down`，格式同第 5 节） | 同左 |
| 其它 `/api/**` | `503` + 统一包 `code=50301`，`message="系统尚未安装"` | 正常业务接口 |
| 其它路径（浏览器导航，`Accept` 含 `text/html`） | `302` → `/install` | 正常业务（前端/静态资源） |

**页面**：单页多步骤向导（6 步：环境检查 / 数据库 / 初始化 / 管理员 / 站点 / 完成），
HTML/CSS/JS 全部由后端 `go:embed` 内嵌，**不依赖前端构建产物**（安装时前端尚未部署）。
每步一个安装 API，页面据 `GET /install/api/status` 的 `state` + `progress` 决定停留步骤。

**刷新与重启后的恢复表现**（保证向导可续跑，不会回退或错位）：

| 现场 | 刷新页面 / 重启进程后的落点 |
| --- | --- |
| 未保存数据库参数 | 第 1 步（状态 `unconfigured`，库不可达时 `db_unreachable` → 第 2 步） |
| 已建表、未建管理员 | 第 4 步（`admin_missing`） |
| 已建管理员、未填站点 | 第 5 步（`site_missing`） |
| 已填站点、未完成 | 第 6 步（`pending`） |
| 已完成 | 向导已关闭（`installed`，页面为重访提示页） |

页面状态提示条（`state_label` + `detail`）与步骤条同源于 status，因此**每一步之后都立即反映实况**；
第 2 步填写的数据库参数只存在进程内存，重启后需重填（库内进度仍可定位到正确的续装步骤）。

**落位规则（两条路径一致）**：页面每次拿到 status 都按 `state`+`progress` 计算应处步骤
（`first_step` 下限、`progress` 上推），**刷新页面**与**第 2 步提交成功之后**都走同一规则——
因此重启续装在「保存并继续」之后直接落到第 5/6 步（而不是固定跳第 3 步）；
全新流程仍是第 3 步。完成页摘要一律取自 status 的 `admin_username`/`site_name`（库内实况）。

**已安装后的关闭规则**：安装完成（`installed` 标记写入 + 配置文件写入成功）后，
`/install` **永久关闭**：重访返回「系统已安装」提示页（HTTP 200，纯提示，不含任何安装表单），
`/install/api/*` 一律返回 `50302`。**不提供重新安装入口**：重装需先清空数据库（删库或删表）后重启服务。
若向导中途停止（进程重启），库内仍有 `install.progress`，服务按 13.1 判定为安装模式、向导可继续走完。

### 13.3 安装 API

安装 API **不挂会员/管理员鉴权**（安装时尚不存在任何账号），仅**安装模式下可用**：
已安装时统一返回 `50302`；未安装时返回 `50301` 的只有业务接口，安装 API 不受影响。
响应统一使用第 2 节的响应包；下文只列 `data`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/install` | 安装向导页（已安装时为「系统已安装」提示页） |
| GET | `/install/api/status` | 当前状态与进度（安装页每次加载与每步之后调用） |
| GET | `/install/api/environment` | 环境检查与运行信息 |
| POST | `/install/api/database/test` | 测试数据库连接（可顺带自动建库） |
| POST | `/install/api/database` | 保存数据库参数到**安装会话**并复验连接 |
| POST | `/install/api/initialize` | 执行内嵌迁移建表（可重复执行） |
| POST | `/install/api/admin` | 创建安装者管理员账号 |
| POST | `/install/api/site` | 写入站点信息（`settings.site`） |
| POST | `/install/api/complete` | 生成 JWT 密钥 → 写配置文件 → 写标记 → 热切换 |

#### 13.3.1 `GET /install/api/status`

```json
{
  "code": 0, "message": "ok",
  "data": {
    "installed": false,
    "state": "tables_missing",
    "state_label": "数据库尚未初始化",
    "detail": "数据库可达但缺少核心表：settings、admins",
    "first_step": 3, "step_count": 6,
    "config_path": "D:/app/config.yaml", "config_exists": true, "config_source": "config.yaml",
    "server_addr": "127.0.0.1:8080",
    "database": {"configured": true, "reachable": true, "host": "127.0.0.1", "port": 3306,
                  "username": "root", "database": "lyidc", "server_version": "5.7.44", "table_count": 0},
    "progress": {"database_ready": true, "tables_ready": false, "admin_ready": false,
                  "site_ready": false, "completed": false},
    "installed_at": "", "admin_console": "/api/v1/admin/auth/login",
    "admin_username": "", "site_name": ""
  }
}
```

`state` 取值（7 个）：`unconfigured`（1）/ `db_unreachable`（2）/ `tables_missing`（3）/
`admin_missing`（4）/ `site_missing`（5）/ `pending`（6）/ `installed`（0，向导已关闭）；
括号内为对应的 `first_step`，语义与判据见 13.1。
`progress` 是各步骤完成情况的布尔量，与 `state` 互为印证（`progress.admin_ready` 与
`state=site_missing|pending`、`progress.site_ready` 与 `state=pending` 的判据同口径）；
页面**同时**使用两者定位步骤，因此刷新后不会回退。

`admin_username` / `site_name` 是**库内实际已就绪内容**的展示字段（完成页摘要的数据源）：

| 字段 | 取值 | 语义 |
| --- | --- | --- |
| `admin_username` | 密码哈希不等于默认哈希的安装者管理员用户名；不存在时为空串 | 完成页显示「管理员账号」；为空才显示「未创建」 |
| `site_name` | `settings.site.name`；键不存在或内容损坏时为空串 | 完成页显示「站点名称」；为空才显示「未填写」 |

两者都来自数据库实时读取，**不使用页面表单的瞬时值**——向导中途刷新或重启进程后表单为空，
若依赖表单会把已就绪的管理员/站点误显示为「未创建/未填写」（与状态条的「已就绪」自相矛盾）。

**`database` 不回显密码，也不回显完整 DSN**（13.7）。

#### 13.3.2 `GET /install/api/environment`

```json
{
  "code": 0, "message": "ok",
  "data": {
    "ok": true,
    "checks": [
      {"key": "config_write", "name": "配置写入点可写", "ok": true,
       "detail": "安装完成后将把数据库连接与 JWT 密钥写入：D:/app/config.yaml", "advice": ""},
      {"key": "migrations", "name": "迁移资源完整", "ok": true,
       "detail": "共 6 个版本：0001_create_system_settings、…、0006_create_finance_and_orders", "advice": ""},
      {"key": "runtime", "name": "运行环境", "ok": true, "detail": "go1.25.11 windows/amd64，PID 10588，…", "advice": ""}
    ],
    "runtime": {"go_version": "go1.25.11", "os": "windows", "arch": "amd64", "pid": 10588,
                 "working_dir": "D:/app", "config_path": "D:/app/config.yaml", "config_source": "config.yaml",
                 "server_addr": "127.0.0.1:8080", "state": "unconfigured",
                 "migrations": [{"version": 1, "name": "0001_create_system_settings"}]}
  }
}
```

检查项失败时 `ok=false` 且 `advice` 给出**具体处置建议**（如目录不可写时的授权方式）。

#### 13.3.3 `POST /install/api/database/test` 与 `POST /install/api/database`

请求体（两者相同）：

| 字段 | 类型 | 必填 | 校验 |
| --- | --- | --- | --- |
| `host` | string | 是 | 非空，不含空格与 `/` `\`（主机名或 IP） |
| `port` | int | 否 | 缺省 3306，取值 1-65535 |
| `username` | string | 是 | 非空 |
| `password` | string | 否 | 允许为空（MySQL 账号可无密码） |
| `database` | string | 是 | `^[A-Za-z0-9_]{1,64}$`（避免拼接建库语句时出现注入面） |
| `create_database` | bool | 否 | 缺省 false；库不存在（MySQL 1049）时为 true 则先按 `utf8mb4/utf8mb4_general_ci` 建库再连 |

`test` 只探测；`database` 在探测成功后把 DSN 存入**安装会话（进程内存）**，参数在「完成」时才写配置文件（13.4）。

```json
{"connected": true, "host": "127.0.0.1", "port": 3306, "username": "root", "database": "lyidc",
 "server_version": "5.7.44", "database_created": true, "table_count": 0}
```

`database` 的响应额外带 `state` / `first_step` / `installed`：
若新参数指向的库**已安装**（修复模式下的典型结果），`installed=true`，服务**当场热切换为正常模式**
（13.6），向导随之关闭。

失败返回 `503` + `code=50303`，`data.advice` 给处置建议，`data.need_create_database` 标记「库不存在且未勾选自动建库」。
失败原因按 MySQL 错误码翻译：`1045` 账号/密码错误、`1049` 库不存在、`1130` 拒绝本机 IP、`1044` 对该库无权限、
网络层错误（dial/超时）提示「网络不通、端口错误或 MySQL 未启动」。

#### 13.3.4 `POST /install/api/initialize`

无请求体（可传 `{}`）。用**内嵌迁移文件**（`go:embed`，0001-0006，与 `cmd/migrate` 同一套，不复制 SQL）建表：

```json
{"from_version": 0, "to_version": 6,
 "applied": [{"version": 1, "name": "0001_create_system_settings"}],
 "no_change": false, "table_count": 11, "state": "admin_missing", "first_step": 4}
```

可重复执行：已是最新版本时 `no_change=true` 且 `applied` 为空。迁移表为 dirty 时返回 `500` + `50001`
并提示按迁移文件手工修复。前置条件：已完成第 2 步（否则 `40002`）；库不可达 `50303`。

#### 13.3.5 `POST /install/api/admin`

| 字段 | 类型 | 必填 | 校验 |
| --- | --- | --- | --- |
| `username` | string | 是 | `^[A-Za-z0-9_]{3,32}$`（与账号体系一致，见 6.1） |
| `password` | string | 是 | 8-72 字节（bcrypt 上限）；**不得为默认密码 `admin123456`** |
| `confirm_password` | string | 是 | 必须与 `password` 相同 |
| `nickname` | string | 否 | ≤32 个字符（按 rune 计），缺省等于 `username` |

```json
{"admin_id": 1, "username": "opsadmin", "created": false, "updated": false,
 "replaced_default_admin": true, "state": "admin_missing", "first_step": 4}
```

- `created`：本次**新建**了管理员行（库内原本没有默认管理员行）；
- `updated`：本次**改写**了本安装会话已建立的账号（重复执行第 4 步的幂等结果）；
- `replaced_default_admin`：改写了迁移 0003 写的默认管理员行（保留其主键 id）。

**默认管理员替换策略（本批定稿）**：

1. 迁移 0003 写入 `admin / admin123456`，其 bcrypt 哈希是「未修改的默认管理员」的唯一判据；
2. 第 4 步在一个事务内：取出哈希等于默认哈希的行（`SELECT … FOR UPDATE`）→ 校验目标用户名未被**其它**管理员占用
   （占用返回 `409`）→ **复用该行**改写为安装者账号（用户名/密码/昵称/角色 `admin`/状态 `active`，
   `last_login_at` 清空）→ 删除其余默认哈希行（极端情况下的重复行）；
3. 安装完成后**库内不得残留哈希等于默认哈希的行**；请求使用 `admin` + `admin123456` 组合会被直接拒绝；
4. 前端文案与完成页均提示「默认管理员已被替换」。

#### 13.3.6 `POST /install/api/site`

| 字段 | 类型 | 必填 | 校验 |
| --- | --- | --- | --- |
| `name` | string | 是 | 1-64 个字符（按 rune 计） |
| `url` | string | 否 | 留空合法；非空须为带主机名的 `http(s)://`（写入时去掉结尾 `/`），供后续生成 `notify_url` 等推荐地址 |
| `admin_email` | string | 否 | 留空合法；非空须为邮箱格式（阶段 6 邮件通知预留） |

写入 `settings.site` = `{"name":…,"url":…,"admin_email":…}`（`updated_by` 记安装者管理员 ID，缺失时记 NULL），
同时更新 `install.progress`。格式问题返回 `40001`，规则问题（名称空/超长）返回 `40002`。

#### 13.3.7 `POST /install/api/complete`

无请求体。前置条件：库可达且表齐、存在安装者管理员（`admins` 非空且不全是默认哈希行）、`site` 已配置；
不满足分别返回 `40002`（引导回对应步骤）。执行顺序**固定**：

1. 生成 32 字节随机 JWT 密钥（base64url，43 字符，`crypto/rand`）；
2. **写配置文件**（合并写入，规范见 13.4）；
3. 写 `installed` 标记（`settings`，条件插入，见 13.5）；
4. 删除 `install.progress`（best effort）；
5. **热切换**：进程内换用新 JWT 密钥与新的数据库句柄重建正常模式引擎，无需重启（13.6）。

```json
{"installed": true, "config_path": "D:/app/config.yaml", "config_written": true,
 "jwt_secret_written": true, "marker_written": true, "restart_required": false,
 "site": {"name": "我的云主机", "url": "https://cloud.example.com", "admin_email": "ops@example.com"},
 "admin_console": "/api/v1/admin/auth/login",
 "admin_console_hint": "前端页面尚未部署：可用 curl 或浏览器插件调用 … 获取管理员 token，随后即可访问 /api/v1/admin/** 接口。",
 "installed_at": "2026-10-08T10:01:45Z"}
```

**响应只回「已写入」状态**：不回显 JWT 密钥、数据库密码或完整 DSN（13.7）。
写配置文件失败返回 `500`（`data.advice` 提示目录权限或 `-config` 改路径），此时**不写标记**、状态不变，可重试。
写标记失败（键已存在）返回 `50302`：说明已有一次安装生效（并发或重复请求），配置文件已写但内容一致，无副作用。

### 13.4 配置文件写入规范

| 项目 | 规范 |
| --- | --- |
| 生效路径 | 优先**当前已加载路径**（`-config` 指定的 > `LYIDC_CONFIG` > 默认查找命中的那个，即 `cfg.SourcePath`）；不存在时用**运行目录下的 `./config.yaml`** |
| 合并语义 | 文件已存在时**保留全部既有键、注释与书写顺序**，只更新 `database.dsn` 与 `jwt.secret`，并补齐缺失的缺省节/键（`server.addr/mode`、`database.max_open_conns/max_idle_conns/conn_max_lifetime`、`jwt.expire_hours`、`log.level/format`）；文件不存在时按缺省结构新建并写说明性文件头注释 |
| 书写风格 | 2 空格缩进；`dsn` / `secret` 用双引号（与 `config.example.yaml` 一致）；键顺序保持既有文件的顺序（新增节追加在末尾） |
| 权限与原子性 | 同目录临时文件写出后**原子替换**，权限 `0600`（含数据库密码与密钥；Windows 上仅表达只读位语义） |
| 内容范围 | **只写部署级参数**（`database.dsn`、`jwt.secret`）；其余「用户可设置」的内容仍走 `settings` 表（12.1） |
| 失败处置 | 目录不可写/文件只读时返回 `500` 并在环境检查与错误 `advice` 中给出处置建议；既有文件 YAML 语法错误时**拒绝覆盖**并提示先修正或删除该文件 |
| 生效方式 | 写盘后进程内立即生效（热切换）；下次启动由加载链读取同一文件，前后一致 |

### 13.5 并发与一次性保护

| 层次 | 机制 |
| --- | --- |
| 进程内 | 安装步骤在 Supervisor 的同一把写锁内执行：并发安装请求**串行化**；后到者会重新判定状态，已安装则返回 `50302` |
| 跨进程 | `installed` 标记用**条件插入**（`INSERT … ON DUPLICATE KEY UPDATE key=key`，受影响行数 0 表示键已存在）：并发/重复安装中只有一次返回成功 |
| 请求级 | 第 4 步在同一会话内重复执行是**幂等更新**（`updated=true`）；第 3 步重复执行 `no_change=true`；第 5 步是 upsert |
| 中途重启 | 库内 `install.progress` 使向导可续跑，且不会被场景 5 误判为存量库（13.1 补充规则 2） |

### 13.6 免重启热切换（本批取舍）

**结论：采用进程内热切换，无需重启。** `install.Supervisor` 持有当前数据库句柄、JWT 配置与正常模式引擎：

1. 安装模式下 `BuildEngine` 尚未构建（不注册业务路由）；
2. 「完成安装」时用**新的 JWT 密钥**与**安装会话里的数据库句柄**构建正常模式引擎（`router.New`），
   在同一把写锁内替换掉引擎引用与状态，随后所有请求交由新引擎处理；
3. 数据库句柄由 Supervisor 统一管理：DSN 变化时退役旧句柄（退出时统一关闭），健康检查与业务接口用同一句柄；
4. 风险与代价：切换瞬间的并发请求要么落在旧引擎（无 DB 或旧密钥）要么落在新引擎，二者都不会返回错误数据；
   已签发的 token 在密钥更换后失效（安装场景本就是全新部署，无有效会话）。**故不保留「提示重启」降级路径**，
   但保留 `restart_required` 字段（恒为 `false`）以便后续如需降级时前端无需改动。

### 13.7 安全边界

| 风险 | 本批口径与缓解 |
| --- | --- |
| 安装期无鉴权（行业惯例：安装时尚无账号可用） | ①仅在**安装模式**可达（装完即关，`/install/**` 与全部安装 API 永久失效）；②安装模式要求「配置文件无 DSN / 库不可达 / 未建表 / 无管理员」四种状态之一，**已安装的系统不会进入安装模式**；③所有安装步骤在写锁内串行，`installed` 标记条件插入保证只有一次安装生效；④请求日志记录 `client_ip` 与状态码便于审计 |
| 安装页被未授权者抢先访问 | 部署后应**立即**完成安装并访问一次 `/install` 确认已关闭；生产环境建议安装期间只对可信网络开放端口（部署注意事项，非接口能力） |
| 密钥（数据库密码、JWT 密钥）外泄 | ①只进**配置文件与进程内存**；②接口响应只回「已写入」状态，`status`/`database` 视图**不含密码与完整 DSN**；③日志只记 `host/port/database`，错误文本经密码抹除（`MaskDSN`/`sanitizeDBError`）后再输出；④测试与演练包含「响应/日志不含密钥」的断言 |
| 默认管理员残留 | 见 13.3.5 的替换策略：安装完成后库内不得存在默认哈希行，且 `admin/admin123456` 被列为禁用组合 |
| 安装接口被用于探测数据库 | 连接测试只回「是否连通」与 MySQL 版本/表数量，不回显凭据；库名有严格字符集限制，建库语句使用反引号包裹 |

### 13.8 错误码汇总（阶段 4+ 新增）

| code | HTTP | 含义 | 出现场景 |
| --- | --- | --- | --- |
| `50301` | 503 | 系统尚未安装 | 未安装时访问业务接口（`/api/**`，health 除外） |
| `50302` | 503 | 系统已安装，安装向导已关闭 | 已安装时访问 `/install/api/**`、重复提交「完成安装」 |
| `50303` | 503 | 数据库连接失败 | 安装向导测试连接/保存参数失败（`data.advice` 给处置建议） |

其余沿用既有错误码：参数格式 `40001`、规则校验 `40002`、用户名冲突 `409`、数据库故障 `50001`、内部错误 `500`。

### 13.9 边界与暂不支持

1. **不提供重新安装/卸载**：装完后无接口可回到安装模式（重装需清空数据库后重启）。
2. **不校验配置文件之外的部署要素**：如防火墙、Nginx、域名解析、HTTPS 证书、时区/磁盘空间（环境检查只覆盖配置写入点、内嵌迁移资源与运行信息）。
3. **不写 `server.addr` 之外的部署参数**：监听端口沿用既有配置或缺省（首次部署如需改端口，仍是启动参数/配置文件层面的事）。
4. **安装会话不持久化**：第 2 步填写的数据库参数在进程内存中，进程重启后需重填（库内进度标记只用于状态判定）。
5. **安装完成不发送通知**：邮件/Webhook 通知留后续阶段（`site.admin_email` 已预留）。
6. **配置文件语法错误时服务不启动**：加载链仍然报错退出（既有行为），无法通过浏览器修复——此时需人工修正或删除该文件。
7. **前端页面由后续前端批次实现**：本批完成页只给纯文本说明（`admin_console` + `admin_console_hint`），实际管理后台页面在前端构建产物部署后可用。

## 14. 订单交付与自动开通（阶段 5a）

本节描述：订单状态机扩展（14.1）、实例表（14.2）、自动交付时序与幂等/失败/重试（14.3）、
接口契约与角色矩阵（14.4）。数据结构由**迁移 0007** 引入（orders ALTER + instances 建表）。

> 交付链路 = 支付成功 →（自动触发 / 管理员重试）→ **上游开通（`CreateHost`）** → 实例落库 →
> 会员/管理端可查。续费、服务操作（开关机/重装/暂停）、到期处理与上游主机状态同步留**阶段 5b**
> （instances 的状态枚举与上游同步字段已预留，本批不产生非 `active` 状态）。

### 14.1 订单状态机扩展（迁移 0007）

`orders.status` 扩为 6 态（迁移 0007 ALTER ENUM；新增列 `host_id` / `provision_error` /
`delivered_at` 见 12.3）：

```
pending ──支付成功（在线回调 / 余额支付）──▶ paid
   │                                          │
   │  本人取消（仅 pending 可取消）             │ 交付认领（自动触发；失败后可由管理员重试）
   ▼                                          ▼
cancelled                                provisioning ──上游开通成功──▶ active
                                              │
                                              └──上游开通失败──▶ failed
                                                                   │
                                          管理员重试：failed → provisioning → …
```

| 流转 | 触发 | 说明 |
| --- | --- | --- |
| `pending → paid` | 在线支付回调 / 余额支付 | 单事务入账（第 12 节口径不变） |
| `pending → cancelled` | 本人取消 | 仅 `pending`；`paid` 及之后不可取消 |
| `paid → provisioning` | 交付认领（`BeginDelivery`） | 行锁 + 状态条件；认领即清空 `provision_error` |
| `provisioning → active` | 上游开通成功 | 写 `host_id` / `delivered_at`，插入实例（同一事务） |
| `provisioning → failed` | 上游开通失败 | 写 `provision_error`（脱敏，按字符截断 500） |
| `failed → provisioning` | 管理员重试交付 | `POST /api/v1/admin/orders/:id/retry-delivery`（仅 admin） |

**幂等锚点（入账）**：`paid` / `provisioning` / `active` / `failed` 都视为「已入账」
（`model.IsOrderSettled`）。这些状态下重复回调 / 重复余额支付一律按 `OutcomeAlreadyPaid` 幂等返回：
不重复入账、不重复计优惠码、**不重复触发交付**；其余非 `pending` 状态（如 `cancelled`）
返回 `OutcomeSkipped`（不处理，回调仍按成功应答）。

**对既有接口的影响**：订单视图新增 `host_id` / `provision_error` / `delivered_at`；
订单列表 `status` 过滤支持 6 态；发起支付/取消的状态提示按 6 态给出（12.4）。

**阶段 5b 扩展**：`paid → provisioning` 的认领与后续落库**按订单 `type` 分流**——
`new` 走上游开通（`CreateHost`，本节口径不变），`renew` 走上游续费（`RenewHost`，
落库为更新实例到期时间而非新建实例，见 15.5）；两条分支共用同一 6 态状态机、
同一认领行锁幂等锚点与同一管理员重试入口。

### 14.2 instances 表（迁移 0007）

订单交付成功后的主机记录；订单 ↔ 实例**本期一对一**（唯一键 `uk_instances_order`），
上游主机 ID 全局唯一（唯一键 `uk_instances_host`）。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int | 本地实例 ID（对外主键） |
| `member_id` / `order_id` | int | 所属会员 / 来源订单（订单快照） |
| `host_id` | int | 上游主机 ID（`hostinfo` 的 `hosts[].id`）。**R16 起该列是带符号 `INT`**：交付在途的实例用**负数占位**（`-订单号`，见 22.2），交付成功后改写为真实上游主机 ID（恒为正） |
| `product_id` / `product_name` | int / string | 本地商品 ID 与商品名快照（订单快照） |
| `name` | string | 主机名（开通时提交上游的 `host`：`oem-` + 订单号小写） |
| `billing_cycle` | string | 计费周期（本地 6 周期之一） |
| `next_due_date` | string \| null | 到期时间（UTC；开通后从上游 `nextduedate` 回读，回读失败留 `null`） |
| `status` | string | `provisioning` / `active` / `suspended` / `cancelled` / `terminated`；5a 只写 `active`，**5b 起 `suspended` 由管理端暂停与到期扫描写入**，**5c 起 `terminated` 由终止收敛写入**；`cancelled` 经 5c 定稿为**不再写入的预留枚举**（申请在途以 `cancel_status=pending` 表达，见 15.8.1，操作矩阵见 15.1）；**R16 起 `provisioning` 是「交付在途」的内部状态**（迁移 0018 扩枚举，见 22.2） |
| `downstream_token` | string | **R16 新增（迁移 0018）**：上游回推鉴权串（32 位十六进制，开通时生成后不再变更），见 22.2 |
| `upstream_status` | string | 上游 `domainstatus` 原文（如 `Active`；同步字段） |
| `dedicated_ip` | string | 上游主 IPv4（未回读为空串） |
| `assigned_ips` | string | 上游附加 IP（库内逗号分隔原文；接口输出为数组） |
| `port` | int | 上游端口（`0` 表示未返回） |
| `username` / `password` | string | 主机账号 / 密码（**敏感字段**，规则见下） |
| `created_at` / `updated_at` | string | RFC3339（UTC）；`created_at` 为交付完成时刻 |

**敏感字段可见性规则**：`username` / `password`（及 `port` / `assigned_ips`）**仅会员本人**
在实例详情接口可见（等价于「在面板查看主机密码」）；实例列表（会员端与管理端）一律不含；
**任何日志不写密码**（交付日志只记订单号与主机 ID）；管理端本期不提供实例详情接口。

**R16 边界（`provisioning` 行不可见）**：交付在途的实例（`status=provisioning`）是上游开通**之前**
落库的内部行，**一律不出现在 `GET /api/v1/instances` 与 `GET /api/v1/admin/instances` 里**
（`ListInstances` 统一排除）；列表/详情的 `status` 查询参数取值也与 14.4 完全一致
（仍只有 `active` / `suspended` / `cancelled` / `terminated`，不接受 `provisioning`）。
其余接口对该状态一律拒绝操作（见 15.1 操作矩阵：仅 `active` 可操作、仅 `active`/`suspended` 可续费/可申请终止）。

### 14.3 自动交付时序与幂等、失败、重试

**时序（在线回调 / 余额支付两支共用）**：

```
① 入账事务（本地库）：订单 → paid（+ 支付字段 / 优惠码计数）      ← 回调应答前完成
② 事务提交后触发交付（Trigger）：不阻塞回调应答（默认异步 goroutine）
③ 认领（事务）：行锁订单 + 状态条件 → provisioning               ← 幂等锚点
④ 预落实例：插入/复用该订单的实例行（status=provisioning、host_id 负数占位、
   downstream_token 生成一次后不再变更）                        ← R16：拿到 instances.id 供上游回推
⑤ 上游开通：CreateHost（clear → add_to_shop → settle → apply_credit，四步都带下游地址三参数）
⑥ 回读：hostinfo 取 nextduedate / domainstatus / IP / 端口 / 账号密码
⑦ 落库（事务）：更新该实例行 → active（真实 host_id / 同步字段）+ 订单 → active（host_id / delivered_at）
```

**触发点（三处）**：在线支付回调入账成功（`OutcomeApplied`）；余额支付成功（`OutcomeApplied`）；
管理员重试交付（同步执行）。**认领失败（重复触发 / 状态不允许）直接返回现状订单，不重复开通。**

**开通参数拼装**（对齐 8.3 的开通参数表）：

| 参数 | 取值 |
| --- | --- |
| `pid` | `products.upstream_pid`（订单快照商品） |
| `billingcycle` | `pricing.UpstreamCycle(orders.cycle)`（本地 `semiannual`/`annual`/… → 上游 `semiannually`/`annually`/…；`monthly` / `quarterly` 同名） |
| `host` | `oem-` + 订单号小写（如 `oem-o20261008143015k7q2zp`，可回溯订单） |
| `password` | 自动生成 16 位随机密码（大写/小写/数字/特殊四类字符齐备，`crypto/rand`） |
| `configoption[<配置项 id>]` | `orders.config_json` 快照原样（键为配置项 id、值为所选值 id；数量型配置的值为数量，见 10.3 第 6 条 / 12.3 / 14.5 第 1 条） |
| `qty` | `orders.qty`（本批恒 1） |
| `currencyid` | 由上游 `/cart/clear` 的 `user.currency` 决定（客户端自动处理） |
| `downstream_url` / `downstream_token` / `downstream_id` | **R16 新增**：本系统的回推地址（站点对外地址）、该实例的 32 位鉴权串、`instances.id`。四步（`clear` / `add_to_shop` / `settle` / `apply_credit`）都带；任一为空则不提交该参数（见 22.2） |

**幂等与防并发**：同一订单同一时刻只有一个交付在执行——`BeginDelivery` 在事务内行锁订单，
仅 `paid`（管理员重试时 `failed` 也可）可转 `provisioning`；重复回调、重复触发、并发重试
都会拿到「不可认领」的现状订单而直接返回。

**超时**：单次交付整体上限 120s（覆盖多次上游调用）；交付结果落库用独立 10s 超时
（上游已开通时即使主超时耗尽也必须落库）。

**失败与重试**：

1. 认领后任何失败（上游未配置/未启用、上游报错、配置快照损坏、生成密码失败等）→ 订单 `failed` +
   `provision_error`（脱敏、截断 500 字符）；管理员修复后在重试接口触发 `failed → provisioning → …`。
2. **回读失败不算交付失败**：主机已开通且上游可能已扣费，若 `hostinfo` 回读失败，
   实例照常落库（`host_id` 已确定，同步字段留空），**避免重试造成重复开通扣费**。
3. **已知边界**：进程在 `provisioning` 时崩溃会留下「交付中」悬挂订单（本批无自动恢复，
   留人工核对/后续批次加对账）；上游已开通但落库失败时订单停在 `provisioning` 并记 ERROR 日志
   （宁可留痕人工核对，也不静默覆盖状态）。
4. **R16 补充**：交付失败时**预落实例行保留**（`status=provisioning`，不出现在任何列表里），
   管理员重试会**复用同一行**（同一个 `instances.id` 与 `downstream_token`，不重新生成）——
   上游那边登记的下游地址三参数因此保持有效，不会出现「换号后旧推送打空」。

**可测性设计**：交付能力以 `router.DeliveryTrigger` 接口注入（`Trigger` 自动触发 / `Deliver` 同步执行）；
生产默认 `internal/delivery.Service`（自动触发异步），集成测试注入 `Async=false` 的同一实现，
使「回调 → 交付 → 落库」在应答返回前确定完成；另有一条异步路径用例验证「回调先应答、
交付随后完成」的最终一致。

### 14.4 接口契约（实例与重试交付）

#### `GET /api/v1/instances`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token；仅本人 |
| 查询参数 | `page`（缺省 1）、`page_size`（缺省 20，1-100）、`status`（可选：`active` / `suspended` / `cancelled` / `terminated`） |
| 成功 | HTTP 200，`data` = `{items, page, page_size, total}`（新建在前）；`items` 为实例摘要（**不含敏感字段**） |
| 错误码 | `401`、`40001`（分页越界、`status` 非法） |

实例摘要字段：`id` / `order_id` / `host_id` / `product_id` / `product_name` / `name` /
`billing_cycle` / `next_due_date` / `status` / `upstream_status` / `dedicated_ip` / `created_at`。

#### `GET /api/v1/instances/:id`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token；仅本人（他人实例与不存在统一 `404 实例不存在`） |
| 成功 | HTTP 200，`data` = 实例摘要 + `assigned_ips`（数组）/ `port` / `username` / `password` / `updated_at` |
| 错误码 | `401`、`40001`（ID 非法）、`404` |

#### `GET /api/v1/admin/instances`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token（`admin` / `finance` / `support` 均可） |
| 查询参数 | `page` / `page_size`、`member_id`（可选，正整数）、`status`（可选，4 态之一） |
| 成功 | HTTP 200，`data` = `{items, page, page_size, total}`；`items` 比会员端摘要多 `member_id`，**不含敏感字段** |
| 错误码 | `401`、`40001`（分页越界、`member_id` 非正整数、`status` 非法） |

#### `GET /api/v1/admin/instances/:id`（阶段 8 新增）

管理端实例详情：字段与会员端 `GET /instances/:id` **同口径**（摘要 + 敏感字段），额外回带 `member_id`。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token；权限沿用管理端实例**列表**口径（`admin` / `finance` / `support` 均可读） |
| 路径参数 | `id`：实例 ID（正整数） |
| 成功 | HTTP 200，`data` = 实例摘要（`id` / `order_id` / `host_id` / `product_id` / `product_name` / `name` / `billing_cycle` / `next_due_date` / `status` / `upstream_status` / `dedicated_ip` / 取消字段 / `created_at`）+ `member_id` + `assigned_ips`（数组）/ `port` / `username` / `password` / `updated_at` |
| 错误码 | `401`、`40001`（ID 非正整数）、`404`（`实例不存在`） |

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "id": 5,
    "order_id": 12,
    "host_id": 10922,
    "member_id": 9,
    "product_id": 3,
    "product_name": "香港二区 CN2 A型",
    "name": "oem-o20261008105520t0j03j",
    "billing_cycle": "monthly",
    "next_due_date": "2026-11-08T10:55:25Z",
    "status": "active",
    "upstream_status": "Active",
    "dedicated_ip": "203.0.113.9",
    "cancel_status": "none",
    "cancel_type": "",
    "cancel_request_id": 0,
    "cancel_requested_at": null,
    "created_at": "2026-10-08T10:55:40Z",
    "assigned_ips": [],
    "port": 22,
    "username": "root",
    "password": "Abcd1234Efgh5678",
    "updated_at": "2026-10-08T11:00:00Z"
  }
}
```

说明：

1. **管理端详情包含主机凭据**（`username` / `password` / `port` / `assigned_ips`）——这是 5a 起
   「敏感字段仅会员**本人**可见、不进列表与日志」规则的**唯一放宽点**：列表仍不含敏感字段，
   详情供具有管理端 token 的运维/客服排障使用（接口不回显到任何日志）。若后续需要按角色再收紧
   （如仅 `admin` 可见 `password`），属于新契约变更。
2. 实例不存在与 ID 非法分别返回 `404` / `40001`，与会员端同口径；管理员 token 访问会员端接口、
   会员 token 访问本接口均为 `401`。

#### `POST /api/v1/admin/orders/:id/retry-delivery`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，**仅 `admin` 角色**（`finance` / `support` 返回 `403`） |
| 请求体 | 无 |
| 成功 | HTTP 200，`data` 为**最新订单对象**：交付成功 `status=active` + `host_id`；交付执行失败 `status=failed` + `provision_error`（仍按 200 返回，便于管理员据状态处置或再次重试） |
| 错误码 | `401`、`403`、`40001`（ID 非法）、`404`（订单不存在）、`40002`（订单尚未支付 / 正在交付中 / 已交付完成 / 已取消）、`50001` |
| 说明 | **同步**执行一次完整交付（认领 → 上游开通 → 回读 → 落库）；`paid`（未触发过）与 `failed` 可重试；`provisioning` / `active` / `pending` / `cancelled` 不可重试 |

> 管理端**订单列表 / 详情**（阶段 8b 补齐：`GET /admin/orders`、`GET /admin/orders/:id`，
> 查看类接口三角色均可读）契约见 12.6；本节的 `retry-delivery` 仍是**唯一**会真实调用上游的
> 管理端订单接口（仅 `admin`）。

### 14.5 真机实测与差异记录（2026-10-08，生产上游）

本批对生产上游完成真机全链路演练（测试会员下单 20.00 → mock 渠道支付入账 → 自动交付 →
上游回读核对 → 提交终止申请），差异与新发现如下（不回写 8.5 / 10.6 的历史实测条目）：

1. **直连下单的 `configoption` 键值必须用上游本地 id（本批修正口径）**：
   上游直连下单接口（`app/home/controller/CartController.php`）以
   **配置项 id（`product_config_options.id`）为键、选项值 id（`product_config_options_sub.id`）为值**
   取配置（`filterConfigOptions` 与 `addToShop` 两处均为 `where id = <键>` 查询）；
   `upstream_id` 是上游**作为下游代理**时的映射字段（`app/common/logic/Host.php` 才用它，
   且过滤 `upstream_id > 0`）。生产数据佐证：阶段 3a 导入的真实商品配置项 `upstream_id` **全为 0**，
   阶段 4 真机订单快照即出现 `{"0":"0"}`；对上游 `add_to_shop` 实测：传入键 `86`（配置项 id）
   或键 `0`（upstream_id），上游都返回「添加成功」——**未识别的键被静默忽略**
   （用户选择丢失且不报错）。
   处置：下单 `config` 与订单 `config_json` 口径修正为 `{"<配置项 id>": "<所选值 id>"}`
   （12.3 / 12.4 已同步，会员端视图本就下发 `options[].id` / `values[].id`，无前端改动）；
   交付按同口径拼 `configoption`；`upstream_id` 继续透传仅作展示。
2. **开通成功的回读对账（逐项一致）**：本次开通主机（`host_id=10922`）上游回读与本地实例一致——
   `domain` = 提交的 `oem-<订单号小写>`、`domainstatus=Active`、`nextduedate`（unix 秒
   `1794135325`）与 `instances.next_due_date`（`2026-11-08T10:55:25Z`）完全相等、
   `dedicatedip` / `billingcycle` / `username=root` 一致；上游账单 `amount` / `firstpaymentamount`
   均为 20.00（真实扣费）。
3. **`hostinfo` 回读字段实测**：`port=0`、`assignedips=[""]`（含空项，接口输出已过滤为空数组）、
   `password` 回传（与开通提交的 16 位密码一致）；`host_option_config`（`all=1`）为空。
4. **「带配置项开通」真机复核已由阶段 5b 补做（15.7 第 1 条）**：本批（5a）演练时未做行为级验证，
   仅以集成测试覆盖参数拼装（断言 `configoption[<配置项 id>]` 与 `cart_data[configoptions][<配置项 id>]`）；
   5b 对同一商品选**非默认**操作系统（`{"87":"278"}` = CentOS-9-Stream-x64）真实开通并回读核对，
   所选值与上游 `host_data.os` 及 `config_options` 完全一致（对照组为上游默认值），口径确认无误。
5. **终止申请（RequestCancel）**：返回 `status=202` + `pending=true` +
   `data.cancel_request_id=432`，主机 `domainstatus` 保持 `Active`（上游异步处理，同 8.5 第 13 条）；
   本地实例状态本批**不同步**（服务操作与状态同步留 5b）。

## 15. 实例操作与续费（阶段 5b）

本节描述：实例状态与操作矩阵（15.1）、会员端操作接口（15.2）、管理端操作接口（15.3）、
操作审计（15.4）、续费链路（15.5）、到期暂停扫描（15.6）、真机实测（15.7）、
**取消/终止流程（15.8，阶段 5c）**。
数据结构由**迁移 0008**（`orders` 扩 `type` / `instance_id` + `instance_operation_logs` 建表）
与**迁移 0009**（`instances` 扩取消申请 5 列，阶段 5c）引入。

> 本批范围：会员端电源/重装/改密、管理端暂停/恢复/同步、续费下单与自动续费、到期暂停扫描。
> 实例申请取消/终止（`RequestCancel`、`terminated` 状态与收敛）由**阶段 5c 补齐，见 15.8**；
> 本节的同步与扫描口径已按 5c 更新（同步含终止收敛、扫描含取消收敛）。

### 15.1 实例状态与操作矩阵

`instances.status` 沿用迁移 0007 的四态枚举（`active` / `suspended` / `cancelled` / `terminated`）；
**本批起产生 `suspended`**（管理端手动暂停与到期未续费自动暂停）；`terminated` 由 5c 收敛写入；
`cancelled` 为 5c 定稿的**预留枚举、不再写入**（「取消申请在途」以 `cancel_status=pending` 表达，
理由见 15.8.1）：

```
active ──管理端暂停 / 到期未续费自动扫描──▶ suspended ──管理端恢复 / 续费成功自动恢复──▶ active
   │                                            │
   └── 会员/管理端申请终止（cancel_status=pending，status 不变）──┴──▶ terminated（上游确认删除后收敛）
```

**状态与操作的可执行矩阵**（定稿；不满足时返回 `40002`，且**失败尝试同样写审计**）：

| 操作 | 入口 | 权限 | 允许的实例状态 | 上游调用 | 本地影响 |
| --- | --- | --- | --- | --- | --- |
| 电源：`soft_on` / `soft_off` / `reboot` / `hard_off` / `hard_reboot` | `POST /instances/:id/power` | 会员本人 | 仅 `active` | `POST /provision/default`（`func=on/off/reboot/hard_off/hard_reboot`） | 无（仅审计） |
| 重装系统 | `POST /instances/:id/reinstall` | 会员本人 | 仅 `active` | `POST /provision/default`（`func=reinstall` + `os`/`port`） | 无（仅审计） |
| 重装可选系统列表 | `GET /instances/:id/reinstall-options` | 会员本人 | 任意 | `GET /host/cloudos` | 无 |
| 重置密码 | `POST /instances/:id/reset-password` | 会员本人 | 仅 `active` | `POST /provision/default`（`func=crack_pass` + `password`） | 新密码落库到实例记录 |
| 续费下单 | `POST /instances/:id/renew` | 会员本人 | `active` / `suspended` 且**无在途取消申请**（5c） | 无（仅本地建单） | 创建 `type=renew` 的 pending 订单 |
| 申请取消/终止 | `POST /instances/:id/cancel` | 会员本人 | `active` / `suspended`（5c） | `POST /host/cancel`（`Immediate` / `Endofbilling`） | `cancel_status → pending`（`status` 不变）；已有在途申请时幂等返回 |
| 代客终止申请 | `POST /admin/instances/:id/cancel` | **仅 admin** | 同会员端（5c） | 同上 | 同上（审计 `actor=admin`） |
| 暂停 | `POST /admin/instances/:id/suspend` | **仅 admin** | 仅 `active` | `POST /provision/default`（`func=suspend` + `reason`） | `status → suspended` |
| 恢复 | `POST /admin/instances/:id/unsuspend` | **仅 admin** | 仅 `suspended` | `POST /provision/default`（`func=unsuspend`） | `status → active` |
| 同步 | `POST /admin/instances/:id/sync` | admin / finance / support | 任意 | `GET /cart/hostinfo`（all=1）+ `POST /provision/default`（`func=status`） | 回写同步字段；按上游 `domainstatus` 收敛 `active ↔ suspended`；**上游已删除/已终止时收敛 `terminated`**（5c） |
| 操作记录 | `GET /instances/:id/logs`、`GET /admin/instances/:id/logs` | 见 15.4 | 任意 | 无 | 无 |
| 到期暂停（自动） | 应用内后台任务 | system | `active` 且 `next_due_date < now` 且**无在途取消申请**（5c） | `POST /provision/default`（`func=suspend` + 固定原因） | `status → suspended` |
| 终止收敛（自动，5c） | 应用内后台任务 | system | `cancel_status=pending` 且到收敛时机（见 15.8.5） | `GET /cart/hostinfo`（all=1） | 上游已删除/已终止 → `status → terminated` + `cancel_status → done` |

**`terminated` 是终态**（5c）：电源/重装/改密/续费/取消申请/暂停/恢复一律拒绝（`40002`，失败尝试同样写审计）；
后续若需要「重新购买」走新订单（本批不做「恢复已终止实例」）。

**硬操作风险标注**：`hard_off`（强制关机）/ `hard_reboot`（强制重启）等价于直接断电/复位，
**可能造成主机数据损坏或文件系统异常**，本批**按需开放**（面板类产品的常规能力），
责任由调用方与操作者承担；契约与接口提示文案均标明「强制」语义，前端应做二次确认。

**异步语义**：上游的电源/重装/改密调用是**受理即返回**——接口返回成功只表示「指令已提交」，
实际执行由上游异步完成（实测：`soft_on` 后 `func=status` 短暂返回 `process/开机中`，
重装期间为 `process/重装中`；改密完成前上游会以 `406 重置密码中不能执行该操作`
拒绝其它模块操作，见 15.7）。需要确认最终状态时用管理端同步接口回读。

**密码口径（重置密码）**：
- 请求体 `password` 省略或空串 → 服务端生成 **16 位强密码**（与开通密码同规则：大小写/数字/特殊四类齐备，`crypto/rand`）；
- 传入 `password` → 校验 **8-64 个字符且同时包含字母与数字**，不满足返回 `40002`；
- 新密码**落库到实例记录**（`instances.password`，仅会员本人在实例详情可见），并在本次响应中返回；
- 密码**不写日志、不写审计**（审计 message 经敏感串替换，见 15.4）。

### 15.2 会员端接口契约

全部要求**会员 token**，且只操作**本人实例**；他人实例与不存在的实例统一 `404 实例不存在`（与 14.4 同口径）。

#### `POST /api/v1/instances/:id/power`

| 项目 | 说明 |
| --- | --- |
| 请求体 | `op`（必填）：`soft_on` / `soft_off` / `reboot` / `hard_off` / `hard_reboot` |
| 成功 | HTTP 200，`data` = `{instance_id, action, message, status}`；`action` 为审计动作（`power_on` / `power_off` / `reboot` / `hard_off` / `hard_reboot`） |
| 错误码 | `401`、`40001`（op 非法/ID 非法）、`404`、`40002`（实例非 active）、`50003`（上游失败） |

#### `GET /api/v1/instances/:id/reinstall-options`

| 项目 | 说明 |
| --- | --- |
| 成功 | HTTP 200，`data` = `{instance_id, os: [{id, name, group}], groups: [{id, name}]}`；`os[].id` 即重装接口的 `os_id` |
| 错误码 | `401`、`404`、`40002`（商品未配置操作系统项）、`50003`（上游失败） |
| 上游口径 | `GET /host/cloudos?productid=<上游 pid>&os_config_option_id=<os 配置项 id>`；**必须带 `os_config_option_id`**（实测不带该参数上游返回空列表），该项 ID 从商品 `config_json` 的 `config_groups[].options[].option_name`（形如 `os|操作系统`）解析 |

#### `POST /api/v1/instances/:id/reinstall`

| 项目 | 说明 |
| --- | --- |
| 请求体 | `os_id`（必填，正整数，取自重装可选系统列表）、`port`（可选，0-65535，0 = 不指定） |
| 成功 | HTTP 200，`data` = `{instance_id, action:"reinstall", message, status}`；上游异步执行 |
| 错误码 | `401`、`40001`（os_id/port 非法）、`404`、`40002`（实例非 active）、`50003` |

#### `POST /api/v1/instances/:id/reset-password`

| 项目 | 说明 |
| --- | --- |
| 请求体 | `password`（可选；省略即自动生成，口径见 15.1） |
| 成功 | HTTP 200，`data` = `{instance_id, action:"reset_password", message, status, password}`（`password` 为最终生效的新密码，仅本次响应返回） |
| 错误码 | `401`、`404`、`40002`（实例非 active / 密码强度不足）、`50003`（上游失败） |

#### `POST /api/v1/instances/:id/renew`

| 项目 | 说明 |
| --- | --- |
| 请求体 | `cycle`（必填，6 周期之一）；**本批不支持优惠码**（无 `coupon_code` 字段，传入被忽略，订单折扣恒为 `0.00`） |
| 金额 | 按**当前商品**该周期售价计算（R7 起 = **基础价 + Σ 配置差价**，见 10.2.1）：基础价取商品当前本地售价，配置差价按**该实例开通时的配置快照**（原订单 `config_json`）重算，与新购同公式；商品下架仍可续费（已购实例不受上架状态影响），该周期无售价返回 `40002`。实例原配置缺失或已无对应配置项时，缺失部分差价按 `0.00`（`note:"无差价数据"`），不阻断续费 |
| 明细 | 续费单同样写 `price_breakdown`（`kind`/`qty`/`value_id` 取自实例原配置快照） |
| 成功 | HTTP 200，`data` 为订单对象（`type=renew`、`instance_id` 已填、`status=pending`） |
| 错误码 | `401`、`40001`（cycle 非法/ID 非法）、`404`、`40002`（实例非 active/suspended、商品不存在、周期不可售）、`50001` |
| 后续 | 支付成功（在线/余额）→ 自动续费交付（15.5）；失败可经管理员重试同入口 `POST /admin/orders/:id/retry-delivery` |

#### `GET /api/v1/instances/:id/logs`

| 项目 | 说明 |
| --- | --- |
| 查询参数 | `page`（缺省 1）、`page_size`（缺省 20，1-100） |
| 成功 | HTTP 200，`data` = `{items, page, page_size, total}`（新记录在前）；`items` 见 15.4 |
| 错误码 | `401`、`40001`、`404`（他人实例与不存在统一 404） |

### 15.3 管理端接口契约

#### `POST /api/v1/admin/instances/:id/suspend`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，**仅 `admin` 角色**（`finance` / `support` 返回 `403`） |
| 请求体 | `reason`（**必填**，≤200 字符；同原因提交上游并写入审计） |
| 成功 | HTTP 200，`data` = `{instance_id, action:"suspend", message, status:"suspended"}` |
| 错误码 | `401`、`403`、`40001`（reason 空/过长、ID 非法）、`404`、`40002`（实例非 active）、`50003` |
| 上游 | `func=suspend` + `reason`；本地状态条件更新（`active → suspended`，并发下只有一个生效） |

#### `POST /api/v1/admin/instances/:id/unsuspend`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 同上（仅 `admin`） |
| 请求体 | 无 |
| 成功 | HTTP 200，`data` = `{instance_id, action:"unsuspend", message, status:"active"}` |
| 错误码 | `401`、`403`、`40001`、`404`、`40002`（实例非 suspended）、`50003` |

#### `POST /api/v1/admin/instances/:id/sync`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，**admin / finance / support 均可**（只读回读 + 状态收敛，不影响计费） |
| 请求体 | 无 |
| 成功 | HTTP 200，`data` = `{instance_id, action:"sync", message, status, power_state, power_desc, status_changed, instance, next_due_date, upstream_status}`；`instance` 为同步后的实例摘要 |
| 错误码 | `401`、`40001`、`404`、`50003`（上游回读故障：网络/鉴权/业务拒绝。**主机不存在自 5c 起不再是错误**，而是终止收敛信号，返回 200 + `terminated=true`） |
| 行为 | ① `hostinfo` 回写 `next_due_date` / `upstream_status` / `dedicated_ip` / `assigned_ips` / `port` / 账号密码（回读不到的字段保留原值）；② 按上游 `domainstatus` **收敛本地状态**（`Suspended ↔ Active`，仅在这两态之间）；③ 附带查询电源状态（失败不影响同步成功，如实记录在 `message`）；④ **终止收敛（5c）**：上游主机不存在或 `domainstatus ∈ {Deleted, Terminated}` → 本地 `status=terminated` + `cancel_status=done`（幂等）、写 `cancel_sync` 审计，本次**跳过字段回写与电源查询**，响应 `terminated=true`（见 15.8.3） |

#### `GET /api/v1/admin/instances/:id/logs`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token（所有角色） |
| 分页 | 同会员端 |
| 错误码 | `401`、`40001`、`404` |

### 15.4 操作审计（`instance_operation_logs`，迁移 0008）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int | 主键 |
| `instance_id` | int | 实例 ID（`instances.id`） |
| `actor_type` | string | `member`（会员操作）/ `admin`（管理员操作）/ `system`（自动：开通、续费、到期暂停） |
| `actor_id` | int | 操作者 ID（`member_id` / `admin_id`；`system` 恒为 0） |
| `action` | string | `create` / `power_on` / `power_off` / `reboot` / `hard_off` / `hard_reboot` / `reinstall` / `reset_password` / `suspend` / `unsuspend` / `sync` / `renew` / **`cancel`（提交取消申请，5c）/ `cancel_sync`（上游确认终止后的本地收敛，5c）** |
| `status` | string | `success` / `fail` |
| `message` | string | 结果说明（≤500 字符，**已脱敏：不含密码与密钥**；上游错误原样透传前先做敏感串替换） |
| `created_at` | string | 发生时间（UTC） |

**留痕范围**：所有实例操作**无论成功失败都写一条**——含失败尝试（状态不允许、上游报错、参数不合法）
与系统自动操作（开通交付、续费交付、到期暂停）。审计写入**不参与业务事务**：
写失败只记服务日志，不改变操作结果（操作结果以实例状态与上游回读为准）。

**开通审计**：`create` 在交付成功后按 `host_id` 反查实例写入（`actor=system`）；
交付失败时尚无实例记录，失败原因落在订单 `provision_error`（不在本表）。

接口视图（会员端与管理端一致）：`{id, instance_id, actor_type, actor_id, action, status, message, created_at}`。

### 15.5 续费链路（`orders` 扩展，迁移 0008）

**数据模型扩展**（见 12.3 的 orders 表）：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `type` | string | `new`（新购，默认）/ `renew`（续费） |
| `instance_id` | int \| null | 续费单对应的实例（`renew` 单必填；`new` 单为 `null`，实例由 `instances.order_id` 反向关联） |

**时序（与新购交付同一骨架，契约 14.3 的续费分支）**：

```
① 会员续费下单（POST /instances/:id/renew）→ 订单 pending（type=renew + instance_id）
② 支付入账（在线回调 / 余额支付，链路与 12.2/12.4 完全相同）
③ 事务提交后触发交付（Trigger，异步）→ 认领行锁（paid → provisioning）      ← 幂等锚点
④ 上游续费：RenewHost（POST /host/renew → POST /apply_credit 余额支付）
⑤ 回读：hostinfo（all=1）取 nextduedate / domainstatus / IP / 端口 / 账号密码
⑥ 恢复：① 回读显示上游已是 Active（**实测：上游在续费成功后会自行解除到期暂停**）→
   本地直接收敛为 active，不再调 Unsuspend（此时调上游会以 `不能解除该暂停` 被拒，见 15.7 第 6 条）；
   ② 上游仍 Suspended（或回读失败且本地 suspended）→ 尝试 Unsuspend（失败不阻断成交，本地保持 suspended）
⑦ 落库（事务）：更新实例同步字段与状态 + 订单 → active（host_id / delivered_at）
```

**金额与优惠码**：续费金额取**当前商品**该周期售价（下单时快照）；**续费单不接受优惠码**
（本批口径，请求体无 `coupon_code`，折扣恒 `0.00`）。

**幂等与失败**：
- 认领行锁保证重复回调 / 重复触发**不重复续费**（同一订单同一时刻只有一个交付在跑）；
- 失败（上游拒绝、余额不足、实例/订单不匹配等）→ 订单 `failed` + `provision_error`（脱敏），
  管理员经 `POST /admin/orders/:id/retry-delivery` 重试（与 5a 同入口，按订单 `type` 分流执行续费）；
- **回读失败不算续费失败**（上游已扣费）：到期时间保留原值，审计 message 标注，可用管理端同步接口核对；
- 续费成功但恢复（Unsuspend）失败：**不阻断续费成交**，实例保持 `suspended`，审计记 `unsuspend/fail`，
  待管理员排查或再次恢复；
- 已知边界：进程在 `provisioning` 时崩溃会留下悬挂续费单（与 14.3 第 3 条同一已知边界）。

**视图同步**：订单视图新增 `type` / `instance_id`（12.4）；实例详情/列表的 `next_due_date` 即为下次到期时间（12.4 起已输出）。

### 15.6 到期暂停扫描（应用内后台任务）

| 项目 | 说明 |
| --- | --- |
| 调度 | 服务启动后延迟 **1 分钟**执行首轮，之后每 **24 小时**一轮（常量：`scheduler.DefaultInitialDelay` / `DefaultInterval`）；由 `cmd/server` 在构建路由时启用（`router.Options.EnableDueScan`），单批 50 条循环处理，单轮整体超时 5 分钟 |
| 命中条件 | `instances.status = active` 且 `next_due_date` 非空且 `< 当前时间`（UTC，按 `next_due_date` 升序）；**排除 `cancel_status=pending` 的实例**（5c：它们已进入终止流程，见 15.8.5） |
| 动作 | 调上游 `func=suspend`（原因固定「到期未续费，系统自动暂停」）→ 本地条件更新 `active → suspended` → 审计（`actor=system`、`action=suspend`） |
| 失败重试 | 单实例失败（上游报错等）**不中断本轮**，审计记 `fail`，本地保持 `active`，**下一轮自动重试** |
| 幂等 | 上游返回业务失败时回读一次主机：若 `domainstatus=Suspended`（上游已自行暂停等）则只收敛本地状态，审计记 `success` 并标注「上游已是暂停状态（幂等）」；本地状态条件更新保证并发下只有一个生效 |
| 终止收敛阶段（5c） | 同一轮扫描在到期暂停之后执行：对「取消申请在途且到收敛时机」的实例回读上游，已删除/已终止 → 收敛 `terminated`（`actor=system`、`action=cancel_sync`）；单批 50 条循环处理，统计字段 `cancel_scanned` / `cancel_converged` / `cancel_failed`（完整口径见 15.8.5） |
| 已知边界 | ① 上游自行暂停（非本系统触发）不在扫描范围，由管理端同步接口收敛；② 扫描间隔窗口内到期的实例最迟下一轮被处理（≤24h+1min）；③ 上游不可达时整轮跳过并记 ERROR（不误改本地状态）；④ **不做到期提醒通知**（阶段 6）；⑤ 到期后本地暂停不自动重新计费，续费成功由续费链路自动恢复（15.5）；⑥ 上游自行删除主机（非本系统申请）不在终止收敛范围（收敛只覆盖 `cancel_status=pending`），由管理端同步接口收敛 |

### 15.7 真机实测与差异记录（2026-10-08，生产上游）

本批对生产上游完成真机演练（带配置项开通复核 → 实例操作逐项回读 → 续费 → 终止申请），
与阶段 2/5a 记录不同的新发现如下（不回写 8.5 / 14.5 的历史条目）：

1. **`configoption` 口径行为级复核通过（补 14.5 第 4 条的欠账）**：对商品「美国一区 Kurun A型」
   （本地 id=11 / 上游 pid 15）按新口径 `config={"87":"278"}` 下单（配置项 `87=os|操作系统`，
   值 `278=CentOS-9-Stream-x64`，**非默认值**），支付后自动开通 `host_id=10923`；
   经 `GET /host/details` 回读，主机 `os` 与 `config_options` 中 `os` 项均为
   `CentOS-9-Stream-x64`（与所选一致）；对照组 `host_id=10922`（5a 开通，快照 `{"0":"0"}`
   未识别键）回读为列表首项 `CentOS-7.6.1810-x64`（上游默认）。**结论：键=配置项 id、
   值=所选值 id 的口径在真实开通链路中精确生效。**
2. **`GET /host/details?host_id=<id>` 可作回读来源**（阶段 2 未收录）：机器凭证（API 登录 JWT）
   可访问该前台接口，返回 `host_data`（含 `os` / `suspendreason` / 电源无关字段）与
   `config_options`（配置项当前取值）、`domainstatus_desc` 等，比重放 `hostinfo` 更丰富；
   **本批未用于落库**（契约冻结的同步来源为 `hostinfo`，见 15.3 的 sync），仅作真机核对手段。
3. **`GET /host/cloudos` 必须带 `os_config_option_id`**（补 8.3 的参数口径）：不带该参数时上游返回
   空列表（实测 `cloud_os: []`），带上商品的「操作系统」配置项 id 才返回可选系统（实测 13 个）。
   `cloud_os[].id` 即重装接口 `os` 参数（阶段 2 已真机验证）。
4. **上游电源/重装/改密为异步受理**：`func=status` 在操作进行中返回 `process`（如「开机中」「重装中」）；
   改密完成前上游会以 `406 重置密码中不能执行该操作` 拒绝其它模块操作（含暂停）。
   接入方不得把「发起成功」当作「已完成」，也不需要自动重试同一指令（等上游完成后自然可再操作）。
5. **实例操作、到期扫描、续费与终止的真机结果**（演练机 `host_id=10923` / 实例 2 / 商品 11）：

   | 步骤 | 结果 |
   | --- | --- |
   | 电源 soft_on / soft_off / reboot | 均受理成功；`status` 轮询依次观测到 `process`（开机中/关机中）→ `on` / `off` / `on`，终态与指令一致 |
   | 重置密码（自动生成 16 位） | 上游 `host/details` 回读密码与响应/落库值**逐字一致**；改密完成后一段时间上游以 `406 重置密码中不能执行该操作` 拒绝其它模块操作（约 30 秒内恢复可操作） |
   | 管理端暂停（带原因） | 上游 `domainstatus=Suspended`、`suspendreason` 与提交原因一致；本地 `status=suspended`；审计 `suspend/success/admin` |
   | 管理端同步 | `同步完成：上游状态 Suspended，到期时间 …；电源状态 …`；本地状态按上游收敛（`status_changed` 正确） |
   | 管理端恢复 | 上游回到 `Active`，本地回到 `active`，审计 `unsuspend/success/admin` |
   | **到期暂停扫描** | 将实例 `next_due_date` 置为过去后重启服务：启动 1 分钟后首轮扫描日志 `scanned=1 suspended=1 failed=0`；上游 `domainstatus=Suspended`、`suspendreason=到期未续费，系统自动暂停`；本地 `suspended`；审计 `suspend/success/system` |
   | **续费（suspended 状态）** | 下单 ¥20 → mock 渠道支付入账 → 自动续费交付 2 秒完成；上游 `nextduedate` **+2 592 000 秒（30 天）**、`domainstatus` 自行回到 `Active`；本地 `next_due_date` 与上游一致、`status` 收敛为 `active`；审计 `renew/success/system` + `unsuspend/success/system`（收敛路径） |
   | 重装 | `reinstall-options` 返回 13 个可选系统（上游全局 os id）；选 `Debian-12.0_x64` 发起重装 → 上游受理（当时 `status=process`），`host/details` 回读 `os` 已更新为目标系统 |
   | 终止申请 | `POST /host/cancel` 返回 `status=202` + `pending=true` + `cancel_request_id=433`（上游异步处理，主机暂仍 `Active`；本地状态由 5c 收敛） |

6. **上游续费成功会自行解除到期暂停（本批真机发现并修复的实现缺口）**：首次真机续费时，
   `RenewHost` 成功后上游已把 `domainstatus` 从 `Suspended` 改回 `Active`；
   而实现按「本地 suspended → 调 Unsuspend」的旧分支再次调用上游，被 `400 不能解除该暂停` 拒绝，
   导致审计记 `unsuspend/fail` 且**本地状态滞留在 suspended**（与上游不一致）。
   处置：续费交付的恢复分支改为**先看回读结果**——上游已 `Active` 时本地直接收敛（不再调 Unsuspend），
   上游仍 `Suspended` 时才主动 `Unsuspend`；两条分支均不阻断续费成交（15.5 第 ⑥ 步）。
   修复后重跑了完整真机续费链路（第二次续费 ¥20）验证：本地状态直接收敛为 `active`、
   审计为 `unsuspend/success/system`（「上游已自行解除暂停，本地状态已收敛为 active」），问题不再复现。

### 15.8 取消/终止流程（阶段 5c）

本节描述：状态机与取消标记（15.8.1）、接口契约（15.8.2）、上游口径与收敛规则（15.8.3）、
操作矩阵更新（15.8.4）、扫描收敛（15.8.5）、真机实测与边界（15.8.6）。
数据结构由**迁移 0009** 引入（`instances` 扩 5 列 + 1 索引；`instance_operation_logs.action` 仅注释扩展，
`VARCHAR(32)` 无需 DDL 变更）。

> 上游没有「直接删除主机」的开放接口：终止必须走**申请流程**（`POST /host/cancel`，按上游配置可能需人工审核）。
> 因此本系统的终止 = **申请 → 上游异步处理 → 本地收敛**三步，接口返回「已受理」不等于「已终止」。

#### 15.8.1 状态机与取消标记（定稿）

**取消申请是实例记录上的标记，不引入新的中间状态**：申请在途期间 `instances.status` 保持
`active` / `suspended` 原值不变，终止完成时一次性转 `terminated`。

| 字段（迁移 0009） | 类型 | 说明 |
| --- | --- | --- |
| `cancel_request_id` | int | 上游回带的取消申请 ID（`POST /host/cancel` 的 `cancel_request_id`；上游未回带为 0） |
| `cancel_type` | string | `immediate`（立即取消 → 上游 `Immediate`）/ `end_of_billing`（到期取消 → 上游 `Endofbilling`）；无申请为空串 |
| `cancel_status` | string | `none` 无申请（默认）/ `pending` 申请在途 / `done` 已终止（本地已收敛为 `terminated`） |
| `cancel_reason` | string | 申请原因（会员填写或服务端兜底文案；管理端必填） |
| `cancel_requested_at` | datetime \| null | 申请提交时间（UTC） |

**为什么不用 `cancelled` 状态**（5c 定稿）：`status` 反映主机的**真实可用状态**（上游仍在运行 →
本地仍是 `active`/`suspended`），取消申请是与服务状态**正交**的流程元数据；若把在途申请写成
`cancelled`，就会丢失「主机仍在运行 / 是否已暂停」的信息，且续费、暂停等判定还要叠加第二套状态维度。
迁移 0007 预留的 `cancelled` 枚举值因此**保留但不再写入**（不改已应用迁移，见 0009 注释）。

```
      ┌─ active ─────────┐
      │                  ├─ 申请取消（cancel_status=pending，status 不变）
      └─ suspended ──────┘        │
                                  ├─ 上游仍在运行 → 保持 pending（到期取消可能持续到账单周期结束）
                                  └─ 上游已删除（domainstatus=Deleted / 主机不在列表中）
                                       → status=terminated + cancel_status=done（终态，幂等）
```

#### 15.8.2 接口契约

两接口共用服务层实现（`instanceops.Service.Cancel`），差异只在鉴权、归属校验与原因必填。

| 项目 | 会员端 | 管理端（代客/强制终止） |
| --- | --- | --- |
| 路径 | `POST /api/v1/instances/:id/cancel` | `POST /api/v1/admin/instances/:id/cancel` |
| 鉴权 | 会员 token，**仅本人实例**（他人/不存在统一 `404 实例不存在`） | 管理员 token，**仅 `admin` 角色**（finance / support `403`），不限归属 |
| 请求体 | `type`（必填：`immediate` / `end_of_billing`）、`reason`（**可空**，空时服务端兜底「会员申请终止（未填写原因）」） | 同左，但 `reason` **必填**（≤200 字符） |
| 允许状态 | `active` / `suspended`；`terminated` 返回 `40002`（已有在途申请走幂等分支而非报错） | 同左 |
| 成功 | HTTP 200，`data` = `{instance_id, action:"cancel", message, status, cancel_request_id, cancel_type, cancel_status, cancel_requested_at, duplicate}` | 同左（审计 `actor=admin`） |
| `duplicate` | `true` 表示已有在途申请，**未重复提交上游**（幂等返回现状；同样写审计留痕） | 同左 |
| 错误码 | `401`、`40001`（type 非法/ID 非法/原因过长）、`404`、`40002`（实例非 active/suspended）、`50003`（上游拒绝） | 加 `403`；`40001` 含 reason 为空 |
| 上游 | `POST /host/cancel`（`id` / `type` / `reason`），受理即返回；失败不落本地标记 | 同左 |

**幂等锚点**：本地写入用条件更新（`cancel_status <> 'pending'`）保证同一实例同一时刻只有一个申请在途；
并发重复提交时后者按幂等分支返回（审计注明「并发重复提交」），不会向前台重复发起上游调用。

**实例视图新增字段**（列表与详情一致，管理端同）：`cancel_status` / `cancel_type` / `cancel_request_id` /
`cancel_requested_at`（14.4 与 15.2 的视图同步扩展，`instance` 摘要见 `sync` 响应）。

#### 15.8.3 上游口径与收敛规则

**上游 `POST /host/cancel` 的三种应答（真机实测，见 15.8.6）**：

| 上游应答 | 语义 | 本地处置 |
| --- | --- | --- |
| `status=202` + `data.pending=true` + `cancel_request_id` | 活动主机：终止申请已受理、待上游处理 | `cancel_status=pending`，文案「取消申请已受理，等待上游处理（申请号 N）」 |
| `status=200` + `data.domainstatus=Deleted`（无申请号） | 主机已被上游终止（重复申请/已删除） | `cancel_status=pending`，文案「上游主机已删除，终止即时生效（等待同步收敛）」，随后同步/扫描立即收敛 |
| `status=400/406` 等业务失败 | 上游拒绝（如「产品为已激活或者暂停的产品才能申请取消」） | 不写本地标记，`50003` + 审计 `cancel/fail` |

> 申请 ID 的字段名上游出现过两种（`cancel_request_id`，文档口径 `cancel_id`），且可能位于 `data` 内或顶层，
> 解析按四种组合依次尝试（任一取到即用）。

**收敛判定（`instanceops` 的两个入口共用同一实现）**：

| 上游回读结果 | 本地动作 | 审计 |
| --- | --- | --- |
| 主机**不在**主机列表中（`ErrHostNotFound`：`hostinfo` 按 `hostid[]` 查不到） | `status → terminated` + `cancel_status → done` | `cancel_sync/success` |
| `domainstatus ∈ {Deleted, Terminated}`（主机记录仍在，如真机 10922/10923） | 同上，并把该 `domainstatus` 回写 `upstream_status` | `cancel_sync/success` |
| `domainstatus ∈ {Active, Suspended}` | **不动作**（申请尚未被上游处理） | 不写审计（避免每轮刷屏） |
| 回读故障（网络/鉴权/业务拒绝，**非主机不存在**） | 不动作 | 管理端同步：`sync/fail` + `50003`；扫描：仅服务日志（不写审计） |

**幂等**：收敛用条件更新（`status <> 'terminated'`），已是 `terminated` 时不重复写；
管理端重复同步返回 200 + `terminated=true` + `status_changed=false`，审计文案标注「幂等」。
**误收敛防护**：回读故障与「主机不存在」严格区分（上游业务失败走 `ErrUpstreamFailed`，绝不触发收敛）。

#### 15.8.4 操作矩阵更新

- **`terminated` 为终态**：电源/重装/改密/续费/取消申请/暂停/恢复全部 `40002`（失败尝试写审计）；
  实例列表/详情/操作记录仍可读。
- **续费收紧**：`cancel_status=pending` 时不可续费（`40002`「实例已有在途取消申请，无法续费」）——
  上游可能按申请终止主机，续费会白付；上游的**撤销申请**能力（`DELETE /host/cancel`，参数 `id`）
  已在上游文档中存在但**本批未接入**，留后续批次（见 15.8.6 未做项）。
- **其他操作不受在途申请影响**：申请放弃后主机仍可开关机/重装/改密（上游删除前服务仍可用），
  暂停/恢复也照常（由上游回答可行性），避免「申请即冻结」影响会员取回数据。

#### 15.8.5 扫描收敛（与到期暂停同一轮）

| 项目 | 说明 |
| --- | --- |
| 命中条件 | `cancel_status='pending'` 且 `cancel_requested_at` 非空，且（`cancel_type='immediate'` **或** `cancel_type='end_of_billing'` 且 `next_due_date < now`）——到期取消要等账单周期结束，上游才可能执行终止，提前回读没有意义 |
| 动作 | 回读上游主机（`hostinfo` all=1）→ 已删除/已终止 → 本地 `terminated` + `cancel_status=done` + 审计 `cancel_sync`（`actor=system`） |
| 统计 | `cancel_scanned`（本轮尝试）/ `cancel_converged`（成功收敛）/ `cancel_failed`（回读或落库失败，下轮重试） |
| 到期暂停的配合 | 扫描的到期暂停分支**排除** `cancel_status=pending` 的实例（申请终止中的实例不应被暂停） |
| 失败重试 | 回读故障/落库失败**只记服务日志**，不写 fail 审计（避免每日刷屏），下一轮自动重试 |
| 已知边界 | ① immediate 申请后每轮（≤24h）回读一次，上游处理完的实例最迟下一轮收敛；② 上游长期不处理（如人工审核积压）时实例持续运行，本系统不做二次提醒（运营可用管理端同步核对）；③ 收敛只覆盖本地有在途申请（`pending`）的实例，上游自行删除的由管理端同步收敛；④ 回读必须按 `hostid[]` 精确过滤，上游返回空列表会被判为「主机不存在」（真机两台已删除主机仍在列表中带 `Deleted` 状态返回，见 15.8.6，该分支已有真机证据） |

#### 15.8.6 真机实测与差异记录（2026-10-08，生产上游）

在 5b 遗留的两台真机（`host 10922` / `host 10923`）上完成收敛演练与新口径实测：

| 步骤 | 结果 |
| --- | --- |
| 上游状态回读 | 两台主机均已在**上游终止完成**：`hostinfo` 仍返回记录，`domainstatus=Deleted`（**不是从列表消失**）；`host/cancelpage` 返回 `406 产品为已激活或者暂停的产品才能申请取消` |
| 会员申请（实例 2，local 仍 active / 上游已 Deleted） | `POST /host/cancel` 返回 `status=200` + `data.domainstatus=Deleted`（**无 `cancel_request_id`**，与首次申请 202 口径不同）；本地记 `cancel_status=pending`，文案「上游主机已删除，终止即时生效（等待同步收敛）」 |
| 管理端同步（实例 1 = 10922） | `terminated=true`、`status_changed=true`、`upstream_status=Deleted`、`cancel_status=done`；审计 `cancel_sync/success/admin` + `sync/success/admin` |
| 管理端同步（实例 2 = 10923） | 同上；审计完整链路 `cancel/success/member`（申请）→ `cancel_sync/success/admin`（收敛）→ `sync/success/admin` |
| 终态操作矩阵 | 本人开机 → `40002`「开机仅在 active 状态可用」（审计 `power_on/fail`）；续费 → `40002`；再次申请取消 → `40002`；管理端暂停 → `40002`（均未发起上游调用） |

**与 5b 记录的衔接**：5b 第 5 条表格里的终止申请（`202` + `cancel_request_id=433`）即本表的实例 2；
其上游处理结果就是本次观测到的 `Deleted`（**5b 记录的 `202` 口径与本次的 `200 + Deleted` 口径共同构成
上游两种应答形态**）。两台主机成为 `terminated` 本地终态后，开发库与上游状态一致。

**未做项（留后续批次）**：① 撤销取消申请（上游 `DELETE /host/cancel`，参数 `id`）与「申请后改主意」的续费恢复；
② 终止/退款联动（终止后按剩余周期的退款策略，涉及财务口径）；③ 到期提醒通知（阶段 6）；
④ 管理端实例详情页与前端展示（前端零改动，本批只扩接口字段）。

## 16. 工单系统（阶段 6a）

本节描述：数据模型（16.1）、状态机（16.2）、接口契约与权限矩阵（16.3）、边界与限流（16.4）、
真机实测（16.5）。数据结构由**迁移 0010** 引入（新建 `tickets` 与 `ticket_messages` 两张表，
**不改动 0001–0009 已应用的任何结构**）。

> 本批范围（6a）：会员提单 → 管理员/客服处理 → 关闭的完整闭环，纯本地域、**不调用上游**。
> **通知体系（站内通知 + 邮件 SMTP + 到期提醒）属阶段 6b**：本批只在工单事件处预留日志与挂点，
> 不做任何通知发送。

### 16.1 数据模型（迁移 0010）

#### `tickets`（工单主表）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | bigint | 主键 |
| `trade_no` | varchar(32) | 工单号：`T` + UTC 时间（`yyyyMMddHHmmss`）+ 6 位随机大写字母/数字，如 `T20261008201530K7Q2ZP`（契约 12.2.5 单号规则的第三个前缀，与 `O`/`R` 同口径） |
| `member_id` | bigint | 提单会员 ID（会员端一切读写按该列隔离） |
| `instance_id` | bigint \| null | 关联实例（可选；**必须是该会员自己的实例**，无关联为 NULL） |
| `category` | enum | `technical` 技术 / `billing` 财务 / `other` 其他 |
| `subject` | varchar(100) | 标题（5-100 字符） |
| `status` | enum | `open` / `replied` / `closed`（状态机见 16.2） |
| `last_reply_at` | datetime | 最近一条**消息**的时间（**含管理员内部备注**，UTC）——列表排序与待办定位的锚点 |
| `closed_at` | datetime \| null | 关闭时间（UTC；未关闭为 NULL） |
| `created_at` / `updated_at` | datetime | 创建 / 最后更新时间（UTC） |

索引：`uk_tickets_trade_no`（唯一）、`idx_tickets_member`(`member_id`, `id`)、
`idx_tickets_status`(`status`, `last_reply_at`)、`idx_tickets_instance`(`instance_id`)。

#### `ticket_messages`（工单消息）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | bigint | 主键 |
| `ticket_id` | bigint | 所属工单（`tickets.id`） |
| `author_type` | enum | `member` 会员 / `admin` 管理员（客服同属 admin） |
| `author_id` | bigint | 作者 ID（按 `author_type` 解释为 `members.id` 或 `admins.id`） |
| `content` | text | 正文（1-5000 字符；首尾空白已由服务端裁剪） |
| `internal` | tinyint(1) | 管理员**内部备注**：`1` 仅管理端可见，会员端接口绝不返回 |
| `created_at` | datetime | 发送时间（UTC） |

索引：`idx_ticket_messages_ticket`(`ticket_id`, `id`)。

**首条消息即工单正文**（结构性保证）：创建工单在同一事务内写入 `tickets` 与 `ticket_messages`
首条记录（`author_type=member`、`internal=0`），因此「工单必有至少一条消息、且首条为会员消息」
恒成立，详情接口不需要拼接 `subject`/`content` 之外的伪消息。

### 16.2 状态机（定稿）

```
              会员提单
                 │
                 ▼
        ┌──▶ open（待客服处理）──────────┐
        │         ▲                     │
 会员回复 │         │ 管理员公开回复        │ 关闭（会员 / 管理员）
        │         ▼                     ▼
        └──  replied（待会员）─────▶ closed（终态）
                    关闭（会员 / 管理员）
```

| 事件 | 触发者 | 状态迁移 | `last_reply_at` | 说明 |
| --- | --- | --- | --- | --- |
| 创建工单 | 会员 | （新）→ `open` | = 创建时间 | 同事务写入首条消息 |
| 会员回复 | 会员（本人） | `open` / `replied` → `open` | 推进 | 已关闭 → `40002` |
| 管理员公开回复 | admin / support | `open` / `replied` → `replied` | 推进 | `internal=false`；已关闭 → `40002` |
| 管理员内部备注 | admin / support | **状态不变** | 推进 | `internal=true`（见下方定稿第 2 条） |
| 关闭 | 会员（本人）/ admin / support | `open` / `replied` → `closed` | 不变 | 写入 `closed_at` |
| 重复关闭 | 同上 | `closed` → `closed` | 不变 | **幂等**：200 + `already_closed=true`，`closed_at` 保持首次值 |

定稿要点：

1. **`closed` 是终态**：关闭后任何一方都不能再回复（会员端与管理端一致 `40002`「工单已关闭」）；
   会员如需继续请**新开工单**——本批**不做重开**（与 15.8 的终止幂等口径一致：终态只读）。
2. **内部备注不改变状态**（定稿）：内部备注是客服协作信息，不是「对会员的回复」，因此
   `status` 保持原值（会员视角仍是「等待客服处理」），也不会在会员端产生任何可见消息；
   但它是一次**活动**，所以照常推进 `last_reply_at`（待办排序按最近活动）。
3. **关闭幂等**（定稿）：重复关闭不报错、不覆盖 `closed_at`——便于前端重试与多标签页并发。
4. **`replied` 只由管理员公开回复产生**：会员回复一律把工单推回 `open`，状态是「球在谁手里」的
   唯一表达，不需要额外的已读标记。

### 16.3 接口契约与权限矩阵

#### 视图对象

`ticket` 对象（列表项 / 详情 / 回复与关闭响应共用；时间为 RFC3339 UTC）：

```json
{
  "id": 1,
  "trade_no": "T20261008201530K7Q2ZP",
  "subject": "主机无法连接，请协助排查",
  "category": "technical",
  "status": "open",
  "instance_id": 3,
  "instance": {"id": 3, "name": "oem-o20261008105520t0j03j", "product_name": "香港云服务器", "status": "active"},
  "last_reply_at": "2026-10-08T12:15:30Z",
  "closed_at": null,
  "created_at": "2026-10-08T12:15:30Z",
  "updated_at": "2026-10-08T12:15:30Z"
}
```

- `instance_id` 为 `null` 时 `instance` 也为 `null`（未关联实例）；
- `instance` 是**概要**（`id` / `name` / `product_name` / `status`），不含主机账号密码等敏感字段；
- 管理端视图在此基础上多 `member_id` 与 `member` 概要 `{id, username, nickname}`。

`message` 对象：

```json
{"id": 12, "author_type": "admin", "author_id": 1, "author_name": "cs01",
 "content": "已为您重启主机，请再试。", "internal": false, "created_at": "2026-10-08T12:20:00Z"}
```

- `author_name` 为作者账号名（会员端与管理端一致；账号已删除时回退 `-`）；
- **会员端**的任何响应中 `internal` 恒为 `false`，且 `internal=true` 的消息**一律不返回**。

**详情响应结构（阶段 8 校正）**：`GET /api/v1/tickets/:id`（会员端）与
`GET /api/v1/admin/tickets/:id`（管理端）的成功 `data` 为**嵌套** `{ticket, messages}`，
与 `POST /tickets`、`/reply`、`/close` 的 `{ticket, …}` 口径一致。
（阶段 6a 的实现曾把工单字段**平铺**在 `data` 上（`data.status` / `data.messages`），
阶段 8 已把实现按本契约统一为嵌套结构，会员端与管理端同步收敛；前端类型与测试一并更新。）

#### 会员端接口（仅本人；他人工单与不存在的工单统一 `404 工单不存在`）

| 接口 | 请求体 / 查询参数 | 成功 `data` | 错误码 |
| --- | --- | --- | --- |
| `POST /api/v1/tickets` | `subject`（必填 5-100 字符）、`content`（必填 1-5000 字符）、`category`（必填三值之一）、`instance_id`（可选正整数） | `{ticket, message}`（`status=open`、首条消息为会员消息） | `401`、`40001`（ID / category 非法）、`40002`（标题或内容空白 / 超长 / **未关闭工单已达上限**）、`404`（`instance_id` 非本人实例或不存在） |
| `GET /api/v1/tickets` | `page`（缺省 1）、`page_size`（缺省 20，1-100）、`status`（可选筛选） | `{items, page, page_size, total}`，排序 `last_reply_at DESC, id DESC` | `401`、`40001`（分页越界 / status 非法） |
| `GET /api/v1/tickets/:id` | — | `{ticket, messages}`（**不含内部备注**，按时间正序） | `401`、`40001`、`404` |
| `POST /api/v1/tickets/:id/reply` | `content`（必填 1-5000 字符） | `{ticket, message}`；状态回 `open` | `401`、`40001`、`404`、`40002`（内容非法 / 工单已关闭） |
| `POST /api/v1/tickets/:id/close` | — | `{ticket, already_closed}`（重复关闭 `already_closed=true`） | `401`、`40001`、`404` |

#### 管理端接口（**admin + support 全权；finance 一律 `403`**——工单域为客服域）

| 接口 | 请求体 / 查询参数 | 成功 `data` | 错误码 |
| --- | --- | --- | --- |
| `GET /api/v1/admin/tickets` | `page` / `page_size`、`status`、`category`、`member_id`、`keyword`（匹配**标题或工单号**，LIKE 通配符已转义） | `{items, page, page_size, total}`（含 `member` 概要） | `401`、`403`、`40001` |
| `GET /api/v1/admin/tickets/:id` | — | `{ticket, messages}`（**含内部备注**，`internal` 原样输出；`ticket` 为管理端视图，含 `member_id` 与 `member` 概要） | `401`、`403`、`40001`、`404` |
| `POST /api/v1/admin/tickets/:id/reply` | `content`（必填）、`internal`（可选 bool，缺省 `false`） | `{ticket, message}`；`internal=false` → 状态 `replied`，`internal=true` → 状态不变 | `401`、`403`、`40001`、`404`、`40002`（内容非法 / 工单已关闭） |
| `POST /api/v1/admin/tickets/:id/close` | — | `{ticket, already_closed}` | `401`、`403`、`40001`、`404` |

**权限矩阵**：

| 接口 | admin | support | finance | 会员 |
| --- | --- | --- | --- | --- |
| `/api/v1/tickets` 全部（会员端） | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `/api/v1/admin/tickets` 全部（管理端） | ✓ | ✓ | ✗（`403`） | ✗（`401`） |

> 与既有矩阵的差异：工单域是**唯一让 `support` 拥有写权限**的域（客服就是工单的处理人），
> 而财务（`finance`）在工单域**只读也不允许**——沿用「finance 只看钱、support 只看/处理服务」
> 的既有边界（12.7）。

### 16.4 边界与限流（定稿）

1. **未关闭工单上限 20**：`status ∈ {open, replied}` 的工单数达 20 后，创建返回 `40002`
   「未关闭工单数已达上限 20，请先关闭既有工单」；关闭任一工单后可继续创建。
   计数与插入不在同一把锁内，**并发下允许瞬时超出**（防滥用而非强一致配额，与 12.9 第 2 条的
   「不做强校验」一致）。
2. **字段约束**：`subject` 5-100 字符、`content` 1-5000 字符，均按**裁剪首尾空白后**的 rune 数计
   （多字节中文按字符计）；纯空白一律拒绝；超长拒绝而不是截断（避免静默丢内容）。
3. **错误码口径**：枚举取值非法 / ID 非法 / 分页越界 → `40001`；长度、空白、上限、已关闭回复
   等规则类 → `40002`；`instance_id` 非本人实例或不存在 → `404 实例不存在`（与 14.4 同口径，
   不暴露他人实例的存在性）。
4. **越权隔离**：会员端任何接口都按 `member_id` 过滤，他人工单与不存在工单同为 `404`；
   管理端不限制归属（客服可见全站工单）。
5. **无外部依赖**：工单链路只读写本地库，不调用上游、不发通知；6b 的通知发送在
   `tickets`/`ticket_messages` 写入点挂接（本批在这些位置留有明确的日志与注释锚点）。
6. **不做（留后续批次）**：附件、指派/转派、优先级、SLA 与首响时限、自定义分类、
   内部备注的编辑与删除、关键字搜索消息正文、工单重开、会员端撤回消息。

### 16.5 真机实测（2026-10-08，开发库）

环境：开发库 `lyidc`（迁移 `0009 → 0010`，`version: 10 (dirty=false)`），后端 `127.0.0.1:8080`，
**全流程真实中文内容**（无 mock、无上游调用）。测试数据准备：注册会员 `vfy6a`（id=9）并直接写库
造一台属于该会员的 active 实例（`instance_id=3`，host 30001）；`cs6a` / `fin6a` 两条管理员记录
（support / finance 角色）用于角色矩阵实测。

| 步骤 | 请求 | 结果（证据） |
| --- | --- | --- |
| 提单（关联自己的实例） | `POST /tickets` | 200；工单号 **`T20261008121849W0QLH7`**（前缀 T + UTC 时间 + 6 位随机，长度 21）；`status=open`、`closed_at=null`、`last_reply_at=created_at`；实例概要 `{id:3, name:"oem-ticket-vfy6a", product_name:"香港云服务器-验证用", status:"active"}`；首条消息 `author_type=member / author_id=9 / internal=false` |
| 列表 / 详情 | `GET /tickets?status=open`、`GET /tickets/1` | 200；列表含实例概要；详情消息流 1 条（中文原文完整：`从今天早上 9 点开始 SSH 就一直连不上（超时）…`） |
| 会员回复 | `POST /tickets/1/reply` | 200；状态保持 `open`、消息 #2 落库、`last_reply_at` 由 12:18:50 推进到 12:18:53 |
| admin 内部备注 | `POST /admin/tickets/1/reply {internal:true}` | 200；消息 #3 `internal=true / author=admin(id=1)`，**状态保持 `open`**（定稿第 2 条实测生效） |
| 内部备注不泄漏 | `GET /tickets/1`（会员） | 200；消息流仅 2 条，内部备注内容**未出现**；管理端 `GET /admin/tickets/1` 同时可见 3 条（含 internal=true） |
| support 公开回复 | `POST /admin/tickets/1/reply {internal:false}` | 200；消息 #4 `author=cs6a(id=2)`，**状态 open → replied**（待会员） |
| 会员回复 | `POST /tickets/1/reply` | 200；**状态 replied → open** |
| finance 角色 | 管理端 4 个接口（列表/详情/回复/关闭） | **全部 `403 当前角色无权执行该操作`**（含只读；`requireAdminRole(admin, support)` 实测生效） |
| 关闭（会员） | `POST /tickets/1/close` | 200；`status=closed`、`closed_at=2026-10-08T12:19:14Z`、`already_closed=false` |
| 关闭后回复 | 会员端与管理端各一次 | **均 `40002 工单已关闭，如需继续请新开工单`**（终态只读） |
| 重复关闭 | 再次 `POST /tickets/1/close` | 200 + `already_closed=true`，`closed_at` **仍为 12:19:14Z**（幂等、不覆盖首次值） |
| 未登录 | 会员端与管理端各一次 | **`401 未携带或携带了无效的凭证`** |
| 越权关联 | `POST /tickets {instance_id:1}`（实例 1 属于会员 7） | **`404 实例不存在`**（与 14.4 同口径，不暴露他人实例存在性） |
| 字段校验 | 标题 2 字 / `category=urgent` | `40002 工单规则不成立: 标题需为 5-100 个字符（当前 2）` / `40001 category 只能是 technical / billing / other` |
| 限流 | 连续提单 20 次 → 第 21 次 | 20 次全 200；第 21 次 **`40002 未关闭工单数已达上限 20，请先关闭既有工单`** |
| 关闭后放行 | 关闭 1 单（id=21）后再提单 | 200，新工单 **`T20261008121929YJM2C7`**（id=22）——closed 不计入上限 |
| 库内一致性 | `SELECT … FROM tickets / ticket_messages` | 工单 1：`status=closed`、`closed_at=12:19:14`；消息 #1-#5 与接口时序一致，内部备注 `internal=1` 落库；限流后 `open_tickets=20`、`closed_tickets=2` |
| 服务日志 | `server_6a.log` | 6b 通知挂点日志按事件打印：`工单已创建 … instance_id=3`、`工单收到会员回复 … status=open`、`工单收到管理员回复 … internal=true status=open`、`工单收到管理员回复 … role=support internal=false status=replied`、`工单已关闭 … already_closed=false/true`；访问日志状态码与上表逐条对应（401/403/200/400） |

**结论**：6a 的提单 → 处理 → 关闭闭环、状态机（含内部备注不改状态、关闭幂等）、
内部备注隔离、角色矩阵（support 全权 / finance 全禁）与限流全部在真机按契约生效；
工单域**零上游调用**、零外部依赖。测试数据（会员 9 的 22 张工单与两套实例、`cs6a`/`fin6a` 两个
测试管理员）留在开发库作证据，生产部署前按需清理。

## 17. 通知体系（阶段 6b）

本阶段交付**站内通知 + 邮件 SMTP + 到期提醒 + 事件接线**。三条设计口径：

1. **用户可设置的内容一律进后台设置**（`settings` 表 + 管理端接口）：SMTP 参数（`email.smtp`）与
   通知开关（`notifications`）都在后台面板里改，改完立即生效，不需要重启、不需要编辑配置文件。
2. **通知是旁路能力**：所有事件都在**业务事务提交之后**触发通知，生产实现内部异步执行
   （独立超时 + panic 恢复），站内通知写库失败、邮件发送失败都**只记日志**，绝不影响业务结果。
3. **不泄漏敏感内容**：通知正文只出现单号、商品名、实例名、到期时间与已脱敏的失败原因；
   不含主机密码、IP、SMTP 口令。

### 17.1 数据模型（迁移 0011）

迁移 0011 **只新建两张表 + 给 `instances` 扩一列**，不改动 0001–0010 已应用的任何结构。

**`notifications`（站内通知收件箱）**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | bigint | 主键 |
| `recipient_type` | enum(`member`,`admin`) | 接收方类型：会员 / 管理员（客服同属 admin） |
| `recipient_id` | bigint | 接收方 ID（按类型解释为 `members.id` 或 `admins.id`） |
| `event` | enum | 事件类型，见下表（9 个取值） |
| `title` | varchar(120) | 标题（纯文本） |
| `content` | varchar(1000) | 正文（纯文本，不含敏感信息） |
| `read_at` | datetime \| null | 已读时间（UTC）；**NULL = 未读** |
| `created_at` | datetime | 创建时间（UTC） |

索引：`idx_notifications_recipient(recipient_type, recipient_id, id)`（收件箱列表）、
`idx_notifications_unread(recipient_type, recipient_id, read_at)`（未读列表与未读计数）。

**事件枚举（`notifications.event`，定稿 9 个）**

| event | 语义 | 接收方 |
| --- | --- | --- |
| `order_delivered` | 新购订单交付成功（上游开通完成） | 会员 |
| `order_failed` | 新购订单交付失败（正文含已脱敏的失败原因） | 会员 |
| `renew_succeeded` | 续费成功（正文含新的到期时间） | 会员 |
| `instance_suspended` | 到期未续费，系统自动暂停 | 会员 |
| `instance_terminated` | 终止收敛（上游主机已删除，本地转 `terminated`） | 会员 |
| `ticket_created` | 新工单 | 管理员 + 客服 |
| `ticket_replied` | 工单新回复（**双向同名**：发给会员 = 客服公开回复了你的工单；发给管理员 = 会员回复了工单） | 会员 / 管理员 + 客服 |
| `ticket_closed` | 工单被**客服**关闭 | 会员 |
| `expiry_reminder` | 到期前提醒 | 会员 |

**`email_logs`（邮件发送留痕，同步发送、成功与失败都记）**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | bigint | 主键 |
| `to_addr` | varchar(255) | 收件人地址 |
| `subject` | varchar(255) | 主题（纯文本） |
| `status` | enum(`success`,`fail`) | 发送结果 |
| `error` | varchar(500) | 失败原因（**已脱敏**，绝不含 SMTP 口令；成功为空串） |
| `created_at` | datetime | 发送时间（UTC） |

索引：`idx_email_logs_created(created_at)`、`idx_email_logs_status(status, created_at)`。

**`instances.expiry_reminded_due`（到期提醒去重锚点）**

`DATETIME NULL`，记录「已就哪个到期时间提醒过」。与 `next_due_date` 相等 ⇒ 本到期周期已提醒；
续费/同步把 `next_due_date` 推进后两者不再相等 ⇒ 提醒**自动重新武装**（见 17.5）。

### 17.2 站内通知接口

会员端与管理端**同构**，唯一区别是接收方身份（会员 token → `recipient_type=member`；
管理员 token → `recipient_type=admin`，读**自己**的收件箱）。

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/notifications` | 会员通知列表（`page` / `page_size` / `unread=true\|false`），回带 `unread` 未读数 |
| `GET /api/v1/notifications/unread-count` | 会员未读数（`{unread}`） |
| `POST /api/v1/notifications/:id/read` | 单条已读（**幂等**：重复已读返回 `already_read=true`，不覆盖首次 `read_at`） |
| `POST /api/v1/notifications/read-all` | 全部已读（返回本次置为已读的 `updated` 条数与 `unread=0`） |
| `GET /api/v1/admin/notifications` | 管理端通知列表（同参数、同视图） |
| `GET /api/v1/admin/notifications/unread-count` | 管理端未读数 |
| `POST /api/v1/admin/notifications/:id/read` | 管理端单条已读（幂等） |
| `POST /api/v1/admin/notifications/read-all` | 管理端全部已读（幂等） |

**权限（定稿）**

| 角色 | 会员端通知 | 管理端通知 |
| --- | --- | --- |
| 会员 | 仅**本人**（他人的通知与不存在的通知统一 `404`，不暴露存在性） | — |
| `admin` / `support` / `finance` | — | 各自读**本人**收件箱（通知是个人收件箱，**不属于工单域**，故 finance 也可访问；只是扇出只覆盖 admin + support，finance 通常是空的） |
| 未登录 / 跨端 token | `401`（会员 token 访问管理端、管理员 token 访问会员端一律 `401`） | 同左 |

**列表响应示例**

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "items": [
      {
        "id": 12,
        "event": "ticket_replied",
        "title": "工单已回复：主机无法连接，请协助排查",
        "content": "客服已回复您的工单 T20261008225813K7Q2ZP（主机无法连接，请协助排查）。\n\n请登录会员中心查看回复内容。",
        "read": false,
        "read_at": null,
        "created_at": "2026-10-08T22:58:13Z"
      }
    ],
    "page": 1,
    "page_size": 20,
    "total": 3,
    "unread": 2
  }
}
```

**单条已读响应（幂等）**：`{"notification": {...}, "already_read": false}`；
重复调用返回 `"already_read": true`，`read_at` 保持首次值。
**全部已读响应**：`{"updated": 2, "unread": 0}`（无未读时 `updated=0`）。

### 17.3 SMTP 设置与测试邮件

#### 设置键 `email.smtp`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `enabled` | bool | 是否启用邮件通知；启用时 `host` / `from` 必须齐全（否则 `40002`） |
| `host` | string | SMTP 服务器主机名（不含端口） |
| `port` | int | 端口；**留空（0）按加密方式取缺省**：`none`→25、`starttls`→587、`ssl`→465 |
| `username` | string | 认证用户名（可空 = 不做认证；**非空时必须提供 password**，否则 `40002`） |
| `password` | string | 认证口令（**三态**：省略 = 保持、给值 = 替换、空串 = 清空；接口只回显 `password_configured` 与掩码） |
| `from` | string | 发件人地址（信封与 `From` 头都用它；非空时必须是合法邮箱） |
| `from_name` | string | 发件人显示名（可空；非 ASCII 自动做 RFC 2047 编码） |
| `encryption` | enum | `none` / `starttls` / `ssl`；空值按 `none` |

**加密方式评估（定稿：三种都实现）**：`ssl` 是**隐式 TLS**（建连即 TLS，通常 465 端口）——
标准库 `net/smtp` 没有内置这一形态，本实现用 `crypto/tls` 先建 TLS 连接再 `smtp.NewClient` 承接，
**不引入任何外部依赖**（约 20 行）；`starttls` 走 `Client.StartTLS`。
明文 + 认证在**非回环地址**上会被标准库拒绝（`smtp.PlainAuth` 的安全保护）——远程发信请用 `starttls` / `ssl`。

#### 设置键 `notifications`

| 字段 | 类型 | 缺省 | 说明 |
| --- | --- | --- | --- |
| `inapp_enabled` | bool | `true` | 站内通知总开关（关：不写 `notifications` 行） |
| `email_enabled` | bool | `true` | 邮件通知总开关（关：不发任何通知邮件；**是否真的发信还取决于 `email.smtp` 是否启用且齐全**） |
| `expiry_reminder_enabled` | bool | `true` | 到期提醒开关（关：到期扫描不产生任何提醒、也不认领去重锚点） |
| `expiry_reminder_days` | int | `7` | 到期前多少天提醒（1–30，越界 `40001`） |

> 缺省为「全开 + 提前 7 天」：**未配置该键的新站点开箱即用**；邮件在 `email.smtp` 未配置时
> 静默跳过（未配置不是故障，不写失败留痕、不刷日志）。

#### 接口（全部**仅 `admin` 角色，含读取**；`finance` / `support` 一律 `403`）

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/admin/settings/email/smtp` | 读取 SMTP 设置（口令只给 `password_configured` + `password_masked`：首 4 位 + `****`） |
| `PUT /api/v1/admin/settings/email/smtp` | 局部更新（字段均可选但至少一个；口令三态；`port_effective` 回带实际端口） |
| `POST /api/v1/admin/settings/email/test` | **发送测试邮件**（同步；`to` 缺省回退 `settings.site.admin_email`，两者皆空 `40001`） |
| `GET /api/v1/admin/settings/notifications` | 读取通知开关 |
| `PUT /api/v1/admin/settings/notifications` | 局部更新通知开关 |

测试邮件接口的错误口径：**SMTP 未配置/未启用 → `40002`**（明确提示先配置）；
**发送失败 → `50004`**（`message` 含已脱敏的 SMTP 原因，绝不含口令）。
测试邮件**不受通知总开关约束**（它的用途就是验证配置），但同样写入 `email_logs`。
邮件报文：纯文本（`text/plain; charset=UTF-8`）、正文 base64（避免中文/长行/点号行被传输层改写）、
主题与非 ASCII 显示名按 RFC 2047 编码、站点名尾注（「本邮件由「<站点名>」自动发送，请勿直接回复。」）。

### 17.4 事件接线表

全部事件都在**业务事务提交后**触发（调用点见下表），生产实现异步执行、失败只记日志。

| 事件 | 触发点（代码位置） | 接收方 | 站内 | 邮件 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `order_delivered` | `internal/delivery` 新购交付落库成功 | 会员 | ✅ | ✅ | 正文含单号、商品名、实例名、到期时间 |
| `order_failed` | `internal/delivery` 订单置 `failed` | 会员 | ✅ | ✅ | 正文含 `provision_error`（交付链路已脱敏） |
| `renew_succeeded` | `internal/delivery` 续费交付落库成功 | 会员 | ✅ | ✅ | 正文含新的到期时间 |
| `instance_suspended` | `internal/scheduler` 到期暂停成功 | 会员 | ✅ | ✅ | 幂等路径（上游已暂停）同样通知一次 |
| `instance_terminated` | `internal/scheduler` 取消申请收敛成功 | 会员 | ✅ | ✅ | **仅 `changed=true` 时发**（幂等重复收敛不再打扰） |
| `expiry_reminder` | `internal/scheduler` 到期提醒扫描 | 会员 | ✅ | ✅ | 先去重认领再投递，见 17.5 |
| `ticket_created` | `router` 提单落库后 | admin + support（扇出） | ✅ | ✅ | 邮件发 `settings.site.admin_email`（有值且 SMTP 可用时） |
| `ticket_replied`（管理端侧） | `router` **会员回复**落库后 | admin + support（扇出） | ✅ | ✅ | 同上 |
| `ticket_replied`（会员侧） | `router` **客服公开回复**落库后 | 会员 | ✅ | ✅ | **内部备注（`internal=true`）不触发任何通知** |
| `ticket_closed` | `router` **客服关闭**成功后 | 会员 | ✅ | ✅ | 幂等重复关闭不重复通知；**会员自行关闭不通知客服**（定稿） |

**扇出与收件人（定稿）**：管理端站内通知按**在职**的 `admin` + `support` **逐个账号**写行
（一行一人，读/未读按人记录）；`finance` 不接收工单类通知；管理端通知**邮件**只发给
`settings.site.admin_email`（安装向导期可空 → 空值跳过邮件）。会员侧邮件发 `members.email`。

**不接线的路径（定稿）**：管理端**手动**暂停/恢复实例（人工操作不打扰会员）、会员自行关闭工单、
管理员内部备注、上游自行暂停/删除（由管理端同步接口收敛）。

**管理员重试交付同样接线**（定稿）：重试与新购交付共用同一条 `deliverNew` 链路，因此
重试成功会补发 `order_delivered`（会员的订单终于开通了）、重试仍失败会再次发 `order_failed`
（含新的失败原因）——这是有意为之：交付结果变化对会员是有价值的信息，且不会产生重复的「同一结果」通知。

### 17.5 到期提醒（scheduler 扩展）

**扫描范围**（同一轮扫描的**阶段零**，在到期暂停之前执行，**不需要上游**）：

```
status = active
  AND cancel_status <> 'pending'          -- 已进入终止流程的不提醒
  AND next_due_date IS NOT NULL
  AND next_due_date >  now                -- 已过期的走暂停链路，不提醒
  AND next_due_date <= now + N 天         -- N = notifications.expiry_reminder_days（缺省 7）
  AND (expiry_reminded_due IS NULL OR expiry_reminded_due <> next_due_date)
```

**去重锚点（定稿）**：`instances.expiry_reminded_due`。先**原子认领**
（`UPDATE ... SET expiry_reminded_due = next_due_date WHERE 锚点 <> next_due_date`，
受影响行数 = 1 者认领成功）再投递通知：

- 同一到期周期**只提醒一次**（并发/重复扫描时只有一个认领成功）；
- 续费或同步把 `next_due_date` 推进后锚点自动失效 ⇒ **新周期重新提醒**；
- **宁可少发不重复发**：认领成功而投递失败的极端情形（进程崩溃、库故障）本周期不再补发。

用列而不是「反查 notifications」的理由：通知总开关关闭时不产生通知行，去重不能依赖通知行是否存在。

**扫描统计**：`ScanReport` 新增 `expiry_scanned` / `expiry_reminded` / `expiry_failed`
（`expiry_failed` = 认领时的数据库错误，**下一轮自动重试**）。

**边界**：上游未配置时提醒阶段照常执行（阶段零先跑，随后上游检查返回错误并跳过暂停/收敛）；
未接线通知（测试默认）时整段跳过；`notifications` 设置读取失败只记警告并跳过本轮提醒（不阻断扫描）。

### 17.6 开关与边界

1. **站内与邮件独立开关**：站内关只停写库、邮件照发；邮件关只停发信、通知照写
   （集成测试对两个方向都有断言）。
2. **未配置 SMTP 静默跳过**：不写 `email_logs` 失败行、不刷服务日志（避免「没配邮件」把日志刷满）。
3. **发送失败留痕**：`email_logs.status=fail` + 已脱敏原因；通知本身不受影响。
4. **幂等**：已读、全部已读、重复关闭、重复收敛、重复扫描都不产生重复通知。
5. **失败隔离**：通知任务内的 panic 会被捕获并记日志；通知链路里的任何错误都不改变业务结果。
6. **不做（留后续批次）**：通知删除/归档、按事件细分的通知偏好、邮件模板自定义与品牌化、
   邮件重试队列、抄送与多人收件、短信与 webhook 通道、通知保留期清理（表会持续增长，需运维定期归档）。

### 17.7 真机实测（2026-10-08，开发库）

环境：开发库 `lyidc`（迁移 `0010 → 0011`，`version: 11 (dirty=false)`），后端 `127.0.0.1:8080`；
**mini SMTP 收信器**（`scratch/stage6b/smtp_sink.py`，Python 标准库 socket，**不装任何依赖**，
应答 EHLO/MAIL/RCPT/DATA 并逐封存盘到 `scratch/stage6b/mail/NNN.eml`）监听 `127.0.0.1:2525`。
测试数据准备（均为开发库数据）：`settings.site` 写入站点名「验证站点-阶段6b」与 `admin_email=ops@oem.example.com`；
注册会员 `vfy6b`（id=10）并写库造一台属于该会员的 active 实例（`id=4`，host 30002）；
实例 `id=3`（会员 9 `vfy6a`）的 `next_due_date` 改为当前 UTC + 3 天用于到期提醒演练。

| 步骤 | 请求 / 动作 | 结果（证据） |
| --- | --- | --- |
| 配置 SMTP | `PUT /admin/settings/email/smtp`（host=127.0.0.1、port=2525、encryption=none、无认证） | 200；视图 `port_effective=2525`、`password_configured=false`、`updated_by=1` |
| 测试邮件 | `POST /admin/settings/email/test {to: ops@oem.example.com}` | 200 `{"sent":true,"to":"ops@oem.example.com"}`；**sink 收到第 1 封**（`mail/001.eml`，690 B）：`from: 验证站点-阶段6b <noreply@oem.example.com>`、`subject: 【验证站点-阶段6b】SMTP 测试邮件`（RFC 2047 编码已正确解码）、正文含「发件服务器：127.0.0.1:2525」与站点名尾注；`email_logs` 记 `success` |
| 提单（关联本人实例） | `POST /tickets`（会员 vfy6b） | 200；工单 **`T20261008124001RHDUN9`**（id=23）；站内通知 **`#1/#2`**（recipient `admin:1` / `admin:2`，`ticket_created`，**finance(id=3) 无**）；**sink 第 2 封**发 `ops@`（正文：会员 vfy6b 提交了工单 …（分类 technical）） |
| 会员回复 | `POST /tickets/23/reply` | 200；站内 **`#3/#4`**（admin+support，`ticket_replied`）；**sink 第 3 封**发 `ops@` |
| 管理员**内部备注** | `POST /admin/tickets/23/reply {internal:true}` | 200（状态保持 `open`）；**零通知、零邮件**（通知表与 `email_logs` 均无新增） |
| 客服公开回复 | `POST /admin/tickets/23/reply {internal:false}`（support cs6a） | 200 `status=replied`；站内 **`#5`**（`member:10`，`ticket_replied`）；**sink 第 4 封**发 `vfy6b@example.com`（正文：客服已回复您的工单 …） |
| 客服关闭 | `POST /admin/tickets/23/close` | 200 `closed_at=2026-10-08T12:40:20Z`；站内 **`#6`**（`member:10`，`ticket_closed`）；**sink 第 5 封**发 `vfy6b@`；**重复关闭** `already_closed=true` 且**无新通知** |
| 到期提醒（run1） | 改库把实例 3 的 `next_due_date` 置为 UTC+3 天 → 服务启动后首轮扫描 | 扫描日志 `expiry_scanned=1 expiry_reminded=1`；站内 **`#7`**（`member:9`，`expiry_reminder`，正文含实例名与「提前 7 天」）；**sink 第 6 封**发 `vfy6a@`；去重锚点 `expiry_reminded_due = next_due_date`；暂停/收敛阶段 `scanned=0`（**本轮零上游调用**） |
| **去重**（run2） | 重启服务 → 第二轮扫描 | 日志 `到期暂停扫描完成：无到期实例、无待收敛的取消申请、无即将到期实例`；`expiry_reminder` 通知仍为 **1 条**、sink 仍为 **6 封** |
| **开关关闭**（run3） | `PUT /admin/settings/notifications {expiry_reminder_enabled:false}` + 到期时间改为 UTC+2 天 → 重启扫描 | 200；扫描日志同上（无提醒）；`expiry_reminder` 仍 1 条、邮件仍 6 封、去重锚点**未被认领**（保持旧值） |
| **重新武装**（run4） | 开关改回 `true`（同一新到期时间）→ 重启扫描 | 日志 `已投递到期提醒 instance_id=3 … next_due_date=2026-10-10T12:42:36Z`；站内 **`#8`**；**sink 第 7 封**——证明 run3 的静默来自开关，且**推进到期时间后重新提醒**（新周期一次） |
| 会员端接口 | `GET /notifications`、`?unread=true`、`/unread-count`、`POST /:id/read`（两次）、`/read-all` | 列表 `total=2 unread=2`（新建在前 #6/#5）；筛选与计数一致；首次已读 `already_read=false read_at=12:45:17Z`，**重复已读 `already_read=true` 且 `read_at` 不变**；`read-all` 返回 `{"updated":1,"unread":0}` |
| 管理端接口与权限 | `GET /admin/notifications`（admin / support / finance）、越权已读、跨端 token | admin `total=2 unread=2`、support `total=2`、**finance `total=0`**（扇出不覆盖）；support 读 admin 的通知 **`404 通知不存在`**；会员 token 访问管理端 **`401`** |
| 口令脱敏 | `PUT /admin/settings/email/smtp {username,password}` | 响应 `password_configured=true`、`password_masked=stag****`；**响应体与 server 日志均无明文口令**（`grep` 计数 0）；随后清空口令恢复（`password_configured=false`） |
| 库内一致性 | `SELECT … FROM notifications / email_logs` | 通知 **8 条**（2 admin×ticket_created + 2 admin×ticket_replied + 1 member×ticket_replied + 1 member×ticket_closed + 2 member×expiry_reminder）；邮件留痕 **8 条全 success**、0 fail；sink 目录 8 个 `.eml` 与留痕逐条对应 |

**结论**：站内通知（会员/管理端各自的收件箱、未读计数、已读幂等、跨端与跨人隔离）、
邮件（设置三态与脱敏、测试邮件、RFC 2047 中文主题、真实投递）、事件接线（工单四类事件、
内部备注零泄漏、会员自行关闭不通知客服、扇出只覆盖 admin + support）与到期提醒
（**每周期一次 + 去重 + 新周期重新武装 + 开关关闭不产生**）全部在真机按契约生效；
扫描轮次对上游**零调用**（无到期实例）。测试数据（会员 10、实例 4、站点设置与 8 条通知/邮件留痕）
留在开发库作证据，生产部署前按需清理。

**未做（本批边界）**：`starttls` / `ssl` 两种加密方式只有单元测试（进程内自签证书假 SMTP）覆盖，
真机验证走的是明文 + 无认证（mini sink 无 TLS 能力）；SMTP **认证**路径同样只有单测覆盖
（mini sink 不校验凭据）；交付类事件（`order_delivered` / `order_failed` / `renew_succeeded`）
由集成测试覆盖（假上游 + 同步通知），**未做真实开通的真机演练**（本批可选项，避免真机开销）。

## 18. 邀请返佣（R10-A / R10-B）

邀请返佣把「老带新」的收益固定成一条**钱的口径**：买家下单并支付成功后，按订单原价与一个生效比例
算出返佣，发到其邀请人的**邀请返现余额**。本节是该口径的唯一权威；公式与比例优先级不得自行改动。

R10-A 落地绑定与返佣结算核心（18.1–18.4 的钱的口径、18.5 的概览/流水/邀请列表与自建券使用规则）；
R10-B 落地自建券创建与管理、提现/转余额（含审核）、管理端返佣管理（18.1.1 的出账申请表与账务口径、
18.5 的自建券/出账接口与折扣上限）。

### 18.1 数据模型（迁移 0015）

| 表 / 列 | 类型 | 说明 |
| --- | --- | --- |
| `members.inviter_id` | `BIGINT UNSIGNED NOT NULL DEFAULT 0` | 邀请人会员 ID；`0` = 无邀请人。**首绑永久生效**（见 18.4） |
| `members.invite_code` | `VARCHAR(32) NULL`，`UNIQUE` | 我的邀请码；**惰性生成**——会员首次开通返现时才生成，`NULL` = 尚未生成 |
| `members.referral_enabled` | `TINYINT(1) NOT NULL DEFAULT 0` | 邀请返现开关（会员自己在后台开/关） |
| `members.rebate_balance` | `DECIMAL(14,2) NOT NULL DEFAULT 0.00` | **邀请返现余额**（与消费余额 `balance` 完全独立，**可为负**，见 18.3） |
| `members.rebate_percent_override` | `DECIMAL(5,2) NULL` | 会员级返佣比例（管理员给单个邀请人特批）；`NULL` = 不覆盖 |
| `coupons.owner_member_id` | `BIGINT UNSIGNED NOT NULL DEFAULT 0` | `0` = 平台券；非 `0` = **邀请人自建券**（归属邀请人，见 18.5） |
| `products.rebate_percent` | `DECIMAL(5,2) NULL` | 商品级返佣比例（优先级最高）；`NULL` = 不覆盖 |
| `products.rebate_disabled` | `TINYINT(1) NOT NULL DEFAULT 0` | `1` = 该商品**完全不返佣**（压过一切比例） |

**`referral_rebates`（返佣流水，新表）**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | `BIGINT UNSIGNED` | 流水 ID |
| `order_id` | `BIGINT UNSIGNED`，**`UNIQUE`** | 订单 ID——**发放幂等锚点**，同一订单只发一次 |
| `inviter_id` | `BIGINT UNSIGNED` | 收到返佣的邀请人 ID |
| `buyer_id` | `BIGINT UNSIGNED` | 下单买家 ID |
| `order_type` | `VARCHAR(16)` | 订单类型快照：`new` / `renew` |
| `base_amount` | `DECIMAL(14,2)` | **订单原价**快照（`orders.amount`：基础价 + Σ 配置差价，不含券减免） |
| `discount_amount` | `DECIMAL(14,2)` | **本单优惠券减免**快照（`orders.discount_amount`） |
| `ratio_percent` | `DECIMAL(5,2)` | 生效比例快照（`0.00`–`100.00`） |
| `ratio_source` | `VARCHAR(16)` | 比例来源：`product` / `member` / `global`（见 18.2） |
| `rebate_amount` | `DECIMAL(14,2)` | 返佣金额，**恒 ≥ 0**（见 18.2 公式） |
| `status` | `VARCHAR(16)` | `granted` 已发放 / `rolled_back` 已回滚 |
| `note` / `created_at` / `updated_at` | | 备注与 UTC 时间 |

**`ledger.type` 枚举扩展**：新增 `referral_rebate`（返佣入账/冲回记账口径；**金额正 = 发放、负 = 冲回**）。
该类型流水的 `balance_before` / `balance_after` 记的是 **`rebate_balance`** 的前后值
（不是消费余额 `balance`），仍满足 `balance_after = balance_before + amount` 恒等式。
会员端 `GET /finance/ledger`（余额流水）**过滤掉**本类型——它动的不是消费余额；
管理端 `GET /admin/ledger?type=referral_rebate` 可按类型对账。

**设置项 `referral`（复用 12.1 设置机制）**：`{"enabled", "default_percent", "max_coupon_percent"}`。
`enabled` 是返佣**总开关**；`default_percent` 是全局默认比例；`max_coupon_percent` 是邀请人自建券的
折扣上限口径（**留待 R10-B 建券接口使用**，本批只落字段与校验）。

### 18.1.1 出账申请表 `referral_payouts`（迁移 0016，R10-B）

提现与转余额共用**同一张申请表**，用 `kind` 区分。取舍：两者的字段（申请人 / 金额 /
状态机 / 审核人 / 审核备注 / 时间戳）与审核动作几乎完全一致，拆两张表会复制一套
状态机、审核路径与幂等锚点；`kind` 一列足以分流。表名取中性的 `payouts`（出账）
而非 `withdrawals`，因为 `convert` 是系统内两账户互转、并非提现。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | `BIGINT UNSIGNED` | 申请 ID |
| `kind` | `VARCHAR(16)` | `withdraw` 提现 / `convert` 转余额 |
| `member_id` | `BIGINT UNSIGNED` | 申请人会员 ID |
| `amount` | `DECIMAL(14,2)` | 申请金额，**恒 > 0** |
| `channel` | `VARCHAR(16)` | 到账渠道（仅 `kind=withdraw`）：`wechat` / `alipay`；`kind=convert` 恒为空串 |
| `account_name` | `VARCHAR(64)` | 收款人姓名（**敏感**：接口回显脱敏、日志与审计一律脱敏）；`kind=convert` 恒为空串 |
| `account_no` | `VARCHAR(64)` | 收款账号（**敏感**，同上）；`kind=convert` 恒为空串 |
| `status` | `VARCHAR(16)` | `pending` / `approved` / `rejected` / `paid`（`kind=convert` 只到 `approved` / `rejected`） |
| `remark` | `VARCHAR(255)` | 申请备注（申请人填） |
| `review_note` | `VARCHAR(255)` | 审核备注（管理员填） |
| `reviewed_by` | `BIGINT UNSIGNED` | 审核管理员 ID；`0` = 尚未审核 |
| `reviewed_at` | `DATETIME NULL` | 审核时间（UTC） |
| `paid_at` | `DATETIME NULL` | 线下打款时间（UTC，仅 `kind=withdraw` 走到 `paid`） |
| `created_at` / `updated_at` | `DATETIME` | UTC |

索引：`idx_referral_payouts_member (member_id, kind, status)`（本人列表 + 在途申请额聚合）、
`idx_referral_payouts_status (status, kind)`（管理端筛选）。

**状态机（两种 kind 共用骨架）**

```
withdraw:  pending ──approve──▶ approved ──mark-paid──▶ paid
           pending ──reject───▶ rejected

convert:   pending ──approve──▶ approved
           pending ──reject───▶ rejected
```

- `rejected` / `paid` 是**终态**，不可再流转到其它状态（`40002`）；
- **重复审核 / 重复打款幂等**：对 `approved` 再次 `approve`、对 `rejected` 再次 `reject`、
  对 `paid` 再次 `mark-paid` 一律 `200` + `already=true`，不重复产生任何资金动作，
  且**不覆盖**首次的 `reviewed_at` / `paid_at`；
- `approve` 一个已 `rejected` 或已 `paid` 的申请、`reject` 一个已 `approved` 或已 `paid`
  的申请、`mark-paid` 一个非 `approved` 的申请，一律 `40002`（含 message 说明当前状态）。

**账务口径（本批定稿，两类不同）**

| kind | 提交时 | 审核通过时 | 驳回时 | 打款时 |
| --- | --- | --- | --- | --- |
| `withdraw` | **提交即冻结**：`rebate_balance` 立即扣减 + 写返现侧 ledger（负） | 只改状态（钱已冻结） | **退回冻结额**：`rebate_balance` 加回 + 写返现侧 ledger（正） | 只记 `paid_at` |
| `convert` | **只登记不动钱**：校验可用余额后建 `pending` 行 | **同一事务**执行换算：`rebate_balance` 减 + `balance` 加 + **两端各一条 ledger** | **不动余额**（提交时就没扣过） | 不适用 |

- **为什么两类口径不同**：提现是「钱要出系统」，冻结口径保证重复申请不会超额、
  且线下打款前资金已锁定，驳回退回与既有返佣冲回（18.4）是同一套账务模式；
  转换是「系统内两个账户互转」，钱不离开系统，不必提前冻结，审核通过时一笔事务
  完成两端变动与两条流水，幂等锚点是 `status: pending→approved` 的**条件更新**
  （影响行数为 0 即已生效过，直接返回 `already=true`）。
- **可用返现余额**（两类申请的提交校验共用）：

  ```
  可用余额 = rebate_balance − Σ(kind=convert 且 status=pending 的 amount)
  ```

  在途 `withdraw` **不需要**再减——它们在提交时已经从 `rebate_balance` 里扣掉了。
  校验在**行锁**（`SELECT … FOR UPDATE`）下进行，并发提交不会超额。
- **两条 ledger 的类型口径**（18.3 的 `referral_rebate` 语义在本批扩写）：
  - **凡动 `rebate_balance` 的账目**（返佣发放 / 冲回 / 提现冻结 / 提现退回 / 转出）
    一律 `type=referral_rebate`（金额有符号：正 = 进、负 = 出），
    `balance_before` / `balance_after` 记的都是 `rebate_balance` 前后值；
    靠 **`ref_type`** 区分单据来源：`referral_rebate`（返佣流水）/ `referral_withdrawal`（提现申请）/
    `referral_conversion`（转余额申请）。
  - **转换的消费侧入账** `type=adjust`（正），`ref_type=referral_conversion`，
    `balance_before` / `balance_after` 记的是消费余额 `balance` 前后值。
    取舍：`adjust` 是既有预留的「人工调整」类型，语义即「非充值、非订单支付的余额变动」；
    用 `recharge`（专指充值入账）或 `order_pay`（专指订单扣款）都会污染那两类的对账口径。
  - **不新增 `ledger.type` 枚举**：避免 0016 再扩 ENUM 与 `IsValidLedgerType` 双处同步的风险面；
    会员端 `GET /finance/ledger` 继续过滤掉 `referral_rebate`（动的不是消费余额），
    因此提现冻结/退回不会出现在会员余额流水里，而转换的 `adjust` 那一条**会出现**（正确）。

**负数口径**：`rebate_balance < 0` 时**禁止**提交提现与转换（`40002` 明确提示），
其余功能（登录 / 下单 / 充值 / 邀请 / 建券）不受影响。

### 18.2 公式与比例优先级

**公式（核心，不得改动）**

```
返佣 = max(订单原价 × 生效比例 − 本单优惠券减免, 0)
```

- **订单原价** = `orders.amount`（基础价 + Σ 配置差价，**不含**券减免）；
- **本单优惠券减免** = `orders.discount_amount`；
- **下取整到分**，结果 **≥ 0**（公式自带地板，**永远不会出现负返佣**）。

**验证例子（实现与测试必须逐一断言）**

| # | 订单原价 | 生效比例 | 券减免 | 返佣 |
| --- | --- | --- | --- | --- |
| 1 | 100.00 | 20% | 0.00 | **20.00** |
| 2 | 100.00 | 20% | 10.00（用券实付 90） | **10.00** |
| 3 | 100.00 | 20% | 20.00（8 折券） | **0.00** |
| 4 | 100.00 | 20% | 10.00（9 折券） | **10.00** |

**生效比例（三级，权重从高到低，取第一个非空命中）**

1. **商品级**（最高）：`products.rebate_percent`（非 `NULL` 时生效）。
   `products.rebate_disabled = 1` → **该商品完全不返佣**（压过一切，包括下面两级与总开关之外的任何比例）。
2. **会员级**：邀请人本人的 `members.rebate_percent_override`（非 `NULL` 时生效，管理员给单个邀请人特批）。
3. **全局默认**：设置项 `referral.default_percent`。

比例取值 `0.00`–`100.00`（两位小数），**非法值一律拒绝**（格式非法 `40001`、越界 `40002`）。
商品级/会员级的取值若在库里已损坏（越界/非法），按**未命中**处理，落到下一级。

### 18.3 参与与不参与（不返清单）

**返**：

- 新购（`orders.type = new`）与续费（`orders.type = renew`）；
- **含用充值余额支付**的订单（余额支付与渠道回调两条入账路径都返）。

**不返**：

- **充值**（`recharges` 入账路径绝不返，`CompleteRechargePayment` 无返佣钩子）；
- **未绑定邀请人**的订单（`members.inviter_id = 0`，无邀请关系）；
- **商品禁用返佣**（`products.rebate_disabled = 1`）；
- **总开关关闭**（`referral.enabled = false`）；
- **公式结果为 0**（券减免吃满比例）——不发放、不写流水。

**返给谁**：买家 `members.inviter_id` 指向的邀请人。

**邀请返现余额（`members.rebate_balance`）语义**

- 与消费余额 `members.balance` 是**两个完全独立的账户**：返佣进 `rebate_balance`，充值/订单支付动 `balance`；
- **可为负**（已提现后客户退款 → 扣成负）；负值时**只禁止提现与转余额**，不影响登录/下单/充值；
- 提现与转余额接口**本批未提供**（R10-B 起）；负值口径在此预先固定，后续实现不得另起。

### 18.4 发放、幂等与回滚

**发放时机**：**支付成功即发**，与入账**同一事务**（挂在两条入账路径里，**不挂在交付**）：

- `store.PayOrderWithBalance`（余额支付）；
- `store.CompleteOrderPayment`（渠道回调入账）。

**幂等**：`referral_rebates.order_id` **唯一约束**，同一订单只发一次；重复回调（渠道重放）
由 `model.IsOrderSettled` 闸门挡住（不重复入账、不重复发放），唯一约束是第二道防线。

**回滚**：订单取消（`status = cancelled`）时回滚该笔返佣——写负向 `ledger(type=referral_rebate)`、
在 `rebate_balance` 上冲回、`referral_rebates.status` 置 `rolled_back`。**回滚幂等**：再次回滚不重复。
当前取消接口只允许 `pending` 订单（从未发放过返佣），因此这条钩子在现有取消路径上是**防御性**的——
一旦出现「已支付订单取消 / 退款」路径，复用同一钩子即可保证返佣被冲回。
系统当前无退款接口；若后续出现其他「钱退回买家」的路径，按同一钩子接入。

**首绑不改**：`members.inviter_id` 一旦非 `0` 就**永久生效**，后续任何路径都不改绑。
绑定只发生在两处：

1. **注册时**带 `invite_code`（见 6.2 与 18.5）；
2. **下单用邀请人自建券**且尚未绑定（见 18.5）。

### 18.5 接口契约

#### `POST /api/v1/auth/register`（扩展）

见 6.2：新增可选 `invite_code`。非法/不存在/自邀 → `40002` 明确报错；解析成功写入 `inviter_id`。

#### `GET /api/v1/referral` — 我的返佣概览

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token（仅本人） |
| 成功 | HTTP 200，`data` = 概览对象 |

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `enabled` | bool | **会员自己的**邀请返现开关（`referral_enabled`），不是全局总开关 |
| `invite_code` | string | 我的邀请码；从未开通时为 `""`（惰性生成） |
| `invite_link` | string | 站内邀请链接 `{站点地址}/register?invite={邀请码}`；站点地址未配置或无邀请码时为 `""` |
| `rebate_balance` | string | **邀请返现余额**（定点小数字符串，可为负） |
| `effective_percent` | string | 我当前的生效返佣比例：会员级特批优先于全局默认；**总开关关闭时恒为 `0.00`**（商品级随订单而变，不在此处给出） |
| `invitee_count` | int | 已邀请人数 |
| `total_rebate` | string | 累计返佣（已发放且未回滚之和） |

```json
{
  "code": 0, "message": "ok",
  "data": {
    "enabled": true, "invite_code": "ABCD2345",
    "invite_link": "https://shop.example.com/register?invite=ABCD2345",
    "rebate_balance": "20.00", "effective_percent": "20.00",
    "invitee_count": 1, "total_rebate": "20.00"
  }
}
```

#### `PUT /api/v1/referral` — 开通 / 关闭邀请返现

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token（仅本人） |
| 请求体 | `{"enabled": true}`（`enabled` 必填） |
| 成功 | HTTP 200，`data` = 概览对象（同 `GET`） |
| 错误码 | `40001`（`enabled` 缺失或类型不符）、`50001` |

**首次开通时惰性生成唯一 `invite_code`**：8 位 `[A-Za-z0-9]`（字符表去掉易混淆的 `0/O/1/I`，
32 字符 base32，约 1.1e12 种组合），`crypto/rand` 均匀取值，唯一键冲突换码重试最多 5 次。
取舍：只有「首次开通」才生成（惰性），总量远小于组合数，冲突由唯一键 + 重试兜底；
8 位比 6 位更抗碰撞但仍短到可以直接念/贴链接。**已生成则复用，开/关/开不换码**。

#### `GET /api/v1/referral/rebates` — 我的返佣流水（分页）

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token（仅本人） |
| 查询参数 | `page` / `page_size`（缺省 `1` / `20`，上限 `100`） |
| 成功 | HTTP 200，`data` = `{items, page, page_size, total}` |

`items[]` 字段：`id` / `order_id` / `order_trade_no` / `order_type` / `buyer_id` / `buyer_username` /
`base_amount` / `discount_amount` / `ratio_percent` / `ratio_source` / `rebate_amount` / `status` / `note` / `created_at`。

#### `GET /api/v1/referral/invitees` — 我邀请的会员（分页）

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token（仅本人） |
| 查询参数 | `page` / `page_size` |
| 成功 | HTTP 200，`data` = `{items, page, page_size, total}` |

`items[]` 字段：`member_id` / `username` / `joined_at` / `order_count` / `total_spent` / `total_rebate`。

- **消费口径**（`total_spent`）：该会员**已入账**订单（`paid`/`provisioning`/`active`/`failed`）的
  `final_amount` 之和（实付）；
- **贡献返佣口径**（`total_rebate`）：给邀请人带来且仍处 `granted` 的返佣之和（`rolled_back` 不计）。

#### 邀请人自建券的绑定与限制（下单/校验路径）

`coupons.owner_member_id` 非 `0` 的优惠码是**邀请人自建券**（建券接口在 **R10-B**，本批只落地使用规则）：

| 场景 | 行为 |
| --- | --- |
| 买家**未绑定**邀请关系 | 允许用券；**下单同事务内首绑**到该券的 `owner_member_id` |
| 买家**已绑定**邀请关系 | **拒绝用券**，`40002`「该优惠码仅限未绑定邀请关系的客户使用」，**不改绑** |
| 邀请人自己用自己的自建券 | **拒绝**，`40002`「不能使用自己创建的优惠码」（否则自邀） |
| 平台券（`owner_member_id = 0`） | 任何人都能用，不触发绑定 |

> 口径说明：「已绑定 → 拒绝」按字面执行——**哪怕买家已绑定到同一归属邀请人**也不接受该券。
> 自建券是获客工具，用途是拉新绑定；老客继续用平台券。

#### `GET/PUT /api/v1/admin/settings/referral` — 返佣设置

复用 12.1 设置机制，**仅 `admin`**（`finance` / `support` 一律 `403`）。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `enabled` | bool | 返佣总开关（关：支付成功不发放任何返佣） |
| `default_percent` | string | 全局默认返佣比例（`0.00`–`100.00`，缺省 `0.00`） |
| `max_coupon_percent` | string | 邀请人自建券折扣上限（`0.00`–`100.00`，缺省 `100.00`；R10-B 使用） |

PUT 局部更新（至少提供一个字段），改完**下一笔支付即用新比例**（返佣结算每次入账都重读本设置）。
越界比例 → `40002`，格式非法 → `40001`。

#### `PUT /api/v1/admin/products/:id`（扩展）

新增两个字段（三态：缺省不改）：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `rebate_percent` | string \| null | 商品级返佣比例；字符串设值（`0.00`–`100.00`）、`null` 清空（落 `NULL` = 不覆盖，落到会员级/全局默认） |
| `rebate_disabled` | bool | 禁用返佣（`true` 时该商品完全不返佣） |

管理端商品视图（列表/详情）同步输出 `rebate_percent`（可空）与 `rebate_disabled`。

#### 邀请人自建优惠码（R10-B）

##### 折扣上限（防倒挂，核心风控）

邀请人自建券的**折扣百分比上限**：

```
上限 = min(settings.referral.max_coupon_percent, 邀请人的生效返佣比例)
```

- **邀请人的生效返佣比例** = 会员级 `members.rebate_percent_override`（非 `NULL` 且合法时优先），
  否则全局 `referral.default_percent`。即 18.2 三级优先级的**前两级**——
  **商品级不参与**：券是跨商品通用的，无法绑定到某个商品的比例上；
- `max_coupon_percent` 是管理员**再收紧**的全局闸门（缺省 `100.00`，即不额外收紧）；
- **含义**：返还 20% 的人最多发 8 折券（`value=20`，即折扣 20%）。此时该单
  `返佣 = max(原价×20% − 券减免, 0) = max(20 − 20, 0) = 0`——返佣恰好归零，
  **任何情况下返佣不为负**（公式地板是第二道防线，上限是第一道）；
- **生效比例为 `0.00` 时上限也是 `0.00`**，此时任何自建券都被拒（发券等于白送折扣、
  没有返佣对冲）。message 明确给出当前上限值；
- 上限**在创建时校验**；`value` 一经创建**不可修改**（见下），因此不存在「改大绕过上限」的路径。
  若将来放开改值，必须按同一公式重算校验。
- **边界**：上限只在**创建时**判定。之后管理员下调 `max_coupon_percent` 或邀请人的特批比例，
  **已存在的自建券不回收**（会误伤已发出去的获客工具）；运营如需收紧，走停用（`status=off`）。

##### `POST /api/v1/referral/coupons` — 创建自建券

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token（仅本人） |
| 前置 | 调用者必须**已开通邀请返现**（`referral_enabled=1`），否则 `40002`「请先开通邀请返现」 |
| 归属 | `owner_member_id` **强制 = 调用者**（请求体不接受该字段，传了也忽略） |

请求体：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `code` | string | 必填；3-32 位 `[A-Za-z0-9_-]`，唯一且大小写不敏感（同 11.1） |
| `type` | string | 必填；本批**只允许 `percent`**（见下） |
| `value` | string | 必填；折扣百分比，`0 < x ≤ 100` 两位小数，且**必须 ≤ 折扣上限** |
| `cycles` | string[] | 可选，同 11.1（省略 / 空数组 = 全部 6 周期） |
| `starts_at` / `expires_at` | string \| null | 可选，RFC3339 UTC，同 11.1 |
| `max_uses` | int | 可选，缺省 `0`（不限），同 11.1 |
| `comment` | string | 可选，≤ 255 字符，同 11.1 |

**为什么本批只允许 `percent`**：折扣上限要用「比例 ≤ 比例」来保证
`原价×返佣比例 − 券减免 ≥ 0` 恒成立。`fixed` 是**绝对金额**减免，
同一张固定额券在 10 元订单上是 100% 折扣、在 1000 元订单上是 1% 折扣，
**无法用一个比例卡住倒挂**——除非再引入「按订单原价分段的上限」，复杂度远超收益。
取舍：本批自建券只做 `percent`；平台券（管理端创建）仍支持 `fixed`，不受此限。

成功 `data` = 优惠码对象（结构同 11.3，额外带 `owner_member_id` 与 `source="member"`）。

| code | HTTP | 场景 |
| --- | --- | --- |
| `40001` | 400 | `code` 非法、`value` 写法非法、`cycles` 含非法周期、时间非 RFC3339、`comment` 超长 |
| `40002` | 400 | 未开通邀请返现；`type` 不是 `percent`；`value ≤ 0` 或 `> 100`；**`value` 超过折扣上限**（message 含当前上限值，如「折扣不能超过 20.00（上限 = min(设置 50.00, 生效比例 20.00)）」）；`starts_at` 不早于 `expires_at`；`max_uses` 越界 |
| `401` | 401 | 未携带/无效会员 token（管理员 token 访问同样 `401`） |
| `409` | 409 | `code` 已存在（大小写不敏感） |
| `50001` | 500 | 写入本地库失败 |

##### `GET /api/v1/referral/coupons` — 我创建的券（分页）

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token（仅本人，只返回 `owner_member_id = 本人` 的券） |
| 查询参数 | `page` / `page_size`（缺省 `1` / `20`，上限 `100`） |
| 成功 | HTTP 200，`data` = `{items, page, page_size, total}`，**新建在前** |

`items[]` 字段：`id` / `code` / `type` / `value` / `cycles` / `starts_at` / `expires_at` /
`max_uses` / `used_count` / `status` / `comment` / `created_at` / `updated_at`。
（含 `code` / `value` / `status` / `used_count` / 有效期，满足运营自查。）

##### `PUT /api/v1/referral/coupons/:id` — 改自己的券

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token（仅本人） |
| 可改字段 | **仅 `status`（启停）与 `expires_at`（含三态：`null` 清空 = 永不过期）** |
| 不可改 | `value` / `code` / `owner_member_id` / `type` —— 请求体里出现这些字段一律 `40001` 明确拒绝（不静默忽略） |
| 归属 | **只能改自己的券**；他人券（含平台券）一律 `404`「优惠码不存在」 |
| 成功 | HTTP 200，`data` 为更新后的券对象 |

错误码：`40001`（含不可改字段、`status` 非法、时间非 RFC3339）、`40002`（`expires_at` 早于
`starts_at`，按更新后最终值校验）、`401`、`404`、`50001`。

> 取舍：`value` 不可改是为了**杜绝绕过折扣上限**（创建时已按当时的上限校验）；
> `code` 不可改是为了避免「改码挖客」（老客户手上还拿着旧码）；
> `owner` 不可改是为了避免「把券转给别人」绕开首绑归属。

##### 管理端自建券可见性（R10-B 扩 11.3）

`GET /api/v1/admin/coupons`、`GET /api/v1/admin/coupons/:id` 的每一项输出
`owner_member_id`（int）与 `source`（`platform` / `member`），供运营区分平台券与自建券。
管理端 `PUT` 对自建券拒绝修改 `value`（见 11.3）；管理端**不代改**自建券的 `owner` / `type`。

#### 提现申请（R10-B）

##### `POST /api/v1/referral/withdrawals` — 提交提现申请

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token（仅本人） |
| 账务 | **提交即冻结**（见 18.1.1）：`rebate_balance` 立即扣减 + 写返现侧 ledger（负） |

请求体：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `amount` | string | 必填；金额 `> 0`、两位小数、≤ 可用返现余额 |
| `channel` | string | 必填；`wechat` / `alipay` |
| `account_name` | string | 必填；收款人姓名，1–64 字符（去首尾空白后） |
| `account_no` | string | 必填；收款账号，1–64 字符 |
| `remark` | string | 可选；≤ 255 字符 |

校验（按顺序）：

| 条件 | code | message 要点 |
| --- | --- | --- |
| 金额写法非法（非十进制 / 超两位小数 / 带负号） | `40001` | 金额格式不正确 |
| `channel` 不是 `wechat`/`alipay` | `40001` | channel 只能是 wechat 或 alipay |
| `account_name` / `account_no` 为空或超 64 字符 | `40001` | 收款人姓名/账号不能为空或超长 |
| `remark` 超 255 字符 | `40001` | |
| `amount ≤ 0` | `40002` | 金额必须大于 0 |
| **`rebate_balance < 0`** | `40002` | 邀请返现余额为负，暂不能提现或转余额 |
| **`amount > 可用余额`** | `40002` | 余额不足（message 含可用余额） |

成功 `data` = 申请对象（账号字段**脱敏**，见下）。

##### `GET /api/v1/referral/withdrawals` — 我的提现申请（分页）

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token（仅本人） |
| 查询参数 | `page` / `page_size`；可选 `status`（`pending` / `approved` / `rejected` / `paid`） |
| 成功 | HTTP 200，`data` = `{items, page, page_size, total}`，**新建在前** |

`items[]` 字段：`id` / `kind` / `amount` / `channel` / `account_name` / `account_no` /
`status` / `remark` / `review_note` / `reviewed_at` / `paid_at` / `created_at` / `updated_at`。
`account_name` / `account_no` **一律脱敏回显**（见下）。

#### 转余额申请（R10-B）

##### `POST /api/v1/referral/conversions` — 申请把返现余额转为消费余额

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 会员 token（仅本人） |
| 账务 | **提交时不动钱**；审核通过时才换算（见 18.1.1） |

请求体：`amount`（必填，同上）、`remark`（可选，≤ 255 字符）。
校验同提现（金额格式 / `> 0` / 负数余额禁止 / `≤ 可用余额`），无需 `channel` 与账号字段。

##### `GET /api/v1/referral/conversions` — 我的转余额申请（分页）

同提现列表口径，只返回 `kind=convert` 的行；`channel` / `account_name` / `account_no`
恒为空串。

#### 管理端审核与打款（R10-B）

##### `GET /api/v1/admin/referral/withdrawals` / `GET /api/v1/admin/referral/conversions`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，角色 `admin` / `finance`（财务域口径，同 `GET /admin/ledger`） |
| 查询参数 | `page` / `page_size`、`status`、`member_id`（正整数，省略 = 不过滤）、`kind`（仅 withdrawals 接口，缺省 `withdraw`；传 `all` 两类都查） |
| 成功 | HTTP 200，`data` = `{items, page, page_size, total}`，**新建在前** |

`items[]` 除申请字段外，额外带 `member` 会员概要（`id` / `username` / `nickname`），
按当页 `member_id` 批量查出，缺失输出 `null`。账号字段**脱敏回显**（见下）。

##### `POST /api/v1/admin/referral/withdrawals/:id/review`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，**仅 `admin`**（`finance` / `support` 一律 `403`） |
| 请求体 | `action`（必填，`approve` / `reject`）、`review_note`（可选，≤ 255 字符） |

行为按 18.1.1 状态机：`approve` 只改状态；`reject` **退回冻结额**
（`rebate_balance` 加回 + 写返现侧 ledger 正向）。重复审核幂等（`already=true`）。
非法流转 `40002`（message 含当前状态）。

##### `POST /api/v1/admin/referral/withdrawals/:id/mark-paid`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，**仅 `admin`** |
| 请求体 | 空体或 `{}`；可选 `review_note`（追加记录线下打款备注） |

仅 `approved` 可标 `paid` 并记 `paid_at`（取服务端当前 UTC 时间）；重复打款幂等
（`already=true`，**不覆盖**首次 `paid_at`）；非 `approved` 一律 `40002`。

##### `POST /api/v1/admin/referral/conversions/:id/review`

同 withdrawals 的 review，但只有 `approve` / `reject` 两态：
`approve` 在**同一事务**里执行换算（`rebate_balance` 减 + `balance` 加 + 两端各一条 ledger）；
`reject` **不动余额**（提交时就没扣过）。重复审核幂等（`already=true`，换算只生效一次）。

#### 敏感数据脱敏口径（R10-B）

`account_name` / `account_no` 是提现收款人信息，遵循仓库既有脱敏范式
（对齐 SMTP 口令的「只进库、对外一律掩码」口径）：

| 场景 | 行为 |
| --- | --- |
| 接口回显（会员端列表 / 管理端列表） | `account_name` → **姓氏保留 + `**`**（如「张三」→「张**」，单字名 → 「**」）；`account_no` → **前 2 后 2，中间 `****`**（不足 6 位只留首尾各 1 位，不足 2 位整体 `****`） |
| 提交请求回执 | 同上脱敏（**不回显原文**） |
| 日志（`slog`） | 一律脱敏；禁止打印原文 |
| 审计 / 备注 | 一律脱敏 |

脱敏只发生在**展示边界**；库内存原文（否则线下打款无法核对收款人）。
任何错误 message、`review_note` 模板、通知文案都不得包含原文。

#### 管理端会员特批比例（R10-B）

见 6.3 `PUT /api/v1/admin/members/:id`（`rebate_percent_override` 三态）。
管理端会员视图输出该字段，供页面显示「已特批 x%」。

### 18.6 角色权限矩阵（邀请返佣部分）

| 接口 | admin | finance | support | 会员 |
| --- | --- | --- | --- | --- |
| `GET/PUT /api/v1/admin/settings/referral` | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `PUT /api/v1/admin/products/:id`（含返佣字段） | ✓ | ✓ | ✗（`403`） | ✗（`401`） |
| `PUT /api/v1/admin/members/:id`（会员级特批比例，R10-B） | ✓ | ✓ | ✗（`403`） | ✗（`401`） |
| `GET/PUT /api/v1/referral` | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `GET /api/v1/referral/rebates` | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `GET /api/v1/referral/invitees` | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `POST/GET /api/v1/referral/coupons`、`PUT /api/v1/referral/coupons/:id`（R10-B） | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `POST/GET /api/v1/referral/withdrawals`、`POST/GET /api/v1/referral/conversions`（R10-B） | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `GET /api/v1/admin/referral/withdrawals`、`GET /api/v1/admin/referral/conversions`（R10-B，查看类） | ✓ | ✓ | ✗（`403`） | ✗（`401`） |
| `POST /api/v1/admin/referral/withdrawals/:id/review`、`/:id/mark-paid`（R10-B） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `POST /api/v1/admin/referral/conversions/:id/review`（R10-B） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `GET /api/v1/admin/ledger?type=referral_rebate`（对账） | ✓ | ✓ | ✗（`403`） | ✗（`401`） |

### 18.7 边界与本批未做

1. **会员级特批比例**（`members.rebate_percent_override`）：R10-B 已提供管理端写入口
   （`PUT /admin/members/:id`，见 6.3）。**边界**：特批比例只影响**之后**发放的返佣
   （`ratio_percent` 是发放时快照）；下调或清空不回收已发放金额。
2. **邀请人自建券**（R10-B）：
   - 只允许 `type=percent`（`fixed` 无法用比例卡倒挂，见「折扣上限」取舍）；
   - `value` / `code` / `owner` 创建后不可改；
   - 折扣上限只在**创建时**判定，事后收紧设置不回收既有券（运营走停用）；
   - 自建券**不可叠加**其它券（订单只用一张券，既有口径），也不参与平台券的任何叠加活动；
   - 管理端**不代改**自建券的 `value`（同一上限约束）。
3. **提现 / 转余额**（R10-B）：
   - **线下打款**：`mark-paid` 只是记账标记，系统**不对接**任何支付渠道出款；
   - 提现**无最低金额 / 每日次数 / 手续费**限制（本批不做，留运营后续定）；
   - 提现**无自动超时**：`pending` 申请不会自动失效，需管理员人工处理；
   - 转换**即时到账**（审核通过即入消费余额，无交付等待）；
   - **负数余额**只禁提现与转换（本批已落地），不影响登录/下单/充值/邀请/建券。
4. **前端**留 **R10-C**（本批零前端改动）：会员端返佣中心（自建券管理、提现、转余额、
   申请记录）与管理端返佣管理页（提现/转换审核、会员特批比例、自建券查看）。
5. 会员端余额流水（`GET /finance/ledger`）不展示 `referral_rebate`（返佣发放/冲回/提现冻结/退回）；
   返佣看 `GET /referral/rebates`；转换的消费侧 `adjust` 那一条**会**出现在余额流水里（正确）。
6. **退款 / 已支付取消**仍无接口（R10-A 边界不变）；一旦出现「钱退回买家」路径，
   按 18.4 同一钩子冲回返佣，并同步冲回已冻结的提现（若该笔返佣已被提现）——
   该联动**本批未做**，当前依赖 `rebate_balance` 可负的口径兜底。

## 19. 系统版本与更新检查（R13-A）

> 本节只做「检测 + 展示」：版本号可查、检查 GitHub 最新正式版、管理端展示与红点提示。**下载、替换、迁移与重启见第 20 节（R13-B）**。本节**无数据库迁移**。

### 19.1 版本号约定（构建期注入）

1. **变量**：`backend/internal/buildinfo` 导出 `Version`、`Commit`、`BuiltAt` 三个字符串变量，缺省 `dev` / `unknown` / `unknown`。本地 `go build` 不注入也能编译运行，此时即为开发构建。
2. **注入方式**：`go build -ldflags "-X github.com/LangfordKuo/Lyidc_OEM/backend/internal/buildinfo.Version=<版本>"`（`Commit` / `BuiltAt` 同理）。
   - `scripts/package.sh --version <版本>`：注入三个变量并打包，产物名仍为 `lyidc-oem-<版本>-<goos>-<goarch>.tar.gz|.zip`；版本号缺 `v` 前缀时补上（`1.2.3` → `v1.2.3`）；未指定版本时为 `dev-<提交号>`（本地打包不冒充正式版）；
   - `.github/workflows/release.yml`：把已解析的 `${VERSION}`（tag 名，如 `v0.10.0`）同时传给打包脚本与后端镜像构建（镜像构建参数 `VERSION` / `COMMIT` / `BUILT_AT`）；
   - `Dockerfile.backend`：`ARG VERSION=dev`，编译时注入，发布镜像与压缩包版本一致；
   - `.github/workflows/ci.yml`：只做 gofmt / vet / test 与前端构建，**不产出发布产物，因此不注入**（见 19.10）。
3. **格式**：统一 `vMAJOR.MINOR.PATCH[-预发布]`，与 tag 一致（如 `v1.0.0`）；比较时忽略 `v` 前缀。`dev` 与 `dev-*`（如 `dev-a1b2c3d`）均视为**开发构建**。
4. **运行时读取**：`cmd/server` 与 `cmd/migrate` 启动日志输出版本；两者均支持 `-version`（打印版本、提交与构建时间后退出，不读配置、不连库）。管理端经 19.3 接口读取当前版本，**前端不写死版本号**。

### 19.2 语义化比较规则

1. **解析**：`vMAJOR.MINOR.PATCH[-预发布][+构建元数据]`，三段必须是非负整数，构建元数据忽略；不符合时 `compare=unknown`。
2. **开发构建**（当前版本为 `dev` / `dev-*`）：`compare=dev_build`、`has_update=false`，**不与最新版比较、不报「有新版本」**。
3. **预发布**：GitHub `releases/latest` 本身不返回预发布与草稿，因此预发布**不会被提示为「有新版本」**；即便最新版本仍带预发布后缀（仓库侧异常），同样 `has_update=false`。当前运行的是预发布时按 semver 比较（同号正式版高于其预发布，`1.1.0 > 1.1.0-rc1`），预发布标识符按 semver 2.0.0 规则比较（数字段按数值、非数字段按字典序、数字段低于非数字段、段数少者低）。
4. **结论**：`has_update = compare==ok && 最新版为正式版 && 最新版 > 当前版`。数字段按数值比较（`v0.10.0 > v0.9.0`）。

### 19.3 检查更新接口

| 接口 | 语义 |
| --- | --- |
| `GET /api/v1/admin/updates/check` | **实时检查**：强制访问 GitHub，成功或失败都写入缓存（19.4），返回同形状数据 |
| `GET /api/v1/admin/updates` | **读缓存**：只读本地缓存，**不发出网请求**；从未检查过时 `status=not_checked` |

鉴权：管理员 token，仅 `admin`（19.6）。两接口成功均为 HTTP 200，`data` 形状相同：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "current_version": "v0.9.0",
    "commit": "a1b2c3d",
    "built_at": "2026-10-14T08:00:00Z",
    "latest_version": "v0.10.0",
    "has_update": true,
    "compare": "ok",
    "status": "ok",
    "message": "",
    "source": "github",
    "repo": "LangfordKuo/Lyidc_OEM",
    "release_name": "v0.10.0",
    "published_at": "2026-10-10T12:00:00Z",
    "notes": "Release 说明原文（Markdown 源文本，前端按纯文本展示）",
    "notes_truncated": false,
    "html_url": "https://github.com/LangfordKuo/Lyidc_OEM/releases/tag/v0.10.0",
    "platform": "linux-amd64",
    "assets": [
      {"name": "lyidc-oem-v0.10.0-linux-amd64.tar.gz", "size": 12345678, "download_url": "https://github.com/…", "platform_match": true}
    ],
    "checked_at": "2026-10-14T10:00:00Z"
  }
}
```

字段语义：

| 字段 | 说明 |
| --- | --- |
| `current_version` | 当前运行版本（构建注入值；开发构建为 `dev`） |
| `commit` / `built_at` | 构建注入的提交与构建时间；未注入为 `unknown` |
| `latest_version` | 最近一次**成功**获取的最新正式版 tag；从未成功为 `""` |
| `has_update` | 见 19.2；只有真正比当前新的正式版才为 `true` |
| `compare` | `ok`（已比较）/ `dev_build`（开发构建）/ `unknown`（无法比较：版本不可解析或尚无最新版） |
| `status` | 最近一次**尝试**的结果（取值见下表） |
| `message` | 面向管理员的说明：`status` 为失败态时为失败原因；否则 `compare` 为 `dev_build` / `unknown` 时为说明；其余为 `""` |
| `source` / `repo` | 数据来源（当前恒为 `github`）与检查的仓库（19.5） |
| `release_name` / `published_at` / `notes` / `notes_truncated` / `html_url` | 最新正式版的名称、发布时间（RFC3339 UTC）、说明原文、是否截断（超过 16 KiB 时截断）、Release 页链接 |
| `platform` | **服务器**运行平台（`GOOS-GOARCH`，如 `linux-amd64`） |
| `assets[]` | 产物列表：`name` / `size` / `download_url` / `platform_match`。`platform_match=true` 当且仅当名称以 `-<platform>.tar.gz` 或 `-<platform>.zip` 结尾（`.sha256` 校验文件不标记） |
| `checked_at` | 最近一次**成功**检查的时间；从未成功为 `""` |

`status` 取值：

| status | HTTP | 场景 | message 口径（示例） |
| --- | --- | --- | --- |
| `ok` | 200 | 成功获取最新正式版 | `""` |
| `not_checked` | 200 | 从未尝试过检查（仅读缓存接口可出现） | `尚未检查更新` |
| `no_release` | 200 | 仓库暂无正式 Release（GitHub `404`） | `仓库 <repo> 暂无正式发布版本` |
| `rate_limited` | 200 | GitHub `403`（限流）或 `429` | `GitHub 访问次数已达上限，请稍后重试；可在更新源设置中配置 GitHub Token 提高额度` |
| `unauthorized` | 200 | GitHub `401`（Token 无效或已过期） | `GitHub Token 无效或已过期，请在更新源设置中更新` |
| `timeout` | 200 | 请求超时（8 秒，19.4） | `连接 GitHub 超时，请稍后重试` |
| `unreachable` | 200 | 网络不可达或 GitHub `5xx` | `无法连接 GitHub 或其服务暂不可用，请稍后重试` |
| `bad_response` | 200 | 响应非法 JSON、超过 1 MiB，或其他非预期状态码 | `GitHub 返回的数据无法解析` |

**失败不走 HTTP 错误码**：GitHub 侧任何失败都返回 `code=0` 的成功包装并带 `status` / `message`，前端据此展示「失败态 + 可重试」，不炸页面。HTTP 错误码只用于鉴权（`401` / `403`）与本地库故障（`500` / `50001`）。

### 19.4 缓存、定时刷新与出网约束

1. **缓存**：settings 键 `updates.check`（JSON），结构如下。`checked_at` 是最近一次**成功**获取的时间，`status` / `message` 是最近一次**尝试**的结果。**失败尝试只改写 `status` / `message`，保留上一次成功的 `release`**：一次限流不会让红点误灭，也不会误亮。
   ```json
   {"repo": "LangfordKuo/Lyidc_OEM", "release": {"tag_name": "v0.10.0", "name": "v0.10.0", "published_at": "2026-10-10T12:00:00Z", "notes": "…", "notes_truncated": false, "html_url": "…", "assets": [{"name": "…", "size": 1, "download_url": "…"}]}, "checked_at": "2026-10-14T10:00:00Z", "status": "ok", "message": ""}
   ```
   `repo` 记录所属仓库：读取时若与当前设置的仓库不一致，视为从未检查（修改仓库后旧结果不会被读到，见 19.5）。`release` 为 `null` 表示从未成功获取过。读写均为「读时生效」，无内存缓存窗口。
2. **定时刷新**（生产装配 `cmd/server` 开启，与到期扫描同一装配方式）：进程启动后延迟 **60 秒**，若缓存缺失或 `checked_at` 距今超过 **6 小时**则检查一次；之后每 **6 小时**检查一次。失败只写服务日志（记录 URL 与 HTTP 状态码，**绝不记录 Token**）并把 `status` / `message` 写入缓存，**不重试、不弹窗**。
3. **出网约束**：`GET https://api.github.com/repos/{owner}/{repo}/releases/latest`；请求头 `Accept: application/vnd.github+json`、`X-GitHub-Api-Version: 2022-11-28`、`User-Agent: lyidc-oem/<版本>`；配置了 Token 时加 `Authorization: Bearer <token>`。超时 **8 秒**（`http.Client.Timeout`；调用方上下文的截止时间同样生效）；响应体上限 **1 MiB**；`notes` 超过 **16 KiB** 时按 UTF-8 字符边界截断并置 `notes_truncated=true`。
4. **并发**：同一进程内检查互斥，定时与手动并发时排队执行，不会同时出网两次。「检查更新」按钮由前端 loading 禁用防连点，后端不设额外冷却。

### 19.5 更新源设置

1. **设置键** `updates.github`（JSON）：`{"repo": "LangfordKuo/Lyidc_OEM", "github_token": "…", "api_base": "", "download_base": ""}`。`github_token` 为可选，只存 settings 表与进程内存，**任何接口与日志都不回显明文**（掩码口径同 12.1，`MaskSecret`）；缺省 `repo` 为 `LangfordKuo/Lyidc_OEM`。
   - `api_base`（R13-B 新增，可选）：检查更新访问的 **API 根地址**，缺省 `https://api.github.com`。供镜像 / 内网代理 / 本地 mock 使用（20.8）。
   - `download_base`（R13-B 新增，可选）：**资产下载基地址**，缺省空串表示原样使用 Release 返回的 `download_url`；非空时下载地址 = `download_base` + 原 URL 的路径部分（20.8）。
   - 两者都**只影响本服务器出网地址**，属于系统级设置（仅 `admin`）；必须为空串或 `http(s)://host[:port][/path]` 形式的绝对地址（不带查询串与片段、不以 `/` 结尾），否则 `40001`。**把 `download_base` 指到非官方地址等于信任该源下发的二进制**，接口回显当前值以便核对。
2. `GET /api/v1/admin/settings/updates`（仅 `admin`）→ `data`：
   ```json
   {"repo": "LangfordKuo/Lyidc_OEM", "github_token_configured": false, "github_token_masked": "", "api_base": "", "download_base": "", "updated_by": null, "updated_at": null}
   ```
   `api_base` / `download_base` 回显**配置原值**（空串表示按缺省）；`api_base` 缺省时 `GET` 也回空串，由后端在检查时取 `https://api.github.com`。
3. `PUT /api/v1/admin/settings/updates`（仅 `admin`）：请求体 `repo` / `github_token` / `api_base` / `download_base` **均可选但至少提供一个**。
   - `repo`：必须是 `owner/name`（`owner` 为字母数字与连字符，`name` 为字母数字与 `._-`），不合法即 `40001`；
   - `github_token`：三态语义同 12.1 `key`——**缺席不变 / 非空串替换 / 空串清空**；非空时长度 ≤ 255 且不含空白与控制字符，否则 `40001`；
   - `api_base` / `download_base`：**非三态**，缺席 = 不变，空串 = 清除（回到缺省 / 原样使用 `download_url`），非空即形状校验（见上）。
   - 成功 HTTP 200，`data` 为更新后的视图。**修改 `repo` 或 `api_base` 时同一请求内清空 `updates.check` 缓存**（旧源的结果不得冒充新源）。
4. **生效**：下一次检查（手动或定时）即使用新仓库、新 Token 与新地址，无需重启。

| code | HTTP | 场景 |
| --- | --- | --- |
| `40001` | 400 | 未提供任何字段；`repo` 格式不合法；`github_token` 形状不合法；`api_base` / `download_base` 形状不合法 |
| `403` | 403 | 角色不是 `admin` |
| `500` / `50001` | 500 | 值损坏 / 库操作失败 |

### 19.6 角色权限矩阵（系统更新部分）

| 接口 | admin | finance | support | 会员 |
| --- | --- | --- | --- | --- |
| `GET /api/v1/admin/updates`（读缓存） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `GET /api/v1/admin/updates/check`（实时检查） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `GET/PUT /api/v1/admin/settings/updates` | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `POST /api/v1/admin/updates/apply`（一键升级，R13-B） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `GET /api/v1/admin/updates/job`（升级任务，R13-B） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |

理由：版本、更新源与升级动作属于系统级操作（升级会替换二进制并重启进程），与设置类接口同口径仅 `admin`（finance / support 不纳入，已登记于此）。

### 19.7 管理端界面（R13-A）

1. 设置页（仅 `admin` 可进，沿用既有闸门）新增卡片「**系统更新**」：
   - 当前版本（读 `GET /admin/updates`），附构建 `commit` 与构建时间；
   - 「检查更新」按钮（loading 时禁用）→ `GET /admin/updates/check`；
   - 结果区：有新版本时醒目展示版本号、发布时间、Release 说明、产物下载链接（`platform_match` 的产物高亮并标注「本服务器平台」）与 Release 页链接；已是最新显示「已是最新（上次检查：…）」；开发构建显示「开发构建，无法比较版本」；失败态显示 `message` 与「重试」按钮，并保留上一次成功结果（若有）；
   - Release 说明按**纯文本**展示（保留换行、不渲染 HTML 与 Markdown，防注入）；
   - 更新源表单（仓库 + GitHub Token，三态输入，沿用 `SecretInput`）。
   - 高亮取舍：按**服务器**运行平台高亮（而非浏览器所在平台），因为升级（R13-B）针对的是服务器进程。
2. **红点**：只由 `has_update` 决定——`has_update=true` 时，在侧栏「设置」项与「系统更新」卡片标题显示红点。无更新、从未成功获取、缓存读取失败时**不显示**（失败不误报）。注意：最近一次**检查失败**但保留了上次成功结论（19.4）时，`has_update` 仍为真，红点继续显示，并在卡片内同时给出失败原因。红点数据来自 `GET /admin/updates`（缓存），**读取红点不触发出网**。
3. 非 `admin` 角色不发起任何更新相关请求（与 19.6 一致）。

### 19.8 一键升级（R13-B 已实现，见第 20 节）

- 接口：`POST /api/v1/admin/updates/apply` 与 `GET /api/v1/admin/updates/job`（20.3 / 20.4）。
- 复用本节的 `latest_version`、`assets[].platform_match` 与 `assets[].download_url`，并要求同名 `.sha256` 产物（20.7）。
- 本节的 `buildinfo` 包与 `-version` 就是升级的版本基线：发布包内的新二进制会在交接前被要求自报版本（20.6.2）。

### 19.9 边界与本批未做

1. **不下载、不替换、不重启**（R13-B）。
2. **预发布不提示为新版本**（19.2）；仓库若只有预发布，表现为 `no_release`（`releases/latest` 不返回预发布）。
3. 只访问 `releases/latest`，不遍历历史版本，也不解析 CHANGELOG。
4. 未鉴权 GitHub 限额约 60 次/小时/IP，超限表现为 `rate_limited`；配置 Token 可提高额度，Token 不参与任何其他用途。
5. 定时刷新只在 `cmd/server` 生产装配启用；`cmd/migrate` 不启动后台任务。
6. Release 说明不渲染 Markdown，前端不新增依赖。

### 19.10 构建与发布联动（同步改动清单）

- `scripts/package.sh`：`--version` 注入三个 buildinfo 变量（既有参数与产物命名不变）。
- `.github/workflows/release.yml`：`${VERSION}` 传给打包与后端镜像构建；`scripts/check_release_workflow.py` 增补版本注入的结构断言。
- `Dockerfile.backend`：`ARG VERSION=dev` 注入 ldflags。
- `.github/workflows/ci.yml`：只做 gofmt / vet / test 与前端构建，**不产出发布产物，不注入**；如后续 CI 产出制品，注入 `dev-<sha7>`。

## 20. 一键自动升级（R13-B）

> 本节实现 19.8 预留的一键升级：**下载 → 校验 → 解包 → 交接 → 替换 → 迁移 → 重启 → 健康检查 → 失败回滚**。
> 面向**主机直装**形态（安装目录含 `lyidc-server[.exe]`、`lyidc-migrate[.exe]`、`dist/`、`config.yaml`），
> **自包含**：不依赖 systemd / Windows 服务 / PM2 等外部守护。容器部署见 20.9 第 2 条。本批**无数据库迁移**。

### 20.1 安装目录与产物

1. **安装目录**＝**当前运行的可执行文件所在目录**（`os.Executable()` 的父目录）。升级只在该目录内读写：
   | 路径 | 角色 |
   | --- | --- |
   | `lyidc-server[.exe]` | 被替换的服务二进制 |
   | `lyidc-migrate[.exe]` | 被替换的迁移二进制（升级时用**新包内的它**执行迁移） |
   | `dist/` | 被替换的前端静态产物（Nginx 托管，替换后**无需 reload**——Nginx 每次请求读文件） |
   | `config.yaml` | **不动**（升级不修改配置） |
   | `upgrade-state.json` | 升级任务状态（20.5，本批新增） |
   | `upgrade.log` | 升级日志（20.5，本批新增） |
   | `.upgrade/` | 任务工作目录（下载包、解包暂存、helper 副本；成功后清理） |
2. **升级包**＝ GitHub Release 的资产 `lyidc-oem-<版本>-<goos>-<goarch>.tar.gz | .zip`（`scripts/package.sh` 的既有产物，**布局不变**），同名 `.sha256` 为校验文件。
3. **不新增发布产物**：helper 由**当前正在运行的二进制复制一份**充当（20.6.1）——否则「第一个带 helper 的版本」无法自我升级，且 helper 自身又要被替换。复制品位于 `.upgrade/<job>/helper/`，不在替换范围内。

### 20.2 前置检查（apply 同步执行，失败即在响应里给出可读原因）

按顺序判定，任一不满足即拒绝（错误码见 20.3）：

| # | 检查 | 失败原因示例 |
| --- | --- | --- |
| 1 | `confirm === true` | `二次确认缺失：请求体必须带 "confirm": true` |
| 2 | `target_version` 为合法语义化版本（19.2） | `target_version 格式不合法` |
| 3 | 当前**不是**开发构建（`dev` / `dev-*`） | `当前为开发构建（版本 dev），没有可靠版本基线，无法判断升级目标；请手动解包替换并执行迁移` |
| 4 | **已有成功检查的缓存**且 `has_update=true`（不强制出网） | `尚未检查更新或没有可用结果，请先「检查更新」` |
| 5 | `target_version` == 缓存里的 `latest_version` | `目标版本 v1.2.0 与最新正式版 v1.1.0 不一致，请重新检查更新` |
| 6 | 目标版本**严格高于**当前版本（不降级、不同版本重装） | `目标版本不高于当前版本` |
| 7 | 存在本服务器平台产物（名称以 `-<GOOS>-<GOARCH>.tar.gz`/`.zip` 结尾） | `发布包缺少本服务器平台（linux-amd64）的产物` |
| 8 | 存在同名 `.sha256` 资产 | `发布包缺少同名 .sha256 校验文件，拒绝升级` |
| 9 | 安装目录可写（试写临时文件） | `安装目录不可写（<dir>），请检查权限` |
| 10 | 磁盘可用空间 ≥ `max(2 × 发布包大小, 512 MiB)` | `磁盘空间不足：需要约 812 MiB，可用 120 MiB` |
| 11 | 能连通更新源：同步拉取 `.sha256`（超时 10 秒） | `无法连接更新源：…`（网络类原因已脱敏） |
| 12 | 无进行中的任务（进程内互斥 + 状态文件 `status=running`） | `已有升级任务进行中` |

检查 11 是**唯一的出网同步动作**，只下载几百字节的校验文件；真正的发布包下载在异步任务里（下载阶段失败同样会写入状态与日志）。

### 20.3 发起升级

`POST /api/v1/admin/updates/apply`（仅 `admin`，20.9）

```json
{"confirm": true, "target_version": "v0.10.0"}
```

成功：**HTTP 202**，`data` 为任务视图（与 20.4 同形状）：

```json
{"code": 0, "message": "ok", "data": {"job_id": "20261014T101530Z-3f9a1c", "status": "running",
 "phase": "preparing", "phase_label": "准备", "progress": 5, "from_version": "v0.9.0",
 "target_version": "v0.10.0", "started_at": "2026-10-14T10:15:30Z", "finished_at": "",
 "updated_at": "2026-10-14T10:15:30Z", "install_dir": "/opt/lyidc", "message": "", "rolled_back": false,
 "db_migrated": false, "log_tail": [], "log_truncated": false}}
```

失败：

| code | HTTP | 场景 |
| --- | --- | --- |
| `40001` | 400 | 请求体非法：`confirm` 不为 `true`；`target_version` 缺失或无法解析 |
| `40002` | 400 | 20.2 第 3–11 条：开发构建 / 无有效检查结果 / 目标版本不一致 / 不高于当前版本 / 缺少平台产物 / 缺少 `.sha256` / 目录不可写 / 空间不足 / 连不上更新源 |
| `409` | 409 | 已有升级任务进行中（含上一次任务仍处于 `running`） |
| `403` | 403 | 角色不是 `admin` |
| `500` / `50001` | 500 | 状态文件读写失败 / 库操作失败 |

**退出进程**：202 响应写出后约 0.5 秒，主进程以 `os.Exit(0)` 结束（跳过优雅关闭），把端口与文件锁让给 helper（20.6）。前端据此在轮询 `job` 时容忍连接中断（20.10）。

### 20.4 查询升级任务

`GET /api/v1/admin/updates/job`（仅 `admin`）→ HTTP 200，`data` 为任务视图；**从未发起过升级时 `data` 为 `null`**（不是 404）。

| 字段 | 说明 |
| --- | --- |
| `job_id` | 任务号（`<UTC 时间戳>-<4 字节随机>`），同时是 `.upgrade/` 下的目录名 |
| `status` | `running` / `done` / `failed` / `rolled_back` |
| `phase` / `phase_label` / `progress` | 见下表；失败/回滚后保留**最后一次到达的阶段** |
| `from_version` / `target_version` | 升级前版本（构建注入值）与目标版本（请求携带值） |
| `started_at` / `finished_at` / `updated_at` | RFC3339 UTC；未结束 `finished_at` 为空串 |
| `install_dir` | 安装目录绝对路径（供管理员核对替换范围） |
| `message` | 成功为空串；失败为**已脱敏**的原因；回滚时为回滚原因 |
| `rolled_back` | 是否已回滚到旧版（`status=rolled_back` 时为 `true`） |
| `db_migrated` | 迁移是否已成功执行（回滚后据此判断「库已前进」的中间态，20.6.3） |
| `log_tail` | `upgrade.log` 末尾 **最多 100 行**（每行按 500 字符截断），按时间正序 |
| `log_truncated` | `log_tail` 是否被截断（行数或行长任一被截） |

阶段（`phase`，`progress` 为该阶段完成时的百分比）：

| phase | progress | 执行者 | 说明 |
| --- | --- | --- | --- |
| `preparing` | 5 | 主进程 | 前置检查通过后的收尾准备（清理陈旧 `.upgrade`、建任务目录、落盘状态） |
| `downloading` | 20 | 主进程 | 下载发布包到 `.upgrade/<job>/` |
| `verifying` | 35 | 主进程 | 计算并比对 sha256（20.7） |
| `extracting` | 50 | 主进程 | 解包到 `.upgrade/<job>/staged/`（防路径穿越） |
| `staging` | 60 | 主进程 | 校验包内结构与**新二进制自报版本**（20.6.2） |
| `handing_off` | 65 | 主进程 | 落盘状态与交接文件、复制 helper、分离启动 helper、退出 |
| `replacing` | 70 | helper | 备份旧文件（`.bak`）并替换二进制与 `dist/` |
| `migrating` | 78 | helper | 用新包内的 `lyidc-migrate` 执行 `up` |
| `restarting` | 85 | helper | 分离启动新 `lyidc-server` |
| `health_check` | 92 | helper | 轮询 `/api/v1/health`（最长 60 秒） |
| `done` | 100 | helper | 成功：清理备份与暂存目录 |
| `failed` | — | 主进程/helper | 未发生替换即失败，或**回滚也失败**（需人工介入，`message` 说明） |
| `rolled_back` | — | helper | 已还原备份并重新拉起旧版（旧版健康检查通过） |

**陈旧任务**：`status=running` 且 `updated_at` 距今超过 **10 分钟**时，视为 helper 已异常消失（断电、被手动结束），读取接口把它落盘为 `status=failed`、`message="升级辅助进程已退出，任务中断；请检查 upgrade.log 与安装目录备份"`。服务启动时也做同一次判定，避免重启后页面永远停在「进行中」。

### 20.5 状态落盘与日志

1. **状态文件** `<安装目录>/upgrade-state.json`（同一时刻只保留**最近一次**任务；原子替换写入，权限 `0600`；内容即 20.4 的字段，另含 helper pid 与阶段时间）。
2. **日志文件** `<安装目录>/upgrade.log`（追加写），主进程与 helper 共写，行格式 `时间 LEVEL 阶段 说明`。**日志不含 GitHub Token、不含任何密钥**（20.7）。
3. **为什么用安装目录下的文件而不是数据库**（本批取舍）：升级会**重启进程**、替换二进制，并且**迁移可能改变库结构**；状态必须在新旧两个版本、迁移前后、库不可用时都能被读到，且 helper 不能依赖「新版本的库访问代码」。安装目录下的文件是唯一同时满足这些条件的载体。接口读取时再从文件组装视图（含日志尾部），因此**刷新页面、进程重启、升级完成或回滚之后都能看到同一份状态**。

### 20.6 helper：交接、替换、迁移、拉起与回滚

#### 20.6.1 交接（主进程）

1. 落盘状态（`phase=handing_off`）；
2. 写 `.upgrade/<job>/handoff.json`：安装目录、状态/日志文件路径、暂存目录、备份目录、目标版本、旧进程 pid、健康检查地址（由监听地址推导；`0.0.0.0` / `::` 探 `127.0.0.1`）、**旧进程的启动参数**（`os.Args[1:]`，保证 `--config` 等参数原样传给新进程）、各类超时；
3. 把当前运行的 `lyidc-server[.exe]` **复制**到 `.upgrade/<job>/helper/`，以复制品为 helper 可执行文件（20.1 第 3 条）；
4. **分离启动** helper（Windows：`DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP`，不继承控制台；Unix：`setsid`），stdout/stderr 追加到 `upgrade.log`，父进程不做 `Wait`；
5. 返回 202 后退出（20.3）。

helper 入口是同一二进制的**子命令**：`lyidc-server upgrade-helper <handoff.json>`（不读配置、不监听端口、不连库）。

#### 20.6.2 helper 执行（分离进程）

1. **等旧进程退出**：按 pid 轮询（最长 60 秒），pid 不可判定（权限不足等）时退化为**端口探测**（连接拒绝即视为已退出）；超时即失败（`失败：等待旧进程退出超时`）；
2. **备份**：`lyidc-server[.exe]` → `.bak`、`lyidc-migrate[.exe]` → `.bak`、`dist/` → `dist.bak/`（同目录改名；已有旧备份先删除）。**三个对象必须都在安装目录里**，缺一即失败且**不做任何改动**（避免出现半套状态；前端托管在别处的部署请手工升级）；
3. **替换**（阶段 `replacing`）：暂存目录里的两个二进制与 `dist/` 移入安装目录。Windows 上运行中的 `.exe` 不能覆盖，因此顺序固定为「**先改名旧文件、再移入新文件**」；
4. **迁移**（阶段 `migrating`）：执行 `<安装目录>/lyidc-migrate[.exe] up`（cwd＝安装目录，继承原 `--config` 参数），stdout/stderr 进 `upgrade.log`，超时 10 分钟；成功后状态置 `db_migrated=true`；
5. **启动新服务**（阶段 `restarting`）：分离启动 `<安装目录>/lyidc-server[.exe]`（cwd＝安装目录，沿用旧进程参数），日志同样进 `upgrade.log`；
6. **健康检查**（阶段 `health_check`）：轮询 `http://<host>:<port>/api/v1/health`，间隔 1 秒、最长 60 秒，**HTTP 200 且 `code=0`** 才算通过（`503 degraded` 视为未通过）；
7. **成功**（`status=done`）：删除 `.bak` / `dist.bak` 与 `.upgrade/<job>`（helper 自身的副本在 Windows 上删不掉时忽略，下一次升级的 `preparing` 会清理陈旧目录）；
8. **失败则回滚**（见 20.6.3）。

#### 20.6.3 迁移与替换的顺序、迁移不可逆的风险与缓解（本批定稿）

- **顺序：先替换文件、后跑迁移、最后才启动新服务**。理由：迁移必须由**新包内的 `lyidc-migrate`** 执行（SQL 与二进制同版本），它随包解到暂存目录、替换后即可用；而新服务在迁移成功前**绝不启动**，因此对外不会出现「新进程 + 旧库结构」的组合。
- **迁移不可逆**：`lyidc-migrate up` 不是事务性的，失败可能留下「部分前进 / dirty」的数据库；**回滚只还原二进制与 `dist`，不还原数据库**（本批**不做数据库备份**——库常在远程实例、helper 无 dump 工具与凭据，把生产库自动导出到安装目录还会带来磁盘与泄露风险）。
- 缓解与取舍（明确接受「二进制回滚但迁移已前进」的中间态）：
  1. 状态里显式给出 `db_migrated` 与失败原因，日志保留 `lyidc-migrate` 的原始输出，管理员可用 `lyidc-migrate up` / `version` 人工重试与核对；
  2. 既有全部迁移都是**向前兼容**语义（加表 / 加列 / 扩枚举，不改写既有列语义），因此旧二进制在前进后的库上**仍可运行**——这是接受该中间态的根据；
  3. 迁移失败时**不启动新服务**，立即回滚二进制并拉起旧版，避免半吊子服务对外提供。
- 回滚步骤：结束新进程（若已启动）→ `.bak` / `dist.bak` 改回原名、移开新文件 → 分离启动旧版 → 健康检查旧版（最长 30 秒）→ 成功则 `status=rolled_back`、`rolled_back=true`、`message` 为失败原因；旧版也起不来则 `status=failed`，`message` 明确要求人工介入并指向 `upgrade.log`。

### 20.7 安全要求

1. **仅 admin**（20.9），升级期间不并发（进程内互斥 + 状态文件 `running`）。
2. **sha256 必须校验**：`.sha256` 资产缺失即拒绝（20.2 第 8 条）；下载后计算 sha256 与文件内的 64 位十六进制摘要比对，**不匹配立即中止，绝不替换任何文件**；摘要文件为 `sha256sum` 格式（`<hex>  <文件名>`），也接受纯 hex，文件名与产物名不一致时拒绝。
3. **资产下载不携带 GitHub Token**：`Authorization` 只用于 GitHub API（`api_base`），**绝不发给资产下载地址**（`download_base` 可被配置成任意主机，带上 Token 就是泄露）。
4. **解包防路径穿越**：拒绝绝对路径、含 `..` 的条目、符号链接与硬链接；总解压上限 1 GiB、单文件上限 512 MiB；超限即失败。
5. **版本一致性**：交接前用**暂存的新二进制**执行 `-version`（`cmd/server` 的既有开关），自报版本必须与 `target_version` 一致（忽略 `v` 前缀差异）；不一致即失败（防止把别的版本包当目标版本装上）。
6. **日志与响应脱敏**：任何日志、状态文件与接口响应都**不含 Token**；升级日志中的 URL 保留主机与路径，不做凭据回显。
7. **不暴露越权路径**：两个接口都挂 `requireAdminRole(RoleAdmin)`；发起接口要求二次确认与目标版本回显校验（防误触、防并发错版）。

### 20.8 更新源注入（真机验证/镜像）

1. 复用 19.5 的设置机制：`api_base` 指向任意 GitHub API 兼容服务（本地 mock、内网镜像），`download_base` 把资产下载重定向到镜像。二者仅 `admin` 可改，**改完立即生效，无需重启**。
2. 下载地址换算：`download_base` 非空时，`下载地址 = download_base + url.Parse(download_url).EscapedPath()`；为空时原样使用 `download_url`。

### 20.9 边界与本批未做

1. **不支持降级与同版本重装**（20.2 第 6 条），也不支持指定历史版本升级。
2. **不适用于容器部署**：容器内替换的是容器可写层，重启后回到镜像内容；容器部署请更新镜像（`docs/deploy.md`）。接口不额外检测容器环境。
3. **不备份数据库**，也没有独立的「回滚按钮」（回滚只在失败时自动发生，见 20.6.3）。
4. **不支持断点续传与多镜像回退**；单次下载失败即整任务失败（可重新发起）。
5. **升级期间不冻结其他 API**：服务短暂中断由进程重启造成，前端只做提示与轮询，不锁死后台。
6. **不校验发布包签名**：sha256 只保证「与更新源声明的一致」，等价于信任更新源（19.5 已登记该风险）。
7. **helper 不感知旧版 `upgrade-state.json` 结构之外的东西**：状态文件是本批新增的运维资产，`.gitignore` 已忽略 `*.log`；`upgrade-state.json` 位于安装目录，**不属于仓库**。

### 20.10 管理端界面（R13-B）

1. 设置页「系统更新」卡片（19.7）在 `has_update=true` 且非开发构建时出现「**立即升级**」按钮；点击弹出**二次确认弹窗**：显示目标版本、将被替换的内容（`lyidc-server` / `lyidc-migrate` / `dist/`，不修改 `config.yaml`）、**「服务会短暂中断（进程会重启，期间接口不可用）」**的明确提示。
2. 提交后卡片进入**阶段进度**展示（轮询 `GET /admin/updates/job`，间隔 2 秒）：阶段名 + 进度条 + 起止时间 + 日志尾部；轮询期间**容忍连接失败**（进程重启时请求会中断，继续按间隔重试，直到重新连上或达到上限）。
3. 结束态：成功（`done`）→「已升级到 <目标版本>」，提示刷新页面；失败（`failed`）→ 失败原因；回滚（`rolled_back`）→ 失败原因 + 「**已自动回滚到 <原版本>，服务已恢复**」，并在 `db_migrated=true` 时补充「数据库迁移已执行，请用 `lyidc-migrate version` 核对」。
4. **进入设置页即读一次任务状态**（`GET /admin/updates/job`）：刷新页面后仍能看到进行中 / 已完成 / 已回滚的任务（状态在服务端持久化，20.5）。进行中时「立即升级」按钮禁用并展示原因，不重复提交。
5. 非 `admin` 角色不进入设置页，因此不发起任何升级相关请求（与 19.6 一致）。
6. **升级前备份提示（R14）**：二次确认弹窗内增加一条醒目提示——「升级会替换程序文件并执行数据库迁移，**建议先到「数据库备份」分区做一次备份**」，并给出跳到该分区的锚点链接。**只提示，不自动备份、不阻断升级**（Langford 明确：升级链路行为不变）。
7. **不改既有行为**：弹窗的字段、按钮、提交流程与任务面板与 R13-B 完全一致，R14 只增加第 6 条提示。

## 21. 数据库备份与恢复（R14）

R14 交付「数据库备份与恢复」：**纯 Go** 逐表导出可重放 SQL（不依赖 `mysqldump` 或任何外部二进制），
以及**覆盖式**恢复；管理端设置页新增「数据库备份」分区。

> **一句话边界**：备份文件**绝不可从网站下载**（Langford：防止黑客通过漏洞拖库）；目录**绝不在 Web 根内**；
> 恢复与删除**只接受备份 ID**；备份内容含管理员密码哈希，**绝不应答回显、绝不写入日志**。

### 21.1 备份实现（纯 Go）与一致性口径

1. **逐表导出**：`SHOW FULL TABLES`（只取 `BASE TABLE`，视图不参与）→ 每表 `SHOW CREATE TABLE` + 流式 `SELECT *` → 生成可重放的 SQL 文本写盘；顺序固定为**每表** `DROP TABLE IF EXISTS` + `CREATE TABLE` + 分批 `INSERT`（先建表后插数据），空表只有建表语句。表按名字排序，导出顺序稳定。
2. **值编码（往返必须逐字节一致）**：
   - `NULL` 与字符串严格区分（`NULL` 不加引号，空串写 `''`）；
   - 单引号 / 反斜杠 / 换行 / 回车 / NUL / `0x1a` 一律转义（`\'`、`\\`、`\n`、`\r`、`\0`、`\Z`），因此一条 `INSERT` 始终是文件里的一行；
   - **二进制列**（`BINARY` / `VARBINARY` / `TINYBLOB` / `BLOB` / `MEDIUMBLOB` / `LONGBLOB` / `BIT` / 空间类型）用十六进制字面量 `X'..'`；**文本列**（`VARCHAR` / `TEXT` / `JSON` / `DECIMAL` / `ENUM` / `SET`）驱动同样返回 `[]byte`，但必须走转义字符串——误用十六进制会改写内容；
   - 时间列按 **UTC 墙钟值**写回（应用 DSN 强制 `parseTime=true&loc=UTC`），与开发机时区解耦；小数秒只在非零时写出（避免把 `DATETIME(0)` 的四舍五入带进来）；零值日期写 `'0000-00-00 00:00:00'`；
   - `AUTO_INCREMENT` 由 `SHOW CREATE TABLE` 原样带出（建表语句里已含当前值）。
3. **分块插入**：单条 `INSERT` **≤ 200 行或 ≤ 512 KiB 先到者为准**（MySQL 5.7 缺省 `max_allowed_packet` 4 MiB，绝不拼一条巨型 INSERT）。
4. **流式写盘**：先写 `<文件名>.tmp` 再原子改名；写盘缓冲 1 MiB；**整库内容不进内存**（内存里只有一条 INSERT 的缓冲）。
5. **一致性口径（本批取舍）**：整库导出在**一个事务**内完成——`SET SESSION TRANSACTION ISOLATION LEVEL REPEATABLE READ` + `START TRANSACTION WITH CONSISTENT SNAPSHOT`，全部表读**同一个一致性快照**；备份点即开始导出的那一刻，不会出现「A 表新数据、B 表旧数据」的错位。
   - 代价：InnoDB 会为长事务保留 undo（导出期间写负载越高占用越多），且导出期间**其他事务的写入不会被阻塞**（快照读）。
   - 收益：恢复出来的一定是一个自洽的库。数据量与单机部署形态下这个取舍划算。
6. **文件头注释**：`generated_at`（UTC）/ `database` / `schema_version` / `app_version` / `operator` / `tables`，并前置 `SET FOREIGN_KEY_CHECKS=0;` 与 `SET NAMES utf8mb4;`——备份文件自带会话前置，重放时不依赖恢复端的连接设置。
7. **元信息**：备份时间（UTC）/ 库名 / schema 版本（`schema_migrations.version`）/ 表数 / 总行数 / 文件大小 / 操作人（管理员用户名）/ 应用版本（`buildinfo.Version`）。

### 21.2 元信息存哪：与文件同目录的 sidecar（本批取舍，**不入库、无新迁移**）

元信息写 `<备份文件名>.json`（`lyidc-backup/v1` 格式），与备份文件同目录、同生共死。

- **为什么不用数据库表**：恢复是覆盖式的——元信息若存库表，恢复一份旧备份会把库内记录一并带回旧状态，于是「恢复点之后创建的备份」会在列表里凭空消失（文件其实还在磁盘上，且 ID 可能被新备份复用，容易误删）；sidecar 始终与文件一致。
- **附带收益**：库不可用（正是最需要看备份的时候）也能正常列出备份。
- sidecar 丢失只降级展示（时间取文件 mtime、其余字段留空并标 `meta_missing=true`），不影响恢复与删除。本批**不新增数据库表、不新增迁移**。

### 21.3 接口

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/admin/db-backups` | 目录状态 + 备份列表（元信息）+ 总占用 |
| `POST /api/v1/admin/db-backups` | 立即备份一次（**同步**返回元信息） |
| `POST /api/v1/admin/db-backups/:id/restore` | **覆盖式**恢复（需 `confirm` + 当前管理员密码） |
| `DELETE /api/v1/admin/db-backups/:id` | 删除一条备份（文件 + 元信息） |

> **备份目录的读取与修改**见 21.10（`GET/PUT /api/v1/admin/settings/db_backup`，R15 起提供）——
> 上表四条接口的形状与语义**不因 R15 改变**。

`GET /api/v1/admin/db-backups` → `data`：

```json
{
  "database": "lyidc",
  "backup_dir": "D:\\lyidc\\backups",
  "backup_dir_default": true,
  "backup_dir_outside_web_root": true,
  "backup_dir_message": "",
  "total_size_bytes": 1048576,
  "auto_cleanup": false,
  "backups": [
    {
      "id": "lyidc-backup-20261014T071500Z-1a2b3c4d",
      "file_name": "lyidc-backup-20261014T071500Z-1a2b3c4d.sql",
      "created_at": "2026-10-14T07:15:00Z",
      "database": "lyidc",
      "schema_version": 17,
      "table_count": 17,
      "row_count": 1234,
      "file_size": 1048576,
      "operator": "admin",
      "app_version": "v0.10.0",
      "file_exists": true,
      "meta_missing": false
    }
  ]
}
```

- `backups` 按时间倒序；`total_size_bytes` 是全部备份文件占用的磁盘空间（不含 sidecar）；`auto_cleanup` **恒为 `false`**（本批不做自动清理，删除由管理员手动执行）；`backup_dir_outside_web_root=false` 时 `backup_dir_message` 给出原因，且备份接口拒绝写入。
- `POST` 成功返回**单条备份**（同上结构）；同步执行（大库可能耗时数十秒），同一时刻至多一个写操作，重复提交 `409`。
- `DELETE` 成功返回 `{"deleted": true, "id": "…", "file_name": "…"}`。
- 恢复见 21.5；**所有接口只返回元信息，没有任何接口能读到备份文件内容**（21.4 第 1 条）。

| code | HTTP | 场景 |
| --- | --- | --- |
| `40001` | 400 | 请求体不是合法 JSON / 字段类型不符 |
| `40002` | 400 | 恢复未确认 / 未给密码 / 密码不正确；备份目录不可用（落在 Web 根内等）；备份文件与当前代码不兼容（格式损坏、截断、`NO_BACKSLASH_ESCAPES`） |
| `403` | 403 | 角色不是 `admin` |
| `404` | 404 | 备份 ID 不存在 / 形状不在白名单 / 备份文件已删 |
| `409` | 409 | 已有备份或恢复在进行中 |
| `50005` | 500 | 导出或恢复执行失败（磁盘、SQL 错误）；message 只带语句种类与表名 |
| `50001` | 500 | 数据库操作失败 |

### 21.4 不可下载 · 目录边界 · ID 白名单（本批最硬的三条）

1. **绝不可从网站下载**：接口只返回元信息，**不提供任何下载/读文件内容的接口**（路由断言测试保证只有列表/创建/恢复/删除四条路由）；接口响应与错误信息都不含备份内容；日志只记元信息与结果。
2. **备份目录绝不在 Web 根内**：缺省 `<安装目录>/backups`（与 `dist/` 同级）；设置键 `db_backup`（JSON：`{"dir": ""}`，留空走缺省，相对路径相对**安装目录**解析，改完下次读取即生效）。
   - 解析结果落在 `<安装目录>/dist` 内、指向已存在的非目录对象、或经**符号链接解析后**落在 Web 根内 → **拒绝**：启动时校验一次（只记警告、不阻断启动），每次创建/列表时再校验（拒绝写入并给出原因）。
   - **R15 起提供修改该目录的界面与接口**（21.10）：保存时即执行**同一套**环境校验（路径解析 → Web 根判定 → 不存在则创建 0750 → 写入探针），校验不过**不落库**；运行时的启动校验与每次使用校验口径**不变**。
3. **文件名不可猜测、接口只接受备份 ID**：文件名为 `lyidc-backup-<yyyyMMddThhmmssZ>-<8 位小写十六进制>.sql`（随机串用 `crypto/rand`）；**ID = 文件名去掉 `.sql`**，形状即白名单：
   - 形状之外的任何输入（`../`、绝对路径、盘符、带扩展名、其他前缀、大小写或长度不符）**一律按「备份不存在」处理（404）**，不接受任何路径参数；
   - 服务端在**自己的白名单目录**里解析 `目录 + ID + .sql`，解析后再复核：结果仍在备份目录内，且 `Lstat` 必须是**普通文件**（符号链接拒绝）。
4. **敏感数据**：备份内容含管理员密码哈希等，**绝不应答回显、绝不写入日志**；操作日志只记 ID、库名、表数、行数、大小、耗时、schema 版本与结果。

### 21.5 恢复（覆盖式、不可撤销）

`POST /api/v1/admin/db-backups/:id/restore`，请求体：

```json
{"confirm": true, "password": "<当前登录管理员的密码>"}
```

1. **校验**（任一不满足即拒绝并给明确原因，且**不动库**）：
   - 角色必须是 `admin`（`requireAdminRole(RoleAdmin)`，finance / support 一律 `403`）；
   - `confirm` 必须为 `true`（否则 `40002`：「恢复会用备份内容覆盖当前数据库且不可撤销」）；
   - `password` 必须与**当前登录管理员**的 `password_hash` 通过 bcrypt 校验（`40002`；不复用会员密码、不复用上游口令，密码本身不写日志）。
2. **语义（覆盖式，不是合并）**：先执行备份文件（逐表 `DROP TABLE IF EXISTS` + `CREATE TABLE` + 分批 `INSERT`），再**删除「当前库有、备份里没有」的表**——备份点之后新建的表会消失，这正是「覆盖」的证据（返回里的 `dropped_tables` 列出被删的表名）。
   - 全程 `FOREIGN_KEY_CHECKS=0`（本项目多数表刻意不建外键，但删除次序仍可能受约束影响），退出前**还原为 1**（会话变量随连接回到连接池，必须还原）；
   - 恢复端的**转义口径**：备份文件用反斜杠转义，因此连接启用了 `NO_BACKSLASH_ESCAPES` 时**直接拒绝**（`40002`）——不偷偷改会话 `sql_mode`（那会顺带关掉严格模式，可能把错误数据静默吞下）。
3. **恢复后追迁移（本批取舍：追，且只在低版本时追）**：
   - 备份里的 `schema_migrations.version` **低于**当前程序内嵌迁移的最新版本 → 恢复完成后执行一次 `up`（与安装向导、`lyidc-migrate` **同一套 golang-migrate + 内嵌迁移文件**，不复制任何迁移 SQL），返回里 `migrated=true` 与 `from_schema_version` / `to_schema_version`；
   - 追迁移用 `Options.DatabaseDSN` 新开一条连接（迁移文件多为多语句，需要 DSN 上的 `multiStatements=true`；应用连接池的 DSN 未必带该参数）；执行失败时接口返回错误并**如实说明「数据已恢复、库停留在版本 N，请用 `lyidc-migrate up` 手动追平」**；
   - 备份版本**高于**当前代码（用新版本备份恢复到旧程序）→ **不迁移**，返回 `warning` 明确提示「当前代码可能无法完全理解该库结构，请尽快升级程序」。
4. **失败即中止**：不吞错、不续跑；错误信息只带语句种类与表名（绝不回显数据内容）。**失败可能留下不完整的库**（见 21.6 的风险声明）。
5. **数据模型与存在性**：备份 ID 不存在或文件已删 → `404`「备份不存在或备份文件已删除」。

`data`（成功）：

```json
{
  "backup_id": "lyidc-backup-20261014T071500Z-1a2b3c4d",
  "file_name": "lyidc-backup-20261014T071500Z-1a2b3c4d.sql",
  "database": "lyidc",
  "from_schema_version": 16,
  "to_schema_version": 17,
  "migrated": true,
  "table_count": 17,
  "dropped_tables": [],
  "restored_at": "2026-10-14T08:02:11Z",
  "warning": ""
}
```

### 21.6 如实声明的风险（Langford 已明确选择不做恢复前自动备份、不做维护模式）

1. **恢复是覆盖式且不可撤销**：执行后当前库内容被备份内容替换，没有「反向恢复」；
2. **失败可能留下不完整的库**：本批**不做恢复前快照**，恢复中途失败（磁盘、SQL、断电）会停在半途——因此界面必须**强烈建议管理员先手动做一次备份**；
3. **恢复期间网站会短暂不可用**：表正在被重建（`DROP` / `CREATE`），并发业务写入可能失败；本批**不做维护模式**，也不冻结其他 API。

界面（21.7）必须把这三条以醒目警告呈现，并要求输入当前管理员密码才能提交。

### 21.7 管理端界面（R14）

设置页新增「**数据库备份**」分区（`id="db-backups"` 锚点，供升级弹窗跳转）：

1. **概览**：当前库名、备份目录（含「不在 Web 根内」的标记；不可用时展示原因）、**备份总占用磁盘**（`formatFileSize`）、以及「**不自动清理**：删除由管理员手动执行」的说明。
2. **立即备份**按钮：进行中禁用并显示 loading；成功 toast（含文件名与大小），失败展示可读原因（目录不可用、磁盘等）。
3. **备份列表**：时间 / 文件大小 / schema 版本 / 表数与行数 / 操作人 / 应用版本；每项操作「**恢复**」与「**删除**」；`file_exists=false`（文件被手工删除）时行内提示且两个操作都不可用。
4. **恢复弹窗**：显示将覆盖的库与所选备份信息 + **不可撤销警告** + **「失败可能留下不完整的库，建议先手动做一次备份」** + **输入当前管理员密码** + 明确确认（按钮标「确认恢复并覆盖」）。
5. **删除确认**：二次确认（含文件名与不可撤销说明）。
6. **不做下载按钮，也不做「查看内容」**。
7. **权限**：整个分区仅 `admin`（设置页整页闸门已挡 finance / support，不发起任何越权请求）。
8. **备份目录可编辑（R15，契约 21.10）**：输入框 + 「保存」按钮 + 校验结果提示（解析后的绝对路径 /
   是否在 Web 根外 / 是否可写；失败时展示后端 `40002` 的可读原因）。旁边必须明示两点：
   ① **改目录不会搬走已有备份**——旧目录里的备份文件不会出现在列表里，要手工迁移或把目录改回去；
   ② **留空即用缺省目录**，并显示缺省目录的实际路径。保存成功后重新读取列表（目录展示随之更新）。

### 21.8 角色权限矩阵（数据库备份部分）

| 接口 | admin | finance | support | 会员 |
| --- | --- | --- | --- | --- |
| `GET /api/v1/admin/db-backups` | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `POST /api/v1/admin/db-backups` | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `POST /api/v1/admin/db-backups/:id/restore` | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `DELETE /api/v1/admin/db-backups/:id` | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `GET /api/v1/admin/settings/db_backup`（R15） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `PUT /api/v1/admin/settings/db_backup`（R15） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |

理由：备份含全库数据与管理员密码哈希，恢复会覆盖整库——与「一键升级」同属系统级操作，仅 `admin`（finance / support 不纳入）；备份目录的设置接口（21.10）决定敏感文件落在哪，权限与备份接口同口径（**仅 admin，含读取**）。

### 21.9 边界与本批未做

1. **不自动清理**（列表里如实展示当前占用，由管理员手动删）；**不自动定时备份**。
2. **不做增量 / 差异备份**，每次都是全量；不做压缩与加密（备份目录权限 0750、文件 0640）。
3. **不提供下载、不支持把备份上传到远端**，也不支持从远端拉取备份文件恢复。
4. **不支持部分表恢复**，也不支持恢复到另一个库（服务端始终覆盖当前连接的库）。
5. **备份格式是自研的 `lyidc-backup/v1`**，不保证与 `mysqldump` 产物互通；恢复只认本格式（注释、转义与分块由本系统生成）。
6. **容器部署（R15 已默认挂卷）**：备份落在容器内的安装目录（`/app/backups`），容器重建即丢失——`deploy/docker-compose.yml` 的 `backend` 服务**默认绑定挂载** `./backups:/app/backups`（落点 `deploy/backups/`，宿主机可见，备份**没有下载入口**，运维直接到该目录取文件）；想换命名卷就写 `backend-backups:/app/backups` 并在顶层 `volumes:` 声明（取文件需 `docker cp`）。若在界面上把备份目录改到别处，需自行保证该路径有持久化挂载。
7. **恢复不阻断其他 API**（没有维护模式）：恢复期间的并发写请求可能失败，属预期行为（21.6 第 3 条）。
8. **不支持 `NO_BACKSLASH_ESCAPES` 的恢复端**（21.5 第 2 条）。
9. **只覆盖基表**：视图不参与导出、也不在恢复时被删除。

### 21.10 备份目录设置（R15，`db_backup` 键的读写）

R14 只有「看到当前目录」；R15 补上**可编辑入口**（设置键 `db_backup`，值结构 `{"dir": "<路径>"}`，21.4）。
保存时即执行与运行时**同一套环境校验**（直接复用 `internal/dbbackup` 的路径解析与目录校验，不另写一套口径），
不允许出现「保存成功但一备份就失败」的配置。

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/admin/settings/db_backup` | 读取配置原值 + 解析后的绝对路径 + 目录可用性（在 Web 根外 / 是否存在 / 是否可写） |
| `PUT /api/v1/admin/settings/db_backup` | 保存备份目录（**保存前完成环境校验并按需创建目录**；校验不过不落库） |

`GET` → `data`：

```json
{
  "dir": "",
  "default": true,
  "default_dir": "/opt/lyidc/backups",
  "resolved_dir": "/opt/lyidc/backups",
  "outside_web_root": true,
  "exists": true,
  "writable": true,
  "message": "",
  "updated_by": null,
  "updated_at": null
}
```

| 字段 | 说明 |
| --- | --- |
| `dir` | 设置项**原值**：空串表示用缺省目录 |
| `default` | 当前是否用缺省目录（`dir` 留空） |
| `default_dir` | 缺省目录的绝对路径（`<安装目录>/backups`），**与当前是否在用无关**（供界面展示「留空 = 用这个」） |
| `resolved_dir` | **当前配置**解析后的绝对路径（相对路径按**安装目录**解析；`dir` 留空时等于 `default_dir`） |
| `outside_web_root` | 是否不在 Web 根（`<安装目录>/dist`）内；`false` 时 `message` 给出原因，且备份接口拒绝写入 |
| `exists` / `writable` | 目录当前是否存在 / **写入探针**（在目录内写一个临时文件再删除）是否通过 |
| `message` | 需要管理员注意的原因（面向管理员、已脱敏）；一切正常时为空串 |

- `GET` **不创建目录、不写库**：目录尚不存在时 `exists=false`、`writable=false`，`message` 说明「保存或首次备份时会自动创建」；目录存在但不可写时 `message` 给出探针失败原因。

`PUT` 请求体：

```json
{"dir": "/data/lyidc-backups"}
```

1. `dir` **必须提供**（键缺席或 `null` → `40001`）；空串或纯空白 = **恢复缺省目录** `<安装目录>/backups`；
2. **保存时的校验顺序（任一步失败即拒绝，且该次保存不落库）**：
   1. 形状校验：不得含控制字符（→ `40001`）；
   2. 路径解析：相对路径按**安装目录**解析为绝对路径（与运行时同一函数）；
   3. **Web 根拒绝**：解析结果落在 `<安装目录>/dist` 内（含符号链接解析后的判定）→ `40002`，message 说明原因；
   4. 目录不存在则按 **0750** 创建（含中间目录；创建失败 → `40002`）；
   5. **写入探针**：在目录内写一个临时文件并删除，失败（权限不足、只读挂载等）→ `40002`；
3. 校验通过才写入 `db_backup` 键，返回**更新后的完整视图**（字段同上，此时 `exists=true`、`writable=true`、`message=""`）；
4. **生效方式沿用 12.1（读时校验、按内容失效）**：备份服务每次使用都重新读设置，改完下一次备份/列表即用新目录，无需重启；
5. **改目录不搬运已有备份**（本批取舍）：旧目录里的备份文件**不会**出现在新目录的列表里，也不会被删除；要保留列表可见性就把文件手工迁移到新目录（sidecar `.json` 一并迁移），或把目录改回旧值。

| code | HTTP | 场景 |
| --- | --- | --- |
| `40001` | 400 | 请求体不是合法 JSON / 字段类型不符 / 缺少 `dir` 键；`dir` 含控制字符 |
| `40002` | 400 | 目录落在 Web 根内、目录创建失败、目录不可写（message 给出可读原因，**不落库**） |
| `403` | 403 | 角色不是 `admin` |
| `401` | 401 | 未登录 |
| `500` | 500 | 备份服务不可用（数据库句柄不可用时的降级口径，与 21.3 一致） |
| `50001` | 500 | 读写本地库失败；库内 `db_backup` 值损坏（只可能由手工改库导致） |

## 22. 上游状态回推（R16）

R16 交付「上游 → 本系统」的状态回推接入：开通时把本系统的回推地址登记给上游，
上游在主机状态变化时 `POST /api/host/sync` 推送最新主机信息，本系统按该推送同步实例状态与字段。

> **与既有链路的关系**：这是本系统**第一次接收来自上游的主动请求**（此前的上游交互全是本系统出站调用，见第 8 节）。
> 推送是**单向、幂等、静默**的：只写 `instances` 与一条审计，**不通知会员、不发站内信、不发邮件、不回读上游**。

### 22.1 数据面（迁移 0018）

迁移 0018（`backend/migrations/0018_add_instance_downstream_token.up.sql` / `.down.sql`，**不改 0001–0017**）：

1. `instances` 新增 `downstream_token VARCHAR(32) NOT NULL DEFAULT ''`（上游回推鉴权串；32 位十六进制）。
2. `instances.status` 扩枚举：`ENUM('provisioning','active','suspended','cancelled','terminated')`——
   新增 `provisioning`（交付在途，见 14.2 的不可见边界）。
3. `instances.host_id` 由 `INT UNSIGNED` 改为 `INT`（带符号）：交付在途用**负数占位**（见 22.2），
   真实上游主机 ID 恒为正；`uk_instances_host` 唯一键**保持不变**（占位值互不相同，真实主机 ID 仍全局唯一）。

存量老实例的 `downstream_token` 为空（历史数据不补发）——它们不参与回推，`/api/host/sync` 对它们一律拒绝（见 22.3）。

### 22.2 开通时登记回推地址

1. **实例先于上游调用落库**：交付认领后、调 `CreateHost` **之前**，先插入（或复用）该订单的实例行——
   订单 ↔ 实例一对一（`uk_instances_order`），因此「预落实例」对同一订单天然幂等。
   - 首建：`status=provisioning`、`host_id = -订单号`（负数占位：既不与真实主机 ID 冲突，
     又在并发交付之间互不相同）、`member_id` / `order_id` / `product_id` / `product_name` / `name` /
     `billing_cycle` 按订单快照写入、`downstream_token` 生成一次；
   - 重试：按 `order_id` 取到既有行，**沿用同一个 `instances.id` 与 `downstream_token`**（不重新生成）；
   - 交付成功：同一行改为 `status=active` 并写入真实 `host_id` 与回读字段（**不新增行**；
     该行若意外缺失则插入一行兜底，保证交付结果照样落库）；
   - 交付失败：该行**保留在 `provisioning`**（列表不可见，见 14.2），重试继续用它。
2. **`downstream_token` 生成规则**：`crypto/rand` 取 16 字节 → 32 位十六进制（**长度恒为 32**，上游要求）。
   同一实例**只在首次落库时生成一次**，之后任何重试、任何同步都不改写。
3. **登记参数（四步全带）**：`downstream_url` / `downstream_token` / `downstream_id` 随开通链路
   `POST /cart/clear`、`/cart/add_to_shop`、`/cart/settle`、`/apply_credit` **四步全部提交**
   （上游只在四步都收到时才在主机上登记下游地址）：

   | 参数 | 取值 |
   | --- | --- |
   | `downstream_url` | 站点对外地址（22.4 的设置项）；未配置时按**触发本次交付的请求**推断 `scheme://host`（`X-Forwarded-Proto` 或是否 TLS 决定 scheme）；都取不到时**不提交该参数**（该实例不参与回推） |
   | `downstream_token` | 该实例的 `instances.downstream_token` |
   | `downstream_id` | 该实例的 `instances.id`（上游回推时原样带回为 `id`） |

4. 续费链路（`/host/renew`，15.5）**不重复登记**：上游主机上已有的下游地址随主机保留。

> **为什么是「触发本次交付的请求」**：交付由支付成功触发（默认在后台 goroutine 里跑），
> 下单请求（`POST /orders`）不带交付；能拿到 Host 的时机是支付回调 / 余额支付 / 管理员重试这三个触发点，
> 本系统在**请求线程内**先解析好地址再交给后台交付（契约 22.6 第 4 条）。

### 22.3 `POST /api/host/sync`（上游 → 本系统）

#### 请求

| 项目 | 约定 |
| --- | --- |
| 路径 | **`/api/host/sync`（顶层，不在 `/api/v1` 之下）**——路径由上游固定拼接（`<downstream_url>` + `/api/host/sync`），改不得 |
| 方法 / 编码 | `POST`，`application/x-www-form-urlencoded`（**form 表单，不是 JSON**） |
| 鉴权 | 请求体里的 `signature` + `rand_str`（见下方签名算法）；**没有 token / 头鉴权**，这是本接口唯一的凭证 |
| 响应 | `{"status":200,"msg":"…"}` / `{"status":400,"msg":"…"}`——**上游约定格式，不套统一响应包**（第 2 节第 5 条例外） |

入参字段（全部来自 form）：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `id` | 是 | **下游实例 ID**（= 开通时登记给上游的 `downstream_id`，即 `instances.id`）。它取自 form，因此**参与签名时是字符串** |
| `rand_str` | 是 | 随机串（上游每次请求生成；本系统只把它参与签名，不校验唯一性） |
| `signature` | 是 | 签名（32 位十六进制，**比对时大小写不敏感**） |
| `host_id` | 否 | 上游主机 ID（本系统不写库，仅用于排查） |
| `domain` / `username` / `password` / `dedicatedip` / `assignedips` / `port` / `os` / `os_url` / `nextduedate` / `domainstatus` / `suspendreason` | 否 | 主机信息（同步口径见下） |
| `type` | 否 | 推送类型（如 `create`）；本系统不区分，一律按同一套字段同步处理 |

#### 签名算法（与上游逐字符一致）

```
sign = upper( md5( json_encode( ksort({ "id": <id 原文>, "token": <该实例的 downstream_token>, "rand_str": <rand_str> }, SORT_STRING) ) ) )
```

1. **参与签名的 `id` 是收到的原始字符串**（PHP 侧 `$_POST['id']` 是字符串，`json_encode` 后带引号）：
   `{"id":"42","rand_str":"aB3xY9","token":"0123456789abcdef0123456789abcdef"}`；
2. 键按**字典序**（`id` < `rand_str` < `token`）；Go 侧用 `map[string]string` 装后 `json.Marshal`
   （map 键输出即字典序，与 PHP `ksort` 一致）；
3. **必须关闭 HTML 转义**（`json.Encoder.SetEscapeHTML(false)`）：否则 `<` / `>` / `&` 会被写成 Unicode 转义形式（`003c` 这类）而与 PHP 的 `json_encode` 不一致；
4. 结果 `md5` 后**转大写**；比对时两边统一转大写。

**golden 向量**（单测逐字符断言，见 `backend/internal/router/host_sync_rule_test.go`）：

| `id` | `rand_str` | `token` | 签名字符串 | `signature` |
| --- | --- | --- | --- | --- |
| `42` | `aB3xY9` | `0123456789abcdef0123456789abcdef` | `{"id":"42","rand_str":"aB3xY9","token":"0123456789abcdef0123456789abcdef"}` | `9840FF4B28D19ED233E2228471ABFBB1` |
| `7` | `Zx9Q2m` | `ffffffffffffffffffffffffffffffff` | `{"id":"7","rand_str":"Zx9Q2m","token":"ffffffffffffffffffffffffffffffff"}` | `FE4C0CF8F11C9455B68C4D7503D71034` |
| `1001` | `A1b2C3` | `5d41402abc4b2a76b9719d911017c592` | `{"id":"1001","rand_str":"A1b2C3","token":"5d41402abc4b2a76b9719d911017c592"}` | `D3D475B73F78EFE8785A783E5539D714` |

#### 处理与校验顺序

1. `signature` 或 `rand_str` 为空 → `{"status":400,"msg":"参数不完整"}`；
2. `id` 非正整数 / 实例不存在 / 该实例 `downstream_token` 为空 → `{"status":400,"msg":"该产品不能使用该接口"}`
   （**统一文案**，不区分「实例不存在」与「未登记」，不泄露实例是否存在；**存量老实例因此一律被拒**）；
3. 验签不过 → `{"status":400,"msg":"签名验证失败"}`；
4. 通过后按下方口径同步；成功 → `{"status":200,"msg":"更新成功"}`。

> 失败**一律 HTTP 200 + body `status=400`**：上游的下游调用统一走 `commonCurl`，它**只在 HTTP 200 时**
> 才 `json_decode` 响应体（非 200 时自己造 `{"status":400,"msg":"请求失败,HTTP状态码:…"}` 并丢掉我们的 body）——
> 因此要让上游看到「签名验证失败」这类业务原因，必须回 200 并在 body 里给 `status=400`；
> 上游据此把本次推送记为已处理（不复推），`status=200` 才记为成功。
> **不做重放窗口 / 时间戳校验**：上游只在状态变化时推送（`rand_str` 为 6 位字母数字随机串）。
>
> 上游侧口径（`app/zjmf.php` 的 `pushHostInfo` / `createSign` / `commonCurl`，只读参考）与本节的对应关系：
> 推送地址 = `rtrim(downstream_url, "/") + "/api/host/sync"`；请求体是 `http_build_query(表单数组)`（**urlencoded，
> 不是 multipart**）；登记门槛 = `downstream_url` 非空 + `downstream_token` 长度**恰好 32** + `downstream_id` 为数字；
> 签名的 `id` 取自登记时的表单字段（PHP 字符串），`nextduedate` 为空时上游会**整个不下发该字段**（与本节的
> 「未出现即不改动」口径配套）。

#### 状态映射（`domainstatus` → 本地 `status`）

比较时统一转小写（上游首字母大写、本地枚举小写）：

| 上游 `domainstatus` | 本地 `instances.status` |
| --- | --- |
| `Active` | `active` |
| `Suspended` | `suspended` |
| `Cancelled` | **不改本地状态**（只更新 `upstream_status` 原文） |
| `Deleted` | **`terminated`**（收敛到既有终止态；不改 `cancel_status`） |
| `Pending` / `Fraud` | **不改本地状态**（只更新 `upstream_status` 原文） |
| 其它未知值 | **不改本地状态**（只更新 `upstream_status` 原文） |

> `Cancelled` **刻意不映射**到本地 `cancelled`（依据 **15.8.1 的 5c 定稿**）：`cancelled` 是「保留但不再写入」
> 的预留枚举，`instances.status` 只反映主机的**真实可用状态**——上游仍在运行时即使已申请取消，
> 本地也保持 `active` / `suspended`，「取消申请在途」由 `cancel_status=pending` 表达（操作矩阵见 15.1）。
> 若把仍在运行的主机写成 `cancelled`，按 15.1（只有 `active` 可操作）会员会在主机还活着的时候失去操作能力；
> 真正的终止收敛只认 `Deleted`（→ `terminated`）与主机不存在（管理端同步 / 扫描，见 15.8.3）。

#### 同步字段与幂等

1. `upstream_status`：`domainstatus` **原文照存**（未知值也存，保证可对账）。
2. `next_due_date`：`nextduedate` **存在且非空**才更新（上游以 unix 秒给出）。
3. `dedicated_ip` / `assigned_ips` / `port` / `username` / `password`：**出现在表单里的字段按原文写入**
   （空值即清空）；**未出现在表单里的字段不改动**。`password` 是敏感字段：
   **只落库，绝不写日志、绝不写审计描述**。
4. **幂等**：状态与字段都无变化时**不写库、不写审计**（同一份推送重复到达是纯读操作）；
   有变化才落库并写**一条**审计（`instance_operation_logs`：`actor_type=system`、`actor_id=0`、
   `action=host_sync`，message 只列发生变化的字段名，**不含任何密码值**）。
5. **静默**：**不通知会员、不发站内信、不发邮件**（与交付/续费等事件通知链路完全解耦）。
6. **不校验回带的上游主机 ID / 域名**（推送不带可比的本地凭据）；`provisioning` 实例同样可以被推送更新
   （上游可能在本地交付落库之前就推 `Active`，字段照写，状态在交付完成时收敛）。

### 22.4 「站点对外地址」设置（`site.url`）

`site` 键自安装向导第 5 步写入（13.3），R16 为它补上管理端读写接口，
并把其中的 `url` 用作上游回推的下游地址（`downstream_url`，见 22.2）。

#### `GET /api/v1/admin/settings/site`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，**仅 `admin` 角色**（含读取；`finance` / `support` 返回 `403`） |
| 成功 | HTTP 200，`data` = `{url, configured, url_recommended, updated_by, updated_at}` |

| 字段 | 说明 |
| --- | --- |
| `url` | 站点对外地址（配置原值；空串 = 未配置 → 交付时按请求 Host 推断） |
| `configured` | `url != ""`（便于前端展示「未配置」） |
| `url_recommended` | 按当前请求推断的地址（`scheme://host`），供界面提示「留空时用这个」，只做提示不校验可达性 |
| `updated_by` / `updated_at` | 最后修改人 / 时间（从未写入为 `null`；向导写入时也记 `updated_by`） |

#### `PUT /api/v1/admin/settings/site`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 同上（仅 `admin`） |
| 请求体 | `{"url":"https://oem.example.com"}`——`url` **必须提供**（键缺席 / `null` → `40001`）；空串 = 清空（回到「按请求 Host 推断」） |
| 成功 | HTTP 200，`data` 为更新后的设置视图 |

校验：必须是带主机名的 `http://` 或 `https://` 地址，否则 `40002` + 可读原因；
保存时**去掉结尾斜杠**（与向导第 5 步同口径，避免拼出 `//api/host/sync`）。
**只改 `url`**：`site` 键里的 `name` / `admin_email`（向导写入）原样保留。

| code | HTTP | 场景与 message |
| --- | --- | --- |
| `40001` | 400 | 未提供 `url`（`请提供 url（留空表示按请求推断）`） |
| `40002` | 400 | `url` 非 `http(s)` 或缺少主机名（`url 必须以 http:// 或 https:// 开头，收到 "..."`） |
| `403` | 403 | 角色不是 `admin` |
| `500` / `50001` | 500 | 值损坏 / 库操作失败 |

### 22.5 角色权限矩阵（R16）

| 能力 | admin | finance | support | 会员 | 访客 / 上游 |
| --- | --- | --- | --- | --- | --- |
| `GET/PUT /admin/settings/site` | ✅ | ❌ | ❌ | ❌ | ❌ |
| `POST /api/host/sync` | —（无 token 鉴权） | — | — | — | ✅（凭签名，且仅限已登记 `downstream_token` 的实例） |

### 22.6 边界与本批未做

1. **存量老实例**（`downstream_token` 为空）不参与回推：本轮不补发 token、不做数据回填。
2. **不做商品同步、不做轮询收敛、不做自动删除**：推送只同步字段与状态；上游删除主机的收敛仍走既有
   管理端同步 / 到期扫描（15.6 / 15.8.3），收到 `Deleted` 只把本地置 `terminated`（**不删实例行**）。
3. **不防重放**：`rand_str` 只参与签名，不落库、不查重（上游侧每次请求都是新的随机串）。
4. **`downstream_url` 未配置时的推断口径**：见 22.2 第 3 条的说明（用触发交付的请求，而非下单请求）；
   两者都取不到（例如从无 Host 的内部调用触发）时该次开通**不登记下游地址**，其余开通行为不受影响。
5. **推送不做限流**：上游只在其侧状态变化时推送，且推送本身幂等。

## 23. 操作日志（R18）

三类日志各自独立成通道，写入 `operation_logs`（迁移 0019），管理端可按通道查询与**清空**；
**没有任何按条删除或修改的接口**（23.4 第 3 条）。管理端页面见 23.11（R18 批 2 落地）。

### 23.1 三个通道与留痕范围

| 通道（`channel`） | 留痕范围 |
| --- | --- |
| `admin` | 管理端**所有写操作**（POST / PUT / PATCH / DELETE）：改设置、改商品、改订单、改会员、返佣审核与打款、备份与恢复、实例管理动作、一键升级……**以及管理员登录成功与失败** |
| `member` | 会员端**所有写操作**：注册、登录、下单、支付、续费、申请终止、充值、提现与转余额、工单、改密码与资料、邀请绑定、自建券、通知已读……（口径 = 「所有非 GET 请求」，不挑） |
| `scheduler` | 定时任务**每次执行**：每轮扫描一条汇总（任务名 / 结果 / 处理条数 / 耗时 / 错误），子步骤有实际动作时另写明细（到期自动暂停 / 终止收敛 / 到期提醒） |

**不记的请求（明确口径）**：

1. 读方法（`GET` / `HEAD` / `OPTIONS`）一律不记；
2. **未命中已注册路由的请求不记**（`POST /api/v1/admin/xxx` 之类 404 不经过路由组中间件）——
   避免被扫描器刷表；
3. 渠道回调 `POST /api/v1/payments/epay/notify` 与上游回推 `POST /api/host/sync` 不记：
   它们不是「某个人」的操作（前者有 `orders`/`ledger` 对账，后者有 `instance_operation_logs` 审计）；
4. 已注册路由上**被鉴权中间件拒绝的写请求要记**（未登录 401 / 角色不足 403 同样留痕，
   操作人为 `0`/空串）——审计需要看到尝试本身。

### 23.2 数据模型（迁移 0019）

`operation_logs`：

| 列 | 类型 | 语义 |
| --- | --- | --- |
| `id` | BIGINT UNSIGNED 自增 | 仅用于展示与分页；**没有按条操作的接口** |
| `channel` | ENUM(`admin`,`member`,`scheduler`) | 通道 |
| `actor_id` | BIGINT UNSIGNED（0 = 无/系统） | 按通道解释为 `admins.id` / `members.id`；未登录尝试与定时任务为 0 |
| `actor_name` | VARCHAR(64) | **用户名快照**：账号改名或删除后日志仍可读、可搜（定时任务为空串） |
| `action` | VARCHAR(128) | 可读动作名（路由映射表 + 处理器补充；未登记路由回落 `METHOD /path`） |
| `method` / `path` | VARCHAR(10) / VARCHAR(191) | HTTP 方法与**路由模板**（如 `/api/v1/admin/products/:id`）；定时任务为空串 |
| `http_status` | SMALLINT UNSIGNED | 原始 HTTP 状态码（定时任务为 0） |
| `status` | ENUM(`success`,`failed`) | 结果：由 HTTP 状态码（2xx/3xx 成功）或任务结果推导 |
| `ip` | VARCHAR(45) | 客户端 IP（沿用仓库取客户端 IP 的实现；定时任务为空串） |
| `duration_ms` | INT UNSIGNED | 处理耗时（请求为结束耗时，定时任务为本轮耗时） |
| `summary` | VARCHAR(500) | **已脱敏**的可读业务摘要（23.7） |
| `created_at` | DATETIME（UTC） | 发生时间（入队时刻），列表一律新记录在前 |

索引：`(channel, created_at)`、`(actor_id, created_at)`、`(created_at)`，
以及 `FULLTEXT (action, summary, actor_name) WITH PARSER ngram`（23.5）。

> `instance_operation_logs`（实例操作审计，契约 15.4）**保持独立、不并入本表**：
> 那是「某台实例上发生过什么」，按实例查询；本表是「谁在系统里做过什么」。

### 23.3 埋点：中间件兜底 + 处理器补充

1. **管理端 / 会员端各挂一个中间件**（挂在鉴权中间件**之前**，故 401/403 也留痕）：
   请求结束时按「方法 + 路由模板 + 状态码」入队一条日志；中间件把待写条目放进 gin 上下文，
   **处理器可选地补充**可读动作名与业务摘要（如「修改上游设置」「已保存 SMTP 设置」）。
   这样既不漏（兜底）又可读（增强）。
2. **路由 → 中文动作名映射表集中在一个文件**（`router/oplog_actions.go`）便于维护；
   未登记路由回落成 `METHOD /path`（漏补不会漏日志）。
3. **登录**：成功与失败都记；**失败只记尝试的用户名**（`actor_name`），密码绝不落库。
   会员登录/注册同样口径。
4. **定时任务**：`ScanOnce` 每轮写一条汇总（`action=定时任务：到期扫描`，摘要含各阶段计数与错误），
   子步骤**确有实际动作**时写明细（`到期自动暂停` / `终止收敛` / `到期提醒`）；
   失败尝试不写明细（只进汇总计数，避免每轮刷屏）。

### 23.4 接口

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/admin/operation-logs` | 列表（高级搜索 + 分页，新记录在前） |
| `DELETE /api/v1/admin/operation-logs?channel=...` | **清空**（按通道或全部） |

**23.4.1 列表**

查询参数：

| 参数 | 说明 |
| --- | --- |
| `channel` | 可选：`admin` / `member` / `scheduler`；省略 = 不过滤（三通道一起查） |
| `actor_id` | 可选：按操作人精确过滤（`0` = 系统/未登录尝试） |
| `status` | 可选：`success` / `failed` |
| `keyword` | 可选：全文关键字（23.5） |
| `start` / `end` | 可选：时间范围（**闭区间**）；接受 RFC3339（`2026-10-14T00:00:00Z`）或纯日期（`2026-10-14`，按 UTC 解释；`end` 只给日期时按**当日 23:59:59** 解释，含当天）；不填 = 不设界（可查全表） |
| `page` / `page_size` | 分页：缺省 1 / 20，上限 100（与全站口径一致） |

成功 → `data`：

```json
{
  "items": [
    {
      "id": 1234,
      "channel": "admin",
      "actor_id": 1,
      "actor_name": "admin",
      "action": "修改站点地址设置",
      "method": "PUT",
      "path": "/api/v1/admin/settings/site",
      "http_status": 200,
      "status": "success",
      "ip": "203.0.113.9",
      "duration_ms": 12,
      "summary": "已保存站点对外地址设置（https://oem.example.com）",
      "created_at": "2026-10-14T07:15:00Z"
    }
  ],
  "page": 1,
  "page_size": 20,
  "total": 1234
}
```

| code | HTTP | 场景 |
| --- | --- | --- |
| `40001` | 400 | `channel` / `status` 取值非法；`actor_id` 非整数；`start`/`end` 格式非法；`keyword` 短于 2 个字符；`page_size` 越界 |
| `40002` | 400 | `start` 晚于 `end`；**关键字检索命中过多、超出执行超时时限（5 秒）被 MySQL 中断**（23.5 第 5 条，message 给出缩小范围的建议） |
| `403` | 403 | 角色不是 `admin`（`finance` / `support` 含读取一律 403） |
| `401` | 401 | 未登录 / 会员 token |
| `50001` | 500 | 数据库操作失败 |

**23.4.2 清空**

`DELETE /api/v1/admin/operation-logs?channel=admin|member|scheduler|all`

- `channel` **必填**（缺省/非法 → `40001`）；`all` 表示清空全部；
- 清空单个通道走**分批 DELETE**（每批 1000 行，避免长事务锁表）；清空全部走 **`TRUNCATE`**
  （快几个数量级，代价是**自增 `id` 归零**）；
- 清空前先冲出在途缓冲（异步写入未落库的条目），因此接口返回时被清通道**确实为空**；
- **清空动作本身留痕**（同步写入，保证返回时已落库），写入**未被清空**的通道：
  **默认 `admin`；被清空的范围包含 `admin`（清 `admin` 或清 `all`）时改写 `scheduler`**。
  即：清 `member`/`scheduler` → 留痕在 `admin`；清 `admin` → 留痕在 `scheduler`；
  **清空全部后三通道里只剩 `scheduler` 的一条清空留痕**（没有「未被清空」的通道，这是明确定下的口径）；
- 该请求本身不再由中间件重复入队（留痕只有一条）。

成功 → `data`：`{"channel":"member","deleted":123,"record_channel":"admin"}`
（`deleted` = 本次删除条数；清空全部时是 TRUNCATE 前的总条数）。

**23.4.3 没有按条删改接口（硬约束）**

**不提供**按 `id` 删除、编辑、批量删选的任何接口或路由；路由断言测试保证
`DELETE/PUT/PATCH/POST /api/v1/admin/operation-logs/:id` 一律 404。
日志只允许整体清空，清空本身留痕（23.4.2）。

### 23.5 关键字搜索：FULLTEXT + ngram（本批选择）

**选择：FULLTEXT 全文索引 + `ngram` 解析器**（MySQL 5.7 InnoDB 支持，中文可搜），
**不是**「关键字必须带时间窗 + LIKE」。理由：关键字搜索允许全表（23.4.1），
`LIKE '%…%'` 是全表扫描、随表增长线性变慢；FULLTEXT 让「无时间窗的中文关键字检索」也保持可用。

口径：

1. 索引列固定为 `(action, summary, actor_name)`，与存储层 `MATCH(...) AGAINST(...)` 的列序一致；
2. 关键字按空白拆成多个词，**每个词按短语整体匹配**（引号短语，`ngram_token_size=2` 时
   「上游设置」能命中「已保存上游设置」，但不会被「设置」单独命中而放大结果）；
   多个词之间是 **AND**（都要命中）；
3. **关键字至少 2 个字符**（中文按字、英文按字母）：`ngram_token_size` 缺省为 2，
   单字符无法被 ngram 索引；短于 2 字符的关键字接口直接 `40001` 明确拒绝，
   **不退回全表 `LIKE`**（那会随表增长拖慢站点）；
4. 索引列覆盖动作名与操作人用户名快照，因此「按动作（如 `备份`）」「按操作人用户名」也可搜。
5. **执行超时保护（R18 批 2 新增）**：关键字命中数万条时 MySQL 5.7 InnoDB 会**物化全部命中**
   （`LIMIT` 截断不了、时间窗也帮不上，实测见 23.10），单次请求会长时间占住一条数据库连接。
   因此**只有关键字检索那条路径**在 SELECT 上带 **`MAX_EXECUTION_TIME` 优化器提示**：
   计数与取页两条语句都带，值取 **5 秒**（5000 ms，即本条的契约值）。提示必须紧跟 `SELECT`
   且 `*/` 之后**留空白**——写成 `SELECT /*+ … */*` 会被 MySQL **静默忽略**（真机踩过：
   预算 1839 ms 的取页语句照跑 3.17 s）。
   
   超时由 MySQL 中断该语句（错误码 **3024**），**连接不被杀掉、表数据不受影响**；接口把这种情况
   翻译成**业务错误 `40002`**（**不是 500**），message 为：**「关键字命中过多，检索超出执行超时：
   请缩小范围（加时间窗或通道过滤），或换更精确的关键字」**——用户拿到的是「换更精确的关键字」，
   而不是 500 或无限等待。
   
   **这条保护的能力边界（真机实测，如实写明；见 23.10）**：MySQL 5.7 InnoDB 的**全文索引物化
   阶段本身不可中断**，超时只在阶段边界的检查点生效——所以它买到的不是「把某条语句切成 5 秒」，
   而是两点：**① 第一条语句超预算时，第二条语句根本不再跑**（12 万命中实测：单条语句 22 s，
   无保护时两条共约 44 s，有保护时约 22 s 就以可读错误收场）；**② 极端检索的结局是可读的
   `40002`，而不是 500 或空等**。命中数在 5 秒可跑完的区间内（约 7 万条以内，如 4.5 万条
   整请求 6.5 s）**照常返回结果**——结果本来拿得到就不该报错。
   其余列表/筛选（无关键字）**不加提示、保持原样**：它们本就是毫秒级（23.10 的 ①②③b④⑥）。

### 23.6 性能与生命周期（不能因为日志多而卡）

1. **写入异步、不阻塞业务请求**：中间件/调度器只做**非阻塞入队**（缓冲 4096 条），
   后台协程攒批（至多 100 条或 200 毫秒）一条多行 INSERT 落库；
   **不参与任何业务事务**；写失败只记服务日志（条目丢弃，不影响业务结果）；
   **缓冲满时丢弃新条目并记服务日志**（绝不反压业务请求）；服务关闭时 `Close()` 冲出剩余缓冲。
2. **清空实现见 23.4.2**（分批 DELETE / TRUNCATE；清空前 Flush）。
3. **索引齐备**（`(channel, created_at)` / `(actor_id, created_at)` / `(created_at)` + FULLTEXT）：
   通道筛选 + 时间倒序、按操作人筛选、纯时间倒序、关键字检索都是索引可用路径。
4. **不做自动清理**（Langford 明确选择）：**表会随业务持续增长，运维靠管理员手动清空**；
   不做定时删除、不做滚动归档、不做告警。
5. **备份与恢复**：本表随整库一起备份与恢复；R18 同时修正了恢复端对
   **MySQL 可执行注释**（`/*!50100 WITH PARSER `ngram` */`）的处理——它不会被当普通注释剥掉，
   否则恢复出来的 `operation_logs` 会**静默丢失 ngram 解析器**、中文关键字检索随之失效
   （`internal/dbbackup` 有专门用例覆盖）。

### 23.7 脱敏口径（硬要求）

**绝不落库**：密码（登录/改密/恢复确认）、API 密钥（上游 `api_key`、易支付 `key`）、
JWT / token、SMTP 口令、第三方账号原文（提现收款账号）、备份文件内容。

1. **请求体整体不落库**：只落「方法 + 路由模板 + 状态码 + 耗时 + IP + 动作名 + 业务摘要」；
2. 处理器补充的摘要只由服务端已知字段（ID、用户名、开关、金额、周期、表数……）拼装，
   不复制请求体；密钥类字段**只记「是否更换」**（绝不记值）；
3. 第二道闸门：写入前对摘要做防御性替换，把形如 `password=…` / `api_key=…` / `token=…` 的片段
   替换为 `***`（只认 ASCII 密钥名 + 等号/冒号，不误伤中文文案）；
4. 管理员登录**失败**记录里只记**尝试的用户名**；
5. 字段按列宽**按字符截断**，保证不会因超长写失败。

### 23.8 角色权限矩阵（R18）

| 能力 | admin | finance | support | 会员 | 访客 |
| --- | --- | --- | --- | --- | --- |
| `GET /admin/operation-logs` | ✅ | ❌ 403 | ❌ 403 | ❌ 401 | ❌ 401 |
| `DELETE /admin/operation-logs` | ✅ | ❌ 403 | ❌ 403 | ❌ 401 | ❌ 401 |

**读与清空都仅 `admin`**（`finance` 不行）。

### 23.9 边界与本批未做

1. **前端页面**：批 1 只出接口；R18 批 2 已补齐（侧栏「日志」三项 + 日志页，见 23.11）。
2. 不做按条删除或修改（23.4.3）、不做导出、不做自动清理与归档、不做日志告警。
3. 定时任务的**失败尝试**只在汇总的计数与错误里体现，不写明细（避免每轮刷屏）。
4. `actor_id=0` 的条目有两种：未登录/凭证无效的写请求尝试，以及定时任务与系统动作。
5. 缓冲满被丢弃的条目**不可恢复**（只留一条服务日志），这是「日志不反压业务」的代价。
6. 未安装（安装向导）阶段的请求不进本表：正常模式引擎尚未构建。

### 23.10 真机实测（2026-10-14，独立临时库 + 真实二进制，5 万条日志）

性能实测（表内 **50,003** 条，其中约 85% 的合成数据命中同一关键字「上游设置」——这是刻意构造的
**最坏情形**；接口每次请求 = 计数 + 取页两次查询，下表为 HTTP 端到端耗时，各跑 3 次）：

| 场景 | 命中条数 | 耗时（3 次） |
| --- | --- | --- |
| ① 无时间窗的列表第 1 页（`page=1&page_size=20`，全表） | 50,003 | **13 / 17 / 14 ms** |
| ② 中文关键字「上游设置」（无时间窗） | 42,857 | **5.88 / 6.12 / 5.94 s** |
| ③ 中文关键字「备份」+ 12 小时时间窗 | 6,481 | **74 / 71 / 74 ms** |
| ③b 仅时间窗（无关键字，会员通道） | 15,127 | **18 / 20 / 19 ms** |
| ④ 操作人 + 结果过滤（`actor_id=7&status=failed`） | 90 | **17 / 17 / 15 ms** |
| ⑤ 关键字「上游设置」+ 12 小时时间窗 | 38,891 | **6.2 / 6.0 / 6.0 s** |
| ⑥ 极少命中的关键字「清空通道」 | 0 | **4.7 / 4.7 / 4.2 ms** |

结论与口径（**如实记录，不掩饰**）：

1. **耗时与「命中集合大小」成正比**（约 0.14 ms / 命中条）：命中几百到几千条是几十毫秒，
   命中数万条要几秒；**与表大小、与是否有时间窗无关**——`LIKE`-式全表扫描在 5 万条时反而更快，
   但那条路会随「表持续增长」线性变慢，而本表**没有自动清理**（23.6 第 4 条），所以选 FULLTEXT。
2. **MySQL 5.7 InnoDB FULLTEXT 的实现特性**（已实测确认）：`LIMIT` **不能**提前截断
   （`... WHERE MATCH(...) LIMIT 10` 仍要 2.8 s，`Ft_hints: no_ranking` 会物化全部命中）；
   `OPTIMIZE TABLE` 也不改变量级。这不是索引缺失——①③③b④ 都是毫秒级，索引路径齐备。
3. **使用建议**（前端已体现，见 23.11）：关键字搜索配时间窗或通道过滤；命中数万条的泛词搜索
   本身没有参考价值——R18 批 2 起这类检索带 **5 秒执行超时**（23.5 第 5 条），超预算时回
   可读的 `40002` 而不是让请求一直等下去（批 1 实测的 5.9–6.2 s 正是因为当时没有任何执行时限）。
4. **批 2 的执行超时实测（独立临时库 + 真实二进制，同口径）**：
   - 命中 **4.5 万**：计数 **3.21 s** + 取页 **3.32 s** = 整请求 **6.5 s**，两条都在 5 秒内
     → **照常返回结果**（结果拿得到就不该报错）；
   - 命中 **12 万**：单条语句 **22 s**，预算 5 秒的超时提示在**物化阶段结束时**才被检查到
     → 请求在 **约 22 s** 以 `40002` 收场（无保护时要跑完两条、约 44 s 才拿到一个没人看的页）。
     **阶段本身不可中断**是 MySQL 5.7 InnoDB FTS 的实现事实（换更小的预算也只会更早地「在阶段
     边界决定放弃」，不会更快掐断）；这条保护的收益是「不再跑第二条 + 失败可读」，已如实写在 23.5。
   - 无关键字的列表与筛选在同一实测里仍是 **38 ms / 76 ms**，未受任何影响。
5. 清空实测：会员通道 33,338 条**分批 DELETE 1.6 s** 完成；清空全部（TRUNCATE）后自增 ID 归零、
   三通道只剩一条 `scheduler` 留痕；按 `id` 的删除/修改路由一律 404（23.4.3）。

端到端造数与断言（真实 HTTP）：管理员登录失败（只记尝试的用户名）/ 成功、修改站点地址（带摘要）、
未登录写请求 401 留痕；会员注册 / 登录 / 登录失败 / 下单 / 取消订单；定时任务首轮扫描
（启动后 1 分钟，`上游未配置` 作为错误进汇总）；读写口径（GET 不产生日志）逐条核对通过。

### 23.11 管理端页面（R18 批 2）

页面只消费 23.4 的两个接口，**不新增任何端点**；权限沿用 23.8（仅 `admin`）。

1. **侧栏「日志」入口（可展开）**：点击展开三个子项 —— **用户日志**（`channel=member`）、
   **定时任务日志**（`channel=scheduler`）、**管理员日志**（`channel=admin`），
   三项都是同一个日志页 `/admin/logs` 的带参链接（`?channel=…`）。
   `finance` / `support` **不渲染该入口**；直接敲 URL 时页面给出统一的无权占位，
   且**不发起任何请求**（23.8 的前端侧配合，服务端仍独立 403）。
2. **日志页 `/admin/logs`**：
   - **URL 参数预置**：`channel` 与 `actor_id` 从查询串读取并预置为筛选条件，
     因此链接**可分享、可刷新**；换通道（含从侧栏另一子项进入）回到第 1 页；
   - **表格列**：时间（UTC 输入按**本地时区**展示，与全站时间展示口径一致）、
     操作人（`actor_name`，空串显示占位文案）、动作、方法 + 路径、结果
     （成功 / 失败，**失败标红**）、IP、耗时（毫秒）、摘要；
   - **高级搜索**：关键字（**提示至少 2 个字符**，单字符由后端 `40001` 拒绝，见 23.5）、
     时间范围（起 / 止，均可留空 = 不设界）、结果（全部 / 成功 / 失败）、通道、
     操作人（`actor_id`，非负整数；`0` = 系统 / 未登录尝试）；
     另有一键清空全部筛选条件；
   - **命中较多的提示**：带了关键字且命中条数 **≥ 1000** 时，提示「命中 N 条，建议加时间窗或
     通道过滤以加快检索，或换更精确的关键字」（对应 23.10 的使用建议）；关键字检索超时
     （23.5 第 5 条）时按 `40002` 的 message 原样展示，提示收窄条件；
   - **分页**：`page` / `page_size`，每页条数与全站管理端列表一致（20）。
3. **清空（只有整体清空）**：
   - 「清空当前通道」：**按当前通道**清空（`DELETE ...?channel=<当前通道>`），经二次确认；
     当前未选定具体通道（在看全部通道）时该按钮**禁用**并说明原因（避免误清全部）；
   - 「清空全部」：`DELETE ...?channel=all`，**更强的二次确认**，文案写明
     **清空后不可恢复**、且自增 `id` 会归零（23.4.2）；
   - 清空成功后刷新列表（被清通道确实为空，见 23.4.2）。
4. **没有按条删除或编辑的入口**：表格行内不提供任何删除 / 编辑 / 批量删选控件——
   与 23.4.3 的硬约束一致（后端也没有对应接口）。
5. **会员页的直达入口**：后台「会员」页每行提供「日志」入口，跳转到
   `/admin/logs?channel=member&actor_id=<该会员 ID>`，即「这个会员在系统里做过什么」。

## 24. 缓存（R19 批 1）

**本批只做缓存基础设施**：抽象层 + 两种驱动（文件 / Redis）+ 后台设置 + 状态 / 重置 / 清空接口
+ 管理端设置页的缓存卡片。**不接入任何业务读路径、不改变任何既有业务行为**——
把缓存接到具体读路径、以及写操作主动失效的接线，留给批 2（24.6 先把范围口径与失效策略定稿）。

三条硬边界（全批通用）：

1. **缓存读写失败绝不让业务接口报错**：读失败按「未命中」处理并记服务日志；写失败只记服务日志，
   调用方不得因缓存问题改变业务结果；
2. **统计不参与业务事务**：命中 / 未命中是进程内累加，统计读写失败不影响任何业务；
3. **只清自己的缓存**：Redis 的清空只删本系统 `key_prefix` 下的键，**绝不 FLUSHDB**
   （同一个 DB 里可能有别的应用的数据）。

### 24.1 两种驱动与默认口径

| 驱动 | 默认 | 说明 |
| --- | --- | --- |
| `file`（文件缓存） | ✅ **默认** | 位置固定为系统临时目录下的 `lyidc-cache/`（如 `C:\Users\<用户>\AppData\Local\Temp\lyidc-cache\`、`/tmp/lyidc-cache/`）；**无需任何配置**；目录不存在自动创建；启动时**不需要预热** |
| `redis` | ❌ 默认关闭 | 由管理员在后台选择并填连接参数后启用；不可用时**自动降级**到文件缓存（24.5） |

两种驱动由同一个抽象层承载（`internal/cache`：`Store` 接口 + 文件实现 + Redis 实现 + `Manager`），
**运行时可切换**：保存设置后立即生效（管理端接口每次读写设置都读一次 settings 表并按内容失效，
与 12.1 的机制一致），**不需要重启服务**。

### 24.2 驱动实现口径

**文件缓存**（`internal/cache/file.go`）：

1. **键 → 文件名用哈希**：`<目录>/<sha256(键) 十六进制>.cache`。键**完全不参与文件名**，
   因此任何键（含 `/`、`\`、`..`、盘符、Windows 保留名、超长键、Unicode）都不可能逃出缓存目录，
   也不会因非法字符写失败（有专门用例断言）；
2. **值按字节原样存**，文件头 16 字节：magic `LYIDCC01`（8 字节）+ 过期时刻
   （Unix 纳秒大端，8 字节；**0 = 永不过期**）。过期判定以**文件头**为准，不依赖 mtime
   （mtime 会被复制 / 解包 / 备份改写）；
3. **写入原子替换**：同目录临时文件（`.tmp-*`）写完 `close` 再 `rename` 覆盖目标；
   读者以「容许并发删除」的共享模式打开（Windows 上否则会与替换 / 删除互斥），
   同一键的写入与删除按哈希分片锁串行；
4. **删除策略**：惰性删除（读到过期 / 头部损坏的文件立即删）+ 定期清理
   （后台协程每 5 分钟扫一遍目录，只删过期的 `.cache` 文件；目录里其它文件绝不碰）；
5. **清空**：删除本驱动写的全部 `.cache` 与残留 `.tmp-*`，目录里其它文件不动。

**Redis 缓存**（`internal/cache/redis.go`）：

1. 用官方客户端 **`github.com/redis/go-redis/v9`**（当前 v9.20.0）；
2. 连接参数与缺省值见 24.3；**超时短**（缺省 1 秒）——拨号、读、写、连接池排队都用同一个超时；
3. **不做命令重试、拨号也只试一次**：缓存层宁可立刻失败并降级，也不把 1 秒的预算放大成数秒拖住请求
   （重试会把「连接被拒绝」拖成「连接超时」，既慢又不好诊断）；
4. 连接池：最大 16 条连接、常驻 1 条空闲连接；
5. **只操作本系统的命名空间**：读写键都带 `key_prefix`；清空与键数量都用
   `SCAN MATCH <prefix>*`（键数量按集合去重），**永不 FLUSHDB**；
6. 状态接口回显服务端版本（`INFO server` 的 `redis_version`；服务端不报该字段时为空串）
   与最近一次成功 `PING` 的往返耗时。

### 24.3 设置接口（仅 `admin`）

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/admin/settings/cache` | 读取缓存设置 + 当前运行状态 |
| `PUT /api/v1/admin/settings/cache` | 局部更新（至少一个字段），**改完立即生效** |

**24.3.1 读取** → `data`：

```json
{
  "driver": "file",
  "redis_enabled": false,
  "effective_driver": "file",
  "degraded": false,
  "degraded_reason": "",
  "file_dir": "C:\\Users\\Administrator\\AppData\\Local\\Temp\\lyidc-cache",
  "redis": {
    "host": "",
    "port": 6379,
    "password_configured": false,
    "password_masked": "",
    "db": 0,
    "key_prefix": "lyidc:",
    "timeout_seconds": 1
  },
  "updated_by": null,
  "updated_at": null
}
```

- `driver` 是**配置选择**的驱动；`redis_enabled` 是启用状态（等价于 `driver == "redis"`，
  供界面直接展示「Redis 已启用 / 未启用」）；
- `effective_driver` / `degraded` / `degraded_reason` 是**当前运行状态**（口径见 24.5）；
- `file_dir` 是文件缓存目录（**无论当前是否生效都给**，便于排障）；
- `redis.password_*` 只回配置标志与掩码（`settings.MaskSecret`：前 4 位 + `****`），**永不回显明文**；
- 读取同时是一次**自愈时机**：处于降级时重新探活 Redis，恢复可用即切回（24.5）。

**24.3.2 保存** 请求体（字段均可选，但**至少一个**）：

```json
{
  "driver": "redis",
  "redis": {
    "host": "127.0.0.1",
    "port": 6379,
    "password": "**三态**",
    "db": 0,
    "key_prefix": "lyidc:",
    "timeout_seconds": 1
  }
}
```

参数与缺省值：

| 字段 | 缺省 | 取值与校验 |
| --- | --- | --- |
| `driver` | `file` | 只能是 `file` / `redis`；`redis` 表示启用 Redis |
| `redis.host` | 空 | 不含端口；**`driver=redis` 时必填**（缺 → `40002`）；≤ 255 字符、不含控制字符 |
| `redis.port` | `6379` | 1-65535（留空 / `0` = 缺省 6379） |
| `redis.password` | 空 | **三态语义**（与其他密钥一致）：**省略 = 保持不变、给值 = 替换、空串 = 清空** |
| `redis.db` | `0` | 0-15 |
| `redis.key_prefix` | `lyidc:` | ≤ 64 字符、不含控制字符；留空 = 缺省前缀 |
| `redis.timeout_seconds` | `1` | **1-10**（短超时口径；留空 = 缺省 1） |

校验规则（**非法一律 `40002` + 可读原因，且不落库**）：驱动取值合法、host / port 合法、
port 范围、db 范围、超时范围、前缀形状；请求体本身不是合法 JSON 或字段类型不符仍是 `40001`。

保存成功的响应与 24.3.1 同形状（带 `updated_by` / `updated_at`），并**当场应用**到缓存管理器：
配置为 Redis 时先做一次 `PING` 探活，失败即标红降级（响应里 `degraded` 即为 `true`）。

**24.3.3 权限与留痕**：两个接口都**仅 `admin`**（含读取；`finance` / `support` → `403`，
会员 / 未登录 → `401`）；保存写操作日志摘要（只记驱动与目标地址、**是否更换口令**，口令明文绝不落库）。

### 24.4 状态与运维接口（仅 `admin`）

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/admin/cache/status` | 运行状态：驱动 / 降级与原因 / 键数量 / 命中率 / 连接信息 |
| `POST /api/v1/admin/cache/reset-stats` | **重置命中统计**（进程内累计，重启本就归零） |
| `POST /api/v1/admin/cache/flush` | **清空缓存**（排障用，**只清缓存、不碰业务数据**） |

**24.4.1 状态** → `data`：

```json
{
  "driver": "redis",
  "effective_driver": "file",
  "degraded": true,
  "degraded_reason": "连接被拒绝（127.0.0.1:6399）",
  "keys": 12,
  "hits": 30,
  "misses": 5,
  "hit_rate": 85.71,
  "stats_since": "2026-10-14T07:10:00Z",
  "file": { "dir": "C:\\Users\\Administrator\\AppData\\Local\\Temp\\lyidc-cache" },
  "redis": {
    "host": "127.0.0.1", "port": 6399, "db": 0, "key_prefix": "lyidc:",
    "timeout_seconds": 1, "version": "", "latency_ms": 0
  }
}
```

- `keys` 是**当前生效驱动**的键数量，用驱动自己的口径：文件缓存数目录里**未过期**的数据文件；
  Redis 用 `SCAN MATCH <prefix>*` 计数（去重，**不含同一个 DB 里别的应用的键**）；
- **命中率 = 进程内累计**：命中 = 读到未过期的值；未命中 = 不存在 / 已过期 / **读失败按未命中处理**；
  `hit_rate = hits / (hits + misses) × 100`，保留两位小数，没有请求时为 `0`；
  **重启归零**，可由 `reset-stats` 重置（`stats_since` 是统计起点）；
- `redis.version` / `latency_ms` 来自最近一次成功探活（服务端不报版本时为空串；
  `latency_ms` 是往返毫秒，**Windows 计时精度下可能为 0**）；**绝不含口令**；
- 该接口同时是一次**探活**（24.5 的自愈时机）。

**24.4.2 重置统计** → `data`：`{"hits":0,"misses":0,"hit_rate":0,"stats_since":"…"}`。
只清统计，**不动缓存内容**。

**24.4.3 清空缓存** → `data`：`{"driver":"file","flushed":true,"keys":0}`（`keys` 为清空后的键数量）。
清空**只清缓存**——不碰任何业务数据（数据库里的商品 / 订单 / 设置一概不动）；
Redis 只清本前缀命名空间；**降级期间**（生效驱动是文件缓存）会额外**尽力清一次配置的 Redis**
（避免恢复后命中残留旧值），这一步失败只记日志、不影响返回。

**24.4.4 错误码**：

| code | HTTP | 场景 |
| --- | --- | --- |
| `40001` | 400 | 请求体非法 / 字段类型不符 / 保存时一个字段都没提供 |
| `40002` | 400 | 设置校验失败（驱动取值、host、端口范围、db 范围、超时范围、前缀形状） |
| `403` | 403 | 角色不是 `admin`（`finance` / `support` 含读取一律 403） |
| `401` | 401 | 未登录 / 会员 token |
| `50001` | 500 | 数据库操作失败 |
| `500` | 500 | 库内设置值损坏；清空缓存失败 |

### 24.5 降级口径（启用 Redis 但不可用）

1. **不可用判定**（任一即算）：连不上（连接被拒绝 / 重置 / DNS 失败）· **认证失败** ·
   **超时** · `PING` 失败；
2. **行为**：**自动降级到文件缓存**，业务不中断；状态里 `degraded=true`
   与 `degraded_reason`（可读原因，如「连接被拒绝（127.0.0.1:6399）」「认证失败」「连接超时」），
   管理端界面**标红**并显示原因。判定发生在两处：保存设置时（应用配置即探活）与
   运行期 Redis 出错时（读失败按未命中处理并降级）；
3. **自愈**：**下次读取缓存设置**（`GET`/`PUT /admin/settings/cache`）或**下次探活**
   （`GET /admin/cache/status`）时重试 Redis，恢复可用即**自动切回 Redis，不必重启**；
4. 降级期间缓存读写照常工作（落在文件缓存上），键数量与统计也随之切换到文件缓存的口径。

### 24.6 缓存范围口径（批 2 才接线，本批先定稿）

**允许缓存**：商品与分类（列表 / 详情 / 分组树）、站点设置读取、首页与仪表盘统计、
上游只读数据（**短 TTL**）。

**不缓存**：实例与订单状态、余额与账目、通知未读数、日志、上游写操作结果
（这些要么随用户操作即时变化、要么是审计证据，缓存它们只会制造不一致）。

**失效策略**：**写操作主动失效**（相关写接口在成功提交后删除对应键）+ **TTL 兜底**
（每个键都有 TTL，即使漏删也会自然过期）。本批已把机制备好（`Manager.Delete` / `Flush` /
注入点），批 2 才把读写路径接上。

### 24.7 边界与本批未做

1. **不接线**：本批不加任何业务缓存读写点，因此**不改变任何现有业务行为**；
2. 不做本地内存层 / 多级缓存、不做容量上限与 LRU 淘汰（靠 TTL 与手动清空）、
   不做缓存预热、不做集群模式（Sentinel / Cluster）、不做缓存内容加密；
3. 文件缓存在多实例部署下**各实例独立**（临时目录本地），Redis 驱动才是多实例共享缓存的口径；
4. `Flush` 与 `reset-stats` 是**排障手段**：清空只清缓存、统计只清计数，都不触碰业务数据；
5. 设置项属于「用户可设置」内容，**只存 settings 表**（键 `cache`），不进 `config.yaml`（12.1）。

## 25. 变更记录

| 日期 | 版本 | 变更内容 |
| --- | --- | --- |
| 2026-10-14 | v31 | R19 批 1 缓存基础设施（**新增第 24 节「缓存」，变更记录顺延为第 25 节；12.1 补 `cache` 设置键；无迁移、不改任何既有接口**）：**① 两种驱动与默认口径（24.1）**——**默认文件缓存**（位置固定为系统临时目录下的 `lyidc-cache/`，**无需任何配置**、目录不存在自动创建、启动不需预热）；**Redis 默认关闭**，由管理员在后台选择驱动并填参数后启用；两种驱动由同一抽象层承载（`internal/cache`：`Store` 接口 + 文件实现 + Redis 实现 + `Manager`），**运行时可切换、保存即生效、不用重启**。**② 驱动实现口径（24.2）**——文件缓存：键 → `<sha256>.cache` **哈希文件名**（任何键都逃不出缓存目录、也不会因非法字符写失败）、值按字节存、头部 16 字节记过期时刻（**0 = 永不过期**，不依赖 mtime）、**同目录临时文件 + rename 原子替换**、**惰性删除 + 每 5 分钟定期清理**、清空只删自己的 `.cache` 与残留 `.tmp-*`；Windows 上读者以**容许并发删除**的共享模式打开、同一键的写入与删除按哈希分片锁串行（否则「边读边替换」会撞出 ACCESS_DENIED，实测踩过）；Redis：官方客户端 **`github.com/redis/go-redis/v9`（v9.20.0）**、连接池 16 + 常驻 1 空闲、**超时短**（缺省 1 秒，可调 1-10）、**不做命令重试且拨号只试一次**（否则「连接被拒绝」会被退避拖成「连接超时」）、**只操作 `<key_prefix>*` 命名空间**（键数量 SCAN 去重计数、清空 SCAN+分批 DEL，**绝不 FLUSHDB**）。**③ 设置接口（24.3）**——`GET/PUT /api/v1/admin/settings/cache`（**仅 admin，含读取**）：驱动选择 `file`/`redis` + Redis 参数 `host`（`driver=redis` 时必填）/ `port`（1-65535，缺省 6379）/ `password`（**三态**：省略=保持、给值=替换、空串=清空）/ `db`（0-15）/ `key_prefix`（缺省 `lyidc:`）/ `timeout_seconds`（1-10，缺省 1）；**非法一律 `40002` + 可读原因且不落库**；保存响应即含运行状态（配置为 Redis 时当场 PING 探活，失败即标红降级）；密钥只回 `password_configured` 与掩码，**永不回显明文**。**④ 状态 / 重置 / 清空（24.4）**——`GET /admin/cache/status`（驱动 / 生效驱动 / **降级与可读原因** / **键数量** / **命中率**与命中·未命中 / 统计起点 / 文件目录或 Redis 连接信息（**不含口令**，可含版本与连接耗时））、`POST /admin/cache/reset-stats`、`POST /admin/cache/flush`（**只清缓存、不碰业务数据**；Redis 只清本前缀；降级期间额外**尽力清一次配置的 Redis**），**三个接口都仅 admin**；**命中率 = 进程内累计**（命中=读到未过期值，未命中=不存在/过期/读失败按未命中处理；`hit_rate = hits/(hits+misses)×100` 两位小数；**重启归零**、可重置）。**⑤ 降级口径（24.5）**——不可用判定 = **连不上 / 认证失败 / 超时 / PING 失败**；行为 = **自动降级到文件缓存、业务不中断**，状态 `degraded=true` + 可读原因（如「连接被拒绝（127.0.0.1:6399）」），界面**标红**；**自愈 = 下次读取设置或下次探活时重试 Redis，恢复即切回（不必重启）**。**⑥ 范围与失效（24.6，批 2 接线、本批先定稿）**——**允许缓存**：商品与分类（列表/详情/分组树）、站点设置读取、首页与仪表盘统计、上游只读数据（短 TTL）；**不缓存**：实例与订单状态、余额与账目、通知未读数、日志、上游写操作结果；**失效策略 = 写操作主动失效 + TTL 兜底**。**⑦ 边界（24.7）**——**本批不接任何业务读路径、不改变任何既有行为**；不做内存层/多级缓存、不做容量上限与 LRU、不做预热、不做集群（Sentinel/Cluster）、不做内容加密；文件缓存在多实例下各实例独立。**⑧ 管理端界面**——设置页新增「缓存」分区：驱动选择、Redis 参数、运行状态（键数量/命中/未命中/命中率/统计起点/目录或连接信息）、**降级标红与原因**、重置统计与清空缓存（二次确认）。**测试**：`go test ./...` 全量绿（后端 610 → **654** 个 `func Test`：`internal/cache` 新增 **28** 个——文件驱动的 Set/Get/TTL 惰性过期/定期清理/删除/清空/键数量/并发读写/**路径穿越与特殊键**（`../`、盘符、保留名、4 KB 键、Unicode 全部落在哈希文件名里）/损坏文件/1 MiB 值/探活建目录，Manager 的默认文件驱动与统计、命中率四舍五入、重置、**驱动切换**、**降级（不可达 → 连接被拒绝、口令错 → 认证失败、超时分类）**、**自愈**、**运行期故障降级**、清空只清本命名空间、降级期间清空也清 Redis、Redis 连接信息与版本解析、配置未变不重建驱动、未配置/文件驱动故障的安全降级；`internal/settings` 新增 **8** 个——缺省口径、三态口令、校验边界逐项、文件驱动允许空 host、落库 JSON 往返、损坏值、旧值归一化；`internal/router` 新增 **8** 个——**五个接口的角色矩阵**（finance/support 403、会员/未登录 401）、读写与掩码/三态、校验 40002 且不落库、**状态/重置/清空端到端**、**降级 + 业务接口照常 + 自愈**、**miniredis 端到端**（键前缀、只数本命名空间、清空不动别人的键）、写接口的**操作日志动作名**、路由形状）；前端 `npm test` 全量绿（**346 → 356 例 / 43 → 44 文件**，新增 `AdminSettingsCache.test.tsx` 10 例——默认文件缓存展示、**降级标红与原因**、Redis 连接信息、切驱动保存的请求体（口令保持时不提交该键）、**口令三态**、就地校验（host 空 / 端口越界都不发请求）、后端 40002 展示原因、重置统计、清空缓存的二次确认与键数量归零）、`npm run build` 通过；**真机验证（独立临时库 + 真实二进制 + 进程内 miniredis，61 项断言）全部通过**——默认文件缓存的驱动/目录/键数量/命中率可读且目录自动创建、五个接口的 401/403 矩阵、五类非法设置全部 40002 且不落库、切 Redis + 不可达地址 → **degraded=true + 「连接被拒绝（127.0.0.1:端口）」**且业务接口照常、切 Redis + miniredis → 生效 redis / 键数量只数本前缀（3 个本前缀 + 1 个别人的键 = 3）/ 清空后 0 且别人键仍在 / 业务数据未动、重置统计归零、**自愈**（服务起来后下一次状态请求即切回）、**口令三态与掩码**（响应/状态/全量操作日志都不含明文）、操作日志动作名、清理归零（临时库删除、文件缓存目录无残留）。 |
| 2026-10-14 | v30 | R18 批 2：操作日志前端 + 关键字检索**执行超时保护**（**23.4.1 错误表补一行、23.5 补第 5 条、23.10 结论改口径、新增 23.11「管理端页面」；无迁移、除下面第 ① 条外接口形状与语义不变**）：**① 关键字检索的执行超时（23.5 第 5 条，本批唯一的后端行为变化）**——背景是批 1 的真机实测（23.10）：**无时间窗列表 42 ms、纯时间窗筛选 65 ms 都很快，但中文关键字命中 4.3 万条要 5.9–6.2 s**（MySQL 5.7 InnoDB 全文索引**物化全部命中**，`LIMIT` 截断不了、加时间窗也帮不上——命中 3.9 万条仍要 6.0 s），极端情况下单个搜索请求会长时间占住一条数据库连接。修法：**只给关键字检索那条路径**（计数 + 取页两条语句）在 SELECT 上加 **`MAX_EXECUTION_TIME` 优化器提示，取 5 秒**（`/*+ MAX_EXECUTION_TIME(5000) */`），由 MySQL 中断超时语句（错误码 3024），**连接不被杀、数据不受影响**；接口把这种情况翻译成**业务错误 `40002`**（**不是 500**）：**「关键字命中过多，检索超出执行超时：请缩小范围（加时间窗或通道过滤），或换更精确的关键字」**，让用户立刻收窄条件重试。**能力边界如实写在 23.5**（真机实测定稿）：MySQL 5.7 InnoDB 的**全文索引物化阶段不可中断**，超时只在阶段边界被检查到——4.5 万命中（两条各约 3.2 s）照常返回结果（整请求 6.5 s），**12 万命中**则单条语句 22 s、请求在约 22 s 以可读 `40002` 收场（无保护时两条要跑约 44 s）；这条保护的收益是「**不再跑第二条语句 + 失败可读**」，不是把单条语句切成 5 秒（当前 MySQL 版本做不到，已写明）。踩到的坑同样入档：提示的 `*/` 必须留空白再跟内容，写成 `SELECT /*+ … */*` 会被 MySQL **静默忽略**（预算 1839 ms 的取页语句照跑 3.17 s），已用单测把语句形态钉死。**其余列表/筛选（无关键字）不加提示、SQL 与执行计划保持原样**——它们本就是毫秒级（23.10 的 ①②③b④⑥，批 2 复测 38 ms / 76 ms），没有理由为它们牺牲执行计划。**② 管理端页面（23.11，前端）**——侧栏新增**可展开的「日志」入口**，点击展开**用户日志**（`channel=member`）/ **定时任务日志**（`channel=scheduler`）/ **管理员日志**（`channel=admin`）三项，分别进入同一个日志页 `/admin/logs` 并预置通道；**仅 `admin` 可见**，`finance` / `support` 连直接敲 URL 也只得到无权占位且**不发任何请求**。日志页：表格八列（时间按**本地时区**展示 / 操作人 `actor_name`（空串给占位）/ 动作 / 方法 + 路径 / 结果（成功 / 失败，**失败标红**）/ IP / 耗时 / 摘要）、`page` / `page_size` 分页（每页 20，与全站管理端口径一致）、**高级搜索**（关键字——输入框提示**至少 2 个字符**；时间范围起 / 止可留空 = 全表；结果全部 / 成功 / 失败；通道；操作人 `actor_id`）+ **一键清空筛选**、命中较多时提示「命中 N 条，建议加时间窗或通道过滤以加快检索」（对应 23.10 的使用建议，关键字超时则原样展示 23.5 第 5 条的 `40002` 文案）、加载 / 空 / 错误态沿用全站组件；**URL 预置**：`channel` 与 `actor_id` 从查询串读取，链接**可分享、可刷新**（侧栏三项就是带 `?channel=` 的链接）；**清空**只有整体清空——「清空当前通道」（二次确认；未选定具体通道时禁用并说明原因）与「清空全部」（**更强确认 + 写明不可恢复与自增归零**）；**表格行内没有任何按条删除 / 编辑 / 批量删选控件**（与 23.4.3 的硬约束一致，后端也没有对应接口）。**③ 会员页直达**——后台「会员」页每行新增「日志」入口，跳 `/admin/logs?channel=member&actor_id=<会员 ID>`。**④ 明确不做**——不加任何自动清理（仍只靠手动清空）、不动 `instance_operation_logs`（15.4 两套并存）、不改 R16 回推与 R17 上游地址逻辑、不提供导出。**测试**：`go test ./...` 全量绿（后端 605 → **610** 个 `func Test`：`internal/store` 新增 4 个——执行超时提示的形态（值、必须 `*/` 加空格结尾）、**语句形态断言**（计数与取页两条 SQL 的前缀，钉死 `*/*` 那个坑）、超时错误的分类（合成 MySQL **3024** 判为超时、其它错误不误判）、超时文案可读性；`internal/router` 新增 1 个真机用例——造 1.2 万条命中同一中文关键字的日志，先量基线，再把预算压到基线的 1/8，断言接口回 **40002 + 可读提示**（不是 500）、无关键字的列表/筛选把预算压到 1 毫秒也照常返回）；前端 `npm test` 全量绿（**328 → 346 例 / 42 → 43 文件**，新增 `AdminOperationLogs.test.tsx` 15 例——行渲染与失败标红、侧栏「日志」三子项与通道切换、URL 预置 channel/actor_id 并预填输入框、关键字（单字符被拦下不发请求）与合法关键字请求参数、时间范围与结果筛选、清空筛选、命中较多提示、分页、按通道清空与清空全部的二次确认与文案、清空失败保留弹窗、**行内没有任何删除/编辑控件**、finance 直连无权占位且不发请求、空态——并在 `AdminRoleMatrix.test.tsx` 补「日志」入口仅 admin 可见 + finance 直连闸门 2 例、`AdminMembers.test.tsx` 补会员行「日志」入口跳转带 `channel=member&actor_id` 1 例）、`npm run build` 通过；**真机验证（独立临时库 + 真实二进制 + 无头 Edge 真实渲染页面，41 项断言）全部通过**——三类日志真实 HTTP 造数、**页面 DOM 与接口逐条比对**（通道/操作人/动作/摘要一致、`actor_id` 预置进输入框、侧栏三子项、行内无删除/编辑控件）、4.5 万命中 **6.35 s 正常返回结果**、12 万命中 **22.79 s 回 40002 + 可读文案**、无关键字列表/筛选 **40 ms / 206 ms** 不受影响、按通道与全部清空及其留痕通道、未登录 401。 |
| 2026-10-14 | v29 | R18 操作日志（**新增第 23 节，变更记录顺延为第 24 节；迁移 0019**）：**① 三通道面（23.1）**——`operation_logs` 承载 `admin`（管理端**全部写操作**，含登录成败）/ `member`（会员端**全部写操作**）/ `scheduler`（定时任务**每轮执行**）三类日志；**不记**读方法、不记未注册路径的 404（防扫描器刷表）、不记渠道回调与上游回推（不是「某个人」的操作，另有对账与 `instance_operation_logs`），**被鉴权拒绝的写请求（401/403）要记**（审计尝试本身，操作人 0/空串）。**② 数据结构（23.2，迁移 0019）**——`id / channel(ENUM) / actor_id / actor_name（**用户名快照**，账号改名或删除后日志仍可读）/ action / method / path（**路由模板**）/ http_status / status(success\|failed) / ip / duration_ms / summary(500，已脱敏) / created_at(UTC 入队时刻)`；索引 `(channel, created_at)`、`(actor_id, created_at)`、`(created_at)` 与 `FULLTEXT(action, summary, actor_name) WITH PARSER ngram`；`instance_operation_logs`（15.4）**保持独立、不并入本表**。**③ 埋点（23.3）**——管理端/会员端各一个**挂在鉴权中间件之前**的记录中间件：写请求结束时按「方法 + 路由 + 状态码」**兜底**入队，控制器可选补充可读动作名与业务摘要（「路由 → 中文动作名」映射表集中在一处便于维护，未登记回落 `METHOD /path`，漏补不会漏日志）；参数不含请求体，故密钥天然不进日志；登录成功与失败都记、**失败只记尝试的用户名**（密码绝不落库）；定时任务每轮写一条汇总（任务名/结果/处理数/耗时/错误），子步骤确有实际动作时写明细（到期自动暂停/终止收敛/到期提醒），失败尝试只进汇总计数（避免每轮刷屏）。**④ 接口（23.4）**——`GET /api/v1/admin/operation-logs`（`channel` / `actor_id` / `status` / `keyword` / `start` / `end` + `page`/`page_size` 上限 100，新记录在前；时间**闭区间**，接受 RFC3339 或纯日期，`end` 只给日期按**当日 23:59:59** 含当天；不填即全表）与 `DELETE /api/v1/admin/operation-logs?channel=admin\|member\|scheduler\|all`（**必填**；按通道清空走**分批 DELETE**（每批 1000，避免长事务锁表）、清空全部走 **TRUNCATE**（自增 ID 归零）；清空前先**冲出异步缓冲**，故接口返回时被清通道确实为空；**清空动作本身同步留痕**并写入**未被清空**的通道——默认 `admin`，被清空范围含 `admin`（清 `admin` 或清全部）时改写 `scheduler`，**清空全部后三通道只剩 `scheduler` 的一条留痕**）；**不提供任何按条删除、编辑、批量删选的接口或路由**（路由断言测试保证 `.../operation-logs/:id` 的 DELETE/PUT/PATCH/POST 一律 404）。**⑤ 关键字搜索（23.5，本批选择）**——选 **FULLTEXT + ngram**（MySQL 5.7 InnoDB 支持中文），**不是**「关键字必须带时间窗 + LIKE」：索引列固定 `(action, summary, actor_name)`；关键字按空白拆词、**每词按短语整体匹配**、多词 **AND**；**至少 2 个字符**（`ngram_token_size=2`，单字符无法被 ngram 索引 → `40001` 明确拒绝，**不退回全表 LIKE**——本表无自动清理、表会持续增长，全表扫描会随表线性变慢）。**⑥ 性能与生命周期（23.6）**——**写入异步、不阻塞业务请求**：非阻塞入队（缓冲 4096）→ 后台协程攒批（至多 100 条或 200 毫秒）一条多行 INSERT；**不参与任何业务事务**；写失败只记服务日志（条目丢弃）；**缓冲满时丢弃并记服务日志**（绝不反压业务）；服务关闭时 `Close()` 冲出剩余缓冲；**不做自动清理**（Langford 明确选择：表持续增长，运维靠管理员手动清空）；**同时修正 R14 恢复路径**——`stripComments` 不再剥掉 MySQL **可执行注释**（`/*!50100 WITH PARSER `ngram` */`），否则备份→恢复会**静默丢掉 ngram 解析器**、恢复后中文检索失效。**⑦ 脱敏（23.7）**——**请求体整体不落库**（只落方法/路由/状态码/耗时/IP/动作名/摘要）；密钥类字段只记「是否更换」；摘要过第二道 `password=…`/`api_key=…`/`token=…` 替换闸门（只认 ASCII 密钥名 + 等号/冒号，不误伤中文文案）；登录失败只记**尝试的用户名**；字段按列宽**按字符截断**。**⑧ 权限（23.8）**——**读与清空都仅 `admin`**（`finance` / `support` 403，会员/匿名 401）。**测试**：`go test ./...` 全量绿（后端 581 → **605** 个 `func Test`：`internal/oplog` 新增 9 个（落库卡住时 Enqueue 不阻塞且 Flush 会等、缓冲满丢弃不 panic 且不落库、归一与脱敏截断、写失败不上抛、Close 冲出剩余并拒绝关闭后入队、Discard/nil 写入器安全、结果推导、脱敏不误伤中文）、`internal/router` 新增 11 个（管理端/会员端写请求都产生日志且 GET 不产生、401/403 留痕而 404/405 不记、登录成功与失败（只记尝试用户名）、分页/时间范围/`actor_id`/`status`/中文关键字与多词 AND/非法参数、清空按通道与全部及其留痕通道、**无按条删改路由**、角色矩阵、脱敏全表断言、落库卡住时请求不阻塞、定时任务汇总与明细）、`internal/store` 新增 2 个（关键字编译与边界收敛）、`internal/dbbackup` 新增 2 个（可执行注释保留、备份恢复后 ngram 仍在且中文检索命中）；`migrations/roundtrip_test.go` 扩到 0019（断言表 + 三条索引 + **ngram 解析器真的在**，down 删表且不影响 0018、再 up 幂等）；`internal/install` 与 `internal/dbbackup` 的既有断言随迁移数 18→19 同步）；**真机验证（独立临时库 + 真实二进制，端口 18080）全部通过**——三类日志真实 HTTP 造数（管理员登录失败/成功、改站点地址、未登录写请求 401 留痕；会员注册/登录/登录失败/下单/取消；定时任务启动后 1 分钟首轮扫描，`上游未配置` 作为错误进汇总）、读写口径逐条核对、筛选项与中文关键字与多词 AND、清空（会员通道 33,338 条分批 DELETE **1.6 s**；清空全部 TRUNCATE 后自增归零且只剩一条 `scheduler` 留痕）、按 id 路由 404、单字符关键字 40001、`start>end` 40002；**5 万条性能实测见 23.10**——全表第 1 页 **13–17 ms**、时间窗/操作人过滤 **15–20 ms**、命中 6 千条的中文关键字 **71–74 ms**、**命中 4.3 万条的泛词 5.9–6.1 s（如实记录：InnoDB FTS 耗时与命中集合成正比、`LIMIT` 无法提前截断，已给出使用建议）**。前端未动（下一批）。 |
| 2026-10-14 | v28 | R17 上游地址改为**构建期固定、后台只读**（**8.6 / 12.1 / 第 9 节改写；本批无迁移**）：**① 地址来源（8.6）**——上游地址不再是可以后台修改的设置，改为与版本号同一套 `-ldflags -X` **构建期注入**（`internal/buildinfo.UpstreamBaseURL`，缺省空串）；动机是「后台账号被冒用就能把整站上游指向同行」，构建期固定后**任何界面与接口都改不了**；settings 键 `upstream` 只剩 `username` / `api_key` / `timeout_seconds`，库内旧版遗留的 `base_url` 键**被忽略**（不再读、不再写，属惰性残留）；**② 「未配置」口径改写**——构建期地址为空 **或** username/api_key 为空即视为未配置（服务照常启动、其余功能不受影响）；**③ 接口（12.1）**——`GET` 的 `base_url` 改为**只读回显构建期值**并新增 `base_url_fixed: true`；`PUT` 请求体**只接受** `username` / `api_key` / `timeout_seconds`，出现 `base_url`（任意取值，含 `null` 与空串）一律 `40002` 拒绝且**不落库**（连同一请求里带的 username / api_key 也不改动）；`api_key` 三态语义不变；**④ 探活（第 9 节）**——`/admin/upstream/health` 的地址字段同样来自构建期，未配置判定不含地址；**⑤ 构建与发布链**——`scripts/package.sh` 要求环境变量 `UPSTREAM_BASE_URL` 非空（**为空直接报错退出**，绝不产出连不上上游的包）；`Dockerfile.backend` 增 `ARG UPSTREAM_BASE_URL` 并写入 `ldflags`；Release 工作流在打包、镜像与冒烟三处**校验仓库变量非空**并注入（地址不落任何入库文件，仓库公开）；**⑥ 前台界面**——「上游对接」卡片的上游地址改为**禁用输入框 + 「由系统固定，不可修改」说明**，保存不再提交 `base_url`，其余字段与「检查连接」探活不变 |
| 2026-10-14 | v27 | R16 上游状态回推（**新增第 22 节，变更记录顺延为第 23 节；第 2 节补「唯一例外」、12.1 补 site 读写、14.1/14.2/14.3 同步口径**；**迁移 0018**）：**① 数据面（22.1，迁移 0018）**——`instances` 新增 `downstream_token VARCHAR(32)`（上游回推鉴权串，32 位十六进制，开通时生成一次后不再变更；存量老实例为空串、**不补发**）、`status` 枚举新增 `provisioning`（交付在途的内部态，见 14.2）、`host_id` 由 `INT UNSIGNED` 改为带符号 `INT`（交付在途用 `-订单号` 占位：既不与真实主机 ID 冲突又在并发交付间互不相同），`uk_instances_host` 唯一键**保持不变**。**② 开通登记（22.2 / 14.3）**——交付流程改为**先落实例、再调上游**：`EnsurePendingInstance` 在 `CreateHost` 之前插入（或复用）该订单的实例行（`status=provisioning`、`host_id` 负数占位、订单快照字段、`downstream_token`），**同一实例的重试沿用同一个 `instances.id` 与 `downstream_token`**（不重新生成）；`downstream_url`/`downstream_token`/`downstream_id` 随 `/cart/clear`、`/cart/add_to_shop`、`/cart/settle`、`/apply_credit` **四步全部提交**；`downstream_url` 取「站点对外地址」，未配置时按**触发本次交付的请求**（支付回调 / 余额支付 / 管理员重试，在请求线程内解析后交给后台交付）推断 `scheme://host`（`X-Forwarded-Proto` 或 TLS），都取不到则不登记；交付失败时在途行**保留在 `provisioning`** 且**不出现在任何列表里**（`ListInstances` 统一排除；`status` 查询取值仍只有既有四态）；续费链路不再重复登记。**③ 接收端 `POST /api/host/sync`（22.3）**——**顶层路由**（不在 `/api/v1`），`application/x-www-form-urlencoded` 表单，响应是**上游约定的 `{"status":200\|400,"msg":"…"}`（不套统一响应包，第 2 节第 5 条例外）**，失败一律 HTTP 200；验签 = `upper(md5(json_encode(ksort({id, token, rand_str}))))`，其中 **id 以字符串参与**（PHP 口径）、**必须关闭 HTML 转义**，比对大小写不敏感，文档给出三个 golden 向量（单测逐字符断言）；校验顺序为「缺 `signature`/`rand_str` → 400 参数不完整」→「id 非法 / 实例不存在 / `downstream_token` 为空 → 400 **统一文案「该产品不能使用该接口」**（不泄露实例是否存在，存量老实例一律被拒）」→「验签不过 → 400 签名验证失败」；状态映射 `Active/Suspended/Deleted → active/suspended/terminated`、`Cancelled`/`Pending`/`Fraud`/未知值**只存原文不改状态**（`Cancelled` 依据 15.8.1 的 5c 定稿**刻意不映射**：`cancelled` 是保留但不再写入的预留枚举，上游仍在运行时本地保持 `active`/`suspended`），且**终态 terminated 不可逆**；同步字段口径为「**出现在表单里才写**（空值即清空），未出现的不动」，`nextduedate` 额外要求非空；**幂等**（状态与字段都没变则不写库、不写审计）、**静默**（不通知会员/不发站内信/不发邮件）、**密码只落库不进日志与审计**（审计只记变化栏目名，`action=host_sync`、`actor_type=system`）。**④ 站点对外地址设置（22.4）**——`GET/PUT /api/v1/admin/settings/site`（**仅 admin，含读取**；finance/support `403`、会员 `401`）读写 `site` 键里的 `url`（`name`/`admin_email` 由向导写入、原样保留），保存时去尾斜杠、必须是带主机名的 `http(s)` 地址（否则 `40002` 且**不落库**），GET 另给按当前请求推断的 `url_recommended`；管理端设置页新增「站点对外地址」分区。**测试**：`go test ./...` 全量绿（563 → **579** 个 `func Test`：`internal/router` 新增 12 个——签名 golden 向量（逐字符断言签名字符串与 md5）/ HTML 转义回归 / 状态映射逐值 / 表单字段口径、接收端六类用例（同步与响应形状、幂等不写库不写审计、五类非法请求一律 400 且不改数据、状态映射端到端含终态不可逆、字段存在性语义、顶层路由）、站点设置三个（角色矩阵、读写与保留 name/admin_email、校验与不落库）、交付两处（四步都带三参数且取值正确、未配置时按请求 Host 回退）；`internal/delivery` 新增 1 个——鉴权串长度恒 32 且不重复；`migrations` 往返测试扩到 0018（up 断言新列/负号列/枚举、down 断言占位行被清而已交付行保留、再 up 幂等）；`internal/install` 与 `internal/dbbackup` 的既有断言随迁移数 17→18 同步）；前端 `npm test` 全量绿（322 → **327**，新增「站点对外地址」分区 5 个用例）、`npm run build` 通过 |
| 2026-10-14 | v26 | R15 备份目录的设置界面 + compose 默认挂载备份目录（**21.10 新增；21.3/21.4/21.7/21.8/21.9 与 12.1 同步；本批无迁移、不改 R14 四个接口的形状与语义**）：**① 备份目录设置接口（21.10）**——`GET/PUT /api/v1/admin/settings/db_backup`（**仅 admin，含读取**；finance/support `403`、未登录/会员 token `401`）。GET 返回配置原值 `dir`（空串=缺省）、`default`、`default_dir`（缺省目录绝对路径，与当前是否在用无关，供界面展示「留空 = 用这个」）、`resolved_dir`（解析后的绝对路径）、`outside_web_root`、`exists`、`writable`、`message`；**GET 不创建目录、不写库**（目录尚不存在时 `exists=false`+提示，`message` 说明保存或首次备份时会自动创建）。PUT 请求体 `{"dir":"<路径>"}`（**必须提供**，键缺席/`null` → `40001`；空串或纯空白 = 恢复缺省目录），**保存时就做环境校验**：形状校验（控制字符 → `40001`）→ 相对路径按**安装目录**解析 → **落在 Web 根（`<安装目录>/dist`）内一律拒绝（`40002` + 可读原因）** → 目录不存在则按 **0750** 创建（含中间目录）→ **写入探针**（临时文件写+删，覆盖权限不足/只读挂载）；**任一步失败都不落库**，通过才写 `db_backup` 键并返回更新后的完整视图（此时 `exists`/`writable` 均为 true）。**② 复用同一套校验（防口径漂移）**——路径解析、Web 根判定、符号链接与非目录判定全部走 `internal/dbbackup`（新增 `DefaultDir` / `InspectDir` / `PrepareDir`，运行时 `BackupDir` 与启动校验行为不变）；R14 的四条接口（21.3）与「不可下载」铁律完全不变，21.3 只加一行指向 21.10 的说明。**③ 改目录不搬运已有备份（21.10 第 5 条，界面必须明示）**——改目录后旧目录里的备份不出现在新目录的列表里、也**不会被删除**；要保留就手工迁移（`.sql` 与同名 sidecar 一起）或把目录改回原值。**④ 前端（21.7 第 8 条）**——「数据库备份」分区新增可编辑的「备份目录」（输入框 + 保存按钮 + 校验结果提示：解析后的绝对路径/不在 Web 根内/可写，失败展示后端 `40002` 原因并保留输入），旁边明示两条必读提示（改目录不搬走已有备份；留空即缺省目录并显示 `default_dir` 实际路径）；保存成功后重读列表（目录展示随之更新）。**⑤ 容器默认挂卷（21.9 第 6 条）**——`deploy/docker-compose.yml` 的 `backend` 服务新增绑定挂载 **`./backups:/app/backups`**（落点 `deploy/backups/`，已被 `.gitignore` 忽略）：备份**没有下载入口**，运维取文件变成「直接到宿主机该目录拿」；文档给出命名卷替代写法（`backend-backups:/app/backups` + 顶层 `volumes:` 声明，取文件需 `docker cp`）、**权限/属主说明**（容器默认以 root 运行 → 宿主机目录/文件属主通常是 root，目录 0750、文件 0640）与「界面上改成别的路径需自备挂载」「绝不要把备份目录指向 Web 可访问目录」的警告，并明确**容器内不适用一键升级**（R13-B 是主机直装场景，容器升级 = 换镜像 + 手动 `migrate up`）。**测试**：`go test ./...` 全量绿（555 → **563** 个 `func Test`：`internal/dbbackup` 新增 6 个——缺省/相对路径解析、Web 根拒绝且不创建、保存时按 0750 创建并探针、指向文件/符号链接拒绝、只读目录探针失败（Windows 下按既有惯例跳过）；`internal/router` 新增 2 个——角色矩阵与保存/留空/拒绝不落库/改目录后列表不含旧备份、服务未接入时两个接口明确拒绝）；前端 `npm test` 全量绿（**318 → 322 例 / 41 文件**，新增 4 例：解析与可写展示 + 两条提示、保存提交 `{dir}` 并重读列表、留空保存提交 `{dir:""}`、后端拒绝展示原因且不谎报成功）；**真机验证（临时库 + 临时安装目录 + 真实二进制，5 项 / 32 条断言）全部通过**——合法目录保存后返回解析后的绝对路径且备份立即落新目录、Web 根内被拒（`40002` + 可读原因、未落库、未创建目录）、留空恢复缺省目录、改目录后旧备份不在列表且文件未被删除、未登录与会员 token `401` / finance 角色 `403`；compose 以 YAML 解析 + 卷结构断言校验（本机无 docker，未跑 `docker compose config`）。 |
| 2026-10-14 | v25 | R14 数据库备份与恢复（**新增第 21 节；变更记录顺延为第 22 节；20.10 补第 6/7 条升级前备份提示；**本批无迁移、无新表**）：**① 备份实现（21.1）**——**纯 Go** 逐表导出（`SHOW FULL TABLES` → `SHOW CREATE TABLE` + 流式 `SELECT *`），**不依赖 `mysqldump` 或任何外部二进制**；NULL 与字符串严格区分、单引号/反斜杠/换行/NUL 转义、二进制列（BLOB/BINARY/BIT/空间类型）走 `X'..'` 十六进制而**文本列（VARCHAR/TEXT/JSON/DECIMAL）必须走转义字符串**（驱动同样返回 `[]byte`）、时间按 UTC 墙钟值往返（含小数秒与零值日期）、`AUTO_INCREMENT` 由建表语句带出；**一致性口径**：整库在**一个事务**里读同一快照（`REPEATABLE READ` + `START TRANSACTION WITH CONSISTENT SNAPSHOT`），代价是长事务 undo 占用、收益是恢复出来一定自洽；**分块**：单条 `INSERT` ≤ 200 行或 ≤ 512 KiB（MySQL 5.7 缺省 `max_allowed_packet` 4 MiB，绝不拼巨型 INSERT）；**流式写盘**（临时文件 + 原子改名，整库不进内存）；每表 `DROP TABLE IF EXISTS` + `CREATE TABLE` + 分批 `INSERT`（空表只有建表）；文件头带 `generated_at`/`database`/`schema_version`/`app_version`/`operator` 与 `SET FOREIGN_KEY_CHECKS=0;` `SET NAMES utf8mb4;`；**② 元信息存 sidecar（21.2，本批取舍）**——`<文件名>.json` 与备份同目录、**不入库、无新表**：恢复是覆盖式的，元信息若存库会被旧备份带回旧状态（恢复点之后的备份会在列表里凭空消失），sidecar 始终与文件一致且库不可用也能列出；③ **接口（21.3）**——`GET /api/v1/admin/db-backups`（目录状态 + 元信息列表 + 总占用 + `auto_cleanup:false`）、`POST /api/v1/admin/db-backups`（立即备份，同步）、`POST /api/v1/admin/db-backups/:id/restore`、`DELETE /api/v1/admin/db-backups/:id`；错误码：`40001`/`40002`/`403`/`404`/`409`/`50005`（新增「数据库备份或恢复失败」）；④ **不可下载 · 目录边界 · ID 白名单（21.4，本批最硬的三条）**——**不提供任何下载/读文件内容的接口**（路由断言测试保证只有四条路由，响应与日志只含元信息）；缺口省目录 `<安装目录>/backups`（与 `dist/` 同级），设置键 `db_backup`（`{"dir":""}`，相对路径相对安装目录解析），**解析结果落在 `<安装目录>/dist` 内一律拒绝**（启动校验一次 + 每次使用再校验；含符号链接解析后判定）；文件名为 `lyidc-backup-<UTC 时间戳>-<8 位十六进制>`（`crypto/rand`），**ID 形状即白名单**（`../`、绝对路径、盘符、带扩展名、大小写/长度不符一律 404），服务端解析后再复核仍在备份目录内且是普通文件（符号链接拒绝）；⑤ **恢复（21.5，覆盖式、不可撤销）**——校验 `admin` + `confirm=true` + **当前登录管理员密码**（bcrypt，任一不满足 40002 且不动库）；逐表 `DROP`/`CREATE`/`INSERT` 后**删除「当前库有、备份里没有」的表**（`dropped_tables` 即覆盖而非合并的证据）；`FOREIGN_KEY_CHECKS=0` 期间执行并在退出前还原（会话变量随连接回池）；`NO_BACKSLASH_ESCAPES` 直接拒绝（不偷偷改 `sql_mode`）；**恢复后追迁移**：备份版本低于当前代码内嵌最新版本时执行一次 `up`（与安装向导/`lyidc-migrate` 同一套 golang-migrate + 内嵌迁移文件，用 `DatabaseDSN` 新开连接因迁移文件含多语句），高于当前代码则返回 `warning` 不迁移；失败不吞错、错误信息只带语句种类与表名；⑥ **如实声明的风险（21.6）**——覆盖式不可撤销、**失败可能留下不完整的库**（不做恢复前快照）、恢复期间网站短暂不可用（不做维护模式），界面必须醒目警告并**强烈建议先手动备份**；⑦ **前端（21.7）**——设置页新增「数据库备份」分区（锚点 `#db-backups`）：当前库名 / 备份目录（含「不在 Web 根内」标记）/ **备份总占用磁盘** / **不自动清理**说明、备份列表（时间/大小/schema 版本/表·行数/操作人/应用版本）、「立即备份」（进行中禁用）、「恢复」弹窗（不可撤销 + 失败风险 + **输入当前管理员密码** + 明确确认）、「删除」二次确认；**不做下载、不做查看内容**；非 admin 不进设置页、不发越权请求；⑧ **升级弹窗提示（20.10 第 6 条，只提示不阻断）**——R13-B 的升级二次确认弹窗内新增「建议先到「数据库备份」分区做一次备份」的醒目提示与锚点链接，**不自动备份、不阻断升级**（升级链路行为不变）；⑨ **边界（21.9）**——不自动清理/不自动定时备份、不做增量与压缩加密、不支持部分表恢复与恢复到其它库、备份格式自研不保证与 mysqldump 互通、容器部署需挂载备份目录。**测试**：`go test ./...` 全量绿（528 → **555** 个 `func Test`：新增 `internal/dbbackup` 包 20 个——字面量转义/分词器/路径白名单 + 真机集成（备份→改数据→恢复逐表行数比对、覆盖性、低版本备份恢复后追迁移、符号链接与 Web 根拒绝、`NO_BACKSLASH_ESCAPES` 拒绝）——并补 `internal/router` 备份接口集成用例 5 个（角色矩阵、只输出元信息、路由断言「无下载」、二次确认与密码、防穿越、覆盖式恢复））；前端 `npm test` 全绿（**307 → 318 例 / 40 → 41 文件**，新增 `AdminSettingsBackups.test.tsx` 11 例）；**真机验证（临时库 + 临时安装目录 + 真实二进制，7 项）全部通过**——备份/恢复逐表行数一致、覆盖性（新增表被删）、schema 16→恢复后自动追到 17、密码错/缺 confirm/非 admin 全部拒绝、防穿越全部 404、不可下载且备份目录不在 Web 根内、中文与特殊字符往返逐字节一致；浏览器（无头 Edge + CDP）实测备份分区/备份列表/恢复弹窗（密码必填 + 不可撤销文案）/删除确认/升级弹窗备份提示五处界面。 |
| 2026-10-14 | v24 | R13-B 一键自动升级（**新增第 20 节；变更记录顺延为第 21 节；19.5 扩两个地址字段、19.6 矩阵补两行、19.8 改为指向第 20 节；本批无迁移**）：**① 新增 `POST /api/v1/admin/updates/apply`（20.3）**——二次确认 `{"confirm":true,"target_version":"v0.10.0"}`；`target_version` 必须等于缓存里的 `latest_version` 且严格高于当前版本（不降级、不错版），成功 **HTTP 202** 返回任务视图，已有进行中任务 **409**；**② 前置检查 12 条（20.2）**——开发构建拒绝、无有效检查结果拒绝、缺本平台产物 / 缺同名 `.sha256` 拒绝、安装目录可写、磁盘空间 ≥ `max(2×包大小, 512 MiB)`、同步拉取 `.sha256` 作为唯一出网预检（失败 40002 给出可读原因，不半路炸）；**③ 新增 `GET /api/v1/admin/updates/job`（20.4）**——阶段 / 进度 / 起止时间 / 目标版本 / 结果 / 失败原因 / 是否回滚 / `db_migrated` / 日志尾部，从未升级过返回 `data:null`；陈旧 `running`（>10 分钟）落盘为 `failed`；**④ 状态落盘定稿（20.5）**——`<安装目录>/upgrade-state.json`（0600 原子写）+ `upgrade.log`，**不用数据库**：升级会重启进程、替换二进制、迁移可能改库结构，状态必须跨版本/跨进程/库不可用时仍可读，helper 也不能依赖新版本的库访问代码；**⑤ helper 分离进程（20.6）**——主进程落盘 + 写 `handoff.json`（含旧 pid、健康检查地址、**旧进程启动参数**）+ **复制正在运行的二进制**当 helper（不新增发布产物、无自举问题、复制品不在替换范围内）+ 分离启动（Windows `DETACHED_PROCESS`、Unix `setsid`）+ 202 后 0.5 秒 `os.Exit(0)`；helper 入口是同一二进制的 `upgrade-helper` 子命令；**⑥ 顺序与不可逆风险（20.6.3，本批拍板）**——**先替换文件、后迁移、最后才启动新服务**（迁移用新包内的 `lyidc-migrate`，且新服务在迁移成功前绝不启动）；**不做数据库备份**，回滚只还原二进制与 `dist`，明确接受「二进制回滚但迁移已前进」的中间态（依据：既有迁移全部向前兼容，旧二进制在新库上仍可运行），并用状态里的 `db_migrated` + 日志里的 migrate 原始输出兜底；**⑦ 回滚（20.6.3）**——备份 `.bak` / `dist.bak`、失败时结束新进程 → 还原 → 拉起旧版 → 健康检查（30 秒）→ `rolled_back`；旧版也起不来则 `failed` 并要求人工介入；**⑧ 安全（20.7）**——sha256 必须校验（缺失即拒绝、不匹配绝不替换任何文件）、**资产下载不携带 GitHub Token**（`download_base` 可指向任意主机）、解包防路径穿越与符号链接（总 1 GiB、单文件 512 MiB）、交接前用新二进制 `-version` 自报版本校验、日志与响应不含 Token；**⑨ 更新源注入（19.5 / 20.8）**——`updates.github` 新增可选 `api_base`（缺省 `https://api.github.com`）与 `download_base`（非空时 `下载地址 = download_base + path`），仅 admin、改完立即生效，供镜像与**本地 mock 真机验证**；改 `repo` 或 `api_base` 同请求清空检查缓存；**⑩ 前端（20.10）**——「立即升级」按钮 + 二次确认弹窗（目标版本、替换内容、服务会短暂中断）+ 阶段进度轮询（2 秒，容忍重启期间的连接中断）+ 结束态（成功 / 失败原因 / 已回滚 + `db_migrated` 提示），进入设置页先读一次任务状态（刷新后可继续看到进行中或已结束的任务），进行中禁用按钮不重复提交，非 admin 不发越权请求；**⑪ 边界（20.9）**——不支持降级/同版本重装/指定历史版本、不适用于容器部署（容器内替换的是可写层）、不支持断点续传与多镜像回退、不校验发布包签名。**测试**：`go test ./...` 全量绿（486 → **528** 个 `func Test`，测试包 18 → 19：新增 `internal/upgrade` 包 41 个并补 `internal/router` 升级接口集成用例）；前端 `npm test` 全绿（**307** 例 / 40 文件，新增 `AdminSettingsUpgrade.test.tsx` 10 例）；**假部署真机验证三情形**全部通过——① 正常升级（替换后新二进制在跑、dist 已换、迁移 16→17 已执行、健康检查通过）② sha256 不匹配 → 拒绝且未替换任何文件、未产生备份 ③ 健康检查失败 → 自动回滚且旧服务恢复可用。 |
| 2026-10-14 | v23 | R13-A 系统版本与更新检查（**新增第 19 节；变更记录顺延为第 20 节；本批无迁移**）：**① 版本号构建期注入（19.1）**——`buildinfo` 包导出 `Version` / `Commit` / `BuiltAt`，缺省 `dev` / `unknown`；`package.sh`、`release.yml`、`Dockerfile.backend` 同步注入，`cmd/server` 与 `cmd/migrate` 支持 `-version`；**② 语义化比较（19.2）**——预发布不提示为新版，开发构建 `compare=dev_build`；**③ 检查更新接口（19.3）**——`GET /api/v1/admin/updates/check`（实时）与 `GET /api/v1/admin/updates`（读缓存）同一形状，GitHub 侧失败走成功包装 + `status` / `message`，不走 HTTP 错误码；**④ 缓存与定时刷新（19.4）**——settings 键 `updates.check`，失败保留上次成功结果，启动后 60 秒补检、之后每 6 小时一次，失败只记日志（不记 Token）；**⑤ 更新源设置（19.5）**——`GET/PUT /api/v1/admin/settings/updates`，仓库缺省 `LangfordKuo/Lyidc_OEM`，GitHub Token 三态与掩码，修改仓库同请求清空缓存；**⑥ 权限（19.6）**——三个接口仅 `admin`；**⑦ 管理端（19.7）**——设置页「系统更新」卡片、侧栏「设置」与卡片标题红点（仅 `has_update=true`），说明纯文本展示，产物按服务器平台高亮；**⑧ 预留（19.8）**——`POST /api/v1/admin/updates/apply` 待 R13-B，本批未注册。 |
| 2026-10-13 | v22 | R12 导入时按上游分组筛选 + 「上游分组 → 本地子分类」映射——**① 导入请求体（10.4，本批唯一既有接口形状变化，向后兼容）**：`POST /admin/products/import` 新增**可选**请求体 `{ "upstream_group_ids": [1,3,5] }`——省略请求体 / `{}` / `[]` / `null` = **全量导入**（与 R11 前完全一致，既有调用与测试不受影响）；传非空数组时**只抓取这些上游分组的商品**，`product_groups` 的 upsert 也只处理选中的分组；**非法值走统一错误码**：非数组 / 含 `0` 或负数 → `40001`，含不在本次上游目录中的分组 → `40002`（`上游分组不存在：…`，**不静默导入 0 个商品还报成功**）；响应**增量**补两个可选字段 `selected_groups`（选中的上游分组 ID，升序去重，全量时不出现）与 `map_fallback_group_ids`（映射目标失效而回落派生的分组，无则不出现），其余 `created/updated/unchanged/groups/failed/failed_pids` 不变；商品 `sort` 仍按上游目录顺序写入（选子集时沿用其原位置，排序稳定）。**② 迁移 0017（up/down 成对 + go:embed，不改 0001–0016）**——新表 `product_group_category_maps`：`id` / `upstream_group_id`（**UNIQUE**，对应 `product_groups.upstream_group_id`）/ `category_id`（**必须指向子分类**，即 `parent_id IS NOT NULL`）/ `created_at` / `updated_at`，**不建外键**（与既有表风格一致），附 `roundtrip_test.go` 的 up→down→up 往返断言。**③ 归类优先级定稿（10.1，本批核心语义）**——每导入一个商品依次判定：**manual（管理员手工改挂）一律不覆盖**（R7 规则不变，最高优先）→ **命中「上游分组 → 子分类」映射则归到该子分类，写入 `category_source=derived`**（取舍：映射是**导入规则的一部分**，改映射后重新导入会刷新这些商品的归属；只有管理端改分类才写 `manual`，两者语义不混淆）→ 无映射则走 R7 的 `category.Derive` 自动派生（行为不变）。**④ 映射目标失效的降级（10.1）**——映射指向的分类被删除、或（被外部改坏）指向了主分类时，导入**不报错**，仅对该分组回落自动派生，并在响应 `map_fallback_group_ids` 与导入日志中体现（不 500、不静默）；映射只作用于导入写入路径，与读时兜底 `EnsureProductCategories`（只补 `category_id=0`）无关；删除映射**不就地回退**，需重新导入该分组才回落（避免与 manual 手工归属混淆）。**⑤ 管理端映射接口（10.4 新增小节）**——`GET /admin/product-group-maps`（列表，含 `upstream_group_id` / `upstream_group_name` / `category_id` / `category_name` / `category_parent_name`，按 `upstream_group_id` 升序，几十条**不分页**；只返回已建立的映射，未映射分组由前端与 `GET /admin/product-groups` 对照展示；目标分类已被删除时两个名字字段为空串）、`PUT /admin/product-group-maps/:upstream_group_id`（upsert `{category_id}`；校验分类存在且**必须是子分类**，否则 `40002`；上游分组尚未导入本地亦 `40002`）、`DELETE /admin/product-group-maps/:upstream_group_id`（不存在 `404`）。**⑥ 上游分组清单复用既有端点**——**不新增** `GET /admin/upstream-groups`：`GET /api/v1/admin/product-groups`（10.4 既有，已含 `upstream_group_id` / `name` / `products{total,on,off}`）即所需的只读分组清单，导入弹窗与映射区块均以它为数据源。**⑦ 权限（10.5 矩阵同步）**——查看类（`GET /admin/product-group-maps`）三角色均可读；写操作（`PUT` / `DELETE`）沿用 `categories.write`（**仅 admin**，与分类写同一闸门），`finance` / `support` 一律 `403`，前端写入口按同一矩阵禁用并给原因、不发越权请求。**⑧ 前端**——「从上游导入商品」由二次确认改为**弹窗**：分组多选（默认全选；全选/无分组时省略 `upstream_group_ids`，部分选择时提交选中 ID，一个都不选时禁用确认并提示原因），结果区沿用既有计数展示；分类页签新增「上游分组映射」区块（表格 + 只列**已有子分类**的两级下拉 + 新增/修改/删除，不做弹窗内联新建分类）。**测试**：`go test ./...` 全量绿，新增覆盖——按分组筛选（只选 1 个 / 选 2 个 / 全选省略 = 全量）、非法值 `40001`（非数组 / 含 0 / 含负数）与 `40002`（分组不在上游目录）、映射命中后商品归到映射子分类且 `category_source=derived`、改映射后重导刷新、删除映射后重导回落派生、映射目标被删/被改成主分类时降级 + 响应体现、`manual` 商品不被映射覆盖、映射 CRUD（非法分类 / 主分类 / 分组不存在 / 重复 upsert / 删除不存在）与角色矩阵（三角色读、写仅 admin）；迁移往返 0017 up→down→up。 |

| 2026-10-13 | v21 | R11 数量型档位区间下放 + 商品详情页展示调整——**① 数量型多档位价格联动修复（10.2.1 / 10.3 第 6 条，本批唯一接口形状变化）**：根因是 R7 的「档位并集归一化」把每档 `qty_minimum`/`qty_maximum` 统一改写成并集（商品 2 数据盘 `[30-30 @0.00]` + `[31-256 @0.50]` 两档都变成 `30~256`），前端按「qty 落在哪个档位区间」挑档位时永远命中第一档（免费档），加量不加价。**修法（方案 A，向后兼容）**：`values[].qty_minimum`/`qty_maximum` **语义不变、继续是并集**（只用于数量控件 min/max 与默认值，旧客户端行为不变），数量型配置项**新增 `options[].tiers[]`**（`{value_id, qty_min, qty_max, pricings}`，按 `qty_min` 升序、与 `values[]` 同源同 hidden 过滤），`qty_min`/`qty_max` 是**该档真实区间**、`pricings` 是该档单位差价；**挑档位与算 `billable` 一律以 `tiers` 为唯一依据**（`billable = qty − 低一档 qty_max`），选项型不下发 `tiers`。后端 `pricing.Quote`/`quantityLine` 本就按 `TierFor(qty)` + `tierFloor` 正确计费，本批不改计费口径，只把真实档位区间补回接口。**算例（前后端同口径，入测试）**：商品 2 数据盘 qty=30 → 差价 `0.00`；qty=31 → `0.50`；qty=50 → `(50−30)×0.50 = 10.00`；商品 1 单档 `[0-256 @0.50]` 回归 qty=0 → `0.00`、qty=50 → `25.00`。**② 纯前端展示调整（接口不变）**：商品详情页删除「配置速览」（`description_lines` 清单）与「价格一览」（六周期表格），接口仍下发 `description_lines`（列表页商品卡仍在用）；计费周期选择器**直接不渲染不可售周期**（价格 `null`），不再置灰，全部不可售时出空态并禁用下单。 |
| 2026-10-13 | v20 | R10-B 邀请返佣（二）：自建券 + 提现/转余额（含审核）+ 管理端返佣管理——**① 折扣上限（18.5，核心风控，口径写死）**——`上限 = min(settings.referral.max_coupon_percent, 邀请人的生效返佣比例)`，其中「生效返佣比例」= 会员级 `rebate_percent_override` 优先、否则全局 `default_percent`（即 18.2 三级的**前两级**，**商品级不参与**——券跨商品通用，绑不到商品比例）；含义是「返还 20% 的人最多发 8 折券」，此时 `返佣 = max(原价×20% − 券减免, 0) = 0` 恰好归零，叠加公式地板保证**任何情况下返佣不为负**；生效比例为 `0.00` 时上限也是 `0.00`，任何自建券被拒（message 含当前上限值与两个来源）；上限**只在创建时判定**，事后收紧设置不回收既有券（运营走停用）。**② 自建券接口（18.5）**——`POST /api/v1/referral/coupons`（前置：`referral_enabled=1` 否则 `40002`；`owner_member_id` 强制 = 调用者；**`type` 本批只允许 `percent`**，`fixed` 是绝对金额减免、无法用一个比例卡住倒挂，故不做；`code`/`cycles`/`starts_at`/`expires_at`/`max_uses`/`comment` 沿用 11.1 校验口径）、`GET /api/v1/referral/coupons`（我创建的券分页）、`PUT /api/v1/referral/coupons/:id`（**只能改自己的券**，他人券与平台券一律 `404`；**仅允许改 `status` 与 `expires_at`**，`value`/`code`/`owner`/`type` 出现在请求体即 `40001` 明确拒绝不静默忽略——杜绝绕过上限与挖客）；管理端 `GET /admin/coupons` 视图补 `owner_member_id` 与派生 `source`（`platform`/`member`）区分平台券与自建券，管理端 `PUT` **拒绝改自建券的 `value`**（`40002`「自建券的折扣值不可修改」）。**③ 迁移 0016（up/down 成对 + go:embed，不改 0001–0015）**——新表 `referral_payouts`（**提现与转余额共用一张申请表，`kind` 区分 `withdraw`/`convert`**；取舍：字段与审核动作几乎一致，拆两表会复制状态机与幂等锚点；表名取中性 `payouts` 因 convert 不是提现），字段 `id`/`kind`/`member_id`/`amount`/`channel`(`wechat`/`alipay`)/`account_name`/`account_no`(**敏感**)/`status`(`pending`/`approved`/`rejected`/`paid`)/`remark`/`review_note`/`reviewed_by`/`reviewed_at`/`paid_at`/`created_at`/`updated_at` + 两个索引；**不新增 `ledger.type` 枚举**（避免 0016 再扩 ENUM 与 `IsValidLedgerType` 双处同步）。**④ 状态机与幂等（18.1.1）**——`withdraw: pending→approved→paid`、`pending→rejected`；`convert: pending→approved|rejected`；`rejected`/`paid` 为终态不可再流转（`40002`）；**重复审核/重复打款幂等**（对 `approved` 再 approve、对 `rejected` 再 reject、对 `paid` 再 mark-paid 一律 `200+already=true`，不重复产生资金动作、**不覆盖**首次 `reviewed_at`/`paid_at`）；非法流转 `40002`。**⑤ 账务口径定稿（18.1.1，两类不同）**——**提现「提交即冻结」**：提交时 `rebate_balance` 立即扣减 + 写返现侧 ledger（负），approve 只改状态，**reject 退回冻结额**（加回 + 正向 ledger），mark-paid 只记 `paid_at`；取舍是「钱要出系统」，冻结保证重复申请不超额、线下打款前资金已锁定，与既有返佣冲回同一套账务模式。**转换「审核通过时才换算」**：提交**只登记不动钱**，approve 在**同一事务**里 `rebate_balance` 减 + `balance` 加 + **两端各一条 ledger**（返现侧 `referral_rebate` 负向、消费侧 `adjust` 正向），幂等锚点是 `status: pending→approved` 的条件更新；**reject 不动余额**（提交时就没扣过）；取舍是「钱不离开系统」，不必提前冻结。**可用余额公式**（两类共用，行锁下校验）：`可用 = rebate_balance − Σ(kind=convert 且 status=pending 的 amount)`，在途 withdraw 不必再减（已冻结）。**⑥ ledger 口径扩写（18.3）**——凡动 `rebate_balance` 的账目（发放/冲回/提现冻结/退回/转出）一律 `type=referral_rebate`（有符号），靠 `ref_type` 区分（`referral_rebate`/`referral_withdrawal`/`referral_conversion`）；转换的消费侧用 **`adjust`**（正，`ref_type=referral_conversion`）——既有预留的「人工调整」类型，语义即非充值非订单支付的余额变动，用 `recharge`/`order_pay` 会污染那两类对账口径；会员端余额流水继续过滤 `referral_rebate`（故提现冻结/退回不出现），转换的 `adjust` 那条**会出现**（正确）。**⑦ 负数口径（18.1.1）**——`rebate_balance < 0` 时**禁止**提现与转换（`40002` 明确提示），登录/下单/充值/邀请/建券不受影响。**⑧ 敏感数据脱敏（18.5）**——`account_name`/`account_no` 对齐 SMTP 口令「只进库、对外一律掩码」口径：接口回显 `account_name` 姓氏保留 + `**`、`account_no` 前 2 后 2 中间 `****`（不足 6 位留首尾各 1、不足 2 位整体 `****`）；提交回执同脱敏不回显原文；日志/审计/备注一律脱敏；库内存原文供线下打款核对。**⑨ 会员级特批比例写入口（6.3，补 18.7 第 1 条）**——新增 `PUT /api/v1/admin/members/:id`（本批只承载 `rebate_percent_override`，**三态**：缺省不改 / `null` 清空 / 字符串设值 `0.00`–`100.00` 两位小数，非法 `40001` 越界 `40002`；权限沿用 `members.status` 口径 admin/finance）；管理端会员视图输出 `rebate_percent_override` 供页面显示「已特批 x%」；设值后下一笔支付即生效，已发放流水不受影响（快照）。**⑩ 提现/转换接口（18.5）**——会员端 `POST/GET /api/v1/referral/withdrawals`、`POST/GET /api/v1/referral/conversions`（仅本人，校验金额格式/`>0`/负数余额/可用余额，账号字段 1–64 字符非空）；管理端 `GET /api/v1/admin/referral/withdrawals`、`GET /api/v1/admin/referral/conversions`（筛选 `status`/`member_id`，分页，带会员概要，**admin/finance**）、`POST /api/v1/admin/referral/withdrawals/:id/review`（approve/reject + `review_note`）、`POST .../mark-paid`（线下已打款 → `paid` + `paid_at`）、`POST /api/v1/admin/referral/conversions/:id/review`——**审核与打款仅 admin**，查看 admin/finance（财务域口径）。**⑪ 同步**：6.4 矩阵补 `PUT /admin/members/:id`；11.1 补 `owner_member_id`/`source`；11.3 补自建券 `value` 不可代改；12.7 矩阵补 7 行；18.6 矩阵补 6 行；18.7 边界重写（特批比例影响范围、自建券只 percent/不可改值/上限只在创建时、提现线下打款无自动超时、退款联动未做）。**测试**：`go test ./...` 全量绿（改前 411 个 `func Test`）；新增覆盖——自建券：未开通返现者建券被拒、`percent` 超上限被拒（「上限 = min(设置, 生效比例)」两种来源各一例）、建券成功后 `owner_member_id` 正确、`type=fixed` 被拒、`value`/`code` 不可改、他人券 404；自建券使用规则回归（未绑定→首绑、已绑定→拒绝、自用→拒绝、平台券不受限）；提现：余额不足/为负/金额非法被拒、提交→审核通过→标记打款全链、驳回退回（断言 `rebate_balance` 加回 + 正向 ledger）、重复审核/重复打款幂等（`already=true` 且不覆盖 `paid_at`）；转换：审核通过才换算（两端余额与两条 ledger 一致）、驳回不动余额、重复审核只生效一次、负数余额禁止；管理端会员特批比例：三态（不改/null/设值）与非法值拒绝、特批后发放比例随之变化 |
| 2026-10-13 | v19 | R10-A 邀请返佣（一）：契约 + 迁移 + 绑定与返佣结算核心——**新增第 18 节「邀请返佣」**（数据模型 / 公式与比例优先级 / 参与与不参与 / 发放幂等与回滚 / 接口 / 角色矩阵 / 边界），变更记录移至第 19 节。**① 钱的口径（18.2，不得改动）**——`返佣 = max(订单原价 × 生效比例 − 本单优惠券减免, 0)`，**下取整到分**、结果恒 ≥ 0（公式自带地板，永远不会负返佣）；「订单原价」= `orders.amount`（基础价 + Σ 配置差价，不含券减免），「本单优惠券减免」= `orders.discount_amount`；四个验证例子入契约与测试（原价 100、比例 20% → 返 20；用券实付 90（减 10）→ 返 10；20% 比例下 8 折券（减 20）→ 返 **0**；9 折券 → 返 10）。**② 生效比例三级优先（18.2）**——商品级 `products.rebate_percent` > 会员级 `members.rebate_percent_override`（邀请人本人特批）> 全局默认 `referral.default_percent`，取第一个非空命中；`products.rebate_disabled=1` **压过一切**（该商品完全不返佣）；比例 `0.00`–`100.00` 两位小数，非法值一律拒绝。**③ 返现余额独立且可负（18.3）**——发放目标是**邀请返现余额** `members.rebate_balance`（**不是**消费余额 `balance`），两个账户完全独立；**可为负**（已提现后客户退款 → 扣成负），负值时只禁止提现与转余额，不影响登录/下单/充值；`ledger.type` 扩枚举 `referral_rebate`（正=发放、负=冲回），该类型流水的 `balance_before`/`after` 记的是 `rebate_balance` 前后值，会员端余额流水过滤掉本类型（动的不是消费余额）、管理端可按类型对账。**④ 参与与不参与（18.3）**——**返**：新购 `new` 与续费 `renew`，**含用充值余额支付**（余额支付与渠道回调两条入账路径都返）；**不返**：**充值**（`recharges` 入账路径绝不挂返佣）、未绑定邀请人（`inviter_id=0`）、商品禁用返佣、总开关关闭、公式结果为 0（券减免吃满比例时不发放也不写流水）。**⑤ 发放与幂等（18.4）**——**支付成功即发**且与入账**同事务**，挂在 `store.PayOrderWithBalance`（余额）与 `store.CompleteOrderPayment`（渠道回调）两处入账口，**不挂在交付**；`referral_rebates.order_id` **唯一约束**为发放幂等锚点，重复回调由 `model.IsOrderSettled` 闸门挡住（不重复入账/不重复发放），唯一约束是第二道防线。**⑥ 回滚（18.4）**——订单取消（`status=cancelled`）回滚该笔返佣：写负向 `ledger(referral_rebate)` + `rebate_balance` 冲回 + `referral_rebates.status` 置 `rolled_back`，**再次回滚不重复**；当前取消接口只允许 `pending`（从未发放过返佣），故钩子在现有取消路径上是**防御性**的（`CancelOrder` 已改为取消成功后同事务调用同一钩子），一旦出现「已支付取消/退款」路径复用即可；系统当前无退款接口，若后续出现其他「钱退回买家」路径按同一钩子接入。**⑦ 首绑不改（18.4）**——`members.inviter_id` 一旦非 `0` 永久生效，绑定只发生在注册带邀请码、或下单用邀请人自建券且尚未绑定两处。**⑧ 迁移 0015**（up/down 成对 + go:embed，不改 0001–0014）——`members` 加 `inviter_id`/`invite_code`（唯一键，惰性生成）/`referral_enabled`/`rebate_balance`/`rebate_percent_override`；`coupons` 加 `owner_member_id`（0=平台券 / 非 0=邀请人自建券）；`products` 加 `rebate_percent`/`rebate_disabled`；新表 `referral_rebates`（`order_id` 唯一 + inviter/buyer 索引）；`ledger.type` **MODIFY 扩枚举** `referral_rebate`（down 先删该类型流水再收窄枚举，保证回滚干净）。**⑨ 接口（18.5）**——`POST /auth/register` 扩**可选** `invite_code`（按 `members.invite_code` 精确匹配解析，非法/不存在/**自邀**（同用户名或同邮箱）一律 `40002` 明确报错、不静默忽略，解析成功写 `inviter_id`）；`GET/PUT /api/v1/referral`（概览 / 开通关闭，**首次开通惰性生成唯一邀请码**：8 位 base32 去 `0/O/1/I`、`crypto/rand` + 唯一冲突换码重试 5 次、已生成则复用不换码；概览含 `invite_link`（域名取站点设置）/`rebate_balance`/`effective_percent`（总开关关闭时恒 `0.00`）/已邀请人数/累计返佣）；`GET /api/v1/referral/rebates`（返佣流水分页）、`GET /api/v1/referral/invitees`（被邀请人分页 + 消费/贡献返佣统计）；`GET/PUT /api/v1/admin/settings/referral`（总开关 + 全局默认比例 + 自建券折扣上限，**仅 admin**，复用 12.1 设置机制，改完下一笔支付即生效）；`PUT /admin/products/:id` 扩 `rebate_percent`（三态：缺省不改/`null` 清空/字符串设值）与 `rebate_disabled`，管理端商品视图同步输出两字段。**⑩ 邀请人自建券绑定规则（18.5，本批只落地使用规则，建券接口留 R10-B）**——买家用 `owner_member_id` 非 `0` 的券：未绑定 → **下单同事务首绑**到 `owner_member_id`；已绑定 → **拒绝用券** `40002`「该优惠码仅限未绑定邀请关系的客户使用」且**不改绑**（首绑不改）；邀请人用自己自建券 → 拒绝（否则自邀）；平台券（owner=0）任何人可用、不触发绑定。**⑪ 同步**：12.1 设置键补 `referral`、12.7 与 10.5 矩阵同步（`/api/v1/referral*` 仅本人、`/admin/settings/referral` 仅 admin、`PUT /admin/products/:id` 含返佣字段）、6.2 注册请求体补 `invite_code`。**测试**：`go test ./...` 全量绿（411 个 `func Test`，此前 393）；新增纯计算四例逐一断言 + 下取整/地板/边界、三级优先级与 `rebate_disabled` 压过一切、新购/续费/余额支付都返而**充值不返**、重复回调只发一次、回滚冲回 + 状态翻转 + 重复回滚不重复、`CancelOrder` 回滚钩子接线、注册邀请码合法绑定/非法/空/自邀拒绝、自建券绑定与首绑不改、概览与流水/邀请列表、管理端设置与 finance 403、会员端余额流水不含返佣而管理端可按类型对账 |
| 2026-10-13 | v18 | R9-A 管理端分类界面：**① 商品视图补分类字段（10.4，本批唯一后端读路径改动）**——`GET /admin/products` 与 `GET /admin/products/:id` 的列表项新增 `category_id`（所属**子分类** ID，`0`=未归类）/ `category_name` / `category_parent`（所属主分类 `{id,name}`，形状对齐会员端 `group.parent`）；三键**始终存在**（未归类输出 `0` / `""` / `{id:0,name:""}`，不省略键），与 `group_id`/`group_name` 并列下发、互不替代，未归类**不回退**组名（会员端 `group` 的组名兜底不适用于管理端）；**② 管理端页面（10.4 新增小节）**——`/admin/categories` 分类管理页（两级树 + 商品计数；改名 / 改排序 / 子分类改挂主分类，写操作**仅 admin**，`finance`/`support` 只读视图）与 `/admin/products` 的「分类」列 + 「改分类」弹窗（目标为**子分类**或「未归类」`0`，提交 `category_id`，成功提示「已改挂，后续导入不再覆盖该商品分类」，即 `category_source=manual` 语义）+ 按分类筛选（`GET /admin/products` 新增查询参数 `category_id`，`0` = 只看未归类，非 0 必须是存在的子分类，否则 `404 分类不存在`）；除该过滤参数外**不新增端点**，界面只消费 10.4 既有接口；**③ 权限点定稿**——分类管理页写操作走新权限点 `categories.write`（**仅 admin**，与商品写 `products.write`（admin/finance）不是同一个权限点），商品改挂分类沿用 `products.write`；10.5 / 12.7 矩阵已含 `GET /admin/categories`（三角色只读）与 `PUT /admin/categories/:id`（仅 admin），本批核对一致、无改动；**④ 前端**：新增 `AdminCategories` 页（沿用 `ProductGroupsPanel` 的改名/改排序弹窗范式与校验口径：名称去首尾空格 1-128 字、sort `-999999`~`999999`）、侧栏「分类」入口（「商品」之后）、`adminRoles.ts` 增补 `AdminPage` 键 `categories` 与权限点 `categories.write`；`UpdateProductInput` 扩 `category_id`；**测试**：后端商品视图分类字段 + 按分类过滤（含 `0` 未归类、`40001`/`404` 负例）用例，前端分类管理页（树渲染/改名/改排序/改挂/无变化不发请求）与权限（`finance`/`support` 只读、禁用 + 原因文案、不发无权限请求）、商品分类列 / 改挂请求体 / 分类筛选请求参数用例 |
| 2026-10-13 | v18 | R9-B 管理端「商品」与「分类」合并为一页（**页面结构合并，接口无变化、契约版本不变**）：侧栏「商品」「分类」两项合并为唯一入口「商品与分类」（`/admin/products`，page 键沿用 `products`），原 `/admin/categories` 分类管理页并入该页的「分类」页签（查询参数 `?tab=categories`，默认打开「商品」页签；**旧地址 `/admin/categories` 重定向**到 `/admin/products?tab=categories`，收藏 / 外链 / 文档不断链）；两个分区的写权限闸门各自保持——商品写 `products.write` / `products.import` / `groups.write`（admin/finance）与分类写 `categories.write`（**仅 admin**）按分区隐藏/禁用并给出 `permissionHint` 原因，不发越权请求；10.4「管理端页面」小节按新结构改写，**全部接口定义、10.5 / 12.7 矩阵与错误码无任何改动** |
| 2026-10-13 | v17 | R7 配置价格联动 + 商品两级分类：**① 配置差价联动计价（新增 10.2.1）**——成交价 = **基础价 + Σ 配置差价**，差价数据取上游 `config_groups[].options[].sub[].pricings[]`（`type:"configoptions"`，已随 `config_json` 落库，**无需新增列**）；差价跟随定价模式（`upstream` 原样 / `markup` 同率加价 / `fixed` 原样叠加在固定价之上），全链路整数分；**数量型差价 = 档位单位差价 × 可计费数量**，可计费数量 = 数量 − 低一档 `qty_maximum`（无低一档为 0）——实测 286 个数量型里 153 单档位 / 133 多档位且多档位 100% 上下衔接（典型如商品 74 `ip_num` `[1-1 @0.00][2-10 @38.00]`：首个 IP 免费、第 2 个起每个 38.00），单档位时退化为「单位差价 × 数量」；无 pricings / 该周期缺失 / 负值（上游 `-1.00` 哨兵）→ 差价 `0.00` + 明细标注「无差价数据」，不阻断下单；优惠码继续对**总额**抵扣；**② 差价明细快照（迁移 0013）**——`orders.price_breakdown_json` 落库（base / deltas[] / total）并随订单对象下发 `price_breakdown`，续费单同口径（按实例开通时配置快照重算）；**③ 数量型取值范围修正**——由「首个值的 qty 区间」改为**全部档位并集**（此前多档位会被锁死，如商品 2 数据盘 `[30-30]`/`[31-256]` 无法加盘），契约 10.3 第 6 条与下单校验同步；**④ 商品两级分类（10.1 新增，迁移 0014）**——新表 `product_categories`（`parent_id` 两级）+ `products.category_id`/`category_source`（**不**给 `product_groups` 加父级：子分类「香港二区」来自商品名、「美国一区」横跨 3 个上游组，与上游组多对多）；导入按地域关键词派生主分类（含西安/襄阳/内蒙/绍兴/十堰 → `国内` 的实测别名）、按「地域+区」形态或组名派生子分类，`key` 稳定键保证改名不漂移、重复导入收敛（R7 首次导入计一次 `updated`，再导入必回 `unchanged`）；会员端 `GET /products` 新增 `categories[]` 树、`groups[]` 语义演进为**子分类**并新增 `parent`（骨架 `id`/`name`/`sort`/`products` 不变，向后兼容）；管理端新增 `GET /admin/categories`、`PUT /admin/categories/:id`（改名 / 子分类改挂主分类，**仅 admin**，10.5 与 12.7 矩阵同步）、`PUT /admin/products/:id` 扩 `category_id`（改挂后 `category_source=manual`，导入不再覆盖）；**⑤ 前端**：配置变化实时重算金额 + 可展开差价明细（商品详情 / 下单页 / 结算页）、两级分类筛选、配置控件文字前 23px 幽灵空隙修复（Tailwind v4 下 `.relative` 生成顺序晚于 `.sr-only`，shadcn `RadioGroupItem` 的 `relative`+`size-4` 把「隐藏」单选撑成 16px flex 项 + `gap-2` = 23px；改为 `absolute sr-only` 由 cn 合并掉 `relative`） |
| 2026-10-09 | v16 | R6 产品配置交互优化：**① 配置选择控件矩阵**（10.3 新增第 5/6/7 条）——按上游 `option_type` 渲染与上游前台一致的控件（**实测**：2026-10-09 全量拉取上游 159 个商品 + 前台配置页 `/cart?action=configureproduct&pid=N` 渲染对照；映射表入 10.3：1=下拉 / 5=两级系统选择 / 6·8·10·12·13=单选横条 / 4·11·14·15·19=数量；未收录编码按值形态兜底，实测无多选型样本）；**② 数量型（拉条型）取值口径落地**——提交数量而非值 id（8.3 既有口径「数量型传 qty」，此前前端按值 id 提交、上游会把值 id 当数量解释）；`values[]` 视图新增 `qty_minimum` / `qty_maximum` 透传（选项型恒 0），下单校验按范围前置拦截（12.4 请求说明与快照口径、12.8 错误码表第 4 条同步）；**③ 系统选择二级菜单**——下单/结算页与重装弹窗的 `os` 配置（值 `^` 前为系统大类）改为两级（大类 → 版本），重装分组取上游 `/host/cloudos` 的 `group` 字段（无分组信息时退化单级）；**④ 前端**：`ConfigSelector` 重写（四种控件 + 二级系统 + 数量步进）、`ReinstallDialog` 两级改造、订单配置快照的数量型展示（`数据盘 100GB`）；**测试**：前端 33 文件 200 用例全绿（新增 `configControl` 27 + `ConfigSelector` 11 + `ReinstallDialog` 5 + 结算页数量型提交 1），后端 `go test ./...` 全绿（新增数量型下单 1 正例 4 负例 + 混用用例、视图 qty 范围透传断言） |
| 2026-10-09 | v15 | R5 商品简介（上游 `description` 拉取 + 商品卡配置列表）——**迁移 0012**：`products.description` 列**自 0004 建立时即存在**，本迁移不重复加列、只把 R5 口径固化进列注释（存「上游原文经一次 HTML 实体反转义后的原始 HTML」；`MODIFY COLUMN` 只改注释，幂等，回滚只还原注释不删列）；**导入链路**：落库前对上游 `description` 做一次 `html.UnescapeString`（确定性转换，转义存量首次导入计一次 `updated`、其后幂等不变）；**解析工具** `descriptionLines`（router 包，纯正则 + strings，无新依赖）：兼容转义/未转义输入 → 提取 `<li>` 行（li 内嵌 `<br>` 再拆）→ 无 `<li>` 时按 `<br>`/块级标签/换行切 → 去标签、折叠空白、去空行、超长不截断，空简介恒为 `[]`；**接口**：会员端 `GET /products`（列表）与 `GET /products/:id`（详情）新增 `description_lines`（列表不下发原文；详情保留解码后 `description` 原文），管理端 `GET /admin/products/:id` 同步带出；**10.1 字段表与写入边界 / 10.3 列表与详情示例+约定 / 10.4 管理端详情 / 10.6 实测第 6 条**同步改写；同步更新安装集成测试的迁移数量断言（11→12）；**测试**：解析工具 13 用例（含双重转义只解码一次、超长不截断）+ 集成用例（落库解码、幂等、列表/详情/管理端 `description_lines`、空简介 `[]`） |
| 2026-10-08 | v14 | 阶段 8b（补阶段 8 缺口：**管理端订单接口**）——**新增 `GET /api/v1/admin/orders`**（全站订单分页：`page` / `page_size` + `status`（6 态）/ `type`（`new` / `renew`）/ `member_id` / `trade_no`（**模糊匹配**，不区分大小写包含匹配）筛选；视图 = **会员端订单视图（12.4）字段完全相同** + `member` 会员概要（`id` / `username` / `nickname` / `email` / `status`，按当页会员 ID 批量查出；会员行缺失输出 `null`））与 **新增 `GET /api/v1/admin/orders/:id`**（订单全字段 + 会员概要 + 交付信息 `host_id` / `provision_error` / `delivered_at` 与关联实例 `instance_id`；不存在 `404 订单不存在`、ID 非法 `40001`）；**权限定稿**：两个查看类接口 **admin / finance / support 均可读**（客服协助会员查询是日常），**重试交付保持仅 admin**（14.4 不变，仅补一条交叉引用）；**契约 12.6 扩写**为「管理端接口（订单查看与对账）」并给出响应示例，**12.7 矩阵**补一行、**12.8 的 `404` 行**注明管理端只看 ID；阶段 8 的已知缺口（后台订单页只有「重试交付工作台」、仪表盘订单指标标注缺失）在本批消除——后台 `/admin/orders` 升级为**列表（筛选/分页）+ 详情（时间线 / 交付信息 / 会员 / 继续处置）**，仪表盘接入订单总数 / 待支付 / 交付失败与最近订单；**真机冒烟（32 项全通过）**：三角色可读列表与详情、会员 token 与匿名 `401`、`status` / `type` / `member_id` / `trade_no` 筛选逐条口径断言（开发库 12 单：new 9 / renew 3）、组合筛选、6 类 `40001` 负例、详情会员概要 `demo7a` 与交付字段（`host_id=40011` / `instance_id=101` / `delivered_at`）、404 与 retry-delivery 的 `403`（finance / support）/ `401`（会员） |
| 2026-10-08 | v13 | 阶段 8（管理后台前端 + 两处后端补充）：**新增 `GET /api/v1/admin/instances/:id`**（管理端实例详情——字段与会员端 `GET /instances/:id` 同口径（摘要 + `assigned_ips` / `port` / `username` / `password` / `updated_at`），额外回带 `member_id`；权限沿用管理端实例**列表**口径（admin / finance / support 均可读），ID 非法 `40001`、不存在 `404`；见 14.4 与 12.7 矩阵同步更新）；**契约 16.3 一致性修正**——会员工单详情 `GET /tickets/:id` 与 `GET /admin/tickets/:id` 的成功 `data` 统一为**嵌套** `{ticket, messages}`（此前实现把工单字段平铺在 `data` 上，与契约的 `ticket + messages` 写法不符；本批改实现对齐契约，会员端与管理端同步，前端类型/页面/测试一并更新），16.3 增补结构说明；**本批不含其他后端改动**；管理后台前端（`/admin/*`：登录、仪表盘、商品、订单、会员、实例、工单、设置、通知）按 6.4 / 10.5 / 12.7 / 15.3 / 16.3 / 17.3 的角色矩阵落地界面可见性（无权限的入口隐藏或禁用 + 403 统一提示），**页面不写契约**；已知缺口（不在本批范围）：管理端**订单列表/详情接口不存在**（`GET /admin/orders`、`GET /admin/orders/:id`），后台订单页只提供按订单 ID 的「重试交付」工作台 |
| 2026-10-08 | v12 | 阶段 6b：新增第 17 节「通知体系」——**迁移 0011**（新建 `notifications` / `email_logs` 两表 + 给 `instances` 扩 `expiry_reminded_due` 列，**不改 0001–0010**）；**站内通知**（会员端 / 管理端同构的 8 个接口：列表（`unread` 筛选 + 回带未读数）/ 未读计数 / 单条已读（**幂等**，不覆盖首次 `read_at`）/ 全部已读；权限定稿：**个人收件箱**——会员仅本人（他人的与不存在的统一 `404`），管理端三类角色各读本人收件箱（通知不属于工单域））；**邮件 SMTP**（设置键 `email.smtp`：enabled/host/port/username/password/from/from_name/encryption(`none`/`starttls`/`ssl`，端口按加密方式取缺省 25/587/465，口令三态脱敏；`POST /admin/settings/email/test` 同步发测试邮件，未配置 `40002`、失败 `50004`；发送器**只用标准库 net/smtp**（`ssl` 用 crypto/tls 建连再 `smtp.NewClient`，无外部依赖）；`email_logs` 同步留痕、error 已脱敏）；**事件接线定稿 9 个事件**（`order_delivered`/`order_failed`/`renew_succeeded`/`instance_suspended`/`instance_terminated`/`ticket_created`/`ticket_replied`(双向同名)/`ticket_closed`/`expiry_reminder`；**事务提交后异步触发、失败只记日志**；管理端站内按在职 admin+support **逐个账号扇出**、邮件发 `settings.site.admin_email`；**内部备注不产生任何通知**、**会员自行关闭不通知客服**）；**到期提醒**（设置键 `notifications`：站内/邮件总开关 + 到期提醒开关与天数，缺省全开、提前 7 天；扫描窗口 `(now, now+N 天]` 且 `status=active`、无在途取消申请；**去重锚点定稿为 `instances.expiry_reminded_due`**——先原子认领再投递，每到期周期**只提醒一次**，续费/同步推进到期时间后**自动重新武装**；该阶段**不需要上游**，在扫描首段执行）；**接线点**：`internal/notify`（新包，事件入口 + SMTP 发送 + 留痕）、delivery/scheduler 通过窄接口 `Notifier` 解耦、router 工单 handler 4 处挂点接线；**顺手修**：`internal/auth` 的篡改 token 用例改为确定性构造（原写法有约 0.1% 概率构造出与原值相同的 token）；**真机实测（17.7）**——开发库（迁移 0010→0011）+ mini SMTP 收信器（标准库 socket）：测试邮件、工单创建/会员回复/客服公开回复/客服关闭四类事件（内部备注与重复关闭零通知）、会员与支持各自收件箱、finance 零扇出、越权 404 与跨端 401、口令仅回显掩码（响应与日志无明文）、到期提醒三轮（首轮投递 → 去重不重复 → 开关关闭不产生 → 重新武装再投递），通知 8 条 / 邮件 8 封全 success；同步更新 12.1（新增两个设置键）/12.7（角色矩阵补通知三行）/错误码表（新增 `50004`）|
| 2026-10-08 | v11 | 阶段 6a：新增第 16 节「工单系统」——**迁移 0010**（新建 `tickets` / `ticket_messages` 两表，**不改 0001–0009**）；**状态机定稿**（`open` 待客服 ↔ `replied` 待会员 → `closed` 终态；会员回复回 `open`、管理员公开回复转 `replied`、**内部备注不改状态**只推进 `last_reply_at`、关闭**幂等**（重复关闭 `already_closed=true` 且不覆盖 `closed_at`）、`closed` 后回复一律 `40002`）；**单号前缀 `T`**（复用 12.2.5 规则与 `createWithTradeNo`）；**限流定稿**（未关闭工单上限 20，超限 `40002`，并发允许瞬时超出）；**字段约束**（`subject` 5-100 / `content` 1-5000 字符，裁剪首尾空白后按 rune 计，纯空白拒绝）；**接口**（会员端 `POST/GET /tickets`、`GET /tickets/:id`、`POST /tickets/:id/reply|close`；管理端 `GET /admin/tickets`（status/category/member_id/keyword 筛选）、`GET /admin/tickets/:id`、`POST /admin/tickets/:id/reply|close`）；**权限定稿**（工单域为客服域：**admin + support 全权，finance 一律 403**；会员端仅本人，他人工单统一 404）；**内部备注不泄漏**（`internal=true` 的消息绝不进会员端响应）；同步更新 12.7（角色矩阵补工单两行）；**真机实测（16.5）**——开发库（迁移 0009→0010）全流程中文内容演练：工单号 `T20261008121849W0QLH7` 全生命周期（提单 → 会员回复 → 内部备注（状态保持 open）→ support 公开回复（转 replied）→ 会员回复（回 open）→ 关闭 → 关闭后回复 40002 → 重复关闭 `already_closed=true` 且 `closed_at` 不覆盖）、内部备注对会员端零泄漏、finance 四接口全 403、越权关联实例 404、第 21 单 40002 且关闭一单后放行、库内消息流与 6b 挂点日志逐条对应；通知体系（站内通知 + 邮件 SMTP + 到期提醒）留**阶段 6b**，本批只在工单事件处预留挂点 |
| 2026-10-08 | v10 | 阶段 5c：新增 **15.8「取消/终止流程」**——**迁移 0009**（`instances` 扩 `cancel_request_id` / `cancel_type` / `cancel_status`(none/pending/done) / `cancel_reason` / `cancel_requested_at` + `idx_instances_cancel`；`instance_operation_logs.action` 仅注释扩展，VARCHAR(32) 无 DDL 变更）；**状态机定稿**（取消申请是与服务状态正交的标记：在途期间 `status` 保持 `active`/`suspended` 不变，上游确认删除后一次性转 `terminated`；迁移 0007 的 `cancelled` 枚举**保留但不再写入**）；**接口**（会员端 `POST /instances/:id/cancel`、管理端 `POST /admin/instances/:id/cancel`（仅 admin、reason 必填）；`type` = `immediate`/`end_of_billing` → 上游 `Immediate`/`Endofbilling`；**重复申请幂等**（在途返回现状 `duplicate=true`，不重复提交上游但留审计）；允许状态 `active`/`suspended`）；**收敛规则**（主机不在列表或 `domainstatus ∈ {Deleted, Terminated}` → `terminated` + `cancel_status=done` + 审计 `cancel_sync`，幂等；回读故障与「主机不存在」严格区分，绝不误收敛）；**操作矩阵更新**（`terminated` 终态全操作拒绝；`cancel_status=pending` 时禁续费；其他操作不受在途申请影响）；**扫描扩展**（到期暂停排除在途申请实例；新增终止收敛阶段 `cancel_scanned/converged/failed`，immediate 每轮回读、end_of_billing 到期后回读，失败不写审计并下轮重试）；**审计 action 扩展** `cancel` / `cancel_sync`；**视图扩展**（实例列表/详情/`sync` 响应新增取消字段，新增 `terminated` 标志）；**真机实测（15.8.6）**——上游终止完成后主机**仍在 `hostinfo` 列表中且 `domainstatus=Deleted`**、对已删除主机的重复申请返回 `200 + data.domainstatus=Deleted`（无申请号，区别于首次申请的 `202 + pending + cancel_request_id`）、两台遗留真机（10922/10923）经同步收敛为 `terminated` 且审计留痕、终态操作矩阵实测全部 `40002`；同步更新 14.2（instances 状态说明）、15.1（矩阵与状态图）、15.2/15.3（renew 收紧、sync 行为与错误码）、15.4（action 枚举）、15.6（扫描范围与边界） |
| 2026-10-08 | v9 | 阶段 5b：新增第 15 节「实例操作与续费」——**操作矩阵与状态约束**（会员端电源/重装/改密仅 `active`；续费 `active`/`suspended`；管理端 suspend/unsuspend 仅 `admin` 且状态受限、sync 全角色；硬操作 `hard_off`/`hard_reboot` 按需开放并标注风险；上游操作异步受理语义与密码强度口径）；**`instance_operation_logs` 审计表**（迁移 0008：id/instance_id/actor_type(member/admin/system)/actor_id/action/status/message(脱敏)/created_at；全部操作含失败尝试与系统自动操作留痕；审计不参与业务事务）；**续费链路**（orders 扩 `type` ENUM('new','renew') + `instance_id`，`POST /instances/:id/renew` 按当前商品售价建单、**不支持优惠码**；支付成功 → 认领行锁幂等 → `RenewHost` → 回读顺延 `next_due_date` → 订单 active；suspended 续费成功自动 Unsuspend（失败不阻断）；失败置 `failed` + 管理员重试同入口；订单视图新增 `type`/`instance_id`）；**到期暂停扫描**（启动延迟 1 分钟 + 每 24h；`active` 且 `next_due_date < now` → 上游 Suspend + 本地 suspended + 审计 actor=system；失败下轮重试、上游已暂停幂等收敛、上游不可达整轮跳过；不做到期提醒）；**接口**（会员端 `POST /instances/:id/power|reinstall|reset-password|renew`、`GET /instances/:id/reinstall-options|logs`；管理端 `POST /admin/instances/:id/suspend|unsuspend|sync`、`GET /admin/instances/:id/logs`）；错误码新增 `50003`（上游调用失败）；**真机实测差异（15.7）**——`configoption` 口径行为级复核通过（所选 os 精确生效，对照组为默认值）、`/host/details` 可作回读来源、`/host/cloudos` 必须带 `os_config_option_id`、电源/重装/改密为异步受理（`process` 中间态与 `406 重置密码中不能执行该操作`）；同步更新 12.3（orders 两列）/12.4（订单视图 `type`/`instance_id`）/12.7（角色矩阵）/12.8（错误码）/14.1（状态机交付分支按 type 分流）/14.2（instances 状态说明：本批起产生 suspended）；变更记录移到第 16 节 |
| 2026-10-08 | v8 | 阶段 5a：新增第 14 节「订单交付与自动开通」——**订单状态机扩为 6 态**（`pending → paid → provisioning → active / failed`，`cancelled` 仅 `pending`；迁移 0007 ALTER ENUM 并新增 `host_id` / `provision_error` / `delivered_at` 列；**入账幂等集合扩为交付态**：`paid`/`provisioning`/`active`/`failed` 重复回调一律幂等、不重复触发交付）；**instances 表**（订单↔实例一对一 + 上游主机 ID 唯一；订单快照字段、上游同步字段（到期时间/domainstatus/IP/端口/账号密码）、状态枚举（本期只写 `active`，后三者留 5b）；敏感字段仅会员本人详情可见、不进列表与日志）；**自动交付时序**（入账提交后触发、不阻塞回调；认领行锁幂等；`CreateHost` 开通参数拼装（pid/周期映射/host 生成/16 位随机密码/`configoption` 快照）；回读失败不阻断交付；失败置 `failed` + 脱敏原因；超时与落库兜底；进程崩溃悬挂为已知边界；可测性以 `DeliveryTrigger` 注入同步实现）；**接口**（会员端 `GET /instances`、`GET /instances/:id`；管理端 `GET /admin/instances`、`POST /admin/orders/:id/retry-delivery`（仅 admin，同步执行、失败仍 200 返回订单供处置））；同步更新 12.3（状态机与列）/12.4（订单视图三字段与状态提示）/12.7（角色矩阵）/12.8（错误码）/12.9（边界第 1、5 条）/12.10（第 7 条改为已落地+5b 范围）；**真机实测差异（14.5）**——下单/交付 `configoption` 键值口径修正为上游本地 id（上游源码与生产数据佐证：真实商品 `upstream_id` 恒为 0 且未识别键被静默忽略），10.3/12.3/12.4/14.3 同步；**真机全链路演练**（订单 O20261008105520T0J03J → host 10922 → 回读核对一致 → 终止申请 cancel_request_id=432）；变更记录移到第 15 节 |
| 2026-10-08 | v7 | 阶段 4+：新增第 13 节「站点安装向导」——**安装状态机**（无 DSN / 库不可达（含修复模式与 `db=down`）/ 表缺失 / 无管理员 / 存量库自动补标记五种场景 + 续装态 `site_missing`/`pending`；`install.progress` 区分「向导走了一半」与「存量库」；任意步刷新页面或重启进程后按 `state`+`progress` 落回正确步骤，不回退不错位）；**安装模式请求分发**（`/install` 与安装 API 放行、`/api/v1/health` 照常、其它 API `503`+`50301`、浏览器导航 `302` 跳转；装完后 `/install` 永久关闭，重访为「系统已安装」提示页，安装 API 一律 `50302`）；**安装页与 9 个安装 API**（内嵌 HTML/CSS/JS 不依赖前端构建产物、字段校验、自动建库、连接失败按 MySQL 错误码给处置建议）；**默认管理员替换策略**（复用迁移 0003 行改写、库内不得残留默认哈希行、禁用 `admin/admin123456` 组合）；**配置文件合并写入规范**（生效路径、保留既有键与注释、原子替换、0600、只写部署级参数）；**并发与一次性保护**（进程内写锁 + `installed` 条件插入）；**免重启热切换**（同锁内换数据库句柄/JWT 密钥/引擎，`restart_required` 恒 false）；**安全边界**（无鉴权的风险与「装完即关」缓解、密钥不回显不落日志）；错误码新增 `50301`/`50302`/`50303`；**安装页「重启续装」两处 UI 修复**（`status` 新增 `admin_username`/`site_name` 供完成页摘要展示库内实况、第 2 步提交后按最新状态落位而非固定跳第 3 步）；12.1 的 settings 键补充 `site`/`installed`/`install.progress`；变更记录移到第 14 节 |
| 2026-10-08 | v6 | 阶段 4：新增第 12 节「支付与财务」——**后台设置机制**（settings 表 + `GET/PUT /admin/settings/payment/epay` 与 `/admin/settings/upstream`，密钥三态与脱敏、审计、读时校验按内容失效的生效方式、渠道可插拔扩展方式）；**易支付渠道**（彩虹标准协议：下单/签名/回调验签/同步跳转/`out_trade_no` 策略/应答口径/错误分支矩阵）；**订单与充值单/余额/流水**（数据模型与状态机、会员端下单/支付（epay + balance）/取消/充值/余额/流水接口、回调入账幂等与金额校验、管理端对账接口、角色矩阵）；**优惠码应用口径**（下单抵扣 + 支付成功条件自增 `used_count` + 极端并发超用不阻断）；更新 8.6（上游参数来源=后台设置，`config.yaml` 的 `upstream` 段停用）、第 9 节（探活按设置取参、掩码口径说明）、11.1/11.6（折扣应用已实现与并发边界）、错误码表新增 `50002`；变更记录移章（v6 时位于第 13 节，v7 起为第 14 节） |
| 2026-10-08 | v5 | 阶段 3b：计费周期 4 → 6（新增 `biennial` / `triennial`，上游字段 `biennially` / `triennially` 映射与中文显示名入契约；`upstream_prices_json`、`pricing_json.fixed`、会员端 `prices` 同步扩为 6 键）；新增第 11 节「优惠码」（coupons 数据模型、管理端 CRUD、公开校验接口 `GET /coupons/:code/validate`、折扣计算口径与 reason 枚举、暂不支持清单）；变更记录补记 v4 |
| 2026-10-08 | v4 | 阶段 3a（补记）：新增第 10 节「商品与计费」（上游导入与幂等、`upstream_prices_json` 缓存、upstream/markup/fixed 三模式定价、上下架校验、会员端只读目录、管理端接口与角色矩阵、生产上游实测差异） |
| 2026-10-08 | v3 | 阶段 2：新增第 8 节「上游对接」（鉴权机制、`{status,msg,data}` 与状态码映射、上游接口清单、实测示例、实测与文档不符之处/字段类型踩坑）与第 9 节「管理端上游探活接口」；本阶段对生产上游完成真机联调（开通/开关机/重启/重装/暂停/恢复/续费/取消申请） |
| 2026-10-08 | v2 | 阶段 1：新增第 6 节「认证与账号」（会员注册/登录/资料/改密、管理员登录/资料/会员列表/启禁用）、1.1 认证方式（JWT HS256 + aud 区分两类 token）、1.2 时间与时区（DATETIME 存 UTC）、RBAC 矩阵与开发默认管理员说明 |
| 2026-10-08 | v1 | 阶段 0：建立统一响应包、错误码表与 `/api/v1/health` 契约 |
