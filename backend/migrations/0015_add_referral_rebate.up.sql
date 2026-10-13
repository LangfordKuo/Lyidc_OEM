-- 0015 邀请返佣（R10-A：契约 18）。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true&loc=UTC）。
-- 金额语义：全部 DECIMAL(14,2) 定点数，Go 侧与接口均用定点小数字符串，计算走整数分。
--
-- 设计要点（口径见 docs/api-contract.md 第 18 节）：
--   1) 返佣公式：返佣 = max(订单原价 × 生效比例 − 本单优惠券减免, 0)，下取整到分；
--      「订单原价」= orders.amount（基础价 + Σ 配置差价，不含券减免），
--      「本单优惠券减免」= orders.discount_amount。
--   2) 生效比例三级取第一个非空命中：products.rebate_percent →
--      members.rebate_percent_override（邀请人本人的特批比例）→ settings referral.default_percent；
--      products.rebate_disabled=1 时该商品完全不返佣（压过一切）。
--   3) 发放目标是**邀请返现余额** members.rebate_balance（与消费余额 members.balance 完全独立），
--      可为负（已提现后客户退款 → 扣成负）；负值只禁止提现与转余额，不影响登录/下单/充值。
--   4) referral_rebates.order_id 唯一是发放幂等锚点：同一订单只发一次（重复回调不重复发放）。
--   5) ledger.type 扩枚举 referral_rebate 记返佣入账/冲回（金额正=发、负=冲回）；
--      该类型流水的 balance_before / balance_after 记的是 **rebate_balance** 的前后值
--      （不是消费余额），保持 BalanceAfter = BalanceBefore + Amount 恒等式。
--   6) coupons.owner_member_id=0 表示平台券；非 0 表示邀请人自建券（R10-B 提供建券接口）。
--      自建券只允许未绑定邀请关系的客户使用：未绑定 → 下单时绑定 owner，已绑定 → 拒绝用券。
--
-- 幂等性：CREATE TABLE / ADD COLUMN / MODIFY COLUMN 由 golang-migrate 版本号保证只跑一次。

-- ── members：邀请关系、邀请码、返现开关与返现余额 ──────────────────────────
ALTER TABLE `members`
    ADD COLUMN `inviter_id` BIGINT UNSIGNED NOT NULL DEFAULT 0
        COMMENT '邀请人会员 ID；0 = 无邀请人。首绑永久生效：注册时绑定后任何路径都不改绑'
        AFTER `balance`,
    ADD COLUMN `invite_code` VARCHAR(32) NULL
        COMMENT '我的邀请码（惰性生成：会员首次开通返现时才生成）；NULL = 尚未生成'
        AFTER `inviter_id`,
    ADD COLUMN `referral_enabled` TINYINT(1) NOT NULL DEFAULT 0
        COMMENT '邀请返现开关（会员自己在后台开/关）：1 开通 / 0 关闭'
        AFTER `invite_code`,
    ADD COLUMN `rebate_balance` DECIMAL(14,2) NOT NULL DEFAULT 0.00
        COMMENT '邀请返现余额（与消费余额 balance 完全独立；可为负）'
        AFTER `referral_enabled`,
    ADD COLUMN `rebate_percent_override` DECIMAL(5,2) NULL
        COMMENT '会员级返佣比例（0.00-100.00，管理员给单个邀请人特批）；NULL = 不覆盖，落到全局默认'
        AFTER `rebate_balance`,
    ADD UNIQUE KEY `uk_members_invite_code` (`invite_code`),
    ADD KEY `idx_members_inviter` (`inviter_id`);

-- ── coupons：邀请人自建券归属 ────────────────────────────────────────────
ALTER TABLE `coupons`
    ADD COLUMN `owner_member_id` BIGINT UNSIGNED NOT NULL DEFAULT 0
        COMMENT '自建券归属邀请人 ID；0 = 平台券，非 0 = 邀请人自建券（仅限未绑定邀请关系的客户使用）'
        AFTER `comment`;

-- ── products：商品级返佣比例 ─────────────────────────────────────────────
ALTER TABLE `products`
    ADD COLUMN `rebate_percent` DECIMAL(5,2) NULL
        COMMENT '商品级返佣比例（0.00-100.00，优先级最高）；NULL = 不覆盖，落到会员级/全局默认'
        AFTER `sort`,
    ADD COLUMN `rebate_disabled` TINYINT(1) NOT NULL DEFAULT 0
        COMMENT '禁用返佣：1 = 该商品完全不返佣（压过一切比例）/ 0 = 正常参与'
        AFTER `rebate_percent`;

-- ── referral_rebates：返佣流水 ───────────────────────────────────────────
CREATE TABLE `referral_rebates` (
    `id`              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '返佣流水 ID',
    `order_id`        BIGINT UNSIGNED NOT NULL                COMMENT '订单 ID（唯一：同一订单只发一次返佣）',
    `inviter_id`      BIGINT UNSIGNED NOT NULL                COMMENT '收到返佣的邀请人 ID',
    `buyer_id`        BIGINT UNSIGNED NOT NULL                COMMENT '下单买家 ID（其 members.inviter_id 指向 inviter_id）',
    `order_type`      VARCHAR(16)     NOT NULL                COMMENT '订单类型快照：new 新购 / renew 续费',
    `base_amount`     DECIMAL(14,2)   NOT NULL                COMMENT '订单原价快照（orders.amount：基础价 + Σ 配置差价，不含券减免）',
    `discount_amount` DECIMAL(14,2)   NOT NULL DEFAULT 0.00   COMMENT '本单优惠券减免快照（orders.discount_amount）',
    `ratio_percent`   DECIMAL(5,2)    NOT NULL                COMMENT '生效比例快照（0.00-100.00）',
    `ratio_source`    VARCHAR(16)     NOT NULL                COMMENT '比例来源：product 商品级 / member 会员级 / global 全局默认',
    `rebate_amount`   DECIMAL(14,2)   NOT NULL                COMMENT '返佣金额 = max(base_amount × ratio_percent − discount_amount, 0)，下取整到分，恒 ≥ 0',
    `status`          VARCHAR(16)     NOT NULL DEFAULT 'granted' COMMENT '状态：granted 已发放 / rolled_back 已回滚（订单取消时冲回）',
    `note`            VARCHAR(255)    NOT NULL DEFAULT ''     COMMENT '备注（如「订单 O2026… 返佣」）',
    `created_at`      DATETIME        NOT NULL                COMMENT '创建时间（UTC）',
    `updated_at`      DATETIME        NOT NULL                COMMENT '最后更新时间（UTC）',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_referral_rebates_order` (`order_id`),
    KEY `idx_referral_rebates_inviter` (`inviter_id`, `status`),
    KEY `idx_referral_rebates_buyer` (`buyer_id`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '邀请返佣流水（发放幂等锚点 = order_id 唯一；status 标记发放/回滚）';

-- ── ledger.type：扩枚举 referral_rebate ──────────────────────────────────
-- 必须与 model/ledger.go 的 IsValidLedgerType 同步（幂等集合：新增类型必须纳入校验集合）。
ALTER TABLE `ledger`
    MODIFY COLUMN `type` ENUM('recharge','order_pay','refund','adjust','referral_rebate') NOT NULL
        COMMENT '流水类型：recharge 充值入账 / order_pay 余额支付扣款 / referral_rebate 邀请返佣入账或冲回 / refund、adjust 预留';
