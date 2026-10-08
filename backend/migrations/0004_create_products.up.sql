-- 0004 建立商品目录表（阶段 3a：上游导入 + 本地定价 + 上下架）。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true&loc=UTC）。
--
-- 字段归属（导入接口的写入边界，见 docs/api-contract.md 商品与计费章节）：
--   上游字段：name / description / type / module / config_json / upstream_prices_json / stock_qty / ontrial_max
--            —— 每次导入按上游最新值覆盖；
--   本地字段：pricing_json / status / sort（products）、name / sort（product_groups）
--            —— 导入只创建、不覆盖，避免把本地定价与上下架状态冲掉。
CREATE TABLE `product_groups` (
    `id`                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '本地分组 ID（管理端过滤/排序用）',
    `upstream_group_id` INT UNSIGNED    NOT NULL                COMMENT '上游分组 ID（GET /cart/all 的 data.products[].id）',
    `name`              VARCHAR(128)    NOT NULL                COMMENT '分组名（首次导入取自上游，可在管理端重命名）',
    `sort`              INT             NOT NULL DEFAULT 0      COMMENT '排序值（升序；首次导入按上游顺序写入，可在管理端修改）',
    `created_at`        DATETIME        NOT NULL                COMMENT '创建时间（UTC）',
    `updated_at`        DATETIME        NOT NULL                COMMENT '最后更新时间（UTC）',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_product_groups_upstream` (`upstream_group_id`),
    KEY `idx_product_groups_sort` (`sort`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '商品分组（上游产品组的本地映射）';

CREATE TABLE `products` (
    `id`                   BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT COMMENT '本地商品 ID（对外主键）',
    `upstream_pid`         INT UNSIGNED     NOT NULL                COMMENT '上游商品 ID（GET /cart/all 的 products[].id，下单时作为 pid）',
    `upstream_group_id`    INT UNSIGNED     NOT NULL                COMMENT '所属上游分组 ID（对应 product_groups.upstream_group_id）',
    `name`                 VARCHAR(255)     NOT NULL                COMMENT '商品名（导入自上游）',
    `description`          TEXT             NULL                    COMMENT '商品描述（上游原文，含 HTML 实体转义与换行）',
    `type`                 VARCHAR(32)      NOT NULL DEFAULT ''     COMMENT '上游商品类型（实测如 dcimcloud）',
    `module`               VARCHAR(64)      NOT NULL DEFAULT ''     COMMENT '上游模块标识（实测如 idcsmart_common）',
    `config_json`          MEDIUMTEXT       NULL                    COMMENT 'GET /cart/get_product_config 的 data 原文缓存（可配置项/自定义字段/价格行，实测单商品最大约 22KB）',
    `upstream_prices_json` TEXT             NULL                    COMMENT '上游周期价格原文与挑选结果：{code, prices{四周期}, rows[]}（负值 -1.00 表示该周期不售）',
    `pricing_json`         VARCHAR(1024)    NOT NULL DEFAULT '{"mode":"upstream"}' COMMENT '本地定价规则（仅管理端可改；mode=upstream/markup/fixed）',
    `stock_qty`            INT              NOT NULL DEFAULT 0      COMMENT '上游库存数量（stock_control=1 时有效，见 config_json.products.stock_control）',
    `ontrial_max`          INT              NOT NULL DEFAULT 0      COMMENT '上游可试用数量（0 表示不提供试用）',
    `status`               ENUM('on','off') NOT NULL DEFAULT 'off'  COMMENT '上架状态：on 上架（会员端可见）/ off 下架（仅管理端可见）',
    `sort`                 INT              NOT NULL DEFAULT 0      COMMENT '排序值（升序；首次导入按上游顺序写入，可在管理端修改）',
    `created_at`           DATETIME         NOT NULL                COMMENT '创建时间（UTC）',
    `updated_at`           DATETIME         NOT NULL                COMMENT '最后更新时间（UTC）',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_products_upstream_pid` (`upstream_pid`),
    KEY `idx_products_group` (`upstream_group_id`),
    KEY `idx_products_status` (`status`),
    KEY `idx_products_sort` (`sort`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '商品（上游商品 + 本地定价/上下架）';
