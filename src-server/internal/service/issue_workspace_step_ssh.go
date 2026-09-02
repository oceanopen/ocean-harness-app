package service

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	ssh_config "github.com/kevinburke/ssh_config"
	"go.uber.org/zap"

	"ocean-harness/src-server/internal/dal/types"
)

// issueWorkspace sshConfig step（T1.2）：根据 issue 关联仓库的 SSH URL（manifest.remoteUrl），
// 从全局 ~/.ssh/config 提取匹配的 Host 段（精确 + 通配符），生成 workspace 级
// {issueId}/.ssh/config。IdentityFile 等保持全局原路径原样转写，不复制私钥；T1.4 clone 时通过
// GIT_SSH_COMMAND="ssh -F <workspace>/.ssh/config" 环境变量指定本文件。
// 降级：全局 config 不存在/不可读、无任何匹配 Host 段 → 步骤置 SKIPPED（git clone 走默认 SSH）。

func init() {
	issueWorkspaceStepRunners[types.IW_STEP_KEY_SSH_CONFIG] = issueWorkspaceRunSSHConfig
}

// issueWorkspaceSSHGlobalConfigPath 返回全局 ssh config 路径（~/.ssh/config）。
// 包级函数变量而非内联调用，便于测试替换为临时文件路径；取不到 home 时返回空串（读取失败走降级）。
var issueWorkspaceSSHGlobalConfigPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// issueWorkspaceRunSSHConfig 生成 workspace 级 .ssh/config。终态协议：
// 生成成功 → 返回 nil（编排置 SUCCESS）；降级跳过 → 自置 SKIPPED + Message 并返回 nil；
// 全局 config 解析失败 → 返回 error（编排置 FAILED）——全局配置损坏应暴露而非静默吞掉。
func issueWorkspaceRunSSHConfig(state *types.IssueWorkspaceState, step *types.IssueWorkspaceStep, logger *zap.Logger) error {
	hosts := issueWorkspaceSSHHostsFromManifest(state.Manifest)
	globalPath := issueWorkspaceSSHGlobalConfigPath()
	raw, err := os.ReadFile(globalPath)
	if err != nil {
		step.Status = types.IW_STATUS_SKIPPED
		step.Message = "全局 ~/.ssh/config 不存在或不可读，跳过生成"
		logger.Info("[issueWorkspace] global ssh config unavailable, skip sshConfig step",
			zap.String("issueId", state.IssueID), zap.String("path", globalPath), zap.Error(err))
		return nil
	}
	content, matched, err := issueWorkspaceBuildSSHConfig(raw, hosts)
	if err != nil {
		return fmt.Errorf("解析全局 ssh config 失败: %w", err)
	}
	if !matched {
		step.Status = types.IW_STATUS_SKIPPED
		if len(hosts) == 0 {
			step.Message = "关联仓库均无可识别的 SSH URL，跳过生成"
		} else {
			step.Message = "全局 ssh config 无匹配 Host 段，跳过生成"
		}
		return nil
	}
	target := filepath.Join(state.BaseDir, state.IssueID, ".ssh", "config")
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		return fmt.Errorf("写入 .ssh/config 失败: %w", err)
	}
	return nil
}

// issueWorkspaceSSHHost 从 remote URL 提取 ssh hostname，不识别的格式返回 ""（不参与匹配）。
// 本期仅支持 scp 风格 user@host:path（实际场景唯一格式：git@github.com:org/repo.git）；
// ssh://git@host[:port]/path 等带协议前缀的格式暂不解析，待有真实需求时在此扩展。
func issueWorkspaceSSHHost(remoteURL string) string {
	if strings.Contains(remoteURL, "://") {
		return ""
	}
	at := strings.IndexByte(remoteURL, '@')
	if at < 0 {
		return ""
	}
	rest := remoteURL[at+1:]
	colon := strings.IndexByte(rest, ':')
	if colon <= 0 {
		return ""
	}
	return rest[:colon]
}

// issueWorkspaceSSHHostsFromManifest 从幂等 manifest 的 remoteUrl 集合提取去重 hostname 列表。
func issueWorkspaceSSHHostsFromManifest(manifest []types.IssueWorkspaceRepoRef) []string {
	seen := make(map[string]struct{}, len(manifest))
	hosts := make([]string, 0, len(manifest))
	for _, ref := range manifest {
		if host := issueWorkspaceSSHHost(ref.RemoteURL); host != "" {
			if _, ok := seen[host]; ok {
				continue
			}
			seen[host] = struct{}{}
			hosts = append(hosts, host)
		}
	}
	return hosts
}

// issueWorkspaceBuildSSHConfig 由全局 config 原文提取与目标 host 集合匹配的 Host 段并序列化，
// matched 表示是否存在至少一个匹配段（false 时调用方降级 SKIPPED）。两处保留/剔除约定：
//   - 文件头裸配置（解析为隐式 Host * 段）与显式 Host * 段匹配一切 host，原样保留——
//     clone 用 ssh -F 指定本文件后不再读全局 config，保留用户全局默认行为（如 AddKeysToAgent）；
//   - Include 指令不展开也不转写——其相对路径在 workspace config 中语义失效（本期范围外）。
func issueWorkspaceBuildSSHConfig(globalConfig []byte, hosts []string) (content string, matched bool, err error) {
	cfg, err := ssh_config.DecodeBytes(globalConfig)
	if err != nil {
		return "", false, err
	}
	var b strings.Builder
	for _, host := range cfg.Hosts {
		if !issueWorkspaceSSHHostMatches(host, hosts) {
			continue
		}
		issueWorkspaceSSHHostDropIncludes(host)
		section := strings.TrimSpace(host.String())
		// 隐式空 Host * 段（Decode 恒生成、无任何 KV）与剔除 Include 后变空的段：无内容贡献，不算匹配。
		if section == "" {
			continue
		}
		matched = true
		b.WriteString(section)
		b.WriteString("\n\n")
	}
	return b.String(), matched, nil
}

// issueWorkspaceSSHHostDropIncludes 就地剔除 Host 段内的 Include 节点（见 build 函数注释）。
func issueWorkspaceSSHHostDropIncludes(h *ssh_config.Host) {
	kept := make([]ssh_config.Node, 0, len(h.Nodes))
	for _, n := range h.Nodes {
		if _, ok := n.(*ssh_config.Include); ok {
			continue
		}
		kept = append(kept, n)
	}
	h.Nodes = kept
}

// issueWorkspaceSSHHostMatches 判断 Host 段的任一 pattern（精确/通配符/negation）匹配任一目标 host。
func issueWorkspaceSSHHostMatches(h *ssh_config.Host, hosts []string) bool {
	// for _, target := range hosts {
	// 	if h.Matches(target) {
	// 		return true
	// 	}
	// }
	// return false

	// 可简写为:
	return slices.ContainsFunc(hosts, h.Matches)
}
