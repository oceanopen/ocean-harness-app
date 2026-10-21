package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/google/uuid"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/bot"
	"ocean-harness/server/internal/bot/wecom"
	"ocean-harness/server/internal/bot/weixin"
	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/global"
)

/*
 * ImBot 对应 /api/imBot 命名空间下的业务逻辑：bot CRUD + supervisor 连接热更新联动。
 * DB 是配置 SSOT；运行时是投影——写库成功后才调 supervisor（ApplyBot/StopBot），失败仅日志。
 */
type ImBot struct {
	apis.Service
}

// GetList 返回全部 bot（id 倒序）+ 运行态合并 + 工作空间名。
func (svc ImBot) GetList() ([]types.ImBotResponseData, error) {
	q := query.Use(svc.Orm)
	rows, err := q.ImBot.WithContext(svc.Context).Order(q.ImBot.ID.Desc()).Find()
	if err != nil {
		return nil, err
	}
	names := svc.workspaceNames()
	statuses := map[string]bot.BotStatusView{}
	if global.BotSupervisor != nil {
		for _, st := range global.BotSupervisor.Statuses() {
			statuses[st.BotID] = st
		}
	}
	out := make([]types.ImBotResponseData, 0, len(rows))
	for _, row := range rows {
		item := types.ImBotResponseData{}.FromModel(row)
		item.WorkspaceName = names[row.WorkspaceID]
		// statuses 缺行（未运行）由 mergeRuntimeStatus 回落 stopped/row.LastError。
		mergeRuntimeStatus(&item, row, statuses[row.ID])
		out = append(out, item)
	}
	return out, nil
}

// GetInfo 返回单个 bot（含运行态合并 + 工作空间名）。
func (svc ImBot) GetInfo(req *types.ImBotGetInfoRequest) (*types.ImBotResponseData, error) {
	row, err := svc.mustGet(req.ID)
	if err != nil {
		return nil, err
	}
	item := types.ImBotResponseData{}.FromModel(row)
	item.WorkspaceName = svc.workspaceNames()[row.WorkspaceID]
	svc.mergeCurrentStatus(&item, row)
	return &item, nil
}

// Create 新增 bot：校验（工作空间存在 / botId 查重）→ 落库 → 启用则拉起连接。
// 渠道边界：仅 wecom 可手工创建（凭据来自企微后台）；weixin 凭据唯一来源是扫码授权。
func (svc ImBot) Create(req *types.ImBotCreateRequest) (*types.ImBotResponseData, error) {
	if req.Channel != enums.CHANNEL_WECOM {
		return nil, errors.New("微信机器人请通过「扫码授权」接入，不支持手工创建")
	}
	if err := svc.ensureWorkspaceExists(req.WorkspaceId); err != nil {
		return nil, err
	}
	if err := svc.ensureBotIdAvailable(req.BotId, ""); err != nil {
		return nil, err
	}
	row := &model.ImBot{
		ID:           uuid.Must(uuid.NewV7()).String(),
		Name:         req.Name,
		Channel:      req.Channel,
		Credential:   credentialJSON(req.BotId, req.Secret),
		WorkspaceID:  req.WorkspaceId,
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

/*
 * Update 更新 bot：secret 留空 = 沿用原值；启用 → ApplyBot（Stop→Start 重启生效），停用 → StopBot。
 * 渠道分支按行 channel（SSOT）：weixin 行仅更新非凭据字段（凭据唯一来源是扫码换绑，botId/secret
 * 不参与校验与回写）。
 */
func (svc ImBot) Update(req *types.ImBotUpdateRequest) (*types.ImBotResponseData, error) {
	row, err := svc.mustGet(req.ID)
	if err != nil {
		return nil, err
	}
	if err := svc.ensureWorkspaceExists(req.WorkspaceId); err != nil {
		return nil, err
	}
	credential := row.Credential
	if row.Channel == enums.CHANNEL_WECOM {
		if err := svc.ensureBotIdAvailable(req.BotId, req.ID); err != nil {
			return nil, err
		}
		secret := req.Secret
		if secret == "" {
			var cred types.ImBotCredential
			_ = json.Unmarshal([]byte(row.Credential), &cred) // 旧凭据损坏则按空处理
			secret = cred.Secret
		}
		credential = credentialJSON(req.BotId, secret)
	}

	row.Name = req.Name
	row.Credential = credential
	row.WorkspaceID = req.WorkspaceId
	row.AccessPolicy = accessPolicyJSON(req.AccessPolicy)
	row.Enabled = enabledYesNo(req.Enabled)

	q := query.Use(svc.Orm)
	/*
	 * 显式列 Updates 而非基线 Save：row 是 mustGet 的陈旧快照，Save 整行回写会把 supervisor
	 * 并发写入的 last_error/channel_state 覆盖回旧值；map 更新恰好绕开这两列的竞态（有意偏离
	 * 基线，勿"纠正"）。
	 */
	if _, err := q.ImBot.WithContext(svc.Context).Where(q.ImBot.ID.Eq(row.ID)).
		Updates(map[string]any{
			"name": row.Name, "credential": row.Credential, "workspace_id": row.WorkspaceID,
			"access_policy": row.AccessPolicy, "enabled": row.Enabled,
		}); err != nil {
		return nil, err
	}
	// channel_state 列不回写（weixin 运行态游标，supervisor 侧自管）；重读行取最新投影。
	fresh, err := svc.mustGet(req.ID)
	if err != nil {
		return nil, err
	}
	row = fresh
	svc.applyRuntime(row)
	item := types.ImBotResponseData{}.FromModel(row)
	svc.mergeCurrentStatus(&item, row)
	return &item, nil
}

/*
 * Delete 物理删除 bot + 事务级联删会话映射（无 DB 外键，service 层手动级联——tracker 基线约定），
 * 并停止其连接。
 */
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

// ---------- 会话级绑定（工具栏数据链，D1/D7） ----------

// GetConversations 返回 bot 的会话列表（最近活跃在前，从未活跃殿后）+ 绑定任务名批量装配。
func (svc ImBot) GetConversations(req *types.ImBotGetConversationsRequest) ([]types.ImBotConversationData, error) {
	if _, err := svc.mustGet(req.BotId); err != nil {
		return nil, err
	}
	q := query.Use(svc.Orm)
	rows, err := q.ImBotConversation.WithContext(svc.Context).
		Where(q.ImBotConversation.BotID.Eq(req.BotId)).
		Order(q.ImBotConversation.LastMessageAt.Desc()).
		Order(q.ImBotConversation.ID.Desc()).
		Find()
	if err != nil {
		return nil, err
	}
	names := svc.boundIssueNames(rows)
	out := make([]types.ImBotConversationData, 0, len(rows))
	for _, row := range rows {
		out = append(out, types.ImBotConversationData{
			ConversationKey: row.ConversationKey,
			ChatType:        chatTypeOfKey(row.ConversationKey),
			BoundIssueId:    row.BoundIssueID,
			BoundIssueName:  names[row.BoundIssueID],
			LastMessageAt:   row.LastMessageAt,
		})
	}
	return out, nil
}

/*
 * BindConversationIssue 会话级绑定/解绑（issueId 空 = 解绑）。绑定校验链：bot 已选工作空间 →
 * issue 存在且归属 bot 工作空间 → 会话行在场（绑定以会话已开启为前提，行不存在即拒）。
 * 只写绑定列不动 claude_session_id；不触发 applyRuntime——绑定每回合现读，无需热重载。
 */
func (svc ImBot) BindConversationIssue(req *types.ImBotBindIssueRequest) error {
	botRow, err := svc.mustGet(req.BotId)
	if err != nil {
		return err
	}
	q := query.Use(svc.Orm)
	if req.IssueId != "" {
		if botRow.WorkspaceID == "" {
			return errors.New("请先为机器人选择工作空间")
		}
		issue, err := q.WorkspaceIssue.WithContext(svc.Context).Where(q.WorkspaceIssue.ID.Eq(req.IssueId)).First()
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("任务不存在")
			}
			return err
		}
		if issue.WorkspaceID != botRow.WorkspaceID {
			return errors.New("任务不属于机器人当前工作空间")
		}
	}
	res, err := q.ImBotConversation.WithContext(svc.Context).
		Where(q.ImBotConversation.BotID.Eq(req.BotId),
			q.ImBotConversation.ConversationKey.Eq(req.ConversationKey)).
		UpdateSimple(
			q.ImBotConversation.BoundIssueID.Value(req.IssueId),
			q.ImBotConversation.UpdatedAt.Value(time.Now()),
		)
	if err != nil {
		return err
	}
	if res.RowsAffected == 0 {
		return errors.New("会话不存在（需先在 IM 中向机器人发消息开启会话）")
	}
	return nil
}

/*
 * boundIssueNames 会话行绑定 issue id → name 一次性映射（对齐 workspaceNames 惯例；
 * 悬空锚/查失败按空名兜底，不阻塞列表加载）。
 */
func (svc ImBot) boundIssueNames(rows []*model.ImBotConversation) map[string]string {
	ids := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if row.BoundIssueID != "" && !seen[row.BoundIssueID] {
			seen[row.BoundIssueID] = true
			ids = append(ids, row.BoundIssueID)
		}
	}
	if len(ids) == 0 {
		return map[string]string{}
	}
	q := query.Use(svc.Orm)
	issues, err := q.WorkspaceIssue.WithContext(svc.Context).Where(q.WorkspaceIssue.ID.In(ids...)).Find()
	if err != nil {
		return map[string]string{}
	}
	names := make(map[string]string, len(issues))
	for _, is := range issues {
		names[is.ID] = is.Name
	}
	return names
}

/*
 * chatTypeOfKey 会话键前缀派生聊天类型（group: → group，single: 及其余 → single）。键格式
 * SSOT 是 bot.FormatConversationKey（前缀自描述），此处按前缀就地解析、不调用 bot 包函数。
 */
func chatTypeOfKey(key string) string {
	if strings.HasPrefix(key, "group:") {
		return "group"
	}
	return "single"
}

// ---------- 扫码授权接入（provision，渠道分发） ----------

// ProvisionBegin 开新扫码会话（返回二维码内容 + 轮询节奏；同渠道已有会话自动取消）。
func (svc ImBot) ProvisionBegin(req *types.ImBotProvisionBeginRequest) (*types.ImBotProvisionView, error) {
	switch req.Channel {
	case enums.CHANNEL_WEIXIN:
		view, err := weixin.StartProvisioning(req.BotID, svc.weixinLocalTokens())
		if err != nil {
			return nil, err
		}
		return weixinProvisionView(view), nil
	default:
		view, err := wecom.StartProvisioning()
		if err != nil {
			return nil, err
		}
		return provisionView(view), nil
	}
}

// ProvisionPoll 轮询一次扫码结果；connected 时激活已完成（bot 已建/换绑并连接）。
func (svc ImBot) ProvisionPoll(req *types.ImBotProvisionPollRequest) (*types.ImBotProvisionView, error) {
	switch req.Channel {
	case enums.CHANNEL_WEIXIN:
		view, err := weixin.RegistrationStatus(req.AttemptID)
		if err != nil {
			return nil, err
		}
		return weixinProvisionView(view), nil
	default:
		view, err := wecom.RegistrationStatus(req.AttemptID)
		if err != nil {
			return nil, err
		}
		return provisionView(view), nil
	}
}

// ProvisionCancel 取消扫码会话。
func (svc ImBot) ProvisionCancel(req *types.ImBotProvisionCancelRequest) error {
	switch req.Channel {
	case enums.CHANNEL_WEIXIN:
		return weixin.CancelProvisioning(req.AttemptID)
	default:
		return wecom.CancelProvisioning(req.AttemptID)
	}
}

// ProvisionVerifyCode 提交微信扫码配对码（仅 needs_verification 态受理；wecom 无此环节）。
func (svc ImBot) ProvisionVerifyCode(req *types.ImBotProvisionVerifyCodeRequest) error {
	if req.Channel != enums.CHANNEL_WEIXIN {
		return errors.New("该渠道无需配对码")
	}
	return weixin.SubmitVerifyCode(req.AttemptID, req.VerifyCode)
}

// weixinLocalTokens 既有 weixin bot 行的 token 清单（BeginLogin 服务端绑定感知入参）。
func (svc ImBot) weixinLocalTokens() []string {
	q := query.Use(svc.Orm)
	rows, err := q.ImBot.WithContext(svc.Context).Where(q.ImBot.Channel.Eq(enums.CHANNEL_WEIXIN)).Find()
	if err != nil {
		return nil
	}
	tokens := make([]string, 0, len(rows))
	for _, row := range rows {
		var cred weixin.Credential
		if json.Unmarshal([]byte(row.Credential), &cred) == nil && cred.Token != "" {
			tokens = append(tokens, cred.Token)
		}
	}
	return tokens
}

// provisionView wecom.AttemptView → DTO（同构字段直拷， secrets 已在源头剥除）。
func provisionView(v wecom.AttemptView) *types.ImBotProvisionView {
	return &types.ImBotProvisionView{
		AttemptID:      v.AttemptID,
		State:          v.State,
		QRContent:      v.QRContent,
		ExpiresAt:      v.ExpiresAt,
		PollIntervalMs: v.PollIntervalMs,
		ErrCode:        v.ErrCode,
		BotID:          v.BotID,
	}
}

// weixinProvisionView weixin.AttemptView → DTO（同构字段直拷，凭据已在源头剥除）。
func weixinProvisionView(v weixin.AttemptView) *types.ImBotProvisionView {
	return &types.ImBotProvisionView{
		AttemptID:      v.AttemptID,
		State:          v.State,
		QRContent:      v.QRContent,
		ExpiresAt:      v.ExpiresAt,
		PollIntervalMs: v.PollIntervalMs,
		ErrCode:        v.ErrCode,
		BotID:          v.BotID,
	}
}

/*
 * ProvisionActivateBot 扫码授权成功的激活回调（main 装配注入 wecom.SetProvisionActivator，
 * 进程级函数——不依赖请求上下文，同 StartEnabled 口径）。botId 查重 → 建 bot 行
 * （enabled=Y、工作空间待补选、默认开放白名单、名称默认可后改）→ 拉起连接。
 */
func ProvisionActivateBot(remoteBotID, secret string) (string, error) {
	if global.BotSupervisor == nil {
		return "", errors.New("bot 运行时未就绪")
	}
	q := query.Use(global.SqliteDB)
	rows, err := q.ImBot.WithContext(context.Background()).Find()
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		var cred types.ImBotCredential
		if json.Unmarshal([]byte(row.Credential), &cred) == nil && cred.BotId == remoteBotID {
			return "", fmt.Errorf("该机器人已接入（%s），无需重复扫码", row.Name)
		}
	}
	policy, _ := json.Marshal(types.ImBotAccessPolicy{Mode: bot.AccessModeOpen, AllowUsers: []string{}})
	row := &model.ImBot{
		ID:           uuid.Must(uuid.NewV7()).String(),
		Name:         "企业微信机器人",
		Channel:      enums.CHANNEL_WECOM,
		Credential:   credentialJSON(remoteBotID, secret),
		WorkspaceID:  "",
		AccessPolicy: string(policy),
		Enabled:      enums.YES_NO_YES,
	}
	if err := q.ImBot.WithContext(context.Background()).Create(row); err != nil {
		return "", err
	}
	if err := global.BotSupervisor.ApplyBot(row); err != nil {
		return "", err
	}
	return row.ID, nil
}

/*
 * ProvisionActivateWeixin 微信扫码授权成功的激活回调（main 装配注入 weixin.SetProvisionActivator，
 * 进程级函数——不依赖请求上下文，同 ProvisionActivateBot 口径）。rebindBotID 非空 = 换绑（仅更新
 * 凭据，锚点/白名单全保留）；否则 accountId 查重 → 新建 bot 行（白名单 [ownerUserId]，工作空间
 * 待补选）→ 拉起连接。
 */
func ProvisionActivateWeixin(cred weixin.Credential, rebindBotID string) (string, error) {
	if global.BotSupervisor == nil {
		return "", errors.New("bot 运行时未就绪")
	}
	if cred.Token == "" || cred.BaseURL == "" || cred.AccountID == "" {
		return "", errors.New("微信授权凭据不完整")
	}
	q := query.Use(global.SqliteDB)
	rows, err := q.ImBot.WithContext(context.Background()).Find()
	if err != nil {
		return "", err
	}
	if rebindBotID != "" {
		return rebindWeixinBot(q, rows, cred, rebindBotID)
	}
	for _, row := range rows {
		var existing weixin.Credential
		if row.Channel == enums.CHANNEL_WEIXIN &&
			json.Unmarshal([]byte(row.Credential), &existing) == nil && existing.AccountID == cred.AccountID {
			return "", fmt.Errorf("该微信账号已接入（%s），无需重复扫码", row.Name)
		}
	}
	credJSON, _ := json.Marshal(cred)
	policy, _ := json.Marshal(types.ImBotAccessPolicy{Mode: bot.AccessModeAllowlist, AllowUsers: []string{cred.OwnerUserID}})
	row := &model.ImBot{
		ID:           uuid.Must(uuid.NewV7()).String(),
		Name:         "微信机器人",
		Channel:      enums.CHANNEL_WEIXIN,
		Credential:   string(credJSON),
		WorkspaceID:  "",
		AccessPolicy: string(policy),
		Enabled:      enums.YES_NO_YES,
	}
	if err := q.ImBot.WithContext(context.Background()).Create(row); err != nil {
		return "", err
	}
	if err := global.BotSupervisor.ApplyBot(row); err != nil {
		return "", err
	}
	return row.ID, nil
}

/*
 * rebindWeixinBot 换绑路径：目标行须为 weixin 渠道且与扫码同账号（防白名单机主错位死锁），
 * 仅更新凭据列并清 channel_state（游标与旧 token 绑定，换凭据必须归零防旧游标上线），
 * 其余配置全保留。
 */
func rebindWeixinBot(q *query.Query, rows []*model.ImBot, cred weixin.Credential, rebindBotID string) (string, error) {
	found := false
	for _, row := range rows {
		if row.ID == rebindBotID {
			if row.Channel != enums.CHANNEL_WEIXIN {
				return "", errors.New("目标机器人不是微信渠道，无法换绑")
			}
			var existing weixin.Credential
			if json.Unmarshal([]byte(row.Credential), &existing) == nil &&
				existing.AccountID != "" && existing.AccountID != cred.AccountID {
				return "", weixin.ErrAccountMismatch
			}
			found = true
			break
		}
	}
	if !found {
		return "", errors.New("目标机器人不存在，无法换绑")
	}
	credJSON, _ := json.Marshal(cred)
	if _, err := q.ImBot.WithContext(context.Background()).Where(q.ImBot.ID.Eq(rebindBotID)).
		UpdateSimple(q.ImBot.Credential.Value(string(credJSON)), q.ImBot.ChannelState.Value("{}")); err != nil {
		return "", err
	}
	row, err := q.ImBot.WithContext(context.Background()).Where(q.ImBot.ID.Eq(rebindBotID)).First()
	if err != nil {
		return "", err
	}
	if err := global.BotSupervisor.ApplyBot(row); err != nil {
		return "", err
	}
	return row.ID, nil
}

// ---------- 内部工具 ----------

// mustGet 按 id 取 bot 行（不存在给中文错误）。
func (svc ImBot) mustGet(id string) (*model.ImBot, error) {
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

/*
 * applyRuntime 落库成功后的连接热更新：启用 → ApplyBot（Stop→Start），停用 → StopBot。
 * 失败仅日志（DB 已是 SSOT，运行时漂移可经重启连接/重启应用收敛）。
 */
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
			zap.String("botID", row.ID), zap.Error(err))
	}
}

/*
 * mergeCurrentStatus 取该 bot 当前运行态合并进响应（Create/Update 后响应即真实状态，
 * 前端 setQueryData 一次往返不出现「已启用+未运行」的矛盾投影）。
 */
func (svc ImBot) mergeCurrentStatus(item *types.ImBotResponseData, row *model.ImBot) {
	var st bot.BotStatusView
	if global.BotSupervisor != nil {
		st, _ = global.BotSupervisor.StatusOf(row.ID)
	}
	mergeRuntimeStatus(item, row, st)
}

/*
 * mergeRuntimeStatus 运行态合并进响应：running=false（未运行/未注册适配器）时
 * ConnState=stopped，LastError 回落 DB 行 last_error（跨重启可见）。
 */
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

// ensureWorkspaceExists 工作空间存在性校验（bot 必选工作空间）。
func (svc ImBot) ensureWorkspaceExists(id string) error {
	q := query.Use(svc.Orm)
	if _, err := q.Workspace.WithContext(svc.Context).Where(q.Workspace.ID.Eq(id)).First(); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("工作空间不存在")
		}
		return err
	}
	return nil
}

// workspaceNames id → name 一次性映射（响应装配展示用）。
func (svc ImBot) workspaceNames() map[string]string {
	q := query.Use(svc.Orm)
	rows, err := q.Workspace.WithContext(svc.Context).Find()
	if err != nil {
		return map[string]string{}
	}
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		names[row.ID] = row.Name
	}
	return names
}

/*
 * ensureBotIdAvailable credential.botId 查重（JSON 列无法建唯一索引，service 层扫描校验；
 * excludeID 供 update 排除自身）。
 */
func (svc ImBot) ensureBotIdAvailable(botId string, excludeID string) error {
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
