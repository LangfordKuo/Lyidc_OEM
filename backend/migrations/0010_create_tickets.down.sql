-- 0010 回滚：删除工单消息与工单表（先子表后主表；本迁移未改动任何既有表）。

DROP TABLE IF EXISTS `ticket_messages`;

DROP TABLE IF EXISTS `tickets`;
