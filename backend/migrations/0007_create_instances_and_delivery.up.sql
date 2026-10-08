-- 0007 交付与实例（阶段 5a：支付成功 → 上游开通 → 实例落库）。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true&loc=UTC）。
--
-- 设计要点（契约见 docs/api-contract.md 第 14 节）：
--   1) orders.status 扩为 6 态：pending → paid → provisioning → active / failed；
--      cancelled 仅限 pending；failed 可由管理员重试（failed → provisioning → …）。
--   2) 交付相关列由本迁移 ALTER 添加（0006 注释预留）：host_id 为上游主机 ID；
--      provision_error 为最近一次交付失败原因（成功时清空）；delivered_at 为交付完成时间。
--   3) instances 表：订单交付成功后的主机记录，订单 ↔ 实例本期一对一（uk_instances_order）。
--      上游同步字段（next_due_date / upstream_status / dedicated_ip / assigned_ips / port /
--      username / password）在开通成功后从上游 hostinfo 回读落库。
--      username / password 为**敏感字段**：仅会员本人（详情接口）可见，不写日志、不进管理端列表。
--   4) instances.status 本期只写 active；suspended / cancelled / terminated 由阶段 5b
--      （服务操作与到期处理）维护，本批只预留枚举。

ALTER TABLE `orders`
    MODIFY COLUMN `status` ENUM('pending','paid','provisioning','active','failed','cancelled')
        NOT NULL DEFAULT 'pending'
        COMMENT '状态：pending 待支付 / paid 已支付待交付 / provisioning 交付中 / active 已交付 / failed 交付失败（可重试）/ cancelled 已取消（仅 pending 可取消）',
    ADD COLUMN `host_id`         INT UNSIGNED NULL        COMMENT '上游主机 ID（交付成功后写入；失败/未交付为 NULL）',
    ADD COLUMN `provision_error` VARCHAR(512) NOT NULL DEFAULT '' COMMENT '最近一次交付失败原因（脱敏；成功时清空）',
    ADD COLUMN `delivered_at`    DATETIME     NULL        COMMENT '交付完成时间（UTC；未交付为 NULL）';

CREATE TABLE `instances` (
    `id`              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '本地实例 ID（对外主键）',
    `member_id`       BIGINT UNSIGNED NOT NULL                COMMENT '所属会员 ID（订单快照）',
    `order_id`        BIGINT UNSIGNED NOT NULL                COMMENT '来源订单 ID（本期一对一，uk_instances_order）',
    `host_id`         INT UNSIGNED    NOT NULL                COMMENT '上游主机 ID（hostinfo 的 hosts[].id，唯一）',
    `product_id`      BIGINT UNSIGNED NOT NULL                COMMENT '本地商品 ID（订单快照）',
    `product_name`    VARCHAR(255)    NOT NULL                COMMENT '商品名快照（订单快照）',
    `name`            VARCHAR(255)    NOT NULL                COMMENT '主机名（开通时提交上游的 host 字段）',
    `billing_cycle`   VARCHAR(16)     NOT NULL                COMMENT '计费周期（本地 6 周期之一）',
    `next_due_date`   DATETIME        NULL                    COMMENT '到期时间（UTC；开通时从上游 nextduedate 回读，回读失败留 NULL）',
    `status`          ENUM('active','suspended','cancelled','terminated') NOT NULL DEFAULT 'active'
                                                              COMMENT '实例状态：active 正常 / suspended 暂停 / cancelled 已申请终止 / terminated 已终止（后三者由阶段 5b 维护，本批只写 active）',
    `upstream_status` VARCHAR(32)     NOT NULL DEFAULT ''     COMMENT '上游 domainstatus 原文（如 Active；同步字段）',
    `dedicated_ip`    VARCHAR(64)     NOT NULL DEFAULT ''     COMMENT '上游主 IPv4（hostinfo 的 dedicatedip；未返回为空串）',
    `assigned_ips`    VARCHAR(512)    NOT NULL DEFAULT ''     COMMENT '上游附加 IP（hostinfo 的 assignedips 原文按逗号连接；无则空串）',
    `port`            INT             NOT NULL DEFAULT 0      COMMENT '上游端口（hostinfo 的 port；0 表示未返回）',
    `username`        VARCHAR(128)    NOT NULL DEFAULT ''     COMMENT '主机用户名（敏感：仅会员本人可见，不进日志）',
    `password`        VARCHAR(255)    NOT NULL DEFAULT ''     COMMENT '主机密码（敏感：仅会员本人可见，不进日志）',
    `created_at`      DATETIME        NOT NULL                COMMENT '创建时间（UTC，交付完成时刻）',
    `updated_at`      DATETIME        NOT NULL                COMMENT '最后更新时间（UTC）',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_instances_order` (`order_id`),
    UNIQUE KEY `uk_instances_host` (`host_id`),
    KEY `idx_instances_member` (`member_id`),
    KEY `idx_instances_status` (`status`),
    KEY `idx_instances_next_due_date` (`next_due_date`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '实例（订单交付成功后的上游主机记录；订单↔实例本期一对一）';
