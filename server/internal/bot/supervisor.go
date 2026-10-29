package bot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

/*
 * Supervisor bot 运行生命周期：Factory 注册表 → 单 bot 运行时（ChannelRuntime + 装配参数）
 * 的启停/重启/状态投影。连接的重连由渠道 SDK 自管（指数退避）；被新连接踢下线属终态、
 * 不自动重连（防双实例互踢风暴），恢复手段是 UI 的重启连接。
 */
type Supervisor struct {
	orch    *Orchestrator
	db      *gorm.DB
	port    int // sidecar 自身端口（claude 子进程 OCEAN_HARNESS_PORT 注入，插件工具链用）
	log     *zap.Logger
	mu      sync.Mutex
	facs    map[enums.Channel]Factory
	lives   map[string]ChannelRuntime // botID → 运行时
	stopped bool                      // StopAll 已置位：后续 EnsureStarted 一律拒绝（防关机窗口复活连接）
}

// BotStatusView 状态投影行（getList/getInfo 合并到 ImBotResponseData 呈现）。
type BotStatusView struct {
	BotID     string
	State     string
	LastError string
}

/*
 * NewSupervisor 构造 supervisor（port 为 sidecar 自身 HTTP 端口；sessions 为 ACP 会话域，引擎路由按 workspace 启动模式消费——两引擎共存，见 driver_route.go）。
 * 路由驱动以三角色装配进编排器：ClaudeDriver（引擎路由）、pendingGate（审批数字快路径，T2.4）、 interactionHandler（审批/表单卡点击消费，T3.2/T3.3）——三者同源于 driverRoute 单实例，共享 resolve 与会话域消费面。
 * deliveries 为回合内文件投递注册表（nil = 投递链路禁用）。appDbPath 为 Rust 共享 app.db 路径
 * （忙时新消息策略读取，可空 = 回落默认）。
 */
func NewSupervisor(db *gorm.DB, port int, appDbPath string, sessions acpSessions, deliveries *DeliveryRegistry, log *zap.Logger) *Supervisor {
	s := &Supervisor{
		db:    db,
		port:  port,
		log:   log,
		facs:  make(map[enums.Channel]Factory),
		lives: make(map[string]ChannelRuntime),
	}
	route := newDriverRoute(db, sessions)
	s.orch = NewOrchestrator(&ConversationStore{DB: db}, route, route, route, log)
	s.orch.Deliveries = deliveries
	s.orch.AppDbPath = appDbPath
	return s
}

// Register 注册渠道工厂（main 装配时调用，每渠道一行）。
func (s *Supervisor) Register(f Factory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.facs[f.Channel()] = f
}

// StartEnabled 启动全部 enabled='Y' 的 bot（sidecar 启动路径；逐个容错，单个失败不阻断其余）。
func (s *Supervisor) StartEnabled() {
	q := query.Use(s.db)
	bots, err := q.ImBot.WithContext(context.Background()).Where(q.ImBot.Enabled.Eq(enums.YES_NO_YES)).Find()
	if err != nil {
		s.log.Error("bot 启动清单读取失败", zap.Error(err))
		return
	}
	for _, b := range bots {
		if err := s.ApplyBot(b); err != nil {
			s.log.Error("bot 启动失败（跳过）", zap.String("botID", b.ID), zap.Error(err))
		}
	}
}

/*
 * ApplyBot 应用 bot 行配置：解析 → EnsureStarted（启动清单 / create-update 启用 / 重启连接
 * 的统一入口；已在跑则先停再起 = 配置变更无脑重启）。成败均回写 t_im_bots.last_error
 * （成功清空）——连接终态错误跨重启可见（迁移列的承诺由本方法兑现）。
 */
func (s *Supervisor) ApplyBot(b *model.ImBot) error {
	cfg, err := s.botRuntimeConfig(b)
	if err != nil {
		s.setLastError(b.ID, err.Error())
		return err
	}
	if err := s.EnsureStarted(cfg); err != nil {
		s.setLastError(b.ID, err.Error())
		return err
	}
	s.setLastError(b.ID, "")
	return nil
}

/*
 * EnsureStarted 以新配置启动单个 bot。以 s.mu 全程串行（Factory.Create 与 rt.Start 均非
 * 阻塞、毫秒级临界区）——同一 bot 的并发调用（如前端双击重启）在此排队，杜绝「旧 runtime
 * 从注册表漏删、双连接互踢风暴」的交错窗口。
 */
func (s *Supervisor) EnsureStarted(cfg BotRuntimeConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped { // 生命周期守卫：StopAll 后无新启（与置位同持 s.mu，无交错窗口）
		return fmt.Errorf("supervisor 已停止，拒绝启动 bot %s", cfg.BotID)
	}
	f, ok := s.facs[cfg.Channel]
	if !ok {
		return fmt.Errorf("渠道 %q 未注册适配器", cfg.Channel)
	}
	s.stopBotLocked(cfg.BotID)

	rt, err := f.Create()
	if err != nil {
		return fmt.Errorf("渠道适配器创建失败: %w", err)
	}
	o := s.orch
	if err := rt.Start(cfg, func(msg InboundMessage) {
		o.HandleInbound(cfg, msg, func() (ReplyStream, error) { return rt.OpenReply(msg.Route) })
	}); err != nil {
		return fmt.Errorf("渠道连接启动失败: %w", err)
	}

	s.lives[cfg.BotID] = rt
	s.log.Info("bot 已启动", zap.String("botID", cfg.BotID), zap.String("channel", string(cfg.Channel)))
	return nil
}

// StopBot 停止单个 bot（未在跑为 no-op；幂等）。
func (s *Supervisor) StopBot(botID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopBotLocked(botID)
}

// stopBotLocked 停止并移出注册表（须持 s.mu）。
func (s *Supervisor) stopBotLocked(botID string) {
	if rt, ok := s.lives[botID]; ok {
		delete(s.lives, botID)
		rt.Stop()
		s.log.Info("bot 已停止", zap.String("botID", botID))
	}
}

/*
 * StopAll 优雅停止全部：断渠道连接（不再收新消息）→ 编排器关队列 + 取消在途回合。
 * 同时置位 stopped——此后全部 EnsureStarted（含 applier 延迟 goroutine）被拒绝，杜绝
 * 关机窗口内复活渠道连接。
 */
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	s.stopped = true
	lives := s.lives
	s.lives = make(map[string]ChannelRuntime)
	s.mu.Unlock()
	for _, rt := range lives {
		rt.Stop()
	}
	s.orch.Stop()
}

// Statuses 全部运行中 bot 的状态投影（运行时快照，不查 DB；getList 与 DB 行合并）。
func (s *Supervisor) Statuses() []BotStatusView {
	s.mu.Lock()
	defer s.mu.Unlock()
	views := make([]BotStatusView, 0, len(s.lives))
	for botID, rt := range s.lives {
		st := rt.Status()
		views = append(views, BotStatusView{BotID: botID, State: st.State, LastError: st.LastError})
	}
	return views
}

// StatusOf 单 bot 运行状态；未在运行返回 (零值, false)。
func (s *Supervisor) StatusOf(botID string) (BotStatusView, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, ok := s.lives[botID]
	if !ok {
		return BotStatusView{}, false
	}
	st := rt.Status()
	return BotStatusView{BotID: botID, State: st.State, LastError: st.LastError}, true
}

// setLastError 回写 last_error 列（尽力而为，失败仅日志——该列是排障可见性，非关键数据）。
func (s *Supervisor) setLastError(botID string, msg string) {
	if rs := []rune(msg); len(rs) > 500 {
		msg = string(rs[:500]) + "…"
	}
	q := query.Use(s.db)
	if _, err := q.ImBot.WithContext(context.Background()).Where(q.ImBot.ID.Eq(botID)).
		UpdateSimple(q.ImBot.LastError.Value(msg)); err != nil {
		s.log.Warn("bot last_error 回写失败", zap.String("botID", botID), zap.Error(err))
	}
}

/*
 * botRuntimeConfig t_im_bots 行 → 运行时装配参数（JSON 列在此解析，渠道凭据保持 JSON 透传）。
 * 会话根目录按档位解析（D8）：workspace 档取工作空间 dir；project 档取任务运行目录
 * {project.dir}/{taskId}/（与 ACP 会话 cwd 同构；未关联任务回落项目 dir）。缺失/未选 → 空串。
 */
func (s *Supervisor) botRuntimeConfig(b *model.ImBot) (BotRuntimeConfig, error) {
	policy, err := ParseAccessPolicy(b.AccessPolicy)
	if err != nil {
		return BotRuntimeConfig{}, fmt.Errorf("access_policy 解析失败: %w", err)
	}
	rootDir := ""
	switch b.ConversationType {
	case enums.CONVERSATION_TYPE_WORKSPACE:
		if b.WorkspaceID != "" {
			q := query.Use(s.db)
			ws, err := q.Workspace.WithContext(context.Background()).Where(q.Workspace.ID.Eq(b.WorkspaceID)).First()
			if err == nil {
				rootDir = ws.Dir
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return BotRuntimeConfig{}, fmt.Errorf("工作空间读取失败: %w", err)
			}
		}
	case enums.CONVERSATION_TYPE_PROJECT:
		if b.ProjectID != "" {
			q := query.Use(s.db)
			pr, err := q.Project.WithContext(context.Background()).Where(q.Project.ID.Eq(b.ProjectID)).First()
			if err == nil {
				rootDir = pr.Dir
				// 任务运行目录（与 ACP 会话 cwd 同构）；未关联任务时回落项目目录（回合由门禁拦截）。
				if b.TaskID != "" {
					if _, e := q.ProjectTask.WithContext(context.Background()).Where(q.ProjectTask.ID.Eq(b.TaskID)).First(); e != nil {
						return BotRuntimeConfig{}, fmt.Errorf("关联任务不存在（可能已删除，请重新配置）: %w", e)
					}
					rootDir = filepath.Join(pr.Dir, b.TaskID)
					if err := os.MkdirAll(rootDir, 0o755); err != nil {
						return BotRuntimeConfig{}, fmt.Errorf("任务目录创建失败: %w", err)
					}
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return BotRuntimeConfig{}, fmt.Errorf("项目读取失败: %w", err)
			}
		}
	}
	return BotRuntimeConfig{
		BotID:            b.ID,
		Channel:          b.Channel,
		Credential:       b.Credential,
		WorkspaceID:      b.WorkspaceID,
		ConversationType: b.ConversationType,
		ProjectID:        b.ProjectID,
		TaskID:           b.TaskID,
		WorkspaceDir:     rootDir,
		AccessPolicy:     policy,
		Port:             s.port,
		ChannelState:     b.ChannelState,
		SaveChannelState: s.setChannelState,
	}, nil
}

// setChannelState 回写 channel_state 列（渠道适配器自管运行态，如微信长轮询游标；尽力而为，失败仅日志——重启重放由核心幂等兜住）。
func (s *Supervisor) setChannelState(botID string, raw string) error {
	q := query.Use(s.db)
	if _, err := q.ImBot.WithContext(context.Background()).Where(q.ImBot.ID.Eq(botID)).
		UpdateSimple(q.ImBot.ChannelState.Value(raw)); err != nil {
		s.log.Warn("bot channel_state 回写失败", zap.String("botID", botID), zap.Error(err))
		return err
	}
	return nil
}
