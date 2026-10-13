-- 0018 上游状态回推（R16）：instances 补 downstream_token、status 扩 provisioning、host_id 转带符号 INT。
-- 时间语义与其他迁移一致：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true&loc=UTC）。
--
-- 设计要点（契约见 docs/api-contract.md 第 22 节）：
--   1) downstream_token：上游回推（POST /api/host/sync）的鉴权串，32 位十六进制。
--      开通时生成一次、此后不再变更（重试沿用同一串）；存量老实例为空串——它们不参与回推，
--      本迁移**不做数据回填**。
--   2) status 新增 'provisioning'：交付在途（提交上游开通之前就落库的那一行）。
--      这是纯内部态：所有实例列表接口一律排除它，其余操作矩阵与既有四态一致（仅 active 可操作等）。
--   3) host_id 由 INT UNSIGNED 改为 INT：交付在途的行用「-订单号」占位（负数必然不是真实上游主机 ID，
--      且订单号唯一 → 并发交付之间互不相同），交付成功后在同一行改写为真实主机 ID（恒为正）。
--      uk_instances_host 唯一键**保持不变**：占位值互不相同，真实主机 ID 仍全局唯一
--      （改 unsigned 而非删唯一键，是为了不削弱「一台上游主机只对应一条实例」的完整性约束）。

ALTER TABLE `instances`
    ADD COLUMN `downstream_token` VARCHAR(32) NOT NULL DEFAULT ''
        COMMENT '上游回推鉴权串（32 位十六进制；开通时生成一次后不再变更；存量老实例为空串，不参与回推）'
        AFTER `name`,
    MODIFY COLUMN `status` ENUM('provisioning','active','suspended','cancelled','terminated') NOT NULL DEFAULT 'active'
        COMMENT '实例状态：provisioning 交付在途（内部态，不出现在任何列表里）/ active 正常 / suspended 暂停 / cancelled 已申请终止 / terminated 已终止',
    MODIFY COLUMN `host_id` INT NOT NULL
        COMMENT '上游主机 ID（hostinfo 的 hosts[].id，唯一）；交付在途为负数占位（-订单号），交付成功后改写为真实主机 ID';
