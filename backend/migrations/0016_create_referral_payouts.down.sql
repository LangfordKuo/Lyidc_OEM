-- 0016 回滚：删除出账申请表。
-- 说明：本表是纯新增，不引用其它表的外键（member_id / reviewed_by 都是裸 ID），
-- 直接 DROP 即可，不会牵连 members / admins。
-- 注意：回滚会丢失全部提现与转余额申请记录；已冻结/已换算的资金动过 members.rebate_balance
-- 与 members.balance，且对应 ledger 行仍在 ledger 表里（0016 不动 ledger），
-- 因此回滚后账目会出现「有流水但无申请单」的孤儿引用（ref_type=referral_withdrawal /
-- referral_conversion 指向已删除的 referral_payouts.id）。生产回滚前请先确认已无在途申请。

DROP TABLE IF EXISTS `referral_payouts`;
