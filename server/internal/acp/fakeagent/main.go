// fakeagent 是 internal/acp 集成测试的进程内假 agent：经真实 stdio 跑 agent.New().Serve，
// 行为由 argv[1] 脚本驱动。TestMain 会将其编译为独立二进制供 acp.Launch 拉起。
//
// 脚本一览：
//
//	happy            两段 agent_message_chunk 后 end_turn
//	permission       prompt 中发起 request_permission，把结果回显进 message chunk
//	late-permission  prompt 立即 end_turn，300ms 后再发起 request_permission（无活动回合路径）
//	elicitation      prompt 中发起 elicitation/create（form 线上格式），把结果回显进 message chunk
//	slow-cancel      持续推送 chunk 直至收到 session/cancel，以 cancelled 终止
//	doctor-happy     session/new 响应后 200ms 推送 available_commands（T1.4 doctor 握手门槛）
package main

import (
	"context"
	"encoding/json"
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
		response, err := client.CreateElicitation(ctx, formElicitation(request.SessionID))
		if err != nil {
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

// formElicitation 构造 form 模式 elicitation 的线上格式。经 raw map 走扩展通道：
// 本 pin 的 typed ElicitationFormMode 不含 requestedSchema 字段（上游生成缺口），
// 但带 sessionId 的 form 载荷在客户端 typed 解码路径上可正常命中 Session scope。
func formElicitation(sessionID schema.SessionId) (request schema.CreateElicitationRequest) {
	raw, err := json.Marshal(map[string]any{
		"sessionId": string(sessionID),
		"message":   "请选择一个选项",
		"mode":      "form",
		"form": map[string]any{
			"sessionId": string(sessionID),
			"requestedSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"choice": map[string]any{"type": "string", "title": "选项"},
				},
				"required": []string{"choice"},
			},
		},
	})
	if err != nil {
		return request
	}
	_ = json.Unmarshal(raw, &request)
	return request
}

func strPtr(s string) *string { return &s }
