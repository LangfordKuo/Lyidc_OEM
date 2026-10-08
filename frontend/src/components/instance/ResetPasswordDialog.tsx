import { Alert, Button, Input, Label, Modal, Radio, RadioGroup, TextField, toast } from '@heroui/react'
import { useState } from 'react'

import { errorMessage } from '../../api/client'
import { resetInstancePassword } from '../../api/instances'
import type { InstanceSummary } from '../../api/types'
import { validateInstancePassword } from '../../lib/validate'
import SecretValue from '../common/SecretValue'

type Mode = 'auto' | 'custom'

// ResetPasswordDialog：重置主机密码（契约 15.2）。自动生成或自定义（8-64 位且含字母与数字），
// 成功后弹窗内展示新密码（可显示/复制），并刷新详情让实例密码同步为最新值。
export default function ResetPasswordDialog({
  instance,
  onClose,
  onDone,
}: {
  instance: InstanceSummary
  onClose: () => void
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
    <Modal
      isOpen
      onOpenChange={(open) => {
        if (!open && !pending) {
          onClose()
        }
      }}
    >
      <Modal.Backdrop>
        <Modal.Container size="md" placement="center">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>重置主机密码</Modal.Heading>
            </Modal.Header>
            <Modal.Body className="space-y-4">
              {result ? (
                <div className="space-y-3">
                  <Alert status="success">
                    <Alert.Indicator />
                    <Alert.Content>
                      <Alert.Title>密码已重置</Alert.Title>
                      <Alert.Description>
                        上游改密为异步执行，约 30 秒内生效；实例详情页可随时查看当前密码。
                      </Alert.Description>
                    </Alert.Content>
                  </Alert>
                  <div className="rounded-lg border border-border bg-surface-secondary/50 p-3">
                    <p className="mb-1 text-xs text-muted">新密码</p>
                    <SecretValue value={result} label="密码" />
                  </div>
                </div>
              ) : (
                <>
                  <RadioGroup
                    aria-label="密码方式"
                    value={mode}
                    onChange={(value) => setMode(value as Mode)}
                    className="gap-3"
                  >
                    {/* HeroUI v3 的 Radio 必须用 Radio.Content 包裹才是可交互控件（否则渲染为不可选中的纯文本）；
                        圆圈（Radio.Control / Radio.Indicator）同样要放在 Radio.Content 内、文本之前。 */}
                    <Radio value="auto">
                      <Radio.Content>
                        <Radio.Control>
                          <Radio.Indicator />
                        </Radio.Control>
                        <span className="flex flex-col">
                          <span className="text-sm font-medium">自动生成</span>
                          <span className="text-xs text-muted">
                            由系统生成 16 位强密码（大小写、数字、符号齐备）
                          </span>
                        </span>
                      </Radio.Content>
                    </Radio>
                    <Radio value="custom">
                      <Radio.Content>
                        <Radio.Control>
                          <Radio.Indicator />
                        </Radio.Control>
                        <span className="flex flex-col">
                          <span className="text-sm font-medium">自定义密码</span>
                          <span className="text-xs text-muted">
                            8-64 个字符，需同时包含字母与数字
                          </span>
                        </span>
                      </Radio.Content>
                    </Radio>
                  </RadioGroup>

                  {mode === 'custom' ? (
                    <TextField
                      name="instance_password"
                      type="password"
                      value={password}
                      onChange={setPassword}
                      isInvalid={Boolean(password && passwordError)}
                    >
                      <Label>新密码</Label>
                      <Input placeholder="输入新密码" />
                      {password && passwordError ? (
                        <p className="mt-1 text-xs text-danger">{passwordError}</p>
                      ) : null}
                    </TextField>
                  ) : null}
                </>
              )}

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
              {result ? (
                <Button variant="primary" onPress={onClose}>
                  我已保存
                </Button>
              ) : (
                <>
                  <Button variant="outline" isDisabled={pending} onPress={onClose}>
                    取消
                  </Button>
                  <Button
                    variant="primary"
                    isDisabled={pending || Boolean(passwordError)}
                    onPress={handleConfirm}
                  >
                    {pending ? '正在重置…' : '确认重置'}
                  </Button>
                </>
              )}
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  )
}
