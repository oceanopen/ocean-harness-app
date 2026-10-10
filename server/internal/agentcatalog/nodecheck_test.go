package agentcatalog

import "testing"

func TestCheckNodeMinVersion(t *testing.T) {
	for version, ok := range map[string]bool{
		"v22.14.0":  true,
		"v18.0.0":   true, // 下限含等
		"v17.9.9":   false,
		"v16.20.2":  false,
		"banana":    false, // 无法解析即落败（宁失败不静默）
		"":          false,
		"22.14.0":   true, // 无 v 前缀容忍
		"v20.0.0-1": true, // 后缀容忍
	} {
		if reason := CheckNodeMinVersion(version, "18"); (reason == "") != ok {
			t.Errorf("CheckNodeMinVersion(%q) reason = %q, want ok=%v", version, reason, ok)
		}
	}
	// min 为空 = 不校验（调用方语义）。
	if reason := CheckNodeMinVersion("banana", ""); reason != "" {
		t.Errorf("min 为空应放行, reason = %q", reason)
	}
	// min 无法解析也落败。
	if reason := CheckNodeMinVersion("v22.0.0", "x"); reason == "" {
		t.Error("min 无法解析应落败")
	}
}
