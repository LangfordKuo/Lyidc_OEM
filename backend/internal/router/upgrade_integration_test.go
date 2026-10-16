package router

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/buildinfo"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/config"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/updates"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upgrade"
)

// stubUpgradeChecker 是升级前置检查用的假检查结果（不出网）。
type stubUpgradeChecker struct {
	release updates.Release
	ok      bool
}

func (s stubUpgradeChecker) CachedRelease(context.Context) (updates.Release, string, bool, error) {
	return s.release, "LangfordKuo/Lyidc_OEM", s.ok, nil
}

// TestUpgradeAdminEndpoints 覆盖一键升级接口（R13-B，契约 20.3 / 20.4 / 20.9）：
// 角色闸门、二次确认与拒绝口径、任务读取（含从未升级、进行中与已完成）。
func TestUpgradeAdminEndpoints(t *testing.T) {
	previousVersion := buildinfo.Version
	buildinfo.Version = "v1.0.0"
	t.Cleanup(func() { buildinfo.Version = previousVersion })

	gdb := testDatabase(t)
	dir := t.TempDir()
	service, err := upgrade.NewService(upgrade.Options{
		Dir:     dir,
		Addr:    "127.0.0.1:18080",
		Args:    []string{},
		Checker: stubUpgradeChecker{release: updates.Release{TagName: "v1.1.0"}, ok: true},
		// P2：更新源地址是构建期固定项（生产由 buildinfo 注入），测试从构造参数注入。
		Source: updates.ResolveSource("", ""),
		Exit:   func(int) {},
	})
	if err != nil {
		t.Fatalf("构造升级服务失败：%v", err)
	}

	engine := New(Options{
		CacheDir: t.TempDir(),
		Logger:   silentLogger(),
		DB:       gdb,
		JWT:      config.JWTConfig{Secret: testJWTSecret, ExpireHours: 168},
		Upgrade:  service,
	})
	seedAdmin(t, gdb, "root", "admin123456", model.RoleAdmin, model.StatusActive)
	seedAdmin(t, gdb, "fin", "admin123456", model.RoleFinance, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "root", "admin123456")
	financeToken, _ := loginAdmin(t, engine, "fin", "admin123456")

	// 角色闸门：未登录 401、finance 403（仅 admin，契约 20.9）。
	rec, _ := doAPI(t, engine, http.MethodPost, "/api/v1/admin/updates/apply", "", map[string]any{"confirm": true})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录发起升级 = HTTP %d，期望 401", rec.Code)
	}
	rec, _ = doAPI(t, engine, http.MethodGet, "/api/v1/admin/updates/job", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录读取升级任务 = HTTP %d，期望 401", rec.Code)
	}
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/admin/updates/apply", map[string]any{"confirm": true, "target_version": "v1.1.0"}},
		{http.MethodGet, "/api/v1/admin/updates/job", nil},
	} {
		rec, _ = doAPI(t, engine, c.method, c.path, financeToken, c.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("finance %s %s = HTTP %d，期望 403", c.method, c.path, rec.Code)
		}
	}

	// 从未升级：任务为 null（不是 404）。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/updates/job", adminToken, nil)
	if rec.Code != http.StatusOK || string(envelope.Data) != "null" {
		t.Fatalf("从未升级时任务应为 null：HTTP %d data=%s", rec.Code, envelope.Data)
	}

	// 参数与规则类拒绝（契约 20.3）。
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/admin/updates/apply", adminToken,
		map[string]any{"target_version": "v1.1.0"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Errorf("缺少二次确认 = HTTP %d code %d，期望 400 / 40001", rec.Code, envelope.Code)
	}
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/admin/updates/apply", adminToken,
		map[string]any{"confirm": true, "target_version": "v1.1.0"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Errorf("测试进程是开发构建之外的版本时也需前置检查通过；实际 HTTP %d code %d", rec.Code, envelope.Code)
	}

	// 进行中的任务：再次发起 → 409（契约 20.3 的互斥口径）。
	seedUpgradeState(t, dir, "job-running", upgrade.StatusRunning, upgrade.PhaseDownloading, time.Now().UTC())
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/admin/updates/apply", adminToken,
		map[string]any{"confirm": true, "target_version": "v1.1.0"})
	if rec.Code != http.StatusConflict || envelope.Code != response.CodeConflict {
		t.Errorf("重复发起 = HTTP %d code %d，期望 409 / 409", rec.Code, envelope.Code)
	}

	// 读取任务：进行中也要能读到阶段、进度与日志尾部（刷新页面后不丢状态，契约 20.5）。
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/updates/job", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("读取升级任务 = HTTP %d", rec.Code)
	}
	view := decodeData[map[string]any](t, envelope)
	if view["job_id"] != "job-running" || view["status"] != upgrade.StatusRunning ||
		view["phase"] != upgrade.PhaseDownloading || view["phase_label"] == "" {
		t.Fatalf("进行中任务视图不符：%+v", view)
	}

	// 完成态：成功任务带目标版本、结束时间与空失败原因。
	seedUpgradeDone(t, dir)
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/updates/job", adminToken, nil)
	view = decodeData[map[string]any](t, envelope)
	if view["status"] != upgrade.StatusDone || view["progress"].(float64) != 100 {
		t.Fatalf("完成态视图不符：%+v", view)
	}

	// 回滚态：必须带上失败原因与 rolled_back 标记，供前端展示「已自动回滚」。
	seedUpgradeRolledBack(t, dir)
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/updates/job", adminToken, nil)
	view = decodeData[map[string]any](t, envelope)
	if view["status"] != upgrade.StatusRolledBack || view["rolled_back"] != true ||
		!strings.Contains(view["message"].(string), "健康检查未通过") {
		t.Fatalf("回滚态视图不符：%+v", view)
	}
}

// seedUpgradeState 直接写状态文件（模拟主进程或 helper 的落盘结果）。
func seedUpgradeState(t *testing.T, dir, id, status, phase string, now time.Time) {
	t.Helper()
	job := map[string]any{
		"job_id": id, "status": status, "phase": phase, "progress": 20,
		"from_version": "v1.0.0", "target_version": "v1.1.0",
		"started_at": now.Format(time.RFC3339), "updated_at": now.Format(time.RFC3339),
		"install_dir": dir,
	}
	writeUpgradeState(t, dir, job)
}

// seedUpgradeDone 写一个「已成功」的任务状态（progress 按 done 阶段的 100 落盘）。
func seedUpgradeDone(t *testing.T, dir string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	writeUpgradeState(t, dir, map[string]any{
		"job_id": "job-done", "status": upgrade.StatusDone, "phase": upgrade.PhaseDone,
		"progress": 100, "from_version": "v1.0.0", "target_version": "v1.1.0",
		"started_at": now, "finished_at": now, "updated_at": now, "install_dir": dir,
		"db_migrated": true,
	})
}

// seedUpgradeRolledBack 写一个「失败并已回滚」的任务状态。
func seedUpgradeRolledBack(t *testing.T, dir string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	writeUpgradeState(t, dir, map[string]any{
		"job_id": "job-rolled-back", "status": upgrade.StatusRolledBack, "phase": upgrade.PhaseRolledBack,
		"progress": 92, "from_version": "v1.0.0", "target_version": "v1.1.0",
		"started_at": now, "finished_at": now, "updated_at": now, "install_dir": dir,
		"rolled_back": true, "db_migrated": true,
		"message": "新版本健康检查未通过：健康检查超时（1m0s）：服务尚未响应",
	})
}

// writeUpgradeState 落盘状态文件（与 upgrade 包的字段名保持一致）。
func writeUpgradeState(t *testing.T, dir string, job map[string]any) {
	t.Helper()
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("编码状态失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, upgrade.StateFile), raw, 0o600); err != nil {
		t.Fatalf("写状态失败：%v", err)
	}
}
