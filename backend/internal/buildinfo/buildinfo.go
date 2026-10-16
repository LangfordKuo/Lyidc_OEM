// Package buildinfo 承载构建期注入的版本、上游地址与更新源信息（契约 19.1 / 8.6 / 19.5）。
//
// 注入方式：go build -ldflags "-X github.com/LangfordKuo/Lyidc_OEM/backend/internal/buildinfo.Version=v1.0.0"
// （Commit / BuiltAt / UpstreamBaseURL / UpdatesAPIBase / UpdatesDownloadBase 同理）。
// 未注入时保持缺省值，本地 go build 照常编译运行，此时即为开发构建。
// 运行时只读这些变量，绝不在代码里写死版本号、上游地址或更新源地址。
package buildinfo

import "fmt"

var (
	// Version 是发布版本，与 tag 一致（如 v1.0.0）；缺省 dev 表示开发构建。
	Version = "dev"
	// Commit 是构建所用提交的短哈希；缺省 unknown。
	Commit = "unknown"
	// BuiltAt 是构建时间（RFC3339，UTC）；缺省 unknown。
	BuiltAt = "unknown"
	// UpstreamBaseURL 是上游「魔方财务系统」的地址（含 http(s)://，不带结尾斜杠）。
	//
	// R17 起上游地址是**构建期固定项**：与版本号同一套 -ldflags 注入机制，运行时只读，
	// 任何界面与接口都无法修改（改了就能把整站指向同行）。缺省空串表示构建时未注入，
	// 此时上游相关功能一律按「未配置」处理，其余功能不受影响。
	UpstreamBaseURL = ""
	// UpdatesAPIBase 是「检查更新」访问的 API 根地址（含 https://，不带结尾斜杠）。
	//
	// P2 起更新源也是**构建期固定项**：它决定这台服务器从哪里取版本信息，以及（配合
	// UpdatesDownloadBase）从哪里下载待执行的发布包——把地址留在后台可改，等于把「往本机
	// 装什么二进制」的权限交给能改设置的人。运行时只读，任何界面与接口都改不了。
	//
	// 与上游地址不同，**缺省（空串）不是「功能不可用」**：地址不是秘密，未注入时解析为
	// 官方缺省 https://api.github.com，更新检查照常可用（见 updates.ResolveSource）。
	UpdatesAPIBase = ""
	// UpdatesDownloadBase 是资产下载基地址（含 https://，不带结尾斜杠）；缺省空串表示
	// **原样使用 Release 资产自带的 browser_download_url**，即走官方 GitHub 资产地址。
	//
	// 仅供镜像 / 内网代理由构建方固定；注入后同时成为下载地址白名单的一员（契约 20.7 第 8 条）。
	UpdatesDownloadBase = ""
)

// Summary 返回 -version 参数与启动日志共用的一行描述。
func Summary() string {
	return fmt.Sprintf("version=%s commit=%s built_at=%s", Version, Commit, BuiltAt)
}
