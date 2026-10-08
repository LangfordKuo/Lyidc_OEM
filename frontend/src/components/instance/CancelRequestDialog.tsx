import { Alert, Button, Input, Label, Modal, Radio, RadioGroup, TextField, toast } from '@heroui/react'
import { useState } from 'react'

import { errorMessage } from '../../api/client'
import { cancelInstance } from '../../api/instances'
import type { CancelType, InstanceSummary } from '../../api/types'
import { validateCancelReason } from '../../lib/validate'

// CancelRequestDialog：申请终止（取消）实例（契约 15.8.2）。
// 提交后本地只记「申请在途」标记，主机继续运行直到上游确认删除；已有在途申请时幂等返回。
export default function CancelRequestDialog({
  instance,
  onClose,
  onDone,
}: {
  instance: InstanceSummary
  onClose: () => void
  onDone: () => void
}) {
  const [type, setType] = useState<CancelType>('immediate')
  const [reason, setReason] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  const reasonError = validateCancelReason(reason)

  const handleConfirm = async () => {
    if (reasonError) {
      setError(reasonError)
      return
    }
    setPending(true)
    setError('')
    try {
      const result = await cancelInstance(instance.id, {
        type,
        ...(reason.trim() ? { reason: reason.trim() } : {}),
      })
      toast.success(result.message || '取消申请已提交')
      onClose()
      onDone()
    } catch (err) {
      setError(errorMessage(err, '提交取消申请失败'))
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
              <Modal.Heading>申请终止实例</Modal.Heading>
            </Modal.Header>
            <Modal.Body className="space-y-4">
              <Alert status="warning">
                <Alert.Indicator />
                <Alert.Content>
                  <Alert.Title>终止后不可恢复</Alert.Title>
                  <Alert.Description>
                    本系统通过「提交申请 → 上游处理 → 本地收敛」完成终止：提交后主机仍会运行一小段时间，
                    上游确认删除后实例转为「已终止」。终止会删除主机与数据，且本批不支持撤销申请。
                  </Alert.Description>
                </Alert.Content>
              </Alert>

              <RadioGroup
                aria-label="终止方式"
                value={type}
                onChange={(value) => setType(value as CancelType)}
                className="gap-3"
              >
                {/* HeroUI v3 的 Radio 必须用 Radio.Content 包裹才是可交互控件（否则渲染为不可选中的纯文本）；
                    圆圈（Radio.Control / Radio.Indicator）同样要放在 Radio.Content 内、文本之前。 */}
                <Radio value="immediate">
                  <Radio.Content>
                    <Radio.Control>
                      <Radio.Indicator />
                    </Radio.Control>
                    <span className="flex flex-col">
                      <span className="text-sm font-medium">立即终止</span>
                      <span className="text-xs text-muted">上游受理后尽快删除主机</span>
                    </span>
                  </Radio.Content>
                </Radio>
                <Radio value="end_of_billing">
                  <Radio.Content>
                    <Radio.Control>
                      <Radio.Indicator />
                    </Radio.Control>
                    <span className="flex flex-col">
                      <span className="text-sm font-medium">到期终止</span>
                      <span className="text-xs text-muted">到当前账单周期结束后再删除主机</span>
                    </span>
                  </Radio.Content>
                </Radio>
              </RadioGroup>

              <TextField
                name="cancel_reason"
                value={reason}
                onChange={setReason}
                isInvalid={Boolean(reason && reasonError)}
              >
                <Label>申请原因（可选）</Label>
                <Input placeholder="如：业务迁移，不再需要该主机" />
                {reason && reasonError ? (
                  <p className="mt-1 text-xs text-danger">{reasonError}</p>
                ) : null}
              </TextField>

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
              <Button variant="danger" isDisabled={pending} onPress={handleConfirm}>
                {pending ? '正在提交…' : '提交终止申请'}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  )
}
