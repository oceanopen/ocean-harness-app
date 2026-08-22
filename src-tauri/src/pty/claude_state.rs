// claude 运行态探测 + 会话引用定位：
//   - claude_running（terminal_03 §3.2）：判定某 PTY 会话的 shell 子进程树内
//     是否跑着 claude（按钮置灰驱动）。
//   - claude_session_ref（terminal_chat T1.2）：定位该 claude 的 sessionId +
//     transcript 路径（chat 只读视图数据源）。
//
// 手段：`~/.claude/sessions/<pid>.json`（sessions 域已有 discover::list_active，
// 存活过滤后即当前全部 claude 进程，且已解析出 sessionId/cwd/status）→ 沿
// `ps -o ppid=` 逐级向上爬父链（enrich.rs classify_terminal 的正向版：它从
// claude 向上找宿主 app，此处从 claude 向上找本 app 的 PTY shell pid）→ 任一级
// pid 命中目标会话的 shell pid 即命中。进程树匹配精确到具体终端（多 pane 同 cwd
// 也能区分），非输出流启发式（特征随版本变、清屏丢状态）。
//
// 前端驱动（useClaudeRunning）：会话 active 即查 + EVENT_CLAUDE_SESSIONS_CHANGED
// （watch 秒级，claude 启动写 json 即触发）+ 5s 轮询兜底退出恢复（Dead 会话
// json 保留、watch 不触发，进程退出只能轮询感知）。

use super::state::PtySessionStore;
use crate::sessions::raw::RawSessionFile;
use crate::shared::types::ClaudeSessionRef;

/// 父链爬取上限：claude → node → zsh →（本 app）足以覆盖；再深属异常进程树
/// （孤儿重挂 init 等），防环/防长循环硬停。
const MAX_ANCESTRY_DEPTH: usize = 8;

/// `ps -p <pid> -o ppid=`，失败返回 None（进程已死 / ps 不可用）。
/// 与 sessions/enrich.rs ps_field 同范式（零 nix/libc 依赖）。
fn parent_pid(pid: u32) -> Option<u32> {
    let output = std::process::Command::new("ps")
        .args(["-p", &pid.to_string(), "-o", "ppid="])
        .output()
        .ok()?;
    let text = String::from_utf8_lossy(&output.stdout)
        .trim()
        .to_string();
    if text.is_empty() {
        return None;
    }
    text.parse::<u32>().ok()
}

/// 本会话 shell 的 pid（child.process_id()；已退出/句柄异常返回 None）。
fn shell_pid(store: &PtySessionStore, session_id: &str) -> Option<u32> {
    let map = store
        .0
        .lock()
        .expect("PtySessionStore mutex poisoned");
    let session = map.get(session_id)?;
    session.child.lock().ok()?.process_id()
}

/// 某 claude 进程到目标 shell pid 的父链深度（0 = 直接子进程）。
/// 不在 shell 子进程树内返回 None。
fn ancestry_depth(claude_pid: u32, shell: u32) -> Option<usize> {
    let mut pid = claude_pid;
    for depth in 0..MAX_ANCESTRY_DEPTH {
        if pid == shell {
            return Some(depth);
        }
        match parent_pid(pid) {
            Some(next) if next != 0 && next != pid => pid = next,
            _ => return None,
        }
    }
    None
}

/// 找到本会话 shell 子进程树内父链最近的 claude，返回其 RawSessionFile。
/// 多 claude 同壳罕见（正常一个 shell 只跑一个 claude 交互会话）；若多个，取父链
/// 最近者（ancestry_depth 最小）。shell pid 未知（会话不在/已退出）返回 None。
fn find_claude_under_shell(store: &PtySessionStore, session_id: &str) -> Option<RawSessionFile> {
    let shell = shell_pid(store, session_id)?;
    crate::sessions::discover::list_active()
        .into_iter()
        .filter_map(|s| ancestry_depth(s.pid, shell).map(|depth| (depth, s)))
        .min_by_key(|(depth, _)| *depth)
        .map(|(_, s)| s)
}

/// 会话是否跑着 claude：遍历全部活跃 claude 进程，任一父链命中本会话 shell
/// 即 true。shell pid 未知（会话不在/已退出）恒 false。
pub fn claude_running(store: &PtySessionStore, session_id: &str) -> bool {
    let Some(shell) = shell_pid(store, session_id) else {
        // debug 级：会话不在 store 属非预期（调用方通常先 spawn），静默降级不刷屏。
        log::debug!(
            "[claude-state] session not in store: {}",
            session_id
        );
        return false;
    };
    let claudes = crate::sessions::discover::list_active();
    let running = claudes
        .iter()
        .any(|s| ancestry_depth(s.pid, shell).is_some());
    // info 级：真机排障主通道（置灰不生效时看 shell pid 与扫描到的 claude 列表）
    log::info!(
        "[claude-state] probe session={} shell_pid={} claude_pids={:?} running={}",
        session_id,
        shell,
        claudes.iter().map(|s| s.pid).collect::<Vec<_>>(),
        running
    );
    running
}

/// cwd 是否可用于推导 transcript 路径：非空、绝对路径、且非根 `/`。
/// 根路径 replace 后得单 `-`（目录名退化），transcript 路径不可信。
fn cwd_usable(cwd: &str) -> bool {
    let c = cwd.trim();
    !c.is_empty() && c.starts_with('/') && c != "/"
}

/// 定位本会话 shell 下 claude 的会话引用（sessionId + transcript 路径）。
///
/// 返回语义（对齐 pty_claude_session 命令三态）：
///   - Ok(None)：shell 下无 claude（未启动/已退出）
///   - Ok(Some)：定位成功，transcript_path 可信
///   - Err(_)：找到 claude 但 cwd 异常（空/非绝对/根路径），transcript 路径不可信
///
/// transcript 路径 = `~/.claude/projects/<cwd 的 `/`→`-`>/<sessionId>.jsonl`
/// （Claude Code 写入约定，前导 `/` 替换后成 `-`，本机实测一致）。
pub fn claude_session_ref(
    store: &PtySessionStore,
    session_id: &str,
) -> Result<Option<ClaudeSessionRef>, String> {
    let Some(raw) = find_claude_under_shell(store, session_id) else {
        log::debug!(
            "[claude-state] no claude under session={}",
            session_id
        );
        return Ok(None);
    };
    if !cwd_usable(&raw.cwd) {
        return Err(format!(
            "claude cwd 异常，transcript 路径不可信：{}",
            raw.cwd
        ));
    }
    let Some(projects_dir) = dirs::home_dir().map(|h| h.join(".claude").join("projects")) else {
        return Err("home_dir not available".to_string());
    };
    let transcript_path = projects_dir
        .join(raw.cwd.replace('/', "-"))
        .join(format!("{}.jsonl", raw.session_id))
        .to_string_lossy()
        .to_string();
    let status = crate::sessions::enrich::map_status(&raw.status);
    log::info!(
        "[claude-state] session_ref session={} claude_pid={} session_id={} status={:?} transcript={}",
        session_id,
        raw.pid,
        raw.session_id,
        status,
        transcript_path
    );
    Ok(Some(ClaudeSessionRef {
        claude_pid: raw.pid,
        session_id: raw.session_id.clone(),
        cwd: raw.cwd.clone(),
        transcript_path,
        status,
        waiting_for: raw.waiting_for.clone(),
    }))
}

#[cfg(test)]
mod tests {
    use super::*;

    // parent_pid / ancestry_depth / find_claude_under_shell 依赖真实进程树，仅
    // MAX_ANCESTRY_DEPTH 常量、cwd_usable 纯函数与空 store 行为可离线断言；
    // 进程匹配逻辑真机验证（任务 5 验证项）。

    #[test]
    fn ancestry_depth_is_tight() {
        // claude → node → zsh → app 共 3 级，8 级上限富余且防环足够短。
        assert!(MAX_ANCESTRY_DEPTH >= 3);
    }

    #[test]
    fn ancestry_depth_rejects_zero_and_self_loop() {
        // pid 0 / 自环（ps 异常返回自身）须终止爬取，防死循环。
        assert!(ancestry_depth(0, 42).is_none());
    }

    #[test]
    fn cwd_usable_accepts_absolute_non_root() {
        assert!(cwd_usable("/Users/foo/bar"));
    }

    #[test]
    fn cwd_usable_rejects_empty_relative_root() {
        assert!(!cwd_usable(""));
        assert!(!cwd_usable("   "));
        assert!(!cwd_usable("relative/path"));
        assert!(!cwd_usable("/"));
    }
}
