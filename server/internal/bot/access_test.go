package bot

import "testing"

func TestEvaluateAccess(t *testing.T) {
	allowAlice := AccessPolicy{Mode: AccessModeAllowlist, AllowUsers: []string{"alice", " bob "}}
	open := AccessPolicy{Mode: AccessModeOpen}
	broken := AccessPolicy{Mode: "yolo"}

	cases := []struct {
		name    string
		policy  AccessPolicy
		sender  string
		allowed bool
		reason  string
	}{
		{"open 放行", open, "anyone", true, ""},
		{"allowlist 命中", allowAlice, "alice", true, ""},
		{"allowlist 未命中", allowAlice, "mallory", false, AccessReasonSenderNotAllowed},
		{"发送者缺失一律拒绝（open 亦然）", open, "  ", false, AccessReasonSenderMissing},
		{"mode 非法 fail closed", broken, "alice", false, AccessReasonPolicyInvalid},
		{"allowlist 空表全拒", AccessPolicy{Mode: AccessModeAllowlist}, "alice", false, AccessReasonSenderNotAllowed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			allowed, reason := EvaluateAccess(c.policy, c.sender)
			if allowed != c.allowed || reason != c.reason {
				t.Fatalf("got (%v,%q), want (%v,%q)", allowed, reason, c.allowed, c.reason)
			}
		})
	}
}

func TestFormatConversationKey(t *testing.T) {
	if got := FormatConversationKey(ChatDirect, "u1"); got != "single:u1" {
		t.Fatalf("single key: %q", got)
	}
	if got := FormatConversationKey(ChatGroup, "c1"); got != "group:c1" {
		t.Fatalf("group key: %q", got)
	}
}
