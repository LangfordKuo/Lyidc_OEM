-- 0010 工单系统（阶段 6a：会员提单 → 管理员/客服处理 → 关闭）。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true）。
--
-- 设计要点（契约见 docs/api-contract.md 第 16 节）：
--   1) 本迁移只**新建两张表**，不改动 0001–0009 已应用的任何结构（工单是独立业务域）。
--   2) tickets.trade_no 复用契约 12.2.5 的本地单号规则（前缀 T + UTC 时间 + 6 位随机），
--      唯一键 uk_tickets_trade_no 是换号重试的锚点（createWithTradeNo）。
--   3) status 三态：open 待客服处理 / replied 待会员 / closed 已关闭（终态，不可回复）。
--      last_reply_at = 最近一条**消息**时间（含管理员内部备注）：管理端待办排序锚点，
--      与 idx_tickets_status(status, last_reply_at) 配套。
--   4) ticket_messages.internal：管理员内部备注，仅管理端可见，会员端接口绝不返回（契约 16.3）。
--      创建工单与首条消息在同一事务内写入（「工单必有至少一条会员消息」是结构性保证）。

CREATE TABLE `tickets` (
    `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '工单 ID（对外主键）',
    `trade_no`      VARCHAR(32)     NOT NULL                COMMENT '工单号：T + UTC 时间（yyyyMMddHHmmss）+ 6 位随机大写字母/数字（契约 12.2.5 单号口径）',
    `member_id`     BIGINT UNSIGNED NOT NULL                COMMENT '提单会员 ID（members.id；会员端按该列隔离）',
    `instance_id`   BIGINT UNSIGNED NULL                    COMMENT '关联实例 ID（instances.id；可选，必须属于该会员）',
    `category`      ENUM('technical','billing','other') NOT NULL DEFAULT 'other'
                                                            COMMENT '分类：technical 技术 / billing 财务 / other 其他',
    `subject`       VARCHAR(100)    NOT NULL                COMMENT '标题（5-100 字符，首尾空白已裁剪）',
    `status`        ENUM('open','replied','closed') NOT NULL DEFAULT 'open'
                                                            COMMENT '状态：open 待客服处理 / replied 待会员 / closed 已关闭（终态）',
    `last_reply_at` DATETIME        NOT NULL                COMMENT '最近一条消息时间（UTC，含管理员内部备注）：列表排序与待办定位锚点',
    `closed_at`     DATETIME        NULL                    COMMENT '关闭时间（UTC；未关闭为 NULL）',
    `created_at`    DATETIME        NOT NULL                COMMENT '创建时间（UTC）',
    `updated_at`    DATETIME        NOT NULL                COMMENT '最后更新时间（UTC）',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_tickets_trade_no` (`trade_no`),
    KEY `idx_tickets_member` (`member_id`, `id`),
    KEY `idx_tickets_status` (`status`, `last_reply_at`),
    KEY `idx_tickets_instance` (`instance_id`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '工单（阶段 6a：会员提单 → 管理员/客服处理 → 关闭）';

CREATE TABLE `ticket_messages` (
    `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '消息 ID',
    `ticket_id`   BIGINT UNSIGNED NOT NULL                COMMENT '所属工单 ID（tickets.id）',
    `author_type` ENUM('member','admin') NOT NULL         COMMENT '作者类型：member 会员 / admin 管理员（客服同属 admin）',
    `author_id`   BIGINT UNSIGNED NOT NULL                COMMENT '作者 ID（按 author_type 解释为 members.id 或 admins.id）',
    `content`     TEXT            NOT NULL                COMMENT '正文（1-5000 字符；首尾空白已由服务端裁剪）',
    `internal`    TINYINT(1)      NOT NULL DEFAULT 0      COMMENT '管理员内部备注：1 仅管理端可见（会员端接口绝不返回）/ 0 公开消息',
    `created_at`  DATETIME        NOT NULL                COMMENT '发送时间（UTC）',
    PRIMARY KEY (`id`),
    KEY `idx_ticket_messages_ticket` (`ticket_id`, `id`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '工单消息（含管理员内部备注；首条消息即工单正文）';
