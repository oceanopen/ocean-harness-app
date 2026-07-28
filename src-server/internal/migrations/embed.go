// Package migrations 通过 go:embed 将 SQL 迁移文件嵌入二进制，
// 供启动时 goose 自动迁移使用（sidecar 分发后无需外部 SQL 文件）。
package migrations

import "embed"

//go:embed migrations/*.sql
var FS embed.FS
