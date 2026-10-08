-- 0008 实例操作与续费（阶段 5b：服务操作 + 续费链路 + 到期暂停扫描的审计表）。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true）。
--
-- 设计要点（契约见 docs/api-contract.md 第 15 节）：
--   1) orders 扩两个列：type 区分新购（new）/ 续费（renew，本批新增的订单类型）；
--      instance_id 记录续费单对应的实例（new 单为 NULL，实例反向由 instances.order_id 关联）。
--   2) instance_operation_logs 是**实例操作审计**：所有实例操作（会员端电源/重装/改密、
--      管理端暂停/恢复/同步、系统自动开通/续费/到期暂停）无论成功失败都留痕；
--      message 为脱敏结果说明（**不含密码/密钥**）。
--   3) 不动 0001–0007 的任何结构；instances 状态枚举沿用 0007 的 4 态
--      （本批起 suspended 由管理端暂停与到期扫描写入）。

ALTER TABLE `orders`
    ADD COLUMN `type`        ENUM('new','renew') NOT NULL DEFAULT 'new'
        COMMENT '订单类型：new 新购 / renew 续费（阶段 5b 新增）' AFTER `status`,
    ADD COLUMN `instance_id` BIGINT UNSIGNED NULL
        COMMENT '续费单对应的实例 ID（instances.id；new 单为 NULL）' AFTER `host_id`;

CREATE TABLE `instance_operation_logs` (
    `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '日志 ID',
    `instance_id` BIGINT UNSIGNED NOT NULL                COMMENT '实例 ID（instances.id）',
    `actor_type`  ENUM('member','admin','system') NOT NULL COMMENT '操作者类型：member 会员 / admin 管理员 / system 系统自动',
    `actor_id`    BIGINT UNSIGNED NOT NULL DEFAULT 0      COMMENT '操作者 ID（member_id / admin_id；system 固定 0）',
    `action`      VARCHAR(32)     NOT NULL                COMMENT '操作：create 开通 / power_on / power_off / reboot / hard_off / hard_reboot / reinstall / reset_password / suspend / unsuspend / sync / renew',
    `status`      ENUM('success','fail') NOT NULL         COMMENT '结果：success 成功 / fail 失败（失败尝试同样留痕）',
    `message`     VARCHAR(512)    NOT NULL DEFAULT ''     COMMENT '结果说明（已脱敏：不含密码与密钥）',
    `created_at`  DATETIME        NOT NULL                COMMENT '发生时间（UTC）',
    PRIMARY KEY (`id`),
    KEY `idx_instance_logs_instance` (`instance_id`, `id`),
    KEY `idx_instance_logs_action` (`action`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '实例操作审计日志（所有实例操作含失败尝试与系统自动操作）';
