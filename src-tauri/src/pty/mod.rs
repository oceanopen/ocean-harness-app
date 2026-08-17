// 任务 2（PTY 命令注册）接线前，骨架类型/函数暂无调用方。届时删除本 allow。
#![allow(dead_code)]

// pty 域：嵌入式终端会话生命周期管理（docs/embedded_terminal.md）。
//
// 与 terminal/ 域的边界：terminal/ 负责跳转/打开外部终端（iTerm2/Terminal.app），
// 本域负责应用内 PTY 会话（spawn/写/resize/关闭/reattach）——一 issue 一终端，
// 锚点为 issue uuid，cwd 为 `${workspace_base_dir}/${issueId}`。
//
// 子模块：
//   provider        —— PtyProvider trait（远程 SSH 扩展预留）+ SpawnOpts/PtySessionInfo
//   local_provider  —— LocalPtyProvider（portable-pty 本机实现）
//   session         —— PtySession（master/writer/child/退出标志）
//   state           —— PtySessionStore（Mutex<HashMap>，抗 webview 刷新常驻）
//
// IPC 命令（pty_spawn 等）与输出通道（Channel/emit）在任务 2 接入本 mod 并注册 lib.rs。

pub mod local_provider;
pub mod provider;
pub mod session;
pub mod state;
