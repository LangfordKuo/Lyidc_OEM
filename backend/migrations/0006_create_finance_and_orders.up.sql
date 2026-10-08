-- 0006 建立支付与财务表（阶段 4：后台设置、易支付渠道、充值/余额/流水、下单与在线支付）。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true&loc=UTC）。
-- 金额语义：全部 DECIMAL(14,2) 定点数，Go 侧与接口均用定点小数字符串，计算走整数分。
--
-- 设计要点（契约见 docs/api-contract.md 第 12 节）：
--   recharges.trade_no / orders.trade_no 为本地单号（R+UTC 时间+随机 / O+UTC 时间+随机），
--     下单支付时直接作为渠道 out_trade_no（重复发起支付沿用同一单号，见契约 12.2）；
--   ledger.amount 为**有符号**金额：入账为正（充值）、出账为负（余额支付订单）；
--   orders.config_json 是所选配置项快照 {"<配置项 upstream_id>": <所选值 upstream_id>}，
--     upstream_id 供阶段 5 开通时拼装 upstream 下单参数；
--   交付相关列（host_id 等）由阶段 5 迁移 ALTER 添加，本批不加。
CREATE TABLE `orders` (
    `id`               BIGINT UNSIGNED   NOT NULL AUTO_INCREMENT COMMENT '本地订单 ID（对外主键）',
    `trade_no`         VARCHAR(64)       NOT NULL                COMMENT '本地订单号（O+UTC 时间+随机；渠道 out_trade_no）',
    `member_id`        BIGINT UNSIGNED   NOT NULL                COMMENT '下单会员 ID',
    `product_id`       BIGINT UNSIGNED   NOT NULL                COMMENT '本地商品 ID（下单时快照）',
    `product_name`     VARCHAR(255)      NOT NULL                COMMENT '商品名快照（商品改名后订单仍显示下单时名称）',
    `cycle`            VARCHAR(16)       NOT NULL                COMMENT '计费周期（monthly/quarterly/semiannual/annual/biennial/triennial）',
    `qty`              INT UNSIGNED      NOT NULL DEFAULT 1      COMMENT '数量（本批固定 1）',
    `config_json`      VARCHAR(1024)     NOT NULL DEFAULT '{}'   COMMENT '所选配置项快照 JSON：{"<配置项 upstream_id>": <所选值 upstream_id>}',
    `amount`           DECIMAL(14,2)     NOT NULL                COMMENT '原价（下单时该商品该周期的本地售价快照）',
    `discount_amount`  DECIMAL(14,2)     NOT NULL DEFAULT 0.00   COMMENT '优惠码折扣额（0.00 表示未用码）',
    `final_amount`     DECIMAL(14,2)     NOT NULL                COMMENT '应付金额 = amount - discount_amount',
    `coupon_id`        BIGINT UNSIGNED   NULL                    COMMENT '所用优惠码 ID（可空；下单快照）',
    `coupon_code`      VARCHAR(64)       NOT NULL DEFAULT ''     COMMENT '所用优惠码原文快照（可空串；大小写不敏感的库内写法）',
    `status`           ENUM('pending','paid','cancelled') NOT NULL DEFAULT 'pending' COMMENT '状态：pending 待支付 / paid 已支付 / cancelled 已取消',
    `pay_channel`      VARCHAR(32)       NOT NULL DEFAULT ''     COMMENT '支付渠道（epay / balance；未支付为空串）',
    `channel_trade_no` VARCHAR(64)       NOT NULL DEFAULT ''     COMMENT '渠道单号（支付成功时记录）',
    `pay_time`         DATETIME          NULL                    COMMENT '支付时间（UTC；未支付为 NULL）',
    `created_at`       DATETIME          NOT NULL                COMMENT '创建时间（UTC）',
    `updated_at`       DATETIME          NOT NULL                COMMENT '最后更新时间（UTC）',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_orders_trade_no` (`trade_no`),
    KEY `idx_orders_member` (`member_id`),
    KEY `idx_orders_status` (`status`),
    KEY `idx_orders_product` (`product_id`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '订单（本批：创建/余额支付/在线支付回调；上游交付留阶段 5）';

CREATE TABLE `recharges` (
    `id`               BIGINT UNSIGNED   NOT NULL AUTO_INCREMENT COMMENT '本地充值单 ID（对外主键）',
    `trade_no`         VARCHAR(64)       NOT NULL                COMMENT '本地充值单号（R+UTC 时间+随机；渠道 out_trade_no）',
    `member_id`        BIGINT UNSIGNED   NOT NULL                COMMENT '充值会员 ID',
    `amount`           DECIMAL(14,2)     NOT NULL                COMMENT '充值金额（1.00 ~ 50000.00）',
    `channel`          VARCHAR(32)       NOT NULL                COMMENT '支付渠道（本批仅 epay）',
    `status`           ENUM('pending','paid','closed') NOT NULL DEFAULT 'pending' COMMENT '状态：pending 待支付 / paid 已入账 / closed 已关闭',
    `channel_trade_no` VARCHAR(64)       NULL                    COMMENT '渠道单号（支付成功时记录）',
    `created_at`       DATETIME          NOT NULL                COMMENT '创建时间（UTC）',
    `paid_at`          DATETIME          NULL                    COMMENT '到账时间（UTC；未支付为 NULL）',
    `expires_at`       DATETIME          NULL                    COMMENT '过期时间（UTC，可空；本批不自动关闭，留后续批次）',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_recharges_trade_no` (`trade_no`),
    KEY `idx_recharges_member` (`member_id`),
    KEY `idx_recharges_status` (`status`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '余额充值单（支付回调入账）';

CREATE TABLE `ledger` (
    `id`             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '流水 ID',
    `member_id`      BIGINT UNSIGNED NOT NULL                COMMENT '会员 ID',
    `type`           ENUM('recharge','order_pay','refund','adjust') NOT NULL COMMENT '流水类型：recharge 充值入账 / order_pay 余额支付扣款 / refund、adjust 预留',
    `amount`         DECIMAL(14,2)   NOT NULL                COMMENT '有符号金额：入账为正（+）、出账为负（-）',
    `balance_before` DECIMAL(14,2)   NOT NULL                COMMENT '变动前余额',
    `balance_after`  DECIMAL(14,2)   NOT NULL                COMMENT '变动后余额（= balance_before + amount）',
    `ref_type`       VARCHAR(32)     NOT NULL DEFAULT ''     COMMENT '关联单据类型：recharge / order（预留空串）',
    `ref_id`         BIGINT UNSIGNED NOT NULL DEFAULT 0      COMMENT '关联单据 ID（recharges.id / orders.id；0 表示无关联）',
    `note`           VARCHAR(255)    NOT NULL DEFAULT ''     COMMENT '备注（如「充值 R2026…」「订单支付 O2026…」）',
    `created_at`     DATETIME        NOT NULL                COMMENT '创建时间（UTC）',
    PRIMARY KEY (`id`),
    KEY `idx_ledger_member` (`member_id`),
    KEY `idx_ledger_type` (`type`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '余额流水（只增不改；refund/adjust 为预留类型）';

-- 通用后台设置（键值对，value 为 JSON 文本）。
--
-- 总原则：所有「用户可设置」的内容都由本表承载，经管理端设置接口读写，**不进 config.yaml**
-- （部署级参数——数据库连接、监听端口、JWT 密钥、日志等级——仍在 config.yaml）。
-- 本批承载两个键：payment.epay（易支付参数）与 upstream（上游对接参数）；
-- 后续阶段的站点/邮件等设置直接复用本表，无需再建表。
-- updated_by 记录最后修改的管理员 ID（审计用，NULL 表示从未经接口写入）。
CREATE TABLE `settings` (
    `key`        VARCHAR(128)    NOT NULL COMMENT '设置键（如 payment.epay / upstream）',
    `value`      TEXT            NULL     COMMENT '设置值（JSON 文本，结构与键一一对应）',
    `updated_by` BIGINT UNSIGNED NULL     COMMENT '最后修改的管理员 ID（审计用；NULL = 未记录）',
    `created_at` DATETIME        NOT NULL COMMENT '创建时间（UTC）',
    `updated_at` DATETIME        NOT NULL COMMENT '最后更新时间（UTC）',
    PRIMARY KEY (`key`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '通用后台设置（键值对；本批承载 payment.epay 与 upstream）';
