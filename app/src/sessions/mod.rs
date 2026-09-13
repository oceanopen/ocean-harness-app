// sessions 域：监听 `~/.claude/sessions/<pid>.json` 元数据，提供活跃 Claude Code 会话快照。
//
// 数据源：
//   ~/.claude/sessions/<pid>.json
// 内容示例：
//   {"pid":1,"sessionId":"...","cwd":"/path","startedAt":1700000000000,
//    "procStart":"...","version":"2.1.153","kind":"interactive","entrypoint":"cli",
//    "status":"idle","updatedAt":1700000000000}
//
// 子模块职责（单一关注点）：
//   raw      —— 反序列化 json 为 RawSessionFile
//   discover —— 扫目录 + 存活校验，返回活跃 RawSessionFile 列表
//   enrich   —— 进程反查（hostApp / hostPid / tty），输出 ClaudeSessionInfo
//   git      —— git 待提交检测（cwd 是否 dirty），供 store::rescan 在空闲会话判定 GitPending
//   store    —— 全量 rescan + 写 ClaudeSessionStore + emit claude-sessions:changed
//   watch    —— notify 文件监听，事件去抖后调 store::rescan
//   poll     —— 兜底轮询（周期可配置），驱动 Dead 老化

pub mod discover;
pub mod enrich;
pub mod git;
pub mod poll;
pub mod raw;
pub mod store;
pub mod watch;

// 统一出口：调用方无需感知子模块边界。
pub use store::rescan;
