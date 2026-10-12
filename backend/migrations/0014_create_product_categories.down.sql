-- 0014 回滚：先摘掉商品归属列，再删分类表。
-- 注意：回滚会丢失商品的分类归属与分类树本身（product_groups 不受影响，仍可按上游组筛选）。
ALTER TABLE `products`
    DROP KEY `idx_products_category`,
    DROP COLUMN `category_source`,
    DROP COLUMN `category_id`;

DROP TABLE IF EXISTS `product_categories`;
