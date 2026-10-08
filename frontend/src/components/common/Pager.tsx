import {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
} from '@/components/ui/pagination'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { pageWindow, totalPages } from '@/lib/pagination'

/**
 * 服务端分页器：按「总条数 + 每页条数」推导页码窗口。
 * 单页或空数据时不渲染（避免无意义的「1 / 1」）。
 */
export default function Pager({
  page,
  total,
  pageSize,
  onChange,
  className,
}: {
  page: number
  total: number
  pageSize: number
  onChange: (page: number) => void
  className?: string
}) {
  const last = totalPages(total, pageSize)
  if (last <= 1) {
    return null
  }

  return (
    <Pagination className={cn('mt-4', className)}>
      <PaginationContent>
        <PaginationItem>
          <Button
            variant="ghost"
            size="sm"
            aria-label="上一页"
            disabled={page <= 1}
            onClick={() => onChange(page - 1)}
          >
            上一页
          </Button>
        </PaginationItem>
        {pageWindow(page, last).map((item, index) =>
          item === 'gap' ? (
            <PaginationItem key={`gap-${index}`}>
              <PaginationEllipsis />
            </PaginationItem>
          ) : (
            <PaginationItem key={item}>
              <Button
                variant={item === page ? 'outline' : 'ghost'}
                size="icon-sm"
                aria-label={`第 ${item} 页`}
                aria-current={item === page ? 'page' : undefined}
                onClick={() => onChange(item)}
              >
                {item}
              </Button>
            </PaginationItem>
          ),
        )}
        <PaginationItem>
          <Button
            variant="ghost"
            size="sm"
            aria-label="下一页"
            disabled={page >= last}
            onClick={() => onChange(page + 1)}
          >
            下一页
          </Button>
        </PaginationItem>
      </PaginationContent>
    </Pagination>
  )
}
