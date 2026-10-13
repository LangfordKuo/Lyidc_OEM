package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// 状态取值（契约 19.3）。
const (
	StatusOK           = "ok"
	StatusNotChecked   = "not_checked"
	StatusNoRelease    = "no_release"
	StatusRateLimited  = "rate_limited"
	StatusUnauthorized = "unauthorized"
	StatusTimeout      = "timeout"
	StatusUnreachable  = "unreachable"
	StatusBadResponse  = "bad_response"
)

// 面向管理员的说明文案（契约 19.3 的 message 口径）。
const (
	messageNotChecked      = "尚未检查更新"
	messageRateLimited     = "GitHub 访问次数已达上限，请稍后重试；可在更新源设置中配置 GitHub Token 提高额度"
	messageUnauthorized    = "GitHub Token 无效或已过期，请在更新源设置中更新"
	messageTimeout         = "连接 GitHub 超时，请稍后重试"
	messageUnreachable     = "无法连接 GitHub 或其服务暂不可用，请稍后重试"
	messageBadResponse     = "GitHub 返回的数据无法解析"
	messageDevBuild        = "当前为开发构建（未注入版本号），无法比较版本"
	messageUnknownVersion  = "无法解析版本号，无法比较"
	messageNoReleaseFormat = "仓库 %s 暂无正式发布版本"
)

const (
	defaultGitHubAPIBase = "https://api.github.com"
	// DefaultRequestTimeout 是单次检查访问 GitHub 的超时（契约 19.4）。
	DefaultRequestTimeout = 8 * time.Second
	// maxResponseBytes 是 GitHub 响应体的读取上限（契约 19.4）。
	maxResponseBytes = 1 << 20
	// MaxNotesBytes 是 Release 说明的存储与展示上限（契约 19.4）。
	MaxNotesBytes = 16 << 10
)

// Release 是最新正式版的摘要（写入缓存，契约 19.4）。
type Release struct {
	TagName        string  `json:"tag_name"`
	Name           string  `json:"name"`
	PublishedAt    string  `json:"published_at"`
	Notes          string  `json:"notes"`
	NotesTruncated bool    `json:"notes_truncated"`
	HTMLURL        string  `json:"html_url"`
	Assets         []Asset `json:"assets"`
}

// Asset 是 Release 的一个产物。
type Asset struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"download_url"`
}

// Failure 是一次检查失败的分类结果：Status 取契约 19.3 的失败态，Message 为面向管理员的说明。
// 调用方用 errors.As 取出；Message 不含 Token。
type Failure struct {
	Status     string
	Message    string
	HTTPStatus int // 上游 HTTP 状态码；0 表示没有拿到响应
	cause      error
}

func (f *Failure) Error() string { return f.Message }

// Unwrap 返回底层原因（网络错误或解析错误），便于日志与测试判定。
func (f *Failure) Unwrap() error { return f.cause }

// Fetcher 是「获取仓库最新正式版」的能力；生产实现为 Client，测试可替换。
type Fetcher interface {
	Latest(ctx context.Context, repo, token string) (Release, error)
}

// Client 访问 GitHub Releases API（契约 19.4 出网约束）。
type Client struct {
	baseURL string
	http    *http.Client
	logger  *slog.Logger
	agent   string
}

// NewClient 构造生产用客户端：访问 api.github.com，超时 DefaultRequestTimeout。
// version 只用于 User-Agent，不影响请求语义。
func NewClient(logger *slog.Logger, version string) *Client {
	return newClient(defaultGitHubAPIBase, DefaultRequestTimeout, logger, version)
}

// newClient 允许测试指定地址与超时（httptest 服务器与短超时用例）。
func newClient(baseURL string, timeout time.Duration, logger *slog.Logger, version string) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
		logger:  logger,
		agent:   "lyidc-oem/" + version,
	}
}

// latestPayload 是 GET /repos/{owner}/{repo}/releases/latest 响应中本功能用到的字段。
type latestPayload struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	Assets      []struct {
		Name        string `json:"name"`
		Size        int64  `json:"size"`
		DownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// Latest 获取仓库最新正式版。任何失败都返回 *Failure（Status 为契约 19.3 的失败态），
// 绝不返回未分类的裸错误，调用方据此写入缓存与展示。
func (c *Client) Latest(ctx context.Context, repo, token string) (Release, error) {
	target := c.baseURL + "/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Release{}, &Failure{Status: StatusBadResponse, Message: messageBadResponse, cause: err}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.agent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		failure := transportFailure(err)
		c.logger.Warn("检查更新：请求 GitHub 失败", "url", target, "status", failure.Status, "error", err)
		return Release{}, failure
	}
	defer resp.Body.Close()

	if failure := statusFailure(resp.StatusCode, repo); failure != nil {
		c.logger.Warn("检查更新：GitHub 返回失败", "url", target,
			"http_status", resp.StatusCode, "status", failure.Status)
		return Release{}, failure
	}
	c.logger.Info("检查更新：GitHub 响应成功", "url", target, "http_status", resp.StatusCode)

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		if isTimeout(err) {
			return Release{}, &Failure{Status: StatusTimeout, Message: messageTimeout, HTTPStatus: resp.StatusCode, cause: err}
		}
		return Release{}, &Failure{Status: StatusBadResponse, Message: messageBadResponse, HTTPStatus: resp.StatusCode, cause: err}
	}
	if len(body) > maxResponseBytes {
		return Release{}, &Failure{Status: StatusBadResponse, Message: messageBadResponse, HTTPStatus: resp.StatusCode,
			cause: fmt.Errorf("响应体超过 %d 字节", maxResponseBytes)}
	}

	var payload latestPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return Release{}, &Failure{Status: StatusBadResponse, Message: messageBadResponse, HTTPStatus: resp.StatusCode, cause: err}
	}
	if strings.TrimSpace(payload.TagName) == "" {
		return Release{}, &Failure{Status: StatusBadResponse, Message: messageBadResponse, HTTPStatus: resp.StatusCode,
			cause: errors.New("响应缺少 tag_name")}
	}

	notes, truncated := truncateNotes(payload.Body, MaxNotesBytes)
	release := Release{
		TagName:        payload.TagName,
		Name:           payload.Name,
		PublishedAt:    payload.PublishedAt,
		Notes:          notes,
		NotesTruncated: truncated,
		HTMLURL:        payload.HTMLURL,
		Assets:         make([]Asset, 0, len(payload.Assets)),
	}
	for _, asset := range payload.Assets {
		release.Assets = append(release.Assets, Asset{
			Name:        asset.Name,
			Size:        asset.Size,
			DownloadURL: asset.DownloadURL,
		})
	}
	return release, nil
}

// statusFailure 把非 200 的 HTTP 状态映射为失败态（契约 19.3）；200 返回 nil。
func statusFailure(code int, repo string) *Failure {
	switch {
	case code == http.StatusOK:
		return nil
	case code == http.StatusNotFound:
		return &Failure{Status: StatusNoRelease, Message: fmt.Sprintf(messageNoReleaseFormat, repo), HTTPStatus: code}
	case code == http.StatusUnauthorized:
		return &Failure{Status: StatusUnauthorized, Message: messageUnauthorized, HTTPStatus: code}
	case code == http.StatusForbidden, code == http.StatusTooManyRequests:
		return &Failure{Status: StatusRateLimited, Message: messageRateLimited, HTTPStatus: code}
	case code >= http.StatusInternalServerError:
		return &Failure{Status: StatusUnreachable, Message: messageUnreachable, HTTPStatus: code}
	default:
		return &Failure{Status: StatusBadResponse, Message: messageBadResponse, HTTPStatus: code,
			cause: fmt.Errorf("GitHub 返回非预期状态码 %d", code)}
	}
}

// transportFailure 把请求级错误分为超时与不可达两类。
func transportFailure(err error) *Failure {
	if isTimeout(err) {
		return &Failure{Status: StatusTimeout, Message: messageTimeout, cause: err}
	}
	return &Failure{Status: StatusUnreachable, Message: messageUnreachable, cause: err}
}

// isTimeout 判断错误是否由超时引起（http.Client.Timeout、上下文截止时间或底层网络超时）。
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// truncateNotes 按 UTF-8 字符边界把说明截断到 limit 字节以内（契约 19.4）。
func truncateNotes(text string, limit int) (string, bool) {
	if len(text) <= limit {
		return text, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut], true
}
