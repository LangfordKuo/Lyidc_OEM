-- 回滚 0018：删掉交付在途的占位行（status=provisioning，host_id 为负数占位），
-- 再改回 host_id 无符号、收起 status 枚举、删 downstream_token 列。
--
-- 先删占位行是必须的：负数占位在 INT UNSIGNED 上没有合法表示，且这些行本就是「交付未完成」的内部行
-- （上游那一侧也没有对应主机），回滚时一并丢弃；已交付的实例行不受影响。

DELETE FROM `instances` WHERE `status` = 'provisioning';

ALTER TABLE `instances`
    MODIFY COLUMN `host_id` INT UNSIGNED NOT NULL
        COMMENT '上游主机 ID（hostinfo 的 hosts[].id，唯一）',
    MODIFY COLUMN `status` ENUM('active','suspended','cancelled','terminated') NOT NULL DEFAULT 'active'
        COMMENT '实例状态：active 正常 / suspended 暂停 / cancelled 已申请终止 / terminated 已终止（后三者由阶段 5b 维护，本批只写 active）',
    DROP COLUMN `downstream_token`;
