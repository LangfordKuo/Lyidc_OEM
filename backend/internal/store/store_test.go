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
		// 工单（阶段 6a）。
		"CreateTicketWithMessage": errOf3(st.CreateTicketWithMessage(ctx, TicketInput{})),
		"ListTickets":             errOf2(st.ListTickets(ctx, TicketFilter{Page: 1, PageSize: 10})),
		"TicketByID":              errOf(st.TicketByID(ctx, 1)),
		"TicketByIDForMember":     errOf(st.TicketByIDForMember(ctx, 1, 1)),
		"CountOpenTickets":        errOf(st.CountOpenTickets(ctx, 1)),
		"ListTicketMessages":      errOf(st.ListTicketMessages(ctx, 1, false)),
		"AppendTicketReply":       errOf3(st.AppendTicketReply(ctx, 1, TicketReplyInput{})),
		"CloseTicket":             errOf3(st.CloseTicket(ctx, 1)),
		"MembersByIDs":            errOf(st.MembersByIDs(ctx, []uint64{1})),
		"AdminsByIDs":             errOf(st.AdminsByIDs(ctx, []uint64{1})),
		"InstancesByIDs":          errOf(st.InstancesByIDs(ctx, []uint64{1})),
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

// errOf3 对应返回 (值, 附带值, 错误) 的方法（工单的创建/回复/关闭）。
func errOf3[T any, U any](_ T, _ U, err error) error { return err }
