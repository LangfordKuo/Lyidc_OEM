import ConsolePlaceholder from '../../components/layout/ConsolePlaceholder'

// 会员区「工单」占位页（阶段 7b 填充）。
export default function ConsoleTickets() {
  return (
    <ConsolePlaceholder
      title="工单"
      description="提单、回复与关闭将在阶段 7b 接入（接口：POST /tickets、GET /tickets、GET /tickets/:id、POST /tickets/:id/reply、POST /tickets/:id/close）。"
      planned={[
        '工单列表：状态（待处理 / 处理中 / 已关闭）与最后回复时间',
        '提交工单：分类、标题、正文',
        '工单详情：与客服的往来消息、追加回复、关闭工单',
      ]}
    />
  )
}
