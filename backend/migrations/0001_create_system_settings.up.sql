-- 0001 建立系统设置表（键值对），阶段 0 基建演示。
CREATE TABLE `system_settings` (
    `key`        VARCHAR(128) NOT NULL COMMENT '配置键名',
    `value`      TEXT         NULL     COMMENT '配置值（复杂结构由业务层序列化）',
    `updated_at` DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '最后更新时间',
    PRIMARY KEY (`key`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '系统设置';
