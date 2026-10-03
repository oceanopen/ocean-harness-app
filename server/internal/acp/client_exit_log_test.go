package acp

import (
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// waitExitLog 轮询等待「ACP agent 进程退出」日志条目（awaitProcessExit 在 proc.Done 后
// 才落日志，存在窗口期，不能在 Close/Kill 返回后立即断言）。
func waitExitLog(t *testing.T, logs *observer.ObservedLogs) observer.LoggedEntry {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		if entries := logs.FilterMessage("ACP agent 进程退出").All(); len(entries) > 0 {
			return entries[0]
		}
		select {
		case <-deadline:
			t.Fatal("退出日志未在时限内落盘")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// exitLogClosedByCaller 读退出日志的 closedByCaller 字段：(是否存在, 字段值)。
func exitLogClosedByCaller(entry observer.LoggedEntry) (present, value bool) {
	for _, field := range entry.Context {
		if field.Key != "closedByCaller" {
			continue
		}
		if field.Type == zapcore.BoolType {
			return true, field.Integer == 1
		}
		return true, false
	}
	return false, false
}

// 主动收尾（Close）：设计内死亡降为 DEBUG 并带 closedByCaller=true——doctor 探测回收、
// StopAll 等路径不打错误观感（与 Err() 的「主动收尾非异常」判别对齐）。
func TestExitLogCallerInitiated(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	client, _ := launchFakeLog(t, "happy", zap.New(core))
	client.Close()
	select {
	case <-client.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("进程未在时限内退出")
	}
	entry := waitExitLog(t, logs)
	if entry.Level != zapcore.DebugLevel {
		t.Fatalf("主动收尾应落 DEBUG，got %s", entry.Level)
	}
	if present, value := exitLogClosedByCaller(entry); !present || !value {
		t.Fatalf("主动收尾应带 closedByCaller=true 字段，got %+v", entry.Context)
	}
}

// 意外死亡（绕过 Close 直接整组强杀）：保持 WARN 且无 closedByCaller 字段——崩溃/被杀
// 仍是需要暴露的异常（与 TestErrAfterUnexpectedDeath 的 Err 断言互为日志面对照）。
func TestExitLogUnexpectedDeath(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	client, _ := launchFakeLog(t, "happy", zap.New(core))
	if err := client.proc.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	select {
	case <-client.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("进程未在时限内退出")
	}
	entry := waitExitLog(t, logs)
	if entry.Level != zapcore.WarnLevel {
		t.Fatalf("意外死亡应保持 WARN，got %s", entry.Level)
	}
	if present, _ := exitLogClosedByCaller(entry); present {
		t.Fatalf("意外死亡不应带 closedByCaller 字段，got %+v", entry.Context)
	}
}
