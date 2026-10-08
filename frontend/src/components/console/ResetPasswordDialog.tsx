import { useState } from 'react'
import { AlertCircleIcon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { errorMessage } from '@/api/client'
import { resetInstancePassword } from '@/api/instances'
import type { InstanceSummary } from '@/api/types'
import SecretValue from '@/components/common/SecretValue'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { validateInstancePassword } from '@/lib/validate'
import { cn } from '@/lib/utils'

type Mode = 'auto' | 'custom'

// ResetPasswordDialog：重置主机密码（契约 15.2）。自动生成或自定义（8-64 位且含字母与数字），
// 成功后弹窗内展示新密码（默认遮蔽、可显示/复制），并刷新详情让实例密码同步为最新值。
export default function ResetPasswordDialog({
  instance,
  onOpenChange,
  onDone,
}: {
  instance: InstanceSummary
  onOpenChange: (open: boolean) => void
  onDone: () => void
}) {
  const [mode, setMode] = useState<Mode>('auto')
  const [password, setPassword] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState('')

  const passwordError = mode === 'custom' ? validateInstancePassword(password) : ''

  const handleConfirm = async () => {
    if (passwordError) {
      setError(passwordError)
      return
    }
    setPending(true)
    setError('')
    try {
      const response = await resetInstancePassword(
        instance.id,
        mode === 'custom' ? password : undefined,
      )
      setResult(response.password ?? '')
      toast.success('密码已重置，请立即保存新密码')
      onDone()
    } catch (err) {
      setError(errorMessage(err, '重置密码失败'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => (!pending ? onOpenChange(open) : undefined)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>重置主机密码</DialogTitle>
          <DialogDescription>
            实例 <span className="font-mono">{instance.name}</span> · 上游改密为异步执行，约 30 秒内生效。
          </DialogDescription>
        </DialogHeader>

        {result ? (
          <div className="space-y-3">
            <Alert>
              <AlertTitle>密码已重置</AlertTitle>
              <AlertDescription>
                请立即保存新密码；实例详情页可随时查看当前密码。
              </AlertDescription>
            </Alert>
            <div className="rounded-lg border border-border bg-muted/40 p-3">
              <p className="mb-1 text-xs text-muted-foreground">新密码</p>
              <SecretValue value={result} className="justify-start" />
            </div>
          </div>
        ) : (
          <>
            <RadioGroup
              value={mode}
              onValueChange={(value) => setMode(value as Mode)}
              aria-label="密码方式"
              className="gap-2"
            >
              <Label
                className={cn(
                  'flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors',
                  mode === 'auto'
                    ? 'border-primary bg-primary/5'
                    : 'border-border hover:bg-muted/50',
                )}
              >
                <RadioGroupItem value="auto" className="mt-0.5" />
                <span className="flex flex-col gap-0.5">
                  <span className="text-sm font-medium text-foreground">自动生成</span>
                  <span className="text-xs text-muted-foreground">
                    由系统生成 16 位强密码（大小写、数字、符号齐备）
                  </span>
                </span>
              </Label>
              <Label
                className={cn(
                  'flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors',
                  mode === 'custom'
                    ? 'border-primary bg-primary/5'
                    : 'border-border hover:bg-muted/50',
                )}
              >
                <RadioGroupItem value="custom" className="mt-0.5" />
                <span className="flex flex-col gap-0.5">
                  <span className="text-sm font-medium text-foreground">自定义密码</span>
                  <span className="text-xs text-muted-foreground">
                    8-64 个字符，需同时包含字母与数字
                  </span>
                </span>
              </Label>
            </RadioGroup>

            {mode === 'custom' ? (
              <div className="space-y-1.5">
                <Label htmlFor="instance_password">新密码</Label>
                <Input
                  id="instance_password"
                  type="password"
                  autoComplete="new-password"
                  value={password}
                  placeholder="输入新密码"
                  aria-invalid={Boolean(password && passwordError)}
                  onChange={(event) => {
                    setPassword(event.target.value)
                    setError('')
                  }}
                />
                {password && passwordError ? (
                  <p className="text-xs text-destructive">{passwordError}</p>
                ) : null}
              </div>
            ) : null}
          </>
        )}

        {error ? (
          <Alert variant="destructive">
            <AlertCircleIcon aria-hidden />
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        <DialogFooter className="gap-2">
          {result ? (
            <Button onClick={() => onOpenChange(false)}>我已保存</Button>
          ) : (
            <>
              <Button variant="outline" disabled={pending} onClick={() => onOpenChange(false)}>
                取消
              </Button>
              <Button
                disabled={pending || Boolean(passwordError)}
                onClick={handleConfirm}
              >
                {pending ? (
                  <>
                    <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
                    正在重置…
                  </>
                ) : (
                  '确认重置'
                )}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
