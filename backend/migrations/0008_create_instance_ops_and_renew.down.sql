-- 0008 回滚：删除实例操作审计表，并把 orders 收敛回 0007 的形态（去掉 type / instance_id）。

DROP TABLE IF EXISTS `instance_operation_logs`;

ALTER TABLE `orders`
    DROP COLUMN `instance_id`,
    DROP COLUMN `type`;
