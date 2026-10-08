package store

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// maxProvisionErrorRunes 对应 orders.provision_error 的 VARCHAR(512)（按字符截断，留足余量）。
const maxProvisionErrorRunes = 500

// InstanceInput 是交付成功时写入 instances 的落库字段
// （订单快照取自订单行，上游同步字段来自开通后的 hostinfo 回读）。
type InstanceInput struct {
	HostID         int
	ProductID      uint64
	ProductName    string
	Name           string
	BillingCycle   string
	NextDueDate    *time.Time
	UpstreamStatus string
	DedicatedIP    string
	AssignedIPs    string
	Port           int
	Username       string
	Password       string
}

// BeginDelivery 认领订单的交付权（幂等与防并发的唯一入口）：
//
//	事务内行锁订单 → 状态为 paid（allowFailed 为 true 时失败态 failed 也算）时
//	置 provisioning、清空 provision_error，返回 claimed=true；
//	其余状态（provisioning / active / pending / cancelled，或 failed 且不允许重试）
//	一律不修改，返回 claimed=false 与最新订单（调用方按状态给出提示）。
//
// 重复回调、重复触发、管理员重试并发时，只有一个调用能拿到 claimed=true。
func (s *Store) BeginDelivery(ctx context.Context, orderID uint64, allowFailed bool) (bool, *model.Order, error) {
	db, err := s.session(ctx)
	if err != nil {
		return false, nil, err
	}

	claimed := false
	var order *model.Order
	err = db.Transaction(func(tx *gorm.DB) error {
		current, err := lockOrder(tx, "id = ?", orderID)
		if err != nil {
			return err
		}
		order = current

		switch current.Status {
		case model.OrderStatusPaid:
		case model.OrderStatusFailed:
			if !allowFailed {
				return nil
			}
		default:
			return nil
		}

		now := time.Now().UTC()
		if err := tx.Model(&model.Order{}).Where("id = ?", current.ID).Updates(map[string]any{
			"status":          model.OrderStatusProvisioning,
			"provision_error": "",
			"updated_at":      now,
		}).Error; err != nil {
			return err
		}
		current.Status = model.OrderStatusProvisioning
		current.ProvisionError = ""
		current.UpdatedAt = now
		claimed = true
		return nil
	})
	if err != nil {
		return false, nil, err
	}
	return claimed, order, nil
}

// CompleteDelivery 交付成功落库：单事务内锁订单 → 插入实例 → 订单置 active
// （写 host_id / delivered_at、清空 provision_error）。
//
// 订单不处于 provisioning 时返回 ErrStateConflict 且不写任何数据
// （状态被外力改动的极端场景：宁可留痕人工核对，也不静默覆盖）。
func (s *Store) CompleteDelivery(ctx context.Context, orderID uint64, in InstanceInput) (*model.Order, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	var order *model.Order
	err = db.Transaction(func(tx *gorm.DB) error {
		current, err := lockOrder(tx, "id = ?", orderID)
		if err != nil {
			return err
		}
		if current.Status != model.OrderStatusProvisioning {
			order = current
			return ErrStateConflict
		}

		now := time.Now().UTC()
		instance := model.Instance{
			MemberID:       current.MemberID,
			OrderID:        current.ID,
			HostID:         in.HostID,
			ProductID:      in.ProductID,
			ProductName:    in.ProductName,
			Name:           in.Name,
			BillingCycle:   in.BillingCycle,
			NextDueDate:    in.NextDueDate,
			Status:         model.InstanceStatusActive,
			UpstreamStatus: in.UpstreamStatus,
			DedicatedIP:    in.DedicatedIP,
			AssignedIPs:    in.AssignedIPs,
			Port:           in.Port,
			Username:       in.Username,
			Password:       in.Password,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := tx.Create(&instance).Error; err != nil {
			return mapDuplicateError(err)
		}

		hostID := in.HostID
		if err := tx.Model(&model.Order{}).Where("id = ?", current.ID).Updates(map[string]any{
			"status":          model.OrderStatusActive,
			"host_id":         hostID,
			"delivered_at":    now,
			"provision_error": "",
			"updated_at":      now,
		}).Error; err != nil {
			return err
		}

		current.Status = model.OrderStatusActive
		current.HostID = &hostID
		current.DeliveredAt = &now
		current.ProvisionError = ""
		current.UpdatedAt = now
		order = current
		return nil
	})
	if err != nil {
		return nil, err
	}
	return order, nil
}

// FailDelivery 把交付中的订单置 failed 并记录失败原因（按字符截断到列宽内）。
//
// 条件更新（仅 provisioning 可转 failed）+ 回读：订单已不在 provisioning
// （如被并发重试改回 provisioning）时不覆盖状态，返回最新记录。
func (s *Store) FailDelivery(ctx context.Context, orderID uint64, reason string) (*model.Order, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if err := db.Model(&model.Order{}).
		Where("id = ? AND status = ?", orderID, model.OrderStatusProvisioning).
		Updates(map[string]any{
			"status":          model.OrderStatusFailed,
			"provision_error": truncateRunes(reason, maxProvisionErrorRunes),
			"updated_at":      now,
		}).Error; err != nil {
		return nil, err
	}
	return orderBy(db, "id = ?", orderID)
}

// truncateRunes 按字符（rune）截断字符串（中文按 1 个字符计）。
func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
