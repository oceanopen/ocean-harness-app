// 文件监听：notify + notify-debouncer-mini 监听 ~/.claude/sessions/ 变化。
//
// 任一环节失败 silently warn 后线程退出，poll 兜底接管。

use std::path::PathBuf;
use std::time::Duration;

use notify::RecursiveMode;
use tauri::AppHandle;

use crate::sessions::discover;
use crate::sessions::store;

/// 去抖窗口：单 turn 内 fsevents burst 合并为一次 rescan。
const WATCH_DEBOUNCE_MS: u64 = 1000;

/// 启动 fs watcher 后台线程。线程生命周期与进程一致。
pub fn start(app: AppHandle) {
    std::thread::spawn(move || {
        let (tx, rx) = std::sync::mpsc::channel();
        let mut debouncer = match notify_debouncer_mini::new_debouncer(
            Duration::from_millis(WATCH_DEBOUNCE_MS),
            tx,
        ) {
            Ok(d) => d,
            Err(e) => {
                log::warn!("[sessions] watcher init failed: {}", e);
                return;
            }
        };

        let dir: Option<PathBuf> = discover::claude_sessions_dir();
        let Some(dir) = dir else {
            log::warn!("[sessions] watcher: home_dir not available");
            return;
        };
        // sessions 目录可能尚未创建（用户未跑过 Claude Code）。手动建空目录供 watch，
        // 失败则 warn 后退出——Claude Code 首次启动后会自己创建，下次 app 重启接管。
        if !dir.exists() {
            if let Err(e) = std::fs::create_dir_all(&dir) {
                log::warn!("[sessions] watcher: create dir failed: {}", e);
                return;
            }
        }
        if let Err(e) = debouncer
            .watcher()
            .watch(&dir, RecursiveMode::NonRecursive)
        {
            log::warn!(
                "[sessions] watcher.watch failed on {}: {}",
                dir.display(),
                e
            );
            return;
        }

        log::info!("[sessions] watcher started on {}", dir.display());

        while rx.recv().is_ok() {
            // force_git=false：fs 事件高频（1s 去抖），空闲会话复用上次缓存的 GitPending 判定，
            // 避免每次 fs 抖动都跑 git。新鲜度由 poll(60s) 兜底。
            store::rescan(&app, false);
        }
    });
}
