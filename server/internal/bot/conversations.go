package bot

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gen/field"
	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

// seenWindowLimit 消息幂等滚窗上限：企微 WS 无补投语义，幂等仅防瞬时重推，100 条远超重推窗口。
const seenWindowLimit = 100

/*
 * ConversationStore bot 域会话行唯一读写点（唯一会话源）：bot 字段落「该 bot 消息实际驱动的
 * 会话行」；锚语义见 EnsureConversation。两类调用上下文（回合 worker / 拦截步回调）写列正交、
 * 单语句 UPDATE 原子；DB 是 SSOT。幂等滚窗为进程内存（见 HasSeen），不落库。
 */
type ConversationStore struct {
	DB *gorm.DB

	// seenMu/seenWindows 消息幂等滚窗（进程内存，重启清零——重推仅瞬时窗口，重启后重复
	// 处理可接受）：键 = <botID>:<conversationKey> → 最近 100 条已处理 msgid，超限丢最旧。
	seenMu      sync.Mutex
	seenWindows map[string][]string
}

/*
 * EnsureConversation 取本 bot 在当前归属下的会话行（幂等键 bot_id+key），不存在则按档定位/
 * 创建（project 档认领任务主会话行 = 与桌面共享同会话；他 bot 占用降级子会话行）；归属漂移
 * 释放旧行 + 活动时戳随迁。行创建即预置锚（pending）；mode 初值 terminal，ACP 路由层覆写档位。
 */
func (s *ConversationStore) EnsureConversation(cfg BotRuntimeConfig, key string) (*model.Conversation, error) {
	q := query.Use(s.DB)
	ownerType, ownerID, err := botConversationOwner(cfg)
	if err != nil {
		return nil, err
	}
	conv, err := s.findByBotKey(q, cfg.BotID, key)
	if err != nil {
		return nil, err
	}
	var carryLastAt *time.Time
	if conv != nil {
		if conv.OwnerType == ownerType && conv.OwnerID == ownerID {
			return conv, nil
		}
		// 归属漂移：释放旧行 bot 字段（行留旧归属，会话历史不动），活动时戳随迁新行。
		carryLastAt = conv.LastMessageAt
		if _, err := q.Conversation.WithContext(context.Background()).Where(q.Conversation.ID.Eq(conv.ID)).UpdateSimple(
			q.Conversation.BotID.Value(""),
			q.Conversation.BotChannel.Value(enums.Channel("")),
			q.Conversation.BotConversationKey.Value(""),
			q.Conversation.UpdatedAt.Value(time.Now()),
		); err != nil {
			return nil, err
		}
	}
	parentID := ""
	if ownerType == enums.OWNER_TYPE_TASK {
		main, e := q.Conversation.WithContext(context.Background()).Where(
			q.Conversation.OwnerType.Eq(enums.OWNER_TYPE_TASK),
			q.Conversation.OwnerID.Eq(ownerID),
			q.Conversation.ParentID.Eq(""),
		).First()
		switch {
		case e == nil && main.BotID == "":
			claimed, ce := s.claimConversation(q, main, cfg, key, carryLastAt)
			return claimed, ce
		case e == nil:
			parentID = main.ID // 他 bot 已占用主会话：降级建子会话行（bot 各自独立会话）
		case !errors.Is(e, gorm.ErrRecordNotFound):
			return nil, e
		}
	}
	row := newBotConversationRow(ownerType, ownerID, parentID, cfg, key, carryLastAt)
	if err := q.Conversation.WithContext(context.Background()).Create(row); err != nil {
		return nil, err
	}
	return row, nil
}

/*
 * newBotConversationRow bot 会话行统一构造：预置锚 + terminal 初值档 + bot 三字段（归属漂移
 * 时活动时戳随迁）。认领降级与定位新建两路径共用此形态。
 */
func newBotConversationRow(ownerType enums.ConversationOwnerType, ownerID, parentID string, cfg BotRuntimeConfig, key string, lastAt *time.Time) *model.Conversation {
	sessionID, status := model.NewPresetClaudeSession()
	return &model.Conversation{
		ID:                  uuid.Must(uuid.NewV7()).String(),
		OwnerType:           ownerType,
		OwnerID:             ownerID,
		Title:               "IM 会话",
		Mode:                enums.CONVERSATION_MODE_TERMINAL,
		ClaudeSessionID:     sessionID,
		ClaudeSessionStatus: status,
		ParentID:            parentID,
		BotID:               cfg.BotID,
		BotChannel:          cfg.Channel,
		BotConversationKey:  key,
		LastMessageAt:       lastAt,
	}
}

// findByBotKey 按 bot 幂等键查行（无行返回 nil, nil）。
func (s *ConversationStore) findByBotKey(q *query.Query, botID, key string) (*model.Conversation, error) {
	conv, err := q.Conversation.WithContext(context.Background()).Where(
		q.Conversation.BotID.Eq(botID),
		q.Conversation.BotConversationKey.Eq(key),
	).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return conv, nil
}

/*
 * claimConversation 认领既有主会话行：bot 字段条件落行（仅当 bot_id 为空——并发认领败方
 * RowsAffected=0，由本函数尾段降级建子会话行）。活动时戳随迁；行本体的标题/档位/锚不动。
 */
func (s *ConversationStore) claimConversation(q *query.Query, main *model.Conversation, cfg BotRuntimeConfig, key string, lastAt *time.Time) (*model.Conversation, error) {
	res, err := q.Conversation.WithContext(context.Background()).Where(
		q.Conversation.ID.Eq(main.ID),
		q.Conversation.BotID.Eq(""),
	).UpdateSimple(botClaimAssigns(q, cfg, key, lastAt)...)
	if err != nil {
		return nil, err
	}
	if res.RowsAffected == 0 {
		/*
		 * 条件更新败方：占用者可能是同 bot 的并发链路（回调 TouchLastMessage/HasSeen 与
		 * 回合 worker 并发首条消息）——先按幂等键重读兜底（命中即返回既有行，与建行路径
		 * 同款）；重读仍无行才是真被他 bot 占用，降级建子会话行。
		 */
		if retry, re := s.findByBotKey(q, cfg.BotID, key); re == nil && retry != nil {
			return retry, nil
		}
		row := newBotConversationRow(main.OwnerType, main.OwnerID, main.ID, cfg, key, lastAt)
		if err := q.Conversation.WithContext(context.Background()).Create(row); err != nil {
			return nil, err
		}
		return row, nil
	}
	main.BotID = cfg.BotID
	main.BotChannel = cfg.Channel
	main.BotConversationKey = key
	main.LastMessageAt = lastAt
	return main, nil
}

/*
 * botClaimAssigns 认领落行的 bot 字段赋值集：活动时戳为可空列（nil = 不触碰，非空才写入），
 * updated_at 随写刷新。
 */
func botClaimAssigns(q *query.Query, cfg BotRuntimeConfig, key string, lastAt *time.Time) []field.AssignExpr {
	assigns := []field.AssignExpr{
		q.Conversation.BotID.Value(cfg.BotID),
		q.Conversation.BotChannel.Value(cfg.Channel),
		q.Conversation.BotConversationKey.Value(key),
		q.Conversation.UpdatedAt.Value(time.Now()),
	}
	if lastAt != nil {
		assigns = append(assigns, q.Conversation.LastMessageAt.Value(*lastAt))
	}
	return assigns
}

// botConversationOwner bot 会话行归属维度（workspace 档 = 工作空间会话；project 档 = 任务会话）。
func botConversationOwner(cfg BotRuntimeConfig) (enums.ConversationOwnerType, string, error) {
	switch cfg.ConversationType {
	case enums.CONVERSATION_TYPE_WORKSPACE:
		if cfg.WorkspaceID == "" {
			return "", "", errors.New("工作空间档未选择工作空间")
		}
		return enums.OWNER_TYPE_WORKSPACE, cfg.WorkspaceID, nil
	case enums.CONVERSATION_TYPE_PROJECT:
		if cfg.TaskID == "" {
			return "", "", errors.New("项目档未关联任务")
		}
		return enums.OWNER_TYPE_TASK, cfg.TaskID, nil
	default:
		return "", "", errors.New("会话类型未配置")
	}
}

/*
 * HeadlessAnchor 读 headless 驱动锚（二择互斥，恒一端为空）：active 态 → resumeID（--resume
 * 续聊）；pending 态 → presetID（建行预生成值，--session-id 开新会话即预置 id）。读失败按
 * 新会话处理由调用方兜底。
 */
func (s *ConversationStore) HeadlessAnchor(cfg BotRuntimeConfig, key string) (resumeID, presetID string, err error) {
	conv, err := s.EnsureConversation(cfg, key)
	if err != nil {
		return "", "", err
	}
	if conv.ClaudeSessionStatus == enums.CLAUDE_SESSION_STATUS_ACTIVE {
		return conv.ClaudeSessionID, "", nil
	}
	return "", conv.ClaudeSessionID, nil
}

// FinishTurn 回合收尾持久化：锚以实际捕获 id 回写（绝对一致）+ 置 active + 最近消息时间。
func (s *ConversationStore) FinishTurn(cfg BotRuntimeConfig, key, sessionID string, at time.Time) error {
	q := query.Use(s.DB)
	_, err := q.Conversation.WithContext(context.Background()).Where(
		q.Conversation.BotID.Eq(cfg.BotID),
		q.Conversation.BotConversationKey.Eq(key),
	).UpdateSimple(
		q.Conversation.ClaudeSessionID.Value(sessionID),
		q.Conversation.ClaudeSessionStatus.Value(enums.CLAUDE_SESSION_STATUS_ACTIVE),
		q.Conversation.LastMessageAt.Value(at),
		q.Conversation.UpdatedAt.Value(time.Now()),
	)
	return err
}

/*
 * TouchLastMessage 活动时间戳顺延（受理即触碰）：行不存在先按档定位/创建（工作空间未选等
 * 归属缺失场景报错，调用方仅告警不阻断受理）。
 */
func (s *ConversationStore) TouchLastMessage(cfg BotRuntimeConfig, key string, at time.Time) error {
	if _, err := s.EnsureConversation(cfg, key); err != nil {
		return err
	}
	q := query.Use(s.DB)
	_, err := q.Conversation.WithContext(context.Background()).Where(
		q.Conversation.BotID.Eq(cfg.BotID),
		q.Conversation.BotConversationKey.Eq(key),
	).UpdateSimple(
		q.Conversation.LastMessageAt.Value(at),
		q.Conversation.UpdatedAt.Value(time.Now()),
	)
	return err
}

/*
 * ResetSessionID 会话锚重置（/clear 与失败自愈）：换新预置 id 并回 pending——下条消息以
 * 新 id 开新会话（旧会话文件留存于 claude 本地存储，按会话维度可定位）。
 */
func (s *ConversationStore) ResetSessionID(cfg BotRuntimeConfig, key string) error {
	sessionID, status := model.NewPresetClaudeSession()
	q := query.Use(s.DB)
	_, err := q.Conversation.WithContext(context.Background()).Where(
		q.Conversation.BotID.Eq(cfg.BotID),
		q.Conversation.BotConversationKey.Eq(key),
	).UpdateSimple(
		q.Conversation.ClaudeSessionID.Value(sessionID),
		q.Conversation.ClaudeSessionStatus.Value(status),
		q.Conversation.UpdatedAt.Value(time.Now()),
	)
	return err
}

// HasSeen 幂等查询：msgid 是否在本会话内存滚窗内（重启清零，重复处理可接受）。
func (s *ConversationStore) HasSeen(cfg BotRuntimeConfig, key, msgID string) bool {
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	for _, id := range s.seenWindows[seenWindowKey(cfg.BotID, key)] {
		if id == msgID {
			return true
		}
	}
	return false
}

/*
 * MarkSeen 受理时落定（不回滚）：回合失败也已用错误终帧回复了用户，视为已处理；
 * 避免失败重推永远卡死的墓碑问题。滚窗超限丢最旧。
 */
func (s *ConversationStore) MarkSeen(cfg BotRuntimeConfig, key, msgID string) {
	windowKey := seenWindowKey(cfg.BotID, key)
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	ids := append(s.seenWindows[windowKey], msgID)
	if len(ids) > seenWindowLimit {
		ids = ids[len(ids)-seenWindowLimit:]
	}
	if s.seenWindows == nil {
		s.seenWindows = map[string][]string{}
	}
	s.seenWindows[windowKey] = ids
}

// seenWindowKey 幂等滚窗的内存键。
func seenWindowKey(botID, key string) string {
	return botID + ":" + key
}

// SetMode 档位对齐（路由层每回合幂等覆写：会话行 mode 跟随容器 launch 档）。
func (s *ConversationStore) SetMode(conversationID string, mode enums.ConversationMode) error {
	q := query.Use(s.DB)
	_, err := q.Conversation.WithContext(context.Background()).Where(q.Conversation.ID.Eq(conversationID)).UpdateSimple(
		q.Conversation.Mode.Value(mode),
		q.Conversation.UpdatedAt.Value(time.Now()),
	)
	return err
}
