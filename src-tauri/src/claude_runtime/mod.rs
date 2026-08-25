// claude_runtime 域：Claude hook 事件经 spool 文件回传，Rust 归一化为唯一运行时状态源，
// 事件驱动推送前端（docs/claude_orca_mode_01_overview.md P1）。
//
// 子模块：
//   types —— HookPayload 反序列化 + 前端事件 payload（specta 导出）
//   store —— ClaudeRuntimeStore（pane → ClaudeRuntimeState）+ JSON 快照落盘/hydrate
//   script / installer —— hook 脚本生成 + 工作区 .claude/settings.json 幂等安装器（T1.2）
//   ingest —— spool 行 → 归一化状态机（围栏/子代理防御/兜底绑定）（T1.3）
//   watch —— spool 目录 notify 监听 + 增量追行 + 目录治理（T1.3）

pub mod ingest;
pub mod installer;
pub mod script;
pub mod store;
pub mod types;
pub mod watch;

// 域入口 re-export：lib.rs 走 claude_runtime::init / claude_runtime::watch::start。
#[allow(unused_imports)]
pub use store::{init, persist};
