package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/google/uuid"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/global"
)

/*
 * Workspace 对应 /api/tracker/workspace 命名空间下的业务逻辑。
 * 嵌入 apis.Service 获得由 controller 灌入的 Context/Orm/Logger；方法只收 req、用 svc.Orm、
 * 返回 WorkspaceResponseData（launch_settings JSON 列装配为结构，对齐 im_bot 域响应范式）。
 * 工作空间直接持有任务（issue）与关联仓库：仓库关联随 create/update 全量保存；
 * 被解绑仓库同步硬删其下 issue 的仓库关联（口径与 validateIssueRepoList 一致，防悬挂拒绝保存）。
 */
type Workspace struct {
	apis.Service
}

/*
 * GetList 返回全部 workspace，按 id 倒序（新建在前）。
 * 一次批查仓库关联中间表，装配为 localRepositoryIds 随工作空间一起返回（前端编辑回显/issue 仓库下拉直接用）。
 */
func (svc Workspace) GetList(req *types.WorkspaceGetListRequest) ([]types.WorkspaceResponseData, error) {
	_ = req // 当前无筛选条件，预留
	q := query.Use(svc.Orm)
	rows, err := q.Workspace.WithContext(svc.Context).Order(q.Workspace.ID.Desc()).Find()
	if err != nil {
		return nil, err
	}
	links, err := q.WorkspaceLocalRepository.WithContext(svc.Context).Find()
	if err != nil {
		return nil, err
	}
	idsByWs := make(map[string][]int, len(rows))
	for _, l := range links {
		idsByWs[l.WorkspaceID] = append(idsByWs[l.WorkspaceID], l.LocalRepositoryID)
	}
	out := make([]types.WorkspaceResponseData, 0, len(rows))
	for _, row := range rows {
		item := types.WorkspaceResponseData{}.FromModel(row)
		ids := idsByWs[row.ID]
		if ids == nil {
			ids = []int{}
		}
		item.LocalRepositoryIDs = ids
		out = append(out, item)
	}
	return out, nil
}

// GetInfo 按 id 返回单个 workspace（含关联仓库 ids）。
func (svc Workspace) GetInfo(req *types.WorkspaceGetInfoRequest) (*types.WorkspaceResponseData, error) {
	q := query.Use(svc.Orm)
	ws, err := q.Workspace.WithContext(svc.Context).Where(q.Workspace.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("工作空间不存在")
		}
		return nil, err
	}
	item := types.WorkspaceResponseData{}.FromModel(ws)
	links, err := q.WorkspaceLocalRepository.WithContext(svc.Context).
		Where(q.WorkspaceLocalRepository.WorkspaceID.Eq(req.ID)).Find()
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.LocalRepositoryID)
	}
	item.LocalRepositoryIDs = ids
	return &item, nil
}

/*
 * Create 创建 workspace（目录校验后插入）；同一事务内插入工作空间 + 全量写入关联仓库。
 * 返回响应含本批写入的 localRepositoryIds。
 */
func (svc Workspace) Create(req *types.WorkspaceCreateRequest) (*types.WorkspaceResponseData, error) {
	if err := validateWorkspaceDir(req.Dir); err != nil {
		return nil, err
	}
	var ws *model.Workspace
	var repoIDs []int
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		ws = &model.Workspace{
			ID:             uuid.Must(uuid.NewV7()).String(),
			Name:           req.Name,
			Dir:            strings.TrimSpace(req.Dir),
			Description:    req.Description,
			Emoji:          req.Emoji,
			LaunchSettings: launchSettingsJSON(req.LaunchSettings),
		}
		if err := q.Workspace.WithContext(svc.Context).Create(ws); err != nil {
			return err
		}
		var e error
		repoIDs, e = replaceWorkspaceRepositories(svc.Context, tx, ws.ID, req.LocalRepositoryIDs)
		return e
	})
	if err != nil {
		return nil, err
	}
	item := types.WorkspaceResponseData{}.FromModel(ws)
	item.LocalRepositoryIDs = repoIDs
	return &item, nil
}

/*
 * Update 更新 workspace：目录校验后保存 + 全量覆盖关联仓库（先全量删后全量插，无 diff）。
 * 被解绑仓库同步硬删该工作空间 issue 关联表中的对应记录，否则 issue 编辑保存时回显悬挂关联会被
 * validateIssueRepoList 拒绝（改任何字段都无法保存）。目录变更时热更新关联 bot 的连接。
 */
func (svc Workspace) Update(req *types.WorkspaceUpdateRequest) (*types.WorkspaceResponseData, error) {
	if err := validateWorkspaceDir(req.Dir); err != nil {
		return nil, err
	}
	var ws *model.Workspace
	var repoIDs []int
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		wq := q.Workspace.WithContext(svc.Context)

		found, err := wq.Where(q.Workspace.ID.Eq(req.ID)).First()
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("工作空间不存在")
			}
			return err
		}
		ws = found
		ws.Name = req.Name
		ws.Dir = strings.TrimSpace(req.Dir)
		ws.Description = req.Description
		ws.Emoji = req.Emoji
		ws.LaunchSettings = launchSettingsJSON(req.LaunchSettings)
		if e := wq.Save(ws); e != nil {
			return e
		}
		// 全量覆盖前先记旧关联集合，用于解绑 diff。
		oldLinks, e := q.WorkspaceLocalRepository.WithContext(svc.Context).
			Where(q.WorkspaceLocalRepository.WorkspaceID.Eq(req.ID)).Find()
		if e != nil {
			return e
		}
		oldSet := make(map[int]struct{}, len(oldLinks))
		for _, l := range oldLinks {
			oldSet[l.LocalRepositoryID] = struct{}{}
		}
		repoIDs, e = replaceWorkspaceRepositories(svc.Context, tx, req.ID, req.LocalRepositoryIDs)
		if e != nil {
			return e
		}
		return unbindWorkspaceIssueRepos(svc.Context, tx, req.ID, oldSet, repoIDs)
	})
	if err != nil {
		return nil, err
	}
	// 仅目录变更才热更新关联 bot（改名/改描述/改启动设置不动连接，避免踢断在途回合）。
	if ws.Dir != strings.TrimSpace(req.Dir) {
		reapplyBots(svc.Orm, svc.Logger, req.ID)
	}
	item := types.WorkspaceResponseData{}.FromModel(ws)
	item.LocalRepositoryIDs = repoIDs
	return &item, nil
}

// launchSettingsJSON 启动设置序列化（nil → 空串 = 未配置，消费端回落全局）。
func launchSettingsJSON(ls *types.WorkspaceLaunchSettings) string {
	if ls == nil {
		return ""
	}
	b, _ := json.Marshal(ls)
	return string(b)
}

/*
 * Delete 物理删除 workspace（无 DB 外键），事务内级联清理其下全部数据，避免悬挂：
 * 其下 issue（+ 仓库关联 + IM 会话绑定锚清列）+ 其下 type（t_workspace_types）+ 工作空间↔仓库中间表 + 关联 bot 的 workspace_id 置空（bot 回到未选态）。
 * type 属 workspace 维度，issue 的 type_id 引用随 issue 行删除自然消失，此处删 type 本体即可。
 */
func (svc Workspace) Delete(req *types.WorkspaceDeleteRequest) error {
	var botIDs []string
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		if _, err := q.Workspace.WithContext(svc.Context).Where(q.Workspace.ID.Eq(req.ID)).First(); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("工作空间不存在")
			}
			return err
		}
		if _, err := q.Workspace.WithContext(svc.Context).Where(q.Workspace.ID.Eq(req.ID)).Delete(); err != nil {
			return err
		}
		// 级联删其下 issue：本体 + 仓库关联 + IM 会话绑定锚清列（行保留，headless 续聊锚不动）。
		issues, ie := q.WorkspaceIssue.WithContext(svc.Context).
			Where(q.WorkspaceIssue.WorkspaceID.Eq(req.ID)).Find()
		if ie != nil {
			return ie
		}
		if len(issues) > 0 {
			issueIDs := make([]string, 0, len(issues))
			for _, i := range issues {
				issueIDs = append(issueIDs, i.ID)
			}
			if _, e := q.WorkspaceIssue.WithContext(svc.Context).
				Where(q.WorkspaceIssue.ID.In(issueIDs...)).Delete(); e != nil {
				return e
			}
			if _, e := q.IssueLocalRepository.WithContext(svc.Context).
				Where(q.IssueLocalRepository.IssueID.In(issueIDs...)).Delete(); e != nil {
				return e
			}
			if _, e := q.ImBotConversation.WithContext(svc.Context).Where(
				q.ImBotConversation.BoundIssueID.In(issueIDs...),
			).UpdateSimple(
				q.ImBotConversation.BoundIssueID.Value(""),
				q.ImBotConversation.UpdatedAt.Value(time.Now()),
			); e != nil {
				return e
			}
		}
		// 级联删其下 type 本体。
		if _, e := q.WorkspaceType.WithContext(svc.Context).
			Where(q.WorkspaceType.WorkspaceID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		// 删工作空间↔仓库中间表记录。
		if _, e := q.WorkspaceLocalRepository.WithContext(svc.Context).
			Where(q.WorkspaceLocalRepository.WorkspaceID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		// 关联 bot 置空（回到未选态），事务后热更新其连接。
		bots, be := q.ImBot.WithContext(svc.Context).Where(q.ImBot.WorkspaceID.Eq(req.ID)).Find()
		if be != nil {
			return be
		}
		for _, b := range bots {
			botIDs = append(botIDs, b.ID)
		}
		if _, e := q.ImBot.WithContext(svc.Context).Where(q.ImBot.WorkspaceID.Eq(req.ID)).
			UpdateSimple(q.ImBot.WorkspaceID.Value("")); e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		return err
	}
	reapplyBotIDs(svc.Orm, svc.Logger, botIDs...)
	return nil
}

/*
 * replaceWorkspaceRepositories 全量替换工作空间的关联仓库：先删该工作空间全部中间表记录，再插入 repoIDs。
 * 约定"无 diff、全量覆盖"——create/update 随工作空间信息一起保存关联，前端只传最终列表。orm 传 tx 复用事务。
 * repoIDs 内部去重 + 过滤 <=0（防唯一索引冲突）；空切片=清空全部关联。返回去重后的有效 id 列表。
 */
func replaceWorkspaceRepositories(ctx context.Context, orm *gorm.DB, workspaceID string, repoIDs []int) ([]int, error) {
	q := query.Use(orm)
	jq := q.WorkspaceLocalRepository.WithContext(ctx)
	// 1) 先全量删除该工作空间现有关联。
	if _, e := jq.Where(q.WorkspaceLocalRepository.WorkspaceID.Eq(workspaceID)).Delete(); e != nil {
		return nil, e
	}
	// 2) 去重后全量插入。
	seen := make(map[int]struct{}, len(repoIDs))
	effective := make([]int, 0, len(repoIDs))
	links := make([]*model.WorkspaceLocalRepository, 0, len(repoIDs))
	for _, rid := range repoIDs {
		if rid <= 0 {
			continue
		}
		if _, ok := seen[rid]; ok {
			continue
		}
		seen[rid] = struct{}{}
		effective = append(effective, rid)
		links = append(links, &model.WorkspaceLocalRepository{WorkspaceID: workspaceID, LocalRepositoryID: rid})
	}
	if len(links) == 0 {
		return effective, nil
	}
	if e := jq.Create(links...); e != nil {
		return nil, e
	}
	return effective, nil
}

/*
 * unbindWorkspaceIssueRepos 解绑级联：被解绑仓库（旧有且新无）从该工作空间 issue 的关联表
 * t_issue_local_repositories 中硬删，否则 issue 编辑保存时回显悬挂关联会被 validateIssueRepoList
 * 拒绝（改任何字段都无法保存）。orm 传 tx 复用事务。
 */
func unbindWorkspaceIssueRepos(ctx context.Context, orm *gorm.DB, workspaceID string, oldSet map[int]struct{}, newIDs []int) error {
	newSet := make(map[int]struct{}, len(newIDs))
	for _, rid := range newIDs {
		if rid > 0 {
			newSet[rid] = struct{}{}
		}
	}
	unbound := make([]int, 0, len(oldSet))
	for id := range oldSet {
		if _, keep := newSet[id]; !keep {
			unbound = append(unbound, id)
		}
	}
	if len(unbound) == 0 {
		return nil
	}
	q := query.Use(orm)
	wsIssues, e := q.WorkspaceIssue.WithContext(ctx).
		Where(q.WorkspaceIssue.WorkspaceID.Eq(workspaceID)).Find()
	if e != nil {
		return e
	}
	if len(wsIssues) == 0 {
		return nil
	}
	issueIDs := make([]string, 0, len(wsIssues))
	for _, i := range wsIssues {
		issueIDs = append(issueIDs, i.ID)
	}
	_, e = q.IssueLocalRepository.WithContext(ctx).
		Where(q.IssueLocalRepository.IssueID.In(issueIDs...)).
		Where(q.IssueLocalRepository.LocalRepositoryID.In(unbound...)).Delete()
	return e
}

// validateWorkspaceDir 工作区目录校验：绝对路径 + 可创建 + 可写（探针文件写入即删）。
func validateWorkspaceDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if !filepath.IsAbs(dir) {
		return errors.New("工作区目录必须为绝对路径")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("工作区目录无法创建: %w", err)
	}
	probe := filepath.Join(dir, ".workspace-write-probe")
	if err := os.WriteFile(probe, []byte("probe"), 0o644); err != nil {
		return fmt.Errorf("工作区目录不可写: %w", err)
	}
	_ = os.Remove(probe)
	return nil
}

// reapplyBots 目录变更后的 bot 连接热更新：重拉关联该工作空间的 bot 并 ApplyBot/StopBot。
func reapplyBots(orm *gorm.DB, logger *zap.Logger, workspaceID string) {
	q := query.Use(orm)
	rows, err := q.ImBot.WithContext(context.Background()).Where(q.ImBot.WorkspaceID.Eq(workspaceID)).Find()
	if err != nil {
		return
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	reapplyBotIDs(orm, logger, ids...)
}

/*
 * reapplyBotIDs 按 id 重拉 bot 行并热更新连接（启用 → ApplyBot，停用 → StopBot）。
 * 失败仅日志（DB 已是 SSOT，运行时漂移可经重启连接/重启应用收敛）。
 */
func reapplyBotIDs(orm *gorm.DB, logger *zap.Logger, ids ...string) {
	if global.BotSupervisor == nil || len(ids) == 0 {
		return
	}
	q := query.Use(orm)
	rows, err := q.ImBot.WithContext(context.Background()).Where(q.ImBot.ID.In(ids...)).Find()
	if err != nil {
		return
	}
	for _, row := range rows {
		if row.Enabled.IsYes() {
			if err := global.BotSupervisor.ApplyBot(row); err != nil && logger != nil {
				logger.Warn("bot 连接热更新失败（DB 已保存，可重启连接恢复）", zap.String("botID", row.ID), zap.Error(err))
			}
		} else {
			global.BotSupervisor.StopBot(row.ID)
		}
	}
}
