import { Button, Modal } from '@heroui/react'
import type { ReactNode } from 'react'

// ConfirmDialog 是危险/关键操作的二次确认弹窗（HeroUI 弹窗，禁用原生 confirm）。
// 用例：电源与硬操作、重装、改密提交、续费、取消申请、取消订单、关闭工单等。
export interface ConfirmDialogProps {
  isOpen: boolean
  title: string
  description?: ReactNode
  /** 弹窗正文中的补充内容（如密码输入、系统选择等表单控件）。 */
  children?: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  /** 危险操作（红色按钮），默认 false。 */
  isDanger?: boolean
  /** 提交中：按钮禁用并显示「处理中…」，避免重复点击。 */
  pending?: boolean
  confirmDisabled?: boolean
  onConfirm: () => void
  onCancel: () => void
}

export default function ConfirmDialog({
  isOpen,
  title,
  description,
  children,
  confirmLabel = '确认',
  cancelLabel = '取消',
  isDanger = false,
  pending = false,
  confirmDisabled = false,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  return (
    <Modal
      isOpen={isOpen}
      onOpenChange={(open) => {
        if (!open && !pending) {
          onCancel()
        }
      }}
    >
      <Modal.Backdrop>
        <Modal.Container size="md" placement="center">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>{title}</Modal.Heading>
            </Modal.Header>
            <Modal.Body className="space-y-3">
              {description ? <div className="text-sm text-muted">{description}</div> : null}
              {children}
            </Modal.Body>
            <Modal.Footer>
              <Button variant="outline" isDisabled={pending} onPress={onCancel}>
                {cancelLabel}
              </Button>
              <Button
                variant={isDanger ? 'danger' : 'primary'}
                isDisabled={pending || confirmDisabled}
                onPress={onConfirm}
              >
                {pending ? '处理中…' : confirmLabel}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  )
}
