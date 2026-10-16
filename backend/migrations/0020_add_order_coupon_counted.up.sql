-- 0020 优惠码「下单即占用」的订单记账列（P4 安全整改 finding#09）。
--
-- 背景：`max_uses`（使用次数上限）此前可被完全绕过——下单只**读** `used_count` 做校验
-- （既不预占、也不在支付时阻断），`used_count` 只在支付事务里条件自增一次，且超用时只记 WARN
-- 不阻断入账。渗透实测（2026-10-16，finding#09）：`max_uses=1` 的 100% 券生成 3 个
-- `final_amount=0.00` 订单并全部余额支付成功、拿到 3 个 active 实例，`used_count` 最终只 =1。
-- 根因是**校验与消耗不在同一时刻**，中间窗口可以无限下不消耗的单。
--
-- 本迁移只加一列，语义（完整口径见 docs/api-contract.md 12.5）：
--   coupon_counted = 1  下单时已在该券上**原子占用**一次（与订单落库同一事务、条件自增）；
--                       取消 `pending` 订单时在同一事务内释放（`used_count` 减 1，不低于 0）并置回 0。
--   coupon_counted = 0  未占用：未用码，或**本迁移之前下单的历史行**（刻意不回填、不回溯
--                       已成交订单）。历史行由支付链路按**老口径**补记一次，券已用尽则拒绝入账，
--                       因此默认 0 正是该兼容分支的判据——回填会把这个分支抹掉。
--
-- 该列**不进任何对外视图**（纯内部记账），与 coupon_id / coupon_code 一起构成「本单用了哪张券、
-- 是否已占用」的完整事实。不改 0001–0019。

ALTER TABLE `orders`
    ADD COLUMN `coupon_counted` TINYINT(1) NOT NULL DEFAULT 0
        COMMENT '本单是否已占用优惠码次数（1=下单时已占用，取消 pending 时释放并置 0；0=未占用或迁移前历史行）'
        AFTER `coupon_code`;

-- 顺带把 `coupons.used_count` 的列注释改成 P4 口径：0005 的原文「本阶段只读不增」在 P4 之后
-- 既不准也不完整（该列现在是「已占用次数」，随订单生命周期增减）。
-- 只改注释，类型与默认值逐字保持 0005 的原文。
ALTER TABLE `coupons`
    MODIFY COLUMN `used_count` INT UNSIGNED NOT NULL DEFAULT 0
        COMMENT '已占用次数（含待支付订单的在途占用；下单时条件自增、取消 pending 订单时释放）';
