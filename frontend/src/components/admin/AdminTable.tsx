import type { ReactNode } from 'react'

import { EmptyBlock } from '@/components/common/PageState'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { cn } from '@/lib/utils'

/**
 * AdminTable 是管理后台列表页的统一表格：列定义 + 行数据 + 空态。
 * 加载态/错误态由页面用 PageState 的 LoadingBlock / ErrorState 渲染（与会员区排版一致）。
 * 窄屏横向滚动，保证列多的管理列表可用。
 */
export interface AdminColumn<T> {
  /** 列唯一键（同时作为 React key）。 */
  key: string
  header: ReactNode
  /** 单元格渲染（row 为原始数据）。 */
  cell: (row: T) => ReactNode
  /** 附加到表头与单元格的 class（宽度、对齐等）。 */
  className?: string
}

export default function AdminTable<T>({
  columns,
  rows,
  rowKey,
  ariaLabel,
  empty,
  className,
}: {
  columns: AdminColumn<T>[]
  rows: T[]
  rowKey: (row: T) => string | number
  ariaLabel: string
  /** 空态占位（缺省为统一的「暂无数据」空态块）。 */
  empty?: ReactNode
  className?: string
}) {
  if (rows.length === 0) {
    return <>{empty ?? <EmptyBlock title="暂无数据" />}</>
  }

  return (
    <div
      className={cn('overflow-x-auto rounded-xl border border-border bg-card', className)}
      data-testid="admin-table"
    >
      <Table aria-label={ariaLabel} className="w-full text-sm">
        <TableHeader>
          <TableRow className="border-b border-border hover:bg-transparent">
            {columns.map((column) => (
              <TableHead
                key={column.key}
                className={cn(
                  'h-10 px-4 text-left text-xs font-medium whitespace-nowrap text-muted-foreground',
                  column.className,
                )}
              >
                {column.header}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => (
            <TableRow key={rowKey(row)} className="border-b border-border last:border-b-0">
              {columns.map((column) => (
                <TableCell
                  key={column.key}
                  className={cn('px-4 py-3 align-middle text-foreground', column.className)}
                >
                  {column.cell(row)}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
