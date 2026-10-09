# Lyidc_OEM API 契约

> **变更流程**：任何接口变更都必须先修改本文档，再修改后端与前端代码。评审时以本文档为准。
>
> 版本：v15（阶段 0 建立；阶段 1 认证与账号；阶段 2 上游对接与探活；阶段 3a 商品与计费；
> 阶段 3b 六周期 + 优惠码；阶段 4 设置机制 + 易支付 + 充值/余额/流水 + 下单与在线支付；
> 阶段 4+ 站点安装向导：首次访问浏览器完成部署，全程零文件编辑；阶段 5a/5b/5c 订单交付与自动开通、
> 实例操作与续费、取消/终止流程；阶段 6a 工单系统；阶段 6b 通知体系：站内通知 + 邮件 SMTP + 到期提醒 + 事件接线；
> 阶段 8 管理端订单接口；**R5 商品简介：上游 description 拉取落库 + 商品卡配置列表（`description_lines`）**）

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
| `prices` | 六周期本地售价；六个键**始终存在**，`null` 表示该周期不可售 |
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
    "group": { "id": 1, "name": "香港二区" },
    "config_groups": [
      {
        "id": 1,
        "name": "区域",
        "options": [
          {
            "id": 1, "name": "area|区域", "type": 12, "upstream_id": 0,
            "values": [
              { "id": 1, "name": "1|HK^香港", "upstream_id": 0, "qty_minimum": 0, "qty_maximum": 0 }
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
              { "id": 3, "name": "15|Debian^Debian-10.3.3-x64", "upstream_id": 0, "qty_minimum": 0, "qty_maximum": 0 },
              { "id": 9, "name": "66|CentOS^CentOS-7.9.2111-x64", "upstream_id": 0, "qty_minimum": 0, "qty_maximum": 0 }
            ]
          },
          {
            "id": 7, "name": "bw|带宽", "type": 11, "upstream_id": 0,
            "values": [ { "id": 34, "name": "带宽", "upstream_id": 0, "qty_minimum": 20, "qty_maximum": 100 } ]
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
   （契约 8.3 的 `configoption` 行：数量型传 `qty`）；取值范围与默认值取该值的
   `qty_minimum` / `qty_maximum`（默认值 = `qty_minimum`，步进 1；
   选项型的这两个字段恒为 0）。下单校验见 12.3。
7. 值名里的 `^` 前是「大类」（如 `15|Debian^Debian-10.3.3-x64` 的 `Debian`、
   `1|HK^香港` 的 `HK`），二级选择以它为大类分组依据（按值出现顺序去重）；
   `^` 后是具体显示名。

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

商品详情 = 列表项字段 + `description` + `description_lines` + `config_groups` + `custom_fields` + `upstream_prices`（上游价格缓存原文，含 `rows`）。`:id` 为本地商品 ID。管理端**列表**（`GET /admin/products`）不带 `description` / `description_lines`（避免列表响应膨胀），仅在详情下发。

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
| `key` | string | 设置键（主键）；本批两个：`payment.epay`、`upstream`；阶段 4+ 新增 `site`（站点信息）、`installed`（安装标记）、`install.progress`（安装进行中标记，临时），见第 13 节；**阶段 6b 新增 `email.smtp`（邮件通道）与 `notifications`（通知开关与到期提醒天数），见第 17 节** |
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
`amount` / `discount_amount` / `final_amount` / `coupon_code` / `status` / **`type`** /
`pay_channel` / `channel_trade_no` / `pay_time` / `host_id` / **`instance_id`** / `provision_error` /
`delivered_at` / `created_at` / `updated_at`（加粗两项为阶段 5b 新增：`type` 区分新购 `new` /
续费 `renew`，`instance_id` 为续费单对应的实例 ID）。

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
| `host_id` | int | 上游主机 ID（`hostinfo` 的 `hosts[].id`） |
| `product_id` / `product_name` | int / string | 本地商品 ID 与商品名快照（订单快照） |
| `name` | string | 主机名（开通时提交上游的 `host`：`oem-` + 订单号小写） |
| `billing_cycle` | string | 计费周期（本地 6 周期之一） |
| `next_due_date` | string \| null | 到期时间（UTC；开通后从上游 `nextduedate` 回读，回读失败留 `null`） |
| `status` | string | `active` / `suspended` / `cancelled` / `terminated`；5a 只写 `active`，**5b 起 `suspended` 由管理端暂停与到期扫描写入**，**5c 起 `terminated` 由终止收敛写入**；`cancelled` 经 5c 定稿为**不再写入的预留枚举**（申请在途以 `cancel_status=pending` 表达，见 15.8.1，操作矩阵见 15.1） |
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
| `configoption[<配置项 id>]` | `orders.config_json` 快照原样（键为配置项 id、值为所选值 id；数量型配置的值为数量，见 10.3 第 6 条 / 12.3 / 14.5 第 1 条） |
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
| 金额 | 按**当前商品**该周期本地售价（商品下架仍可续费：已购实例不受上架状态影响；该周期无售价返回 `40002`） |
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

## 18. 变更记录

| 日期 | 版本 | 变更内容 |
| --- | --- | --- |
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
