package browser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

/*
 * 全局共享登录态同步编排（T7.2，D3 修订：凭据仅 auth-states/default.json 一份落盘）：
 * spawn 置 ready 前锁外注入（首导航前结构保证）；运行期周期导出 + 收尾（Close/无头切换）
 * 导出，合并并集语义回写——任一会话登录后全部实例共享。全部 best-effort——失败仅告警
 * 降级，登录态退回 profile 目录自管。引擎文件沙箱只放行 downloads/<profile>/ 与 cwd，
 * 共享文件路径不可直传，staging 一律中转（见 tools.go EngineToolStorageState 注释）。
 */

// 时序与预算常量（var 化供测试覆写；编码规则 1——时序缺陷不引入防抖掩盖）。
var (
	// authInjectTimeout spawn 注入预算（含引擎清空恢复 + 逐 origin localStorage 恢复）。
	authInjectTimeout = 20 * time.Second
	// authExportTimeout 单次导出回写预算（收尾路径短预算不拖累 Close）。
	authExportTimeout = 8 * time.Second
	// authSyncInterval 运行期周期导出间隔。
	authSyncInterval = 60 * time.Second
)

// stateEmpty 空态判定（cookies 与 origins 双空，空态不注入——免无谓清空会话）。
func stateEmpty(s StorageState) bool {
	return len(s.Cookies) == 0 && len(s.Origins) == 0
}

// stateHash 内容指纹（canonical 序列化后 sha256，与文件原始格式化无关）。
func stateHash(s StorageState) string {
	data, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

/*
 * callEngineDirect 引擎工具直调（不经 Forward：Close 期 Forward 会重拉引擎、就绪期会
 * 重置 idle 计时；对齐 capturePagesCtx 直调先例）。isError 结果归一错误返回。
 */
func callEngineDirect(ctx context.Context, handle *EngineHandle, tool string, args map[string]any) error {
	res, err := handle.Session().CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return err
	}
	if res.IsError {
		return fmt.Errorf("引擎拒绝: %s", ResultText(res))
	}
	return nil
}

// stagingInjectFile 注入 staging 文件名（固定名：同 profile 单引擎串行，无竞争）。
const stagingInjectFile = ".auth-state-inject.json"

/*
 * injectAuthState spawn 置 ready 前注入全局共享登录态：共享文件缺失/空态/与上次注入同
 * 内容（hash 相同）均跳过；注入 = staging 中转 set_storage_state（引擎先清空再恢复）。失败
 * 仅告警——会话照常就绪，登录态退回 profile 自管（用户重登后导出链路自愈回写）。
 */
func (m *Manager) injectAuthState(entry *sessionEntry, profile string, handle *EngineHandle) {
	path := AuthStatePath()
	shared, exists, err := readSharedState(path)
	if err != nil {
		m.log.Warn("共享登录态读取失败（跳过注入）", zap.String("profile", profile), zap.Error(err))
		return
	}
	if !exists || stateEmpty(shared) {
		return
	}
	sum := stateHash(shared)
	if sum == "" {
		return
	}
	if sum == entry.authInjectedHash {
		return // 重开遇同内容共享文件：跳过无谓清空重注
	}

	dlDir, err := DownloadsDir(profile)
	if err != nil {
		m.log.Warn("下载目录不可用（跳过注入）", zap.String("profile", profile), zap.Error(err))
		return
	}
	staging := filepath.Join(dlDir, stagingInjectFile)
	data, err := json.MarshalIndent(shared, "", "  ")
	if err != nil {
		m.log.Warn("共享登录态序列化失败（跳过注入）", zap.String("profile", profile), zap.Error(err))
		return
	}
	if err := os.WriteFile(staging, data, 0o600); err != nil {
		m.log.Warn("注入 staging 写入失败（跳过注入）", zap.String("profile", profile), zap.Error(err))
		return
	}
	defer os.Remove(staging)

	ctx, cancel := context.WithTimeout(context.Background(), authInjectTimeout)
	defer cancel()
	if err := callEngineDirect(ctx, handle, EngineToolSetStorageState, map[string]any{"filename": staging}); err != nil {
		m.log.Warn("共享登录态注入失败（会话照常就绪）", zap.String("profile", profile), zap.Error(err))
		return
	}
	m.authInjectedHashStore(profile, sum)
	m.log.Info("已注入全局共享登录态", zap.String("profile", profile))
}

/*
 * exportAuthState 引擎当前登录态导出回写全局共享文件（staging 中转 → 与共享文件并集
 * 合并 → 共享文件锁内原子写；读侧损坏按空态自愈重建）。best-effort，失败仅告警。
 */
func (m *Manager) exportAuthState(profile string, handle *EngineHandle) {
	dlDir, err := DownloadsDir(profile)
	if err != nil {
		m.log.Warn("下载目录不可用（跳过导出）", zap.String("profile", profile), zap.Error(err))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), authExportTimeout)
	defer cancel()
	tmp, err := os.CreateTemp(dlDir, ".auth-state-export-*.json")
	if err != nil {
		m.log.Warn("导出 staging 创建失败（跳过导出）", zap.String("profile", profile), zap.Error(err))
		return
	}
	staging := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(staging)

	if err := callEngineDirect(ctx, handle, EngineToolStorageState, map[string]any{"filename": staging}); err != nil {
		m.log.Warn("登录态导出被引擎拒绝", zap.String("profile", profile), zap.Error(err))
		return
	}
	data, err := os.ReadFile(staging)
	if err != nil {
		m.log.Warn("导出 staging 读取失败（跳过回写）", zap.String("profile", profile), zap.Error(err))
		return
	}
	var engine StorageState
	if err := json.Unmarshal(data, &engine); err != nil {
		m.log.Warn("导出 staging 解析失败（跳过回写）", zap.String("profile", profile), zap.Error(err))
		return
	}

	path := AuthStatePath()
	withSharedFile(func() {
		shared, _, rerr := readSharedState(path)
		if rerr != nil {
			m.log.Warn("共享登录态读取失败（按空态重建）", zap.String("profile", profile), zap.Error(rerr))
		}
		merged := mergeStorageState(shared, engine, time.Now())
		if err := writeSharedStateAtomic(path, merged); err != nil {
			m.log.Warn("共享登录态回写失败", zap.String("profile", profile), zap.Error(err))
		}
	})
}

/*
 * runAuthSync ready 期周期导出：handle 生命周期绑定（Exited 即退，Close/无头切换/意外
 * 死亡三路径自然收敛，无独立注册表）；每轮先复查 entry 仍持有该 handle（防释放后误调
 * 新引擎），callMu 空闲才导出（忙即跳过本轮，不与在途导航竞争）。
 */
func (m *Manager) runAuthSync(entry *sessionEntry, profile string, handle *EngineHandle) {
	ticker := time.NewTicker(authSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-handle.Exited():
			return
		case <-ticker.C:
		}
		entry.mu.Lock()
		active := entry.handle == handle
		entry.mu.Unlock()
		if !active {
			return
		}
		if !entry.callMu.TryLock() {
			m.log.Debug("登录态周期导出跳过（会话忙）", zap.String("profile", profile))
			continue
		}
		m.exportAuthState(profile, handle)
		entry.callMu.Unlock()
	}
}

// authInjectedHashStore 记录已注入内容指纹（spawn 路径单写者；跨 idle 重建留存）。
func (m *Manager) authInjectedHashStore(profile, sum string) {
	entry := m.entryOf(profile)
	entry.mu.Lock()
	entry.authInjectedHash = sum
	entry.mu.Unlock()
}
