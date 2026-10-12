-- 0013 订单差价明细快照（R7：配置差价联动计价，契约 10.2.1 / 12.4）。
-- 时间语义：本迁移不动任何 DATETIME 列。
--
-- 背景与取舍：
--   1) 自 R7 起订单 amount = 基础价 + Σ 配置差价，为对账可查需要把差价明细随单快照。
--   2) 不复用 config_json：它存的是「配置项 id → 所选值 id / 数量」的**选择快照**，
--      是交付链路拼 configoption 的依据（见 14.3），塞进价格会污染交付语义；
--      故单列 price_breakdown_json 承载计价明细。
--   3) 形状（契约 12.4）：
--      {"base":"20.00","deltas":[{"option_id":10,"option_name":"data_disk_size|数据盘",
--        "kind":"quantity","value_id":37,"qty":50,"unit":"0.50","billable":50,
--        "amount":"25.00","note":""}],"total":"45.00"}
--      kind=select 时 value_id 有值；kind=quantity 时 qty/billable 有值；
--      note 为「无差价数据」时该项按 0.00 计（不阻断下单）。
--   4) 存量订单该列为 NULL，视图输出 null（不伪造空明细）。
--
-- 幂等性：ADD COLUMN 在 5.7 下重复执行会报错，迁移器按 golang-migrate 记录的版本号只跑一次。
ALTER TABLE `orders`
    ADD COLUMN `price_breakdown_json` TEXT NULL
        COMMENT 'R7 成交价差价明细快照：{base, deltas[], total}；amount = base + Σ deltas[].amount；存量订单为 NULL'
        AFTER `final_amount`;
