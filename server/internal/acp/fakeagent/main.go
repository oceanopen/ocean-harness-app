/*
 * fakeagent 是 internal/acp 集成测试的进程内假 agent：经 ironpark acp1 agent 面跑真实 stdio，行为由 argv[1] 脚本驱动，TestMain 编译为独立二进制供 acp.Launch 拉起。
 * 脚本一览：happy 两段 chunk 后 end_turn；permission 发起 request_permission，返回后按 canceled 标记收敛（取消则不再回显）；late-permission 先 end_turn、300ms 后再发起权限（无活动回合路径）；elicitation 发起 form elicitation 并回显结果；slow-cancel 持续推送 chunk 直至 session/cancel 以 cancelled 终止。
 * doctor-happy 响应后 200ms 推 available_commands（握手探针门槛）；early-push 响应前抢跑推 chunk + prompt 补推一段；load 声明 load 能力且响应前重放两段历史；crash 响应后 200ms 整进程退出 1（EOF → 收敛序 → Terminated 哨兵）。
 */
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
)

const (
	scriptHappy          = "happy"
	scriptPermission     = "permission"
	scriptLatePermission = "late-permission"
	scriptElicitation    = "elicitation"
	scriptSlowCancel     = "slow-cancel"
	scriptDoctorHappy    = "doctor-happy"
	scriptEarlyPush      = "early-push"
	scriptLoad           = "load"
	scriptCrash          = "crash"
)

/*
 * fakeAgent 全脚本共用骨架：conn 是 client 侧 typed 发信面（推 update、权限、elicitation），
 * canceled 记录已收 session/cancel 的会话，permission/slow-cancel 据此以 cancelled 收敛。
 */
type fakeAgent struct {
	conn     *acp1.AgentSideConnection
	mu       sync.Mutex
	canceled map[string]bool
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fakeagent <script>")
		os.Exit(2)
	}
	if _, ok := map[string]bool{
		scriptHappy: true, scriptPermission: true, scriptLatePermission: true,
		scriptElicitation: true, scriptSlowCancel: true, scriptDoctorHappy: true,
		scriptEarlyPush: true, scriptLoad: true, scriptCrash: true,
	}[os.Args[1]]; !ok {
		fmt.Fprintf(os.Stderr, "fakeagent: 未知脚本 %q\n", os.Args[1])
		os.Exit(2)
	}
	conn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return &fakeAgent{conn: c, canceled: make(map[string]bool)}
	}, acp.NewStdioTransport(os.Stdin, os.Stdout))
	if err := conn.Start(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent serve: %v\n", err)
		os.Exit(1)
	}
}

func (a *fakeAgent) Initialize(_ context.Context, _ *acp1.InitializeRequest) (*acp1.InitializeResponse, error) {
	caps := &acp1.AgentCapabilities{}
	/*
	 * 仅 load 脚本声明 session/load 能力：其余脚本保持空能力，保留「能力未声明 →
	 * 客户端 requireCapability 拒绝」的可测路径。
	 */
	if os.Args[1] == scriptLoad {
		caps.LoadSession = ptr(true)
	}
	return &acp1.InitializeResponse{ProtocolVersion: acp1.ProtocolVersion, AgentCapabilities: caps}, nil
}

func (a *fakeAgent) NewSession(ctx context.Context, _ *acp1.NewSessionRequest) (*acp1.NewSessionResponse, error) {
	id := acp1.SessionID("fake-" + strconv.Itoa(int(time.Now().UnixNano())))
	/*
	 * doctor-happy：响应落 wire 后再推送 available_commands——客户端处理完 session/new
	 * 响应才注册会话运行时，acpdoctor 以收到该推送为握手门槛。
	 */
	if os.Args[1] == scriptDoctorHappy {
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = a.notify(context.Background(), id, acp1.NewSessionUpdate(acp1.SessionUpdateAvailableCommandsUpdate{
				AvailableCommands: []acp1.AvailableCommand{{Name: "create_plan", Description: "创建实现计划"}},
			}))
		}()
	}
	// early-push：响应落 wire 前同步抢跑推送——命中客户端「早于会话注册」的早到帧缓冲路径。
	if os.Args[1] == scriptEarlyPush {
		if err := a.chunk(ctx, id, "early"); err != nil {
			return nil, err
		}
	}
	// crash：响应落 wire 后整进程退出（客户端 EOF → 收敛序 → Terminated 哨兵 + Err 证据）。
	if os.Args[1] == scriptCrash {
		go func() {
			time.Sleep(200 * time.Millisecond)
			os.Exit(1)
		}()
	}
	return &acp1.NewSessionResponse{
		SessionID: id,
		Modes: &acp1.SessionModeState{
			AvailableModes: []acp1.SessionMode{
				{ID: "default", Name: "Default"},
				{ID: "acceptEdits", Name: "Accept Edits"},
				{ID: "bypassPermissions", Name: "Bypass"},
			},
			CurrentModeID: "default",
		},
	}, nil
}

func (a *fakeAgent) CancelSession(_ context.Context, notification *acp1.CancelNotification) error {
	a.mu.Lock()
	a.canceled[string(notification.SessionID)] = true
	a.mu.Unlock()
	return nil
}

func (a *fakeAgent) SetSessionMode(ctx context.Context, request *acp1.SetSessionModeRequest) (*acp1.SetSessionModeResponse, error) {
	// 补发 current_mode_update：客户端本地模式视图的唯一回写通道。
	if err := a.notify(ctx, request.SessionID, acp1.NewSessionUpdate(acp1.SessionUpdateCurrentModeUpdate{CurrentModeID: request.ModeID})); err != nil {
		return nil, err
	}
	return &acp1.SetSessionModeResponse{}, nil
}

func (a *fakeAgent) Prompt(ctx context.Context, request *acp1.PromptRequest) (*acp1.PromptResponse, error) {
	switch os.Args[1] {
	case scriptHappy, scriptLoad: // load：恢复后回合与 happy 同款（回声两段 + end_turn）
		if err := a.chunk(ctx, request.SessionID, "你好，"); err != nil {
			return nil, err
		}
		if err := a.chunk(ctx, request.SessionID, "ACP 回合完成"); err != nil {
			return nil, err
		}
		return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil

	case scriptPermission:
		response, err := a.conn.RequestPermission(ctx, &acp1.RequestPermissionRequest{
			SessionID: request.SessionID,
			Options:   permissionOptions(),
			ToolCall:  acp1.ToolCallUpdate{ToolCallID: "tool-1", Title: ptr("Bash: echo hi")},
		})
		if err != nil {
			return nil, err
		}
		// session/cancel 已达（真实 agent 的取消收敛语义）：回合以 cancelled 终止，不推进回显。
		if a.isCanceled(request.SessionID) {
			return &acp1.PromptResponse{StopReason: acp1.StopReasonCancelled}, nil
		}
		echo := "permission:cancelled"
		if selected, ok := response.Outcome.As[acp1.RequestPermissionOutcomeSelected](); ok {
			echo = "permission:selected:" + string(selected.OptionID)
		}
		if err := a.chunk(ctx, request.SessionID, echo); err != nil {
			return nil, err
		}
		return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil

	case scriptLatePermission:
		// prompt 立即收尾，300ms 后才发起权限请求：命中客户端「无活动回合」路径。
		go func() {
			time.Sleep(300 * time.Millisecond)
			echo := "late-permission:cancelled"
			if response, err := a.conn.RequestPermission(context.Background(), &acp1.RequestPermissionRequest{
				SessionID: request.SessionID,
				Options:   permissionOptions(),
			}); err == nil {
				if selected, ok := response.Outcome.As[acp1.RequestPermissionOutcomeSelected](); ok {
					echo = "late-permission:selected:" + string(selected.OptionID)
				}
			}
			_ = a.chunk(context.Background(), request.SessionID, echo)
		}()
		return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil

	case scriptElicitation:
		response, err := a.elicitationChoice(ctx, request.SessionID)
		if err != nil {
			return nil, err
		}
		echo := "elicitation:decline"
		if accept, ok := response.As[acp1.CreateElicitationResponseAccept](); ok {
			echo = "elicitation:accept"
			if choice, ok := accept.Content["choice"]; ok {
				if text, err := choice.As[string](); err == nil {
					echo = "elicitation:accept:choice=" + text
				}
			}
		} else if _, ok := response.As[acp1.CreateElicitationResponseCancel](); ok {
			echo = "elicitation:cancel"
		}
		if err := a.chunk(ctx, request.SessionID, echo); err != nil {
			return nil, err
		}
		return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil

	case scriptSlowCancel:
		for i := 0; ; i++ {
			if a.isCanceled(request.SessionID) {
				return &acp1.PromptResponse{StopReason: acp1.StopReasonCancelled}, nil
			}
			if err := a.chunk(ctx, request.SessionID, fmt.Sprintf("tick-%d", i)); err != nil {
				return nil, err
			}
			time.Sleep(50 * time.Millisecond)
		}

	case scriptEarlyPush:
		if err := a.chunk(ctx, request.SessionID, "late"); err != nil {
			return nil, err
		}
		return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
	}
	return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
}

/*
 * LoadSession load 脚本专用：响应前同步重放两段历史 chunk（模拟适配器 replaySessionHistory
 * 时序——重放先于 load 响应到达客户端，走早到帧缓冲）；其余脚本未广告 load 能力，正常不会到达。
 */
func (a *fakeAgent) LoadSession(ctx context.Context, request *acp1.LoadSessionRequest) (*acp1.LoadSessionResponse, error) {
	if os.Args[1] != scriptLoad {
		return nil, acp.MethodNotFound("session/load")
	}
	for _, text := range []string{"历史-1", "历史-2"} {
		if err := a.chunk(ctx, request.SessionID, text); err != nil {
			return nil, err
		}
	}
	return &acp1.LoadSessionResponse{
		Modes: &acp1.SessionModeState{
			AvailableModes: []acp1.SessionMode{{ID: "default", Name: "Default"}},
			CurrentModeID:  "default",
		},
	}, nil
}

func (a *fakeAgent) notify(ctx context.Context, sessionID acp1.SessionID, update acp1.SessionUpdate) error {
	return a.conn.SessionUpdate(ctx, &acp1.SessionNotification{SessionID: sessionID, Update: update})
}

func (a *fakeAgent) chunk(ctx context.Context, sessionID acp1.SessionID, text string) error {
	return a.notify(ctx, sessionID, acp1.NewSessionUpdate(acp1.SessionUpdateAgentMessageChunk{Content: acp1.TextBlock(text)}))
}

func (a *fakeAgent) isCanceled(sessionID acp1.SessionID) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.canceled[string(sessionID)]
}

func permissionOptions() []acp1.PermissionOption {
	return []acp1.PermissionOption{
		{OptionID: "allow", Kind: acp1.PermissionOptionKindAllowOnce, Name: "Allow"},
		{OptionID: "reject", Kind: acp1.PermissionOptionKindRejectOnce, Name: "Reject"},
	}
}

// elicitationChoice 发起 form 模式 elicitation：typed 直发即真适配器的扁平 wire 形态（mode/sessionId/message/requestedSchema 平铺）。
func (a *fakeAgent) elicitationChoice(ctx context.Context, sessionID acp1.SessionID) (*acp1.CreateElicitationResponse, error) {
	params, err := acp1.NewCreateElicitationRequest(acp1.CreateElicitationRequestFormSession{
		SessionID: sessionID,
		Message:   "请选择一个选项",
		RequestedSchema: acp1.ElicitationSchema{
			Type: ptr(acp1.ElicitationSchemaTypeObject),
			Properties: map[string]acp1.ElicitationPropertySchema{
				"choice": acp1.NewElicitationPropertySchema(acp1.ElicitationPropertySchemaString{Title: ptr("选项")}),
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return a.conn.CreateElicitation(ctx, &params)
}

func ptr[T any](v T) *T { return &v }
