package updates

import (
	"fmt"
	"net/url"
	"strings"
)

// OfficialAPIBase 是官方缺省的更新源 API 根地址（契约 19.5）：构建期未注入 UpdatesAPIBase
// 时用它。更新源不是秘密，缺省即回退官方地址，功能照常可用——这与上游地址「未注入即不可用」
// 的口径刻意不同。
const OfficialAPIBase = "https://api.github.com"

// githubDownloadHosts 是资产下载**恒允许**的官方域（契约 20.7 第 8 条）。
// 只列 GitHub 自己会出现在 Release 资产链路上的主机：API 主机、网页主机、
// 301 之后的资产主机与 codeload（tarball 源码包）。
var githubDownloadHosts = []string{
	"github.com",
	"api.github.com",
	"objects.githubusercontent.com",
	"codeload.github.com",
}

// Source 是更新源的**生效取值**（契约 19.5）：构建期固定，运行时任何接口与界面都改不了。
//
// 它是「读哪里」与「从哪里下载」的唯一来源：settings 表里的 updates.github 只承载
// repo 与 github_token（凭据可改、地址固定），库内遗留的 api_base / download_base 一律不读。
type Source struct {
	// APIBase 是生效的 API 根地址，恒非空（未注入时已回退 OfficialAPIBase）。
	APIBase string
	// DownloadBase 是生效的资产下载基地址；空串表示原样使用 Release 资产自带的
	// browser_download_url（契约 20.8）。
	DownloadBase string
}

// ResolveSource 把构建期注入的两个变量解析成生效更新源（契约 19.5）：
// api_base 未注入时回退 OfficialAPIBase；download_base 未注入时留空。
// 两个变量都按去首尾空白处理，避免 ldflags 里的空白把地址带歪。
func ResolveSource(injectedAPIBase, injectedDownloadBase string) Source {
	apiBase := strings.TrimSpace(injectedAPIBase)
	if apiBase == "" {
		apiBase = OfficialAPIBase
	}
	return Source{
		APIBase:      strings.TrimRight(apiBase, "/"),
		DownloadBase: strings.TrimRight(strings.TrimSpace(injectedDownloadBase), "/"),
	}
}

// ValidateDownloadURL 校验一个**下载地址**（发布包与它的 .sha256）是否允许访问
// （契约 20.7 第 8 条，apply 前的二次校验，纵深防御）：
//
//   - 必须是绝对地址，且协议为 https——http 明文下载等于把「装什么二进制」交给链路上任何人；
//   - host 必须在白名单内：固定的 GitHub 官方域 ∪ 构建期注入的 download_base 的 host。
//
// 不匹配即返回可读错误，调用方据此拒绝并记 WARN。sha256 校验的口径不变（20.7 第 2 条）：
// 它只保证「与更新源声明的一致」，白名单负责先把「更新源是谁」钉死。
func (s Source) ValidateDownloadURL(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("更新源下载地址不是合法的绝对地址")
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("更新源下载地址必须使用 https，实际为 %s://%s", parsed.Scheme, parsed.Host)
	}
	host := strings.ToLower(parsed.Host)
	for _, allowed := range s.downloadHosts() {
		if host == allowed {
			return nil
		}
	}
	return fmt.Errorf("更新源下载地址的主机 %s 不在允许范围内（仅允许 GitHub 官方域与构建期注入的下载地址）", host)
}

// DownloadHosts 返回生效的下载主机白名单（只读副本，供日志与测试核对）。
func (s Source) DownloadHosts() []string {
	hosts := s.downloadHosts()
	return append([]string(nil), hosts...)
}

// downloadHosts 组装白名单：GitHub 官方域 + 构建期注入的 download_base 的 host（若注入且可解析）。
func (s Source) downloadHosts() []string {
	hosts := make([]string, 0, len(githubDownloadHosts)+1)
	hosts = append(hosts, githubDownloadHosts...)
	if injected := hostOf(s.DownloadBase); injected != "" {
		hosts = append(hosts, injected)
	}
	return hosts
}

// hostOf 取出地址的 host（小写，含端口）；空串或不可解析时返回空串。
func hostOf(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Host)
}
