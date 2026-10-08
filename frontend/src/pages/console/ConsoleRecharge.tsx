import ConsolePlaceholder from '../../components/layout/ConsolePlaceholder'

// 会员区「余额充值」占位页（阶段 7b 填充）。
export default function ConsoleRecharge() {
  return (
    <ConsolePlaceholder
      title="余额充值"
      description="在线充值、充值单列表与余额流水将在阶段 7b 接入（接口：POST /recharges、GET /recharges、GET /finance/ledger）。"
      planned={[
        '在线充值：输入金额（1.00 ~ 50000.00）跳转易支付收银台',
        '充值单列表：状态与到账时间',
        '余额流水：充值入账 / 订单支付扣款明细',
      ]}
    />
  )
}
