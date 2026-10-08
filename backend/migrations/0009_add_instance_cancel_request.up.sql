-- 0009 实例取消/终止申请（阶段 5c：会员申请取消 → 上游 RequestCancel → 状态收敛）。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true）。
--
-- 设计要点（契约见 docs/api-contract.md 15.8）：
--   1) 取消申请是**实例记录上的标记**，不引入新的中间状态：申请在途期间 instances.status
--      保持 active / suspended 原值不变（取消申请是正交元数据，见契约 15.8.1 状态机）。
--      原因：status 反映主机真实可用状态，收敛（上游 Deleted）时才转 terminated；
--      迁移 0007 预留的 cancelled 枚举值**本批起确认不再写入**（保留枚举、不做 DDL 变更）。
--   2) cancel_status 三态：none 无申请 / pending 申请在途（等待上游处理）/ done 已终止
--      （上游主机已删除或 domainstatus=Deleted/Terminated，本地已收敛为 terminated）。
--   3) instance_operation_logs.action 为 VARCHAR(32)（0008），本批新增取值
--      cancel / cancel_sync **不改类型**（枚举值登记在契约 15.4）；下方 MODIFY 只为同步列注释，
--      MySQL 5.7 对「仅改列注释」按就地元数据变更处理，不重建表、不锁写。
--   4) 不动 0001–0008 的任何结构。

ALTER TABLE `instances`
    ADD COLUMN `cancel_request_id` INT UNSIGNED NOT NULL DEFAULT 0
        COMMENT '上游取消申请 ID（POST /host/cancel 回带；0 表示无申请记录）' AFTER `password`,
    ADD COLUMN `cancel_type` VARCHAR(16) NOT NULL DEFAULT ''
        COMMENT '取消方式：immediate 立即取消 / end_of_billing 到期取消（提交上游时映射为 Immediate / Endofbilling；空串表示无申请）' AFTER `cancel_request_id`,
    ADD COLUMN `cancel_status` ENUM('none','pending','done') NOT NULL DEFAULT 'none'
        COMMENT '取消申请状态：none 无申请 / pending 申请在途 / done 已终止（上游已删除，本地 converged=terminated）' AFTER `cancel_type`,
    ADD COLUMN `cancel_reason` VARCHAR(255) NOT NULL DEFAULT ''
        COMMENT '取消申请原因（会员填写或服务端默认文案；管理端强制终止必填）' AFTER `cancel_status`,
    ADD COLUMN `cancel_requested_at` DATETIME NULL
        COMMENT '取消申请提交时间（UTC；无申请为 NULL）' AFTER `cancel_reason`,
    ADD KEY `idx_instances_cancel` (`cancel_status`, `cancel_requested_at`);

ALTER TABLE `instance_operation_logs`
    MODIFY COLUMN `action` VARCHAR(32) NOT NULL
        COMMENT '操作：create / power_on / power_off / reboot / hard_off / hard_reboot / reinstall / reset_password / suspend / unsuspend / sync / renew / cancel（提交取消申请）/ cancel_sync（上游确认终止后的本地收敛）';
