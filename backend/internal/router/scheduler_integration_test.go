package router

import (
	"context"
	"net/http"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/scheduler"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// ---------------------------------------------------------------------------
// 阶段 5b：到期暂停扫描（clock 注入 + 手动 ScanOnce 的确定性集成测试）
// ---------------------------------------------------------------------------

// newTestScanner 构造可注入 clock 的扫描器（不经 router，直接驱动 ScanOnce）。
func newTestScanner(t *testing.T, gdb *gorm.DB, host *fakeHostServer, now time.Time) *scheduler.Scanner {
	t.Helper()
	return scheduler.New(store.New(gdb), upstream.StaticProvider{C: host.client(t)}, scheduler.Options{
		Clock:  func() time.Time { return now },
		Logger: silentLogger(),
	})
}

// TestDueScanSuspendsExpiredInstances 验证：到期实例被暂停（上游 + 本地 + 审计），未到期实例不受影响。
func TestDueScanSuspendsExpiredInstances(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "scanuser")
	expired := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	healthy := newInstanceFor(t, engine, gateway, gdb, token, product.ID)

	now := time.Now().UTC()
	setInstanceDue(t, gdb, expired.ID, now.Add(-24*time.Hour))
	setInstanceDue(t, gdb, healthy.ID, now.Add(72*time.Hour))

	scanner := newTestScanner(t, gdb, host, now)
	report, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if report.Scanned != 1 || report.Suspended != 1 || report.Failed != 0 {
		t.Fatalf("扫描统计 = %+v，期望 scanned=1 suspended=1 failed=0", report)
	}

	// 到期实例：上游收到 suspend（带原因），本地 suspended，审计 actor=system。
	form := host.lastForm(pathProvisionDef)
	if form.Get("func") != "suspend" || form.Get("reason") == "" {
		t.Fatalf("上游暂停参数错误: func=%q reason=%q", form.Get("func"), form.Get("reason"))
	}
	if got := instanceFromDB(t, gdb, expired.ID); got.Status != model.InstanceStatusSuspended {
		t.Fatalf("到期实例状态 = %s，期望 suspended", got.Status)
	}
	last := lastInstanceLog(t, gdb, expired.ID)
	if last.Action != model.ActionSuspend || last.Status != model.InstanceOpSuccess || last.ActorType != model.ActorTypeSystem {
		t.Fatalf("到期暂停审计异常: %+v", last)
	}

	// 未到期实例不受影响。
	if got := instanceFromDB(t, gdb, healthy.ID); got.Status != model.InstanceStatusActive {
		t.Fatalf("未到期实例状态 = %s，期望 active", got.Status)
	}
	if logs := instanceLogsFromDB(t, gdb, healthy.ID); len(logs) != 1 { // 仅开通审计
		t.Fatalf("未到期实例不应有暂停审计: %+v", logs)
	}

	// 幂等：再次扫描不再命中（已 suspended）。
	report, err = scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("第二轮扫描失败: %v", err)
	}
	if report.Scanned != 0 {
		t.Fatalf("第二轮扫描应无到期实例: %+v", report)
	}
}

// TestDueScanIdempotentWhenUpstreamSuspended 验证上游已暂停时的幂等处理：
// Suspend 调用失败 + 回读 domainstatus=Suspended → 只收敛本地状态并记 success。
func TestDueScanIdempotentWhenUpstreamSuspended(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "scanidem")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)

	now := time.Now().UTC()
	setInstanceDue(t, gdb, instance.ID, now.Add(-time.Hour))
	// 模拟「上游已自行暂停」：suspend 再次调用被上游拒绝，回读为 Suspended。
	host.setProvisionFail("suspend", "主机已是暂停状态")
	host.mu.Lock()
	host.hosts[instance.HostID].suspended = true
	host.mu.Unlock()

	scanner := newTestScanner(t, gdb, host, now)
	report, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if report.Suspended != 1 || report.Failed != 0 {
		t.Fatalf("幂等路径应记为成功: %+v", report)
	}
	if got := instanceFromDB(t, gdb, instance.ID); got.Status != model.InstanceStatusSuspended {
		t.Fatalf("本地状态 = %s，期望 suspended", got.Status)
	}
	if last := lastInstanceLog(t, gdb, instance.ID); last.Status != model.InstanceOpSuccess {
		t.Fatalf("幂等路径应审计 success: %+v", last)
	}
}

// TestDueScanFailureRetries 验证暂停失败留痕并保持 active（下一轮重试可成功）。
func TestDueScanFailureRetries(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "scanfail")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)

	now := time.Now().UTC()
	setInstanceDue(t, gdb, instance.ID, now.Add(-time.Hour))
	host.setProvisionFail("suspend", "上游暂时不可用")

	scanner := newTestScanner(t, gdb, host, now)
	report, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("扫描不应因单实例失败而中断: %v", err)
	}
	if report.Failed != 1 || report.Suspended != 0 {
		t.Fatalf("失败统计 = %+v，期望 failed=1", report)
	}
	if got := instanceFromDB(t, gdb, instance.ID); got.Status != model.InstanceStatusActive {
		t.Fatalf("失败后本地状态 = %s，期望保持 active（留待下轮重试）", got.Status)
	}
	if last := lastInstanceLog(t, gdb, instance.ID); last.Status != model.InstanceOpFail {
		t.Fatalf("失败应审计 fail: %+v", last)
	}

	// 上游恢复 → 下一轮成功。
	host.setProvisionFail("suspend", "")
	report, err = scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("重试扫描失败: %v", err)
	}
	if report.Suspended != 1 {
		t.Fatalf("重试后统计 = %+v，期望 suspended=1", report)
	}
	if got := instanceFromDB(t, gdb, instance.ID); got.Status != model.InstanceStatusSuspended {
		t.Fatalf("重试后本地状态 = %s，期望 suspended", got.Status)
	}
}

// TestDueScanSkipsWhenUpstreamUnconfigured 验证上游未配置时扫描直接返回错误（不误停实例）。
func TestDueScanSkipsWhenUpstreamUnconfigured(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "scannoup")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	now := time.Now().UTC()
	setInstanceDue(t, gdb, instance.ID, now.Add(-time.Hour))

	scanner := scheduler.New(store.New(gdb), upstream.StaticProvider{C: nil}, scheduler.Options{
		Clock:  func() time.Time { return now },
		Logger: silentLogger(),
	})
	if _, err := scanner.ScanOnce(context.Background()); err == nil {
		t.Fatal("上游未配置时扫描应返回错误")
	}
	if got := instanceFromDB(t, gdb, instance.ID); got.Status != model.InstanceStatusActive {
		t.Fatalf("上游不可用时不应改动实例状态: %s", got.Status)
	}
}

// TestDueScanBatches 验证单批数量限制下的循环扫描（一次 ScanOnce 处理多批到期实例）。
func TestDueScanBatches(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "scanbatch")
	now := time.Now().UTC()
	instances := make([]*model.Instance, 0, 3)
	for i := 0; i < 3; i++ {
		instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
		setInstanceDue(t, gdb, instance.ID, now.Add(-time.Duration(i+1)*time.Hour))
		instances = append(instances, instance)
	}

	scanner := scheduler.New(store.New(gdb), upstream.StaticProvider{C: host.client(t)}, scheduler.Options{
		BatchSize: 2, // 3 条到期：分两批（2 + 1）
		Clock:     func() time.Time { return now },
		Logger:    silentLogger(),
	})
	report, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("分批扫描失败: %v", err)
	}
	if report.Scanned != 3 || report.Suspended != 3 {
		t.Fatalf("分批扫描统计 = %+v，期望 3/3", report)
	}
	for _, instance := range instances {
		if got := instanceFromDB(t, gdb, instance.ID); got.Status != model.InstanceStatusSuspended {
			t.Fatalf("实例 %d 状态 = %s，期望 suspended", instance.ID, got.Status)
		}
	}
}

// ---------------------------------------------------------------------------
// 阶段 5c：取消申请收敛扫描（与到期暂停扫描同一轮）
// ---------------------------------------------------------------------------

// TestScanConvergesPendingCancels 验证取消申请收敛：上游已删除/已终止 → 本地 terminated；
// 仍在途（上游未处理）与未到时机的到期取消不动；有在途申请的到期实例不被暂停。
func TestScanConvergesPendingCancels(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "scancancel")
	now := time.Now().UTC()

	// A：立即取消在途、上游已删除（且已过到期时间——不得被暂停，应被收敛）。
	converged := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	setInstanceCancel(t, gdb, converged.ID, model.CancelTypeImmediate, now.Add(-2*time.Hour))
	setInstanceDue(t, gdb, converged.ID, now.Add(-time.Hour))
	host.deleteHost(converged.HostID)

	// B：立即取消在途、上游仍 Active（申请尚未被上游处理）→ 保持原状、无新增审计。
	inFlight := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	setInstanceCancel(t, gdb, inFlight.ID, model.CancelTypeImmediate, now.Add(-time.Hour))

	// C：到期取消在途、到期时间已过、上游已删除 → 收敛。
	expiredEOB := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	setInstanceCancel(t, gdb, expiredEOB.ID, model.CancelTypeEndOfBilling, now.Add(-30*24*time.Hour))
	setInstanceDue(t, gdb, expiredEOB.ID, now.Add(-time.Hour))
	host.deleteHost(expiredEOB.HostID)

	// D：到期取消在途、尚未到期（上游此时不会终止）→ 本轮不收敛（不浪费回读，也不暂停）。
	futureEOB := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	setInstanceCancel(t, gdb, futureEOB.ID, model.CancelTypeEndOfBilling, now.Add(-time.Hour))
	setInstanceDue(t, gdb, futureEOB.ID, now.Add(72*time.Hour))
	host.deleteHost(futureEOB.HostID)

	// E：无取消申请的到期实例 → 仍按到期扫描暂停。
	plain := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	setInstanceDue(t, gdb, plain.ID, now.Add(-time.Hour))

	scanner := newTestScanner(t, gdb, host, now)
	report, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if report.CancelScanned != 3 || report.CancelConverged != 2 || report.CancelFailed != 0 {
		t.Fatalf("收敛统计 = %+v，期望 cancel_scanned=3 converged=2 failed=0（D 未到时机不扫）", report)
	}
	if report.Scanned != 1 || report.Suspended != 1 {
		t.Fatalf("到期暂停统计 = %+v，期望仅 E 被暂停（A/C 有在途申请不暂停）", report)
	}

	for _, tc := range []struct {
		name     string
		instance *model.Instance
		want     string
	}{
		{"上游已删除（立即取消）", converged, model.InstanceStatusTerminated},
		{"上游已删除（到期取消且已到期）", expiredEOB, model.InstanceStatusTerminated},
		{"上游未处理（仍在途）", inFlight, model.InstanceStatusActive},
		{"到期取消未到到期时间", futureEOB, model.InstanceStatusActive},
		{"无取消申请", plain, model.InstanceStatusSuspended},
	} {
		if got := instanceFromDB(t, gdb, tc.instance.ID); got.Status != tc.want {
			t.Fatalf("%s：状态 = %s，期望 %s", tc.name, got.Status, tc.want)
		}
	}
	if got := instanceFromDB(t, gdb, converged.ID); got.CancelStatus != model.InstanceCancelDone {
		t.Fatalf("收敛后 cancel_status = %s，期望 done", got.CancelStatus)
	}
	last := lastInstanceLog(t, gdb, converged.ID)
	if last.Action != model.ActionCancelSync || last.Status != model.InstanceOpSuccess ||
		last.ActorType != model.ActorTypeSystem {
		t.Fatalf("收敛审计异常: %+v", last)
	}
	// 在途未收敛的实例不应新增审计（避免每轮刷屏）。
	if logs := instanceLogsFromDB(t, gdb, inFlight.ID); len(logs) != 1 {
		t.Fatalf("未收敛实例不应新增审计: %+v", logs)
	}

	// 幂等：再次扫描不再命中已收敛实例（cancel_status=done）。
	report, err = scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("第二轮扫描失败: %v", err)
	}
	if report.CancelScanned != 1 || report.CancelConverged != 0 {
		t.Fatalf("第二轮应仅剩在途实例（未收敛）: %+v", report)
	}
}

// TestScanCancelConvergenceFailureRetries 验证回读故障时不误收敛、不写审计，下一轮恢复后收敛。
func TestScanCancelConvergenceFailureRetries(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "scancancelfail")
	now := time.Now().UTC()
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	setInstanceCancel(t, gdb, instance.ID, model.CancelTypeImmediate, now.Add(-time.Hour))

	host.setHostinfoFail("上游网关超时")
	scanner := newTestScanner(t, gdb, host, now)
	report, err := scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("扫描不应因单实例失败而中断: %v", err)
	}
	if report.CancelFailed != 1 || report.CancelConverged != 0 {
		t.Fatalf("回读故障统计 = %+v，期望 cancel_failed=1", report)
	}
	if got := instanceFromDB(t, gdb, instance.ID); got.Status != model.InstanceStatusActive {
		t.Fatalf("回读故障不得收敛: %s", got.Status)
	}
	if logs := instanceLogsFromDB(t, gdb, instance.ID); len(logs) != 1 {
		t.Fatalf("回读故障不写审计（仅服务日志）: %+v", logs)
	}

	// 上游删除主机并恢复回读 → 下一轮收敛。
	host.setHostinfoFail("")
	host.deleteHost(instance.HostID)
	report, err = scanner.ScanOnce(context.Background())
	if err != nil {
		t.Fatalf("重试扫描失败: %v", err)
	}
	if report.CancelConverged != 1 {
		t.Fatalf("重试后统计 = %+v，期望 cancel_converged=1", report)
	}
	if got := instanceFromDB(t, gdb, instance.ID); got.Status != model.InstanceStatusTerminated {
		t.Fatalf("重试后状态 = %s，期望 terminated", got.Status)
	}
}

// TestDueScanNotEnabledByDefault 验证集成测试引擎默认不启动后台扫描（EnableDueScan 零值）。
func TestDueScanNotEnabledByDefault(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	token, _ := memberTokenFor(t, engine, "scandefault")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)
	setInstanceDue(t, gdb, instance.ID, time.Now().UTC().Add(-time.Hour))

	// 引擎已构建且处理过请求（公开商品目录），实例仍不应被自动暂停（默认不启动调度器）。
	doAPI(t, engine, http.MethodGet, "/api/v1/products", "", nil)
	if got := instanceFromDB(t, gdb, instance.ID); got.Status != model.InstanceStatusActive {
		t.Fatalf("默认未启用扫描时实例不应被暂停: %s", got.Status)
	}
}
