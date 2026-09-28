package bot

import "strings"

// 访问拒绝原因（结构化输出，日志与提示帧共用）。
const (
	AccessReasonPolicyInvalid    = "policy-invalid"     // 策略 JSON 损坏 / mode 非法 → fail closed
	AccessReasonSenderMissing    = "sender-missing"     // 发送者身份缺失（无法归属，一律拒绝）
	AccessReasonSenderNotAllowed = "sender-not-allowed" // allowlist 未命中
)

// EvaluateAccess 访问白名单纯函数判定（fail closed）：open 放行；allowlist 比对 SenderID；
// 发送者身份缺失 / mode 非法 / 策略损坏一律拒绝。返回 (是否放行, 拒绝原因)。
// 单聊/群聊共用一套策略（按发送者判定）；拒绝时的提示/静默分流在 orchestrator（提示语义）。
func EvaluateAccess(policy AccessPolicy, senderID string) (bool, string) {
	if strings.TrimSpace(senderID) == "" {
		return false, AccessReasonSenderMissing
	}
	switch policy.Mode {
	case AccessModeOpen:
		return true, ""
	case AccessModeAllowlist:
		for _, u := range policy.AllowUsers {
			if strings.TrimSpace(u) == senderID {
				return true, ""
			}
		}
		return false, AccessReasonSenderNotAllowed
	default:
		return false, AccessReasonPolicyInvalid
	}
}
