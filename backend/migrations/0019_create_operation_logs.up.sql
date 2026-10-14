-- 0019 操作日志（R18）：新建 operation_logs 三通道日志表（管理员 / 会员 / 定时任务）。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true&loc=UTC）。
--
-- 设计要点（契约见 docs/api-contract.md 第 23 节）：
--   1) 三个独立通道用 channel 区分：admin 管理员写操作（含登录成败）/ member 会员写操作 /
--      scheduler 定时任务每轮执行；**不含**渠道回调（payments/epay/notify）与上游回推
--      （POST /api/host/sync）——它们不是「某个人」的操作，前者有 orders/ledger 对账、
--      后者有 instance_operation_logs 审计。
--   2) actor_id + actor_name 是**快照**：actor_name 记录操作发生时的用户名/管理员用户名，
--      保证账号被改名或删除后日志仍可读、可搜；系统动作（定时任务）actor_id=0、actor_name 为空串。
--   3) status 是**结果**（success / failed，由 HTTP 状态码或任务结果推导），http_status 是原始
--      HTTP 状态码（定时任务恒 0）；两者分开，便于「只看失败」而不被 4xx 语义混淆。
--   4) summary 是**已脱敏**的可读业务摘要（密码、API 密钥、JWT、SMTP 口令绝不落库；
--      请求体整体不落库——只落方法 + 路由模板 + 状态码 + 耗时 + IP + 动作名 + 摘要）。
--   5) 关键字搜索走 FULLTEXT + ngram 解析器（MySQL 5.7 InnoDB 支持，中文可搜）：
--      索引列固定为 (action, summary, actor_name)，与存储层 MATCH(...) AGAINST 的列序一致；
--      ngram_token_size 缺省为 2，故**关键字至少 2 个字符**（单字符无法被 ngram 索引，
--      接口直接以 40001 拒绝，契约 23.5 已写明）。
--   6) 三条普通索引覆盖三种访问路径：按通道 + 时间倒序、按操作人 + 时间倒序、纯时间倒序。
--   7) **本表不做自动清理**（Langford 明确选择）：只靠管理员手动清空（按通道分批 DELETE /
--      清空全部 TRUNCATE），表会随业务持续增长。
--
-- 幂等性：CREATE TABLE 由 golang-migrate 版本号保证只跑一次；不改 0001–0018。

CREATE TABLE `operation_logs` (
    `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '日志 ID（仅用于展示与分页，不提供按条删除/修改）',
    `channel`     ENUM('admin','member','scheduler') NOT NULL
                                                          COMMENT '通道：admin 管理员写操作（含登录成败）/ member 会员写操作 / scheduler 定时任务执行',
    `actor_id`    BIGINT UNSIGNED NOT NULL DEFAULT 0       COMMENT '操作人 ID（按通道解释为 admins.id / members.id；未登录尝试与系统任务为 0）',
    `actor_name`  VARCHAR(64)     NOT NULL DEFAULT ''      COMMENT '操作人用户名快照（账号改名/删除后仍可读；系统任务为空串）',
    `action`      VARCHAR(128)    NOT NULL DEFAULT ''      COMMENT '可读动作名（如「修改上游设置」「管理员登录失败」；路由映射表 + 处理器补充）',
    `method`      VARCHAR(10)     NOT NULL DEFAULT ''      COMMENT 'HTTP 方法（定时任务为空串）',
    `path`        VARCHAR(191)    NOT NULL DEFAULT ''      COMMENT '路由模板（如 /api/v1/admin/products/:id；定时任务为空串）',
    `http_status` SMALLINT UNSIGNED NOT NULL DEFAULT 0     COMMENT 'HTTP 状态码（定时任务为 0；401/403 等被拒的写请求同样留痕）',
    `status`      ENUM('success','failed') NOT NULL DEFAULT 'success'
                                                          COMMENT '结果：success 成功 / failed 失败（由 HTTP 状态码或任务结果推导）',
    `ip`          VARCHAR(45)     NOT NULL DEFAULT ''      COMMENT '客户端 IP（IPv6 最长 45 字符；定时任务为空串）',
    `duration_ms` INT UNSIGNED    NOT NULL DEFAULT 0       COMMENT '处理耗时（毫秒；请求为结束耗时，定时任务为本轮耗时）',
    `summary`     VARCHAR(500)    NOT NULL DEFAULT ''      COMMENT '已脱敏的可读业务摘要（绝不包含密码/密钥/JWT/SMTP 口令；请求体整体不落库）',
    `created_at`  DATETIME        NOT NULL                 COMMENT '发生时间（UTC，新记录在前）',
    PRIMARY KEY (`id`),
    KEY `idx_operation_logs_channel_created` (`channel`, `created_at`),
    KEY `idx_operation_logs_actor_created` (`actor_id`, `created_at`),
    KEY `idx_operation_logs_created` (`created_at`),
    FULLTEXT KEY `ft_operation_logs_search` (`action`, `summary`, `actor_name`) WITH PARSER ngram
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '操作日志（R18：管理员 / 会员 / 定时任务三通道；只允许清空，不允许按条删改）';
