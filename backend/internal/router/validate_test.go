package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		username string
		wantErr  bool
	}{
		{username: "alice"},
		{username: "a_1"},
		{username: "user_2026"},
		{username: strings.Repeat("a", 32)},
		{username: "ab", wantErr: true},
		{username: strings.Repeat("a", 33), wantErr: true},
		{username: "alice smith", wantErr: true},
		{username: "alice@example.com", wantErr: true},
		{username: "爱丽丝", wantErr: true},
		{username: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.username, func(t *testing.T) {
			if err := validateUsername(tt.username); (err != nil) != tt.wantErr {
				t.Errorf("validateUsername(%q) err=%v, 期望错误=%t", tt.username, err, tt.wantErr)
			}
		})
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		email   string
		wantErr bool
	}{
		{email: "alice@example.com"},
		{email: "a.b+tag@sub.example.cn"},
		{email: strings.Repeat("a", 100) + "@example.com"},
		{email: "", wantErr: true},
		{email: "alice", wantErr: true},
		{email: "alice@example", wantErr: true},
		{email: "@example.com", wantErr: true},
		{email: "alice@.com", wantErr: true},
		{email: "alice smith@example.com", wantErr: true},
		{email: strings.Repeat("a", 130) + "@example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			if err := validateEmail(tt.email); (err != nil) != tt.wantErr {
				t.Errorf("validateEmail(%q) err=%v, 期望错误=%t", tt.email, err, tt.wantErr)
			}
		})
	}
}

func TestValidateNicknameAndPhone(t *testing.T) {
	if err := validateNickname("小明"); err != nil {
		t.Errorf("validateNickname(中文) = %v, 期望 nil", err)
	}
	if err := validateNickname(strings.Repeat("昵", 33)); err == nil {
		t.Error("超长昵称期望报错")
	}
	if err := validateNickname("   "); err == nil {
		t.Error("空白昵称期望报错")
	}

	valid := []string{"", "13800138000", "+86 138-0013-8000", "(010) 8888-6666"}
	for _, phone := range valid {
		if err := validatePhone(phone); err != nil {
			t.Errorf("validatePhone(%q) = %v, 期望 nil", phone, err)
		}
	}
	invalid := []string{"1234", "138001380001380013800013800138000", "138@0013", "aaa-bbbb"}
	for _, phone := range invalid {
		if err := validatePhone(phone); err == nil {
			t.Errorf("validatePhone(%q) 期望报错", phone)
		}
	}
}

func TestBearerTokenParsing(t *testing.T) {
	tests := []struct {
		header string
		want   string
		wantOK bool
	}{
		{header: "Bearer abc.def.ghi", want: "abc.def.ghi", wantOK: true},
		{header: "bearer abc", want: "abc", wantOK: true},
		{header: "  Bearer   abc  ", want: "abc", wantOK: true},
		{header: ""},
		{header: "abc"},
		{header: "Basic abc"},
		{header: "Bearer"},
		{header: "Bearer "},
		{header: "Bearer a b"},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			got, ok := bearerToken(req)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("bearerToken(%q) = (%q, %t), 期望 (%q, %t)", tt.header, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestIntQueryDefaultsAndBounds(t *testing.T) {
	newContext := func(raw string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/?page="+raw, nil)
		return c
	}

	if value, err := intQuery(newContext(""), "page", defaultPage, 1, maxPage); err != nil || value != defaultPage {
		t.Errorf("缺省值 = (%d, %v), 期望 (%d, nil)", value, err, defaultPage)
	}
	if value, err := intQuery(newContext("3"), "page", defaultPage, 1, maxPage); err != nil || value != 3 {
		t.Errorf("正常值 = (%d, %v), 期望 (3, nil)", value, err)
	}
	for _, raw := range []string{"0", "-1", "abc", "1.5", "99999999"} {
		if _, err := intQuery(newContext(raw), "page", defaultPage, 1, maxPage); err == nil {
			t.Errorf("intQuery(%q) 期望报错", raw)
		}
	}
}

func TestFormatTimeIsRFC3339UTC(t *testing.T) {
	location := time.FixedZone("CST", 8*3600)
	moment := time.Date(2026, 10, 8, 14, 18, 31, 0, location)

	if got, want := formatTime(moment), "2026-10-08T06:18:31Z"; got != want {
		t.Errorf("formatTime() = %q, 期望 %q", got, want)
	}
	if formatTimePtr(nil) != nil {
		t.Error("formatTimePtr(nil) 期望 nil")
	}
	formatted := formatTimePtr(&moment)
	if formatted == nil || *formatted != "2026-10-08T06:18:31Z" {
		t.Errorf("formatTimePtr() = %v, 期望 2026-10-08T06:18:31Z", formatted)
	}
}

func TestViewsNeverExposePasswordHash(t *testing.T) {
	member := &model.Member{
		ID:           1,
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "$2a$10$secret",
		Nickname:     "爱丽丝",
		Status:       model.StatusActive,
		Balance:      model.ZeroMoney,
	}
	admin := &model.Admin{
		ID:           1,
		Username:     "admin",
		PasswordHash: "$2a$10$secret",
		Nickname:     "管理员",
		Role:         model.RoleAdmin,
		Status:       model.StatusActive,
	}

	for name, payload := range map[string]any{
		"memberView": newMemberView(member),
		"adminView":  newAdminView(admin),
	} {
		raw := toJSON(t, payload)
		if strings.Contains(raw, "password") || strings.Contains(raw, "$2a$") {
			t.Errorf("%s 泄露了密码字段: %s", name, raw)
		}
	}
}
