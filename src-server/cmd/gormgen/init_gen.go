package main

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gorm.io/gen"
	"gorm.io/gorm"

	"we-claude-terminal/go-server/internal/global"
)

// InitGen 装配 gorm/gen 生成器（适配 sqlite + 本仓库命名约定）。
//
// 关键配置：
//   - WithFileNameStrategy：去 t_ 前缀（t_workspaces → workspaces.gen.go）。
//   - WithJSONTagNameStrategy：列名 snake → JSON 小驼峰（workspace_id → workspaceId）。
func InitGen() {
	G = gen.NewGenerator(gen.Config{
		OutPath:           "./internal/dal/query",
		ModelPkgPath:      "./internal/dal/model",
		Mode:              gen.WithDefaultQuery | gen.WithQueryInterface, // gen.WithDefaultQuery：生成一个全局 Query 对象 Q | gen.WithQueryInterface：生成 Query 接口
		FieldWithIndexTag: true,                                          // 从数据库生成索引标记
		FieldWithTypeTag:  true,                                          // 从数据库生成类型标记
	})

	// 复用服务已建的 sqlite 连接做表结构 introspect。
	G.UseDB(global.SqliteDB)

	// sqlite 列类型 → Go 类型映射。sqlite 的 ColumnType.DatabaseTypeName() 返回大写
	// （如 "INTEGER"/"REAL"），gen 区分大小写按原样匹配，故键用大写。
	G.WithDataTypeMap(map[string]func(gorm.ColumnType) (dataType string){
		"INTEGER": func(gorm.ColumnType) (dataType string) { return "int" },
		"REAL":    func(gorm.ColumnType) (dataType string) { return "float64" },
	})

	// 字段类型覆盖（按列名全局匹配，跨表生效）。
	G.WithOpts(
		gen.FieldType("deleted_at", "gorm.DeletedAt"),
		gen.FieldType("is_default", "bool"),
		gen.FieldType("is_triage", "bool"),
		gen.FieldType("is_draft", "bool"),
	)

	// 生成文件名去 t_ 前缀，避免文件名带业务表前缀。
	G.WithFileNameStrategy(func(tableName string) (fileName string) {
		return strings.TrimPrefix(tableName, "t_")
	})

	// JSON 标签：列名 snake_case → 小驼峰。
	G.WithJSONTagNameStrategy(lowerCamel)
}

// lowerCamel 将 snake_case 列名转为小驼峰（workspace_id → workspaceId）。
// 列名均为 ASCII，直接做字节大小写转换，避免引入 golang.org/x/text 依赖。
func lowerCamel(col string) string {
	parts := strings.Split(strings.ToLower(strings.ReplaceAll(col, "-", "_")), "_")
	var b strings.Builder
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i == 0 {
			b.WriteString(p)
			continue
		}
		b.WriteString(cases.Title(language.Und).String(p))
	}
	return b.String()
}
