package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// deliverRenew 执行一次续费交付（认领后的完整链路，契约 15.4）：
//
//	① 实例校验（订单必须带 instance_id 且实例存在）；
//	② 上游续费：RenewHost（host/renew + 余额支付）；
//	③ 回读 hostinfo（all=1）：到期时间 / domainstatus / IP / 端口 / 账号密码；
//	④ 若实例本地为 suspended 或上游 domainstatus 为 Suspended → 尝试 Unsuspend
//	   （**失败不阻断续费成交**，实例状态保持 suspended，由管理员排查）；
//	⑤ 单事务落库：更新实例同步字段与状态 + 订单置 active（doc_id 与 5a 同骨架）。
//
// 幂等：入口的 BeginDelivery 行锁认领保证重复回调/重复触发不重复续费。
// 回读失败不视为续费失败（上游已扣费），到期时间保留旧值并在审计中标注。
func (s *Service) deliverRenew(ctx context.Context, order *model.Order) (*model.Order, error) {
	if order.InstanceID == nil || *order.InstanceID == 0 {
		return s.failOrder(order, "续费订单缺少实例关联（instance_id），无法续费")
	}

	instance, err := s.store.InstanceByID(ctx, *order.InstanceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return s.failOrder(order, fmt.Sprintf("续费订单关联的实例 %d 不存在", *order.InstanceID))
		}
		return nil, err
	}
	if instance.MemberID != order.MemberID {
		return s.failOrder(order, "续费订单与实例的会员归属不一致")
	}

	client, err := s.client(ctx)
	if err != nil {
		return s.failOrder(order, err.Error())
	}

	cycle := pricing.UpstreamCycle(order.Cycle)
	if cycle == "" {
		return s.failOrder(order, "订单周期无法映射到上游计费周期："+order.Cycle)
	}

	s.logger.Info("开始续费交付", "order_id", order.ID, "trade_no", order.TradeNo,
		"instance_id", instance.ID, "host_id", instance.HostID, "cycle", order.Cycle)

	if _, err := client.RenewHost(ctx, instance.HostID, cycle); err != nil {
		return s.failOrder(order, "上游续费失败："+err.Error())
	}

	// 回读最新上游字段；失败时保留旧值（续费已成交，不因回读失败置 failed）。
	sync, readbackOK := s.readbackForRenew(ctx, client, instance)

	// 续费成功后恢复实例可用性（两条路径，均不阻断续费成交）：
	//   ① 回读显示上游已是 Active（实测：上游在续费成功后会自行解除到期暂停）→
	//      本地直接收敛为 active，**不再调 Unsuspend**（此时调上游会以「不能解除该暂停」被拒）；
	//   ② 上游仍为 Suspended（或回读失败且本地 suspended）→ 尝试 Unsuspend，失败则保持 suspended。
	status := instance.Status
	switch {
	case strings.EqualFold(strings.TrimSpace(sync.UpstreamStatus), "Active"):
		if instance.Status == model.InstanceStatusSuspended {
			status = model.InstanceStatusActive
			s.audit(ctx, instance.ID, model.ActionUnsuspend, model.InstanceOpSuccess,
				"续费成功，上游已自行解除暂停，本地状态已收敛为 active")
		}
	case needsResume(instance.Status, sync.UpstreamStatus):
		if _, err := client.Unsuspend(ctx, instance.HostID); err != nil {
			s.logger.Warn("续费成功但恢复主机失败（不阻断续费成交，可稍后手动恢复）",
				"error", err, "order_id", order.ID, "instance_id", instance.ID, "host_id", instance.HostID)
			s.audit(ctx, instance.ID, model.ActionUnsuspend, model.InstanceOpFail, "续费后自动恢复失败："+err.Error())
		} else {
			status = model.InstanceStatusActive
			sync.UpstreamStatus = "Active"
			s.audit(ctx, instance.ID, model.ActionUnsuspend, model.InstanceOpSuccess, "续费成功，已解除上游暂停")
		}
	}

	message := "续费成功"
	if !readbackOK {
		message = "续费成功（上游回读失败，到期时间保留原值，可用同步接口核对）"
	}
	if status == model.InstanceStatusSuspended {
		message += "；主机仍处于暂停状态，请管理员核查"
	}

	updated, completeErr := s.completeRenew(order.ID, instance.ID, sync, status)
	if completeErr != nil {
		s.logger.Error("续费落库失败（上游已续费，订单停在 provisioning 需人工核对）",
			"error", completeErr, "order_id", order.ID, "trade_no", order.TradeNo,
			"instance_id", instance.ID, "host_id", instance.HostID)
		return nil, completeErr
	}

	s.audit(ctx, instance.ID, model.ActionRenew, model.InstanceOpSuccess, message)
	s.logger.Info("续费交付完成", "order_id", updated.ID, "trade_no", updated.TradeNo,
		"instance_id", instance.ID, "host_id", instance.HostID, "cycle", order.Cycle)
	return updated, nil
}

// readbackForRenew 回读上游主机字段并合并为落库输入：回读失败时保留实例旧值，
// 返回 ok=false 供调用方在审计中标注。
func (s *Service) readbackForRenew(ctx context.Context, client *upstream.Client, instance *model.Instance) (store.InstanceSyncInput, bool) {
	sync := store.InstanceSyncInput{
		NextDueDate:    instance.NextDueDate,
		UpstreamStatus: instance.UpstreamStatus,
		DedicatedIP:    instance.DedicatedIP,
		AssignedIPs:    instance.AssignedIPs,
		Port:           instance.Port,
		Username:       instance.Username,
		Password:       instance.Password,
	}

	host, err := client.Host(ctx, instance.HostID)
	if err != nil {
		s.logger.Warn("续费成功但回读上游主机信息失败（实例同步字段保留原值）",
			"error", err, "instance_id", instance.ID, "host_id", instance.HostID)
		return sync, false
	}

	sync.NextDueDate = DueDateOf(host)
	sync.UpstreamStatus = host.DomainStatus
	sync.DedicatedIP = host.DedicatedIP
	sync.AssignedIPs = strings.Join(nonEmpty(host.AssignedIPs), ",")
	sync.Port = host.Port
	if host.Username != "" {
		sync.Username = host.Username
	}
	if host.Password != "" {
		sync.Password = host.Password
	}
	return sync, true
}

// needsResume 判断续费后是否需要尝试解除暂停：本地已暂停，或上游 domainstatus 为 Suspended。
func needsResume(localStatus, upstreamStatus string) bool {
	if localStatus == model.InstanceStatusSuspended {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(upstreamStatus), "Suspended")
}

// completeRenew 用独立超时落库续费结果（不随调用方 ctx 取消而丢失）。
func (s *Service) completeRenew(orderID, instanceID uint64, sync store.InstanceSyncInput, status string) (*model.Order, error) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	return s.store.CompleteRenewDelivery(ctx, orderID, store.RenewDeliveryInput{
		InstanceID: instanceID,
		Sync:       sync,
		Status:     status,
	})
}

// failOrder 记录交付失败（订单 → failed + 脱敏原因）并返回与 5a 一致的错误。
func (s *Service) failOrder(order *model.Order, reason string) (*model.Order, error) {
	failed, failErr := s.fail(order.ID, reason)
	if failErr != nil {
		return nil, failErr
	}
	s.logger.Error("订单交付失败", "error", reason, "order_id", order.ID, "trade_no", order.TradeNo)
	return failed, fmt.Errorf("%w: %s", ErrProvisionFailed, reason)
}

// audit 写一条实例操作审计（系统自动操作，actor=system）。写失败只记日志，不影响业务结果。
func (s *Service) audit(ctx context.Context, instanceID uint64, action, status, message string) {
	if err := s.store.AppendInstanceLog(ctx, store.InstanceLogInput{
		InstanceID: instanceID,
		ActorType:  model.ActorTypeSystem,
		Action:     action,
		Status:     status,
		Message:    message,
	}); err != nil {
		s.logger.Warn("实例操作审计写入失败", "error", err, "instance_id", instanceID, "action", action)
	}
}

// client 返回当前设置下的上游客户端（未配置/未启用时返回明确错误）。
func (s *Service) client(ctx context.Context) (*upstream.Client, error) {
	client, enabled, err := s.upstream.Current(ctx)
	if err != nil {
		if errors.Is(err, upstream.ErrNotConfigured) {
			return nil, errors.New("上游未配置或未启用（请管理员在后台设置中填写并启用）")
		}
		return nil, fmt.Errorf("读取上游设置失败：%v", err)
	}
	if !enabled {
		return nil, errors.New("上游未配置或未启用（请管理员在后台设置中填写并启用）")
	}
	return client, nil
}
