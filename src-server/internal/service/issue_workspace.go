package service

import (
	"errors"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/src-server/internal/apis"
	"ocean-harness/src-server/internal/dal/model"
	"ocean-harness/src-server/internal/dal/query"
	"ocean-harness/src-server/internal/dal/types"
)

// IssueWorkspace 对应 /api/issueWorkspace 命名空间：issue 运行工作空间初始化编排
// （docs/agent_dev_01_tasks.md T1.1）。
//
// 模型：init 受理 → 同步生成/合并状态文件（.workspace-init-state.json，唯一持久真相，见
// issue_workspace_state.go）→ 立即返回 → 后台 goroutine 串行执行步骤、逐步落盘（见
// issue_workspace_runner.go）→ status 轮询读文件。执行阶段纯状态文件驱动（仓库清单在受理时
// 已从 DB 解析入文件），不碰 DB 与请求 ctx，天然支持崩溃后按文件判定 INTERRUPTED 并重试。
//
// 同域文件分工：本文件仅服务入口（受理/查询/DB 解析）；准入控制见 issue_workspace_registry.go；
// 状态派生/合并/幂等辅助见 issue_workspace_merge.go。
type IssueWorkspace struct {
	apis.Service
}

// Init 受理 issue 工作空间初始化：校验 → 查关联仓库 → 准入控制（同 issue 执行中重复触发返回当前进度）
// → 幂等短路（上次 SUCCESS 且关联未变）→ 增量合并状态文件并同步落盘（RUNNING）→ 后台串行执行。
// baseDir 由前端逐请求传入（appConfig 的 workspace_base_dir，Go 不持久化）。
func (svc IssueWorkspace) Init(req *types.IssueWorkspaceInitRequest) (*types.IssueWorkspaceStatusResponseData, error) {
	if !filepath.IsAbs(req.BaseDir) {
		return nil, errors.New("baseDir 须为绝对路径")
	}
	if !issueWorkspaceValidIssueID(req.IssueID) {
		return nil, errors.New("issueId 非法")
	}
	q := query.Use(svc.Orm)
	if _, err := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ID.Eq(req.IssueID)).First(); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("issue 不存在")
		}
		return nil, err
	}
	repos, err := svc.findIssueRepos(req.IssueID)
	if err != nil {
		return nil, err
	}

	// 准入控制：同 issue 已有活跃写者 → 不重复触发，返回当前进度（前端继续轮询）。
	if !issueWorkspaceAcquire(req.IssueID) {
		return issueWorkspaceDeriveStatusResponse(req.BaseDir, req.IssueID)
	}
	// 此处起持有写者名额：失败路径须显式归还；成功路径移交后台 goroutine（其 defer 归还）。

	old, err := loadIssueWorkspaceState(req.BaseDir, req.IssueID)
	if err != nil {
		// 状态文件损坏（CORRUPTED）：按可重试语义覆盖重建，不阻断初始化。
		svc.Logger.Warn("[issueWorkspace] state file corrupted, rebuilding", zap.String("issueId", req.IssueID), zap.Error(err))
		old = nil
	}

	// 幂等短路：上次已全部成功、无任何占位（SKIPPED）结果且关联仓库/基准分支集合未变 → 不再执行。
	// 含 SKIPPED 的旧 SUCCESS 须放行走 merge 重跑——T1.2/T1.3/T1.4 注册实现后自动接续执行。
	if old != nil && old.Status == types.IW_STATUS_SUCCESS && !issueWorkspaceHasSkipped(old) &&
		issueWorkspaceManifestEqual(old.Manifest, issueWorkspaceRepoRefs(repos)) {
		issueWorkspaceRelease(req.IssueID)
		return &types.IssueWorkspaceStatusResponseData{ServerStatus: types.IW_STATUS_SUCCESS, State: old}, nil
	}

	state := issueWorkspaceMergeState(old, req.BaseDir, req.IssueID, repos)
	state.Status = types.IW_STATUS_RUNNING // 受理即执行中（后台任务即将启动）
	if err := saveIssueWorkspaceState(state); err != nil {
		issueWorkspaceRelease(req.IssueID)
		return nil, err
	}

	// 响应快照必须在启动 goroutine 之前深拷贝：go 语句建立 happens-before，此后 state 归后台
	// 写者独占；若先启动再拷贝，拷贝动作本身就会与 goroutine 的首步写（置 SKIPPED/RUNNING）竞争。
	snapshot := issueWorkspaceCloneState(state)
	// 后台执行：不捕获请求 ctx 与整个 svc（*gin.Context 请求结束即回收）——仅取 Logger
	// （进程级 *zap.Logger 单例，跨请求安全）传入，让隔离成为结构性而非注释性的。
	logger := svc.Logger
	go func() {
		defer issueWorkspaceRelease(req.IssueID)
		issueWorkspaceRunSteps(state, logger)
	}()
	return &types.IssueWorkspaceStatusResponseData{ServerStatus: types.IW_STATUS_RUNNING, State: snapshot}, nil
}

// Status 查询初始化进度（读状态文件派生顶层结论，不查库）。
func (svc IssueWorkspace) Status(req *types.IssueWorkspaceStatusRequest) (*types.IssueWorkspaceStatusResponseData, error) {
	if !filepath.IsAbs(req.BaseDir) {
		return nil, errors.New("baseDir 须为绝对路径")
	}
	if !issueWorkspaceValidIssueID(req.IssueID) {
		return nil, errors.New("issueId 非法")
	}
	return issueWorkspaceDeriveStatusResponse(req.BaseDir, req.IssueID)
}

// issueWorkspaceValidIssueID 校验 issueId 可安全拼入文件路径（正常值为 uuid v7 文本）：
// 拒绝空串、路径分隔符与 Clean 后会变化的值（防 "../x" 路径穿越）。Init/Status 共用
// （Init 另有 DB 存在性校验，Status 不查库故依赖本校验）。
func issueWorkspaceValidIssueID(issueID string) bool {
	return issueID != "" &&
		issueID != "." && issueID != ".." &&
		!strings.ContainsAny(issueID, `/\`) &&
		filepath.Clean(issueID) == issueID
}

// issueWorkspaceRepoInput 是 issue 关联仓库的初始化输入（DB 查询结果，写入状态文件后不再依赖 DB）。
type issueWorkspaceRepoInput struct {
	LocalRepositoryID int
	Name              string
	RemoteURL         string
	BaseBranch        string
}

// findIssueRepos 查 issue 关联的仓库+基准分支（t_issue_local_repositories → t_local_repositories
// 两步批查）。baseBranch 取关联时选定的 repository_branch（freeSolo 可为空，T1.4 clone 时再回退
// 仓库默认分支）；仓库已被删除（级联清理的正常时序差）的关联跳过。
func (svc IssueWorkspace) findIssueRepos(issueID string) ([]issueWorkspaceRepoInput, error) {
	q := query.Use(svc.Orm)
	links, err := q.IssueLocalRepository.WithContext(svc.Context).Where(q.IssueLocalRepository.IssueID.Eq(issueID)).Find()
	if err != nil {
		return nil, err
	}
	out := make([]issueWorkspaceRepoInput, 0, len(links))
	if len(links) == 0 {
		return out, nil
	}
	repoIDs := make([]int, 0, len(links))
	for _, l := range links {
		repoIDs = append(repoIDs, l.LocalRepositoryID)
	}
	repos, err := q.LocalRepository.WithContext(svc.Context).Where(q.LocalRepository.ID.In(repoIDs...)).Find()
	if err != nil {
		return nil, err
	}
	repoMap := make(map[int]*model.LocalRepository, len(repos))
	for _, r := range repos {
		repoMap[r.ID] = r
	}
	for _, l := range links {
		r, ok := repoMap[l.LocalRepositoryID]
		if !ok {
			continue
		}
		out = append(out, issueWorkspaceRepoInput{
			LocalRepositoryID: l.LocalRepositoryID,
			Name:              r.Name,
			RemoteURL:         r.RemoteURL,
			BaseBranch:        l.RepositoryBranch,
		})
	}
	return out, nil
}
