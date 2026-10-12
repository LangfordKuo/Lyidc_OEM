import { Button } from '@/components/ui/button'
import { permissionHint } from '@/lib/adminRoles'

/** 分类行：名称 + 计数摘要 + 编辑入口（无权限时禁用并给出原因）。 */
export default function CategoryRow({
  title,
  meta,
  badge,
  canWrite,
  onEdit,
}: {
  title: string
  meta: string
  badge?: string
  /** categories.write（仅 admin）：无权限时编辑入口禁用。 */
  canWrite: boolean
  onEdit: () => void
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border px-3 py-2">
      <div className="min-w-0">
        {/* 名称与徽标分成两个元素：名称独立成节点，便于按文本精确定位。 */}
        <p className="flex items-center gap-2">
          <span className="truncate text-sm font-medium text-foreground">{title}</span>
          {badge ? (
            <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] leading-4 text-muted-foreground">
              {badge}
            </span>
          ) : null}
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground">{meta}</p>
      </div>
      <Button
        size="sm"
        variant="outline"
        disabled={!canWrite}
        title={canWrite ? undefined : permissionHint('categories.write')}
        onClick={onEdit}
      >
        编辑
      </Button>
    </div>
  )
}
