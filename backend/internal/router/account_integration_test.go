package router

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// assertNoSecrets 断言响应体没有泄露密码哈希字段或 bcrypt 哈希。
func assertNoSecrets(t *testing.T, label, body string) {
	t.Helper()
	if strings.Contains(body, "password_hash") || strings.Contains(body, "$2a$") ||
		strings.Contains(body, "password") {
		t.Errorf("%s 响应疑似泄露密码信息: %s", label, body)
	}
}

func TestRegisterValidationAndConflicts(t *testing.T) {
	engine := newAccountEngine(t, testDatabase(t))

	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"username": "alice",
		"email":    "Alice@Example.com",
		"password": "alice123456",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("注册 HTTP 状态码 = %d, 期望 200（body=%s）", rec.Code, rec.Body.String())
	}
	if envelope.Code != response.CodeSuccess || envelope.Message != "ok" {
		t.Fatalf("注册 code/message = (%d, %q), 期望 (0, ok)", envelope.Code, envelope.Message)
	}

	member := decodeData[memberView](t, envelope)
	if member.ID == 0 || member.Username != "alice" || member.Email != "Alice@Example.com" {
		t.Errorf("注册返回 member = %+v, 期望 id>0 username=alice email=Alice@Example.com", member)
	}
	if member.Nickname != "alice" {
		t.Errorf("默认昵称 = %q, 期望 alice", member.Nickname)
	}
	if member.Phone != nil {
		t.Errorf("默认手机号 = %v, 期望 null", *member.Phone)
	}
	if member.Status != model.StatusActive {
		t.Errorf("默认状态 = %q, 期望 active", member.Status)
	}
	if member.Balance != "0.00" {
		t.Errorf("默认余额 = %q, 期望 \"0.00\"", member.Balance)
	}
	if member.LastLoginAt != nil {
		t.Errorf("注册时 last_login_at = %v, 期望 null", *member.LastLoginAt)
	}
	if _, err := time.Parse(time.RFC3339, member.CreatedAt); err != nil {
		t.Errorf("created_at = %q 不是 RFC3339: %v", member.CreatedAt, err)
	} else if !strings.HasSuffix(member.CreatedAt, "Z") {
		t.Errorf("created_at = %q, 期望 UTC（Z 结尾）", member.CreatedAt)
	}
	assertNoSecrets(t, "注册", rec.Body.String())

	tests := []struct {
		name    string
		body    any
		want    int
		wantMsg string
	}{
		{
			name:    "用户名重复（大小写不敏感）",
			body:    map[string]string{"username": "ALICE", "email": "other@example.com", "password": "alice123456"},
			want:    response.CodeConflict,
			wantMsg: "用户名已被占用",
		},
		{
			name:    "邮箱重复（大小写不敏感）",
			body:    map[string]string{"username": "bob", "email": "alice@example.com", "password": "bob123456"},
			want:    response.CodeConflict,
			wantMsg: "邮箱已被占用",
		},
		{
			name:    "密码过短",
			body:    map[string]string{"username": "carol", "email": "carol@example.com", "password": "short7"},
			want:    response.CodeInvalidParam,
			wantMsg: "密码至少 8 个字节",
		},
		{
			name:    "用户名非法",
			body:    map[string]string{"username": "a b", "email": "carol@example.com", "password": "carol123456"},
			want:    response.CodeInvalidParam,
			wantMsg: "用户名需为 3-32 位字母、数字或下划线",
		},
		{
			name:    "邮箱非法",
			body:    map[string]string{"username": "carol", "email": "carol@example", "password": "carol123456"},
			want:    response.CodeInvalidParam,
			wantMsg: "邮箱格式不正确",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/auth/register", "", tt.body)
			if envelope.Code != tt.want {
				t.Fatalf("code = %d, 期望 %d（body=%s）", envelope.Code, tt.want, rec.Body.String())
			}
			if envelope.Message != tt.wantMsg {
				t.Errorf("message = %q, 期望 %q", envelope.Message, tt.wantMsg)
			}
			if rec.Code != response.HTTPStatus(tt.want) {
				t.Errorf("HTTP 状态码 = %d, 期望 %d", rec.Code, response.HTTPStatus(tt.want))
			}
		})
	}

	if rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/auth/register", "", 12345); envelope.Code != response.CodeInvalidParam {
		t.Errorf("非法 JSON body code = %d, 期望 40001（HTTP %d）", envelope.Code, rec.Code)
	}
}

func TestLoginAndCurrentMember(t *testing.T) {
	gdb := testDatabase(t)
	engine := newAccountEngine(t, gdb)
	registerMember(t, engine, "alice", "alice@example.com", "alice123456")
	seedAdmin(t, gdb, "admin", "admin123456", model.RoleAdmin, model.StatusActive)

	// 错误密码 / 不存在的用户 / 缺少参数
	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"username": "alice", "password": "wrong-password"})
	if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
		t.Errorf("错误密码登录 = (HTTP %d, code %d), 期望 (401, 401)", rec.Code, envelope.Code)
	}
	if envelope.Message != "用户名或密码错误" {
		t.Errorf("错误密码提示 = %q, 期望 用户名或密码错误", envelope.Message)
	}
	if _, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"username": "nobody", "password": "whatever123"}); envelope.Code != response.CodeUnauthorized {
		t.Errorf("不存在用户 code = %d, 期望 401", envelope.Code)
	}
	if _, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"username": "alice", "password": ""}); envelope.Code != response.CodeInvalidParam {
		t.Errorf("空密码 code = %d, 期望 40001", envelope.Code)
	}

	// 登录成功：token 为三段式 JWT，expires_at 为 RFC3339
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"username": "alice", "password": "alice123456"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("登录失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	login := decodeData[memberLoginView](t, envelope)
	if strings.Count(login.Token, ".") != 2 {
		t.Errorf("token = %q, 期望三段式 JWT", login.Token)
	}
	if _, err := time.Parse(time.RFC3339, login.ExpiresAt); err != nil {
		t.Errorf("expires_at = %q 不是 RFC3339: %v", login.ExpiresAt, err)
	}
	if login.Member.LastLoginAt == nil {
		t.Error("登录后 last_login_at 期望非空")
	}
	assertNoSecrets(t, "登录", rec.Body.String())

	// 无 token / 非法 token / 管理员 token 访问会员接口
	if rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/members/me", "", nil); rec.Code != http.StatusUnauthorized ||
		envelope.Code != response.CodeUnauthorized {
		t.Errorf("无 token 访问 /members/me = (HTTP %d, code %d), 期望 (401, 401)", rec.Code, envelope.Code)
	}
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/members/me", "not-a-token", nil); envelope.Code != response.CodeUnauthorized {
		t.Errorf("非法 token code = %d, 期望 401", envelope.Code)
	}
	adminToken, _ := loginAdmin(t, engine, "admin", "admin123456")
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/members/me", adminToken, nil); envelope.Code != response.CodeUnauthorized {
		t.Errorf("管理员 token 访问会员接口 code = %d, 期望 401（aud 区分）", envelope.Code)
	}

	// 正常访问
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/members/me", login.Token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("带 token 访问 /members/me 失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	current := decodeData[memberView](t, envelope)
	if current.Username != "alice" || current.ID != login.Member.ID {
		t.Errorf("当前会员 = %+v, 期望 alice(id=%d)", current, login.Member.ID)
	}
	assertNoSecrets(t, "/members/me", rec.Body.String())
}

func TestUpdateProfile(t *testing.T) {
	engine := newAccountEngine(t, testDatabase(t))
	registerMember(t, engine, "alice", "alice@example.com", "alice123456")
	registerMember(t, engine, "bob", "bob@example.com", "bob123456")
	token, _ := loginMember(t, engine, "alice", "alice123456")

	// 更新昵称/手机号/邮箱
	rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/members/me", token, map[string]string{
		"nickname": "爱丽丝",
		"phone":    "+86 138-0013-8000",
		"email":    "alice.new@example.com",
	})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("更新资料失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	updated := decodeData[memberView](t, envelope)
	if updated.Nickname != "爱丽丝" || updated.Email != "alice.new@example.com" {
		t.Errorf("更新结果 = %+v, 期望昵称=爱丽丝 邮箱=alice.new@example.com", updated)
	}
	if updated.Phone == nil || *updated.Phone != "+86 138-0013-8000" {
		t.Errorf("手机号 = %v, 期望 +86 138-0013-8000", updated.Phone)
	}
	assertNoSecrets(t, "更新资料", rec.Body.String())

	// 再次提交同一邮箱（排除自己）应成功
	if _, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/members/me", token,
		map[string]string{"email": "alice.new@example.com"}); envelope.Code != response.CodeSuccess {
		t.Errorf("重复提交自己的邮箱 code = %d, 期望 0", envelope.Code)
	}

	// 手机号传空串表示清空
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/members/me", token, map[string]string{"phone": ""})
	if envelope.Code != response.CodeSuccess {
		t.Fatalf("清空手机号失败: %s", rec.Body.String())
	}
	if cleared := decodeData[memberView](t, envelope); cleared.Phone != nil {
		t.Errorf("清空后 phone = %v, 期望 null", *cleared.Phone)
	}

	// 邮箱冲突
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/members/me", token,
		map[string]string{"email": "BOB@example.com"})
	if rec.Code != http.StatusConflict || envelope.Code != response.CodeConflict {
		t.Errorf("邮箱冲突 = (HTTP %d, code %d), 期望 (409, 409)", rec.Code, envelope.Code)
	}
	if envelope.Message != "邮箱已被占用" {
		t.Errorf("邮箱冲突提示 = %q, 期望 邮箱已被占用", envelope.Message)
	}

	// 参数校验
	paramCases := []map[string]any{
		{},
		{"nickname": "   "},
		{"nickname": strings.Repeat("昵", 33)},
		{"phone": "12"},
		{"phone": "138@0013"},
		{"email": "bad-email"},
	}
	for _, body := range paramCases {
		rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/members/me", token, body)
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
			t.Errorf("参数 %v = (HTTP %d, code %d), 期望 (400, 40001)", body, rec.Code, envelope.Code)
		}
	}

	// 未认证
	if rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/members/me", "", map[string]string{"nickname": "x"}); rec.Code != http.StatusUnauthorized ||
		envelope.Code != response.CodeUnauthorized {
		t.Errorf("无 token 更新资料 = (HTTP %d, code %d), 期望 (401, 401)", rec.Code, envelope.Code)
	}
}

func TestChangePassword(t *testing.T) {
	engine := newAccountEngine(t, testDatabase(t))
	registerMember(t, engine, "alice", "alice@example.com", "alice123456")
	token, _ := loginMember(t, engine, "alice", "alice123456")

	// 旧密码错误 → 401
	rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/members/me/password", token, map[string]string{
		"old_password": "wrong-password",
		"new_password": "alice654321",
	})
	if rec.Code != http.StatusUnauthorized || envelope.Code != response.CodeUnauthorized {
		t.Fatalf("旧密码错误 = (HTTP %d, code %d), 期望 (401, 401)", rec.Code, envelope.Code)
	}
	if envelope.Message != "旧密码不正确" {
		t.Errorf("提示 = %q, 期望 旧密码不正确", envelope.Message)
	}

	// 新密码过短 → 40001
	if _, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/members/me/password", token, map[string]string{
		"old_password": "alice123456",
		"new_password": "short7",
	}); envelope.Code != response.CodeInvalidParam {
		t.Errorf("新密码过短 code = %d, 期望 40001", envelope.Code)
	}

	// 修改成功
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/members/me/password", token, map[string]string{
		"old_password": "alice123456",
		"new_password": "alice654321",
	})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("修改密码失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	if string(envelope.Data) != "null" {
		t.Errorf("修改密码 data = %s, 期望 null", envelope.Data)
	}

	// 旧密码失效、新密码可用
	if _, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"username": "alice", "password": "alice123456"}); envelope.Code != response.CodeUnauthorized {
		t.Errorf("旧密码登录 code = %d, 期望 401", envelope.Code)
	}
	loginMember(t, engine, "alice", "alice654321")

	// 本阶段不做 token 主动失效：修改密码前签发的 token 在有效期内仍可用（契约中已写明）。
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/members/me", token, nil); envelope.Code != response.CodeSuccess {
		t.Errorf("旧 token 访问 code = %d, 期望 0（阶段 1 不做主动失效）", envelope.Code)
	}
}

func TestAdminLoginProfileAndMemberList(t *testing.T) {
	gdb := testDatabase(t)
	engine := newAccountEngine(t, gdb)
	seedAdmin(t, gdb, "admin", "admin123456", model.RoleAdmin, model.StatusActive)
	registerMember(t, engine, "alice", "alice@example.com", "alice123456")
	registerMember(t, engine, "bob", "bob@example.com", "bob123456")

	// 登录失败：密码错误、参数缺失
	if rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/admin/auth/login", "",
		map[string]string{"username": "admin", "password": "wrong-password"}); rec.Code != http.StatusUnauthorized ||
		envelope.Code != response.CodeUnauthorized {
		t.Errorf("管理员错误密码 = (HTTP %d, code %d), 期望 (401, 401)", rec.Code, envelope.Code)
	}
	if _, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/admin/auth/login", "",
		map[string]string{"username": "", "password": ""}); envelope.Code != response.CodeInvalidParam {
		t.Errorf("管理员空参数 code = %d, 期望 40001", envelope.Code)
	}

	token, admin := loginAdmin(t, engine, "admin", "admin123456")
	if admin.Role != model.RoleAdmin || admin.Username != "admin" || admin.LastLoginAt == nil {
		t.Errorf("管理员登录返回 = %+v, 期望 role=admin 且 last_login_at 非空", admin)
	}
	if strings.Count(token, ".") != 2 {
		t.Errorf("管理员 token = %q, 期望三段式 JWT", token)
	}

	// profile
	rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/profile", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("管理员 profile 失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	if profile := decodeData[adminView](t, envelope); profile.Username != "admin" || profile.Role != model.RoleAdmin {
		t.Errorf("profile = %+v, 期望 admin/admin", profile)
	}
	assertNoSecrets(t, "管理员 profile", rec.Body.String())

	// 未认证 / 会员 token 访问管理端
	if rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/profile", "", nil); rec.Code != http.StatusUnauthorized ||
		envelope.Code != response.CodeUnauthorized {
		t.Errorf("管理端无 token = (HTTP %d, code %d), 期望 (401, 401)", rec.Code, envelope.Code)
	}
	memberToken, _ := loginMember(t, engine, "alice", "alice123456")
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/members", memberToken, nil); envelope.Code != response.CodeUnauthorized {
		t.Errorf("会员 token 访问管理端 code = %d, 期望 401", envelope.Code)
	}

	// 分页：page_size=1，按 id 倒序 → 第一条为 bob
	rec, envelope = doAPI(t, engine, http.MethodGet, "/api/v1/admin/members?page=1&page_size=1", token, nil)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("会员列表失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	list := decodeData[memberListView](t, envelope)
	if list.Total != 2 || list.Page != 1 || list.PageSize != 1 || len(list.Items) != 1 {
		t.Fatalf("列表 = %+v, 期望 total=2 page=1 page_size=1 items=1", list)
	}
	if list.Items[0].Username != "bob" {
		t.Errorf("第一条 = %q, 期望 bob（按 id 倒序）", list.Items[0].Username)
	}
	assertNoSecrets(t, "会员列表", rec.Body.String())

	// 第二页
	if list := listMembersRequest(t, engine, "?page=2&page_size=1", token); len(list.Items) != 1 || list.Items[0].Username != "alice" {
		t.Errorf("第二页 = %+v, 期望 alice", list.Items)
	}

	// 模糊过滤（大小写不敏感）
	if list := listMembersRequest(t, engine, "?username=ALI", token); list.Total != 1 || len(list.Items) != 1 || list.Items[0].Username != "alice" {
		t.Errorf("username 模糊过滤 = %+v, 期望命中 alice", list)
	}
	if list := listMembersRequest(t, engine, "?email=BOB@", token); list.Total != 1 || list.Items[0].Username != "bob" {
		t.Errorf("email 模糊过滤 = %+v, 期望命中 bob", list)
	}

	// 过滤通配符按字面量处理：% 不应匹配全部
	if list := listMembersRequest(t, engine, "?username=%25", token); list.Total != 0 {
		t.Errorf("通配符过滤 total = %d, 期望 0（通配符按字面量匹配）", list.Total)
	}

	// status 过滤
	if list := listMembersRequest(t, engine, "?status=disabled", token); list.Total != 0 {
		t.Errorf("status=disabled total = %d, 期望 0", list.Total)
	}

	// 参数非法
	for _, query := range []string{"page=0", "page=abc", "page_size=0", "page_size=101", "page_size=x", "status=banned"} {
		rec, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/members?"+query, token, nil)
		if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
			t.Errorf("查询 %q = (HTTP %d, code %d), 期望 (400, 40001)", query, rec.Code, envelope.Code)
		}
	}

	// 改状态：非法 id / 非法状态 / 不存在
	if _, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/members/abc/status", token,
		map[string]string{"status": model.StatusDisabled}); envelope.Code != response.CodeInvalidParam {
		t.Errorf("非法 id code = %d, 期望 40001", envelope.Code)
	}
	if _, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/members/1/status", token,
		map[string]string{"status": "banned"}); envelope.Code != response.CodeInvalidParam {
		t.Errorf("非法 status code = %d, 期望 40001", envelope.Code)
	}
	if rec, envelope := doAPI(t, engine, http.MethodPut, "/api/v1/admin/members/999999/status", token,
		map[string]string{"status": model.StatusDisabled}); rec.Code != http.StatusNotFound ||
		envelope.Code != response.CodeNotFound {
		t.Errorf("不存在会员 = (HTTP %d, code %d), 期望 (404, 404)", rec.Code, envelope.Code)
	}
}

func TestRoleGuardBlocksSupportFromChangingStatus(t *testing.T) {
	gdb := testDatabase(t)
	engine := newAccountEngine(t, gdb)
	seedAdmin(t, gdb, "boss", "boss123456", model.RoleAdmin, model.StatusActive)
	seedAdmin(t, gdb, "finance", "finance123456", model.RoleFinance, model.StatusActive)
	seedAdmin(t, gdb, "support", "support123456", model.RoleSupport, model.StatusActive)
	seedAdmin(t, gdb, "blocked", "blocked123456", model.RoleAdmin, model.StatusDisabled)
	member := registerMember(t, engine, "alice", "alice@example.com", "alice123456")

	// 禁用管理员登录被拒
	if rec, envelope := doAPI(t, engine, http.MethodPost, "/api/v1/admin/auth/login", "",
		map[string]string{"username": "blocked", "password": "blocked123456"}); rec.Code != http.StatusForbidden ||
		envelope.Code != response.CodeForbidden {
		t.Errorf("禁用管理员登录 = (HTTP %d, code %d), 期望 (403, 403)", rec.Code, envelope.Code)
	}

	supportToken, support := loginAdmin(t, engine, "support", "support123456")
	if support.Role != model.RoleSupport {
		t.Fatalf("support 角色 = %q", support.Role)
	}

	// support 可以查看列表与资料
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/members", supportToken, nil); envelope.Code != response.CodeSuccess {
		t.Errorf("support 查看会员列表 code = %d, 期望 0", envelope.Code)
	}
	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/admin/profile", supportToken, nil); envelope.Code != response.CodeSuccess {
		t.Errorf("support 查看 profile code = %d, 期望 0", envelope.Code)
	}

	// support 改状态 → 403
	path := "/api/v1/admin/members/" + itoa(member.ID) + "/status"
	rec, envelope := doAPI(t, engine, http.MethodPut, path, supportToken, map[string]string{"status": model.StatusDisabled})
	if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
		t.Fatalf("support 改状态 = (HTTP %d, code %d), 期望 (403, 403)", rec.Code, envelope.Code)
	}
	if envelope.Message != "当前角色无权执行该操作" {
		t.Errorf("403 提示 = %q, 期望 当前角色无权执行该操作", envelope.Message)
	}

	// finance 改状态 → 允许
	financeToken, _ := loginAdmin(t, engine, "finance", "finance123456")
	if rec, envelope := doAPI(t, engine, http.MethodPut, path, financeToken,
		map[string]string{"status": model.StatusDisabled}); rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Errorf("finance 改状态 = (HTTP %d, code %d), 期望 (200, 0)", rec.Code, envelope.Code)
	}

	// admin 改状态 → 允许
	bossToken, _ := loginAdmin(t, engine, "boss", "boss123456")
	if rec, envelope := doAPI(t, engine, http.MethodPut, path, bossToken,
		map[string]string{"status": model.StatusActive}); rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Errorf("admin 改状态 = (HTTP %d, code %d), 期望 (200, 0)", rec.Code, envelope.Code)
	}
}

func TestDisabledMemberIsRejectedEverywhere(t *testing.T) {
	gdb := testDatabase(t)
	engine := newAccountEngine(t, gdb)
	seedAdmin(t, gdb, "admin", "admin123456", model.RoleAdmin, model.StatusActive)
	member := registerMember(t, engine, "alice", "alice@example.com", "alice123456")
	token, _ := loginMember(t, engine, "alice", "alice123456")

	if _, envelope := doAPI(t, engine, http.MethodGet, "/api/v1/members/me", token, nil); envelope.Code != response.CodeSuccess {
		t.Fatalf("禁用前 /members/me code = %d, 期望 0", envelope.Code)
	}

	adminToken, _ := loginAdmin(t, engine, "admin", "admin123456")
	path := "/api/v1/admin/members/" + itoa(member.ID) + "/status"
	rec, envelope := doAPI(t, engine, http.MethodPut, path, adminToken, map[string]string{"status": model.StatusDisabled})
	if rec.Code != http.StatusOK {
		t.Fatalf("禁用会员失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	if disabled := decodeData[memberView](t, envelope); disabled.Status != model.StatusDisabled {
		t.Errorf("禁用后 status = %q, 期望 disabled", disabled.Status)
	}

	// 登录被拒：403（契约写死的一种）
	rec, envelope = doAPI(t, engine, http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"username": "alice", "password": "alice123456"})
	if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
		t.Fatalf("禁用会员登录 = (HTTP %d, code %d), 期望 (403, 403)", rec.Code, envelope.Code)
	}
	if envelope.Message != "账号已被禁用" {
		t.Errorf("提示 = %q, 期望 账号已被禁用", envelope.Message)
	}

	// 旧 token 立即失效：/members/me 与改密码均 403
	for _, target := range []struct {
		method string
		path   string
		body   any
	}{
		{method: http.MethodGet, path: "/api/v1/members/me"},
		{method: http.MethodPost, path: "/api/v1/members/me/password",
			body: map[string]string{"old_password": "alice123456", "new_password": "alice654321"}},
	} {
		rec, envelope := doAPI(t, engine, target.method, target.path, token, target.body)
		if rec.Code != http.StatusForbidden || envelope.Code != response.CodeForbidden {
			t.Errorf("禁用后 %s %s = (HTTP %d, code %d), 期望 (403, 403)", target.method, target.path, rec.Code, envelope.Code)
		}
	}

	// 重新启用后可正常登录
	if rec, envelope := doAPI(t, engine, http.MethodPut, path, adminToken,
		map[string]string{"status": model.StatusActive}); rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("启用会员失败: HTTP %d body=%s", rec.Code, rec.Body.String())
	}
	loginMember(t, engine, "alice", "alice123456")
}

func TestAccountRoutesRegistered(t *testing.T) {
	engine := New(Options{Logger: silentLogger()})

	expected := map[string]bool{
		http.MethodPost + " /api/v1/auth/register":           false,
		http.MethodPost + " /api/v1/auth/login":              false,
		http.MethodGet + " /api/v1/members/me":               false,
		http.MethodPut + " /api/v1/members/me":               false,
		http.MethodPost + " /api/v1/members/me/password":     false,
		http.MethodPost + " /api/v1/admin/auth/login":        false,
		http.MethodGet + " /api/v1/admin/profile":            false,
		http.MethodGet + " /api/v1/admin/members":            false,
		http.MethodPut + " /api/v1/admin/members/:id/status": false,
	}

	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := expected[key]; ok {
			expected[key] = true
		}
	}
	for key, found := range expected {
		if !found {
			t.Errorf("未注册路由 %s", key)
		}
	}
}
