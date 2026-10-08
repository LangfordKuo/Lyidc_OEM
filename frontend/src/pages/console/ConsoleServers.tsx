import ConsolePlaceholder from '../../components/layout/ConsolePlaceholder'

// 会员区「我的服务器」占位页（阶段 7b 填充）。
export default function ConsoleServers() {
  return (
    <ConsolePlaceholder
      title="我的服务器"
      description="实例列表与详情、开关机/重装/改密/续费等操作将在阶段 7b 接入。后端接口已就绪（契约第 15 节）。"
      planned={[
        '实例列表：状态、到期时间、配置与主机信息',
        '实例操作：开机 / 关机 / 重启 / 重装系统 / 重置密码',
        '续费下单与到期提醒入口',
        '取消申请（退款终止）与操作日志查看',
      ]}
    />
  )
}
