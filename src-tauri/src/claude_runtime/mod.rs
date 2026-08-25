// claude_runtime 域：Claude hook 事件经 spool 文件回传，Rust 归一化为唯一运行时状态源，
// 事件驱动推送前端（docs/claude_orca_mode_01_overview.md P1）。
//
// 子模块：
//   types —— HookPayload 反序列化 + 前端事件 payload（specta 导出）
//   store —— ClaudeRuntimeStore（pane → ClaudeRuntimeState）+ JSON 快照落盘/hydrate
//   script / installer —— hook 脚本生成 + 工作区 .claude/settings.json 幂等安装器（T1.2）
// 后续模块（T1.3）：
//   watch / ingest —— spool 目录监听 + 载荷归一化状态机

pub mod installer;
pub mod script;
pub mod store;
pub mod types;

// 域入口 re-export：lib.rs 走 claude_runtime::init，ingest（T1.3）走 claude_runtime::persist。
// persist 暂无调用方（T1.3 落地后接手），re-export 属预留——同 store::persist 的 allow 注记。
#[allow(unused_imports)]
pub use store::{init, persist};
