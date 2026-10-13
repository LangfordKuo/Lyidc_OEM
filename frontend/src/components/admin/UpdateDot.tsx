import { STATUS_TONE_DOT_CLASSES } from '@/lib/statusTone'
import { cn } from '@/lib/utils'

/**
 * 系统更新红点（契约 19.7）：侧栏「设置」项与设置页「系统更新」卡片标题共用。
 * 与侧栏「通知」未读徽章同为红色提示、同样右侧对齐（由调用方通过 className 决定），颜色取自 statusTone 出口。
 * 只表示「有新版本」，不带数字；显示条件（has_update === true）由调用方判断，这里只负责渲染。
 */
export default function UpdateDot({ className }: { className?: string }) {
  return (
    <span
      data-testid="update-dot"
      className={cn('inline-block size-2 shrink-0 rounded-full', STATUS_TONE_DOT_CLASSES.error, className)}
    >
      <span className="sr-only">有新版本</span>
    </span>
  )
}
