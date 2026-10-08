import { Table } from '@heroui/react'
import type { ReactNode } from 'react'

import { EmptyBlock } from '../common/PageState'

// AdminTable 是管理后台列表页的统一表格：列定义 + 行数据 + 空态。
// 加载态/错误态由页面用 PageState 的 LoadingBlock / ErrorState 渲染（保持与会员区一致的排版）。
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
    <div className={`overflow-x-auto rounded-xl border border-border bg-surface ${className ?? ''}`}>
      <Table className="w-full text-sm">
        {/* HeroUI v3 的 Table 由外壳（div）+ Content（react-aria Table）组合而成：
            集合上下文在 Content 上，Header/Body/Row/Cell 必须放在 Content 内部。 */}
        <Table.Content aria-label={ariaLabel}>
          <Table.Header>
            {columns.map((column, index) => (
              <Table.Column
                key={column.key}
                id={column.key}
                isRowHeader={index === 0}
                className={`px-4 py-3 text-left text-xs font-medium whitespace-nowrap text-muted ${column.className ?? ''}`}
              >
                {column.header}
              </Table.Column>
            ))}
          </Table.Header>
          <Table.Body>
            {rows.map((row) => (
              <Table.Row key={rowKey(row)} id={String(rowKey(row))} className="border-t border-border">
                {columns.map((column) => (
                  <Table.Cell
                    key={column.key}
                    className={`px-4 py-3 align-middle text-foreground ${column.className ?? ''}`}
                  >
                    {column.cell(row)}
                  </Table.Cell>
                ))}
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Content>
      </Table>
    </div>
  )
}
