// 扫描 `~/.claude/sessions/` 下所有 `<pid>.json`，过滤出活跃会话。
//
//   - 文件名 stem 必须是纯数字 pid（Claude Code 写入约定）
//   - 进程必须存活（kill -0 等价检查），否则视为 Dead 残留跳过
//   - 文件必须能成功反序列化为 RawSessionFile，损坏 silently skip
//
// 存活检查不引入 nix/libc 依赖：用 `ps -p <pid> -o pid=` 检查 stdout 非空，
// 跨平台兼容（macOS / Linux 行为一致），实现极简。

use std::fs;
use std::path::PathBuf;

use crate::sessions::raw::{self, RawSessionFile};

/// `~/.claude/sessions`，home_dir 探测失败返回 None。
pub(crate) fn claude_sessions_dir() -> Option<PathBuf> {
    dirs::home_dir().map(|h| h.join(".claude").join("sessions"))
}

/// 判断进程是否存活。等价于 `kill -0 <pid>`，但用 `ps` 实现以避免 nix/libc 依赖。
/// pid 为 0 或超大值时 ps 会返回非零 exit code，自然返回 false。
fn is_process_alive(pid: u32) -> bool {
    let output = std::process::Command::new("ps")
        .args(["-p", &pid.to_string(), "-o", "pid="])
        .output();
    match output {
        Ok(out) => !out.stdout.is_empty(),
        Err(_) => false,
    }
}

/// 列出所有活跃 Claude Code 会话。返回顺序未指定（调用方按需排序）。
///
/// 任一文件 IO / 反序列化失败 silently skip（容忍单文件损坏）；
/// sessions 目录不存在 / home_dir 探测失败返回空 Vec（不报错）。
pub fn list_active() -> Vec<RawSessionFile> {
    let Some(dir) = claude_sessions_dir() else {
        log::warn!("[sessions] home_dir not available");
        return vec![];
    };
    let Ok(entries) = fs::read_dir(&dir) else {
        // 目录不存在（用户从未跑过 Claude Code）视为空列表，不打 warn。
        return vec![];
    };

    let mut found = vec![];
    for entry in entries.flatten() {
        let path = entry.path();
        if !path.is_file() {
            continue;
        }
        if path.extension().and_then(|e| e.to_str()) != Some("json") {
            continue;
        }
        let Some(stem) = path.file_stem().and_then(|s| s.to_str()) else {
            continue;
        };
        if !stem.chars().all(|c| c.is_ascii_digit()) {
            continue;
        }

        let Ok(content) = fs::read_to_string(&path) else {
            continue;
        };
        let Ok(raw_file) = raw::parse(&content) else {
            continue;
        };

        // 双重校验：文件名 pid 与 json 内 pid 必须一致；进程必须存活。
        // 不一致说明文件被外部污染，跳过避免脏数据进入 store。
        if stem != raw_file.pid.to_string() {
            continue;
        }
        if !is_process_alive(raw_file.pid) {
            continue;
        }

        found.push(raw_file);
    }
    found
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn current_process_is_alive() {
        // 当前测试进程必然存活。
        assert!(is_process_alive(std::process::id()));
    }

    #[test]
    fn nonexistent_process_is_dead() {
        // pid 1<<31 几乎不可能存在。
        assert!(!is_process_alive(u32::MAX - 1));
    }
}
