// Package buildinfo 承载构建期注入的版本信息（契约 19.1）。
//
// 注入方式：go build -ldflags "-X github.com/LangfordKuo/Lyidc_OEM/backend/internal/buildinfo.Version=v1.0.0"
// （Commit / BuiltAt 同理）。未注入时保持缺省值，本地 go build 照常编译运行，此时即为开发构建。
// 运行时只读这三个变量，绝不在代码里写死版本号。
package buildinfo

import "fmt"

var (
	// Version 是发布版本，与 tag 一致（如 v1.0.0）；缺省 dev 表示开发构建。
	Version = "dev"
	// Commit 是构建所用提交的短哈希；缺省 unknown。
	Commit = "unknown"
	// BuiltAt 是构建时间（RFC3339，UTC）；缺省 unknown。
	BuiltAt = "unknown"
)

// Summary 返回 -version 参数与启动日志共用的一行描述。
func Summary() string {
	return fmt.Sprintf("version=%s commit=%s built_at=%s", Version, Commit, BuiltAt)
}
