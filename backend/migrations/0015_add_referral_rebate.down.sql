-- 0015 回滚：先摘掉 ledger 枚举扩展与返佣流水表，再摘掉 coupons / products / members 的返佣列。
-- 注意：回滚会丢失全部邀请关系、邀请码、返现余额与返佣流水；members.balance（消费余额）不受影响。

-- ledger 枚举收窄：MODIFY 会丢弃取值为 referral_rebate 的行吗？不会——MySQL 收窄 ENUM 时
-- 非法取值会被静默置为 ''（严格模式下报错），因此先删掉这批流水再收窄，保证回滚干净。
DELETE FROM `ledger` WHERE `type` = 'referral_rebate';

ALTER TABLE `ledger`
    MODIFY COLUMN `type` ENUM('recharge','order_pay','refund','adjust') NOT NULL
        COMMENT '流水类型：recharge 充值入账 / order_pay 余额支付扣款 / refund、adjust 预留';

DROP TABLE IF EXISTS `referral_rebates`;

ALTER TABLE `products`
    DROP COLUMN `rebate_disabled`,
    DROP COLUMN `rebate_percent`;

ALTER TABLE `coupons`
    DROP COLUMN `owner_member_id`;

ALTER TABLE `members`
    DROP KEY `idx_members_inviter`,
    DROP KEY `uk_members_invite_code`,
    DROP COLUMN `rebate_percent_override`,
    DROP COLUMN `rebate_balance`,
    DROP COLUMN `referral_enabled`,
    DROP COLUMN `invite_code`,
    DROP COLUMN `inviter_id`;
