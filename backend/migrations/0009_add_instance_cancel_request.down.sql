-- 0009 回滚：去掉 instances 的取消申请列与索引，并把审计 action 注释还原为 0008 形态。

ALTER TABLE `instances`
    DROP KEY `idx_instances_cancel`,
    DROP COLUMN `cancel_requested_at`,
    DROP COLUMN `cancel_reason`,
    DROP COLUMN `cancel_status`,
    DROP COLUMN `cancel_type`,
    DROP COLUMN `cancel_request_id`;

ALTER TABLE `instance_operation_logs`
    MODIFY COLUMN `action` VARCHAR(32) NOT NULL
        COMMENT '操作：create 开通 / power_on / power_off / reboot / hard_off / hard_reboot / reinstall / reset_password / suspend / unsuspend / sync / renew';
