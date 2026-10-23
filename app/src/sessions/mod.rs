/*
 * sessions 域：监听 `~/.claude/sessions/<pid>.json` 元数据（{"pid","sessionId","cwd","startedAt","procStart","version","kind","entrypoint","status","updatedAt"}），提供活跃 Claude Code 会话快照。
 * 子模块职责（单一关注点）：raw 反序列化 json 为 RawSessionFile / discover 扫目录 + 存活校验返回活跃列表 / enrich 进程反查（hostApp / hostPid / tty）输出 ClaudeSessionInfo / git 待提交检测供 store::rescan 判定 GitPending / store 全量 rescan + 写 ClaudeSessionStore + emit claude-sessions:changed / watch notify 文件监听事件去抖后调 store::rescan / poll 兜底轮询（周期可配置）驱动 Dead 老化 / jsonl 直读 ~/.claude/projects 转录消息（Tauri command 供前端历史查看）。
 */
pub mod discover;
pub mod enrich;
pub mod git;
pub mod jsonl;
pub mod poll;
pub mod raw;
pub mod store;
pub mod watch;

// 统一出口：调用方无需感知子模块边界。
pub use store::rescan;
