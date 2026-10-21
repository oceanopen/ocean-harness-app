package bot

import "strings"

/*
 * 回合失败文案分类（两引擎共用 SSOT）：额度/认证/连接三类，命中即出人话说明并标注
 * 「非 IM 服务故障」，未命中回落通用文案。关键词对错误全文小写化子串匹配、按序首个命中——
 * headless 的 stderr 摘要拼在错误串里，额度原因因此优先于进程退出的连接类命中。
 */
var turnErrorCategories = []struct {
	match    []string
	headline string
}{
	{
		match:    []string{"usage limit", "rate limit", "credit balance", "quota", "429"},
		headline: "💰 模型额度受限或被限流，回合被模型服务中断（非 IM 服务故障）。等待额度恢复后重新发送即可。",
	},
	{
		match:    []string{"未登录", "auth required", "api key", "logged out", "unauthorized", "authentication"},
		headline: "🔑 模型认证失败（登录态或 API Key 失效，非 IM 服务故障）。请在桌面端完成 claude 登录后重试。",
	},
	{
		match:    []string{"connection closed", "connection refused", "异常退出", "已断开", "事件流已断开", "eof", "exit status"},
		headline: "🔌 模型服务连接中断（进程退出或连接丢失，非 IM 服务故障）。请重新发送消息重试。",
	},
}

// classifyTurnError 错误分类文案；未命中返回空串。
func classifyTurnError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	for _, cat := range turnErrorCategories {
		for _, kw := range cat.match {
			if strings.Contains(text, kw) {
				return cat.headline
			}
		}
	}
	return ""
}

// turnFailureReply 回合/启动失败的统一用户文案：分类说明优先，原始错误首行保底诊断。
func turnFailureReply(prefix string, err error) string {
	if err == nil {
		return prefix + "未知错误"
	}
	if headline := classifyTurnError(err); headline != "" {
		return prefix + headline + "\n\n原始错误：" + firstLine(err.Error())
	}
	return prefix + firstLine(err.Error()) + "\n请重试或调整后重新发送。"
}
