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

## 10. 商品与计费（阶段 3a）

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
| `name` / `description` / `type` / `module` | string | 上游 | 商品名/描述/类型/模块 |
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

### 10.2 定价模型

**上游价格缓存 `upstream_prices_json`**

```json
{
  "code": "CNY",
  "prices": {
    "monthly": "20.00", "quarterly": "60.00", "semiannual": "-1.00", "annual": "200.00"
  },
  "rows": [ { "id": 1, "type": "product", "currency": 1, "code": "CNY", "monthly": "20.00", "…": "上游原文" } ]
}
```

| 字段 | 说明 |
| --- | --- |
| `code` | 选中价格行的货币代码（优先选 `CNY` 行，其次首行「月付价可用」的行，最后回退首行） |
| `prices` | 四个周期的上游原值；键是**周期名**（`monthly` / `quarterly` / `semiannual` / `annual`，注意不是上游字段名 `annually` / `semiannually`） |
| `rows` | 上游 `product_pricings` 原文（仅管理端详情接口返回，便于与上游对账） |

约定：**负数表示该周期不售**（上游实测用 `-1.00` 标记未开通的周期，见 10.6 第 2 条）。
上游价不可用（缺省、非法、负数）时该周期视为「无上游价」。

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
| `fixed` | object | 固定覆盖价：周期名 → 金额字符串。键只能是 `monthly` / `quarterly` / `semiannual` / `annual`。`mode=fixed` 时至少一项；`mode=upstream` 时不允许出现 |

**单周期计算顺序**：上游价 →（若配置了 `markup_percent`）按上游价加价 →（若该周期有固定价）用固定价覆盖。

1. 加价：`上游价 × (1 + markup_percent/100)`，**四舍五入到分**（half-up，例：`20.05 × 1.10 = 22.055 → 22.06`）。
2. 固定价**不依赖上游价**：上游该周期不售时，固定价照样生效。
3. 上游价不可用且无固定价覆盖 → 该周期**不可售**，接口输出 `null`（不回退成 `0.00`）。
4. 金额一律用定点小数字符串（如 `"22.00"`），全链路整数分计算，避免浮点误差；金额格式为非负十进制、最多两位小数、上限 `999999999999.99`。
5. 规则校验是**严格模式**：出现未知字段（如把 `markup_percent` 拼成 `markup_percentt`）直接报错，避免「少写一个字母 → 静默不加价」造成资金损失。

**校验错误码划分**：格式类（非法 JSON、未知字段、未知周期、金额写法非法、`mode` 取值非法、
加价率小数超两位）→ `40001`；规则类（`mode=markup` 缺 `markup_percent`、`mode=fixed` 的 `fixed` 为空、
`mode=upstream` 携带 `markup_percent`/`fixed`、加价率越界）→ `40002`。

### 10.3 会员端接口（只读）

商品目录**无需鉴权**：商品与价格对访客可见（阶段 4 的下单/支付接口才要求会员 token）。
会员端只暴露**已上架**（`status=on`）商品，且**不返回任何上游 ID**（`upstream_pid` / `upstream_group_id` 都不出现）。

#### `GET /api/v1/products`

按分组组织的上架商品目录（不含空分组；分组与商品均按 `sort` 升序）。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 不需要 |
| 查询参数 | 无 |
| 成功 | HTTP 200，`data` 为 `{groups, total}` |

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "groups": [
      {
        "id": 1,
        "name": "香港二区",
        "sort": 0,
        "products": [
          {
            "id": 1,
            "name": "香港二区 CN2 A型",
            "type": "dcimcloud",
            "sort": 0,
            "prices": { "monthly": "22.00", "quarterly": "66.00", "semiannual": "132.00", "annual": "220.00" },
            "stock_qty": 70,
            "stock_control": 1,
            "ontrial_max": 0
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
| `prices` | 四周期本地售价；`null` 表示该周期不可售 |
| `stock_qty` / `stock_control` | `stock_control=1` 时 `stock_qty` 才是有效库存；`0` 表示上游不限库存 |
| `ontrial_max` | 可试用数量（`0` 表示不提供试用） |

#### `GET /api/v1/products/:id`

商品详情：配置项 + 四周期价格 + 库存/试用信息。`:id` 是**本地商品 ID**。

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
    "prices": { "monthly": "22.00", "quarterly": "66.00", "semiannual": "132.00", "annual": "220.00" },
    "stock_qty": 70,
    "stock_control": 1,
    "ontrial_max": 0,
    "description": "&lt;li&gt;CPU:2核心&lt;/li&gt;\n&lt;li&gt;内存:1G&lt;/li&gt;",
    "group": { "id": 1, "name": "香港二区" },
    "config_groups": [
      {
        "id": 1,
        "name": "区域",
        "options": [
          {
            "id": 1, "name": "area|区域", "type": 12, "upstream_id": 0,
            "values": [ { "id": 1, "name": "1|HK^香港", "upstream_id": 0 } ]
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

1. `config_groups[].options[].upstream_id` / `values[].upstream_id` 是阶段 4 下单时
   `configoption[<upstream_id>]` 的键，会员端**保留下发**（商品级的上游 ID 仍不下发）。
2. 会员端**过滤掉上游标记为隐藏**（`hidden != 0`）的可配置项与可选值；管理端不做过滤。
3. `name` 是上游文案原文（形如 `area|区域`、`1|HK^香港`），阶段 3a 不做文案清洗。
4. `description` 是上游原文，含 HTML 实体转义（`&lt;li&gt;`）与换行，前端如需渲染 HTML 需自行反转义。

### 10.4 管理端接口

管理端接口全部挂在管理员 token 下（`aud=admin`）。**导入 / 改定价 / 上下架 / 改分组仅 `admin`、`finance` 可调用，`support` 返回 `403`**；查看类接口（列表、详情）所有角色可调用。

#### `POST /api/v1/admin/products/import`

从上游拉取商品目录与逐个商品详情，批量 upsert 到本地（幂等）。**上游侧全程只读**。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，角色 `admin` / `finance` |
| 请求体 | 无 |
| 上游调用 | `GET /cart/all` 一次 + `GET /cart/get_product_config` 每个商品一次（4 并发） |
| 超时 | 单次上游请求取 `upstream.timeout_seconds`；整次导入上限 5 分钟 |
| 成功 | HTTP 200，`data` 为 `{created, updated, unchanged, groups, failed, failed_pids}` |

```bash
curl -s -X POST http://127.0.0.1:8080/api/v1/admin/products/import \
  -H 'Authorization: Bearer <admin-token>'
```

```json
{ "code": 0, "message": "ok", "data": { "created": 159, "updated": 0, "unchanged": 0, "groups": 29, "failed": 0 } }
```

| 字段 | 说明 |
| --- | --- |
| `created` | 新建商品数（新商品默认 `status=off`、`pricing_json={"mode":"upstream"}`，须人工定价后上架） |
| `updated` | 上游字段发生变化的商品数 |
| `unchanged` | 与库内完全一致的商品数 |
| `groups` | 本次处理的分组数（分组只创建不覆盖） |
| `failed` / `failed_pids` | 详情抓取失败而被跳过的商品数与 ID 列表（`failed_pids` 最多 20 个，`failed=0` 时不出现） |

行为约定：

1. 同一批数据重复导入：`created=0, updated=0, unchanged=<商品数>, groups=<分组数>`（幂等）。
2. 单个商品的详情抓取失败不中断整次导入，计入 `failed` 并跳过该商品；**目录里有商品但全部失败**时返回 `500`。
3. 首次导入按上游目录顺序写入 `sort`（分组与商品都是）。
4. 对上游已删除的商品不做处理（本地保留，由管理端下架）。

错误码：

| code | HTTP | 场景与 message |
| --- | --- | --- |
| `40001` | 400 | —（本接口无请求体） |
| `403` | 403 | 角色为 `support` |
| `500` | 500 | 上游未配置（`上游未配置，无法导入商品`）、上游目录拉取失败（`上游商品目录拉取失败：…`）、全部商品详情抓取失败 |
| `50001` | 500 | 写入本地库失败 |

#### `GET /api/v1/admin/products`

分页查询商品（含下架商品）。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token（所有角色） |
| 查询参数 | `page`（默认 1）、`page_size`（默认 20，1-100）、`group_id`（**本地分组 ID**）、`status`（`on` / `off`）、`keyword`（商品名模糊匹配） |
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
        "name": "香港二区 CN2 A型",
        "type": "dcimcloud",
        "module": "idcsmart_common",
        "status": "on",
        "sort": 0,
        "stock_qty": 70,
        "stock_control": 1,
        "ontrial_max": 0,
        "pricing": { "mode": "markup", "markup_percent": 10 },
        "prices": { "monthly": "22.00", "quarterly": "66.00", "semiannual": "132.00", "annual": "220.00" },
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

错误码：

| code | HTTP | 场景与 message |
| --- | --- | --- |
| `40001` | 400 | 分页参数越界/非数字、`status` 取值非法、`group_id` 非正整数 |
| `404` | 404 | `group_id` 指向不存在的分组（`分组不存在`） |
| `401` / `403` | 401/403 | token 无效 / 管理员被禁用 |

#### `GET /api/v1/admin/products/:id`

商品详情 = 列表项字段 + `description` + `config_groups` + `custom_fields` + `upstream_prices`（上游价格缓存原文，含 `rows`）。`:id` 为本地商品 ID。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token（所有角色） |
| 成功 | HTTP 200，`data` 为商品详情（字段见上；`config_groups` 不做 hidden 过滤） |
| 错误码 | `40001`（ID 非正整数）、`404`（`商品不存在`） |

#### `PUT /api/v1/admin/products/:id`

修改本地字段：定价规则、上下架、排序。

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，角色 `admin` / `finance` |
| 请求体 | `pricing_json`（定价规则，见 10.2）、`status`（`on` / `off`）、`sort`（`-999999` ~ `999999`）；三个字段均可选但**至少提供一个** |
| 成功 | HTTP 200，`data` 为更新后的商品详情（与 `GET /admin/products/:id` 同构） |

```bash
curl -s -X PUT http://127.0.0.1:8080/api/v1/admin/products/1 \
  -H 'Authorization: Bearer <admin-token>' -H 'Content-Type: application/json' \
  -d '{"pricing_json":{"mode":"markup","markup_percent":10},"status":"on","sort":3}'
```

注意：`pricing_json` 既接受**对象**（`{"mode":"markup","markup_percent":10}`）也接受**字符串**
（`"{\"mode\":\"markup\",\"markup_percent\":10}"`），传 `null` 表示重置为缺省规则 `{"mode":"upstream"}`。
`status` 与 `sort` 只在提供时才修改；未提供的字段保持原值。

错误码：

| code | HTTP | 场景与 message |
| --- | --- | --- |
| `40001` | 400 | 未提供任何字段、定价规则格式非法（含未知字段/未知周期/金额写法非法）、`status` 取值非法、`sort` 越界、ID 非正整数 |
| `40002` | 400 | 定价规则不成立（缺 `markup_percent`、`fixed` 为空、`mode=upstream` 携带加价率/固定价等） |
| `403` | 403 | 角色为 `support` |
| `404` | 404 | `商品不存在` |
| `409` | 409 | **更新后为上架状态，但四个周期都没有可用价格**（`商品没有任何可用周期的价格，无法上架（请先配置固定价或确认上游价格可用）`）：避免上架一个买不到的商品 |
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

### 10.5 角色权限矩阵（商品部分）

| 接口 | admin | finance | support |
| --- | --- | --- | --- |
| `GET /api/v1/products`、`GET /api/v1/products/:id` | 公开（无需 token） | 公开 | 公开 |
| `GET /api/v1/admin/products`、`GET /api/v1/admin/products/:id` | ✓ | ✓ | ✓ |
| `GET /api/v1/admin/product-groups` | ✓ | ✓ | ✓ |
| `POST /api/v1/admin/products/import` | ✓ | ✓ | ✗（`403`） |
| `PUT /api/v1/admin/products/:id`（改定价/上下架/排序） | ✓ | ✓ | ✗（`403`） |
| `PUT /api/v1/admin/product-groups/:id` | ✓ | ✓ | ✗（`403`） |

### 10.6 实测差异与约定（2026-10-08，生产上游）

1. **`/cart/all` 的库存与试用字段恒为 0，不可用**：实测 159 个商品的 `stock_control` / `qty` / `ontrial`
   全为 0，而 `GET /cart/get_product_config` 的 `products.qty` / `stock_control` 才是真实值
   （例：商品 1 `stock_control=1, qty=70`）。因此导入的库存/试用一律取自**商品详情接口**，
   `stock_control` 随 `config_json` 缓存下发到视图，`stock_qty` 落 `products.stock_qty`。
2. **负数价格表示「该周期不售」**：实测商品 1 的 `biennially` / `triennially` 为 `-1.00`；
   159 个商品里 `monthly` 有 5 个、`quarterly` 33 个、`semiannually` 54 个、`annually` 32 个为负值或缺省。
   本地定价据此把该周期判为「无上游价」（输出 `null`），**不会**退化成 `0.00` 或负数。
3. **目录与详情两个接口的字段不一致**：`/cart/all` 的商品 `gid` 为 0（分组关系由目录的嵌套结构给出），
   详情接口的 `products.gid` 才是真实分组；`/cart/all` 有 `module`（如 `idcsmart_common`）而详情接口为空。
   导入时分组一律取**目录里的父分组 ID**，其余字段「目录优先、详情兜底」。
4. **单商品详情体量最大约 22KB**（159 个商品实测，`product_pricings` + `config_groups` + `sub` 价格行），
   故 `config_json` 用 `MEDIUMTEXT` 缓存原文。
5. **可配置项文案是上游原文**：`option_name` 形如 `area|区域`、`1|HK^香港`（`code|名称` / `序号|代码^名称`），
   阶段 3a 不清洗，会员端原样下发（见 10.3）。
6. **`description` 含 HTML 实体转义**：上游把 `<li>` 存成 `&lt;li&gt;`（实测 159 个商品均是），
   入库保留原文，前端渲染需自行反转义。
7. **并发抓取安全**：导入对上游是纯只读（`/cart/all` + `/cart/get_product_config`），
   4 并发抓 159 个商品实测约 6.5 秒（串行约 40 秒以上）；客户端自带 405 重登与幂等 GET 重试。

## 11. 变更记录

| 日期 | 版本 | 变更内容 |
| --- | --- | --- |
| 2026-10-08 | v3 | 阶段 2：新增第 8 节「上游对接」（鉴权机制、`{status,msg,data}` 与状态码映射、上游接口清单、实测示例、实测与文档不符之处/字段类型踩坑）与第 9 节「管理端上游探活接口」；本阶段对生产上游完成真机联调（开通/开关机/重启/重装/暂停/恢复/续费/取消申请） |
| 2026-10-08 | v2 | 阶段 1：新增第 6 节「认证与账号」（会员注册/登录/资料/改密、管理员登录/资料/会员列表/启禁用）、1.1 认证方式（JWT HS256 + aud 区分两类 token）、1.2 时间与时区（DATETIME 存 UTC）、RBAC 矩阵与开发默认管理员说明 |
| 2026-10-08 | v1 | 阶段 0：建立统一响应包、错误码表与 `/api/v1/health` 契约 |
