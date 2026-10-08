import { Dropdown } from '@heroui/react'
import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'

import { fetchBalance } from '../../api/finance'
import { useAuth } from '../../auth/authContext'
import { paths } from '../../app/paths'
import { formatMoney } from '../../lib/format'

// UserMenu 是顶栏右侧的会员菜单：昵称 + 余额（实时读取）+ 控制台/订单/充值入口 + 退出登录。
// HeroUI v3 的 Dropdown.Trigger 本身就是按钮，无需再包一层 Button。
export default function UserMenu() {
  const { member, setBalance, logout } = useAuth()
  const navigate = useNavigate()
  const memberId = member?.id

  // 余额实时化：登录后（含切换账号）拉一次服务端余额，覆盖本地缓存的旧值。
  useEffect(() => {
    if (!memberId) {
      return
    }
    let cancelled = false
    fetchBalance()
      .then((result) => {
        if (!cancelled) {
          setBalance(result.balance)
        }
      })
      .catch(() => {
        // 读取失败保留缓存值（菜单不展示错误，避免顶栏噪音）
      })
    return () => {
      cancelled = true
    }
  }, [memberId, setBalance])

  if (!member) {
    return null
  }

  const handleAction = (key: React.Key) => {
    switch (key) {
      case 'console':
        navigate(paths.console)
        break
      case 'orders':
        navigate(paths.consoleOrders)
        break
      case 'recharge':
        navigate(paths.consoleRecharge)
        break
      case 'logout':
        logout()
        navigate(paths.home, { replace: true })
        break
      default:
        break
    }
  }

  return (
    <Dropdown>
      <Dropdown.Trigger
        aria-label="会员菜单"
        className="rounded-lg px-2 py-1 hover:bg-surface-secondary"
      >
        <span className="flex items-center gap-2">
          <span className="grid size-7 place-items-center rounded-full bg-accent text-xs font-medium text-accent-foreground">
            {(member.nickname || member.username).slice(0, 1).toUpperCase()}
          </span>
          <span className="hidden max-w-24 truncate text-sm sm:inline">
            {member.nickname || member.username}
          </span>
        </span>
      </Dropdown.Trigger>
      <Dropdown.Popover placement="bottom end">
        <Dropdown.Menu onAction={handleAction}>
          <Dropdown.Item id="profile" isDisabled textValue="当前账号">
            <div className="flex flex-col gap-0.5 py-1">
              <span className="text-sm font-medium">{member.nickname || member.username}</span>
              <span className="text-xs text-muted">余额 {formatMoney(member.balance)}</span>
            </div>
          </Dropdown.Item>
          <Dropdown.Item id="console" textValue="控制台">
            控制台
          </Dropdown.Item>
          <Dropdown.Item id="orders" textValue="我的订单">
            我的订单
          </Dropdown.Item>
          <Dropdown.Item id="recharge" textValue="余额充值">
            余额充值
          </Dropdown.Item>
          <Dropdown.Item id="logout" textValue="退出登录">
            退出登录
          </Dropdown.Item>
        </Dropdown.Menu>
      </Dropdown.Popover>
    </Dropdown>
  )
}
