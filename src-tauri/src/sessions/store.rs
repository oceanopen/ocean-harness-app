// 全量重扫：discover → enrich → git 判定（GitPending）→ 写 ClaudeSessionStore → emit。
//
// 保证 Dead 会话立即从列表消失。emit 失败 / write 失败不阻塞主流程——
// 下一轮 watcher / poller 会重新执行，对齐现有容错风格。
//
// GitPending 的"空闲判断 + 缓存复用"策略（见 finalize_status）：
//   - 仅 base status == Idle 的会话才跑 git（Busy/Waiting 不跑）。
//   - fs watcher 高频触发（force_git=false）时，已是 idle 基线的会话复用上次缓存的
//     status，避免每次 fs 抖动都跑 git。GitPending 有界过期——poll(60s)/手动刷新
//     (force_git=true) 强制重算，最长延迟 60s。
//
// 显隐联动不再在此触发：pet 前端收到 claude-sessions:changed payload 后基于 count
// 自行调 show_pet_claude_sessions_task_window / hide_pet_claude_sessions_task_window，后端不做自动联动。

use std::collections::HashMap;

use tauri::{AppHandle, Emitter, Manager};

use crate::sessions::{discover, enrich, git};
use crate::shared::events::EVENT_CLAUDE_SESSIONS_CHANGED;
use crate::shared::state::claude_sessions::{
    ClaudeSessionStore, RescanLock, write_claude_sessions,
};
use crate::shared::types::{ClaudeSessionInfo, ClaudeSessionStatus, TerminalApp};

/// 全量重扫会话目录并刷新前端。
///
/// `force_git` 控制 GitPending 判定策略：
///   - false（fs watcher，高频）：Idle 会话若上次也是 Idle/GitPending，复用缓存的 status，
///     避免每次 fs 抖动都跑 git。
///   - true（poll / 手动刷新 / 启动）：所有 Idle 会话强制重跑 git，保证徽章新鲜。
///
/// 调用点：
///   - lib.rs setup 末尾（force_git=true，启动时初始化）
///   - sessions::watch 收到 fs 事件后（force_git=false）
///   - sessions::poll 周期触发（force_git=true）
///   - windows::panel::refresh_sessions 手动刷新（force_git=true）
///
/// 并发：用 RescanLock 互斥，避免 watcher/poll/命令三线程同时跑 git 串行堆积。
/// 仅输出数量日志：调用方高频触发，详情日志会刷屏。
pub fn rescan(app: &AppHandle, force_git: bool) {
    // 临界区：读旧快照 → 跑 git → 写回，整体串行化。
    // 持锁期间最坏约 N×100ms（N 个 idle 会话各跑一次 git），watcher 1s 去抖可吸收。
    // 先绑定 rescan_lock 再 lock：State 需存活到函数末以维持 MutexGuard 借用
    //（临时量在语句末释放会让 guard 悬垂，E0716）。
    // state() 而非 try_state()：lib.rs setup 必先 manage RescanLock 再启动 watcher/poll。
    let rescan_lock = app.state::<RescanLock>();
    let _guard = rescan_lock
        .0
        .lock()
        .expect("RescanLock mutex poisoned");

    let raws = discover::list_active();
    // 过滤 Unknown 宿主：终端被关闭后孤立的 claude 进程 parent chain 爬到 launchd
    // 也匹配不到已知终端，跳转按钮本来就是禁用的，留在列表里只会误导用户。
    let enriched: Vec<ClaudeSessionInfo> = raws
        .iter()
        .map(enrich::enrich)
        .filter(|s| s.host_app != TerminalApp::Unknown)
        .collect();

    // 读旧快照：lock 后 clone 出来立即释放，git 跑期间不持 store 锁。
    let prev_map: HashMap<u32, ClaudeSessionInfo> = {
        let store = app.state::<ClaudeSessionStore>();
        let map = store
            .0
            .lock()
            .expect("ClaudeSessionStore mutex poisoned");
        map.values()
            .cloned()
            .map(|s| (s.pid, s))
            .collect()
    };

    // 对每个 enriched session 计算最终 status（GitPending transition + 缓存复用）。
    let sessions: Vec<ClaudeSessionInfo> = enriched
        .into_iter()
        .map(|mut s| {
            s.status = finalize_status(&s, prev_map.get(&s.pid), force_git);
            s
        })
        .collect();

    let count = sessions.len();
    // write_claude_sessions 是替换式 move 写入，emit 需要先 clone 一份快照复用。
    let snapshot = sessions.clone();
    let store = app.state::<ClaudeSessionStore>();
    write_claude_sessions(&store, sessions);

    if let Err(e) = app.emit(EVENT_CLAUDE_SESSIONS_CHANGED, &snapshot) {
        log::warn!(
            "[sessions] emit claude-sessions:changed failed: {}",
            e
        );
    }

    log::info!(
        "[sessions] rescan: {} session(s), force_git={}",
        count,
        force_git
    );
}

/// 根据本次 enrich 的 base status、上次快照 prev、force_git 决定最终 status。
///
/// 规则：
///   - base != Idle → 原样返回（Busy/Waiting，git 无关；Dead 已被 discover 过滤不会进入）。
///   - base == Idle：
///     - prev 不存在（新会话）/ prev 非 idle 基线（刚从 Busy/Waiting 转入）/ force_git
///       → 跑 git，dirty 则 GitPending 否则 Idle。
///     - prev 是 idle 基线（Idle 或 GitPending）且 !force_git → 复用 prev.status（不跑 git）。
///
/// 抽成独立纯函数便于单元测试（参数为值/引用，不依赖 AppHandle）。
fn finalize_status(
    cur: &ClaudeSessionInfo,
    prev: Option<&ClaudeSessionInfo>,
    force_git: bool,
) -> ClaudeSessionStatus {
    use ClaudeSessionStatus::*;

    if cur.status != Idle {
        return cur.status;
    }

    // prev 曾处于 idle 基线（Idle 或 GitPending 都代表"base 是空闲"）才有缓存可复用。
    let was_idle_base = matches!(
        prev.map(|p| p.status),
        Some(Idle) | Some(GitPending)
    );

    let need_git = force_git || prev.is_none() || !was_idle_base;

    if need_git {
        if git::is_dirty(&cur.cwd) {
            GitPending
        } else {
            Idle
        }
    } else {
        // need_git == false ⟹ !force_git && prev.is_some() && was_idle_base，
        // 故 prev 必 Some，且其 status 为 Idle 或 GitPending。
        prev.expect("need_git=false requires prev=Some")
            .status
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::shared::types::{ClaudeSessionInfo, ClaudeSessionStatus::*, TerminalApp};

    /// 构造测试用 ClaudeSessionInfo，仅填 status 相关字段。
    fn mock_info(pid: u32, status: ClaudeSessionStatus, cwd: &str) -> ClaudeSessionInfo {
        ClaudeSessionInfo {
            pid,
            session_id: format!("sess-{pid}"),
            cwd: cwd.to_string(),
            project_name: "proj".to_string(),
            status,
            started_at: 0,
            updated_at: 0,
            host_app: TerminalApp::ITerm2,
            host_pid: 0,
            tty: String::new(),
        }
    }

    // transition 决策单测：cur.cwd 用不存在的绝对路径，让 git::is_dirty 必然返回
    // false（黑盒化 git），从而专注 finalize_status 的分支逻辑，不依赖真实仓库状态。
    const FAKE_CWD: &str = "/nonexistent/test/path/12345";

    #[test]
    fn busy_unchanged_no_git() {
        let cur = mock_info(1, Busy, FAKE_CWD);
        assert_eq!(finalize_status(&cur, None, true), Busy);
        assert_eq!(finalize_status(&cur, None, false), Busy);
    }

    #[test]
    fn waiting_unchanged_no_git() {
        let cur = mock_info(1, Waiting, FAKE_CWD);
        assert_eq!(finalize_status(&cur, None, true), Waiting);
        assert_eq!(finalize_status(&cur, None, false), Waiting);
    }

    #[test]
    fn idle_new_session_force_git_clean() {
        // 新会话 + force_git + cwd 不存在（is_dirty=false）→ Idle
        let cur = mock_info(1, Idle, FAKE_CWD);
        assert_eq!(finalize_status(&cur, None, true), Idle);
    }

    #[test]
    fn idle_new_session_no_force_still_runs_git() {
        // prev=None 即便 force_git=false 也无缓存可复用，必须跑 git（FAKE_CWD → clean → Idle）
        let cur = mock_info(1, Idle, FAKE_CWD);
        assert_eq!(finalize_status(&cur, None, false), Idle);
    }

    #[test]
    fn idle_reuse_cached_idle_when_no_force() {
        // prev=Idle, force=false → 复用，不跑 git
        let cur = mock_info(1, Idle, FAKE_CWD);
        let prev = mock_info(1, Idle, FAKE_CWD);
        assert_eq!(finalize_status(&cur, Some(&prev), false), Idle);
    }

    #[test]
    fn idle_reuse_cached_gitpending_when_no_force() {
        // prev=GitPending, force=false → 复用 GitPending（核心缓存复用场景）
        let cur = mock_info(1, Idle, FAKE_CWD);
        let prev = mock_info(1, GitPending, FAKE_CWD);
        assert_eq!(
            finalize_status(&cur, Some(&prev), false),
            GitPending
        );
    }

    #[test]
    fn idle_force_git_overrides_cache() {
        // prev=GitPending, force=true → 强制重算（FAKE_CWD → clean → Idle）
        let cur = mock_info(1, Idle, FAKE_CWD);
        let prev = mock_info(1, GitPending, FAKE_CWD);
        assert_eq!(finalize_status(&cur, Some(&prev), true), Idle);
    }

    #[test]
    fn idle_prev_busy_runs_git() {
        // prev=Busy（刚转 idle）→ was_idle_base=false → need_git=true → 跑 git（FAKE_CWD → Idle）
        let cur = mock_info(1, Idle, FAKE_CWD);
        let prev = mock_info(1, Busy, FAKE_CWD);
        assert_eq!(finalize_status(&cur, Some(&prev), false), Idle);
    }

    #[test]
    fn idle_prev_waiting_runs_git() {
        // prev=Waiting（刚转 idle）→ 同上，必须跑 git
        let cur = mock_info(1, Idle, FAKE_CWD);
        let prev = mock_info(1, Waiting, FAKE_CWD);
        assert_eq!(finalize_status(&cur, Some(&prev), false), Idle);
    }
}
