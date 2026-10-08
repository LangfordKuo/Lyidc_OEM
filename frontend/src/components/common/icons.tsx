import type { SVGProps } from 'react'

// 轻量内联图标：HeroUI v3 只带少量图标，这里补齐页面用到的线性图标。
// 统一 24×24 viewBox + currentColor 描边，颜色由外层文本色决定。
type IconProps = SVGProps<SVGSVGElement>

function Icon({ children, ...props }: IconProps) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      width={20}
      height={20}
      {...props}
    >
      {children}
    </svg>
  )
}

export function IconMenu(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M4 6h16M4 12h16M4 18h16" />
    </Icon>
  )
}

export function IconClose(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M6 6l12 12M18 6L6 18" />
    </Icon>
  )
}

export function IconDashboard(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="3" y="3" width="7" height="9" rx="1.5" />
      <rect x="14" y="3" width="7" height="5" rx="1.5" />
      <rect x="14" y="12" width="7" height="9" rx="1.5" />
      <rect x="3" y="16" width="7" height="5" rx="1.5" />
    </Icon>
  )
}

export function IconServer(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="3" y="4" width="18" height="7" rx="2" />
      <rect x="3" y="13" width="18" height="7" rx="2" />
      <path d="M7 7.5h.01M7 16.5h.01" />
    </Icon>
  )
}

export function IconOrders(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M6 3h9l4 4v14a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z" />
      <path d="M14 3v5h5M9 13h6M9 17h4" />
    </Icon>
  )
}

export function IconWallet(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M3 7a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v1" />
      <rect x="3" y="7" width="18" height="12" rx="2" />
      <path d="M16 13h2" />
    </Icon>
  )
}

export function IconTicket(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M4 9V6a1 1 0 0 1 1-1h14a1 1 0 0 1 1 1v3a2 2 0 0 0 0 6v3a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1v-3a2 2 0 0 0 0-6z" />
      <path d="M12 8v8" strokeDasharray="2 3" />
    </Icon>
  )
}

export function IconBell(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M6 10a6 6 0 1 1 12 0c0 4 1.5 5 1.5 5h-15S6 14 6 10z" />
      <path d="M10.5 18a1.5 1.5 0 0 0 3 0" />
    </Icon>
  )
}

export function IconCheck(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M5 13l4 4L19 7" />
    </Icon>
  )
}

export function IconClock(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" />
    </Icon>
  )
}

export function IconAlert(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M12 4l9 16H3l9-16z" />
      <path d="M12 10v4M12 17.5h.01" />
    </Icon>
  )
}

export function IconBolt(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M13 3L5 14h6l-1 7 8-11h-6l1-7z" />
    </Icon>
  )
}

export function IconShield(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M12 3l7 3v6c0 4.5-3 7.8-7 9-4-1.2-7-4.5-7-9V6l7-3z" />
      <path d="M9 12l2 2 4-4" />
    </Icon>
  )
}

export function IconLifeRing(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="9" />
      <circle cx="12" cy="12" r="3.5" />
      <path d="M5.6 5.6l3.8 3.8M14.6 14.6l3.8 3.8M18.4 5.6l-3.8 3.8M9.4 14.6l-3.8 3.8" />
    </Icon>
  )
}

export function IconArrowRight(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M5 12h14M13 6l6 6-6 6" />
    </Icon>
  )
}

// 管理后台补充图标：商品（箱子）、会员（用户）、设置（齿轮）、外部跳转。
export function IconProduct(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M21 8.5L12 3.5 3 8.5v7L12 20.5l9-5v-7z" />
      <path d="M3 8.5l9 5 9-5M12 13.5v7" />
    </Icon>
  )
}

export function IconUsers(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="9" cy="8" r="3.2" />
      <path d="M3.5 19.5c0-3 2.5-5 5.5-5s5.5 2 5.5 5" />
      <path d="M16 5.5a3 3 0 010 5.6M17 14.8c2.1.5 3.5 2.2 3.5 4.7" />
    </Icon>
  )
}

export function IconSettings(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.7 1.7 0 00.3 1.9l.1.1a2 2 0 11-2.8 2.8l-.1-.1a1.7 1.7 0 00-2.9 1.2 2 2 0 11-4 0 1.7 1.7 0 00-2.9-1.2l-.1.1a2 2 0 11-2.8-2.8l.1-.1A1.7 1.7 0 003 15a2 2 0 010-4 1.7 1.7 0 001.2-2.9l-.1-.1a2 2 0 112.8-2.8l.1.1A1.7 1.7 0 0010 4.2a2 2 0 014 0 1.7 1.7 0 002.9 1.2l.1-.1a2 2 0 112.8 2.8l-.1.1A1.7 1.7 0 0021 11a2 2 0 010 4z" />
    </Icon>
  )
}

export function IconLogout(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M15 4h3a2 2 0 012 2v12a2 2 0 01-2 2h-3" />
      <path d="M10 8l-4 4 4 4M6 12h9" />
    </Icon>
  )
}
