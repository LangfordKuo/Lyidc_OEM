# Lyidc_OEM API 契约

> **变更流程**：任何接口变更都必须先修改本文档，再修改后端与前端代码。评审时以本文档为准。
>
> 版本：v7（阶段 0 建立；阶段 1 认证与账号；阶段 2 上游对接与探活；阶段 3a 商品与计费；
> 阶段 3b 六周期 + 优惠码；阶段 4 设置机制 + 易支付 + 充值/余额/流水 + 下单与在线支付；
> **阶段 4+ 站点安装向导：首次访问浏览器完成部署，全程零文件编辑**）

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
| `50002` | 支付渠道调用失败 | 渠道不可达、渠道拒绝下单、渠道响应异常（阶段 4，见 12.2） |

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

### 8.6 配置项（阶段 4 起：**由后台设置承载，不再走 `config.yaml`**）

上游对接参数自阶段 4 起迁移到「后台管理设置」（settings 表 + 管理端接口，见 12.1），
**改完立即生效、无需重启**；`backend/config.yaml` 的 `upstream` 段已停用并删除（`config` 包不再解析该段）。
本文件只保留部署级参数（监听地址、数据库连接、JWT 密钥、日志等级）。

| 设置字段（键 `upstream`） | 类型 | 说明 |
| --- | --- | --- |
| `base_url` | string | 上游地址（含 `http(s)://`，不带结尾斜杠），如 `https://lyew.com` |
| `username` | string | 上游账号（**必须是注册手机号**，上游对 username 有 4-20 字符硬校验，见 8.5 第 1 条） |
| `api_key` | string | 上游 API 密钥（12 位随机串，不是登录密码）；写入后接口只回 `api_key_configured` 与掩码，**绝不回显明文** |
| `timeout_seconds` | int | 单次上游请求超时，缺省 5，取值 1-120 |

读取/写入接口：`GET` / `PUT /api/v1/admin/settings/upstream`（仅 `admin` 角色，见 12.1）。
三项（`base_url` / `username` / `api_key`）全空表示不启用上游对接：服务照常启动，仅上游相关功能按「未配置」处理
（探活返回 `connected=false` + `error="上游未配置"`，商品导入返回明确错误），**其余功能不受影响**。
只填了一部分时同样按未配置处理，但设置接口仍能看到已填字段（便于管理员分步填写）。

**生效方式**：上游客户端按当前设置**动态构造**——每次使用都读一次 settings（主键查询，开销极小），
设置内容未变时复用现有客户端（**保留其 JWT 缓存**，不重复登录），内容变化时立即重建客户端并丢弃旧 JWT
缓存（契约 12.1）。因此不存在「启动时读一次」的陈旧配置。

## 9. 管理端上游探活接口（阶段 2）

### `GET /api/v1/admin/upstream/health`

管理员鉴权（`Authorization: Bearer <admin token>`）后探测上游连通性：客户端会向上游发起一次只读调用（`POST /zjmf_api_login` 换取 JWT + `GET /cart/credit`）。

| 项目 | 说明 |
| --- | --- |
| 方法与路径 | `GET /api/v1/admin/upstream/health` |
| 鉴权 | 管理员 token（`aud=admin`），与 `/api/v1/admin/profile` 一致 |
| 请求参数 | 无 |
| 上游调用 | 登录 + 读取余额（只读，不产生写操作） |
| 上游参数 | **按当前后台设置动态读取**（键 `upstream`，见 8.6 / 12.1）；未配置时不发起请求 |
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
| `base_url` | string | 上游地址（来自后台设置，非密） |
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

1. 上游未配置（后台设置里 `base_url` / `username` / `api_key` 任一为空）时不发起请求，直接返回
   `connected=false` 与 `error="上游未配置"`（此时仍回带已填写的 `base_url`，便于管理员排查）。
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
            "prices": {
              "monthly": "22.00", "quarterly": "66.00", "semiannual": "132.00", "annual": "220.00",
              "biennial": null, "triennial": null
            },
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
| `prices` | 六周期本地售价；六个键**始终存在**，`null` 表示该周期不可售 |
| `stock_qty` / `stock_control` | `stock_control=1` 时 `stock_qty` 才是有效库存；`0` 表示上游不限库存 |
| `ontrial_max` | 可试用数量（`0` 表示不提供试用） |

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

1. `config_groups[].options[].id` / `values[].id` 是下单 `config`（进而交付 `configoption`）的
   键与值——**阶段 5a 真机实测口径**（见 14.5 第 1 条：上游直连下单按本地 id 取配置）；
   `upstream_id` 为上游「代理模式」映射字段（本项目真实数据恒为 0），会员端**保留下发**
   仅作透传展示（商品级的上游 ID 仍不下发）。
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
5. **周期解析升级后的首次导入会出现 `updated>0`，属预期**：阶段 3b 把 `upstream_prices_json`
   从 4 键扩为 6 键（新增 `biennial` / `triennial`），而导入的「上游变化检测」按字段整串比较，
   因此升级后首次导入会刷新所有商品的该字段（`updated=<商品数>`）；**再导入一次必回到
   `unchanged=<商品数>`**（幂等保持）。

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
6. **`description` 含 HTML 实体转义**：上游把 `<li>` 存成 `&lt;li&gt;`（实测 159 个商品均是），
   入库保留原文，前端渲染需自行反转义。
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
| `key` | string | 设置键（主键）；本批两个：`payment.epay`、`upstream`；阶段 4+ 新增 `site`（站点信息）、`installed`（安装标记）、`install.progress`（安装进行中标记，临时），见第 13 节 |
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
集成测试对此有断言（换地址后旧上游不再被调用；只改密钥后业务调用会在新密钥下重新登录）。

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
    "username": "16650492239",
    "api_key_configured": true,
    "api_key_masked": "1sXR****",
    "timeout_seconds": 10,
    "updated_by": 1,
    "updated_at": "2026-10-08T09:42:33Z"
  }
}
```

`api_key_configured` / `api_key_masked` 语义同支付 `key`；`timeout_seconds` 未配置时输出缺省 5。
字段语义与「配一半」时的行为见 8.6。

#### `PUT /api/v1/admin/settings/upstream`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 同上（仅 `admin`） |
| 请求体 | `base_url` / `username` / `api_key` / `timeout_seconds`，**均可选但至少提供一个**；`api_key` 三态语义同支付 `key` |
| 成功 | HTTP 200，`data` 为更新后的设置视图 |

| code | HTTP | 场景与 message |
| --- | --- | --- |
| `40001` | 400 | 未提供任何字段；`base_url` 不是合法 `http(s)` 地址 |
| `40002` | 400 | `timeout_seconds` 不在 1-120：`timeout_seconds 需为 1-120 之间的整数，收到 0` |
| `403` | 403 | 角色不是 `admin` |
| `500` / `50001` | 500 | 值损坏 / 库操作失败 |

**生效承诺**：PUT 之后，下一次上游调用必须使用新参数——**包括只改 `api_key`**（地址不变、密钥变了）的情况：
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
| `config_json` | string | 所选配置项快照 `{"<配置项 id>": "<所选值 id>"}`（阶段 5a 起交付时原样拼 `configoption`，口径见 14.5 第 1 条） |
| `amount` / `discount_amount` / `final_amount` | string | 原价 / 优惠码折扣额 / 应付金额（定点小数字符串，`final = amount − discount`） |
| `coupon_id` / `coupon_code` | int \| null / string | 所用优惠码快照（未用码：NULL / 空串） |
| `status` | string | `pending` / `paid` / `provisioning` / `active` / `failed` / `cancelled`（阶段 5a 扩为 6 态，完整状态机见 14.1） |
| `pay_channel` / `channel_trade_no` / `pay_time` | string / string / string \| null | 支付渠道（`epay` / `balance`）、渠道单号、支付时间（UTC） |
| `host_id` | int \| null | 上游主机 ID（阶段 5a 交付成功后写入；未交付为 `null`） |
| `provision_error` | string | 最近一次交付失败原因（脱敏，最多 500 字符；成功时清空，空串表示无错误） |
| `delivered_at` | string \| null | 交付完成时间（UTC；未交付为 `null`） |
| `created_at` / `updated_at` | string | RFC3339（UTC） |

状态机（阶段 5a 扩为 6 态，流转规则与幂等锚点见 14.1）：`pending → paid`（在线支付回调 / 余额支付）、
`pending → cancelled`（本人取消）、`paid → provisioning → active / failed`（自动交付）、
`failed → provisioning`（管理员重试）。**没有** `paid → refunded`，**没有**自动超时关闭（12.10）。

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

#### `POST /api/v1/orders`

| 项目 | 说明 |
| --- | --- |
| 请求体 | `product_id`（必填）、`cycle`（必填，6 周期之一）、`config`（可选）、`coupon_code`（可选，空串 = 不用码） |
| 成功 | HTTP 200，`data` 为订单对象（含金额明细） |

`config` 的键与值都是**上游配置项的本地 ID**：键为该商品可配置项的 `id`（`options[].id`），
值为所选可选值的 `id`（`values[].id`）；值接受 **JSON 字符串或整数**写法（服务端统一按字符串快照）。
只接受该商品**会员可见**的项与值（上游标记 `hidden` 的项/值按「未知」处理）。
（键值口径的阶段 5a 真机实测依据见 14.5 第 1 条；`upstream_id` 不再作为下单键值。）

```bash
curl -s -X POST http://127.0.0.1:8080/api/v1/orders \
  -H 'Authorization: Bearer <member-token>' -H 'Content-Type: application/json' \
  -d '{"product_id":1,"cycle":"annual","config":{"11":111},"coupon_code":"CASH20"}'
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
    "cycle": "annual",
    "qty": 1,
    "config": {"11": "111"},
    "amount": "200.00",
    "discount_amount": "20.00",
    "final_amount": "180.00",
    "coupon_code": "CASH20",
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

校验顺序（依次判断，命中即返回）：

| # | 条件 | code / HTTP | message |
| --- | --- | --- | --- |
| 1 | 请求体非合法 JSON、`product_id` 非正整数、`cycle` 非本地周期、`config` 键值类型不符 | `40001` / 400 | 见 12.8 |
| 2 | 商品不存在**或已下架**（统一 404，不泄露存在性） | `404` / 404 | `商品不存在` |
| 3 | 该周期无本地售价（不可售） | `40002` / 400 | `该商品在 <cycle> 周期不可售（无本地售价）` |
| 4 | `config` 含未知配置项/未知取值（含隐藏项与隐藏值）或快照超 1024 字节 | `40002` / 400 | `未知的配置项 "..."（不在该商品的可配置项内）` / `配置项 "..." 不支持所选值 "..."` |
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

### 12.6 管理端接口（对账）

两个只读列表，用于财务对账；`admin` 与 `finance` 可查，`support` 返回 `403`。

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
| `GET /api/v1/admin/recharges`、`GET /api/v1/admin/ledger` | ✓ | ✓ | ✗（`403`） | ✗（`401`） |
| `GET /api/v1/admin/instances`（阶段 5a） | ✓ | ✓ | ✓ | ✗（`401`） |
| `POST /api/v1/admin/orders/:id/retry-delivery`（阶段 5a，仅 admin） | ✓ | ✗（`403`） | ✗（`403`） | ✗（`401`） |
| `POST/GET /api/v1/orders`、`GET /api/v1/orders/:id`、`POST /api/v1/orders/:id/pay`、`POST /api/v1/orders/:id/cancel` | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `GET /api/v1/instances`、`GET /api/v1/instances/:id`（阶段 5a） | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `POST/GET /api/v1/recharges`、`GET /api/v1/finance/balance`、`GET /api/v1/finance/ledger` | ✗（`401`） | ✗（`401`） | ✗（`401`） | ✓（仅本人） |
| `POST\|GET /api/v1/payments/epay/notify`、`GET /api/v1/payments/epay/return` | 公开（无鉴权，验签是凭证） | 公开 | 公开 | 公开 |

### 12.8 错误码汇总（阶段 4 新增场景）

| code | 含义与典型场景 |
| --- | --- |
| `40001` | 参数错误：分页越界、`status`/`type` 取值非法、`member_id` 非正整数、金额写法非法、URL 非法、设置请求体为空、订单/商品 ID 非法、`config` 键值类型不符 |
| `40002` | 参数校验失败：周期不可售、未知配置项/取值、优惠码不存在或无效、订单状态不允许支付/取消/**重试交付**、余额不足、渠道未启用或配置不完整、`pay_type` 不受支持、`enabled=true` 缺必填设置项、超时越界 |
| `401` | 未携带/无效 token（会员接口用管理员 token 访问同样 401） |
| `403` | 角色不足（设置接口非 admin；财务对账接口非 admin/finance；**重试交付接口非 admin**） |
| `404` | 商品不存在或已下架、订单不存在或非本人、**实例不存在或非本人**、优惠码不存在（校验接口） |
| `409` | （阶段 4 未新用） |
| `500` | 库内设置值损坏等内部错误 |
| `50001` | 本地库读写失败 |
| `50002` | 支付渠道调用失败（渠道拒绝下单、响应异常、网络失败）；message 为脱敏后的渠道提示 |

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

### 14.2 instances 表（迁移 0007）

订单交付成功后的主机记录；订单 ↔ 实例**本期一对一**（唯一键 `uk_instances_order`），
上游主机 ID 全局唯一（唯一键 `uk_instances_host`）。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int | 本地实例 ID（对外主键） |
| `member_id` / `order_id` | int | 所属会员 / 来源订单（订单快照） |
| `host_id` | int | 上游主机 ID（`hostinfo` 的 `hosts[].id`） |
| `product_id` / `product_name` | int / string | 本地商品 ID 与商品名快照（订单快照） |
| `name` | string | 主机名（开通时提交上游的 `host`：`oem-` + 订单号小写） |
| `billing_cycle` | string | 计费周期（本地 6 周期之一） |
| `next_due_date` | string \| null | 到期时间（UTC；开通后从上游 `nextduedate` 回读，回读失败留 `null`） |
| `status` | string | `active` / `suspended` / `cancelled` / `terminated`；**本期只写 `active`**，后三者由阶段 5b 维护 |
| `upstream_status` | string | 上游 `domainstatus` 原文（如 `Active`；同步字段） |
| `dedicated_ip` | string | 上游主 IPv4（未回读为空串） |
| `assigned_ips` | string | 上游附加 IP（库内逗号分隔原文；接口输出为数组） |
| `port` | int | 上游端口（`0` 表示未返回） |
| `username` / `password` | string | 主机账号 / 密码（**敏感字段**，规则见下） |
| `created_at` / `updated_at` | string | RFC3339（UTC）；`created_at` 为交付完成时刻 |

**敏感字段可见性规则**：`username` / `password`（及 `port` / `assigned_ips`）**仅会员本人**
在实例详情接口可见（等价于「在面板查看主机密码」）；实例列表（会员端与管理端）一律不含；
**任何日志不写密码**（交付日志只记订单号与主机 ID）；管理端本期不提供实例详情接口。

### 14.3 自动交付时序与幂等、失败、重试

**时序（在线回调 / 余额支付两支共用）**：

```
① 入账事务（本地库）：订单 → paid（+ 支付字段 / 优惠码计数）      ← 回调应答前完成
② 事务提交后触发交付（Trigger）：不阻塞回调应答（默认异步 goroutine）
③ 认领（事务）：行锁订单 + 状态条件 → provisioning               ← 幂等锚点
④ 上游开通：CreateHost（clear → add_to_shop → settle → apply_credit）
⑤ 回读：hostinfo 取 nextduedate / domainstatus / IP / 端口 / 账号密码
⑥ 落库（事务）：插入实例 + 订单 → active（host_id / delivered_at）
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
| `configoption[<配置项 id>]` | `orders.config_json` 快照原样（键为配置项 id、值为所选值 id，见 12.3 与 14.5 第 1 条） |
| `qty` | `orders.qty`（本批恒 1） |
| `currencyid` | 由上游 `/cart/clear` 的 `user.currency` 决定（客户端自动处理） |

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

#### `POST /api/v1/admin/orders/:id/retry-delivery`

| 项目 | 说明 |
| --- | --- |
| 鉴权 | 管理员 token，**仅 `admin` 角色**（`finance` / `support` 返回 `403`） |
| 请求体 | 无 |
| 成功 | HTTP 200，`data` 为**最新订单对象**：交付成功 `status=active` + `host_id`；交付执行失败 `status=failed` + `provision_error`（仍按 200 返回，便于管理员据状态处置或再次重试） |
| 错误码 | `401`、`403`、`40001`（ID 非法）、`404`（订单不存在）、`40002`（订单尚未支付 / 正在交付中 / 已交付完成 / 已取消）、`50001` |
| 说明 | **同步**执行一次完整交付（认领 → 上游开通 → 回读 → 落库）；`paid`（未触发过）与 `failed` 可重试；`provisioning` / `active` / `pending` / `cancelled` 不可重试 |

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
4. **本批真机未覆盖「带配置项开通」**：演练商品（上游 pid 15）配置项的 `upstream_id` 全为 0，
   按 id 口径选值需要前端按新口径下单，本批以集成测试覆盖参数拼装
   （断言 `configoption[<配置项 id>]` 与 `cart_data[configoptions][<配置项 id>]`），
   真机行为留待 5b 服务操作（重装选系统等）顺带复核。
5. **终止申请（RequestCancel）**：返回 `status=202` + `pending=true` +
   `data.cancel_request_id=432`，主机 `domainstatus` 保持 `Active`（上游异步处理，同 8.5 第 13 条）；
   本地实例状态本批**不同步**（服务操作与状态同步留 5b）。

## 15. 变更记录

| 日期 | 版本 | 变更内容 |
| --- | --- | --- |
| 2026-10-08 | v8 | 阶段 5a：新增第 14 节「订单交付与自动开通」——**订单状态机扩为 6 态**（`pending → paid → provisioning → active / failed`，`cancelled` 仅 `pending`；迁移 0007 ALTER ENUM 并新增 `host_id` / `provision_error` / `delivered_at` 列；**入账幂等集合扩为交付态**：`paid`/`provisioning`/`active`/`failed` 重复回调一律幂等、不重复触发交付）；**instances 表**（订单↔实例一对一 + 上游主机 ID 唯一；订单快照字段、上游同步字段（到期时间/domainstatus/IP/端口/账号密码）、状态枚举（本期只写 `active`，后三者留 5b）；敏感字段仅会员本人详情可见、不进列表与日志）；**自动交付时序**（入账提交后触发、不阻塞回调；认领行锁幂等；`CreateHost` 开通参数拼装（pid/周期映射/host 生成/16 位随机密码/`configoption` 快照）；回读失败不阻断交付；失败置 `failed` + 脱敏原因；超时与落库兜底；进程崩溃悬挂为已知边界；可测性以 `DeliveryTrigger` 注入同步实现）；**接口**（会员端 `GET /instances`、`GET /instances/:id`；管理端 `GET /admin/instances`、`POST /admin/orders/:id/retry-delivery`（仅 admin，同步执行、失败仍 200 返回订单供处置））；同步更新 12.3（状态机与列）/12.4（订单视图三字段与状态提示）/12.7（角色矩阵）/12.8（错误码）/12.9（边界第 1、5 条）/12.10（第 7 条改为已落地+5b 范围）；**真机实测差异（14.5）**——下单/交付 `configoption` 键值口径修正为上游本地 id（上游源码与生产数据佐证：真实商品 `upstream_id` 恒为 0 且未识别键被静默忽略），10.3/12.3/12.4/14.3 同步；**真机全链路演练**（订单 O20261008105520T0J03J → host 10922 → 回读核对一致 → 终止申请 cancel_request_id=432）；变更记录移到第 15 节 |
| 2026-10-08 | v7 | 阶段 4+：新增第 13 节「站点安装向导」——**安装状态机**（无 DSN / 库不可达（含修复模式与 `db=down`）/ 表缺失 / 无管理员 / 存量库自动补标记五种场景 + 续装态 `site_missing`/`pending`；`install.progress` 区分「向导走了一半」与「存量库」；任意步刷新页面或重启进程后按 `state`+`progress` 落回正确步骤，不回退不错位）；**安装模式请求分发**（`/install` 与安装 API 放行、`/api/v1/health` 照常、其它 API `503`+`50301`、浏览器导航 `302` 跳转；装完后 `/install` 永久关闭，重访为「系统已安装」提示页，安装 API 一律 `50302`）；**安装页与 9 个安装 API**（内嵌 HTML/CSS/JS 不依赖前端构建产物、字段校验、自动建库、连接失败按 MySQL 错误码给处置建议）；**默认管理员替换策略**（复用迁移 0003 行改写、库内不得残留默认哈希行、禁用 `admin/admin123456` 组合）；**配置文件合并写入规范**（生效路径、保留既有键与注释、原子替换、0600、只写部署级参数）；**并发与一次性保护**（进程内写锁 + `installed` 条件插入）；**免重启热切换**（同锁内换数据库句柄/JWT 密钥/引擎，`restart_required` 恒 false）；**安全边界**（无鉴权的风险与「装完即关」缓解、密钥不回显不落日志）；错误码新增 `50301`/`50302`/`50303`；**安装页「重启续装」两处 UI 修复**（`status` 新增 `admin_username`/`site_name` 供完成页摘要展示库内实况、第 2 步提交后按最新状态落位而非固定跳第 3 步）；12.1 的 settings 键补充 `site`/`installed`/`install.progress`；变更记录移到第 14 节 |
| 2026-10-08 | v6 | 阶段 4：新增第 12 节「支付与财务」——**后台设置机制**（settings 表 + `GET/PUT /admin/settings/payment/epay` 与 `/admin/settings/upstream`，密钥三态与脱敏、审计、读时校验按内容失效的生效方式、渠道可插拔扩展方式）；**易支付渠道**（彩虹标准协议：下单/签名/回调验签/同步跳转/`out_trade_no` 策略/应答口径/错误分支矩阵）；**订单与充值单/余额/流水**（数据模型与状态机、会员端下单/支付（epay + balance）/取消/充值/余额/流水接口、回调入账幂等与金额校验、管理端对账接口、角色矩阵）；**优惠码应用口径**（下单抵扣 + 支付成功条件自增 `used_count` + 极端并发超用不阻断）；更新 8.6（上游参数来源=后台设置，`config.yaml` 的 `upstream` 段停用）、第 9 节（探活按设置取参、掩码口径说明）、11.1/11.6（折扣应用已实现与并发边界）、错误码表新增 `50002`；变更记录移章（v6 时位于第 13 节，v7 起为第 14 节） |
| 2026-10-08 | v5 | 阶段 3b：计费周期 4 → 6（新增 `biennial` / `triennial`，上游字段 `biennially` / `triennially` 映射与中文显示名入契约；`upstream_prices_json`、`pricing_json.fixed`、会员端 `prices` 同步扩为 6 键）；新增第 11 节「优惠码」（coupons 数据模型、管理端 CRUD、公开校验接口 `GET /coupons/:code/validate`、折扣计算口径与 reason 枚举、暂不支持清单）；变更记录补记 v4 |
| 2026-10-08 | v4 | 阶段 3a（补记）：新增第 10 节「商品与计费」（上游导入与幂等、`upstream_prices_json` 缓存、upstream/markup/fixed 三模式定价、上下架校验、会员端只读目录、管理端接口与角色矩阵、生产上游实测差异） |
| 2026-10-08 | v3 | 阶段 2：新增第 8 节「上游对接」（鉴权机制、`{status,msg,data}` 与状态码映射、上游接口清单、实测示例、实测与文档不符之处/字段类型踩坑）与第 9 节「管理端上游探活接口」；本阶段对生产上游完成真机联调（开通/开关机/重启/重装/暂停/恢复/续费/取消申请） |
| 2026-10-08 | v2 | 阶段 1：新增第 6 节「认证与账号」（会员注册/登录/资料/改密、管理员登录/资料/会员列表/启禁用）、1.1 认证方式（JWT HS256 + aud 区分两类 token）、1.2 时间与时区（DATETIME 存 UTC）、RBAC 矩阵与开发默认管理员说明 |
| 2026-10-08 | v1 | 阶段 0：建立统一响应包、错误码表与 `/api/v1/health` 契约 |
