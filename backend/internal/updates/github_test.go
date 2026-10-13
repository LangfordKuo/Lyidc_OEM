package updates

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const testRepo = "LangfordKuo/Lyidc_OEM"

// latestBody 是一份符合 GitHub releases/latest 响应结构的最小样例。
const latestBody = `{
  "tag_name": "v0.10.0",
  "name": "v0.10.0",
  "published_at": "2026-10-10T12:00:00Z",
  "body": "修复若干问题",
  "html_url": "https://github.com/LangfordKuo/Lyidc_OEM/releases/tag/v0.10.0",
  "assets": [
    {"name": "lyidc-oem-v0.10.0-linux-amd64.tar.gz", "size": 12345, "browser_download_url": "https://example.test/linux.tar.gz"},
    {"name": "lyidc-oem-v0.10.0-linux-amd64.tar.gz.sha256", "size": 90, "browser_download_url": "https://example.test/linux.sha256"}
  ]
}`

// newTestClient 启动一个假 GitHub 服务，返回指向它的客户端（超时可由用例指定）。
func newTestClient(t *testing.T, timeout time.Duration, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return newClient(server.URL, timeout, slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
}

// respondJSON 返回固定状态码与 JSON 正文的处理器。
func respondJSON(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func requireFailure(t *testing.T, err error, status string, httpStatus int) *Failure {
	t.Helper()
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("期望 *Failure，实际 %T: %v", err, err)
	}
	if failure.Status != status {
		t.Fatalf("status = %q，期望 %q（message=%q）", failure.Status, status, failure.Message)
	}
	if failure.HTTPStatus != httpStatus {
		t.Fatalf("http_status = %d，期望 %d", failure.HTTPStatus, httpStatus)
	}
	if failure.Message == "" {
		t.Fatal("失败必须带可展示的 message")
	}
	return failure
}

func TestLatestParsesReleaseAndSendsHeaders(t *testing.T) {
	var gotPath, gotAccept, gotAgent, gotAPIVersion, gotAuth string
	client := newTestClient(t, DefaultRequestTimeout, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		gotAgent = r.Header.Get("User-Agent")
		gotAPIVersion = r.Header.Get("X-GitHub-Api-Version")
		gotAuth = r.Header.Get("Authorization")
		respondJSON(http.StatusOK, latestBody)(w, r)
	})

	release, err := client.Latest(context.Background(), testRepo, "tok-123")
	if err != nil {
		t.Fatalf("Latest 失败: %v", err)
	}
	if gotPath != "/repos/LangfordKuo/Lyidc_OEM/releases/latest" {
		t.Errorf("请求路径 = %q", gotPath)
	}
	if gotAccept != "application/vnd.github+json" || gotAPIVersion != "2022-11-28" {
		t.Errorf("Accept / X-GitHub-Api-Version 不符: %q / %q", gotAccept, gotAPIVersion)
	}
	if gotAgent != "lyidc-oem/test" {
		t.Errorf("User-Agent = %q", gotAgent)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("配置了 Token 时必须带 Bearer 头，实际 %q", gotAuth)
	}
	if release.TagName != "v0.10.0" || release.Name != "v0.10.0" || release.Notes != "修复若干问题" ||
		release.NotesTruncated || release.PublishedAt != "2026-10-10T12:00:00Z" {
		t.Errorf("release 字段解析不符: %+v", release)
	}
	if len(release.Assets) != 2 || release.Assets[0].DownloadURL != "https://example.test/linux.tar.gz" ||
		release.Assets[0].Size != 12345 {
		t.Errorf("assets 解析不符: %+v", release.Assets)
	}
}

func TestLatestWithoutTokenSendsNoAuthorization(t *testing.T) {
	var gotAuth string
	client := newTestClient(t, DefaultRequestTimeout, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		respondJSON(http.StatusOK, latestBody)(w, r)
	})
	if _, err := client.Latest(context.Background(), testRepo, ""); err != nil {
		t.Fatalf("Latest 失败: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("未配置 Token 时不应发送 Authorization，实际 %q", gotAuth)
	}
}

func TestLatestFailureStatuses(t *testing.T) {
	cases := []struct {
		name       string
		httpStatus int
		status     string
	}{
		{"无正式发布（404）", http.StatusNotFound, StatusNoRelease},
		{"Token 无效（401）", http.StatusUnauthorized, StatusUnauthorized},
		{"限流（403）", http.StatusForbidden, StatusRateLimited},
		{"限流（429）", http.StatusTooManyRequests, StatusRateLimited},
		{"上游 500", http.StatusInternalServerError, StatusUnreachable},
		{"上游 502", http.StatusBadGateway, StatusUnreachable},
		{"非预期状态码", http.StatusTeapot, StatusBadResponse},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := newTestClient(t, DefaultRequestTimeout, respondJSON(c.httpStatus, `{"message":"x"}`))
			_, err := client.Latest(context.Background(), testRepo, "")
			requireFailure(t, err, c.status, c.httpStatus)
		})
	}
}

func TestLatestNoReleaseMessageNamesRepo(t *testing.T) {
	client := newTestClient(t, DefaultRequestTimeout, respondJSON(http.StatusNotFound, `{}`))
	_, err := client.Latest(context.Background(), testRepo, "")
	failure := requireFailure(t, err, StatusNoRelease, http.StatusNotFound)
	if !strings.Contains(failure.Message, testRepo) {
		t.Errorf("no_release 的 message 应包含仓库名，实际 %q", failure.Message)
	}
}

func TestLatestBadResponses(t *testing.T) {
	cases := map[string]string{
		"非法 JSON":     `{not json`,
		"缺少 tag_name": `{"name":"x"}`,
		"空 tag_name":  `{"tag_name":"   "}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			client := newTestClient(t, DefaultRequestTimeout, respondJSON(http.StatusOK, body))
			_, err := client.Latest(context.Background(), testRepo, "")
			requireFailure(t, err, StatusBadResponse, http.StatusOK)
		})
	}
}

func TestLatestRejectsOversizedBody(t *testing.T) {
	oversized := `{"tag_name":"v1.0.0","body":"` + strings.Repeat("a", maxResponseBytes) + `"}`
	client := newTestClient(t, DefaultRequestTimeout, respondJSON(http.StatusOK, oversized))
	_, err := client.Latest(context.Background(), testRepo, "")
	requireFailure(t, err, StatusBadResponse, http.StatusOK)
}

func TestLatestTimeout(t *testing.T) {
	client := newTestClient(t, 50*time.Millisecond, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	})
	_, err := client.Latest(context.Background(), testRepo, "")
	requireFailure(t, err, StatusTimeout, 0)
}

func TestLatestUnreachable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close() // 关闭后端口不可达，模拟网络不通
	client := newClient(url, DefaultRequestTimeout, slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	_, err := client.Latest(context.Background(), testRepo, "")
	requireFailure(t, err, StatusUnreachable, 0)
}

func TestTruncateNotesKeepsUTF8Boundary(t *testing.T) {
	text := strings.Repeat("中", 8000) // 每个汉字 3 字节，共 24000 字节
	truncated, ok := truncateNotes(text, MaxNotesBytes)
	if !ok {
		t.Fatal("超过上限时必须标记截断")
	}
	if len(truncated) > MaxNotesBytes {
		t.Errorf("截断后长度 %d 超过上限 %d", len(truncated), MaxNotesBytes)
	}
	if !utf8.ValidString(truncated) {
		t.Error("截断结果必须是合法 UTF-8（不能切坏多字节字符）")
	}

	short, ok := truncateNotes("说明", MaxNotesBytes)
	if ok || short != "说明" {
		t.Errorf("未超限时应原样返回，实际 %q / %v", short, ok)
	}
}
