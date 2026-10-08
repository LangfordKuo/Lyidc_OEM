-- 0003 建立管理员表，并写入开发默认账号。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true&loc=UTC）。
-- 警告：生产环境部署后必须立即修改 admin 密码（阶段 1 没有管理员管理接口，改密需 SQL 或后续阶段接口）。
CREATE TABLE `admins` (
    `id`            BIGINT UNSIGNED                       NOT NULL AUTO_INCREMENT COMMENT '管理员 ID',
    `username`      VARCHAR(32)                           NOT NULL                COMMENT '登录用户名（唯一，utf8mb4_general_ci 大小写不敏感）',
    `password_hash` VARCHAR(100)                          NOT NULL                COMMENT 'bcrypt 密码哈希，禁止对外返回',
    `nickname`      VARCHAR(32)                           NOT NULL DEFAULT ''     COMMENT '昵称',
    `role`          ENUM('admin','finance','support')     NOT NULL DEFAULT 'admin' COMMENT '角色：admin 超级管理员 / finance 财务 / support 客服',
    `status`        ENUM('active','disabled')             NOT NULL DEFAULT 'active' COMMENT '账号状态：active 正常 / disabled 已禁用',
    `created_at`    DATETIME                              NOT NULL                COMMENT '创建时间（UTC）',
    `updated_at`    DATETIME                              NOT NULL                COMMENT '最后更新时间（UTC）',
    `last_login_at` DATETIME                              NULL                    COMMENT '最后登录时间（UTC，可空）',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_admins_username` (`username`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '后台管理员账号';

-- 开发默认管理员：admin / admin123456（bcrypt cost=10）。生产必须立即修改密码。
INSERT INTO `admins` (`username`, `password_hash`, `nickname`, `role`, `status`, `created_at`, `updated_at`)
VALUES ('admin', '$2a$10$qAK3HvL3qCHfyUPNbaXHzeao1T.IspaU9fy/9H15xgtM5av.ZZIdW', '超级管理员', 'admin', 'active',
        UTC_TIMESTAMP(), UTC_TIMESTAMP());
