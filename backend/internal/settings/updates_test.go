package settings

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateUpdatesRepo(t *testing.T) {
	valid := []string{"LangfordKuo/Lyidc_OEM", "a/b", "my-org/repo.name_1"}
	for _, repo := range valid {
		if err := ValidateUpdatesRepo(repo); err != nil {
			t.Errorf("ValidateUpdatesRepo(%q) 应当通过，实际 %v", repo, err)
		}
	}

	invalid := []string{
		"", "noslash", "a/b/c", "/repo", "owner/", "a/.", "a/..",
		"a/b c", "a/b?x=1", "bad_owner/repo", "a/" + strings.Repeat("x", 101),
	}
	for _, repo := range invalid {
		err := ValidateUpdatesRepo(repo)
		if !errors.Is(err, ErrFormat) {
			t.Errorf("ValidateUpdatesRepo(%q) 应返回 ErrFormat，实际 %v", repo, err)
		}
	}
}

func TestValidateUpdatesToken(t *testing.T) {
	if err := validateUpdatesToken(""); err != nil {
		t.Errorf("留空 Token 合法，实际 %v", err)
	}
	if err := validateUpdatesToken("ghp_abcdefghijklmnopqrstuvwxyz0123456789"); err != nil {
		t.Errorf("常规 Token 应合法，实际 %v", err)
	}
	for _, token := range []string{"has space", "line\nbreak", strings.Repeat("t", 256)} {
		if err := validateUpdatesToken(token); !errors.Is(err, ErrFormat) {
			t.Errorf("validateUpdatesToken 应拒绝 %q，实际 %v", token, err)
		}
	}
}

func TestUpdatesApplyThreeStateToken(t *testing.T) {
	current := Updates{Repo: "a/b", GitHubToken: "old-token"}

	// 省略 = 保持不变。
	kept, err := current.Apply(UpdatesUpdate{})
	if err != nil || kept != current {
		t.Fatalf("省略字段应保持不变，实际 %+v / %v", kept, err)
	}

	// 非空串 = 替换。
	replacement := " new-token "
	replaced, err := current.Apply(UpdatesUpdate{GitHubToken: &replacement})
	if err != nil || replaced.GitHubToken != "new-token" || replaced.Repo != "a/b" {
		t.Fatalf("替换 Token 不符: %+v / %v", replaced, err)
	}

	// 空串 = 清空，仓库不受影响。
	empty := ""
	cleared, err := current.Apply(UpdatesUpdate{GitHubToken: &empty})
	if err != nil || cleared.GitHubToken != "" || cleared.Repo != "a/b" {
		t.Fatalf("清空 Token 不符: %+v / %v", cleared, err)
	}
}

func TestUpdatesApplyRejectsBadRepo(t *testing.T) {
	bad := "not a repo"
	_, err := Updates{Repo: DefaultUpdatesRepo}.Apply(UpdatesUpdate{Repo: &bad})
	if !errors.Is(err, ErrFormat) {
		t.Fatalf("非法仓库应返回 ErrFormat，实际 %v", err)
	}
}

func TestDecodeUpdatesDefaultsAndCorruption(t *testing.T) {
	value, err := decodeUpdates("")
	if err != nil || value.Repo != DefaultUpdatesRepo || value.GitHubToken != "" {
		t.Fatalf("未配置时应取缺省仓库，实际 %+v / %v", value, err)
	}

	value, err = decodeUpdates(`{"github_token":"tok"}`)
	if err != nil || value.Repo != DefaultUpdatesRepo || value.GitHubToken != "tok" {
		t.Fatalf("缺省仓库字段应补齐，实际 %+v / %v", value, err)
	}

	if _, err := decodeUpdates("{not json"); !errors.Is(err, ErrCorrupt) {
		t.Errorf("非法 JSON 应返回 ErrCorrupt，实际 %v", err)
	}
	if _, err := decodeUpdates(`{"repo":"broken"}`); !errors.Is(err, ErrCorrupt) {
		t.Errorf("库内仓库非法应返回 ErrCorrupt，实际 %v", err)
	}
}
