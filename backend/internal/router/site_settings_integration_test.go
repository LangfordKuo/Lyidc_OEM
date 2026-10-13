package router

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
)

// settingByKey 读库取一条设置的原文（第二返回值为是否存在）。
func settingByKey(t *testing.T, gdb *gorm.DB, key string) (string, bool) {
	t.Helper()
	var setting model.Setting
	err := gdb.Where("`key` = ?", key).Take(&setting).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "", false
	case err != nil:
		t.Fatalf("读取设置 %s 失败: %v", key, err)
	}
	return setting.Value, true
}

// containsAny 判断文本是否包含任一子串。
func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 「站点对外地址」设置接口（R16，契约 22.4）：GET/PUT /api/v1/admin/settings/site
// ---------------------------------------------------------------------------

// TestSiteSettingsPermissions 验证只有 admin 能读写（含读取）：finance / support 403、会员 401。
func TestSiteSettingsPermissions(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage5Engine(t, gdb, nil, nil)

	memberToken, _ := memberTokenFor(t, engine, "sitesettings")
	finance := stage4AdminToken(t, engine, gdb, "sitefin", model.RoleFinance)
	support := stage4AdminToken(t, engine, gdb, "sitesup", model.RoleSupport)

	for name, token := range map[string]string{"会员": memberToken, "finance": finance, "support": support} {
		rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/site", token, nil)
		wantStatus, wantCode := http.StatusForbidden, response.CodeForbidden
		if name == "会员" {
			wantStatus, wantCode = http.StatusUnauthorized, response.CodeUnauthorized
		}
		if rec.Code != wantStatus || envelope.Code != wantCode {
			t.Fatalf("%s GET /admin/settings/site 应 %d/%d：HTTP %d, body=%s",
				name, wantStatus, wantCode, rec.Code, rec.Body.String())
		}
		rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/site", token,
			map[string]any{"url": "https://oem.example.com"})
		if rec.Code != wantStatus || envelope.Code != wantCode {
			t.Fatalf("%s PUT /admin/settings/site 应 %d/%d：HTTP %d, body=%s",
				name, wantStatus, wantCode, rec.Code, rec.Body.String())
		}
	}
}

// TestSiteSettingsReadWrite 验证读写口径：默认未配置 → 保存后回显（去尾斜杠）并向交付生效；
// 只改 url，向导写入的 name / admin_email 原样保留；空串 = 清空。
func TestSiteSettingsReadWrite(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage5Engine(t, gdb, nil, nil)
	admin := stage4AdminToken(t, engine, gdb, "site", model.RoleAdmin)

	// 1) 未配置：url 为空串、configured=false，并给出按当前请求推断的推荐值。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/settings/site", admin, nil)
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("读取站点设置失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	view := decodeData[siteSettingsView](t, envelope)
	if view.URL != "" || view.Configured {
		t.Fatalf("未配置时应为空串/未配置: %+v", view)
	}
	if view.URLRecommended != "http://example.com" {
		t.Fatalf("推荐地址 = %q，期望按请求推断的 http://example.com", view.URLRecommended)
	}

	// 2) 向导先写过 name / admin_email：PUT 只改 url，另外两项必须原样保留。
	seedSetting(t, gdb, settings.KeySite, `{"name":"Lyidc","url":"","admin_email":"ops@example.com"}`)
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/site", admin,
		map[string]any{"url": "https://oem.example.com/"}) // 带尾斜杠 → 保存时去掉
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("保存站点设置失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	view = decodeData[siteSettingsView](t, envelope)
	if view.URL != "https://oem.example.com" || !view.Configured || view.UpdatedBy == nil {
		t.Fatalf("保存后的视图错误: %+v", view)
	}
	stored, ok := settingByKey(t, gdb, settings.KeySite)
	if !ok || stored != `{"name":"Lyidc","url":"https://oem.example.com","admin_email":"ops@example.com"}` {
		t.Fatalf("落库的 site 键 = %s，期望保留 name / admin_email 且 url 去尾斜杠", stored)
	}

	// 3) 空串 = 清空（回到按请求 Host 推断），其余字段仍保留。
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/site", admin, map[string]any{"url": ""})
	if rec.Code != http.StatusOK || envelope.Code != 0 {
		t.Fatalf("清空站点设置失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	if view = decodeData[siteSettingsView](t, envelope); view.URL != "" || view.Configured {
		t.Fatalf("清空后应回到未配置: %+v", view)
	}
	if stored, ok := settingByKey(t, gdb, settings.KeySite); !ok ||
		stored != `{"name":"Lyidc","url":"","admin_email":"ops@example.com"}` {
		t.Fatalf("清空后落库的 site 键 = %s", stored)
	}
}

// TestSiteSettingsValidation 验证保存校验（契约 22.4）：url 必填（40001）、
// 非 http(s)/缺主机名 → 40002，且**非法值一律不落库**。
func TestSiteSettingsValidation(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage5Engine(t, gdb, nil, nil)
	admin := stage4AdminToken(t, engine, gdb, "sitebad", model.RoleAdmin)

	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/site", admin, map[string]any{})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("缺 url 应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	for _, bad := range []string{"oem.example.com", "ftp://oem.example.com", "https://", " https://   "} {
		rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/settings/site", admin,
			map[string]any{"url": bad})
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
			t.Fatalf("非法地址 %q 应 40002：HTTP %d, body=%s", bad, rec.Code, rec.Body.String())
		}
		if !containsAny(envelope.Message, "http://", "https://") {
			t.Fatalf("非法地址的提示应说明格式要求，得到 %q", envelope.Message)
		}
	}

	if _, ok := settingByKey(t, gdb, settings.KeySite); ok {
		t.Fatal("校验失败不应落库")
	}
}
