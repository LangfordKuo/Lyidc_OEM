package updates

import (
	"strings"
	"testing"
)

// TestResolveSourceFallsBackToOfficialAPIBase 覆盖「未注入时的行为」（契约 19.5）：
// api_base 回退官方缺省（地址不是秘密，更新检查照常可用）；download_base 留空表示
// 原样使用 Release 资产自带的 browser_download_url。
func TestResolveSourceFallsBackToOfficialAPIBase(t *testing.T) {
	source := ResolveSource("", "")
	if source.APIBase != OfficialAPIBase {
		t.Fatalf("未注入 api_base 应回退 %q，实际 %q", OfficialAPIBase, source.APIBase)
	}
	if source.DownloadBase != "" {
		t.Fatalf("未注入 download_base 应留空，实际 %q", source.DownloadBase)
	}

	// 只注入 api_base 时 download_base 仍为空（两者独立）。
	onlyAPI := ResolveSource("https://api.github.example/v3", "")
	if onlyAPI.APIBase != "https://api.github.example/v3" || onlyAPI.DownloadBase != "" {
		t.Fatalf("只注入 api_base 时结果不符：%+v", onlyAPI)
	}
}

// TestResolveSourceTrimsInjectedValues 覆盖注入值的归一：首尾空白去掉、结尾斜杠去掉，
// 避免拼路径时出现双斜杠或带空白的 host。
func TestResolveSourceTrimsInjectedValues(t *testing.T) {
	source := ResolveSource("  https://api.github.example/v3/  ", " https://mirror.example.com/dl/ ")
	if source.APIBase != "https://api.github.example/v3" {
		t.Fatalf("api_base 归一结果不符：%q", source.APIBase)
	}
	if source.DownloadBase != "https://mirror.example.com/dl" {
		t.Fatalf("download_base 归一结果不符：%q", source.DownloadBase)
	}
	// 纯空白等于未注入。
	if whitespace := ResolveSource("   ", ""); whitespace.APIBase != OfficialAPIBase {
		t.Fatalf("纯空白应视为未注入，实际 %q", whitespace.APIBase)
	}
}

// TestValidateDownloadURLAllowsGitHubHosts 覆盖白名单的官方域（契约 20.7 第 8 条）。
func TestValidateDownloadURLAllowsGitHubHosts(t *testing.T) {
	source := ResolveSource("", "")
	allowed := []string{
		"https://github.com/LangfordKuo/Lyidc_OEM/releases/download/v1.0.0/lyidc-oem-v1.0.0-linux-amd64.tar.gz",
		"https://github.com/LangfordKuo/Lyidc_OEM/releases/download/v1.0.0/lyidc-oem-v1.0.0-linux-amd64.tar.gz.sha256",
		"https://api.github.com/repos/LangfordKuo/Lyidc_OEM/releases/assets/1",
		"https://objects.githubusercontent.com/github-production-release-asset/1?X-Amz-Signature=abc",
		"https://codeload.github.com/LangfordKuo/Lyidc_OEM/tar.gz/refs/tags/v1.0.0",
		"  https://github.com/a/b/releases/download/v1/a.tar.gz  ",
	}
	for _, rawURL := range allowed {
		if err := source.ValidateDownloadURL(rawURL); err != nil {
			t.Errorf("应允许下载 %q，实际被拒：%v", rawURL, err)
		}
	}
}

// TestValidateDownloadURLRejectsNonAllowlisted 覆盖拒绝口径：明文 http、非白名单域、
// 前缀混淆域（github.com.evil.example）、非绝对地址一律拒绝，且错误可读、不含查询串。
func TestValidateDownloadURLRejectsNonAllowlisted(t *testing.T) {
	source := ResolveSource("", "")
	rejected := []string{
		"http://github.com/a/b/releases/download/v1/a.tar.gz",
		"http://objects.githubusercontent.com/a.tar.gz",
		"https://evil.example.com/a.tar.gz",
		"https://github.com.evil.example.com/a.tar.gz",
		"https://api.github.com.evil.example/repos/a/b",
		"https://127.0.0.1:8080/a.tar.gz",
		"ftp://github.com/a.tar.gz",
		"/releases/download/v1/a.tar.gz",
		"releases/download/v1/a.tar.gz",
		"",
	}
	for _, rawURL := range rejected {
		err := source.ValidateDownloadURL(rawURL)
		if err == nil {
			t.Errorf("应拒绝下载 %q", rawURL)
			continue
		}
		if strings.Contains(err.Error(), "X-Amz-Signature") {
			t.Errorf("拒绝说明不得回显查询串：%v", err)
		}
	}
}

// TestValidateDownloadURLAllowsInjectedDownloadHost 覆盖「构建期注入的 download_base 的 host
// 也在白名单内」：镜像 / 内网代理可用，但仅限注入过的那个 host，且仍然必须 https。
func TestValidateDownloadURLAllowsInjectedDownloadHost(t *testing.T) {
	source := ResolveSource("", "https://mirror.example.com")
	if err := source.ValidateDownloadURL("https://mirror.example.com/dl/lyidc-oem-v1.0.0-linux-amd64.tar.gz"); err != nil {
		t.Fatalf("注入的下载主机应被允许，实际被拒：%v", err)
	}
	// 明文 http 仍拒绝——http 下载等于把「装什么二进制」交给链路上任何人。
	if err := source.ValidateDownloadURL("http://mirror.example.com/dl/a.tar.gz"); err == nil {
		t.Error("注入主机上的明文 http 地址必须被拒")
	}
	// 未注入该 host 的另一次部署里，同一地址必须被拒（白名单不是全局放宽）。
	plain := ResolveSource("", "")
	if err := plain.ValidateDownloadURL("https://mirror.example.com/dl/a.tar.gz"); err == nil {
		t.Error("未注入的 host 不得进入白名单")
	}
}

// TestValidateDownloadURLKeepsInjectedPort 覆盖带端口的注入地址：白名单按 host:port 精确匹配，
// 同主机的其它端口不算命中（本地 mock 源的真实形态）。
func TestValidateDownloadURLKeepsInjectedPort(t *testing.T) {
	source := ResolveSource("", "https://127.0.0.1:8443")
	if err := source.ValidateDownloadURL("https://127.0.0.1:8443/dl/a.tar.gz"); err != nil {
		t.Fatalf("注入的主机与端口应命中白名单，实际被拒：%v", err)
	}
	if err := source.ValidateDownloadURL("https://127.0.0.1:9000/dl/a.tar.gz"); err == nil {
		t.Error("同主机的其它端口不得命中白名单")
	}
}

// TestDownloadHostsListsGitHubAndInjected 覆盖白名单的可核对输出（启动日志用它）。
func TestDownloadHostsListsGitHubAndInjected(t *testing.T) {
	plain := ResolveSource("", "")
	hosts := plain.DownloadHosts()
	for _, want := range []string{"github.com", "api.github.com", "objects.githubusercontent.com", "codeload.github.com"} {
		if !containsString(hosts, want) {
			t.Errorf("白名单缺少 %q：%v", want, hosts)
		}
	}
	if containsString(hosts, "evil.example.com") {
		t.Errorf("白名单混入了无关主机：%v", hosts)
	}

	withMirror := ResolveSource("", "https://mirror.example.com")
	if !containsString(withMirror.DownloadHosts(), "mirror.example.com") {
		t.Errorf("注入的下载主机应进入白名单：%v", withMirror.DownloadHosts())
	}
	// 返回的是副本：调用方改写不改变生效白名单。
	mutable := withMirror.DownloadHosts()
	mutable[0] = "evil.example.com"
	if withMirror.DownloadHosts()[0] == "evil.example.com" {
		t.Error("DownloadHosts 必须返回副本")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
