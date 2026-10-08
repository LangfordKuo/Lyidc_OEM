import { Alert, Button, Modal, Radio, RadioGroup, toast } from '@heroui/react'
import { useState } from 'react'

import { errorMessage } from '../../api/client'
import { fetchReinstallOptions, reinstallInstance } from '../../api/instances'
import type { InstanceSummary } from '../../api/types'
import { useAsync } from '../../hooks/useAsync'
import { LoadingBlock, ErrorState } from '../common/PageState'

// ReinstallDialog：选择系统 + 二次确认后发起重装（契约 15.2）。
// 可选系统在打开时按需拉取；上游为异步执行，返回成功仅表示已受理。
export default function ReinstallDialog({
  instance,
  onClose,
  onDone,
}: {
  instance: InstanceSummary
  onClose: () => void
  onDone: () => void
}) {
  const [osId, setOsId] = useState<string>('')
  const [pending, setPending] = useState(false)

  const optionsState = useAsync(() => fetchReinstallOptions(instance.id), [instance.id])
  const options = optionsState.data?.os ?? []

  const selected = options.find((item) => String(item.id) === osId) ?? null

  const handleConfirm = async () => {
    if (!selected) {
      return
    }
    setPending(true)
    try {
      const result = await reinstallInstance(instance.id, { os_id: selected.id })
      toast.success(result.message || '重装指令已提交')
      onClose()
      onDone()
    } catch (error) {
      toast.danger(errorMessage(error, '重装失败'))
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
        <Modal.Container size="lg" placement="center">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>重装系统</Modal.Heading>
            </Modal.Header>
            <Modal.Body className="space-y-4">
              <Alert status="danger">
                <Alert.Indicator />
                <Alert.Content>
                  <Alert.Title>重装会清空系统盘数据</Alert.Title>
                  <Alert.Description>
                    重装将格式化系统盘并安装所选系统，操作不可撤销；请先确认已备份重要数据。
                  </Alert.Description>
                </Alert.Content>
              </Alert>

              {optionsState.loading ? <LoadingBlock label="正在读取可选系统…" /> : null}
              {optionsState.error ? (
                <ErrorState message={optionsState.error} onRetry={optionsState.reload} />
              ) : null}

              {!optionsState.loading && !optionsState.error && options.length === 0 ? (
                <p className="text-sm text-muted">该商品未配置可选操作系统，请联系客服。</p>
              ) : null}

              {options.length > 0 ? (
                <RadioGroup
                  aria-label="选择操作系统"
                  value={osId}
                  onChange={(value) => setOsId(String(value))}
                >
                  <div className="max-h-72 space-y-2 overflow-y-auto pr-1">
                    {options.map((option) => (
                      // HeroUI v3 的 Radio 必须用 Radio.Content 包裹才是可交互控件（否则渲染为不可选中的纯文本），
                      // 圆圈（Radio.Control / Radio.Indicator）也要放在 Radio.Content 内、文本之前。
                      <Radio key={option.id} value={String(option.id)}>
                        <Radio.Content>
                          <Radio.Control>
                            <Radio.Indicator />
                          </Radio.Control>
                          <span className="flex flex-col">
                            <span className="text-sm">{option.name}</span>
                            {option.group ? (
                              <span className="text-xs text-muted">{option.group}</span>
                            ) : null}
                          </span>
                        </Radio.Content>
                      </Radio>
                    ))}
                  </div>
                </RadioGroup>
              ) : null}
            </Modal.Body>
            <Modal.Footer>
              <Button variant="outline" isDisabled={pending} onPress={onClose}>
                取消
              </Button>
              <Button
                variant="danger"
                isDisabled={pending || !selected}
                onPress={handleConfirm}
              >
                {pending ? '正在提交…' : '确认重装'}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  )
}
