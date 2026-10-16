-- 0020 回滚：删掉 `orders.coupon_counted`，并把 `coupons.used_count` 的列注释还原为 0005 原文。
-- 顺序与 up 相反：先还原注释，再删列。
--
-- **回滚不还原数据**：`used_count` 的当前值保持现状——它是业务事实（哪些订单占用了次数），
-- 不是结构。删掉 `coupon_counted` 之后，所有订单重新变回「未占用」，支付链路回到阶段 4 的
-- 老口径（支付成功时条件自增、超用只记 WARN 不阻断），与本次整改前的行为一致。
--
-- 注意：回滚**不会**把整改后已经计入的占用退回去，因此若在回滚前已有订单占用过次数，
-- 回滚后这些占用会随着「支付时再自增一次」被重复计入一次——这正是回到老口径的应有语义，
-- 不要试图用 UPDATE 补偿（补偿会破坏 `used_count` 与订单事实的对应关系）。

ALTER TABLE `coupons`
    MODIFY COLUMN `used_count` INT UNSIGNED NOT NULL DEFAULT 0
        COMMENT '已使用次数（本阶段只读不增）';

ALTER TABLE `orders`
    DROP COLUMN `coupon_counted`;
