-- 0017 导入时按上游分组筛选 + 「上游分组 → 本地子分类」映射（R12：契约 10.1 / 10.4）。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true&loc=UTC）。
--
-- 设计要点（口径见 docs/api-contract.md 10.1）：
--   1) 一列上游分组对应**至多一条**映射，故 upstream_group_id 建 UNIQUE；
--      upsert 语义（PUT /admin/product-group-maps/:upstream_group_id）由该唯一键保证。
--   2) category_id 必须指向**子分类**（product_categories.parent_id IS NOT NULL）——
--      商品只挂子分类（R7 约定），映射自然也只能落到子分类。该约束由应用层校验
--      （store 先查 SubCategoryByID 再写），本表**不建外键**：与 products / product_groups /
--      product_categories 既有风格一致，且「分类被外部删除」的降级路径（契约 10.1）
--      需要能读到悬空行，外键会直接拒绝而非让应用层优雅回落自动派生。
--   3) 不建外键的另一面：product_groups / product_categories 删除本表不联动；
--      导入侧对「目标分类已失效」做显式降级（回落自动派生 + 响应/日志体现），
--      映射列表接口对悬空行输出空名字供页面提示「目标分类已失效」。
--
-- 幂等性：CREATE TABLE 由 golang-migrate 版本号保证只跑一次。

CREATE TABLE `product_group_category_maps` (
    `id`                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '本地映射 ID',
    `upstream_group_id` INT UNSIGNED    NOT NULL                COMMENT '上游分组 ID（唯一，对应 product_groups.upstream_group_id）',
    `category_id`       BIGINT UNSIGNED NOT NULL                COMMENT '目标本地子分类 ID（product_categories.id，必须 parent_id 非空；不建外键）',
    `created_at`        DATETIME        NOT NULL                COMMENT '创建时间（UTC）',
    `updated_at`        DATETIME        NOT NULL                COMMENT '最后更新时间（UTC）',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_product_group_category_maps_group` (`upstream_group_id`),
    KEY `idx_product_group_category_maps_category` (`category_id`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '上游分组 → 本地子分类映射（导入时优先于自动派生；契约 10.1）';
