package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

func TestEscapeLikeEscapesWildcards(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "alice", want: "alice"},
		{in: "a%c", want: `a\%c`},
		{in: "a_c", want: `a\_c`},
		{in: `a\c`, want: `a\\c`},
		{in: `100%_done\`, want: `100\%\_done\\`},
	}

	for _, tt := range tests {
		if got := escapeLike(tt.in); got != tt.want {
			t.Errorf("escapeLike(%q) = %q, 期望 %q", tt.in, got, tt.want)
		}
	}
}

func TestContainsKeywordWrapsEscapedValue(t *testing.T) {
	if got := containsKeyword("a%b"); got != `%a\%b%` {
		t.Errorf("containsKeyword(%q) = %q, 期望 %q", "a%b", got, `%a\%b%`)
	}
}

func TestStoreWithoutDatabaseReturnsUnavailable(t *testing.T) {
	st := New(nil)
	ctx := context.Background()
	now := time.Now()

	tests := map[string]error{
		"CreateMember":         st.CreateMember(ctx, &model.Member{}),
		"MemberByID":           errOf(st.MemberByID(ctx, 1)),
		"MemberByUsername":     errOf(st.MemberByUsername(ctx, "alice")),
		"UpdateMemberProfile":  errOf(st.UpdateMemberProfile(ctx, 1, MemberProfileUpdate{})),
		"UpdateMemberPassword": st.UpdateMemberPassword(ctx, 1, "hash"),
		"UpdateMemberStatus":   errOf(st.UpdateMemberStatus(ctx, 1, model.StatusDisabled)),
		"TouchMemberLogin":     st.TouchMemberLogin(ctx, 1, now),
		"ListMembers":          errOf2(st.ListMembers(ctx, MemberFilter{Page: 1, PageSize: 10})),
		"AdminByID":            errOf(st.AdminByID(ctx, 1)),
		"AdminByUsername":      errOf(st.AdminByUsername(ctx, "admin")),
		"TouchAdminLogin":      st.TouchAdminLogin(ctx, 1, now),
	}

	for name, err := range tests {
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s 错误 = %v, 期望 ErrUnavailable", name, err)
		}
	}
}

// errOf 丢弃值只取错误。
func errOf[T any](_ T, err error) error { return err }

// errOf2 对应返回 (切片, 计数, 错误) 的方法。
func errOf2[T any](_ T, _ int64, err error) error { return err }
