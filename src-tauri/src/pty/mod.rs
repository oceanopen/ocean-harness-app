// 嵌入式 PTY 模块（docs/worktree_term.md §7）。
// P1 桩（Module G）：PTY 真实现（portable-pty + PtyProvider trait + PtySessionStore + xterm 双向流）是后续阶段；
// 本期仅提供 pty_stop_for_worktree 桩命令，锁定「删 worktree 前先停 PTY」两阶段编排契约（§9.3），
// 让前端 D4 清理流程调用链一次写定，P2 接入真实现时无需改调用点。

/// 删除 worktree 前置：停掉该 worktree 绑定的全部 PTY，返回停止的会话数。
/// P1 桩：PTY 未实现，恒返回 0。P2 落地 portable-pty 后接入 PtySessionStore，按 worktreeId 批量 kill 绑定会话。
/// 返回 u32（非 usize）：specta 禁止导出 BigInt 类型（usize/i64 等）以避免前端精度损失（docs/worktree_term.md §7.3 的 usize 在此适配为 u32，语义不变）。
#[tauri::command]
#[specta::specta]
pub fn pty_stop_for_worktree(worktree_id: String) -> u32 {
    let _ = worktree_id; // P1 占位，避免 unused 警告；P2 接 PtySessionStore 后真正使用。
    0
}
