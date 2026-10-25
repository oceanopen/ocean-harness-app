package service

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"ocean-harness/server/internal/acpsession"
	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/global"
)

/*
 * Project 对应 /api/project 命名空间下的业务逻辑。项目 = 独立实体线（项目 → 任务 → 主/子会话），
 * 项目根目录 = 任务运行目录根（{project.dir}/{taskId}/）；仓库多对多关联随 create/update 全量替换
 * （无 diff 全量覆盖）。目录校验复用 validateWorkspaceDir（字段族与工作区目录语义同构）。
 */
type Project struct {
	apis.Service
}

// GetList 返回全部 project（id 倒序），批量装配关联仓库 id。
func (svc Project) GetList(req *types.ProjectGetListRequest) ([]types.ProjectResponseData, error) {
	_ = req // 当前无筛选条件，预留
	q := query.Use(svc.Orm)
	rows, err := q.Project.WithContext(svc.Context).Order(q.Project.ID.Desc()).Find()
	if err != nil {
		return nil, err
	}
	idsByProject, err := projectRepoIDsByProject(svc.Context, q, projectIDs(rows))
	if err != nil {
		return nil, err
	}
	out := make([]types.ProjectResponseData, 0, len(rows))
	for _, row := range rows {
		item := types.ProjectResponseData{}.FromModel(row)
		item.LocalRepositoryIDs = idsByProject[row.ID]
		out = append(out, item)
	}
	return out, nil
}

// GetInfo 按 id 返回单个 project（含关联仓库 id）。
func (svc Project) GetInfo(req *types.ProjectGetInfoRequest) (*types.ProjectResponseData, error) {
	q := query.Use(svc.Orm)
	found, err := q.Project.WithContext(svc.Context).Where(q.Project.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("项目不存在")
		}
		return nil, err
	}
	item := types.ProjectResponseData{}.FromModel(found)
	idsByProject, err := projectRepoIDsByProject(svc.Context, q, []string{found.ID})
	if err != nil {
		return nil, err
	}
	item.LocalRepositoryIDs = idsByProject[found.ID]
	return &item, nil
}

// Create 创建 project（目录校验 + 仓库关联落库同事务）。
func (svc Project) Create(req *types.ProjectCreateRequest) (*types.ProjectResponseData, error) {
	if err := validateWorkspaceDir(req.Dir); err != nil {
		return nil, err
	}
	row := &model.Project{
		ID:             uuid.Must(uuid.NewV7()).String(),
		Name:           req.Name,
		Dir:            strings.TrimSpace(req.Dir),
		Description:    req.Description,
		Emoji:          req.Emoji,
		LaunchSettings: launchSettingsJSON(req.LaunchSettings),
	}
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		if err := query.Use(tx).Project.WithContext(svc.Context).Create(row); err != nil {
			return err
		}
		_, err := replaceProjectRepositories(svc.Context, tx, row.ID, req.LocalRepositoryIDs)
		return err
	})
	if err != nil {
		return nil, err
	}
	item := types.ProjectResponseData{}.FromModel(row)
	item.LocalRepositoryIDs = dedupeRepoIDs(req.LocalRepositoryIDs)
	return &item, nil
}

// Update 更新 project：目录校验后保存 + 仓库关联全量替换（同事务）；目录变更时热更新关联 bot 的连接。
func (svc Project) Update(req *types.ProjectUpdateRequest) (*types.ProjectResponseData, error) {
	if err := validateWorkspaceDir(req.Dir); err != nil {
		return nil, err
	}
	q := query.Use(svc.Orm)
	pq := q.Project.WithContext(svc.Context)
	found, err := pq.Where(q.Project.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("项目不存在")
		}
		return nil, err
	}
	oldDir := found.Dir
	found.Name = req.Name
	found.Dir = strings.TrimSpace(req.Dir)
	found.Description = req.Description
	found.Emoji = req.Emoji
	found.LaunchSettings = launchSettingsJSON(req.LaunchSettings)
	err = svc.Orm.Transaction(func(tx *gorm.DB) error {
		if e := query.Use(tx).Project.WithContext(svc.Context).Save(found); e != nil {
			return e
		}
		_, e := replaceProjectRepositories(svc.Context, tx, found.ID, req.LocalRepositoryIDs)
		return e
	})
	if err != nil {
		return nil, err
	}
	// 仅目录变更才热更新关联 bot（改名/改描述/改启动设置不动连接，避免踢断在途回合）。
	if oldDir != found.Dir {
		reapplyProjectBots(svc.Orm, svc.Logger, req.ID)
	}
	item := types.ProjectResponseData{}.FromModel(found)
	item.LocalRepositoryIDs = dedupeRepoIDs(req.LocalRepositoryIDs)
	return &item, nil
}

/*
 * Delete 物理删除 project（无 DB 外键）。事务内级联：其下任务行 + 任务↔仓库关联 + 任务会话行
 * （claude 本地文件不动）+ 类型 + 项目↔仓库关联；关联 bot 的 project_id/task_id 清列（回到未选态）。
 * 事务后进程内级联：被删任务名下会话逐个 Discard（放事务外，服务未装配时跳过）。
 */
func (svc Project) Delete(req *types.ProjectDeleteRequest) error {
	var botIDs []string
	var discardConvIDs []string
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		if _, e := q.Project.WithContext(svc.Context).Where(q.Project.ID.Eq(req.ID)).First(); e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("项目不存在")
			}
			return e
		}
		tasks, e := q.ProjectTask.WithContext(svc.Context).Where(q.ProjectTask.ProjectID.Eq(req.ID)).Find()
		if e != nil {
			return e
		}
		taskIDs := make([]string, 0, len(tasks))
		for _, t := range tasks {
			taskIDs = append(taskIDs, t.ID)
		}
		if len(taskIDs) > 0 {
			convs, ce := q.Conversation.WithContext(svc.Context).
				Where(q.Conversation.OwnerType.Eq(enums.OWNER_TYPE_TASK), q.Conversation.OwnerID.In(taskIDs...)).Find()
			if ce != nil {
				return ce
			}
			for _, c := range convs {
				discardConvIDs = append(discardConvIDs, c.ID)
			}
			if _, e := q.ProjectTask.WithContext(svc.Context).Where(q.ProjectTask.ID.In(taskIDs...)).Delete(); e != nil {
				return e
			}
			if _, e := q.TaskLocalRepository.WithContext(svc.Context).Where(q.TaskLocalRepository.TaskID.In(taskIDs...)).Delete(); e != nil {
				return e
			}
			if _, e := q.Conversation.WithContext(svc.Context).
				Where(q.Conversation.OwnerType.Eq(enums.OWNER_TYPE_TASK), q.Conversation.OwnerID.In(taskIDs...)).Delete(); e != nil {
				return e
			}
		}
		if _, e := q.ProjectType.WithContext(svc.Context).Where(q.ProjectType.ProjectID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		if _, e := q.ProjectLocalRepository.WithContext(svc.Context).Where(q.ProjectLocalRepository.ProjectID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		bots, be := q.ImBot.WithContext(svc.Context).Where(q.ImBot.ProjectID.Eq(req.ID)).Find()
		if be != nil {
			return be
		}
		for _, b := range bots {
			botIDs = append(botIDs, b.ID)
		}
		if _, e := q.ImBot.WithContext(svc.Context).Where(q.ImBot.ProjectID.Eq(req.ID)).
			UpdateSimple(q.ImBot.ProjectID.Value(""), q.ImBot.TaskID.Value("")); e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		return err
	}
	if global.AcpSessions != nil {
		for _, convID := range discardConvIDs {
			global.AcpSessions.Discard(acpsession.ConversationSessionKey(convID))
		}
	}
	reapplyBotIDs(svc.Orm, svc.Logger, botIDs...)
	return nil
}

// projectIDs 行切片 → id 列表。
func projectIDs(rows []*model.Project) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

// projectRepoIDsByProject 批查项目↔仓库关联并按项目分组（缺失项目键 absent = 无关联）。
func projectRepoIDsByProject(ctx context.Context, q *query.Query, projectIDs []string) (map[string][]int, error) {
	out := make(map[string][]int, len(projectIDs))
	if len(projectIDs) == 0 {
		return out, nil
	}
	links, err := q.ProjectLocalRepository.WithContext(ctx).
		Where(q.ProjectLocalRepository.ProjectID.In(projectIDs...)).Find()
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		out[l.ProjectID] = append(out[l.ProjectID], l.LocalRepositoryID)
	}
	return out, nil
}

/*
 * replaceProjectRepositories 全量替换项目关联仓库：先删该项目全部中间表记录，再插入 repoIDs。
 * 约定"无 diff、全量覆盖"——create/update 随项目信息一起保存关联，前端只传最终列表。orm 传 tx 复用事务。
 * repoIDs 内部去重 + 过滤 <=0（防唯一索引冲突）；空切片=清空全部关联。返回去重后的有效 id 列表。
 */
func replaceProjectRepositories(ctx context.Context, orm *gorm.DB, projectID string, repoIDs []int) ([]int, error) {
	q := query.Use(orm)
	jq := q.ProjectLocalRepository.WithContext(ctx)
	if _, e := jq.Where(q.ProjectLocalRepository.ProjectID.Eq(projectID)).Delete(); e != nil {
		return nil, e
	}
	effective := dedupeRepoIDs(repoIDs)
	if len(effective) == 0 {
		return effective, nil
	}
	links := make([]*model.ProjectLocalRepository, 0, len(effective))
	for _, rid := range effective {
		links = append(links, &model.ProjectLocalRepository{ProjectID: projectID, LocalRepositoryID: rid})
	}
	if e := jq.Create(links...); e != nil {
		return nil, e
	}
	return effective, nil
}

// dedupeRepoIDs 仓库 id 去重 + 过滤 <=0（防唯一索引冲突）。
func dedupeRepoIDs(repoIDs []int) []int {
	seen := make(map[int]struct{}, len(repoIDs))
	effective := make([]int, 0, len(repoIDs))
	for _, rid := range repoIDs {
		if rid <= 0 {
			continue
		}
		if _, ok := seen[rid]; ok {
			continue
		}
		seen[rid] = struct{}{}
		effective = append(effective, rid)
	}
	return effective
}

// reapplyProjectBots 项目目录变更后的 bot 连接热更新：重拉关联该项目的 bot 并 ApplyBot/StopBot。
func reapplyProjectBots(orm *gorm.DB, logger *zap.Logger, projectID string) {
	q := query.Use(orm)
	rows, err := q.ImBot.WithContext(context.Background()).Where(q.ImBot.ProjectID.Eq(projectID)).Find()
	if err != nil {
		return
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	reapplyBotIDs(orm, logger, ids...)
}
