import { Alert, Button, Input, Label, TextField, toast } from '@heroui/react'
import { useState, type FormEvent } from 'react'

import {
  importAdminProducts,
  listAdminProductGroups,
  listAdminProducts,
  updateAdminProduct,
} from '../../api/adminProducts'
import { errorMessage } from '../../api/client'
import type { AdminProduct, ProductImportResult } from '../../api/types'
import { useAdminAuth } from '../../auth/adminAuthContext'
import AdminTable, { type AdminColumn } from '../../components/admin/AdminTable'
import FilterSelect, { AdminFilterBar } from '../../components/admin/FilterSelect'
import ProductGroupsPanel from '../../components/admin/ProductGroupsPanel'
import ProductPricingDialog from '../../components/admin/ProductPricingDialog'
import ConfirmDialog from '../../components/common/ConfirmDialog'
import Pager from '../../components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '../../components/common/PageState'
import StatusFilter from '../../components/common/StatusFilter'
import StatusBadge from '../../components/StatusBadge'
import { useAsync } from '../../hooks/useAsync'
import { ADMIN_ROLE_LABELS, hasPermission, permissionHint } from '../../lib/adminRoles'
import { availableCycles, CYCLE_LABELS, type BillingCycle, type CyclePrices } from '../../lib/cycles'
import { formatMoney } from '../../lib/format'
import { DEFAULT_PAGE_SIZE } from '../../lib/pagination'

type StatusFilterValue = '' | 'on' | 'off'

const STATUS_OPTIONS: { value: StatusFilterValue; label: string }[] = [
  { value: '', label: '全部' },
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

// AdminProducts 是管理后台「商品」页（契约 10.4 / 10.5）：
// 列表（关键词 / 分组 / 状态筛选 + 分页）+ 上游导入 + 定价设置 + 上下架 + 分组管理。
// 角色矩阵：admin / finance 全部可用；support 只读（列表与分组可看，写操作按钮禁用并给出原因）。
export default function AdminProducts() {
  const { role } = useAdminAuth()
  const canWrite = hasPermission(role, 'products.write')
  const canImport = hasPermission(role, 'products.import')
  const canEditGroups = hasPermission(role, 'groups.write')

  const [keywordInput, setKeywordInput] = useState('')
  const [keyword, setKeyword] = useState('')
  const [groupId, setGroupId] = useState('')
  const [status, setStatus] = useState<StatusFilterValue>('')
  const [page, setPage] = useState(1)
  const [refreshKey, setRefreshKey] = useState(0)

  const [importOpen, setImportOpen] = useState(false)
  const [importing, setImporting] = useState(false)
  const [importResult, setImportResult] = useState<ProductImportResult | null>(null)
  const [importError, setImportError] = useState('')

  const [pricingTarget, setPricingTarget] = useState<AdminProduct | null>(null)
  const [statusTarget, setStatusTarget] = useState<AdminProduct | null>(null)
  const [statusPending, setStatusPending] = useState(false)
  const [statusError, setStatusError] = useState('')

  const productsState = useAsync(
    () =>
      listAdminProducts({
        page,
        page_size: DEFAULT_PAGE_SIZE,
        ...(groupId ? { group_id: Number(groupId) } : {}),
        ...(status ? { status } : {}),
        ...(keyword ? { keyword } : {}),
      }),
    [page, groupId, status, keyword, refreshKey],
  )
  const groupsState = useAsync(() => listAdminProductGroups(), [refreshKey])

  const products = productsState.data?.items ?? []
  const groups = groupsState.data?.items ?? []

  /** 刷新列表与分组（写操作成功后调用，保证两侧数据一致）。 */
  const refresh = () => setRefreshKey((value) => value + 1)

  const handleSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setKeyword(keywordInput.trim())
    setPage(1)
  }

  const changeGroup = (next: string) => {
    setGroupId(next)
    setPage(1)
  }

  const changeStatus = (next: StatusFilterValue) => {
    setStatus(next)
    setPage(1)
  }

  /** 上游导入：一次只读拉取上游目录 + 逐个商品详情，耗时可能数十秒，用 ConfirmDialog 二次确认。 */
  const handleImport = async () => {
    // 确认后立即收起弹窗，由顶部按钮的「正在导入…」禁用态防重复提交。
    setImportOpen(false)
    setImporting(true)
    setImportError('')
    setImportResult(null)
    try {
      const result = await importAdminProducts()
      setImportResult(result)
      setPage(1)
      refresh()
      toast.success('商品导入完成')
    } catch (err) {
      // 上游未配置 / 目录拉取失败等（500）原样展示后端 message。
      setImportError(errorMessage(err, '导入商品失败，请稍后重试'))
    } finally {
      setImporting(false)
    }
  }

  /** 上下架：ConfirmDialog 二次确认后提交 status（上架可能触发后端 409 校验）。 */
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

  const failedPids = importResult?.failed_pids?.slice(0, 20) ?? []

  const columns: AdminColumn<AdminProduct>[] = [
    {
      key: 'name',
      header: '商品',
      cell: (row) => (
        <div className="min-w-0">
          <p className="truncate font-medium text-foreground">{row.name}</p>
          <p className="mt-0.5 font-mono text-xs text-muted">#{row.id}</p>
        </div>
      ),
    },
    {
      key: 'group',
      header: '分组',
      cell: (row) => (row.group_name ? row.group_name : <span className="text-muted">未分组</span>),
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
            <p className="mt-0.5 text-xs text-muted">可售 {count} 个周期</p>
          </div>
        )
      },
    },
    {
      key: 'stock',
      header: '库存',
      cell: (row) =>
        row.stock_control === 0 ? (
          <span className="text-muted">不限</span>
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
            isDisabled={!canWrite}
            onPress={() => setPricingTarget(row)}
          >
            定价设置
          </Button>
          <Button
            size="sm"
            variant={row.status === 'on' ? 'danger' : 'primary'}
            isDisabled={!canWrite}
            onPress={() => {
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
          <h1 className="text-xl font-semibold text-foreground">商品</h1>
          <p className="mt-1 text-sm text-muted">
            管理本地商品目录：从上游导入、配置定价规则、上架或下架商品。
          </p>
          {!canWrite ? (
            <p className="mt-1 text-xs text-muted">
              当前角色（{role ? ADMIN_ROLE_LABELS[role] : '未登录'}）为只读视角：导入、定价设置、
              上下架与分组保存均已禁用（{permissionHint('products.write')}）。
            </p>
          ) : null}
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onPress={refresh}>
            刷新
          </Button>
          <Button
            variant="primary"
            size="sm"
            isDisabled={!canImport || importing}
            onPress={() => setImportOpen(true)}
          >
            {importing ? '正在导入…' : '从上游导入商品'}
          </Button>
        </div>
      </header>

      {importing ? (
        <Alert status="default">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Description>
              正在从上游导入商品（只读调用上游目录与商品详情，可能耗时数十秒），请勿关闭页面…
            </Alert.Description>
          </Alert.Content>
        </Alert>
      ) : null}

      {importError ? (
        <Alert status="danger">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>导入失败</Alert.Title>
            <Alert.Description>{importError}</Alert.Description>
          </Alert.Content>
          <Button size="sm" variant="outline" onPress={() => setImportError('')}>
            关闭
          </Button>
        </Alert>
      ) : null}

      {importResult ? (
        <Alert status={importResult.failed > 0 ? 'warning' : 'success'}>
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>上游导入完成</Alert.Title>
            <Alert.Description>
              新建 {importResult.created} 个、更新 {importResult.updated} 个、无变化{' '}
              {importResult.unchanged} 个、分组 {importResult.groups} 个。
              {importResult.failed > 0
                ? ` 另有 ${importResult.failed} 个商品详情抓取失败，已跳过（不影响其余商品）。`
                : ''}
            </Alert.Description>
            {failedPids.length > 0 ? (
              <p className="mt-1 text-xs text-muted">
                跳过的商品 ID（最多展示 20 个）：{failedPids.join('、')}
              </p>
            ) : null}
          </Alert.Content>
          <Button size="sm" variant="outline" onPress={() => setImportResult(null)}>
            关闭
          </Button>
        </Alert>
      ) : null}

      <AdminFilterBar
        actions={
          <span className="text-xs text-muted">
            共 {productsState.data?.total ?? 0} 个商品
            {groupId ? '（已按分组筛选）' : ''}
          </span>
        }
      >
        <form className="flex items-end gap-2" onSubmit={handleSearch}>
          <TextField
            name="product_keyword"
            value={keywordInput}
            onChange={setKeywordInput}
            className="w-52"
          >
            <Label>关键词</Label>
            <Input placeholder="搜索商品名称" />
          </TextField>
          <Button type="submit" size="sm" variant="outline">
            搜索
          </Button>
          {keyword ? (
            <Button
              size="sm"
              variant="ghost"
              onPress={() => {
                setKeywordInput('')
                setKeyword('')
                setPage(1)
              }}
            >
              清除
            </Button>
          ) : null}
        </form>

        <FilterSelect
          label="分组"
          value={groupId}
          onChange={changeGroup}
          options={[
            { value: '', label: '全部分组' },
            ...groups.map((group) => ({ value: String(group.id), label: group.name })),
          ]}
        />

        <StatusFilter
          label="按状态筛选"
          options={STATUS_OPTIONS}
          value={status}
          onChange={changeStatus}
        />
      </AdminFilterBar>

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
                keyword || groupId || status
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
          pageSize={productsState.data.page_size || DEFAULT_PAGE_SIZE}
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

      <ConfirmDialog
        isOpen={Boolean(statusTarget)}
        title={statusTarget?.status === 'on' ? '确认下架商品' : '确认上架商品'}
        description={
          statusTarget?.status === 'on'
            ? `「${statusTarget?.name}」下架后会员端不可见、不可下单；已开通的实例不受影响。`
            : `「${statusTarget?.name}」上架后会员端即可见并下单；若六个周期都没有可用价格，上架会被拒绝（409）。`
        }
        confirmLabel={statusTarget?.status === 'on' ? '确认下架' : '确认上架'}
        isDanger={statusTarget?.status === 'on'}
        pending={statusPending}
        onConfirm={handleStatusConfirm}
        onCancel={() => {
          setStatusTarget(null)
          setStatusError('')
        }}
      >
        {statusError ? (
          <Alert status="danger">
            <Alert.Indicator />
            <Alert.Content>
              <Alert.Description>{statusError}</Alert.Description>
            </Alert.Content>
          </Alert>
        ) : null}
      </ConfirmDialog>

      <ConfirmDialog
        isOpen={importOpen}
        title="从上游导入商品"
        description="将从上游读取商品目录与每个商品的详情（只读调用），耗时可能数十秒，期间请不要关闭页面。导入按上游 PID 幂等：已存在则更新、无变化则跳过，并自动创建缺失的分组。"
        confirmLabel="开始导入"
        pending={importing}
        onConfirm={handleImport}
        onCancel={() => setImportOpen(false)}
      />
    </div>
  )
}
