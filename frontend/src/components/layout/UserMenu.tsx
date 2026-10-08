import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { LayoutDashboardIcon, LogOutIcon, WalletIcon } from 'lucide-react'

import { fetchBalance } from '@/api/finance'
import { paths } from '@/app/paths'
import { useAuth } from '@/auth/authContext'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { formatMoney } from '@/lib/format'

/** 顶栏右侧的会员菜单：昵称 + 余额（实时读取）+ 退出登录。 */
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
        // 读取失败保留缓存值（顶栏不展示错误，避免噪音）。
      })
    return () => {
      cancelled = true
    }
  }, [memberId, setBalance])

  if (!member) {
    return null
  }

  const displayName = member.nickname || member.username

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="gap-2 px-1.5" aria-label="会员菜单">
          <Avatar className="size-7">
            <AvatarFallback className="bg-primary text-xs font-medium text-primary-foreground">
              {displayName.slice(0, 1).toUpperCase()}
            </AvatarFallback>
          </Avatar>
          <span className="hidden max-w-24 truncate sm:inline">{displayName}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        <DropdownMenuLabel>
          <span className="block truncate text-sm font-medium">{displayName}</span>
          <span className="mt-0.5 block text-xs font-normal text-muted-foreground">
            余额 {formatMoney(member.balance)}
          </span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => navigate('/console')}>
          <LayoutDashboardIcon aria-hidden />
          会员区
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => navigate('/console')}>
          <WalletIcon aria-hidden />
          余额与订单
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={() => {
            logout()
            navigate(paths.home, { replace: true })
          }}
        >
          <LogOutIcon aria-hidden />
          退出登录
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
