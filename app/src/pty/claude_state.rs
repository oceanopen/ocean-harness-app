/*
 * claude 运行态探测：解析某 PTY 会话 shell 子进程树内的 claude sessionId（无则 None；bool 为其包装），「启动 claude」按钮置灰与终端捕获（T2.1 锚回写）共用。
 * 手段：`~/.claude/sessions/<pid>.json`（存活过滤即当前全部 claude 进程）→ 沿 `ps -o ppid=` 逐级向上爬父链 → 任一级 pid 命中目标会话 shell pid 即命中；进程树匹配精确到具体终端（多 pane 同 cwd 也能区分）。
 * 前端驱动（useClaudeRunning / useClaudeSessionCapture）：会话 active 即查 + EVENT_CLAUDE_SESSIONS_CHANGED + 轮询兜底。
 */
use super::state::PtySessionStore;

/*
 * 父链爬取上限：claude → node → zsh →（本 app）足以覆盖；再深属异常进程树
 * （孤儿重挂 init 等），防环/防长循环硬停。
 */
const MAX_ANCESTRY_DEPTH: usize = 8;

/*
 * `ps -p <pid> -o ppid=`，失败返回 None（进程已死 / ps 不可用）。
 * 与 sessions/enrich.rs ps_field 同范式（零 nix/libc 依赖）。
 */
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

// 本会话 shell 的 pid（child.process_id()；已退出/句柄异常返回 None）。
fn shell_pid(store: &PtySessionStore, session_id: &str) -> Option<u32> {
    let map = store
        .0
        .lock()
        .expect("PtySessionStore mutex poisoned");
    let session = map.get(session_id)?;
    session.child.lock().ok()?.process_id()
}

/*
 * 某 claude 进程到目标 shell pid 的父链深度（0 = 直接子进程）。
 * 不在 shell 子进程树内返回 None。
 */
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

/*
 * 会话子进程树内 claude 进程的 sessionId：遍历活跃 claude 进程，父链命中本会话
 * shell 即取其 sessionId（子树内多 claude 时取扫描序首个）。shell pid 未知（会话
 * 不在/已退出）恒 None。
 */
pub fn claude_session_id(store: &PtySessionStore, session_id: &str) -> Option<String> {
    let Some(shell) = shell_pid(store, session_id) else {
        // debug 级：会话不在 store 属非预期（调用方通常先 spawn），静默降级不刷屏。
        log::debug!(
            "[claude-state] session not in store: {}",
            session_id
        );
        return None;
    };
    let claudes = crate::sessions::discover::list_active();
    let matched = claudes
        .iter()
        .find_map(|s| ancestry_depth(s.pid, shell).map(|_| s.session_id.clone()));
    // info 级：真机排障主通道（置灰/捕获不生效时看 shell pid 与扫描到的 claude 列表）
    log::info!(
        "[claude-state] probe session={} shell_pid={} claude_pids={:?} matched={}",
        session_id,
        shell,
        claudes.iter().map(|s| s.pid).collect::<Vec<_>>(),
        matched.is_some()
    );
    matched
}

// 会话是否跑着 claude（claude_session_id 的 bool 包装）。
pub fn claude_running(store: &PtySessionStore, session_id: &str) -> bool {
    claude_session_id(store, session_id).is_some()
}

#[cfg(test)]
mod tests {
    use super::*;

    /*
     * parent_pid / ancestry_depth 依赖真实进程树，仅 MAX_ANCESTRY_DEPTH 常量与
     * 防环边界可离线断言；进程匹配逻辑真机验证（任务 5 验证项）。
     */

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
    fn ancestry_depth_hits_real_child() {
        // 真实子进程经父链可达（Darwin 实测可能隔一层 spawn 中介进程，深度非契约）；
        // 反向上爬（自己 → 父链）不会命中子进程 pid。
        let mut child = std::process::Command::new("sleep")
            .arg("5")
            .spawn()
            .expect("spawn sleep");
        let pid = child.id();
        assert!(matches!(ancestry_depth(pid, std::process::id()), Some(_)));
        assert!(ancestry_depth(std::process::id(), pid).is_none());
        child.kill().ok();
        child.wait().ok();
    }
}
