// 状态语义色的**唯一**出口：低饱和的绿 / 琥珀 / 红 / 灰。
//
// 约定（R4 视觉规范）：这三种颜色只允许出现在状态 Badge、状态圆点、状态文字上，
// 不做装饰性使用；其余界面一律收敛在蓝 / 白 / 灰三色体系内。
// 这里的 class 供 StatusBadge（Badge 形态）与状态文字（Timeline 圆点、到期时间）共用，
// 新增状态色调映射时只改本文件，避免各处硬编码 emerald / amber。
import type { StatusTone } from './orderStatus'

/** Badge 形态：浅色底 + 同色文字。 */
export const STATUS_TONE_BADGE_CLASSES: Record<StatusTone, string> = {
  ok: 'bg-success/10 text-success dark:bg-green-500/15 dark:text-green-400',
  warn: 'bg-warning/10 text-warning dark:bg-amber-500/15 dark:text-amber-400',
  error: 'bg-destructive/10 text-destructive dark:bg-destructive/20',
  pending: 'bg-muted text-muted-foreground',
}

/** 文字/圆点形态：只上色，不加底。 */
export const STATUS_TONE_TEXT_CLASSES: Record<StatusTone, string> = {
  ok: 'text-success dark:text-green-400',
  warn: 'text-warning dark:text-amber-400',
  error: 'text-destructive',
  pending: 'text-muted-foreground',
}

/** 圆点（Timeline 等）用的纯色底。 */
export const STATUS_TONE_DOT_CLASSES: Record<StatusTone, string> = {
  ok: 'bg-success',
  warn: 'bg-warning',
  error: 'bg-destructive',
  pending: 'bg-muted-foreground/40',
}
