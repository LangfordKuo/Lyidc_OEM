import { useState } from 'react'
import { AlertTriangleIcon, DatabaseBackupIcon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { createDbBackup, deleteDbBackup, fetchDbBackups, restoreDbBackup } from '@/api/adminBackups'
import { errorMessage } from '@/api/client'
import type { DbBackup, DbBackupRestoreResult } from '@/api/types'
import AdminTable, { type AdminColumn } from '@/components/admin/AdminTable'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import { ErrorState, LoadingBlock } from '@/components/common/PageState'
import StatusBadge from '@/components/common/StatusBadge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { useAsync } from '@/hooks/useAsync'
import { formatDateTimeOr, formatFileSize } from '@/lib/format'

/**
 * DbBackupsPanel 是设置页的「数据库备份」分区（契约 21.7，R14）。
 *
 * 只有元信息展示：**没有下载按钮，也没有「查看内容」**（契约 21.4：备份含管理员密码哈希，
 * 绝不从网站下发）。恢复是覆盖式且不可撤销，必须输入当前管理员密码；删除需二次确认。
 * 整个设置页仅 admin 可进（整页闸门已挡 finance / support），因此这里不额外判断角色、
 * 也不发起任何越权请求。
 */
export default function DbBackupsPanel() {
  const state = useAsync(fetchDbBackups, [])
  const [creating, setCreating] = useState(false)
  const [restoreTarget, setRestoreTarget] = useState<DbBackup | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<DbBackup | null>(null)
  const [password, setPassword] = useState('')
  const [restoring, setRestoring] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [lastResult, setLastResult] = useState<DbBackupRestoreResult | null>(null)

  const data = state.data

  const handleCreate = async () => {
    setCreating(true)
    setLastResult(null)
    try {
      const record = await createDbBackup()
      toast.success(`备份完成：${record.file_name}（${formatFileSize(record.file_size)}）`)
      state.reload()
    } catch (err) {
      toast.error(errorMessage(err, '备份失败，请稍后重试'))
    } finally {
      setCreating(false)
    }
  }

  const handleRestore = async () => {
    if (!restoreTarget) {
      return
    }
    if (!password.trim()) {
      toast.error('请输入当前管理员密码以确认恢复')
      return
    }
    setRestoring(true)
    try {
      const result = await restoreDbBackup(restoreTarget.id, { confirm: true, password })
      setLastResult(result)
      setRestoreTarget(null)
      setPassword('')
      state.reload()
      toast.success(`已用 ${result.file_name} 覆盖当前数据库`)
    } catch (err) {
      // 密码错误 / 未确认 / 目录不可用等都由后端给出可读原因（40002）。
      toast.error(errorMessage(err, '恢复失败，请稍后重试'))
    } finally {
      setRestoring(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) {
      return
    }
    setDeleting(true)
    try {
      await deleteDbBackup(deleteTarget.id)
      toast.success(`已删除备份 ${deleteTarget.file_name}`)
      setDeleteTarget(null)
      state.reload()
    } catch (err) {
      toast.error(errorMessage(err, '删除失败，请稍后重试'))
    } finally {
      setDeleting(false)
    }
  }

  const columns: AdminColumn<DbBackup>[] = [
    {
      key: 'created_at',
      header: '备份时间',
      cell: (row) => <span className="whitespace-nowrap">{formatDateTimeOr(row.created_at)}</span>,
    },
    {
      key: 'file',
      header: '文件',
      cell: (row) => (
        <div className="space-y-0.5">
          <span className="font-mono text-xs break-all">{row.file_name}</span>
          {row.file_exists ? null : (
            <p className="text-xs text-destructive">文件已不存在（被手工删除），不可恢复</p>
          )}
          {row.meta_missing ? (
            <p className="text-xs text-warning">元信息缺失（字段已按可得信息回退）</p>
          ) : null}
        </div>
      ),
    },
    {
      key: 'size',
      header: '大小',
      className: 'whitespace-nowrap',
      cell: (row) => formatFileSize(row.file_size),
    },
    {
      key: 'schema',
      header: 'schema 版本',
      className: 'whitespace-nowrap',
      cell: (row) => row.schema_version,
    },
    {
      key: 'rows',
      header: '表 / 行数',
      className: 'whitespace-nowrap',
      cell: (row) => `${row.table_count} 表 · ${row.row_count} 行`,
    },
    {
      key: 'operator',
      header: '操作人',
      cell: (row) => (
        <span>
          {row.operator || '—'}
          <span className="block text-xs text-muted-foreground">{row.app_version || '—'}</span>
        </span>
      ),
    },
    {
      key: 'actions',
      header: '操作',
      className: 'whitespace-nowrap',
      cell: (row) => (
        <div className="flex flex-wrap gap-2">
          <Button
            size="sm"
            variant="outline"
            disabled={!row.file_exists}
            title={row.file_exists ? undefined : '备份文件已不存在'}
            onClick={() => {
              setPassword('')
              setRestoreTarget(row)
            }}
          >
            恢复
          </Button>
          <Button
            size="sm"
            variant="outline"
            className="text-destructive"
            onClick={() => setDeleteTarget(row)}
          >
            删除
          </Button>
        </div>
      ),
    },
  ]

  return (
    <Card id="db-backups" data-testid="db-backups-panel">
      <CardHeader className="border-b">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <div className="flex items-center gap-2">
              <DatabaseBackupIcon className="size-4 text-muted-foreground" aria-hidden />
              <CardTitle className="text-base">数据库备份</CardTitle>
            </div>
            <CardDescription>
              手动备份整库为 SQL 文件（备份文件存在 Web 目录之外，且不提供下载）；恢复为
              <strong>覆盖式</strong>，会替换当前库的全部数据。
            </CardDescription>
          </div>
          <Button
            disabled={creating || (data ? !data.backup_dir_outside_web_root : true)}
            onClick={handleCreate}
            data-testid="db-backup-create"
            title={
              data && !data.backup_dir_outside_web_root ? '备份目录不可用，请先修正配置' : undefined
            }
          >
            {creating ? (
              <>
                <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                备份中…
              </>
            ) : (
              '立即备份'
            )}
          </Button>
        </div>
      </CardHeader>

      <CardContent className="space-y-4">
        {state.loading && !data ? (
          <LoadingBlock label="正在读取备份列表…" />
        ) : state.error && !data ? (
          <ErrorState message={state.error} onRetry={state.reload} />
        ) : data ? (
          <>
            <dl className="grid gap-3 text-sm sm:grid-cols-3">
              <div>
                <dt className="text-xs text-muted-foreground">当前数据库</dt>
                <dd className="font-mono text-foreground">{data.database}</dd>
              </div>
              <div className="sm:col-span-2">
                <dt className="text-xs text-muted-foreground">备份目录</dt>
                <dd className="flex flex-wrap items-center gap-2">
                  <span className="font-mono text-xs break-all text-foreground">
                    {data.backup_dir}
                  </span>
                  {data.backup_dir_outside_web_root ? (
                    <StatusBadge tone="ok" label="不在 Web 根内" />
                  ) : (
                    <StatusBadge tone="error" label="目录不可用" />
                  )}
                  {data.backup_dir_default ? (
                    <span className="text-xs text-muted-foreground">（缺省目录）</span>
                  ) : null}
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">备份总占用磁盘</dt>
                <dd className="text-foreground" data-testid="db-backup-total-size">
                  {formatFileSize(data.total_size_bytes)}
                </dd>
              </div>
            </dl>

            {data.backup_dir_outside_web_root ? null : (
              <Alert variant="destructive">
                <AlertTriangleIcon aria-hidden />
                <AlertTitle>备份目录不可用</AlertTitle>
                <AlertDescription>{data.backup_dir_message}</AlertDescription>
              </Alert>
            )}

            <p className="text-xs text-muted-foreground">
              备份文件<strong>不会自动清理</strong>：需要腾出空间时请用下方「删除」手动处理。
              备份内容含全库数据（含管理员密码哈希），因此本页不提供下载，也不提供查看内容；
              文件在服务器上的备份目录内，请用运维手段处理异地留存。
            </p>

            {lastResult ? (
              <Alert data-testid="db-backup-restore-result">
                <AlertDescription>
                  已用 {lastResult.file_name} 覆盖当前数据库（{lastResult.table_count} 张表
                  {lastResult.dropped_tables.length > 0
                    ? `，删除备份中不存在的表：${lastResult.dropped_tables.join('、')}`
                    : ''}
                  {lastResult.migrated
                    ? `；已执行数据库迁移：${lastResult.from_schema_version} → ${lastResult.to_schema_version}`
                    : ''}
                  ）。
                  {lastResult.warning ? (
                    <span className="mt-1 block text-warning">{lastResult.warning}</span>
                  ) : null}
                </AlertDescription>
              </Alert>
            ) : null}

            <AdminTable
              columns={columns}
              rows={data.backups}
              rowKey={(row) => row.id}
              ariaLabel="数据库备份列表"
              empty={
                <p className="text-sm text-muted-foreground">
                  还没有备份。建议在每次升级、改配置或批量导入前点一次「立即备份」。
                </p>
              }
            />
          </>
        ) : null}
      </CardContent>

      <ConfirmDialog
        open={restoreTarget !== null}
        onOpenChange={(open) => {
          if (!open) {
            setRestoreTarget(null)
            setPassword('')
          }
        }}
        title="恢复数据库（覆盖式）"
        confirmLabel="确认恢复并覆盖"
        danger
        pending={restoring}
        onConfirm={handleRestore}
        description={
          restoreTarget ? (
            <div className="space-y-2">
              <p>
                将用 <span className="font-mono text-xs">{restoreTarget.file_name}</span>
                （{formatDateTimeOr(restoreTarget.created_at)}，
                {formatFileSize(restoreTarget.file_size)}）的备份内容
                <strong className="text-destructive">覆盖当前数据库 {data?.database ?? ''}</strong>。
              </p>
              <p className="text-destructive">
                <strong>不可撤销</strong>：当前库的数据会被备份内容整体替换，备份点之后的新数据
                （含之后新建的表）都会消失。
              </p>
              <p>
                <strong>失败可能留下不完整的库</strong>（本系统不做恢复前自动备份），
                恢复期间网站也会短暂不可用。强烈建议先点一次「立即备份」再做恢复。
              </p>
            </div>
          ) : null
        }
      >
        <Field>
          <FieldLabel htmlFor="db-restore-password">当前管理员密码（必填）</FieldLabel>
          <Input
            id="db-restore-password"
            name="db_restore_password"
            type="password"
            autoComplete="current-password"
            placeholder="输入你本人的登录密码以确认"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </Field>
      </ConfirmDialog>

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteTarget(null)
          }
        }}
        title="删除备份"
        confirmLabel="确认删除"
        danger
        pending={deleting}
        onConfirm={handleDelete}
        description={
          deleteTarget ? (
            <p>
              将删除 <span className="font-mono text-xs">{deleteTarget.file_name}</span>
              （{formatDateTimeOr(deleteTarget.created_at)}，{formatFileSize(deleteTarget.file_size)}）。
              删除后无法恢复该备份文件。
            </p>
          ) : null
        }
      />
    </Card>
  )
}
