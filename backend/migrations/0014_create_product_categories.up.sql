-- 0014 商品两级分类（R7：主分类 / 子分类，契约 10.1）。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true&loc=UTC）。
--
-- 背景与取舍：
--   1) 不给 product_groups 加 parent_id：子分类名与上游组名不是一一对应——
--      子分类「香港二区」由**商品名**派生（6 个商品分属上游组「二区 CN2标准性能」），
--      子分类「美国一区」更是横跨 3 个上游组（「一区 Kurun精品」「传家宝」「美国一区 CERA高防」）；
--      一个上游组也可能拆到多个子分类。单表父子列表达不了这种多对多。
--   2) 新表 product_categories（parent_id 两级）+ products.category_id 挂到**子分类**：
--      分类是商品级归属，与 product_groups（上游组映射，服务导入幂等与管理端过滤）正交。
--   3) key 是**派生稳定键**（唯一）：主分类 `m:<地域>`，子分类 `z:<区段>` 或 `g:<upstream_group_id>`。
--      导入按 key find-or-create，管理员改 name 不会导致重建或漂移（契约 10.1）。
--   4) category_source 标记归属来源：`derived`（导入派生，可被再次刷新）/
--      `manual`（管理员手工改挂，导入**不再覆盖**）。
--
-- 幂等性：CREATE TABLE / ADD COLUMN 由 golang-migrate 版本号保证只跑一次。
CREATE TABLE `product_categories` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '本地分类 ID',
    `parent_id`  BIGINT UNSIGNED NULL                    COMMENT '父分类 ID；NULL = 主分类，非空 = 子分类（仅两级）',
    `key`        VARCHAR(64)     NOT NULL                COMMENT '派生稳定键（m:<地域> / z:<区段> / g:<上游组 id>），导入按它查找，改名不漂移',
    `name`       VARCHAR(128)    NOT NULL                COMMENT '分类名（本地字段：首次派生写入，之后只能在管理端改）',
    `sort`       INT             NOT NULL DEFAULT 0      COMMENT '排序值（升序；主分类按地域固定序，子分类按派生出现序）',
    `created_at` DATETIME        NOT NULL                COMMENT '创建时间（UTC）',
    `updated_at` DATETIME        NOT NULL                COMMENT '最后更新时间（UTC）',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_product_categories_key` (`key`),
    KEY `idx_product_categories_parent` (`parent_id`, `sort`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '商品两级分类（主分类 parent_id IS NULL / 子分类 parent_id 指向主分类）';

ALTER TABLE `products`
    ADD COLUMN `category_id` BIGINT UNSIGNED NOT NULL DEFAULT 0
        COMMENT '所属子分类 ID（product_categories.id，指向 parent_id 非空的行）；0 = 尚未归类'
        AFTER `upstream_group_id`,
    ADD COLUMN `category_source` VARCHAR(16) NOT NULL DEFAULT 'derived'
        COMMENT '归属来源：derived=导入按商品名/组名派生（可刷新）/ manual=管理员手工改挂（导入不覆盖）'
        AFTER `category_id`,
    ADD KEY `idx_products_category` (`category_id`);
