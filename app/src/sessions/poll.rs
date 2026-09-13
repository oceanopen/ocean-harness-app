// 兜底轮询：周期可配置，每 N 秒全量 rescan 一次，
// 驱动 Dead 老化 + 兜底 fs watcher 漏报。
//
// 即时性由 watcher 负责，本线程只驱动老化与漏报兜底，粗粒度即可。
// 周期经 app-config-changed 事件动态更新（见 set_interval）：写入原子变量后，下个循环周期生效。

use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;

use tauri::{AppHandle, Manager};

use crate::sessions::store;
use crate::shared::app_config::{
    AppConfigState, DEFAULT_POLL_INTERVAL_SECS, MAX_POLL_INTERVAL_SECS, MIN_POLL_INTERVAL_SECS,
    POLL_INTERVAL_SECS_KEY,
};

/// 当前兜底轮询周期（秒）。manage 到 app 供 set_interval 更新；
/// poll 线程每轮循环开头读最新值，故配置变更后下个周期即生效。
pub struct PollIntervalState(Arc<AtomicU64>);

/// 将秒数 clamp 到 [MIN, MAX]，防御非法配置（直接改 DB / 脏数据）。
fn clamp_interval(secs: u64) -> u64 {
    secs.clamp(MIN_POLL_INTERVAL_SECS, MAX_POLL_INTERVAL_SECS)
}

/// 从 SQLite 读初始周期：解析失败或越界则回退默认值。
fn read_initial_interval(app: &AppHandle) -> u64 {
    app.try_state::<AppConfigState>()
        .and_then(|s| {
            crate::shared::app_config::read_app_config_raw(s.inner(), POLL_INTERVAL_SECS_KEY).ok()
        })
        .flatten()
        .and_then(|v| v.parse::<u64>().ok())
        .map(clamp_interval)
        .unwrap_or(DEFAULT_POLL_INTERVAL_SECS)
}

/// 启动兜底轮询后台线程。把当前周期 manage 为 PollIntervalState 供 set_interval 更新。
/// 线程生命周期与进程一致。
pub fn start(app: AppHandle) {
    let initial = read_initial_interval(&app);
    let interval = Arc::new(AtomicU64::new(initial));
    app.manage(PollIntervalState(interval.clone()));

    std::thread::spawn(move || {
        loop {
            let secs = interval.load(Ordering::Relaxed);
            std::thread::sleep(Duration::from_secs(secs));
            // force_git=true：兜底轮询强制重算空闲会话的 git 状态，
            // 驱动 GitPending 过期（用户在终端 commit 后徽章在此周期内回退）。
            store::rescan(&app, true);
        }
    });
}

/// 更新兜底轮询周期（秒），下个循环周期生效。非法值 clamp 到 [MIN, MAX]。
pub fn set_interval(app: &AppHandle, secs: u64) {
    if let Some(state) = app.try_state::<PollIntervalState>() {
        state
            .0
            .store(clamp_interval(secs), Ordering::Relaxed);
    }
}
