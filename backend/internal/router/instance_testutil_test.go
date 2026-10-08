package router

import (
	"net/http"
	"testing"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// ---------------------------------------------------------------------------
// 阶段 5b 测试辅助：实例开通、状态构造、审计读取
// ---------------------------------------------------------------------------

// newInstanceFor 走 5a 开通链路交付一台实例（下单 → 在线支付 → 自动交付），返回实例。
func newInstanceFor(t *testing.T, engine http.Handler, gateway *fakeEpayGateway, gdb *gorm.DB,
	token string, productID uint64) *model.Instance {
	t.Helper()
	order := createOrder(t, engine, token, map[string]any{
		"product_id": productID, "cycle": "monthly", "config": map[string]any{},
	})
	updated := payOrderByEpayNotify(t, engine, gateway, token, order)
	if updated.Status != model.OrderStatusActive {
		t.Fatalf("开通链路未交付成功: status=%s provision_error=%q", updated.Status, updated.ProvisionError)
	}
	return instanceByOrderID(t, gdb, order.ID)
}

// setInstanceStatus 直接改库设置实例状态（构造 suspended / terminated 等前置条件）。
func setInstanceStatus(t *testing.T, gdb *gorm.DB, instanceID uint64, status string) {
	t.Helper()
	if err := gdb.Model(&model.Instance{}).Where("id = ?", instanceID).
		Update("status", status).Error; err != nil {
		t.Fatalf("更新实例状态失败: %v", err)
	}
}

// setInstanceDue 直接改库设置实例到期时间（构造到期场景）。
func setInstanceDue(t *testing.T, gdb *gorm.DB, instanceID uint64, due any) {
	t.Helper()
	if err := gdb.Model(&model.Instance{}).Where("id = ?", instanceID).
		Update("next_due_date", due).Error; err != nil {
		t.Fatalf("更新实例到期时间失败: %v", err)
	}
}

// instanceFromDB 读库取实例（管理端视角）。
func instanceFromDB(t *testing.T, gdb *gorm.DB, instanceID uint64) *model.Instance {
	t.Helper()
	var instance model.Instance
	if err := gdb.First(&instance, instanceID).Error; err != nil {
		t.Fatalf("查询实例失败: %v", err)
	}
	return &instance
}

// instanceLogsFromDB 读库取实例的操作记录（新记录在前）。
func instanceLogsFromDB(t *testing.T, gdb *gorm.DB, instanceID uint64) []model.InstanceOperationLog {
	t.Helper()
	var logs []model.InstanceOperationLog
	if err := gdb.Where("instance_id = ?", instanceID).Order("id DESC").Find(&logs).Error; err != nil {
		t.Fatalf("查询实例操作记录失败: %v", err)
	}
	return logs
}

// lastInstanceLog 取最近一条实例操作记录（不存在时 Fatal）。
func lastInstanceLog(t *testing.T, gdb *gorm.DB, instanceID uint64) model.InstanceOperationLog {
	t.Helper()
	logs := instanceLogsFromDB(t, gdb, instanceID)
	if len(logs) == 0 {
		t.Fatalf("实例 %d 没有任何操作记录", instanceID)
	}
	return logs[0]
}
