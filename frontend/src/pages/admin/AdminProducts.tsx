import { useEffect, useState, type FormEvent } from 'react'
import { AlertCircleIcon } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'

import { listAdminCategories } from '@/api/adminCategories'
import {
  listAdminProductGroups,
  listAdminProducts,
  updateAdminProduct,
} from '@/api/adminProducts'
import { errorMessage } from '@/api/client'
import type { AdminProduct } from '@/api/types'
import { useAdminAuth } from '@/auth/adminAuthContext'
import AdminTable, { type AdminColumn } from '@/components/admin/AdminTable'
import CategoriesPanel from '@/components/admin/CategoriesPanel'
import FilterSelect, { AdminFilterBar } from '@/components/admin/FilterSelect'
import ProductCategoryDialog from '@/components/admin/ProductCategoryDialog'
import ProductGroupMapsPanel from '@/components/admin/ProductGroupMapsPanel'
import ProductGroupsPanel from '@/components/admin/ProductGroupsPanel'
import ProductImportDialog from '@/components/admin/ProductImportDialog'
import ProductPricingDialog from '@/components/admin/ProductPricingDialog'
import ProductSyncDialog from '@/components/admin/ProductSyncDialog'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import Pager from '@/components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import StatusFilter from '@/components/common/StatusFilter'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useAsync } from '@/hooks/useAsync'
import { ADMIN_ROLE_LABELS, hasPermission, permissionHint } from '@/lib/adminRoles'
import { availableCycles, CYCLE_LABELS, type BillingCycle, type CyclePrices } from '@/lib/cycles'
import { formatMoney } from '@/lib/format'
import { ADMIN_PAGE_SIZE } from '@/lib/pagination'

type StatusFilterValue = '' | 'on' | 'off'

/** 页签：商品列表 / 分类树。分类页签由查询参数 `?tab=categories` 表示，便于分享与刷新保持。 */
type ProductsTab = 'products' | 'categories'

const STATUS_OPTIONS: { value: 'on' | 'off'; label: string }[] = [
  { value: 'on', label: '已上架' },
  { value: 'off', label: '已下架' },
]

/** 六周期全不可售的兜底值（后端 prices 恒为六键，这里只防脏数据把整页带崩）。 */
const EMPTY_PRICES: CyclePrices = {
  monthly: null,
  quarterly: null,
  semiannual: null,
  annual: null,
  biennial: null,
  triennial: null,
}

/** 列表展示价：六周期里优先月付，其次按标准顺序取首个可售周期；全不可售返回 null。 */
function primaryPrice(prices: CyclePrices): { cycle: BillingCycle; amount: string } | null {
  const cycles = availableCycles(prices)
  const cycle = cycles.includes('monthly') ? 'monthly' : cycles[0]
  if (!cycle) {
    return null
  }
  return { cycle, amount: prices[cycle] ?? '' }
}

/**
 * AdminProducts 是管理后台「商品与分类」页（契约 10.4 / 10.5，R9-B 合并原「分类」页）：
 * 页内两个页签——「商品」= 列表（关键词 / 分组 / 分类 / 状态筛选 + 分页）+ 上游导入
 * + 定价设置 + 改分类 + 上下架 + 分组管理；「分类」= 两级分类树 + 商品计数
 * + 改名 / 改排序 / 子分类改挂主分类。
 * 页签由查询参数驱动（默认商品，`?tab=categories` 打开分类）；旧地址 `/admin/categories`
 * 在路由层重定向到分类页签。
 * 角色矩阵：商品写 = admin / finance（products.write / products.import / groups.write）；
 * 分类写**仅 admin**（categories.write）；support 全程只读，写入口禁用并给出原因。
 */
export default function AdminProducts() {
  const { role } = useAdminAuth()
  const canWrite = hasPermission(role, 'products.write')
  const canImport = hasPermission(role, 'products.import')
  const canSync = hasPermission(role, 'products.sync')
  const canEditGroups = hasPermission(role, 'groups.write')
  const canWriteCategories = hasPermission(role, 'categories.write')

  const [searchParams, setSearchParams] = useSearchParams()
  const activeTab: ProductsTab =
    searchParams.get('tab') === 'categories' ? 'categories' : 'products'

  const [keywordInput, setKeywordInput] = useState('')
  const [keyword, setKeyword] = useState('')
  const [groupId, setGroupId] = useState('')
  const [categoryId, setCategoryId] = useState('')
  const [status, setStatus] = useState<StatusFilterValue>('')
  const [page, setPage] = useState(1)
  const [refreshKey, setRefreshKey] = useState(0)

  const [importOpen, setImportOpen] = useState(false)
  // R20 同步：勾选集合按本地商品 ID 记录（跨筛选保留；刷新列表后按当前页重新裁剪）。
  const [selectedIds, setSelectedIds] = useState<Set<number>>(() => new Set())
  const [syncOpen, setSyncOpen] = useState(false)

  const [pricingTarget, setPricingTarget] = useState<AdminProduct | null>(null)
  const [statusTarget, setStatusTarget] = useState<AdminProduct | null>(null)
  const [statusPending, setStatusPending] = useState(false)
  const [statusError, setStatusError] = useState('')
  const [categoryTarget, setCategoryTarget] = useState<AdminProduct | null>(null)

  // 商品列表与分组只在「商品」页签拉取；分类树两个页签共用（筛选项、改分类弹窗与分类页签）。
  const productsState = useAsync(
    () =>
      listAdminProducts({
        page,
        page_size: ADMIN_PAGE_SIZE,
        ...(groupId ? { group_id: Number(groupId) } : {}),
        ...(categoryId ? { category_id: Number(categoryId) } : {}),
        ...(status ? { status } : {}),
        ...(keyword ? { keyword } : {}),
      }),
    [page, groupId, categoryId, status, keyword, refreshKey],
    activeTab === 'products',
  )
  // 分组列表两个页签都要用（商品页签做筛选/分组面板，分类页签做「上游分组映射」区块），故常开。
  const groupsState = useAsync(() => listAdminProductGroups(), [refreshKey])
  // 改分类弹窗的选项来源（契约 10.4）：两级分类树，商品只挂子分类。
  const categoriesState = useAsync(() => listAdminCategories(), [refreshKey])

  const products = productsState.data?.items ?? []
  const groups = groupsState.data?.items ?? []
  const categories = categoriesState.data?.items ?? []

  /** 刷新列表 / 分组 / 分类（写操作成功后调用，保证各处数据一致）。 */
  const refresh = () => setRefreshKey((value) => value + 1)

  // R20 同步的勾选：只对**当前页可见列表**有效——翻页、改筛选或刷新后自动清空，
  // 保证「按钮上的已选数」与「提交的条数」永远一致（不做隐式的跨页勾选）。
  useEffect(() => {
    setSelectedIds(new Set())
  }, [page, groupId, categoryId, status, keyword, refreshKey])

  const selectedProducts = products.filter((product) => selectedIds.has(product.id))
  const allOnPageSelected = products.length > 0 && selectedProducts.length === products.length

  const toggleProductSelection = (id: number, checked: boolean) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (checked) {
        next.add(id)
      } else {
        next.delete(id)
      }
      return next
    })
  }

  const toggleAllOnPage = (checked: boolean) => {
    setSelectedIds(new Set(checked ? products.map((product) => product.id) : []))
  }

  /** 页签切换：分类页签写入 `?tab=categories`，商品页签恢复干净 URL（其余查询参数保留）。 */
  const changeTab = (value: string) => {
    const params = new URLSearchParams(searchParams)
    if (value === 'categories') {
      params.set('tab', 'categories')
    } else {
      params.delete('tab')
    }
    setSearchParams(params, { replace: true })
  }

  const handleSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setKeyword(keywordInput.trim())
    setPage(1)
  }

  const changeGroup = (next: string) => {
    setGroupId(next)
    setPage(1)
  }

  const changeCategory = (next: string) => {
    setCategoryId(next)
    setPage(1)
  }

  const changeStatus = (next: StatusFilterValue) => {
    setStatus(next)
    setPage(1)
  }

  /** 上下架：二次确认后提交 status（上架可能触发后端 409 校验）。 */
  const handleStatusConfirm = async () => {
    if (!statusTarget) {
      return
    }
    const next = statusTarget.status === 'on' ? 'off' : 'on'
    setStatusPending(true)
    setStatusError('')
    try {
      await updateAdminProduct(statusTarget.id, { status: next })
      toast.success(next === 'on' ? `「${statusTarget.name}」已上架` : `「${statusTarget.name}」已下架`)
      setStatusTarget(null)
      refresh()
    } catch (err) {
      setStatusError(errorMessage(err, '操作失败，请稍后重试'))
    } finally {
      setStatusPending(false)
    }
  }

  const columns: AdminColumn<AdminProduct>[] = [
    {
      // R20 同步的行多选：勾选若干商品后可点右上角「同步」。
      key: 'select',
      header: (
        <Checkbox
          aria-label="全选本页商品"
          checked={
            allOnPageSelected ? true : selectedProducts.length > 0 ? 'indeterminate' : false
          }
          onCheckedChange={(checked) => toggleAllOnPage(checked === true)}
        />
      ),
      cell: (row) => (
        <Checkbox
          aria-label={`选择商品 ${row.name}`}
          checked={selectedIds.has(row.id)}
          onCheckedChange={(checked) => toggleProductSelection(row.id, checked === true)}
        />
      ),
      className: 'w-10',
    },
    {
      key: 'name',
      header: '商品',
      cell: (row) => (
        <div className="min-w-0">
          <p className="truncate font-medium text-foreground">{row.name}</p>
          <p className="mt-0.5 font-mono text-xs text-muted-foreground">#{row.id}</p>
        </div>
      ),
    },
    {
      key: 'group',
      header: '分组',
      cell: (row) =>
        row.group_name ? row.group_name : <span className="text-muted-foreground">未分组</span>,
    },
    {
      key: 'category',
      header: '分类',
      cell: (row) =>
        row.category_id ? (
          <span className="text-foreground">
            {row.category_parent.name ? `${row.category_parent.name} / ` : ''}
            {row.category_name}
          </span>
        ) : (
          <span className="text-muted-foreground">未归类</span>
        ),
    },
    {
      key: 'status',
      header: '状态',
      cell: (row) =>
        row.status === 'on' ? (
          <StatusBadge tone="ok" label="已上架" />
        ) : (
          <StatusBadge tone="pending" label="已下架" />
        ),
    },
    {
      key: 'price',
      header: '本地售价',
      cell: (row) => {
        const prices = row.prices ?? EMPTY_PRICES
        const primary = primaryPrice(prices)
        const count = availableCycles(prices).length
        return (
          <div>
            <p className="text-foreground">
              {primary ? `${formatMoney(primary.amount)} / ${CYCLE_LABELS[primary.cycle]}` : '—'}
            </p>
            <p className="mt-0.5 text-xs text-muted-foreground">可售 {count} 个周期</p>
          </div>
        )
      },
    },
    {
      key: 'stock',
      header: '库存',
      cell: (row) =>
        row.stock_control === 0 ? (
          <span className="text-muted-foreground">不限</span>
        ) : (
          <span className="text-foreground">{row.stock_qty}</span>
        ),
    },
    {
      key: 'actions',
      header: '操作',
      className: 'whitespace-nowrap',
      cell: (row) => (
        <div className="flex flex-wrap items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            disabled={!canWrite}
            title={canWrite ? undefined : permissionHint('products.write')}
            onClick={() => setPricingTarget(row)}
          >
            定价设置
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={!canWrite}
            title={canWrite ? undefined : permissionHint('products.write')}
            onClick={() => setCategoryTarget(row)}
          >
            改分类
          </Button>
          <Button
            size="sm"
            variant={row.status === 'on' ? 'destructive' : 'default'}
            disabled={!canWrite}
            title={canWrite ? undefined : permissionHint('products.write')}
            onClick={() => {
              setStatusError('')
              setStatusTarget(row)
            }}
          >
            {row.status === 'on' ? '下架' : '上架'}
          </Button>
        </div>
      ),
    },
  ]

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">商品与分类</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            商品目录：从上游导入、配置定价、上架或下架、改挂分类；分类树：改名、改排序与子分类改挂。
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" onClick={refresh}>
            刷新
          </Button>
          {selectedProducts.length > 0 ? (
            <span className="text-xs text-muted-foreground">已选 {selectedProducts.length} 个商品</span>
          ) : null}
          <Button
            size="sm"
            variant="outline"
            disabled={!canSync || selectedProducts.length === 0}
            title={
              !canSync
                ? permissionHint('products.sync')
                : selectedProducts.length === 0
                  ? '请先勾选要同步的商品'
                  : undefined
            }
            onClick={() => setSyncOpen(true)}
          >
            同步
          </Button>
          <Button
            size="sm"
            disabled={!canImport}
            title={canImport ? undefined : permissionHint('products.import')}
            onClick={() => setImportOpen(true)}
          >
            从上游导入商品
          </Button>
        </div>
      </header>

      <Tabs value={activeTab} onValueChange={changeTab}>
        <TabsList aria-label="商品与分类页签">
          <TabsTrigger value="products">商品</TabsTrigger>
          <TabsTrigger value="categories">分类</TabsTrigger>
        </TabsList>

        <TabsContent value="products" className="space-y-5 pt-2">
          {!canWrite ? (
            <p className="text-xs text-muted-foreground">
              当前角色（{role ? ADMIN_ROLE_LABELS[role] : '未登录'}）为只读视角：导入、定价设置、
              改分类、上下架与分组保存均已禁用（{permissionHint('products.write')}）。
            </p>
          ) : null}

          <form onSubmit={handleSearch}>
            <AdminFilterBar
              actions={
                <span className="text-xs text-muted-foreground">
                  共 {productsState.data?.total ?? 0} 个商品
                  {groupId ? '（已按分组筛选）' : ''}
                  {categoryId ? '（已按分类筛选）' : ''}
                </span>
              }
            >
              <div className="space-y-1">
                <Label htmlFor="product_keyword" className="text-xs text-muted-foreground">
                  关键词
                </Label>
                <Input
                  id="product_keyword"
                  name="product_keyword"
                  className="w-52"
                  placeholder="搜索商品名称"
                  value={keywordInput}
                  onChange={(event) => setKeywordInput(event.target.value)}
                />
              </div>
              <Button type="submit" size="sm" variant="outline">
                搜索
              </Button>
              {keyword ? (
                <Button
                  size="sm"
                  variant="ghost"
                  type="button"
                  onClick={() => {
                    setKeywordInput('')
                    setKeyword('')
                    setPage(1)
                  }}
                >
                  清除
                </Button>
              ) : null}
              <FilterSelect
                label="分组"
                value={groupId}
                onChange={changeGroup}
                options={[
                  { value: '', label: '全部分组' },
                  ...groups.map((group) => ({ value: String(group.id), label: group.name })),
                ]}
              />
              <FilterSelect
                label="分类"
                value={categoryId}
                onChange={changeCategory}
                options={[
                  { value: '', label: '全部分类' },
                  { value: '0', label: '未归类' },
                  ...categories.flatMap((main) =>
                    main.subcategories.map((sub) => ({
                      value: String(sub.id),
                      label: `${main.name} / ${sub.name}`,
                    })),
                  ),
                ]}
              />
            </AdminFilterBar>
          </form>

          <StatusFilter
            label="按状态筛选"
            options={STATUS_OPTIONS}
            value={status}
            onChange={changeStatus}
          />

          {productsState.loading ? <LoadingBlock label="正在读取商品列表…" /> : null}
          {productsState.error ? (
            <ErrorState message={productsState.error} onRetry={productsState.reload} />
          ) : null}

          {!productsState.loading && !productsState.error ? (
            <AdminTable
              ariaLabel="商品列表"
              columns={columns}
              rows={products}
              rowKey={(product) => product.id}
              empty={
                <EmptyBlock
                  title="没有符合条件的商品"
                  description={
                    keyword || groupId || categoryId || status
                      ? '换个关键词或筛选条件看看。'
                      : '可以从上游导入商品目录，导入后需先配置定价再上架。'
                  }
                />
              }
            />
          ) : null}

          {productsState.data && productsState.data.total > 0 ? (
            <Pager
              page={page}
              total={productsState.data.total}
              pageSize={productsState.data.page_size || ADMIN_PAGE_SIZE}
              onChange={setPage}
            />
          ) : null}

          <ProductGroupsPanel
            groups={groups}
            loading={groupsState.loading}
            error={groupsState.error}
            onReload={groupsState.reload}
            canWrite={canEditGroups}
            selectedGroupId={groupId}
            onSelectGroup={changeGroup}
          />
        </TabsContent>

        <TabsContent value="categories" className="space-y-5 pt-2">
          {!canWriteCategories ? (
            <p className="text-xs text-muted-foreground">
              当前角色（{role ? ADMIN_ROLE_LABELS[role] : '未登录'}）为只读视角：改名、改排序与改挂均已禁用（
              {permissionHint('categories.write')}）。
            </p>
          ) : null}
          <CategoriesPanel
            categories={categories}
            loading={categoriesState.loading}
            error={categoriesState.error}
            onReload={categoriesState.reload}
            canWrite={canWriteCategories}
            onChanged={refresh}
          />
          <ProductGroupMapsPanel
            groups={groups}
            categories={categories}
            canWrite={canWriteCategories}
          />
        </TabsContent>
      </Tabs>

      {pricingTarget ? (
        <ProductPricingDialog
          product={pricingTarget}
          onClose={() => setPricingTarget(null)}
          onUpdated={() => {
            setPricingTarget(null)
            refresh()
            toast.success('定价已保存')
          }}
        />
      ) : null}

      {categoryTarget ? (
        <ProductCategoryDialog
          product={categoryTarget}
          categories={categories}
          onClose={() => setCategoryTarget(null)}
          onUpdated={() => {
            setCategoryTarget(null)
            refresh()
            toast.success('已改挂，后续导入不再覆盖该商品分类')
          }}
        />
      ) : null}

      <ConfirmDialog
        open={Boolean(statusTarget)}
        onOpenChange={(open) => {
          if (!open) {
            setStatusTarget(null)
            setStatusError('')
          }
        }}
        title={statusTarget?.status === 'on' ? '确认下架商品' : '确认上架商品'}
        description={
          statusTarget?.status === 'on'
            ? `「${statusTarget?.name}」下架后会员端不可见、不可下单；已开通的实例不受影响。`
            : `「${statusTarget?.name}」上架后会员端即可见并下单；若六个周期都没有可用价格，上架会被拒绝（409）。`
        }
        confirmLabel={statusTarget?.status === 'on' ? '确认下架' : '确认上架'}
        danger={statusTarget?.status === 'on'}
        pending={statusPending}
        onConfirm={handleStatusConfirm}
      >
        {statusError ? (
          <Alert variant="destructive">
            <AlertCircleIcon aria-hidden />
            <AlertDescription>{statusError}</AlertDescription>
          </Alert>
        ) : null}
      </ConfirmDialog>

      {syncOpen ? (
        <ProductSyncDialog
          products={selectedProducts}
          onClose={() => setSyncOpen(false)}
          onSynced={() => {
            refresh()
            toast.success('商品同步完成')
          }}
        />
      ) : null}

      {importOpen ? (
        <ProductImportDialog
          groups={groups}
          groupsLoading={groupsState.loading}
          groupsError={groupsState.error}
          onClose={() => setImportOpen(false)}
          onImported={() => {
            setPage(1)
            refresh()
            toast.success('商品导入完成')
          }}
        />
      ) : null}
    </div>
  )
}
