import { fetchBalance } from '@/api/finance'
import type { Order, OrderPayResult } from '@/api/types'
import { useAuth } from '@/auth/authContext'
import PayDialog from '@/components/checkout/PayDialog'
import { useAsync } from '@/hooks/useAsync'

/**
 * 会员区「继续支付」弹窗：复用结算页的 PayDialog，并负责两件会员区特有的事——
 * 1. 打开时拉一次实时余额（余额支付可用性按最新值判断，不用页面加载时的旧快照）；
 * 2. 余额支付成功后把 balance_after 写回登录态（顶栏用户菜单同步显示新余额）。
 */
export default function PayOrderDialog({
  order,
  onOpenChange,
  onPaid,
}: {
  /** 待支付订单；为 null 时弹窗保持关闭。 */
  order: Order | null
  onOpenChange: (open: boolean) => void
  onPaid: (result: OrderPayResult) => void
}) {
  const { member, setBalance } = useAuth()
  const balanceState = useAsync(fetchBalance, [order?.id], order !== null)
  const balance = balanceState.data?.balance ?? member?.balance ?? '0.00'

  return (
    <PayDialog
      order={order}
      open={order !== null}
      onOpenChange={onOpenChange}
      balance={balance}
      onPaid={(result) => {
        if (result.pay.balance_after) {
          setBalance(result.pay.balance_after)
        }
        onPaid(result)
      }}
    />
  )
}
