-- 0017 回滚：删除「上游分组 → 本地子分类」映射表。
-- 说明：纯新增表，不引用其它表的外键（upstream_group_id / category_id 都是裸 ID），直接 DROP 即可。
-- 注意：回滚会丢失全部映射配置；已按映射归类过的商品**库内归属不变**
-- （products.category_id / category_source 是导入时写入的快照，0017 不动 products），
-- 只是此后导入不再自动套用映射，回到 R7 的自动派生口径（契约 10.1）。

DROP TABLE IF EXISTS `product_group_category_maps`;
