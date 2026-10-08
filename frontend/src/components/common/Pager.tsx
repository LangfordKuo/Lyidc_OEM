import { Pagination } from '@heroui/react'

import { DEFAULT_PAGE_SIZE, pageWindow, totalPages } from '../../lib/pagination'

// Pager 是列表页统一的分页条：左侧汇总文案 + 右侧页码（上一页/下一页 + 页码窗口）。
export default function Pager({
  page,
  total,
  pageSize = DEFAULT_PAGE_SIZE,
  onChange,
}: {
  page: number
  total: number
  pageSize?: number
  onChange: (page: number) => void
}) {
  const last = totalPages(total, pageSize)

  return (
    <Pagination size="sm" className="flex-wrap justify-between gap-2">
      <Pagination.Summary className="text-xs text-muted">
        共 {total} 条 · 第 {page}/{last} 页
      </Pagination.Summary>
      {last > 1 ? (
        <Pagination.Content>
          <Pagination.Item>
            <Pagination.Previous
              isDisabled={page <= 1}
              onPress={() => onChange(Math.max(1, page - 1))}
            >
              <Pagination.PreviousIcon />
              上一页
            </Pagination.Previous>
          </Pagination.Item>
          {pageWindow(page, last).map((item, index) =>
            item === 'gap' ? (
              <Pagination.Item key={`gap-${index}`}>
                <Pagination.Ellipsis />
              </Pagination.Item>
            ) : (
              <Pagination.Item key={item}>
                <Pagination.Link isActive={item === page} onPress={() => onChange(item)}>
                  {item}
                </Pagination.Link>
              </Pagination.Item>
            ),
          )}
          <Pagination.Item>
            <Pagination.Next
              isDisabled={page >= last}
              onPress={() => onChange(Math.min(last, page + 1))}
            >
              下一页
              <Pagination.NextIcon />
            </Pagination.Next>
          </Pagination.Item>
        </Pagination.Content>
      ) : null}
    </Pagination>
  )
}
