-- 0013 回滚：删除订单差价明细快照列。
-- 列内是计价快照，删除后对账明细丢失（成交价 amount 本身仍在 orders 上，不受影响）。
ALTER TABLE `orders`
    DROP COLUMN `price_breakdown_json`;
