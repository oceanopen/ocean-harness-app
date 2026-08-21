mod pty;
mod sessions;
mod shared;
mod terminal;
mod windows;

use tauri::{Listener, Manager};
use tauri_specta::{Builder, collect_commands};

// 集中注册所有 IPC 命令到 tauri-specta Builder。
// run()（注册 invoke handler）与 bin/export_bindings.rs（生成 TS 绑定）共用此函数，
// 保证命令清单单一来源，避免两份注册表漂移。
pub fn build_specta_builder() -> Builder<tauri::Wry> {
    use crate::pty::session::PtyEvent;
    use crate::shared::types::{
        AppConfigChangedPayload, ClaudeSessionInfo, ClaudeSessionStatus, TerminalApp, YesNo,
    };
    use crate::terminal::NavErr;
    Builder::<tauri::Wry>::new()
        .commands(collect_commands![
            windows::panel::show_panel_window,
            windows::panel::get_claude_sessions,
            windows::panel::refresh_sessions,
            windows::panel::navigate_to_claude_session,
            windows::panel::open_in_editor,
            windows::panel::is_java_project,
            windows::panel::open_in_terminal,
            windows::panel::open_in_file_manager,
            windows::panel::open_path,
            windows::pet_claude_sessions_summary::show_pet_claude_sessions_summary_window,
            windows::pet_claude_sessions_summary::hide_pet_claude_sessions_summary_window,
            windows::pet_claude_sessions_summary::toggle_pet_claude_sessions_summary_window,
            windows::pet_claude_sessions_summary::get_pet_claude_sessions_summary_visibility_state,
            windows::pet_claude_sessions_task::show_pet_claude_sessions_task_window,
            windows::pet_claude_sessions_task::hide_pet_claude_sessions_task_window,
            windows::pet_claude_sessions_task::fit_pet_claude_sessions_task,
            windows::settings::show_settings_window,
            shared::app_config::get_app_config,
            shared::app_config::set_app_config,
            shared::http_server::http_server_status,
            shared::http_server::set_http_server_enabled,
            shared::http_server::cleanup_orphan_http_server,
            pty::pty_spawn,
            pty::pty_write,
            pty::pty_resize,
            pty::pty_shutdown,
            pty::pty_shutdown_issue,
            pty::pty_list_sessions,
            pty::pty_exists,
            pty::pty_claude_running,
            pty::pty_reattach,
            pty::create_directory,
        ])
        // 以下类型不出现在任何 command 签名中（仅作为事件载荷或前端数据模型），
        // 用 typ 显式注册，让 specta 把它们导出到 bindings.ts 供前端复用。
        .typ::<AppConfigChangedPayload>()
        .typ::<ClaudeSessionStatus>()
        .typ::<TerminalApp>()
        .typ::<YesNo>()
        .typ::<ClaudeSessionInfo>()
        .typ::<NavErr>()
        // PtyEvent 是 Channel<PtyEvent> 的泛型载荷（Channel 本身在参数签名中可见，
        // 但泛型参数类型需显式注册才能导出 union 定义）。
        .typ::<PtyEvent>()
        // Rust/TS 共用常量经 .constant() 导出到 bindings.ts（Rust 单源，前端 re-export）。
        // config key 族（app_config.rs）：后端读取的 key 在此定义，前端仅消费生成物，
        // 根治此前 appConfig.ts 手工镜像的双份维护漂移（POLL 三值曾实际漂移）。
        .constant("LANGUAGE_KEY", shared::app_config::LANGUAGE_KEY)
        .constant("PET_CLAUDE_SESSIONS_SUMMARY_DRAGGABLE_KEY", shared::app_config::PET_CLAUDE_SESSIONS_SUMMARY_DRAGGABLE_KEY)
        .constant("POLL_INTERVAL_SECS_KEY", shared::app_config::POLL_INTERVAL_SECS_KEY)
        .constant("DEFAULT_POLL_INTERVAL_SECS", shared::app_config::DEFAULT_POLL_INTERVAL_SECS)
        .constant("MIN_POLL_INTERVAL_SECS", shared::app_config::MIN_POLL_INTERVAL_SECS)
        .constant("MAX_POLL_INTERVAL_SECS", shared::app_config::MAX_POLL_INTERVAL_SECS)
        .constant("ITERM2_SPLIT_DIRECTION_KEY", shared::app_config::ITERM2_SPLIT_DIRECTION_KEY)
        .constant("DEFAULT_ITERM2_SPLIT_DIRECTION", shared::app_config::DEFAULT_ITERM2_SPLIT_DIRECTION)
        .constant("TERMINAL_POST_OPEN_COMMAND_KEY", shared::app_config::TERMINAL_POST_OPEN_COMMAND_KEY)
        .constant("DEFAULT_TERMINAL_POST_OPEN_COMMAND", shared::app_config::DEFAULT_TERMINAL_POST_OPEN_COMMAND)
        .constant("HTTP_SERVER_PORT_KEY", shared::app_config::HTTP_SERVER_PORT_KEY)
        .constant("MIN_HTTP_SERVER_PORT", shared::app_config::MIN_HTTP_SERVER_PORT)
        .constant("MAX_HTTP_SERVER_PORT", shared::app_config::MAX_HTTP_SERVER_PORT)
        // HTTP 端口默认值（http_server.rs）：设置页帮助文案按运行模式展示默认端口。
        .constant("HTTP_SERVER_PORT_TEST", shared::http_server::HTTP_SERVER_PORT_TEST)
        .constant("HTTP_SERVER_PORT_RELEASE", shared::http_server::HTTP_SERVER_PORT_RELEASE)
        // 事件名（events.rs）：emit/listen 字符串 typo 不编译报错，单源导出消双份。
        .constant("EVENT_APP_CONFIG_CHANGED", crate::shared::events::EVENT_APP_CONFIG_CHANGED)
        .constant("EVENT_CLAUDE_SESSIONS_CHANGED", crate::shared::events::EVENT_CLAUDE_SESSIONS_CHANGED)
        .constant("EVENT_CLAUDE_SESSION_NAV_FAILED", crate::shared::events::EVENT_CLAUDE_SESSION_NAV_FAILED)
        .constant("EVENT_PET_CLAUDE_SESSIONS_TASK_REFIT", crate::shared::events::EVENT_PET_CLAUDE_SESSIONS_TASK_REFIT)
        .constant("EVENT_PANEL_NAVIGATE", crate::shared::events::EVENT_PANEL_NAVIGATE)
        .constant("EVENT_PANEL_SHOWN", crate::shared::events::EVENT_PANEL_SHOWN)
        .constant("EVENT_SETTINGS_NAVIGATE", crate::shared::events::EVENT_SETTINGS_NAVIGATE)
        .constant("EVENT_HTTP_SERVER_STATE_CHANGED", crate::shared::events::EVENT_HTTP_SERVER_STATE_CHANGED)
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let specta_builder = build_specta_builder();

    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_dialog::init())
        .invoke_handler(specta_builder.invoke_handler())
        .setup(move |app| {
            // macOS 隐藏 Dock 图标：将应用激活策略设为 Accessory（代理应用），
            // 应用不再出现在程序坞和应用菜单栏，只保留顶部状态栏托盘图标。
            // 该 API 仅 macOS 生效；Windows/Linux 任务栏隐藏由各窗口的 skip_taskbar(true) 负责。
            #[cfg(target_os = "macos")]
            {
                let _ = app.set_activation_policy(tauri::ActivationPolicy::Accessory);
            }

            // 日志：dev 用 Info，同时输出 stdout + 日志文件；release 用 Warn，只写日志文件（OS 日志目录，1 MiB 轮转、保留 1 份），方便生产排障。
            let log_plugin = if cfg!(debug_assertions) {
                tauri_plugin_log::Builder::default()
                    .level(log::LevelFilter::Info)
                    .targets([
                        tauri_plugin_log::Target::new(tauri_plugin_log::TargetKind::Stdout),
                        tauri_plugin_log::Target::new(tauri_plugin_log::TargetKind::LogDir {
                            file_name: None,
                        }),
                    ])
                    .build()
            } else {
                tauri_plugin_log::Builder::default()
                    .level(log::LevelFilter::Warn)
                    .targets([tauri_plugin_log::Target::new(
                        tauri_plugin_log::TargetKind::LogDir { file_name: None },
                    )])
                    // 1 MiB/文件，保留最近 1 份（旧的重命名带日期），总量 ~5 MiB 有界
                    .max_file_size(1_048_576)
                    .rotation_strategy(tauri_plugin_log::RotationStrategy::KeepSome(1))
                    .build()
            };
            app.handle().plugin(log_plugin)?;

            shared::app_config::init(app)?;
            shared::state::claude_sessions::init(app)?;
            pty::state::init(app)?;
            // shell-ready 包装文件根（app_data_dir/shell-ready）：注入失败仅 warn，
            // startup_command 降级为裸 spawn，终端照常可用。
            if let Ok(dir) = app.path().app_data_dir() {
                pty::local_provider::set_app_data_dir(dir);
            } else {
                log::warn!("[pty] resolve app_data_dir failed, shell-ready disabled");
            }
            windows::tray::setup(app)?;

            // 先 rescan 填充 ClaudeSessionStore 并广播首批快照，保证后续 pet_claude_sessions_task / pet
            // 窗口 React mount 后初次拉取 IPC 时 store 必有数据，根治启动期"0 个活跃"竞态。
            // force_git=true：启动首次对空闲会话跑一次 git，得到准确的 GitPending 初值。
            sessions::rescan(app.handle(), true);

            // 预构建 pet_claude_sessions_task 窗口（隐藏）：webview 异步加载，React mount 时机虽不确定，
            // 但 store 已满，初次 IPC 必拿到非空数据；后续 claude-sessions:changed 事件持续驱动。
            if let Err(e) = windows::pet_claude_sessions_task::ensure(app.handle()) {
                log::warn!(
                    "[pet-claude-sessions-task] startup ensure failed: {}",
                    e
                );
            }

            sessions::watch::start(app.handle().clone());
            sessions::poll::start(app.handle().clone());

            // 桌宠显隐读 pet_claude_sessions_summary_visible 偏好：用户上次隐藏则保持隐藏，否则启动显示。
            // pet 显示后由前端基于 count 调 show_pet_claude_sessions_task_window 联动面板显隐。
            windows::pet_claude_sessions_summary::startup_show(app.handle());
            // 托盘菜单在 setup 时基于窗口可见性初始化文案，此时 pet 窗口尚未创建，
            // 故恒为"显示桌宠"；startup_show 确定真实显隐后刷新一次以纠正文案。
            windows::tray::refresh_menu_texts(app.handle());

            specta_builder.mount_events(app);

            let handle = app.handle().clone();
            app.listen(
                crate::shared::events::EVENT_APP_CONFIG_CHANGED,
                move |event| {
                    let Ok(value) = serde_json::from_str::<serde_json::Value>(event.payload())
                    else {
                        return;
                    };
                    let key = value.get("key").and_then(|v| v.as_str());
                    if key == Some(shared::app_config::LANGUAGE_KEY) {
                        windows::tray::refresh_menu_texts(&handle);
                    } else if key == Some(shared::app_config::POLL_INTERVAL_SECS_KEY) {
                        if let Some(secs) = value
                            .get("value")
                            .and_then(|v| v.as_str())
                            .and_then(|s| s.parse::<u64>().ok())
                        {
                            sessions::poll::set_interval(&handle, secs);
                        }
                    }
                },
            );

            // 后台异步拉起 Go 本地 HTTP 服务（dev 先 go build，build 用随包二进制），前端 fetch 直连。
            // 非核心依赖：init 在后台线程进行，失败仅 log::warn，永不阻塞 setup、永不拖垮 app。
            shared::http_server::init(app.handle());

            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building tauri application")
        .run(|app, event| {
            // 应用退出时回收子进程：Go 服务（kill + wait）与全部 PTY shell，避免孤儿进程。
            if let tauri::RunEvent::Exit = event {
                if let Some(state) = app.try_state::<shared::http_server::HttpServerState>() {
                    shared::http_server::shutdown(state.inner());
                }
                if let Some(state) = app.try_state::<pty::state::PtySessionStore>() {
                    pty::shutdown_all(state.inner());
                }
            }
        });
}
