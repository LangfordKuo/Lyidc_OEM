import { Alert, Button, Input, Label, Modal, TextField, toast } from '@heroui/react'
import { useState } from 'react'

import { updateAdminProductGroup } from '../../api/adminProducts'
import { errorMessage } from '../../api/client'
import type { AdminProductGroup } from '../../api/types'
import { permissionHint } from '../../lib/adminRoles'

// 商品分组面板（契约 10.4）：可折叠区块展示全部分组（含空分组）与商品计数，
// 支持「点击分组行 → 列表按该分组筛选」与「重命名 / 改排序」（仅 admin / finance，groups.write）。
// 分组名与排序是本地字段，导入不会覆盖，因此可放心按自己的品牌命名。

const MAX_SORT = 999999

/** 分组名校验：去首尾空格后 1-128 个字符。 */
function validateGroupName(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) {
    return '请输入分组名称'
  }
  if (trimmed.length > 128) {
    return '名称最多 128 个字符'
  }
  return ''
}

/** 排序校验：整数，-999999 ~ 999999。 */
function validateGroupSort(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) {
    return '请输入排序值'
  }
  if (!/^-?\d+$/.test(trimmed)) {
    return '排序需为整数'
  }
  const sort = Number(trimmed)
  if (sort < -MAX_SORT || sort > MAX_SORT) {
    return `排序需为 ${-MAX_SORT} 到 ${MAX_SORT} 之间的整数`
  }
  return ''
}

export default function ProductGroupsPanel({
  groups,
  loading,
  error,
  onReload,
  canWrite,
  selectedGroupId,
  onSelectGroup,
}: {
  groups: AdminProductGroup[]
  loading: boolean
  error: string
  onReload: () => void
  /** groups.write：无权限时仅可查看与筛选，编辑入口禁用。 */
  canWrite: boolean
  /** 当前列表使用中的分组筛选值（'' = 全部分组）。 */
  selectedGroupId: string
  onSelectGroup: (groupId: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const [editTarget, setEditTarget] = useState<AdminProductGroup | null>(null)

  return (
    <section className="rounded-xl border border-border bg-surface">
      <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
        <div className="min-w-0">
          <p className="text-sm font-medium text-foreground">商品分组</p>
          <p className="mt-0.5 text-xs text-muted">
            共 {groups.length} 个分组；点击分组行可把该分组作为列表筛选条件。
            {canWrite ? '' : `（${permissionHint('groups.write')}，暂不可重命名或改排序）`}
          </p>
        </div>
        <Button
          size="sm"
          variant="outline"
          aria-expanded={expanded}
          onPress={() => setExpanded((value) => !value)}
        >
          {expanded ? '收起分组' : `展开分组（${groups.length}）`}
        </Button>
      </div>

      {expanded ? (
        <div className="space-y-2 border-t border-border px-4 py-3">
          {loading ? <p className="text-xs text-muted">正在读取分组…</p> : null}

          {error ? (
            <Alert status="danger">
              <Alert.Indicator />
              <Alert.Content>
                <Alert.Description>分组读取失败：{error}</Alert.Description>
              </Alert.Content>
              <Button size="sm" variant="outline" onPress={onReload}>
                重试
              </Button>
            </Alert>
          ) : null}

          {!loading && !error && groups.length === 0 ? (
            <p className="text-xs text-muted">
              还没有分组：从上游导入商品时会按上游目录自动创建分组。
            </p>
          ) : null}

          {groups.map((group) => {
            const active = selectedGroupId === String(group.id)
            return (
              <div
                key={group.id}
                className={[
                  'flex flex-wrap items-center justify-between gap-3 rounded-lg border px-3 py-2',
                  active ? 'border-accent bg-accent-soft' : 'border-border',
                ].join(' ')}
              >
                <button
                  type="button"
                  className="min-w-0 text-left"
                  aria-pressed={active}
                  onClick={() => onSelectGroup(active ? '' : String(group.id))}
                >
                  <span className="block truncate text-sm text-foreground hover:text-accent">
                    {group.name}
                    {active ? <span className="ml-2 text-xs text-accent">筛选中</span> : null}
                  </span>
                  <span className="mt-0.5 block text-xs text-muted">
                    排序 {group.sort} · 商品 {group.products.total} 个（已上架 {group.products.on} / 已下架{' '}
                    {group.products.off}）
                  </span>
                </button>
                <Button
                  size="sm"
                  variant="outline"
                  isDisabled={!canWrite}
                  onPress={() => setEditTarget(group)}
                >
                  编辑
                </Button>
              </div>
            )
          })}
        </div>
      ) : null}

      {editTarget ? (
        <GroupEditDialog
          group={editTarget}
          onClose={() => setEditTarget(null)}
          onSaved={() => {
            setEditTarget(null)
            onReload()
          }}
        />
      ) : null}
    </section>
  )
}

/** 分组编辑弹窗：重命名（1-128 字符）与改排序（-999999 ~ 999999），至少提交一次修改。 */
function GroupEditDialog({
  group,
  onClose,
  onSaved,
}: {
  group: AdminProductGroup
  onClose: () => void
  onSaved: () => void
}) {
  const [name, setName] = useState(group.name)
  const [sort, setSort] = useState(String(group.sort))
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const nameError = name ? validateGroupName(name) : ''
  const sortError = sort ? validateGroupSort(sort) : ''

  const handleSubmit = async () => {
    const trimmed = name.trim()
    const nameMessage = validateGroupName(name)
    const sortMessage = validateGroupSort(sort)
    if (nameMessage || sortMessage) {
      setError(nameMessage || sortMessage)
      return
    }
    if (trimmed === group.name && Number(sort) === group.sort) {
      setError('名称与排序均未变化，无需保存')
      return
    }

    setPending(true)
    setError('')
    try {
      await updateAdminProductGroup(group.id, { name: trimmed, sort: Number(sort.trim()) })
      toast.success(`分组「${trimmed}」已更新`)
      onSaved()
    } catch (err) {
      setError(errorMessage(err, '保存分组失败，请稍后重试'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Modal
      isOpen
      onOpenChange={(open) => {
        if (!open && !pending) {
          onClose()
        }
      }}
    >
      <Modal.Backdrop>
        <Modal.Container size="sm" placement="center">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>编辑分组</Modal.Heading>
            </Modal.Header>
            <Modal.Body className="space-y-4">
              <TextField
                name="group_name"
                value={name}
                onChange={(value) => {
                  setName(value)
                  setError('')
                }}
                isInvalid={Boolean(name && nameError)}
              >
                <Label>分组名称</Label>
                <Input placeholder="1-128 个字符，保存时自动去首尾空格" />
                {name && nameError ? (
                  <p className="mt-1 text-xs text-danger">{nameError}</p>
                ) : null}
              </TextField>

              <TextField
                name="group_sort"
                value={sort}
                onChange={(value) => {
                  setSort(value)
                  setError('')
                }}
                isInvalid={Boolean(sort && sortError)}
              >
                <Label>排序</Label>
                <Input placeholder="-999999 ~ 999999，越小越靠前" inputMode="numeric" />
                {sort && sortError ? (
                  <p className="mt-1 text-xs text-danger">{sortError}</p>
                ) : null}
              </TextField>

              <p className="text-xs text-muted">
                分组名与排序是本地字段，上游导入不会覆盖；商品计数由系统实时统计。
              </p>

              {error ? (
                <Alert status="danger">
                  <Alert.Indicator />
                  <Alert.Content>
                    <Alert.Description>{error}</Alert.Description>
                  </Alert.Content>
                </Alert>
              ) : null}
            </Modal.Body>
            <Modal.Footer>
              <Button variant="outline" isDisabled={pending} onPress={onClose}>
                取消
              </Button>
              <Button variant="primary" isDisabled={pending} onPress={handleSubmit}>
                {pending ? '正在保存…' : '保存'}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  )
}
