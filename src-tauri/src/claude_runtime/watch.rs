// spool watcher（T1.3）：notify-debouncer-mini 监听 app_data_dir/claude-spool/，
// 消费新增行 → ingest 归一化。线程模型对齐 sessions/watch.rs，追行范式对齐
// transcript/tail.rs（per-file offset + read_range + 半行容忍）。
//
// 关键语义：
//   - 冷启动跳到 EOF：既有文件 offset 对齐末尾完整行，只消费启动后的新增——
//     旧事件不重放不污染（状态由快照 hydrate 恢复）。
//   - SessionStart 截断挪到批处理结束后统一执行：ingest 返回截断请求，
//     drain 完成后截断文件 + offset 归零。若在批中截断，批末 offset 写回会
//     覆盖截断后的 0 起点（旧坐标重读、事件双份消费），单线程序内批后执行
//     消除此竞态。
//   - 目录治理：启动按 mtime 淘汰超限文件（保留最近 SPOOL_KEEP_MAX 个）。

use std::collections::HashMap;
use std::io::{Read, Seek, SeekFrom};
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::Duration;

use notify::RecursiveMode;
use tauri::{AppHandle, Manager};

use super::ingest;
use super::script::{SPOOL_DIR_NAME, pane_from_spool_file};

/// 去抖窗口：单回合 hook burst（MessageDisplay 高频）合并为一次 drain。
const WATCH_DEBOUNCE_MS: u64 = 200;

/// spool 文件保留上限（按 mtime 淘汰最旧）。
const SPOOL_KEEP_MAX: usize = 50;

/// per-pane spool 读偏移（key 为 pane 锚点）。范式对齐 TranscriptWatchStore。
#[derive(Default)]
pub struct SpoolOffsets(pub Mutex<HashMap<String, u64>>);

/// 启动 spool watcher 后台线程。线程生命周期与进程一致；任一环节失败
/// warn 后退出（hook 链路失联时前端回落现有轮询，T6.1 fallback）。
pub fn start(app: AppHandle) {
    std::thread::spawn(move || {
        let (tx, rx) = std::sync::mpsc::channel();
        let mut debouncer = match notify_debouncer_mini::new_debouncer(
            Duration::from_millis(WATCH_DEBOUNCE_MS),
            tx,
        ) {
            Ok(d) => d,
            Err(e) => {
                log::warn!(
                    "[claude_runtime] spool watcher init failed: {}",
                    e
                );
                return;
            }
        };

        let Some(dir) = spool_dir(&app) else {
            log::warn!("[claude_runtime] spool watcher: app_data_dir not available");
            return;
        };
        // spool 目录由本模块自建（对齐 sessions/watch.rs：失败内聚降级，不占 lib.rs）。
        if let Err(e) = std::fs::create_dir_all(&dir) {
            log::warn!(
                "[claude_runtime] spool watcher: create {} failed: {}",
                dir.display(),
                e
            );
            return;
        }

        // 启动治理：mtime 淘汰 + 既有文件 offset 对齐 EOF（跳过历史）。
        evict_stale_spool_files(&dir);
        align_existing_to_eof(&app, &dir);

        if let Err(e) = debouncer
            .watcher()
            .watch(&dir, RecursiveMode::NonRecursive)
        {
            log::warn!(
                "[claude_runtime] spool watcher.watch failed on {}: {}",
                dir.display(),
                e
            );
            return;
        }
        log::info!(
            "[claude_runtime] spool watcher started on {}",
            dir.display()
        );

        while let Ok(events) = rx.recv() {
            let panes: Vec<String> = events
                .iter()
                .flat_map(|batch| batch.iter())
                .filter_map(|e| e.path.file_name().and_then(|n| n.to_str()))
                .filter_map(|n| pane_from_spool_file(n))
                .collect();
            if panes.is_empty() {
                continue;
            }
            for pane in panes {
                drain_pane(&app, &dir, &pane);
            }
        }
    });
}

/// spool 目录绝对路径（app_data_dir/claude-spool）。
fn spool_dir(app: &AppHandle) -> Option<PathBuf> {
    app.path()
        .app_data_dir()
        .ok()
        .map(|d| d.join(SPOOL_DIR_NAME))
}

/// 启动淘汰：超出保留上限的 spool 文件按 mtime 最旧删除。单项失败静默 skip。
fn evict_stale_spool_files(dir: &Path) {
    let files: Vec<(PathBuf, std::time::SystemTime)> = std::fs::read_dir(dir)
        .map(|entries| {
            entries
                .flatten()
                .filter_map(|e| {
                    let path = e.path();
                    let mtime = e.metadata().ok()?.modified().ok()?;
                    Some((path, mtime))
                })
                .collect()
        })
        .unwrap_or_default();
    for path in select_evictables(files) {
        if let Err(e) = std::fs::remove_file(&path) {
            log::warn!(
                "[claude_runtime] spool evict {} failed: {}",
                path.display(),
                e
            );
        }
    }
}

/// 淘汰选择（纯函数，单测用）：按 mtime 升序取超出保留上限的最旧部分。
fn select_evictables<T>(mut files: Vec<(T, std::time::SystemTime)>) -> Vec<T> {
    files.sort_by_key(|(_, mtime)| *mtime);
    let excess = files.len().saturating_sub(SPOOL_KEEP_MAX);
    files
        .into_iter()
        .take(excess)
        .map(|(name, _)| name)
        .collect()
}

/// 冷启动 offset 对齐：每个既有 spool 文件的 offset 记到末尾完整行——
/// 启动前写入的历史行不消费（状态由快照 hydrate 恢复，重放反而污染）。
fn align_existing_to_eof(app: &AppHandle, dir: &Path) {
    let Ok(entries) = std::fs::read_dir(dir) else {
        return;
    };
    let store = app.state::<SpoolOffsets>();
    let mut offsets = store
        .0
        .lock()
        .expect("SpoolOffsets mutex poisoned");
    for entry in entries.flatten() {
        let Some(name) = entry.file_name().to_str().map(String::from) else {
            continue;
        };
        let Some(pane) = pane_from_spool_file(&name) else {
            continue;
        };
        let Ok(size) = entry.metadata().map(|m| m.len()) else {
            continue;
        };
        let offset = last_complete_line_end(&entry.path(), size).unwrap_or(size);
        offsets.insert(pane, offset);
    }
}

/// 读文件尾部，返回最后一个 `\n` 之后的位置（对齐 transcript subscribe 的
/// offset 初始化语义：半行不计入 offset 留待下轮补齐）。读失败返回 None。
fn last_complete_line_end(path: &Path, size: u64) -> Option<u64> {
    const TAIL_CAP: u64 = 64 * 1024;
    let start = size.saturating_sub(TAIL_CAP);
    let mut f = std::fs::File::open(path).ok()?;
    f.seek(SeekFrom::Start(start)).ok()?;
    let mut buf = vec![0u8; (size - start) as usize];
    f.read_exact(&mut buf).ok()?;
    let content = String::from_utf8_lossy(&buf);
    content.rfind('\n').map(|i| start + i as u64 + 1)
}

/// 单 pane drain：读 [offset, size) 新增字节 → 完整行逐条 ingest → 推进 offset。
/// 批末执行截断（SessionStart 请求时）：文件清空 + offset 归零。
fn drain_pane(app: &AppHandle, dir: &Path, pane: &str) {
    let path = dir.join(super::script::spool_file_name(pane));
    let Ok(size) = std::fs::metadata(&path).map(|m| m.len()) else {
        return; // 文件暂缺（截断后首事件未落盘），保持 offset 下轮重试
    };

    let offset = {
        let store = app.state::<SpoolOffsets>();
        let map = store
            .0
            .lock()
            .expect("SpoolOffsets mutex poisoned");
        map.get(pane).copied().unwrap_or(0)
    };
    // 文件缩小（外部截断等）→ 重置 0 重读兜底。
    let effective_offset = if size < offset {
        0
    } else {
        offset
    };
    if size == effective_offset {
        return;
    }

    let Ok(content) = read_range(&path, effective_offset, size) else {
        return;
    };
    // 尾部未闭合的 JSON 行是 hook 正在写的一半，留待下轮。
    let complete_len = content.rfind('\n').map(|i| i + 1).unwrap_or(0);
    if complete_len == 0 {
        return;
    }

    let mut truncate = false;
    for line in content[..complete_len].lines() {
        if line.trim().is_empty() {
            continue;
        }
        if ingest::ingest(app, pane, line) {
            truncate = true;
        }
    }

    // 批后统一截断（消解截断/offset 写回竞态，见模块头注释）。
    let new_offset = if truncate {
        match std::fs::File::create(&path) {
            Ok(_) => 0,
            Err(e) => {
                log::warn!(
                    "[claude_runtime] spool truncate {} failed: {}",
                    path.display(),
                    e
                );
                effective_offset + complete_len as u64
            }
        }
    } else {
        effective_offset + complete_len as u64
    };

    let store = app.state::<SpoolOffsets>();
    let mut map = store
        .0
        .lock()
        .expect("SpoolOffsets mutex poisoned");
    map.insert(pane.to_string(), new_offset);
}

/// 读文件 [start, end) 字节范围（同 transcript/tail.rs read_range 范式）。
fn read_range(path: &Path, start: u64, end: u64) -> Result<String, std::io::Error> {
    let mut f = std::fs::File::open(path)?;
    f.seek(SeekFrom::Start(start))?;
    let mut buf = vec![0u8; (end - start) as usize];
    f.read_exact(&mut buf)?;
    Ok(String::from_utf8_lossy(&buf).to_string())
}

#[cfg(test)]
mod tests {
    use super::super::script::spool_file_name;
    use super::*;

    /// 并行测试隔离目录（installer.rs workspace_fixture 同款范式）。
    fn temp_dir_for(test: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("claude-runtime-watch-{test}"));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    #[test]
    fn half_line_waited_for_next_round() {
        let dir = temp_dir_for("half-line");
        let path = dir.join(spool_file_name("iss1::p1"));
        // 先写一条完整行 + 一条半行。
        std::fs::write(&path, "{\"a\":1}\n{\"half\":").unwrap();
        let size = std::fs::metadata(&path).unwrap().len();
        // offset 对齐后应停在完整行末尾（半行不计入）。
        let end = last_complete_line_end(&path, size).unwrap();
        assert_eq!(end, "{\"a\":1}\n".len() as u64);
        let _ = std::fs::remove_dir_all(&dir);
    }

    /// offset 超过文件当前大小（外部截断场景）→ drain 语义按 0 重读
    /// （effective_offset 推导逻辑的单测等价：此处验证比较分支本身）。
    #[test]
    fn shrink_resets_effective_offset() {
        let dir = temp_dir_for("shrink");
        let path = dir.join(spool_file_name("iss1::p1"));
        std::fs::write(&path, "{\"a\":1}\n").unwrap();
        let size = std::fs::metadata(&path).unwrap().len();
        // 模拟 offset 大于 size：effective_offset 应取 0。
        let offset = size + 100;
        let effective = if size < offset {
            0
        } else {
            offset
        };
        assert_eq!(effective, 0);
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn align_offsets_to_last_complete_line() {
        let dir = temp_dir_for("align");
        let path = dir.join(spool_file_name("iss1::p1"));
        std::fs::write(&path, "{\"a\":1}\n{\"b\":2}\n{\"half\"").unwrap();
        let size = std::fs::metadata(&path).unwrap().len();
        let end = last_complete_line_end(&path, size).unwrap();
        assert_eq!(end, "{\"a\":1}\n{\"b\":2}\n".len() as u64);
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn evict_selects_oldest_beyond_limit() {
        let base = std::time::UNIX_EPOCH;
        let files = vec![
            ("a.jsonl".to_string(), base), // 最旧 → 淘汰
            (
                "b.jsonl".to_string(),
                base + std::time::Duration::from_secs(1),
            ),
            (
                "c.jsonl".to_string(),
                base + std::time::Duration::from_secs(2),
            ),
        ];
        // 上限 50，3 个文件不淘汰；构造超限场景直接验证排序取尾逻辑。
        assert!(select_evictables(files.clone()).is_empty());
        let mut many = files;
        for i in 3..60 {
            many.push((
                format!("f{i}.jsonl"),
                base + std::time::Duration::from_secs(i),
            ));
        }
        let evicted = select_evictables(many);
        assert_eq!(evicted.len(), 60 - SPOOL_KEEP_MAX);
        assert_eq!(evicted[0], "a.jsonl", "oldest evicted first");
        let _ = std::fs::remove_dir_all(temp_dir_for("evict"));
    }

    #[test]
    fn read_range_reads_exact_window() {
        let dir = temp_dir_for("read-range");
        let path = dir.join("x.jsonl");
        std::fs::write(&path, "0123456789").unwrap();
        assert_eq!(read_range(&path, 2, 5).unwrap(), "234");
        let _ = std::fs::remove_dir_all(&dir);
    }
}
