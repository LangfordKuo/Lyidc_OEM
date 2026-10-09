-- 0012 回滚：仅还原列注释到 0004 的原文口径。
-- 列本身属于 0004（自建表起即存在），回滚**不删除列**，避免丢数据。
ALTER TABLE `products`
    MODIFY COLUMN `description` TEXT NULL
        COMMENT '商品描述（上游原文，含 HTML 实体转义与换行）';
