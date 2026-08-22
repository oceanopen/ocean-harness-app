// 跨 IPC 边界的共享类型（Rust ↔ TypeScript）。
// 通过 tauri-specta 自动导出到 src/shared/bindings.ts（运行 `pnpm gen:bindings`）。
// 修改本文件后必须重新生成 bindings.ts，否则前后端类型会漂移。

use serde::{Deserialize, Serialize};
use specta::Type;
// Number 用于把 i64/u32 等 BigInt-style 类型在 specta 导出时映射为 TS `number`。
// startedAt/updatedAt 是毫秒时间戳（远小于 2^53），精度安全。
use specta_typescript::Number;

// ============================================================
// AppConfigChangedPayload：app-config-changed 事件
// ============================================================

/// set_app_config 命令成功后通过 `app-config-changed` 事件广播给所有窗口的载荷。
/// 订阅方（AppThemeProvider / AppI18nProvider）据此响应配置变化。
#[derive(Clone, Serialize, Deserialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct AppConfigChangedPayload {
    /// 变更的配置 key（与 src/shared/appConfig.ts 中的 *_KEY 常量对齐）。
    pub key: String,
    /// 新值（配置统一以字符串形式存储，订阅方按 key 自行 decode）。
    pub value: String,
}

// ============================================================
// 终端会话：panel 窗口卡片 / pet 窗口桌宠状态
// ============================================================

/// 终端会话状态。直接映射 `~/.claude/sessions/<pid>.json` 里的 `status` 字段
/// （busy/waiting/idle）外加两个本地推断状态：GitPending（空闲且有未提交改动）与
/// Dead（进程已退出但 json 残留）。前端 ClaudeSessionCard 据此切换状态 Chip 配色与文案。
#[derive(Clone, Copy, PartialEq, Eq, Debug, Serialize, Deserialize, Type)]
pub enum ClaudeSessionStatus {
    /// 运行中：Claude 正在执行工具/生成回复。
    Busy,
    /// 等待输入：Claude 已完成回复，等用户输入。
    Waiting,
    /// 空闲：会话长时间无活动，但仍存活。
    Idle,
    /// 本地派生：会话空闲（base=Idle）且其 cwd 存在未提交 git 改动（含 untracked）。
    /// 由 `store::rescan` 在 enrich 后二次判定，不来自 Claude json。
    /// 有界过期：fs watcher 触发的 rescan（force_git=false）复用上次缓存值，
    /// poll（60s）/手动刷新（force_git=true）强制重算，避免 watcher 高频跑 git。
    GitPending,
    /// 已失效：进程已退出，json 残留。discover 阶段会过滤掉，理论上不会出现在前端。
    Dead,
}

/// 宿主终端应用。通过 `ps -p <ppid>` 链式反查 Claude 进程的祖先进程名得出。
/// 用于决定跳转时调用哪个 AppleScript 脚本。
#[derive(Clone, Copy, PartialEq, Eq, Debug, Serialize, Deserialize, Type)]
pub enum TerminalApp {
    ITerm2,
    Terminal,
    IntelliJ,
    /// 本 app（we-claude-terminal）的嵌入式 PTY 终端。跳转 = 前端聚焦对应
    /// issue 终端（terminal_03 任务 6；当前跳转暂走 UnsupportedHostApp，
    /// 聚焦联动在后续模块接线——但监控列表不再过滤，会话可见）。
    WeTerm,
    /// 未识别的宿主终端（如 VSCode 内嵌、Wezterm、Alacritty 等）。跳转按钮将禁用。
    Unknown,
}

/// Y/N 布尔风格配置值。serde rename 到单字母，序列化与 specta 导出均为 "Y"/"N"。
#[derive(Clone, Copy, PartialEq, Eq, Debug, Serialize, Deserialize, Type)]
pub enum YesNo {
    #[serde(rename = "Y")]
    Yes,
    #[serde(rename = "N")]
    No,
}

impl YesNo {
    /// 对应的存储字符串（app_config 层以裸字符串存储，非 JSON，故不走 serde 序列化）。
    pub const fn as_str(self) -> &'static str {
        match self {
            YesNo::Yes => "Y",
            YesNo::No => "N",
        }
    }
}

/// 终端会话快照。ClaudeSessionsPage 渲染 ClaudeSessionCard 列表的数据源；
/// PetClaudeSessionsSummaryApp 聚合所有会话取"最忙"状态作为桌宠展示态。
#[derive(Clone, Serialize, Deserialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct ClaudeSessionInfo {
    /// Claude Code 进程 pid（也是 `~/.claude/sessions/<pid>.json` 的文件名）。
    pub pid: u32,
    /// Claude Code 会话 ID（uuid）。从 json 的 `sessionId` 字段读取。
    pub session_id: String,
    /// 会话工作目录绝对路径。
    pub cwd: String,
    /// projectName = basename(cwd)，用于 UI 展示与 AppleScript 模糊匹配。
    pub project_name: String,
    /// 会话状态（Busy/Waiting/Idle/GitPending/Dead）。
    pub status: ClaudeSessionStatus,
    /// 会话启动时间（毫秒时间戳）。对应 json 的 `startedAt`。
    #[specta(type = Number)]
    pub started_at: i64,
    /// 最后一次状态更新时间（毫秒时间戳）。对应 json 的 `updatedAt`。
    #[specta(type = Number)]
    pub updated_at: i64,
    /// 宿主终端应用类型，决定跳转策略。
    pub host_app: TerminalApp,
    /// 宿主终端进程 pid（用于 AppleScript 间接定位）。
    pub host_pid: u32,
    /// 宿主终端的 tty 设备路径（如 `/dev/ttys004`），AppleScript 精确匹配用。
    /// 无法识别时为空字符串。
    pub tty: String,
}

/// PTY 会话内 claude 的定位引用（`pty_claude_session` 返回）。
/// 给定主 pane 的 session_id，定位「跑在该 PTY shell 下的 claude」的 sessionId + transcript 路径，
/// chat 视图据此订阅 transcript JSONL。仅出参（后端→前端），故不 derive Deserialize。
#[derive(Clone, Serialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct ClaudeSessionRef {
    /// claude 进程 pid（也是 `~/.claude/sessions/<pid>.json` 的文件名）。
    pub claude_pid: u32,
    /// claude 会话 ID（uuid）。从 json 的 `sessionId` 字段读取。
    pub session_id: String,
    /// 会话工作目录绝对路径。
    pub cwd: String,
    /// transcript JSONL 绝对路径（`~/.claude/projects/<cwd 的 `/`→`-`>/<sessionId>.jsonl`）。
    pub transcript_path: String,
    /// 会话状态（Busy/Waiting/Idle，经 `enrich::map_status` 归一化）。
    pub status: ClaudeSessionStatus,
}

// ============================================================
// HTTP 本地服务（go-server sidecar）运行态：panel 窗口「服务状态」菜单
// ============================================================

/// HTTP 本地服务的运行态：三态，反映 sidecar 生命周期。
/// - Stopped：未运行（从未启动 / 已停止 / 启动失败回退）
/// - Starting：已 spawn 子进程，等待启动成败裁定（日志静默后按 PID 身份校验）
/// - Running：自有进程已 bind 端口，HTTP 服务可 fetch
#[derive(Clone, Copy, PartialEq, Eq, Serialize, Type)]
#[serde(rename_all = "lowercase")]
#[repr(u8)]
pub enum HttpServerRunState {
    Stopped = 0,
    Starting = 1,
    Running = 2,
}

/// HTTP 本地服务的运行态快照（http_server_status 命令返回）。
/// 前端 ServerStatusPage 据此渲染 Switch 与服务地址，并 fetch <address>/api/baseInfo/getServerRunInfo。
/// 仅出参（后端→前端），故不 derive Deserialize。
#[derive(Clone, Serialize, Type)]
#[serde(rename_all = "camelCase")]
pub struct HttpServerStatus {
    /// 运行态（stopped/starting/running）。running 时端口已就绪，可直接 fetch，无需重试。
    pub run_state: HttpServerRunState,
    /// 服务地址（http://127.0.0.1:<port>），前端 fetch getServerRunInfo 用。
    pub address: String,
    /// 监听端口（默认 dev=9000/build=9100，可由「服务配置」覆盖）。
    pub port: u16,
    /// 运行模式（debug/release），与 Go gin mode 对齐。
    pub mode: String,
    /// 最近一次启动失败的详细原因（启动期进程退出 / 身份校验未通过 / 超时时填充；正常启动/停止为 None）。
    /// run_state 为 stopped 且此字段非空时，前端「服务状态」页据此 toast 失败原因。
    pub start_last_error: Option<String>,
    /// 本次启动过程的全量日志（stdout+stderr，换行拼接；仅 Starting 期间累积，启动结束即定格）。
    pub start_recent_log: Option<String>,
}
