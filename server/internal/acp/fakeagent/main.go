// fakeagent 是 internal/acp 集成测试的进程内假 agent：经真实 stdio 跑 agent.New().Serve，
// 行为由 argv[1] 脚本驱动。TestMain 会将其编译为独立二进制供 acp.Launch 拉起。
//
// 脚本一览：
//
//	happy            两段 agent_message_chunk 后 end_turn
//	permission       prompt 中发起 request_permission，把结果回显进 message chunk
//	late-permission  prompt 立即 end_turn，300ms 后再发起 request_permission（无活动回合路径）
//	elicitation      prompt 中发起 elicitation/create（线上扁平格式直发，含 requestedSchema），
//	                 把结果回显进 message chunk
//	slow-cancel      持续推送 chunk 直至收到 session/cancel，以 cancelled 终止
//	doctor-happy     session/new 响应后 200ms 推送 available_commands（T1.4 doctor 握手门槛）
//	early-push       session/new 响应前抢跑推送 chunk（早到帧缓冲回放路径）+ prompt 补推一段
//	crash            session/new 响应后 200ms 整进程退出 1（意外死亡路径：EOF → 收敛序 →
//	                 消费方收到 Terminated 哨兵且 Err() 携带异常退出证据）
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/BrokkAi/acp-go/agent"
	"github.com/BrokkAi/acp-go/schema"
)

const (
	scriptHappy          = "happy"
	scriptPermission     = "permission"
	scriptLatePermission = "late-permission"
	scriptElicitation    = "elicitation"
	scriptSlowCancel     = "slow-cancel"
	scriptDoctorHappy    = "doctor-happy"
	scriptEarlyPush      = "early-push"
	scriptCrash          = "crash"
)

// fakeAgent 全脚本共用的 agent 骨架：广告三档权限模式目录，SetMode 就地回写并补发
// current_mode_update 通知（客户端据此验证本地模式视图维护）。
type fakeAgent struct {
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
		scriptEarlyPush: true, scriptCrash: true,
	}[os.Args[1]]; !ok {
		fmt.Fprintf(os.Stderr, "fakeagent: 未知脚本 %q\n", os.Args[1])
		os.Exit(2)
	}
	a := &fakeAgent{canceled: make(map[string]bool)}
	if err := agent.New(a).Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent serve: %v\n", err)
		os.Exit(1)
	}
}

func (a *fakeAgent) Initialize(_ context.Context, _ agent.Client, _ schema.InitializeRequest) (schema.InitializeResponse, error) {
	return schema.InitializeResponse{ProtocolVersion: 1, AgentCapabilities: &schema.AgentCapabilities{}}, nil
}

func (a *fakeAgent) NewSession(_ context.Context, client agent.Client, _ schema.NewSessionRequest) (schema.NewSessionResponse, error) {
	id := schema.SessionId("fake-" + strconv.Itoa(int(time.Now().UnixNano())))
	// doctor-happy：响应落 wire 后再推送 available_commands——客户端在处理完 session/new
	// 响应才注册会话运行时，早于响应的推送会被当作早期帧丢弃（session.go 收帧过滤）。
	if os.Args[1] == scriptDoctorHappy {
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = client.Notify(context.Background(), schema.SessionUpdateMethodName, schema.SessionNotification{
				SessionID: id,
				Update: schema.SessionUpdate{AvailableCommandsUpdate: &schema.AvailableCommandsUpdate{
					AvailableCommands: []schema.AvailableCommand{
						{Name: "create_plan", Description: "创建实现计划"},
					},
				}},
			})
		}()
	}
	// early-push：响应落 wire 前同步抢跑推送——通知帧先于 session/new 响应到达客户端，
	// 命中「早于会话注册」路径（客户端缓冲后于 NewSession 返回时回放）。
	if os.Args[1] == scriptEarlyPush {
		_ = client.Notify(context.Background(), schema.SessionUpdateMethodName, schema.SessionNotification{
			SessionID: id,
			Update: schema.SessionUpdate{AgentMessageChunk: &schema.ContentChunk{
				Content: schema.ContentBlock{Text: &schema.TextContent{Text: "early"}},
			}},
		})
	}
	// crash：响应落 wire 后整进程退出（客户端 EOF → 收敛序 → Terminated 哨兵 + Err 证据）。
	if os.Args[1] == scriptCrash {
		go func() {
			time.Sleep(200 * time.Millisecond)
			os.Exit(1)
		}()
	}
	return schema.NewSessionResponse{
		SessionID: id,
		Modes: &schema.SessionModeState{
			AvailableModes: []schema.SessionMode{
				{ID: "default", Name: "Default"},
				{ID: "acceptEdits", Name: "Accept Edits"},
				{ID: "bypassPermissions", Name: "Bypass"},
			},
			CurrentModeID: "default",
		},
	}, nil
}

func (a *fakeAgent) CancelSession(_ context.Context, notification schema.CancelNotification) error {
	a.mu.Lock()
	a.canceled[string(notification.SessionID)] = true
	a.mu.Unlock()
	return nil
}

func (a *fakeAgent) SetMode(_ context.Context, client agent.Client, request schema.SetSessionModeRequest) (schema.SetSessionModeResponse, error) {
	// 补发 current_mode_update：客户端本地模式视图的唯一回写通道。
	if err := client.Notify(context.Background(), schema.SessionUpdateMethodName, schema.SessionNotification{
		SessionID: request.SessionID,
		Update:    schema.SessionUpdate{CurrentModeUpdate: &schema.CurrentModeUpdate{CurrentModeID: request.ModeID}},
	}); err != nil {
		return schema.SetSessionModeResponse{}, err
	}
	return schema.SetSessionModeResponse{}, nil
}

func (a *fakeAgent) Prompt(ctx context.Context, client agent.Client, request schema.PromptRequest, updates agent.SessionUpdater) (schema.PromptResponse, error) {
	switch os.Args[1] {
	case scriptHappy:
		if err := a.chunk(updates, request.SessionID, "你好，"); err != nil {
			return schema.PromptResponse{}, err
		}
		if err := a.chunk(updates, request.SessionID, "ACP 回合完成"); err != nil {
			return schema.PromptResponse{}, err
		}
		return schema.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil

	case scriptPermission:
		response, err := client.RequestPermission(ctx, schema.RequestPermissionRequest{
			SessionID: request.SessionID,
			Options:   permissionOptions(),
			ToolCall: schema.ToolCallUpdate{
				ToolCallID: "tool-1",
				Title:      strPtr("Bash: echo hi"),
			},
		})
		if err != nil {
			return schema.PromptResponse{}, err
		}
		echo := "permission:cancelled"
		if response.Outcome.Selected != nil {
			echo = "permission:selected:" + string(response.Outcome.Selected.OptionID)
		}
		if err := a.chunk(updates, request.SessionID, echo); err != nil {
			return schema.PromptResponse{}, err
		}
		return schema.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil

	case scriptLatePermission:
		// prompt 立即收尾，300ms 后才发起权限请求：命中客户端「无活动回合」路径。
		go func() {
			time.Sleep(300 * time.Millisecond)
			response, err := client.RequestPermission(context.Background(), schema.RequestPermissionRequest{
				SessionID: request.SessionID,
				Options:   permissionOptions(),
			})
			echo := "late-permission:cancelled"
			if err == nil && response.Outcome.Selected != nil {
				echo = "late-permission:selected:" + string(response.Outcome.Selected.OptionID)
			}
			_ = a.chunk(updates, request.SessionID, echo)
		}()
		return schema.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil

	case scriptElicitation:
		var response schema.CreateElicitationResponse
		if err := client.Call(ctx, schema.ElicitationCreateMethodName, elicitationParams(request.SessionID), &response); err != nil {
			return schema.PromptResponse{}, err
		}
		echo := "elicitation:decline"
		switch {
		case response.Accept != nil:
			echo = "elicitation:accept"
			if choice, ok := response.Accept.Content["choice"]; ok {
				echo = fmt.Sprintf("elicitation:accept:choice=%v", choice)
			}
		case response.Cancel != nil:
			echo = "elicitation:cancel"
		}
		if err := a.chunk(updates, request.SessionID, echo); err != nil {
			return schema.PromptResponse{}, err
		}
		return schema.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil

	case scriptSlowCancel:
		for i := 0; ; i++ {
			a.mu.Lock()
			canceled := a.canceled[string(request.SessionID)]
			a.mu.Unlock()
			if canceled {
				return schema.PromptResponse{StopReason: schema.StopReasonCancelled}, nil
			}
			if err := a.chunk(updates, request.SessionID, fmt.Sprintf("tick-%d", i)); err != nil {
				return schema.PromptResponse{}, err
			}
			time.Sleep(50 * time.Millisecond)
		}

	case scriptEarlyPush:
		if err := a.chunk(updates, request.SessionID, "late"); err != nil {
			return schema.PromptResponse{}, err
		}
		return schema.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil
	}
	return schema.PromptResponse{StopReason: schema.StopReasonEndTurn}, nil
}

func (a *fakeAgent) chunk(updates agent.SessionUpdater, sessionID schema.SessionId, text string) error {
	return updates.Update(schema.SessionUpdate{
		AgentMessageChunk: &schema.ContentChunk{Content: schema.ContentBlock{Text: &schema.TextContent{Text: text}}},
	})
}

func permissionOptions() []schema.PermissionOption {
	return []schema.PermissionOption{
		{OptionID: "allow", Kind: schema.PermissionOptionKindAllowOnce, Name: "Allow"},
		{OptionID: "reject", Kind: schema.PermissionOptionKindRejectOnce, Name: "Reject"},
	}
}

// elicitationParams 构造 form 模式 elicitation 的线上扁平格式（真适配器 wire 形态：
// mode/sessionId/message/requestedSchema 平铺）。经 agent.Client.Call 扩展通道直发——
// typed client.CreateElicitation 会被 acp-go 生成模型丢掉 requestedSchema（上游生成
// 缺口，见客户端 elicitation.go 补偿），fixture 必须走 raw 才能覆盖投影全保真链路。
func elicitationParams(sessionID schema.SessionId) map[string]any {
	return map[string]any{
		"mode":      "form",
		"sessionId": string(sessionID),
		"message":   "请选择一个选项",
		"requestedSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"choice": map[string]any{"type": "string", "title": "选项"},
			},
		},
	}
}

func strPtr(s string) *string { return &s }
