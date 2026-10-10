// node 版本预检（node 解析职责面的另一半）：ResolveNodeBin 负责定位 node 可执行
// 文件，本文件负责其版本探测与下限校验，两者同属 agentcatalog 单一 SSOT。browser
// 域引擎拉起消费；acpdoctor 存量仿写随后续任务顺带迁移（见任务文档 §5 扩展点）。
package agentcatalog

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// NodeVersion 执行 `node --version`（以解析到的绝对路径执行，防 PATH 漂移），返回形如
// v22.14.0 的版本串。
func NodeVersion(ctx context.Context, nodeBin string) (string, error) {
	out, err := exec.CommandContext(ctx, nodeBin, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("node 版本探测失败: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// CheckNodeMinVersion node 主版本下限校验：返回空串 = 通过；非空 = 用户可读原因。
// min 为空 = 不校验（调用方语义）；任一侧无法解析即落败（宁失败不静默，与 catalog
// parse 同哲学——产物缺陷不该被静默放行）。
func CheckNodeMinVersion(version, min string) string {
	if min == "" {
		return ""
	}
	major, ok := majorFromVersion(version)
	if !ok {
		return fmt.Sprintf("node 版本输出无法解析: %q", version)
	}
	minMajor, ok := majorFromVersion(min)
	if !ok {
		return fmt.Sprintf("nodeMinVersion 无法解析: %q", min)
	}
	if major < minMajor {
		return fmt.Sprintf("node 版本过低（%s）：需要 Node.js ≥%s，请升级后重试", version, min)
	}
	return ""
}

// majorFromVersion 取主版本号（容忍 v 前缀与任意后缀，如 v22.14.0 → 22）；无法解析
// 返回 false。
func majorFromVersion(version string) (int, bool) {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if dot := strings.IndexByte(v, '.'); dot >= 0 {
		v = v[:dot]
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
