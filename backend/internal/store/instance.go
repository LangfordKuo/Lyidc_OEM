package store

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// InstanceFilter 是实例分页查询条件（page 从 1 开始）。
type InstanceFilter struct {
	// MemberID 为 0 表示不按会员过滤（管理端）；会员端一律传本人 ID。
	MemberID uint64
	Status   string
	Page     int
	PageSize int
}

// ListInstances 按条件分页查询实例（新建在前），返回当页数据与总数。
func (s *Store) ListInstances(ctx context.Context, filter InstanceFilter) ([]model.Instance, int64, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&model.Instance{})
	if filter.MemberID != 0 {
		query = query.Where("member_id = ?", filter.MemberID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]model.Instance, 0)
	if err := query.Session(&gorm.Session{}).
		Order("id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// InstanceByIDForMember 查询**本人**实例；他人实例与不存在的实例统一返回 ErrNotFound（对外 404）。
func (s *Store) InstanceByIDForMember(ctx context.Context, id, memberID uint64) (*model.Instance, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return instanceBy(db, "id = ? AND member_id = ?", id, memberID)
}

// InstanceByID 按本地主键查询实例（管理端；不限会员）。
func (s *Store) InstanceByID(ctx context.Context, id uint64) (*model.Instance, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return instanceBy(db, "id = ?", id)
}

// InstanceByHostID 按上游主机 ID 查询实例（交付链路用；上游主机 ID 全局唯一）。
func (s *Store) InstanceByHostID(ctx context.Context, hostID int) (*model.Instance, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return instanceBy(db, "host_id = ?", hostID)
}

// InstanceByOrderID 按订单 ID 查询实例（订单 ↔ 实例一对一；通知组装用）。
func (s *Store) InstanceByOrderID(ctx context.Context, orderID uint64) (*model.Instance, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	return instanceBy(db, "order_id = ?", orderID)
}

// InstanceSyncInput 是实例上游同步字段的落库输入（阶段 5b）。
//
// 语义：**调用方提供完整快照**（未回读到的字段由调用方保留旧值后传入），
// 本方法对提供的字段全量覆盖；空串/零值同样覆盖（用于「上游确实没有该值」的情形）。
type InstanceSyncInput struct {
	NextDueDate    *time.Time
	UpstreamStatus string
	DedicatedIP    string
	AssignedIPs    string
	Port           int
	Username       string
	Password       string
}

// UpdateInstanceSync 覆盖实例的上游同步字段并更新 updated_at（单条 UPDATE）。
func (s *Store) UpdateInstanceSync(ctx context.Context, id uint64, in InstanceSyncInput) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	result := db.Model(&model.Instance{}).Where("id = ?", id).Updates(map[string]any{
		"next_due_date":   in.NextDueDate,
		"upstream_status": in.UpstreamStatus,
		"dedicated_ip":    in.DedicatedIP,
		"assigned_ips":    in.AssignedIPs,
		"port":            in.Port,
		"username":        in.Username,
		"password":        in.Password,
		"updated_at":      time.Now().UTC(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// RowsAffected 为 0 也可能是「值完全相同」，因此回读一次以区分不存在。
		_, err := instanceBy(db, "id = ?", id)
		return err
	}
	return nil
}

// UpdateInstanceStatus 条件更新实例状态（from → to）；实例不存在返回 ErrNotFound，
// 当前状态不等于 from 返回 ErrStateConflict（不回写）。
func (s *Store) UpdateInstanceStatus(ctx context.Context, id uint64, from, to string) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	result := db.Model(&model.Instance{}).
		Where("id = ? AND status = ?", id, from).
		Updates(map[string]any{"status": to, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		current, err := instanceBy(db, "id = ?", id)
		if err != nil {
			return err
		}
		if current.Status != from {
			return ErrStateConflict
		}
	}
	return nil
}

// UpdateInstancePassword 仅更新实例密码字段（重置密码成功后落库；实例不存在返回 ErrNotFound）。
func (s *Store) UpdateInstancePassword(ctx context.Context, id uint64, password string) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	result := db.Model(&model.Instance{}).Where("id = ?", id).Updates(map[string]any{
		"password":   password,
		"updated_at": time.Now().UTC(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		_, err := instanceBy(db, "id = ?", id)
		return err
	}
	return nil
}

// InstanceCancelInput 是取消申请标记的落库输入（契约 15.8.1）。
type InstanceCancelInput struct {
	// RequestID 是上游回带的取消申请 ID（取不到时 0）。
	RequestID int
	// Type 取 model.CancelTypeImmediate / CancelTypeEndOfBilling。
	Type string
	// Reason 是申请原因（已由服务层裁剪与兜底，非空）。
	Reason string
}

// UpdateInstanceCancelRequest 写入取消申请标记（cancel_status → pending）并更新 updated_at。
//
// 条件更新保证**同一实例同一时刻只有一个申请在途**（幂等锚点）：cancel_status 已是 pending
// 时返回 ErrStateConflict（回读后按幂等分支处理）；实例不存在返回 ErrNotFound。
func (s *Store) UpdateInstanceCancelRequest(ctx context.Context, id uint64, in InstanceCancelInput) error {
	db, err := s.session(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result := db.Model(&model.Instance{}).
		Where("id = ? AND cancel_status <> ?", id, model.InstanceCancelPending).
		Updates(map[string]any{
			"cancel_request_id":   in.RequestID,
			"cancel_type":         in.Type,
			"cancel_status":       model.InstanceCancelPending,
			"cancel_reason":       in.Reason,
			"cancel_requested_at": now,
			"updated_at":          now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		current, err := instanceBy(db, "id = ?", id)
		if err != nil {
			return err
		}
		if current.CancelStatus == model.InstanceCancelPending {
			return ErrStateConflict
		}
	}
	return nil
}

// MarkInstanceTerminated 把实例收敛为 terminated（幂等）：status → terminated、cancel_status → done。
// upstreamStatus 非空时一并写入 upstream_status（如上游 domainstatus=Deleted；主机已不存在时传空串保留原值）。
// 返回 changed 表示本次是否发生了状态变更（已是 terminated 时 false 且不报错）。
func (s *Store) MarkInstanceTerminated(ctx context.Context, id uint64, upstreamStatus string) (bool, error) {
	db, err := s.session(ctx)
	if err != nil {
		return false, err
	}
	fields := map[string]any{
		"status":        model.InstanceStatusTerminated,
		"cancel_status": model.InstanceCancelDone,
		"updated_at":    time.Now().UTC(),
	}
	if upstreamStatus != "" {
		fields["upstream_status"] = upstreamStatus
	}

	result := db.Model(&model.Instance{}).
		Where("id = ? AND status <> ?", id, model.InstanceStatusTerminated).
		Updates(fields)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		// 已是 terminated（幂等）或实例不存在：回读区分。
		if _, err := instanceBy(db, "id = ?", id); err != nil {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

// ListPendingCancelInstances 返回「取消申请在途且已到收敛时机」的实例（到期扫描收敛用），
// 最多 limit 条、按申请时间升序（先到先处理）：
//
//   - immediate（立即取消）：提交后每轮扫描都尝试回读收敛（requestedAt 之前提交的）；
//   - end_of_billing（到期取消）：仅当 next_due_date 已过（上游此时才会执行终止）才尝试。
func (s *Store) ListPendingCancelInstances(ctx context.Context, dueBefore time.Time, limit int) ([]model.Instance, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.Instance, 0)
	if err := db.Where("cancel_status = ? AND cancel_requested_at IS NOT NULL AND (cancel_type = ? OR (cancel_type = ? AND next_due_date IS NOT NULL AND next_due_date < ?))",
		model.InstanceCancelPending, model.CancelTypeImmediate, model.CancelTypeEndOfBilling, dueBefore.UTC()).
		Order("cancel_requested_at ASC, id ASC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ListDueInstances 返回到期未续的 active 实例（next_due_date 非空且早于 dueBefore），
// 按 next_due_date 升序最多 limit 条（到期扫描用；调用方循环取批直到取空）。
//
// 阶段 5c 起**排除有在途取消申请的实例**（cancel_status=pending）：它们已进入终止流程
// （上游将按申请终止，到期取消的等待周期结束时由上游删除），予以暂停是错误动作；
// 其收敛由 ListPendingCancelInstances 的扫描分支负责（契约 15.8.5）。
func (s *Store) ListDueInstances(ctx context.Context, dueBefore time.Time, limit int) ([]model.Instance, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.Instance, 0)
	if err := db.Where("status = ? AND cancel_status <> ? AND next_due_date IS NOT NULL AND next_due_date < ?",
		model.InstanceStatusActive, model.InstanceCancelPending, dueBefore.UTC()).
		Order("next_due_date ASC, id ASC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ListExpiringInstances 返回「即将到期且尚未提醒过」的 active 实例（到期提醒扫描用，契约 17.5）：
// dueAfter < next_due_date <= dueBefore，且 next_due_date 与去重锚点 expiry_reminded_due 不相等
// （相等 = 本到期周期已提醒过；续费/同步推进到期时间后两者不再相等，提醒自动重新武装）。
//
// 排除 cancel_status=pending 的实例（已进入终止流程，提醒续费是错误动作），
// 也排除尚无到期时间的实例（上游未回读到）。按 next_due_date 升序最多 limit 条。
func (s *Store) ListExpiringInstances(ctx context.Context, dueAfter, dueBefore time.Time, limit int) ([]model.Instance, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.Instance, 0)
	if err := db.Where("status = ? AND cancel_status <> ? AND next_due_date IS NOT NULL AND next_due_date > ? AND next_due_date <= ? "+
		"AND (expiry_reminded_due IS NULL OR expiry_reminded_due <> next_due_date)",
		model.InstanceStatusActive, model.InstanceCancelPending, dueAfter.UTC(), dueBefore.UTC()).
		Order("next_due_date ASC, id ASC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ClaimExpiryReminder 认领「本到期周期的一次到期提醒」：把去重锚点写成当前的 next_due_date。
//
// 条件更新（锚点不等于当前到期时间）保证同一周期只有一次认领成功：并发/重复扫描时
// 只有一行被更新（RowsAffected=1），其余返回 false（本次不提醒）。
// 实例不存在返回 ErrNotFound；到期时间尚未回读（NULL）时同样不认领（返回 false）。
func (s *Store) ClaimExpiryReminder(ctx context.Context, id uint64) (bool, error) {
	db, err := s.session(ctx)
	if err != nil {
		return false, err
	}
	result := db.Exec("UPDATE instances SET expiry_reminded_due = next_due_date, updated_at = ? "+
		"WHERE id = ? AND next_due_date IS NOT NULL AND (expiry_reminded_due IS NULL OR expiry_reminded_due <> next_due_date)",
		time.Now().UTC(), id)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, nil
	}
	// 未被认领：区分「实例不存在」（报错）与「本周期已提醒过」（正常的幂等结果）。
	if _, err := instanceBy(db, "id = ?", id); err != nil {
		return false, err
	}
	return false, nil
}

// InstancesByIDs 按主键批量查询实例（工单视图组装 instance 概要、校验归属用）；
// ids 为空时返回空切片（不发起查询）。**只用于概要展示**：调用方不得把敏感字段放进列表视图。
func (s *Store) InstancesByIDs(ctx context.Context, ids []uint64) ([]model.Instance, error) {
	db, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]model.Instance, 0)
	if len(ids) == 0 {
		return items, nil
	}
	if err := db.Where("id IN ?", ids).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// instanceBy 是内部按条件查询单条实例。
func instanceBy(db *gorm.DB, query string, args ...any) (*model.Instance, error) {
	var instance model.Instance
	if err := db.Where(query, args...).Take(&instance).Error; err != nil {
		return nil, notFoundIfNeeded(err)
	}
	return &instance, nil
}
