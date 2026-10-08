-- 0005 建立优惠码表（阶段 3b：规则与校验；折扣应用与使用记账留到订单/支付阶段）。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true&loc=UTC）。
--
-- 字段说明（契约见 docs/api-contract.md 优惠码章节）：
--   code        大小写不敏感唯一（显式 COLLATE utf8mb4_general_ci），3-32 位 [A-Za-z0-9_-]；
--   value       percent 时表示百分比（0 < x <= 100），fixed 时表示固定减免金额（> 0）；
--   cycles_json 适用周期的 JSON 数组文本（如 ["annual","biennial"]），空数组 [] 表示全部 6 周期；
--   used_count  本阶段只读不增（扣减与并发控制留到订单阶段），max_uses=0 表示不限次数；
--   status      停用即 off（本阶段不提供 DELETE，契约写明）。
CREATE TABLE `coupons` (
    `id`          BIGINT UNSIGNED   NOT NULL AUTO_INCREMENT COMMENT '本地优惠码 ID（对外主键）',
    `code`        VARCHAR(64)       COLLATE utf8mb4_general_ci NOT NULL COMMENT '优惠码（3-32 位 [A-Za-z0-9_-]，大小写不敏感唯一）',
    `type`        ENUM('percent','fixed') NOT NULL              COMMENT '折扣类型：percent 按比例 / fixed 固定减免',
    `value`       DECIMAL(12,2)     NOT NULL                COMMENT '折扣值：percent 为百分比（0<x<=100）/ fixed 为减免金额（>0）',
    `cycles_json` VARCHAR(255)      NOT NULL DEFAULT '[]'   COMMENT '适用周期 JSON 数组文本（空数组=全部 6 周期；元素须属 monthly/quarterly/semiannual/annual/biennial/triennial）',
    `starts_at`   DATETIME          NULL                    COMMENT '生效时间（UTC，NULL=立即生效）',
    `expires_at`  DATETIME          NULL                    COMMENT '过期时间（UTC，NULL=永不过期）',
    `max_uses`    INT UNSIGNED      NOT NULL DEFAULT 0      COMMENT '最大使用次数（0=不限）',
    `used_count`  INT UNSIGNED      NOT NULL DEFAULT 0      COMMENT '已使用次数（本阶段只读不增）',
    `status`      ENUM('on','off')  NOT NULL DEFAULT 'on'   COMMENT '状态：on 启用 / off 停用（停用取代删除）',
    `comment`     VARCHAR(255)      NOT NULL DEFAULT ''     COMMENT '备注（管理端可见）',
    `created_at`  DATETIME          NOT NULL                COMMENT '创建时间（UTC）',
    `updated_at`  DATETIME          NOT NULL                COMMENT '最后更新时间（UTC）',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_coupons_code` (`code`),
    KEY `idx_coupons_status` (`status`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '优惠码（规则与校验；折扣应用与使用记账留订单阶段）';
