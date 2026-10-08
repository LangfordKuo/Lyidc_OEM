-- 0006 回滚：删除支付与财务表（顺序与外键无关，保持与建表相反）。
DROP TABLE IF EXISTS `settings`;
DROP TABLE IF EXISTS `ledger`;
DROP TABLE IF EXISTS `recharges`;
DROP TABLE IF EXISTS `orders`;
