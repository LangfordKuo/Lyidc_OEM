-- 0011 通知体系（阶段 6b：站内通知 + 邮件 SMTP + 到期提醒）。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true）。
--
-- 设计要点（契约见 docs/api-contract.md 第 17 节）：
--   1) 本迁移**新建两张表**并**给 instances 扩一列**（到期提醒去重锚点），
--      不改动 0001–0010 已应用的任何结构。
--   2) notifications 是**收件箱**：一行 = 一个接收方的一条通知。接收方用
--      (recipient_type, recipient_id) 二选一解释为 members.id 或 admins.id；
--      管理端通知按「admin + support 逐个账号」扇出（不做广播行，读/未读状态按人记录）。
--   3) notifications.event 是事件枚举（契约 17.4 事件接线表的键）：同一 event 在会员侧与
--      管理端侧含义由 recipient_type 解释（如 ticket_replied：会员侧=客服回复了你的工单，
--      管理端侧=会员回复了工单）。
--   4) read_at 为 NULL 表示未读；重复已读是幂等 UPDATE（不覆盖首次时间），
--      idx_notifications_unread 同时服务「未读列表」「未读计数」两个查询。
--   5) instances.expiry_reminded_due 是**到期提醒去重锚点**：
--      记录「已就哪个到期时间提醒过」，等于 instances.next_due_date 时表示本周期的提醒已完成；
--      续费/同步把 next_due_date 推进后不等于该值，提醒自动重新武装（契约 17.5）。
--      用列而不是 notifications 反查：开关关闭时不产生通知行，去重不能依赖通知行是否存在。
--   6) email_logs 是**邮件发送留痕**（同步发送，成功与失败都记一行）：error 已脱敏，
--      绝不落 SMTP 密码；本表不参与任何业务事务，写入失败只记服务日志。

CREATE TABLE `notifications` (
    `id`             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '通知 ID（对外主键）',
    `recipient_type` ENUM('member','admin') NOT NULL         COMMENT '接收方类型：member 会员 / admin 管理员（含客服）',
    `recipient_id`   BIGINT UNSIGNED NOT NULL                COMMENT '接收方 ID（按 recipient_type 解释为 members.id 或 admins.id）',
    `event`          ENUM('order_delivered','order_failed','renew_succeeded','instance_suspended','instance_terminated','ticket_created','ticket_replied','ticket_closed','expiry_reminder') NOT NULL
                                                             COMMENT '事件类型（契约 17.4 事件接线表）',
    `title`          VARCHAR(120)    NOT NULL                COMMENT '标题（纯文本，不含敏感信息）',
    `content`        VARCHAR(1000)   NOT NULL                COMMENT '正文（纯文本，不含密码/IP 等敏感信息）',
    `read_at`        DATETIME        NULL                    COMMENT '已读时间（UTC；未读为 NULL）',
    `created_at`     DATETIME        NOT NULL                COMMENT '创建时间（UTC）',
    PRIMARY KEY (`id`),
    KEY `idx_notifications_recipient` (`recipient_type`, `recipient_id`, `id`),
    KEY `idx_notifications_unread` (`recipient_type`, `recipient_id`, `read_at`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '站内通知收件箱（阶段 6b：会员与管理员各读各的）';

CREATE TABLE `email_logs` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '日志 ID',
    `to_addr`    VARCHAR(255)    NOT NULL                COMMENT '收件人地址',
    `subject`    VARCHAR(255)    NOT NULL                COMMENT '主题（纯文本，不含敏感信息）',
    `status`     ENUM('success','fail') NOT NULL         COMMENT '发送结果：success 成功 / fail 失败',
    `error`      VARCHAR(500)    NOT NULL DEFAULT ''     COMMENT '失败原因（已脱敏，绝不含 SMTP 密码；成功为空串）',
    `created_at` DATETIME        NOT NULL                COMMENT '发送时间（UTC）',
    PRIMARY KEY (`id`),
    KEY `idx_email_logs_created` (`created_at`),
    KEY `idx_email_logs_status` (`status`, `created_at`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '邮件发送留痕（阶段 6b；同步发送，成功与失败都记）';

-- 到期提醒去重锚点（契约 17.5）：记录「已就哪个到期时间提醒过」。
ALTER TABLE `instances`
    ADD COLUMN `expiry_reminded_due` DATETIME NULL
        COMMENT '到期提醒去重锚点：已提醒过的到期时间（UTC）；与 next_due_date 相等表示本周期已提醒'
        AFTER `next_due_date`;
