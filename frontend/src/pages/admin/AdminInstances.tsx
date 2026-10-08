import { Button, Input, Label, TextField } from '@heroui/react'
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { listAdminInstances } from '../../api/adminInstances'
import type { AdminInstance, InstanceStatus } from '../../api/types'
import { paths } from '../../app/paths'
import StatusBadge from '../../components/StatusBadge'
import AdminTable, { type AdminColumn } from '../../components/admin/AdminTable'
import Pager from '../../components/common/Pager'
import { EmptyBlock, ErrorState, LoadingBlock } from '../../components/common/PageState'
import StatusFilter from '../../components/common/StatusFilter'
import { useAsync } from '../../hooks/useAsync'
import { expiryColorClass, expiryState, expiryText } from '../../lib/expiry'
import { formatCycleLabel, formatDateTimeOr } from '../../lib/format'
import { cancelStatusLabel, instanceStatusLabel, instanceStatusTone } from '../../lib/instanceStatus'
import { DEFAULT_PAGE_SIZE } from '../../lib/pagination'

type StatusFilterValue = '' | InstanceStatus

const STATUS_OPTIONS: { value: StatusFilterValue; label: string }[] = [
  { value: '', label: '全部' },
  { value: 'active', label: '运行中' },
  { value: 'suspended', label: '已暂停' },
  { value: 'terminated', label: '已终止' },
  { value: 'cancelled', label: '已取消' },
]

// AdminInstances 是管理后台「实例」列表页（契约 14.4）：全站实例 + 会员/状态筛选。
// 列表不含主机账号密码等敏感字段（详情页才有），三角色均可访问。
export default function AdminInstances() {
  const navigate = useNavigate()
  const [status, setStatus] = useState<StatusFilterValue>('')
  const [memberID, setMemberID] = useState('')
  const [appliedMemberID, setAppliedMemberID] = useState('')
  const [memberError, setMemberError] = useState('')
  const [page, setPage] = useState(1)

  const instancesState = useAsync(
    () =>
      listAdminInstances({
        page,
        page_size: DEFAULT_PAGE_SIZE,
        ...(status ? { status } : {}),
        ...(appliedMemberID ? { member_id: Number(appliedMemberID) } : {}),
      }),
    [page, status, appliedMemberID],
  )
  const instances = instancesState.data?.items ?? []

  const applyMemberFilter = () => {
    const trimmed = memberID.trim()
    if (trimmed && (!/^\d+$/.test(trimmed) || Number(trimmed) <= 0)) {
      setMemberError('会员 ID 必须为正整数')
      return
    }
    setMemberError('')
    setAppliedMemberID(trimmed)
    setPage(1)
  }

  const columns: AdminColumn<AdminInstance>[] = [
    {
      key: 'name',
      header: '实例',
      cell: (instance) => (
        <Link
          className="font-mono text-sm text-foreground hover:text-accent"
          to={paths.adminInstanceDetail(instance.id)}
        >
          {instance.name}
        </Link>
      ),
    },
    {
      key: 'member',
      header: '会员',
      cell: (instance) => <span className="text-sm text-muted">#{instance.member_id}</span>,
    },
    {
      key: 'product',
      header: '商品',
      cell: (instance) => (
        <span className="text-sm">
          {instance.product_name || '—'}
          <span className="ml-1 text-xs text-muted">{formatCycleLabel(instance.billing_cycle)}</span>
        </span>
      ),
    },
    {
      key: 'ip',
      header: 'IP',
      cell: (instance) => <span className="font-mono text-xs">{instance.dedicated_ip || '—'}</span>,
    },
    {
      key: 'status',
      header: '状态',
      cell: (instance) => (
        <span className="flex flex-wrap items-center gap-1.5">
          <StatusBadge
            tone={instanceStatusTone(instance.status)}
            label={instanceStatusLabel(instance.status)}
          />
          {instance.cancel_status === 'pending' ? (
            <StatusBadge tone="warn" label={cancelStatusLabel(instance.cancel_status)} />
          ) : null}
        </span>
      ),
    },
    {
      key: 'due',
      header: '到期',
      cell: (instance) => {
        const state = expiryState(instance.next_due_date)
        return (
          <span className={`text-xs ${expiryColorClass(state)}`}>
            {formatDateTimeOr(instance.next_due_date)}
            {instance.next_due_date ? ` · ${expiryText(instance.next_due_date)}` : ''}
          </span>
        )
      },
    },
    {
      key: 'actions',
      header: '操作',
      cell: (instance) => (
        <Button
          size="sm"
          variant="outline"
          onPress={() => navigate(paths.adminInstanceDetail(instance.id))}
        >
          详情与操作
        </Button>
      ),
    },
  ]

  return (
    <div className="space-y-5">
      <header>
        <h1 className="text-xl font-semibold text-foreground">实例</h1>
        <p className="mt-1 text-sm text-muted">
          全站实例列表（按会员/状态筛选）。详情页可执行同步、暂停/恢复与终止申请。
        </p>
      </header>

      <div className="flex flex-wrap items-end justify-between gap-3">
        <StatusFilter
          label="按状态筛选"
          options={STATUS_OPTIONS}
          value={status}
          onChange={(next) => {
            setStatus(next)
            setPage(1)
          }}
        />
        <div className="flex items-end gap-2">
          <TextField
            name="member_id"
            type="text"
            value={memberID}
            onChange={(value) => {
              setMemberID(value)
              setMemberError('')
            }}
            isInvalid={Boolean(memberError)}
            className="w-40"
          >
            <Label>会员 ID</Label>
            <Input placeholder="全部会员" inputMode="numeric" />
          </TextField>
          <Button size="sm" variant="outline" onPress={applyMemberFilter}>
            查询
          </Button>
          <Button size="sm" variant="outline" onPress={instancesState.reload}>
            刷新
          </Button>
        </div>
      </div>
      {memberError ? <p className="text-xs text-danger">{memberError}</p> : null}

      {instancesState.loading ? <LoadingBlock label="正在读取实例…" /> : null}
      {instancesState.error ? (
        <ErrorState message={instancesState.error} onRetry={instancesState.reload} />
      ) : null}

      {!instancesState.loading && !instancesState.error ? (
        <>
          <AdminTable
            ariaLabel="实例列表"
            columns={columns}
            rows={instances}
            rowKey={(instance) => instance.id}
            empty={
              <EmptyBlock
                title={status || appliedMemberID ? '当前筛选下没有实例' : '还没有实例'}
                description={
                  status || appliedMemberID
                    ? '换个状态或会员 ID 再试。'
                    : '会员下单并交付成功后，实例会出现在这里。'
                }
              />
            }
          />
          {instancesState.data && instancesState.data.total > 0 ? (
            <Pager
              page={page}
              total={instancesState.data.total}
              pageSize={instancesState.data.page_size || DEFAULT_PAGE_SIZE}
              onChange={setPage}
            />
          ) : null}
        </>
      ) : null}
    </div>
  )
}
