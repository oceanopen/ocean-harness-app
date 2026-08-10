// Package githost 抽象 GitHub / GitLab 等 git 托管平台的 PR 能力（创建/合并）。
// 任务 1.4（D3 PR 平台抽象）：host 检测 + REST API 调用（net/http 标准库，零外部依赖）。
//
// token 由调用方随请求传入（CreatePRRequest/MergePRRequest.Token），本包不持久持有、
// 不入日志/错误（仅在请求 header 使用，docs/worktree_lifecycle.md §4.5）。
//
// 设计：Provider 绑定 baseURL + host + ownerRepo（一次 PR 操作对应一个 repo，构造时解析）。
// DetectProvider 按 remote host 精确匹配（github.com/gitlab.com）；自建/enterprise 由调用方
// 手动 NewGitHubProvider/NewGitLabProvider + 显式 baseURL 构造。
package githost

import "context"

// ProviderKind 标识 git 托管平台类型（仅运行期使用，不入库，故不实现 driver.Valuer）。
// 命名照搬 dal/enums 的 SCREAMING_SNAKE_CASE 约定。
type ProviderKind string

const (
	PROVIDER_KIND_GITHUB ProviderKind = "github"
	PROVIDER_KIND_GITLAB ProviderKind = "gitlab"
)

// Provider 抽象 git 托管平台的 PR 能力，绑定单个 repo（baseURL+host+ownerRepo 在构造时解析）。
// 本期实现 GitHubProvider / GitLabProvider。
type Provider interface {
	// CreatePullRequest 创建 PR；token 随请求传入（不持久持有）。
	CreatePullRequest(ctx context.Context, req CreatePRRequest) (*PR, error)
	// MergePullRequest 合并指定 PR。
	MergePullRequest(ctx context.Context, req MergePRRequest) error

	// Kind 返回平台类型（展示/分支用）。
	Kind() ProviderKind
	// Host 返回 remote host（如 github.com，展示用）。
	Host() string
	// OwnerRepo 返回 owner/repo（GitHub）或 group/sub/repo（GitLab subgroup，展示用）。
	OwnerRepo() string
}

// CreatePRRequest 创建 PR 的入参。owner/repo 由 Provider 构造时绑定，此处只传业务字段 + token。
type CreatePRRequest struct {
	Title string // PR 标题
	Head  string // 源分支（开发分支）
	Base  string // 目标分支（如 main）
	Body  string // PR 描述（可选）
	Token string // 平台 API token（仅在本次请求 header 使用，不入日志）
}

// MergePRRequest 合并 PR 的入参。
type MergePRRequest struct {
	Number int    // PR 编号（GitHub pull number / GitLab merge request iid）
	Token  string // 平台 API token
}

// PR 创建 PR 的返回。
type PR struct {
	Number int    // PR 编号
	URL    string // PR web 地址（GitHub html_url / GitLab web_url）
}
