package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/model"
	"we-claude-terminal/go-server/internal/dal/query"
	"we-claude-terminal/go-server/internal/dal/types"
)

// IssueWorkspace 对应 /api/issueWorkspace 命名空间：issue 运行工作空间初始化编排
// （docs/agent_dev_01_tasks.md T1.1）。
//
// 模型：init 受理 → 同步生成/合并状态文件（.workspace-init-state.json，唯一持久真相，见
// issue_workspace_state.go）→ 立即返回 → 后台 goroutine 串行执行步骤、逐步落盘 → status 轮询读文件。
// 执行阶段纯状态文件驱动（仓库清单在受理时已从 DB 解析入文件），不碰 DB 与请求 ctx，
// 天然支持崩溃后按文件判定 INTERRUPTED 并重试。
type IssueWorkspace struct {
	apis.Service
}

// issueWorkspaceRegistry 活跃初始化任务登记（准入控制）：同一 issue 至多一个后台写者。
// 锁只保护本 map（纳秒级临界区），不保护文件——文件写的串行由「唯一写者 goroutine + 步骤 for 循环」
// 构造保证。进程重启后 registry 随进程清空，status 侧以「文件 RUNNING 但无登记」判定 INTERRUPTED。
var issueWorkspaceRegistry = struct {
	sync.Mutex
	tasks map[string]struct{}
}{tasks: map[string]struct{}{}}

// issueWorkspaceStepRunner 单个初始化步骤的执行函数签名（T1.2/T1.3/T1.4 的实现按同签名注册）。
type issueWorkspaceStepRunner func(state *types.IssueWorkspaceState, step *types.IssueWorkspaceStep, logger *zap.Logger) error

// issueWorkspaceStepRunners 步骤实现注册表：key → 执行函数。未注册的步骤置 SKIPPED——
// T1.2（sshConfig）/T1.3（mcpConfig）/T1.4（cloneRepos）落地时各自补注册实现，编排逻辑不再改。
var issueWorkspaceStepRunners = map[string]issueWorkspaceStepRunner{
	types.IW_STEP_KEY_CREATE_DIRS: issueWorkspaceRunCreateDirs,
}

// issueWorkspaceStepTitles 步骤骨架单一来源：顺序即固定执行顺序。
var issueWorkspaceStepTitles = []struct {
	Key   string
	Title string
}{
	{types.IW_STEP_KEY_CREATE_DIRS, "创建目录结构"},
	{types.IW_STEP_KEY_SSH_CONFIG, "生成 SSH Config"},
	{types.IW_STEP_KEY_MCP_CONFIG, "生成 MCP Config"},
	{types.IW_STEP_KEY_CLONE_REPOS, "Clone 仓库与分支"},
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

// issueWorkspaceCloneState 深拷贝状态（Steps/Repos 逐元素复制，切片 nil 性保持）。
// Init 响应返回快照、原 state 交给后台 goroutine 持续 mutate，两者不共享可变内存。
func issueWorkspaceCloneState(src *types.IssueWorkspaceState) *types.IssueWorkspaceState {
	dst := *src
	if src.Steps != nil {
		dst.Steps = make([]*types.IssueWorkspaceStep, len(src.Steps))
		for i, s := range src.Steps {
			step := *s
			if s.Repos != nil {
				step.Repos = make([]*types.IssueWorkspaceRepoState, len(s.Repos))
				for j, r := range s.Repos {
					repo := *r
					step.Repos[j] = &repo
				}
			}
			dst.Steps[i] = &step
		}
	}
	if src.Manifest != nil {
		dst.Manifest = make([]types.IssueWorkspaceRepoRef, len(src.Manifest))
		copy(dst.Manifest, src.Manifest)
	}
	return &dst
}

// issueWorkspaceHasSkipped 判断状态中是否存在 SKIPPED 步骤/仓库（本期占位结果）。
// 幂等短路据此放行「含占位结果的旧 SUCCESS」走 merge 重跑，后续任务注册实现后自动接续执行。
func issueWorkspaceHasSkipped(state *types.IssueWorkspaceState) bool {
	for _, s := range state.Steps {
		if s.Status == types.IW_STATUS_SKIPPED {
			return true
		}
		for _, r := range s.Repos {
			if r.Status == types.IW_STATUS_SKIPPED {
				return true
			}
		}
	}
	return false
}

// issueWorkspaceDeriveStatusResponse 读状态文件并派生顶层 serverStatus：无文件→NOT_INITIALIZED；
// 解析失败→CORRUPTED；文件 RUNNING 但 registry 无活跃任务→INTERRUPTED（上次进程中断，可重试）；
// 否则原样返回文件状态。init 的「重复触发」路径同样复用本派生。
func issueWorkspaceDeriveStatusResponse(baseDir, issueID string) (*types.IssueWorkspaceStatusResponseData, error) {
	state, err := loadIssueWorkspaceState(baseDir, issueID)
	if err != nil {
		return &types.IssueWorkspaceStatusResponseData{ServerStatus: types.IW_STATUS_CORRUPTED}, nil
	}
	if state == nil {
		return &types.IssueWorkspaceStatusResponseData{ServerStatus: types.IW_STATUS_NOT_INITIALIZED}, nil
	}
	if state.Status == types.IW_STATUS_RUNNING && !issueWorkspaceActive(issueID) {
		return &types.IssueWorkspaceStatusResponseData{ServerStatus: types.IW_STATUS_INTERRUPTED, State: state}, nil
	}
	return &types.IssueWorkspaceStatusResponseData{ServerStatus: state.Status, State: state}, nil
}

// issueWorkspaceAcquire 登记活跃任务：登记成功返回 true；已存在（执行中重复触发）返回 false。
func issueWorkspaceAcquire(issueID string) bool {
	issueWorkspaceRegistry.Lock()
	defer issueWorkspaceRegistry.Unlock()
	if _, ok := issueWorkspaceRegistry.tasks[issueID]; ok {
		return false
	}
	issueWorkspaceRegistry.tasks[issueID] = struct{}{}
	return true
}

// issueWorkspaceRelease 归还写者名额（goroutine defer / init 失败路径调用）。
func issueWorkspaceRelease(issueID string) {
	issueWorkspaceRegistry.Lock()
	defer issueWorkspaceRegistry.Unlock()
	delete(issueWorkspaceRegistry.tasks, issueID)
}

// issueWorkspaceActive 查询是否存在活跃任务（INTERRUPTED 判定用）。
func issueWorkspaceActive(issueID string) bool {
	issueWorkspaceRegistry.Lock()
	defer issueWorkspaceRegistry.Unlock()
	_, ok := issueWorkspaceRegistry.tasks[issueID]
	return ok
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

// issueWorkspaceRepoRefs 由输入派生幂等 manifest（对比键 localRepositoryId+baseBranch）。
func issueWorkspaceRepoRefs(repos []issueWorkspaceRepoInput) []types.IssueWorkspaceRepoRef {
	refs := make([]types.IssueWorkspaceRepoRef, 0, len(repos))
	for _, r := range repos {
		refs = append(refs, types.IssueWorkspaceRepoRef{
			LocalRepositoryID: r.LocalRepositoryID,
			RemoteURL:         r.RemoteURL,
			BaseBranch:        r.BaseBranch,
		})
	}
	return refs
}

// issueWorkspaceRepoKey 仓库身份键（幂等对比/增量合并共用的单一来源）：localRepositoryId+baseBranch。
func issueWorkspaceRepoKey(repoID int, baseBranch string) string {
	return fmt.Sprintf("%d|%s", repoID, baseBranch)
}

// issueWorkspaceManifestEqual 对比两个 manifest 是否同集合（键见 issueWorkspaceRepoKey，与顺序无关）。
func issueWorkspaceManifestEqual(a, b []types.IssueWorkspaceRepoRef) bool {
	if len(a) != len(b) {
		return false
	}
	keys := make(map[string]struct{}, len(a))
	for _, r := range a {
		keys[issueWorkspaceRepoKey(r.LocalRepositoryID, r.BaseBranch)] = struct{}{}
	}
	for _, r := range b {
		if _, ok := keys[issueWorkspaceRepoKey(r.LocalRepositoryID, r.BaseBranch)]; !ok {
			return false
		}
	}
	return true
}

// issueWorkspaceMergeState 把 issue 当前关联仓库合并进旧状态，产出本轮待执行状态（PENDING）。
// 规则：
//   - 全局步骤：旧状态中 SUCCESS 的步骤保留成功结论（幂等跳过重跑）；SKIPPED 不保留——占位结果
//     每次重跑时重新推导，T1.2/T1.3/T1.4 实现注册后可自动接续执行；
//   - cloneRepos 仓库级：关联与基准分支未变且旧结果 SUCCESS 的仓库保留（增量跳过），新增/分支变更/
//     旧结果非 SUCCESS 的仓库重置 PENDING，已移除的仓库剔除；全部仓库有既有成功结果且步骤曾成功才保留成功；
//   - manifest 全量重建为当前关联集合；顶层 error 清空（旧失败原因已吸收进重试）。
func issueWorkspaceMergeState(old *types.IssueWorkspaceState, baseDir, issueID string, repos []issueWorkspaceRepoInput) *types.IssueWorkspaceState {
	state := &types.IssueWorkspaceState{
		Version:   issueWorkspaceStateVersion,
		IssueID:   issueID,
		BaseDir:   baseDir,
		Status:    types.IW_STATUS_PENDING,
		Steps:     make([]*types.IssueWorkspaceStep, 0, len(issueWorkspaceStepTitles)),
		Manifest:  issueWorkspaceRepoRefs(repos),
		CreatedAt: time.Now(),
	}
	oldSteps := make(map[string]*types.IssueWorkspaceStep)
	if old != nil {
		state.CreatedAt = old.CreatedAt
		for _, s := range old.Steps {
			oldSteps[s.Key] = s
		}
	}

	for _, def := range issueWorkspaceStepTitles {
		step := &types.IssueWorkspaceStep{Key: def.Key, Title: def.Title, Status: types.IW_STATUS_PENDING}
		prev := oldSteps[def.Key]
		if def.Key == types.IW_STEP_KEY_CLONE_REPOS {
			step.Repos = issueWorkspaceRepoStates(repos, issueID)
			if prev != nil {
				step.Repos = mergeIssueWorkspaceRepoStates(prev.Repos, step.Repos)
			}
			if !issueWorkspaceReposPending(step.Repos) && prev != nil && prev.Status == types.IW_STATUS_SUCCESS {
				step.Status = types.IW_STATUS_SUCCESS
			}
		} else if prev != nil && prev.Status == types.IW_STATUS_SUCCESS {
			step.Status = types.IW_STATUS_SUCCESS
		}
		state.Steps = append(state.Steps, step)
	}
	return state
}

// issueWorkspaceRepoStates 由输入构造全新仓库级子状态（全部 PENDING，targetBranch 统一 agent_{issueId}）。
func issueWorkspaceRepoStates(repos []issueWorkspaceRepoInput, issueID string) []*types.IssueWorkspaceRepoState {
	out := make([]*types.IssueWorkspaceRepoState, len(repos))
	for i, r := range repos {
		out[i] = &types.IssueWorkspaceRepoState{
			LocalRepositoryID: r.LocalRepositoryID,
			Name:              r.Name,
			RemoteURL:         r.RemoteURL,
			BaseBranch:        r.BaseBranch,
			TargetBranch:      "agent_" + issueID,
			Status:            types.IW_STATUS_PENDING,
		}
	}
	return out
}

// mergeIssueWorkspaceRepoStates 把新仓库清单合并进旧结果：键见 issueWorkspaceRepoKey，
// 关联与分支未变且旧结果为 SUCCESS 的仓库保留，其余（新增/分支变更/旧结果非 SUCCESS）重置 PENDING
// 待（重）执行。返回顺序对齐新清单。
func mergeIssueWorkspaceRepoStates(prev, next []*types.IssueWorkspaceRepoState) []*types.IssueWorkspaceRepoState {
	prevKeys := make(map[string]*types.IssueWorkspaceRepoState, len(prev))
	for _, p := range prev {
		prevKeys[issueWorkspaceRepoKey(p.LocalRepositoryID, p.BaseBranch)] = p
	}
	merged := make([]*types.IssueWorkspaceRepoState, len(next))
	for i, n := range next {
		if p, ok := prevKeys[issueWorkspaceRepoKey(n.LocalRepositoryID, n.BaseBranch)]; ok && p.Status == types.IW_STATUS_SUCCESS {
			merged[i] = p
		} else {
			merged[i] = n
		}
	}
	return merged
}

// issueWorkspaceReposPending 判断仓库级子状态中是否存在待执行项。
func issueWorkspaceReposPending(repos []*types.IssueWorkspaceRepoState) bool {
	for _, r := range repos {
		if r.Status == types.IW_STATUS_PENDING {
			return true
		}
	}
	return false
}

// issueWorkspaceRunSteps 串行执行步骤（后台 goroutine 内调用，纯状态文件驱动：不碰 DB 与请求 ctx）。
// 非法状态：跳过（增量合并保留的既有成功结果）；未注册实现的步骤整体置 SKIPPED（含其仓库级子状态）；
// 每次状态变化即时落盘（前端轮询可见）；任一步骤失败置顶层 FAILED 并终止后续步骤。
// 顶层 recover：未恢复的 panic 在任意 goroutine 都会终止整个进程（Recovery 中间件只覆盖 HTTP
// handler 协程），T1.2-T1.4 将注册子进程/文件写等真实 panic 面——runner panic 统一转为 FAILED 落盘。
func issueWorkspaceRunSteps(state *types.IssueWorkspaceState, logger *zap.Logger) {
	defer func() {
		if r := recover(); r != nil {
			// 执行中步骤至多一个（单写者串行）；runner panic 时它停留在 RUNNING，
			// 补记 FAILED 使状态文件自洽（顶层 FAILED + 步骤 FAILED，而非步骤悬挂 RUNNING）。
			for _, step := range state.Steps {
				if step.Status == types.IW_STATUS_RUNNING {
					step.Status = types.IW_STATUS_FAILED
				}
			}
			state.Status = types.IW_STATUS_FAILED
			state.Error = fmt.Sprintf("初始化任务异常退出: %v", r)
			if err := saveIssueWorkspaceState(state); err != nil {
				logger.Error("[issueWorkspace] persist state after panic failed", zap.String("issueId", state.IssueID), zap.Error(err))
			}
			logger.Error("[issueWorkspace] step runner panicked", zap.String("issueId", state.IssueID), zap.Any("panic", r), zap.Stack("stack"))
		}
	}()
	persist := func() {
		if err := saveIssueWorkspaceState(state); err != nil {
			logger.Error("[issueWorkspace] persist state file failed", zap.String("issueId", state.IssueID), zap.Error(err))
		}
	}
	for _, step := range state.Steps {
		if step.Status != types.IW_STATUS_PENDING {
			continue
		}
		runner, ok := issueWorkspaceStepRunners[step.Key]
		if !ok {
			// 本期占位步骤（sshConfig/mcpConfig/cloneRepos）：置 SKIPPED，待 T1.2/T1.3/T1.4 注册实现。
			step.Status = types.IW_STATUS_SKIPPED
			for _, repo := range step.Repos {
				repo.Status = types.IW_STATUS_SKIPPED
				repo.Message = "本期未实现，等待后续任务接入"
			}
			persist()
			continue
		}
		step.Status = types.IW_STATUS_RUNNING
		persist()
		if err := runner(state, step, logger); err != nil {
			step.Status = types.IW_STATUS_FAILED
			state.Status = types.IW_STATUS_FAILED
			state.Error = fmt.Sprintf("步骤「%s」失败: %v", step.Title, err)
			persist()
			return
		}
		step.Status = types.IW_STATUS_SUCCESS
		persist()
	}
	state.Status = types.IW_STATUS_SUCCESS
	state.Error = ""
	persist()
}

// issueWorkspaceRunCreateDirs 创建工作空间目录骨架：{issueId}/.ssh 与 {issueId}/repo。
// issue 根目录由状态文件首次写入时创建（见 saveIssueWorkspaceState 的 MkdirAll）。
func issueWorkspaceRunCreateDirs(state *types.IssueWorkspaceState, _ *types.IssueWorkspaceStep, _ *zap.Logger) error {
	root := filepath.Join(state.BaseDir, state.IssueID)
	for _, sub := range []string{".ssh", "repo"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", sub, err)
		}
	}
	return nil
}
