package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// KeyUpdates 系统更新源设置键（值结构见 Updates，契约 19.5）。
const KeyUpdates = "updates.github"

// DefaultUpdatesRepo 是缺省更新仓库，即本项目的公开仓库（契约 19.5）。
const DefaultUpdatesRepo = "LangfordKuo/Lyidc_OEM"

// DefaultUpdatesAPIBase 是缺省 GitHub API 根地址（契约 19.5）。
const DefaultUpdatesAPIBase = "https://api.github.com"

// maxUpdatesTokenLen 是 GitHub Token 的长度上限（契约 19.5）。
const maxUpdatesTokenLen = 255

// GitHub 用户名与仓库名的字符集（owner 允许连字符，name 允许 . 与 _）。
var (
	updatesOwnerPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	updatesNamePattern  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// Updates 是 updates.github 设置的取值结构（系统更新源，契约 19.5）。
//
// GitHubToken 可选，只存数据库与进程内存，对外一律掩码（MaskSecret）；
// 它只用于提高 GitHub API 额度，不参与任何其他用途。
type Updates struct {
	// Repo 是检查更新所用的 GitHub 仓库，形如 owner/name。
	Repo string `json:"repo"`
	// GitHubToken 可选的 GitHub Token；空串表示未配置（走未鉴权额度）。
	GitHubToken string `json:"github_token"`
	// APIBase 可选：检查更新访问的 API 根地址，空串表示用缺省 DefaultUpdatesAPIBase（契约 19.5）。
	APIBase string `json:"api_base"`
	// DownloadBase 可选：资产下载基地址，空串表示原样使用 Release 里的 download_url（契约 20.8）。
	DownloadBase string `json:"download_base"`
}

// UpdatesUpdate 是 PUT /api/v1/admin/settings/updates 的更新意图：nil 表示不修改。
// GitHubToken 三态语义与支付 key 一致：省略 = 保持、新值 = 替换、空串 = 清空；
// APIBase / DownloadBase **非三态**：省略 = 不变、空串 = 清除。
type UpdatesUpdate struct {
	Repo         *string
	GitHubToken  *string
	APIBase      *string
	DownloadBase *string
}

// Apply 把更新意图合并到当前值并整体校验。
func (u Updates) Apply(upd UpdatesUpdate) (Updates, error) {
	merged := u
	if upd.Repo != nil {
		merged.Repo = strings.TrimSpace(*upd.Repo)
	}
	if upd.GitHubToken != nil {
		merged.GitHubToken = strings.TrimSpace(*upd.GitHubToken)
	}
	if upd.APIBase != nil {
		merged.APIBase = strings.TrimSpace(*upd.APIBase)
	}
	if upd.DownloadBase != nil {
		merged.DownloadBase = strings.TrimSpace(*upd.DownloadBase)
	}
	if err := merged.Validate(); err != nil {
		return Updates{}, err
	}
	return merged, nil
}

// Validate 校验取值：repo 必须是 owner/name；token 非空时长度不超过 255 且不含空白与控制字符；
// 两个地址字段要么为空，要么是 http(s) 绝对地址（契约 19.5）。
func (u Updates) Validate() error {
	if err := ValidateUpdatesRepo(u.Repo); err != nil {
		return err
	}
	if err := validateUpdatesToken(u.GitHubToken); err != nil {
		return err
	}
	if err := ValidateUpdatesBaseURL("api_base", u.APIBase); err != nil {
		return err
	}
	return ValidateUpdatesBaseURL("download_base", u.DownloadBase)
}

// APIBaseOr 返回生效的 API 根地址（未配置时取缺省）。
func (u Updates) APIBaseOr() string {
	if strings.TrimSpace(u.APIBase) == "" {
		return DefaultUpdatesAPIBase
	}
	return u.APIBase
}

// ValidateUpdatesBaseURL 校验可注入的出网地址：留空合法；否则必须是 http(s) 绝对地址，
// 不带查询串、片段与用户信息，且不以 / 结尾（拼路径时不产生双斜杠）。
func ValidateUpdatesBaseURL(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("%w: %s 不是合法地址", ErrFormat, field)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: %s 只支持 http/https 地址", ErrFormat, field)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%w: %s 缺少主机名", ErrFormat, field)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return fmt.Errorf("%w: %s 不能带查询串、片段或用户信息", ErrFormat, field)
	}
	if strings.HasSuffix(parsed.Path, "/") {
		return fmt.Errorf("%w: %s 不能以 / 结尾", ErrFormat, field)
	}
	return nil
}

// Encode 返回落库用的 JSON 文本。
func (u Updates) Encode() (string, error) { return encode(u) }

// ValidateUpdatesRepo 校验仓库标识是否为 owner/name 形式。
func ValidateUpdatesRepo(repo string) error {
	owner, name, found := strings.Cut(repo, "/")
	if !found || owner == "" || name == "" ||
		!updatesOwnerPattern.MatchString(owner) || len(owner) > 39 ||
		!updatesNamePattern.MatchString(name) || len(name) > 100 ||
		name == "." || name == ".." {
		return fmt.Errorf("%w: repo 需为 owner/name 形式（owner 为字母数字与连字符，name 为字母数字与 ._-），收到 %q",
			ErrFormat, repo)
	}
	return nil
}

// validateUpdatesToken 校验 GitHub Token 的形状（留空合法；错误信息不回显 Token 内容）。
func validateUpdatesToken(token string) error {
	if token == "" {
		return nil
	}
	if len(token) > maxUpdatesTokenLen {
		return fmt.Errorf("%w: github_token 长度不能超过 %d 字符", ErrFormat, maxUpdatesTokenLen)
	}
	for _, r := range token {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("%w: github_token 不能包含空白或控制字符", ErrFormat)
		}
	}
	return nil
}

// decodeUpdates 解析库内 JSON；空值按未配置处理（仓库取缺省值）。
func decodeUpdates(raw string) (Updates, error) {
	value := Updates{Repo: DefaultUpdatesRepo}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return value, nil
	}
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return Updates{}, fmt.Errorf("%w: %s 的值不是合法 JSON：%v", ErrCorrupt, KeyUpdates, err)
	}
	value.Repo = strings.TrimSpace(value.Repo)
	if value.Repo == "" {
		value.Repo = DefaultUpdatesRepo
	}
	if err := ValidateUpdatesRepo(value.Repo); err != nil {
		return Updates{}, fmt.Errorf("%w: %s 的 repo 值不合法", ErrCorrupt, KeyUpdates)
	}
	value.GitHubToken = strings.TrimSpace(value.GitHubToken)
	value.APIBase = strings.TrimSpace(value.APIBase)
	value.DownloadBase = strings.TrimSpace(value.DownloadBase)
	for field, address := range map[string]string{"api_base": value.APIBase, "download_base": value.DownloadBase} {
		if err := ValidateUpdatesBaseURL(field, address); err != nil {
			return Updates{}, fmt.Errorf("%w: %s 的 %s 值不合法", ErrCorrupt, KeyUpdates, field)
		}
	}
	return value, nil
}

// UpdatesState 是 updates.github 的读取结果。
type UpdatesState struct {
	Value Updates
	Meta  Meta
}

// Updates 读取 updates.github 设置；未配置时返回缺省仓库与空 Token。
func (r *Reader) Updates(ctx context.Context) (UpdatesState, error) {
	raw, meta, err := r.raw(ctx, KeyUpdates)
	if err != nil {
		return UpdatesState{}, err
	}
	value, err := decodeUpdates(raw)
	if err != nil {
		return UpdatesState{}, err
	}
	return UpdatesState{Value: value, Meta: meta}, nil
}
