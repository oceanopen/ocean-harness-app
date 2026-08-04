package main

// GenModelLocalRepository 注册本地仓库表 t_local_repositories，生成对应 DO（LocalRepository）。
//
// sub_dir_list 为 JSON 文本列，DO 字段类型保持 string（gen 默认 TEXT → string），
// 由 service 层负责 []types.RepoSubDir ↔ JSON 字符串的序列化（不在 DO 层引入自定义 Scanner/Valuer，
// 否则 model 包需 import types，而 types 已 import model，会造成循环依赖）。
func GenModelLocalRepository() {
	localRepo := G.GenerateModelAs("t_local_repositories", "LocalRepository")
	// ApplyBasic 注册到 query 层（GenerateModelAs 仅生成 model 文件，ApplyBasic 才生成 query）。
	G.ApplyBasic(localRepo)
}
