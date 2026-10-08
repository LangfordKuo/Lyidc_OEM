-- 0004 回滚：删除商品表与分组表（先删商品，避免残留悬挂的分组外键语义）。
DROP TABLE IF EXISTS `products`;
DROP TABLE IF EXISTS `product_groups`;
