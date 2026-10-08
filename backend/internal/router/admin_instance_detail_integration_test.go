package router

import (
	"net/http"
	"testing"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// TestAdminInstanceDetail 验证阶段 8 新增的 GET /api/v1/admin/instances/:id：
// 字段对齐会员端详情口径（含主机账号密码等敏感字段）+ member_id，权限沿用管理端列表口径。
func TestAdminInstanceDetail(t *testing.T) {
	gdb := testDatabase(t)
	host := newFakeHostServer(t)
	engine := newStage5Engine(t, gdb, host.client(t), nil)
	gateway := newFakeEpayGateway(t)
	seedEpaySetting(t, gdb, gateway.baseURL(), "https://oem.example.com/api/v1/payments/epay/notify")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	admin := stage4AdminToken(t, engine, gdb, "detadmin", model.RoleAdmin)
	finance := stage4AdminToken(t, engine, gdb, "detfin", model.RoleFinance)
	support := stage4AdminToken(t, engine, gdb, "detsup", model.RoleSupport)
	token, member := memberTokenFor(t, engine, "detuser")
	instance := newInstanceFor(t, engine, gateway, gdb, token, product.ID)

	path := "/api/v1/admin/instances/" + itoa(instance.ID)

	// 角色矩阵：管理端实例列表口径（三角色均可读），会员 token 401。
	for name, at := range map[string]string{"admin": admin, "finance": finance, "support": support} {
		rec, envelope := doAPI(t, engine, http.MethodGet, path, at, nil)
		if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
			t.Fatalf("%s 读取实例详情应 200：HTTP %d, body=%s", name, rec.Code, rec.Body.String())
		}
		detail := decodeData[adminInstanceDetailView](t, envelope)
		if detail.MemberID != member.ID {
			t.Fatalf("%s 详情 member_id = %d，期望 %d", name, detail.MemberID, member.ID)
		}
		if detail.ID != instance.ID || detail.OrderID != instance.OrderID || detail.HostID != instance.HostID {
			t.Fatalf("%s 详情基础字段错误: %+v", name, detail)
		}
		// 与会员端详情同口径：缺省 IP 为闭集（假上游回读可能为空），
		// 但账号与端口字段必须存在（password 由交付时随机生成，非空）。
		if detail.Username == "" || detail.Password == "" {
			t.Fatalf("%s 详情缺少主机账号字段: username=%q password_len=%d",
				name, detail.Username, len(detail.Password))
		}
		if detail.AssignedIPs == nil {
			t.Fatalf("%s 详情 assigned_ips 应为数组（无值时输出空数组）", name)
		}
		if detail.UpdatedAt == "" || detail.Status != instance.Status {
			t.Fatalf("%s 详情状态/更新时间字段错误: %+v", name, detail)
		}
	}

	rec, _ := doAPI(t, engine, http.MethodGet, path, token, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("会员调管理端实例详情应 401，得到 %d", rec.Code)
	}

	// 不存在 404；ID 非法 40001。
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/instances/987654", admin, nil)
	if rec.Code != http.StatusNotFound || envelope.Code != response.CodeNotFound {
		t.Fatalf("不存在实例应 404：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/instances/abc", admin, nil)
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("非法 ID 应 40001：HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
}
