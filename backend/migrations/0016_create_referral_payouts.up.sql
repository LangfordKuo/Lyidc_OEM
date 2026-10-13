-- 0016 邀请返佣（二）：提现与转余额申请表（R10-B：契约 18.1.1）。
-- 时间语义：所有 DATETIME 列统一存 UTC（后端强制 DSN parseTime=true&loc=UTC）。
-- 金额语义：DECIMAL(14,2) 定点数，Go 侧与接口均用定点小数字符串，计算走整数分。
--
-- 设计要点（口径见 docs/api-contract.md 第 18 节）：
--   1) 提现与转余额共用**同一张申请表**，用 kind 区分 withdraw / convert。
--      取舍：两者字段（申请人/金额/状态机/审核人/审核备注/时间戳）与审核动作几乎一致，
--      拆两张表会复制一套状态机、审核路径与幂等锚点；kind 一列足以分流。
--      表名取中性的 payouts（出账）而非 withdrawals，因为 convert 是系统内两账户互转。
--   2) 状态机：withdraw pending→approved→paid、pending→rejected；
--      convert pending→approved|rejected；rejected/paid 为终态不可再流转。
--      重复审核/重复打款幂等（200+already=true，不覆盖首次 reviewed_at/paid_at）。
--   3) 账务口径（两类不同，契约 18.1.1）：
--      · withdraw「提交即冻结」：提交时 rebate_balance 立即扣减 + 写返现侧 ledger（负），
--        approve 只改状态，reject 退回冻结额（加回 + 正向 ledger），mark-paid 只记 paid_at；
--      · convert「审核通过时才换算」：提交只登记不动钱，approve 同一事务里
--        rebate_balance 减 + balance 加 + 两端各一条 ledger，reject 不动余额。
--   4) 可用余额 = rebate_balance − Σ(kind=convert 且 status=pending 的 amount)；
--      在途 withdraw 不必再减（已冻结）。校验在行锁下进行，并发提交不会超额。
--   5) 敏感字段 account_name / account_no：库内存原文（线下打款要核对收款人），
--      接口回显、日志与审计一律脱敏（对齐 SMTP 口令「只进库、对外一律掩码」口径）。
--   6) 本迁移**不新增 ledger.type 枚举**：凡动 rebate_balance 的账目一律 type=referral_rebate
--      （有符号），靠 ref_type 区分（referral_rebate / referral_withdrawal / referral_conversion）；
--      转换的消费侧用既有 adjust。避免 0016 再扩 ENUM 与 IsValidLedgerType 双处同步的风险面。
--
-- 幂等性：CREATE TABLE 由 golang-migrate 版本号保证只跑一次。

CREATE TABLE `referral_payouts` (
    `id`           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '出账申请 ID',
    `kind`         VARCHAR(16)     NOT NULL                COMMENT '申请类型：withdraw 提现 / convert 转余额（返现余额 → 消费余额）',
    `member_id`    BIGINT UNSIGNED NOT NULL                COMMENT '申请人会员 ID',
    `amount`       DECIMAL(14,2)   NOT NULL                COMMENT '申请金额，恒 > 0',
    `channel`      VARCHAR(16)     NOT NULL DEFAULT ''     COMMENT '到账渠道（仅 kind=withdraw）：wechat 微信 / alipay 支付宝；convert 恒为空串',
    `account_name` VARCHAR(64)     NOT NULL DEFAULT ''     COMMENT '收款人姓名（敏感：接口/日志/审计一律脱敏，库内留原文）；convert 恒为空串',
    `account_no`   VARCHAR(64)     NOT NULL DEFAULT ''     COMMENT '收款账号（敏感，同上）；convert 恒为空串',
    `status`       VARCHAR(16)     NOT NULL DEFAULT 'pending'
                                                            COMMENT '状态：pending 待审核 / approved 已通过 / rejected 已驳回 / paid 已打款（convert 只到 approved）',
    `remark`       VARCHAR(255)    NOT NULL DEFAULT ''     COMMENT '申请备注（申请人填）',
    `review_note`  VARCHAR(255)    NOT NULL DEFAULT ''     COMMENT '审核备注（管理员填）',
    `reviewed_by`  BIGINT UNSIGNED NOT NULL DEFAULT 0      COMMENT '审核管理员 ID；0 = 尚未审核',
    `reviewed_at`  DATETIME        NULL                    COMMENT '审核时间（UTC）；重复审核不覆盖首次',
    `paid_at`      DATETIME        NULL                    COMMENT '线下打款时间（UTC，仅 kind=withdraw 走到 paid）；重复打款不覆盖首次',
    `created_at`   DATETIME        NOT NULL                COMMENT '创建时间（UTC）',
    `updated_at`   DATETIME        NOT NULL                COMMENT '最后更新时间（UTC）',
    PRIMARY KEY (`id`),
    KEY `idx_referral_payouts_member` (`member_id`, `kind`, `status`),
    KEY `idx_referral_payouts_status` (`status`, `kind`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci
  COMMENT = '邀请返现出账申请（kind 区分提现/转余额；状态机与账务口径见契约 18.1.1）';
