# Lyidc_OEM API 契约

> **变更流程**：任何接口变更都必须先修改本文档，再修改后端与前端代码。评审时以本文档为准。
>
> 版本：v3（阶段 0 建立；阶段 1 新增认证与账号；阶段 2 新增上游对接与上游探活）

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
| 请求体 | `username`（3-32 位字母/数字/下划线）、`email`（≤128 字节，需符合邮箱格式）、`password`（8-72 字节） |
| 成功 | HTTP 200，`data` 为会员对象（`nickname` 缺省等于 `username`，`status=active`，`balance="0.00"`） |

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

### 6.4 角色权限矩阵

| 接口 | admin | finance | support |
| --- | --- | --- | --- |
| `POST /api/v1/admin/auth/login` | ✓ | ✓ | ✓ |
| `GET /api/v1/admin/profile` | ✓ | ✓ | ✓ |
| `GET /api/v1/admin/members` | ✓ | ✓ | ✓ |
| `PUT /api/v1/admin/members/:id/status` | ✓ | ✓ | ✗（`403`） |

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
| 凭证 | 「用户名（账号手机号/邮箱）」+「API 密钥」（上游前台 安全中心 → API 生成的 12 位随机串，**不是登录密码**） |
| 密钥传输 | 仅在 `POST /zjmf_api_login` 的表单体中传输一次，之后所有请求只用 JWT |

**第一步：换取 JWT**

```http
POST {base_url}/zjmf_api_login
Content-Type: application/x-www-form-urlencoded

username=<上游账号，必须 4-20 字符>&password=<API 密钥>
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
| `Hosts` | `GET /cart/hostinfo` | `hostid[]`、`all` | 已购产品（主机）列表 |
| `Credit` | `GET /cart/credit` | — | 账号余额与货币 |
| `Summary` | `GET /cart/summary` | — | API 概览（开关、数量、调用量） |
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

未鉴权调用（HTTP 200，业务 400/405）：

```bash
curl -s -X POST https://lyew.com/cart/summary
```

```json
{"status":400,"msg":"暂未开通API功能","is_aff":"1"}
```

```bash
curl -s -X POST https://lyew.com/zjmf_api_login \
  -d 'username=<账号>' -d 'password=<API 密钥>'
```

```json
{"status":400,"msg":"鉴权失败","is_aff":"1"}
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

通过 `/api/*` 的路由还会返回 `{"status":400,"msg":"请输入用户名密码"}`（缺 Basic 头）或 `{"status":400,"msg":"当前IP不允许访问"}`（IP 未加白）。

### 8.5 实测与文档不符之处 / 已知限制

1. **`username` 长度硬校验 4–20（当前联调阻塞点）**：上游 `app/home/controller/LoginController.php` 的 `zjmfApiLogin` / `resourceLogin` 均使用 `require|length:4,20`，**超过 20 字符的邮箱一律在校验阶段失败并统一返回 `鉴权失败`**（与密码错误同文案，无法区分）。上游文档标注 username 为「用户名(手机号+区号)」——**手机号 ≤ 20 字符可用，长邮箱不可用**。

   实测证据链（2026-10-08，可复跑脚本见 `scratch/auth_evidence.sh`）：

   | # | 请求 | 响应 | 说明 |
   | --- | --- | --- | --- |
   | 1 | `POST /zjmf_api_login`，username=`langfordkuo@foxmail.com`（23 字符），password=API 密钥 | `{"status":400,"msg":"鉴权失败"}` | 换 JWT 失败 |
   | 2 | `POST /zjmf_api_login`，同一账号 + 错误密码 | `{"status":400,"msg":"鉴权失败"}` | 与 1 文案相同，说明无法用文案区分「长度不合法」与「密码错误」 |
   | 3 | `POST /resource_login_supplier`（同一套 4–20 校验逻辑），同一账号 + 错误密码 | `{"status":400,"msg":"鉴权失败"}` | 该接口密码错误时本应返回 `账号或密码错误`，说明**用户查询根本没执行** ⇒ 卡在长度校验 |
   | 4 | `POST /login_pass_email`，同一邮箱 + 任意密码 | `{"status":400,"msg":"账号或密码错误"}` | 该邮箱在上游**已注册** |
   | 5 | `POST /login_pass_email`，不存在的邮箱 | `{"status":400,"msg":"邮箱未注册"}` | 负对照，证明 4 的判读成立 |

   **影响与结论**：在账号 `langfordkuo@foxmail.com`（23 字符）下无法换取 JWT，因此所有需要鉴权的上游接口（`/cart/hostinfo`、`/cart/credit`、`/provision/default`、开通/续费链路）均不可用；`GET /api/v1/admin/upstream/health` 会如实返回 `connected=false` + `error="上游鉴权失败: 鉴权失败 (api=/zjmf_api_login)"`（实测 HTTP 200、latency≈1.27s）。**解除方式**：改用该账号在上游绑定的**手机号**作为 `upstream.username`（≤20 字符），或为对接新开一个 ≤20 字符登录名的账号。
2. **上游官方文档站不含资源 API 定义**：`http://w2.test.idcsmart.com/doc?name=...`（已抓取于 `docs/references/upstream/`）只覆盖前台/后台管理控制器，`/zjmf_api_login`、`/cart/*`、`/provision/default` 等资源 API 均无文档；本节清单来自 v3.7.5 源码与实测。
3. **响应包风格不同**：上游 `{status,msg,data}` vs 我方 `{code,message,data}`。上游包**不进入**我方对外接口，只在上游客户端内部映射（`backend/internal/upstream/errors.go`）。
4. **业务错误也返回 HTTP 200**：不可用 HTTP 状态码判成败（本阶段实测全部命中该行为）。
5. **`/cart/all` 未鉴权也返回数据**：上游未对该只读接口强制登录；我方客户端仍要求先登录，避免误用匿名态数据（实测该接口匿名态返回 157 479 字节 / 29 个分组 / 159 个商品，已用于验证解析层）。
6. **开通/续费是「下单 + 余额支付」两步**：上游没有单独的「开通」接口，必须 `add_to_shop`/`settle` 后再 `apply_credit` 扣上游余额；余额不足时 `apply_credit` 返回业务失败（非异常）。
7. **`/apply_credit` 的支付成功码是 `1001` 而不是 `200`**：上游返回 `status=200` 表示「已生成账单但未支付」（上游官方下游实现即把 200 当作失败处理），只有 `1001` 才是余额支付成功且回带 `data.hostid[]`。
8. **业务状态码与内层 `data.status` 同名不同义**：`/provision/default` 的外层 `status=200` 是业务成功码，内层 `data.status` 是电源状态文案（`"on"`/`"off"`），解析时不能合并成同一个字段（`internal/upstream.ProvisionResult.DataField` 负责取内层值）。
9. **部分接口把结果放在顶层**：`/zjmf_api_login` 的 `jwt`、`/cart/clear` 的 `hostid`/`invoiceid`/`user` 都在响应顶层而非 `data` 内；`/cart/settle`、`/apply_credit` 的 `hostid` 又出现在 `data` 内。客户端对两处都做兼容（`Response.HostIDs`）。

### 8.6 配置项（`backend/config.yaml` → `upstream`）

| 配置键 | 类型 | 说明 |
| --- | --- | --- |
| `upstream.base_url` | string | 上游地址（含 `http(s)://`，不带结尾斜杠），如 `https://lyew.com` |
| `upstream.username` | string | 上游账号（手机号/邮箱）。**上游登录必需**，故在任务给定的 `base_url/api_key/timeout_seconds` 之外额外增加此项 |
| `upstream.api_key` | string | API 密钥；**真实密钥只允许写在本地 `config.yaml`（已 gitignore）**，`config.example.yaml` 留空并注明由系统设置下发 |
| `upstream.timeout_seconds` | int | 单次上游请求超时，缺省 5，取值 1-120 |

三项（`base_url` / `username` / `api_key`）全空表示不启用上游对接：服务照常启动，仅探活接口返回 `connected=false`。
只配了其中一部分时启动会打印 `上游配置不完整` 告警并列出缺失键。

## 9. 管理端上游探活接口（阶段 2）

### `GET /api/v1/admin/upstream/health`

管理员鉴权（`Authorization: Bearer <admin token>`）后探测上游连通性：客户端会向上游发起一次只读调用（`POST /zjmf_api_login` 换取 JWT + `GET /cart/credit`）。

| 项目 | 说明 |
| --- | --- |
| 方法与路径 | `GET /api/v1/admin/upstream/health` |
| 鉴权 | 管理员 token（`aud=admin`），与 `/api/v1/admin/profile` 一致 |
| 请求参数 | 无 |
| 上游调用 | 登录 + 读取余额（只读，不产生写操作） |
| 超时 | 取配置 `upstream.timeout_seconds`，缺省 5s |

成功（HTTP 200）：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "connected": true,
    "base_url": "https://lyew.com",
    "latency_ms": 412,
    "api_key_masked": "1sXR****ZG5",
    "checked_at": "2026-10-08T07:31:02Z"
  }
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `connected` | bool | 上游只读调用是否成功 |
| `base_url` | string | 上游地址（来自配置，非密） |
| `latency_ms` | int | 本次探活耗时（毫秒） |
| `api_key_masked` | string | 脱敏后的 API 密钥，仅保留首 4 位与后 3 位；未配置时为空串 |
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

1. 上游未配置（`upstream.base_url` 或 `api_key` 为空）时不发起请求，直接返回 `connected=false` 与 `error="上游未配置"`。
2. 探活失败**不返回 5xx**，便于前端把它当作状态展示而不是错误弹窗；管理员 token 无效仍按第 1.1 节返回 `401`。
3. 密钥只以脱敏形式出现在响应与日志中（`1sXR****ZG5`），完整密钥仅存在于本地 `config.yaml`。

## 10. 变更记录

| 日期 | 版本 | 变更内容 |
| --- | --- | --- |
| 2026-10-08 | v3 | 阶段 2：新增第 8 节「上游对接」（鉴权机制、`{status,msg,data}` 与状态码映射、上游接口清单、实测示例、实测与文档不符之处）与第 9 节「管理端上游探活接口」 |
| 2026-10-08 | v2 | 阶段 1：新增第 6 节「认证与账号」（会员注册/登录/资料/改密、管理员登录/资料/会员列表/启禁用）、1.1 认证方式（JWT HS256 + aud 区分两类 token）、1.2 时间与时区（DATETIME 存 UTC）、RBAC 矩阵与开发默认管理员说明 |
| 2026-10-08 | v1 | 阶段 0：建立统一响应包、错误码表与 `/api/v1/health` 契约 |
