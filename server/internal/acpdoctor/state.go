package acpdoctor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"ocean-harness/server/internal/agentcatalog"
)

// 进程内结论缓存与受理在跑集合（包级单例，对齐 agentcatalog/clibin 的包级状态惯例）：
// 生命周期随 sidecar——重启即回到 unknown，由启动后台探测（RunStartupProbe）重新填满。
// checking 不是存储态：由 inflight 集合在 Snapshot 推导，缓存里只存结论（Report）。
var (
	stateMu  sync.RWMutex
	reports  = map[string]Report{}    // agentCode → 最近结论
	inflight = map[string]*checkRun{} // agentCode → 在跑探测（受理单飞）
)

// probeFunc 探测执行函数（测试替换点：编排与真实探测解耦）。
var probeFunc = CheckEntry

// checkRun 一次探测的终结信号：done 关闭前结论必已写缓存（schedule 的收尾顺序保证），
// 等待方醒来读到的即是本次结果。
type checkRun struct {
	done chan struct{}
}

// EntryState 单条目四态视图（HTTP 投影形态）：status ∈ healthy/unhealthy/unknown/checking，
// checking 时 CheckedAt 等字段为上一次结论（探测中不产生新结论）。
type EntryState struct {
	AgentCode    string
	Status       Status
	CheckedAt    time.Time
	Stage        Stage
	Reason       string
	CommandCount int
	NodeVersion  string
}

// RequestCheck 受理探测：agentCode 为空 = 全部 enabled 条目，逐条受理——在跑的 join
// （不并发重跑），空闲的立即起后台 goroutine。立即返回受理的 agentCode 列表，不等待
// 探测完成（前端以 getInfo 轮询四态）。agentCode 指定但 catalog 无此条目时报错。
func RequestCheck(agentCode string, dirs Dirs, log *zap.Logger) ([]string, error) {
	entries, err := resolveEntries(agentCode)
	if err != nil {
		return nil, err
	}
	accepted := make([]string, 0, len(entries))
	for _, entry := range entries {
		schedule(entry, dirs, log)
		accepted = append(accepted, entry.Code)
	}
	return accepted, nil
}

// RunStartupProbe 启动后台全量探测（非阻塞）：串行逐条目跑完再受理下一条——避免并发
// spawn 多个 node 子进程。由 main 启动期装配；与手动受理共用同一 schedule（单飞互斥）。
func RunStartupProbe(dirs Dirs, log *zap.Logger) {
	go func() {
		for _, entry := range agentcatalog.Enabled() {
			<-schedule(entry, dirs, log)
		}
	}()
}

// Snapshot 返回全部 enabled 条目的四态视图（catalog 顺序）。checking 由 inflight 推导：
// 在跑即 checking，此前有旧结论则结论字段原样带出（前端按 status 分支消费）。
func Snapshot() []EntryState {
	entries := agentcatalog.Enabled()
	states := make([]EntryState, 0, len(entries))
	stateMu.RLock()
	defer stateMu.RUnlock()
	for _, entry := range entries {
		state := EntryState{AgentCode: entry.Code, Status: StatusUnknown}
		if report, ok := reports[entry.Code]; ok {
			state.AgentCode = report.AgentCode
			state.Status = report.Status
			state.CheckedAt = report.CheckedAt
			state.Stage = report.Stage
			state.Reason = report.Reason
			state.CommandCount = report.CommandCount
			state.NodeVersion = report.NodeVersion
		}
		// 在跑压过一切：checking 派生态最终生效（启动探测填满缓存后的手动重检是常规
		// 路径，若被旧结论覆盖，前端轮询条件 checking/unknown 永不命中，新结论不可达）。
		if _, running := inflight[entry.Code]; running {
			state.Status = StatusChecking
		}
		states = append(states, state)
	}
	return states
}

// schedule 受理单条探测：已在跑返回其 done；否则登记 inflight 并起后台 goroutine。
// 收尾顺序固定为「写缓存 → 摘除 inflight → 关 done」，等待方醒来即见本次结论。
func schedule(entry agentcatalog.Entry, dirs Dirs, log *zap.Logger) <-chan struct{} {
	stateMu.Lock()
	if run, ok := inflight[entry.Code]; ok {
		stateMu.Unlock()
		return run.done
	}
	run := &checkRun{done: make(chan struct{})}
	inflight[entry.Code] = run
	stateMu.Unlock()

	go func() {
		// 探测不横跨调用方 ctx（异步受理模型下后台走完全程），取消语义由
		// CheckEntry 内的总预算（probeBudget）驱动。
		report := probeFunc(context.Background(), entry, dirs, log)
		stateMu.Lock()
		reports[entry.Code] = report
		delete(inflight, entry.Code)
		stateMu.Unlock()
		close(run.done)
	}()
	return run.done
}

// resolveEntries 解析受理范围：空 agentCode = 全部 enabled 条目；指定 code 走 catalog
// 查表（缺失报错，宁失败不静默）。
func resolveEntries(agentCode string) ([]agentcatalog.Entry, error) {
	if agentCode == "" {
		return agentcatalog.Enabled(), nil
	}
	entry, ok := agentcatalog.GetAgentCatalogInfoByCode(agentCode)
	if !ok {
		return nil, fmt.Errorf("catalog 无 agentCode %q 条目", agentCode)
	}
	return []agentcatalog.Entry{entry}, nil
}
