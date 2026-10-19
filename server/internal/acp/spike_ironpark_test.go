package acp_test

/*
 * ironpark spike 集成验收(T2):ironpark acp1 直连 vendored claude-agent-acp 的最小链路
 * 与扩展兼容验证。独立于生产代码,不触碰 internal/acp 任何实现。
 */

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ironpark/acp-go/acp1"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/agentcatalog"
	"ocean-harness/server/internal/dal/enums"
)

/*
 * TestIronparkSpike ironpark acp1 直连 vendored adapter 的 spike 验收,与 TestRealHandshake
 * 同门禁同装配链路。三项断言:claudeCode 扩展(实际在 agentCapabilities._meta)、modes 双轨
 * 形状、elicitation 全字段;_meta 透传以 systemPrompt 注名闭环(初始模式只来自文件级 settings)。
 */
func TestIronparkSpike(t *testing.T) {
	if os.Getenv("OCEAN_ACP_REAL_HANDSHAKE") != "1" {
		t.Skip("ironpark spike 验收:需要 OCEAN_ACP_REAL_HANDSHAKE=1 与本机 claude 凭据")
	}
	entry, ok := agentcatalog.GetAgentCatalogInfoByCode(string(enums.AGENT_CODE_CLAUDE_ACP))
	if !ok {
		t.Fatal("catalog 缺映射条目 claude-acp")
	}
	srcRoot := os.Getenv("OCEAN_ACP_RESOURCES_DIR")
	if srcRoot == "" {
		srcRoot = filepath.Join("..", "..", "..", "app", "resources", "acp-adapters")
		if _, err := os.Stat(srcRoot); err != nil {
			t.Fatalf("vendored 资源源缺失(先跑 pnpm server:vendor:prepare): %v", err)
		}
	}
	dstRoot := os.Getenv("OCEAN_ACP_ADAPTERS_DIR")
	if dstRoot == "" {
		dstRoot = t.TempDir()
	}
	installDir, err := agentcatalog.EnsureVendored(entry, srcRoot, dstRoot)
	if err != nil {
		t.Fatalf("vendored 受管安装: %v", err)
	}
	claudeBin, err := agentcatalog.VendoredClaudeBin(installDir)
	if err != nil {
		t.Fatalf("vendored 自带 claude 解析: %v", err)
	}
	env, err := acp.ClaudeEnvOverrides(entry.Env, claudeBin)
	if err != nil {
		t.Fatalf("claude 路径一致性注入: %v", err)
	}
	cfg, err := entry.VendoredSpawnConfig(srcRoot, installDir, t.TempDir(), env)
	if err != nil {
		t.Fatalf("vendored 入口翻译: %v", err)
	}
	if custom := os.Getenv("OCEAN_ACP_REAL_COMMAND"); custom != "" {
		cfg.Command = strings.Fields(custom)
	}

	stderr := &syncBuffer{}
	cmd := exec.Command(cfg.Command[0], cfg.Command[1:]...)
	cmd.Dir = cfg.Cwd
	cmd.Env = acp.MergeEnv(cfg.Env)
	cmd.Stderr = stderr
	client := &spikeClient{}
	agentProc, err := acp1.SpawnAgent(context.Background(), cmd, func(*acp1.ClientSideConnection) acp1.Client { return client })
	if err != nil {
		t.Fatalf("SpawnAgent: %v\nstderr: %s", err, stderr.tail())
	}
	defer agentProc.Close()

	caps := acp1.ClientCapabilitiesOf(client)
	caps.Elicitation.Form = &acp1.ElicitationFormCapabilities{}
	caps.Session = &acp1.ClientSessionCapabilities{
		ConfigOptions: &acp1.SessionConfigOptionsCapabilities{Boolean: &acp1.BooleanConfigOptionCapabilities{}},
	}
	ctx := context.Background()
	init, err := agentProc.Initialize(ctx, &acp1.InitializeRequest{
		ClientCapabilities: caps,
		ClientInfo:         &acp1.Implementation{Name: "ocean-harness-spike", Version: "0.0.1"},
	})
	if err != nil {
		t.Fatalf("Initialize: %v\nstderr: %s", err, stderr.tail())
	}
	t.Logf("agent: %s %s 协议版本: %d", init.AgentInfo.Name, init.AgentInfo.Version, init.ProtocolVersion)
	if init.AgentCapabilities == nil {
		t.Fatalf("initialize 应答缺 agentCapabilities\nstderr: %s", stderr.tail())
	}
	claudeCode, ok, err := init.AgentCapabilities.Meta.Get[map[string]any]("claudeCode")
	if err != nil {
		t.Fatalf("claudeCode 扩展解码: %v", err)
	}
	if !ok {
		t.Fatalf("agentCapabilities._meta 缺 claudeCode 扩展标记\nstderr: %s", stderr.tail())
	}
	t.Logf("claudeCode 扩展: %v", claudeCode)

	newMeta := acp1.Meta{}
	if err := newMeta.Set("systemPrompt", "Your name is "+spikeSystemToken+". When asked for your name, reply with exactly that token and nothing else."); err != nil {
		t.Fatalf("_meta 构造: %v", err)
	}
	created, err := agentProc.NewSession(ctx, &acp1.NewSessionRequest{
		Cwd:        cfg.Cwd,
		MCPServers: []acp1.MCPServer{},
		Meta:       newMeta,
	})
	if err != nil {
		t.Fatalf("NewSession: %v\nstderr: %s", err, stderr.tail())
	}
	if created.Modes == nil {
		t.Fatalf("session/new 应答缺 modes\nstderr: %s", stderr.tail())
	}
	available := map[string]string{}
	for _, mode := range created.Modes.AvailableModes {
		available[string(mode.ID)] = mode.Name
	}
	t.Logf("modes: current=%s available=%v", created.Modes.CurrentModeID, available)
	for _, want := range []string{"default", "acceptEdits", "plan", "auto"} {
		if _, ok := available[want]; !ok {
			t.Fatalf("availableModes 缺基本模式 %q(实际: %v)\nstderr: %s", want, available, stderr.tail())
		}
	}
	if _, ok := available[string(created.Modes.CurrentModeID)]; !ok {
		t.Fatalf("currentModeId %q 不在 availableModes 内(实际: %v)", created.Modes.CurrentModeID, available)
	}

	session := agentProc.Session(created.SessionID)
	turn1, err := session.Prompt(context.Background(), acp1.NewContentBlock(acp1.ContentBlockText{Text: "What is your name? Reply with only your name."}))
	if err != nil {
		t.Fatalf("Prompt(轮1): %v\nstderr: %s", err, stderr.tail())
	}
	resp1 := waitSpikeTurn(t, turn1, stderr)
	if resp1.StopReason != acp1.StopReasonEndTurn {
		t.Fatalf("轮1 stopReason = %q, want end_turn\nstderr: %s", resp1.StopReason, stderr.tail())
	}
	text1 := client.takeText()
	if !strings.Contains(text1, spikeSystemToken) {
		t.Fatalf("_meta.systemPrompt 透传闭环失败: 轮1 回复不含注入 token(回复: %q)\nstderr: %s", text1, stderr.tail())
	}

	client.resetText()
	turn2, err := session.Prompt(context.Background(), acp1.NewContentBlock(acp1.ContentBlockText{Text: "Use the AskUserQuestion tool to ask me: 'Which database should we use?' with options 'SQLite' (lightweight, file-based) and 'PostgreSQL' (production-grade). Do not proceed until I answer."}))
	if err != nil {
		t.Fatalf("Prompt(轮2): %v\nstderr: %s", err, stderr.tail())
	}
	resp2 := waitSpikeTurn(t, turn2, stderr)
	if resp2.StopReason != acp1.StopReasonEndTurn {
		t.Fatalf("轮2 stopReason = %q, want end_turn\nstderr: %s", resp2.StopReason, stderr.tail())
	}
	if strings.TrimSpace(client.takeText()) == "" {
		t.Fatalf("轮2 accept 应答后未收到任何文本 chunk\nstderr: %s", stderr.tail())
	}
	record, decodeErr := client.elicitationResult()
	if decodeErr != nil {
		t.Fatalf("elicitation 请求解码失败(非 FormSession 变体?): %v\nstderr: %s", decodeErr, stderr.tail())
	}
	if record == nil {
		t.Fatalf("轮2 未触发 elicitation/create\nstderr: %s", stderr.tail())
	}
	if record.sessionID != created.SessionID {
		t.Fatalf("elicitation sessionId = %q, want %q", record.sessionID, created.SessionID)
	}
	if record.toolCallID == "" {
		t.Fatal("elicitation 缺 toolCallId(typed 字段被丢?)")
	}
	if !client.sawToolCall(record.toolCallID) {
		t.Fatalf("elicitation toolCallId %q 未在先前 tool_call update 中出现", record.toolCallID)
	}
	if record.schemaType != string(acp1.ElicitationSchemaTypeObject) {
		t.Fatalf("elicitation requestedSchema.type = %q, want object", record.schemaType)
	}
	if !record.hasQuestion0 {
		t.Fatalf("elicitation requestedSchema.properties 缺 question_0(实际: %v)", record.schemaProperties)
	}
	t.Logf("elicitation: sessionId=%s toolCallId=%s message=%q schemaProperties=%v", record.sessionID, record.toolCallID, record.message, record.schemaProperties)
}

/*
 * waitSpikeTurn 阻塞等回合收敛,180s 未收敛按失败收口(prompt/elicitation 均不限时,
 * 死等只会挂死测试进程)。
 */
func waitSpikeTurn(t *testing.T, turn *acp1.Turn, stderr *syncBuffer) *acp1.PromptResponse {
	t.Helper()
	type waitResult struct {
		resp *acp1.PromptResponse
		err  error
	}
	results := make(chan waitResult, 1)
	go func() {
		resp, err := turn.Wait()
		results <- waitResult{resp: resp, err: err}
	}()
	select {
	case result := <-results:
		if result.err != nil {
			t.Fatalf("Turn.Wait: %v\nstderr: %s", result.err, stderr.tail())
		}
		return result.resp
	case <-time.After(180 * time.Second):
		t.Fatalf("回合 180s 未收敛\nstderr: %s", stderr.tail())
	}
	return nil
}

/*
 * spikeClient ironpark 客户端桩:累积文本 chunk 与 tool_call id,elicitation 按表单
 * 变体解码记录后固定 accept,权限请求按取消收敛(凭据异常时不挂死)。
 */
type spikeClient struct {
	acp1.UnimplementedClient

	mu             sync.Mutex
	turnText       strings.Builder
	toolCalls      map[acp1.ToolCallID]bool
	elicitation    *spikeElicitation
	elicitationErr error
}

// spikeElicitation FormSession 变体解码出的 typed 字段记录(断言素材)。
type spikeElicitation struct {
	sessionID        acp1.SessionID
	toolCallID       acp1.ToolCallID
	message          string
	schemaType       string
	schemaProperties []string
	hasQuestion0     bool
}

func (c *spikeClient) SessionUpdate(_ context.Context, params *acp1.SessionNotification) error {
	switch v := params.Update.Variant().(type) {
	case acp1.SessionUpdateAgentMessageChunk:
		if text, ok := acp1.TextOf(v.Content); ok && strings.TrimSpace(text) != "" {
			c.mu.Lock()
			c.turnText.WriteString(text)
			c.mu.Unlock()
		}
	case acp1.SessionUpdateToolCall:
		c.mu.Lock()
		if c.toolCalls == nil {
			c.toolCalls = map[acp1.ToolCallID]bool{}
		}
		c.toolCalls[v.ToolCallID] = true
		c.mu.Unlock()
	}
	return nil
}

func (c *spikeClient) RequestPermission(_ context.Context, _ *acp1.RequestPermissionRequest) (*acp1.RequestPermissionResponse, error) {
	return acp1.PermissionCancelled(), nil
}

func (c *spikeClient) CreateElicitation(_ context.Context, params *acp1.CreateElicitationRequest) (*acp1.CreateElicitationResponse, error) {
	form, err := params.As[acp1.CreateElicitationRequestFormSession]()
	if err != nil {
		c.mu.Lock()
		c.elicitationErr = fmt.Errorf("FormSession 变体解码: %w", err)
		c.mu.Unlock()
		resp := acp1.NewCreateElicitationResponse(acp1.CreateElicitationResponseDecline{})
		return &resp, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	record := &spikeElicitation{
		sessionID: form.SessionID,
		message:   form.Message,
	}
	if form.ToolCallID != nil {
		record.toolCallID = *form.ToolCallID
	}
	if form.RequestedSchema.Type != nil {
		record.schemaType = string(*form.RequestedSchema.Type)
	}
	for name := range form.RequestedSchema.Properties {
		record.schemaProperties = append(record.schemaProperties, name)
		if name == "question_0" {
			record.hasQuestion0 = true
		}
	}
	c.elicitation = record
	answer, err := acp1.NewElicitationContentValue("PostgreSQL")
	if err != nil {
		return nil, err
	}
	resp := acp1.NewCreateElicitationResponse(acp1.CreateElicitationResponseAccept{
		Content: map[string]acp1.ElicitationContentValue{"question_0": answer},
	})
	return &resp, nil
}

func (c *spikeClient) CompleteElicitation(_ context.Context, _ *acp1.CompleteElicitationNotification) error {
	return nil
}

func (c *spikeClient) resetText() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.turnText.Reset()
}

func (c *spikeClient) takeText() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turnText.String()
}

func (c *spikeClient) sawToolCall(id acp1.ToolCallID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.toolCalls[id]
}

func (c *spikeClient) elicitationResult() (*spikeElicitation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.elicitation, c.elicitationErr
}

// spikeSystemToken _meta.systemPrompt 注入的名字 token(轮1 问名验证透传闭环)。
const spikeSystemToken = "OCEAN-SPIKE-7F3A"

// stderrSummaryRunes 失败摘要保留 rune 数(driver_claude 300 先例同量级)。
const stderrSummaryRunes = 300

/*
 * syncBuffer 并发安全的 stderr 捕获(exec 写入与测试读取跨 goroutine)。
 */
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// tail stderr 尾部摘要(超长截断,失败诊断用)。
func (b *syncBuffer) tail() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	text := b.buf.String()
	runes := []rune(text)
	if len(runes) > stderrSummaryRunes {
		return "..." + string(runes[len(runes)-stderrSummaryRunes:])
	}
	return text
}
