import ConsolePlaceholder from '../../components/layout/ConsolePlaceholder'

// 会员区「通知」占位页（阶段 7b 填充）。
export default function ConsoleNotifications() {
  return (
    <ConsolePlaceholder
      title="通知"
      description="站内通知列表、已读标记与到期提醒将在阶段 7b 接入（接口：GET /notifications、GET /notifications/unread-count、POST /notifications/:id/read、POST /notifications/read-all）。"
      planned={[
        '通知列表：订单/交付/工单/到期提醒等事件',
        '未读计数与一键全部已读',
        '单条标记已读、点击跳转关联对象',
      ]}
    />
  )
}
