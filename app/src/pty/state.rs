// PtySessionStore：嵌入式终端会话存储（照 ClaudeSessionStore 范式）。
//
// key 为会话锚点 `issueId::<paneId>`（main → `issueId::main`，一 issue 多 pane）。
// webview 刷新 Rust 不死，store 常驻实现抗刷新；app 退出时由 lib.rs
// RunEvent::Exit → pty::shutdown_all_provider 回收。
//
// 唯一实例由 LocalPtyProvider 自持（spawn 写入侧），不再 app.manage——曾因
// manage 出第二实例致 State<PtySessionStore> 查询恒空（probe 恒 false 的历史
// bug 成因），幽灵 manage 已随退出清理改走真源一并删除。

use std::collections::HashMap;
use std::sync::Mutex;

use super::session::PtySession;

/// 会话存储。锁 poison 走 expect panic 兜底（与 ClaudeSessionStore 风格一致）：
/// 会话操作均为短临界区（insert/remove/读写句柄），poison 只在持锁线程 panic 时发生，
/// 此时进程状态已不可信，快速失败优于静默死锁。
#[derive(Default)]
pub struct PtySessionStore(pub Mutex<HashMap<String, PtySession>>);
