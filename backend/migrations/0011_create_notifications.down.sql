-- 0011 回滚：删除通知体系（两张新表 + instances 的去重列）。
ALTER TABLE `instances` DROP COLUMN `expiry_reminded_due`;
DROP TABLE IF EXISTS `email_logs`;
DROP TABLE IF EXISTS `notifications`;
