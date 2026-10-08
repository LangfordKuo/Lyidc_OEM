-- 0007 回滚：删除实例表并把 orders 收敛回 0006 的形态。
-- 先把 0007 引入的交付状态收敛为 paid（枚举收窄前必须，否则 ALTER 会截断失败），
-- 再删除交付相关列、恢复原 ENUM。

UPDATE `orders` SET `status` = 'paid' WHERE `status` IN ('provisioning', 'active', 'failed');

ALTER TABLE `orders`
    MODIFY COLUMN `status` ENUM('pending','paid','cancelled') NOT NULL DEFAULT 'pending'
        COMMENT '状态：pending 待支付 / paid 已支付 / cancelled 已取消',
    DROP COLUMN `delivered_at`,
    DROP COLUMN `provision_error`,
    DROP COLUMN `host_id`;

DROP TABLE IF EXISTS `instances`;
