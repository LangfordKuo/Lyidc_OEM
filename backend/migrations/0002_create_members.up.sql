-- 0002 建立会员表（阶段 1 账号体系）。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true&loc=UTC，见 docs/api-contract.md 第 1.2 节）。
-- 金额语义：balance 为 DECIMAL(14,2) 定点数，阶段 1 仅落字段，流水表留待阶段 4（支付财务）新建 ledger。
CREATE TABLE `members` (
    `id`            BIGINT UNSIGNED            NOT NULL AUTO_INCREMENT COMMENT '会员 ID',
    `username`      VARCHAR(32)                NOT NULL                COMMENT '登录用户名（唯一，utf8mb4_general_ci 大小写不敏感）',
    `email`         VARCHAR(128)               NOT NULL                COMMENT '邮箱（唯一，utf8mb4_general_ci 大小写不敏感）',
    `password_hash` VARCHAR(100)               NOT NULL                COMMENT 'bcrypt 密码哈希，禁止对外返回',
    `nickname`      VARCHAR(32)                NOT NULL DEFAULT ''     COMMENT '昵称',
    `phone`         VARCHAR(32)                NULL                    COMMENT '手机号（可空）',
    `status`        ENUM('active','disabled')  NOT NULL DEFAULT 'active' COMMENT '账号状态：active 正常 / disabled 已禁用',
    `balance`       DECIMAL(14,2)              NOT NULL DEFAULT 0.00   COMMENT '账户余额（单位：元；流水表留待阶段 4）',
    `created_at`    DATETIME                   NOT NULL                COMMENT '创建时间（UTC）',
    `updated_at`    DATETIME                   NOT NULL                COMMENT '最后更新时间（UTC）',
    `last_login_at` DATETIME                   NULL                    COMMENT '最后登录时间（UTC，可空）',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_members_username` (`username`),
    UNIQUE KEY `uk_members_email` (`email`),
    KEY `idx_members_status` (`status`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '会员账号';
