package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/bot"
	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/global"
)

// ImBot 对应 /api/imBot 命名空间下的业务逻辑：bot CRUD + supervisor 连接热更新联动。
// DB 是配置 SSOT；运行时是投影——写库成功后才调 supervisor（ApplyBot/StopBot），失败仅日志。
type ImBot struct {
	apis.Service
}

// GetList 返回全部 bot（id 倒序）+ 运行态合并。
func (svc ImBot) GetList() ([]types.ImBotResponseData, error) {
	q := query.Use(svc.Orm)
	rows, err := q.ImBot.WithContext(svc.Context).Order(q.ImBot.ID.Desc()).Find()
	if err != nil {
		return nil, err
	}
	statuses := map[int]bot.BotStatusView{}
	if global.BotSupervisor != nil {
		for _, st := range global.BotSupervisor.Statuses() {
			statuses[st.BotID] = st
		}
	}
	out := make([]types.ImBotResponseData, 0, len(rows))
	for _, row := range rows {
		item := types.ImBotResponseData{}.FromModel(row)
		// statuses 缺行（未运行）由 mergeRuntimeStatus 回落 stopped/row.LastError。
		mergeRuntimeStatus(&item, row, statuses[row.ID])
		out = append(out, item)
	}
	return out, nil
}

// GetInfo 返回单个 bot（含运行态合并）。
func (svc ImBot) GetInfo(req *types.ImBotGetInfoRequest) (*types.ImBotResponseData, error) {
	row, err := svc.mustGet(req.ID)
	if err != nil {
		return nil, err
	}
	item := types.ImBotResponseData{}.FromModel(row)
	svc.mergeCurrentStatus(&item, row)
	return &item, nil
}

// Create 新增 bot：校验（工作目录可写 / botId 查重）→ 落库 → 启用则拉起连接。
func (svc ImBot) Create(req *types.ImBotCreateRequest) (*types.ImBotResponseData, error) {
	if err := svc.validateWorkspaceDir(req.WorkspaceDir); err != nil {
		return nil, err
	}
	if err := svc.ensureBotIdAvailable(req.BotId, 0); err != nil {
		return nil, err
	}
	row := &model.ImBot{
		Name:         req.Name,
		Channel:      enums.CHANNEL_WECOM,
		Credential:   credentialJSON(req.BotId, req.Secret),
		WorkspaceDir: req.WorkspaceDir,
		Model:        req.Model,
		SystemPrompt: req.SystemPrompt,
		AllowedTools: allowedToolsJSON(req.AllowedTools),
		AccessPolicy: accessPolicyJSON(req.AccessPolicy),
		Enabled:      enabledYesNo(req.Enabled),
	}
	q := query.Use(svc.Orm)
	if err := q.ImBot.WithContext(svc.Context).Create(row); err != nil {
		return nil, err
	}
	svc.applyRuntime(row)
	item := types.ImBotResponseData{}.FromModel(row)
	svc.mergeCurrentStatus(&item, row)
	return &item, nil
}

// Update 更新 bot：secret 留空 = 沿用原值；启用 → ApplyBot（Stop→Start 重启生效），停用 → StopBot。
func (svc ImBot) Update(req *types.ImBotUpdateRequest) (*types.ImBotResponseData, error) {
	row, err := svc.mustGet(req.ID)
	if err != nil {
		return nil, err
	}
	if err := svc.validateWorkspaceDir(req.WorkspaceDir); err != nil {
		return nil, err
	}
	if err := svc.ensureBotIdAvailable(req.BotId, req.ID); err != nil {
		return nil, err
	}
	secret := req.Secret
	if secret == "" {
		var cred types.ImBotCredential
		_ = json.Unmarshal([]byte(row.Credential), &cred) // 旧凭据损坏则按空处理
		secret = cred.Secret
	}

	row.Name = req.Name
	row.Credential = credentialJSON(req.BotId, secret)
	row.WorkspaceDir = req.WorkspaceDir
	row.Model = req.Model
	row.SystemPrompt = req.SystemPrompt
	row.AllowedTools = allowedToolsJSON(req.AllowedTools)
	row.AccessPolicy = accessPolicyJSON(req.AccessPolicy)
	row.Enabled = enabledYesNo(req.Enabled)

	q := query.Use(svc.Orm)
	// 显式列 Updates 而非基线 Save：row 是 mustGet 的陈旧快照，Save 整行回写会把 supervisor
	// 并发写入的 last_error 覆盖回旧值；map 更新恰好绕开该列的竞态（有意偏离基线，勿"纠正"）。
	if _, err := q.ImBot.WithContext(svc.Context).Where(q.ImBot.ID.Eq(row.ID)).
		Updates(map[string]any{
			"name": row.Name, "credential": row.Credential, "workspace_dir": row.WorkspaceDir,
			"model": row.Model, "system_prompt": row.SystemPrompt, "allowed_tools": row.AllowedTools,
			"access_policy": row.AccessPolicy, "enabled": row.Enabled,
		}); err != nil {
		return nil, err
	}
	svc.applyRuntime(row)
	item := types.ImBotResponseData{}.FromModel(row)
	svc.mergeCurrentStatus(&item, row)
	return &item, nil
}

// Delete 物理删除 bot + 事务级联删会话映射（无 DB 外键，service 层手动级联——tracker 基线约定），
// 并停止其连接。
func (svc ImBot) Delete(req *types.ImBotDeleteRequest) error {
	if _, err := svc.mustGet(req.ID); err != nil {
		return err
	}
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		qtx := query.Use(tx)
		if _, err := qtx.ImBotConversation.WithContext(svc.Context).
			Where(qtx.ImBotConversation.BotID.Eq(req.ID)).Delete(); err != nil {
			return err
		}
		if _, err := qtx.ImBot.WithContext(svc.Context).Where(qtx.ImBot.ID.Eq(req.ID)).Delete(); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if global.BotSupervisor != nil {
		global.BotSupervisor.StopBot(req.ID)
	}
	return nil
}

// Restart 以当前落库配置重连（被踢/长时间挂死后的人工恢复手段）。
func (svc ImBot) Restart(req *types.ImBotRestartRequest) error {
	row, err := svc.mustGet(req.ID)
	if err != nil {
		return err
	}
	if !row.Enabled.IsYes() {
		return errors.New("bot 已停用，请先启用再重启连接")
	}
	if global.BotSupervisor == nil {
		return errors.New("bot 运行时未就绪")
	}
	return global.BotSupervisor.ApplyBot(row)
}

// ---------- 内部工具 ----------

// mustGet 按 id 取 bot 行（不存在给中文错误）。
func (svc ImBot) mustGet(id int) (*model.ImBot, error) {
	q := query.Use(svc.Orm)
	row, err := q.ImBot.WithContext(svc.Context).Where(q.ImBot.ID.Eq(id)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("bot 不存在")
		}
		return nil, err
	}
	return row, nil
}

// applyRuntime 落库成功后的连接热更新：启用 → ApplyBot（Stop→Start），停用 → StopBot。
// 失败仅日志（DB 已是 SSOT，运行时漂移可经重启连接/重启应用收敛）。
func (svc ImBot) applyRuntime(row *model.ImBot) {
	if global.BotSupervisor == nil {
		return
	}
	var err error
	if row.Enabled.IsYes() {
		err = global.BotSupervisor.ApplyBot(row)
	} else {
		global.BotSupervisor.StopBot(row.ID)
	}
	if err != nil {
		svc.Logger.Warn("bot 连接热更新失败（DB 已保存，可重启连接恢复）",
			zap.Int("botID", row.ID), zap.Error(err))
	}
}

// mergeCurrentStatus 取该 bot 当前运行态合并进响应（Create/Update 后响应即真实状态，
// 前端 setQueryData 一次往返不出现「已启用+未运行」的矛盾投影）。
func (svc ImBot) mergeCurrentStatus(item *types.ImBotResponseData, row *model.ImBot) {
	var st bot.BotStatusView
	if global.BotSupervisor != nil {
		st, _ = global.BotSupervisor.StatusOf(row.ID)
	}
	mergeRuntimeStatus(item, row, st)
}

// mergeRuntimeStatus 运行态合并进响应：running=false（未运行/未注册适配器）时
// ConnState=stopped，LastError 回落 DB 行 last_error（跨重启可见）。
func mergeRuntimeStatus(item *types.ImBotResponseData, row *model.ImBot, st bot.BotStatusView) {
	item.LastError = row.LastError
	if st.State != "" {
		item.ConnState = st.State
		if st.LastError != "" {
			item.LastError = st.LastError
		}
		return
	}
	item.ConnState = "stopped"
}

// validateWorkspaceDir 工作目录校验：绝对路径 + 可创建 + 可写（探针文件写入即删）。
func (svc ImBot) validateWorkspaceDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return errors.New("工作目录必须为绝对路径")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("工作目录无法创建: %w", err)
	}
	probe := filepath.Join(dir, ".imbot-write-probe")
	if err := os.WriteFile(probe, []byte("probe"), 0o644); err != nil {
		return fmt.Errorf("工作目录不可写: %w", err)
	}
	_ = os.Remove(probe)
	return nil
}

// ensureBotIdAvailable credential.botId 查重（JSON 列无法建唯一索引，service 层扫描校验；
// excludeID 供 update 排除自身）。
func (svc ImBot) ensureBotIdAvailable(botId string, excludeID int) error {
	q := query.Use(svc.Orm)
	rows, err := q.ImBot.WithContext(svc.Context).Find()
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.ID == excludeID {
			continue
		}
		var cred types.ImBotCredential
		if json.Unmarshal([]byte(row.Credential), &cred) == nil && cred.BotId == botId {
			return fmt.Errorf("botId 已被其他机器人使用（%s）", row.Name)
		}
	}
	return nil
}

// credentialJSON 组装 credential JSON（shape SSOT：types.ImBotCredential）。
func credentialJSON(botId, secret string) string {
	b, _ := json.Marshal(types.ImBotCredential{BotId: botId, Secret: secret})
	return string(b)
}

// allowedToolsJSON 工具白名单序列化（nil → "[]" = 采用代码默认白名单）。
func allowedToolsJSON(tools []string) string {
	if tools == nil {
		tools = []string{}
	}
	b, _ := json.Marshal(tools)
	return string(b)
}

// accessPolicyJSON 访问白名单序列化（allowUsers nil → []，保证 JSON 形态完整）。
func accessPolicyJSON(p types.ImBotAccessPolicy) string {
	if p.AllowUsers == nil {
		p.AllowUsers = []string{}
	}
	b, _ := json.Marshal(p)
	return string(b)
}

// enabledYesNo bool → 公共是否型映射。
func enabledYesNo(b bool) enums.YesNo {
	if b {
		return enums.YES_NO_YES
	}
	return enums.YES_NO_NO
}
