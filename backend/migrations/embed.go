// Package migrations 以 embed 方式内嵌 golang-migrate 风格的 SQL 迁移文件。
package migrations

import "embed"

// FS 内嵌 backend/migrations 下的全部 SQL 迁移文件。
//
//go:embed *.sql
var FS embed.FS
