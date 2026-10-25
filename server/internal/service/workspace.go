package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
 * Workspace 对应 /api/workspace 命名空间下的业务逻辑。
 * 嵌入 apis.Service 获得由 controller 灌入的 Context/Orm/Logger；方法只收 req、用 svc.Orm、
 * 返回 WorkspaceResponseData（launch_settings JSON 列装配为结构，对齐 im_bot 域响应范式）。
 * 工作空间仅承载工作目录 + 默认会话执行模式 + 基础信息（去任务归属与仓库关联，任务/仓库归项目域）。
 */
type Workspace struct {
	apis.Service
}

// GetList 返回全部 workspace，按 id 倒序（新建在前）。
func (svc Workspace) GetList(req *types.WorkspaceGetListRequest) ([]types.WorkspaceResponseData, error) {
	_ = req // 当前无筛选条件，预留
	q := query.Use(svc.Orm)
	rows, err := q.Workspace.WithContext(svc.Context).Order(q.Workspace.ID.Desc()).Find()
	if err != nil {
		return nil, err
	}
	out := make([]types.WorkspaceResponseData, 0, len(rows))
	for _, row := range rows {
		out = append(out, types.WorkspaceResponseData{}.FromModel(row))
	}
	return out, nil
}

// GetInfo 按 id 返回单个 workspace。
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
	return &item, nil
}

// Create 创建 workspace（目录校验后插入）。
func (svc Workspace) Create(req *types.WorkspaceCreateRequest) (*types.WorkspaceResponseData, error) {
	if err := validateWorkspaceDir(req.Dir); err != nil {
		return nil, err
	}
	ws := &model.Workspace{
		ID:             uuid.Must(uuid.NewV7()).String(),
		Name:           req.Name,
		Dir:            strings.TrimSpace(req.Dir),
		Description:    req.Description,
		Emoji:          req.Emoji,
		LaunchSettings: launchSettingsJSON(req.LaunchSettings),
	}
	q := query.Use(svc.Orm)
	if err := q.Workspace.WithContext(svc.Context).Create(ws); err != nil {
		return nil, err
	}
	item := types.WorkspaceResponseData{}.FromModel(ws)
	return &item, nil
}

// Update 更新 workspace：目录校验后保存；目录变更时热更新关联 bot 的连接。
func (svc Workspace) Update(req *types.WorkspaceUpdateRequest) (*types.WorkspaceResponseData, error) {
	if err := validateWorkspaceDir(req.Dir); err != nil {
		return nil, err
	}
	q := query.Use(svc.Orm)
	wq := q.Workspace.WithContext(svc.Context)

	found, err := wq.Where(q.Workspace.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("工作空间不存在")
		}
		return nil, err
	}
	oldDir := found.Dir
	found.Name = req.Name
	found.Dir = strings.TrimSpace(req.Dir)
	found.Description = req.Description
	found.Emoji = req.Emoji
	found.LaunchSettings = launchSettingsJSON(req.LaunchSettings)
	if e := wq.Save(found); e != nil {
		return nil, e
	}
	// 仅目录变更才热更新关联 bot（改名/改描述/改启动设置不动连接，避免踢断在途回合）。
	if oldDir != found.Dir {
		reapplyBots(svc.Orm, svc.Logger, req.ID)
	}
	item := types.WorkspaceResponseData{}.FromModel(found)
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
 * Delete 物理删除 workspace（无 DB 外键）。任务/类型/仓库关联归项目域，不在级联面内；
 * 关联 bot 的 workspace_id 置空（bot 回到未选态），事务后热更新其连接。
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
