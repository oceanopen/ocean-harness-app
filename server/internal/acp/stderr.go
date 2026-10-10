package acp

import (
	"strings"
	"sync"
)

// stderr 行三分类（Gold-Band 行为语义，仅行为参照）：噪音计数不逐行打日志、失败信号
// Warn 级、其余诊断 Debug 级。tail 始终接收全部 stderr（崩溃栈未必带 npm 前缀，
// 诊断完整性优先），分类只决定日志级别。
type stderrKind int

const (
	stderrNoise      stderrKind = iota // 已知噪音：npm 旧配置键告警等，不计入失败证据
	stderrFailure                      // 失败信号：npm error / ENOENT 等，Warn + 视为启动失败证据
	stderrDiagnostic                   // 其余诊断输出：Debug 级
)

// stderrFailureMarkers 命中即归失败信号的子串（统一小写匹配；errno 码在
// node 输出中为大写，故大小写不敏感以免枚举两种形态）。
var stderrFailureMarkers = []string{
	"npm error", "npm err!",
	"enospc", "enoent", "eacces", "eperm",
}

// stderrNoiseMarkers 命中即归噪音的子串。
var stderrNoiseMarkers = []string{
	"unknown user config", // npm 对旧配置键的固定告警，无诊断价值
	"[session/query]",     // agent 自身查询日志前缀（Gold-Band 同款噪音源）
}

// classifyStderr 单行三分类。
func classifyStderr(line string) stderrKind {
	lower := strings.ToLower(line)
	for _, marker := range stderrNoiseMarkers {
		if strings.Contains(lower, marker) {
			return stderrNoise
		}
	}
	for _, marker := range stderrFailureMarkers {
		if strings.Contains(lower, marker) {
			return stderrFailure
		}
	}
	return stderrDiagnostic
}

// truncateRunes 尾部保留 max 个 rune（超长行截断后仍继续读，不中断 stderr 泵）。
func truncateRunes(s string, max int) string {
	if rs := []rune(s); len(rs) > max {
		return "…" + string(rs[len(rs)-max:])
	}
	return s
}

// StderrTail 有界字节环：只保留尾部 cap 字节，并发安全；Text 输出合法 UTF-8
// （环裁剪落在多字节字符中间时，残缺前缀字节经 ToValidUTF8 替换为 U+FFFD）。
// 导出供 browser 域引擎子进程复用（配合 PumpStderr，单一 SSOT）。
type StderrTail struct {
	mu  sync.Mutex
	buf []byte
	cap int
}

// NewStderrTail 构造指定容量的 stderr tail（容量 = 尾部保留字节数），配合 PumpStderr
// 使用（导出供 browser 域引擎子进程复用）。
func NewStderrTail(capacity int) *StderrTail {
	return &StderrTail{cap: capacity}
}

func (t *StderrTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(p) >= t.cap {
		t.buf = append(t.buf[:0], p[len(p)-t.cap:]...)
		return len(p), nil
	}
	if excess := len(t.buf) + len(p) - t.cap; excess > 0 {
		t.buf = t.buf[excess:]
	}
	t.buf = append(t.buf, p...)
	return len(p), nil
}

// Text 返回 tail 内容（UTF-8 合法化）。
func (t *StderrTail) Text() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.ToValidUTF8(string(t.buf), "�")
}
