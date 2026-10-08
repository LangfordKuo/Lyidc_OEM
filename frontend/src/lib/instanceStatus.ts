import type { CancelStatus, InstanceStatus, PowerOp } from '../api/types'
import type { StatusTone } from '../components/StatusBadge'

// 实例状态文案与色调（契约 15.1 的四态 + 15.8.1 的取消标记）。

export const INSTANCE_STATUS_LABELS: Record<InstanceStatus, string> = {
  active: '运行中',
  suspended: '已暂停',
  cancelled: '已取消',
  terminated: '已终止',
}

export const INSTANCE_STATUS_TONES: Record<InstanceStatus, StatusTone> = {
  active: 'ok',
  suspended: 'warn',
  cancelled: 'pending',
  terminated: 'error',
}

export const CANCEL_STATUS_LABELS: Record<CancelStatus, string> = {
  none: '',
  pending: '终止申请在途',
  done: '已终止',
}

export function instanceStatusLabel(status: string): string {
  return INSTANCE_STATUS_LABELS[status as InstanceStatus] ?? status
}

export function instanceStatusTone(status: string): StatusTone {
  return INSTANCE_STATUS_TONES[status as InstanceStatus] ?? 'pending'
}

export function cancelStatusLabel(status: string): string {
  return CANCEL_STATUS_LABELS[status as CancelStatus] ?? ''
}

// ---------------------------------------------------------------------------
// 电源操作（契约 15.1）：hard_* 为强制操作，前端一律二次确认并标注风险。
// ---------------------------------------------------------------------------
export interface PowerAction {
  op: PowerOp
  label: string
  description: string
  danger: boolean
  confirmTitle: string
  confirmText: string
}

export const POWER_ACTIONS: PowerAction[] = [
  {
    op: 'soft_on',
    label: '开机',
    description: '正常启动主机',
    danger: false,
    confirmTitle: '确认开机？',
    confirmText: '将向主机发送开机指令，执行由上游异步完成。',
  },
  {
    op: 'soft_off',
    label: '关机',
    description: '正常关闭主机',
    danger: false,
    confirmTitle: '确认关机？',
    confirmText: '将向主机发送关机指令，请先确认业务已妥善停止。',
  },
  {
    op: 'reboot',
    label: '重启',
    description: '正常重启主机',
    danger: false,
    confirmTitle: '确认重启？',
    confirmText: '将向主机发送重启指令，重启期间服务会短暂中断。',
  },
  {
    op: 'hard_off',
    label: '强制关机',
    description: '强制断电（有数据损坏风险）',
    danger: true,
    confirmTitle: '确认强制关机？',
    confirmText:
      '强制关机等价于直接断电，可能造成数据损坏或文件系统异常，请仅在正常关机无效时使用。',
  },
  {
    op: 'hard_reboot',
    label: '强制重启',
    description: '强制复位（有数据损坏风险）',
    danger: true,
    confirmTitle: '确认强制重启？',
    confirmText:
      '强制重启等价于直接复位，可能造成数据损坏或文件系统异常，请仅在正常重启无效时使用。',
  },
]

export function powerAction(op: string): PowerAction | null {
  return POWER_ACTIONS.find((item) => item.op === op) ?? null
}

// ---------------------------------------------------------------------------
// 操作记录（契约 15.4）：action 文案与留痕来源。
// ---------------------------------------------------------------------------
export const INSTANCE_ACTION_LABELS: Record<string, string> = {
  create: '开通实例',
  power_on: '开机',
  power_off: '关机',
  reboot: '重启',
  hard_off: '强制关机',
  hard_reboot: '强制重启',
  reinstall: '重装系统',
  reset_password: '重置密码',
  suspend: '暂停',
  unsuspend: '恢复',
  sync: '状态同步',
  renew: '续费',
  cancel: '提交终止申请',
  cancel_sync: '终止收敛',
}

export const ACTOR_TYPE_LABELS: Record<string, string> = {
  member: '会员',
  admin: '管理员',
  system: '系统',
}

export function instanceActionLabel(action: string): string {
  return INSTANCE_ACTION_LABELS[action] ?? action
}
