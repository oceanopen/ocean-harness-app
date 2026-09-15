// ocean-harness CLI 命令注册：把随包 sidecar 的 cli 二进制 symlink 到 /usr/local/bin，
// 让任意终端可直接执行 `ocean-harness-cli xxx`（release）/ `ocean-harness-dev-cli xxx`（debug）。
//
// 注册模型（参照 orca 四态状态机）：
//   classify 用 symlink_metadata + canonicalize 判定四态——Installed（symlink 指向本 app 的
//   cli_bin）/ Stale（symlink 指向别处，含悬空）/ Conflict（同名普通文件，用户自有文件，拒碰）/
//   NotInstalled。只在 NotInstalled/Stale 上操作，Conflict 永不改写。
//
// 提权链路：先试直接 symlink（多数机器 admin 用户对 /usr/local/bin 有组写权限）；
// EACCES 时 osascript `with administrator privileges` 弹系统密码框。init 自动注册路径下
// 用户取消授权 → 写 app_config 记「被拒时的 app 版本」（版本化语义：同版本不再自动弹框，
// app 升级后自动重试一次），见 CLI_ELEVATION_DECLINED_KEY。
//
// 生命周期：init 在 setup 末尾后台线程幂等执行（仅 NotInstalled 且本版本未拒绝时自动装，
// Stale/Conflict 只留日志不动手）；install/uninstall 以 IPC 命令暴露供前端/后续 bot 调用。
// 非 macOS 平台整体 no-op（Windows 注册进 PATH 属后续工作）。

use std::path::{Path, PathBuf};

use tauri::{AppHandle, Manager};

use crate::shared::app_config::{
    AppConfigState, CLI_ELEVATION_DECLINED_KEY, read_app_config_raw, write_app_config_raw,
};
use crate::shared::types::{CliCommandLinkState, CliCommandStatus};

/// CLI symlink 落点目录（macOS 上 PATH 默认包含，orca 同款）。
const CLI_BIN_DIR: &str = "/usr/local/bin";

/// 注册的命令名：debug 构建 = ocean-harness-dev-cli（连 dev app 的 9000 端口服务），
/// release 构建 = ocean-harness-cli（连 release app 的 9100）。按编译期区分，与
/// app_data_dir 的 dev/release 隔离同构——两套环境互不覆盖。
pub fn link_name() -> &'static str {
    if cfg!(debug_assertions) {
        "ocean-harness-dev-cli"
    } else {
        "ocean-harness-cli"
    }
}

/// symlink 完整路径。
fn link_path() -> PathBuf {
    Path::new(CLI_BIN_DIR).join(link_name())
}

/// 本 app 的 cli 二进制路径：随包 sidecar 与主程序同目录，命名 = {identifier}-cli_bin
/// （tauri externalBin 约定：打包去 triple 后缀进 Contents/MacOS/，dev 下 tauri-build
/// 拷到 target/debug/<同名>；与 go_server_bin 同一命名规则，见 scripts/build-server.mjs）。
fn cli_bin_path(app: &AppHandle) -> Result<PathBuf, String> {
    let exe = std::env::current_exe().map_err(|e| format!("获取当前可执行文件路径失败: {e}"))?;
    let dir = exe
        .parent()
        .ok_or_else(|| "无法定位可执行文件所在目录".to_string())?;
    Ok(dir.join(format!("{}-cli_bin", app.config().identifier)))
}

// ---- app_config：提权取消记忆（版本化语义） ----

fn declined_version(app: &AppHandle) -> Option<String> {
    let state = app.try_state::<AppConfigState>()?;
    read_app_config_raw(state.inner(), CLI_ELEVATION_DECLINED_KEY)
        .ok()
        .flatten()
        .filter(|v| !v.trim().is_empty())
}

fn is_declined_for_current_version(app: &AppHandle) -> bool {
    declined_version(app).is_some_and(|v| v == app.package_info().version.to_string())
}

/// 记录「本版本用户已拒绝提权」。仅注册路径被取消时写入——自动弹框打扰一次即止。
fn mark_declined(app: &AppHandle) {
    let Some(state) = app.try_state::<AppConfigState>() else {
        return;
    };
    let version = app.package_info().version.to_string();
    if let Err(e) = write_app_config_raw(
        state.inner(),
        CLI_ELEVATION_DECLINED_KEY,
        &version,
    ) {
        log::warn!("[cli-register] 写入提权取消记录失败: {e}");
    }
}

// ---- macOS 实现（symlink + osascript 提权） ----

#[cfg(target_os = "macos")]
mod imp {
    use std::fs;
    use std::io;
    use std::os::unix::fs::symlink;
    use std::process::Command;
    use std::thread;

    use super::*;

    /// 提权操作结果。Canceled 单列：自动注册路径据此写取消记忆。
    enum ElevateOutcome {
        Success,
        Canceled(String),
        Failed(String),
    }

    /// shell 单引号包裹（路径含空格/特殊字符安全；单引号本身按 '\'' 惯例转义）。
    fn shell_quote(s: &str) -> String {
        format!("'{}'", s.replace('\'', r"'\''"))
    }

    /// osascript 通用执行：包一层「用户取消」判定——错误文案随系统语言变化，
    /// errAECanceled(-128) 是唯一的 locale 无关信号。
    fn run_osascript(apple_script: &str) -> ElevateOutcome {
        match Command::new("osascript")
            .arg("-e")
            .arg(apple_script)
            .output()
        {
            Ok(out) if out.status.success() => ElevateOutcome::Success,
            Ok(out) => {
                let stderr = String::from_utf8_lossy(&out.stderr);
                if stderr.contains("-128") {
                    ElevateOutcome::Canceled(stderr.trim().to_string())
                } else {
                    ElevateOutcome::Failed(stderr.trim().to_string())
                }
            }
            Err(e) => ElevateOutcome::Failed(e.to_string()),
        }
    }

    /// AppleScript 字符串内转义反斜杠与双引号（单引号原样穿透给 shell）。
    fn wrap_admin(shell_cmd: &str) -> String {
        format!(
            "do shell script \"{}\" with administrator privileges",
            shell_cmd
                .replace('\\', "\\\\")
                .replace('"', "\\\""),
        )
    }

    /// 提权建链：mkdir -p + ln -sfn（-n 替换既有 symlink，-f 强制覆盖）。
    fn symlink_elevated(bin: &Path, link: &Path) -> ElevateOutcome {
        let shell_cmd = format!(
            "mkdir -p {} && ln -sfn {} {}",
            shell_quote(CLI_BIN_DIR),
            shell_quote(&bin.to_string_lossy()),
            shell_quote(&link.to_string_lossy()),
        );
        run_osascript(&wrap_admin(&shell_cmd))
    }

    /// 卸载时对 /usr/local/bin 无写权限的兜底提权 rm。
    fn remove_elevated(link: &Path) -> ElevateOutcome {
        run_osascript(&wrap_admin(&format!(
            "rm -f {}",
            shell_quote(&link.to_string_lossy())
        )))
    }

    /// 四态判定。悬空 symlink（目标已删）归 Stale——「指向别处」的特例，同样可安全替换。
    fn classify(link: &Path, bin: &Path) -> CliCommandLinkState {
        let Ok(md) = fs::symlink_metadata(link) else {
            return CliCommandLinkState::NotInstalled;
        };
        if !md.is_symlink() {
            return CliCommandLinkState::Conflict;
        }
        match (link.canonicalize(), bin.canonicalize()) {
            (Ok(a), Ok(b)) if a == b => CliCommandLinkState::Installed,
            _ => CliCommandLinkState::Stale,
        }
    }

    fn build_status(app: &AppHandle) -> CliCommandStatus {
        let bin = cli_bin_path(app).unwrap_or_default();
        CliCommandStatus {
            state: classify(&link_path(), &bin),
            link_name: link_name().to_string(),
            link_path: link_path().to_string_lossy().to_string(),
            bin_path: bin.to_string_lossy().to_string(),
            elevation_declined_version: declined_version(app),
        }
    }

    pub fn status(app: &AppHandle) -> CliCommandStatus {
        build_status(app)
    }

    /// 确保命令已注册（幂等）。返回终态快照；失败返回中文错误。
    /// 提权被取消时同样写入取消记忆（用户主动拒绝，本轮不再打扰）。
    pub fn install(app: &AppHandle) -> Result<CliCommandStatus, String> {
        let bin = cli_bin_path(app)?;
        if fs::symlink_metadata(&bin).is_err() {
            return Err(format!(
                "CLI 二进制不存在: {}（dev 模式请先执行 pnpm server:test 构建产物）",
                bin.display()
            ));
        }
        let link = link_path();
        match classify(&link, &bin) {
            CliCommandLinkState::Conflict => {
                return Err(format!(
                    "{} 已存在且不是符号链接（可能是用户自有文件），拒绝覆盖；请手动处理后重试",
                    link.display()
                ));
            }
            CliCommandLinkState::Installed => return Ok(build_status(app)),
            // 旧 symlink 指向别处：先尝试摘掉再走正常建链（摘不掉也无妨，提权分支的 ln -sfn 会覆盖）。
            CliCommandLinkState::Stale => {
                let _ = fs::remove_file(&link);
            }
            CliCommandLinkState::NotInstalled => {}
        }
        match symlink(&bin, &link) {
            Ok(()) => {}
            // 权限不足（或摘旧链接失败导致的 EEXIST）→ osascript 提权。
            Err(e)
                if e.kind() == io::ErrorKind::PermissionDenied
                    || e.kind() == io::ErrorKind::AlreadyExists =>
            {
                match symlink_elevated(&bin, &link) {
                    ElevateOutcome::Success => {}
                    ElevateOutcome::Canceled(msg) => {
                        mark_declined(app);
                        return Err(format!("已取消管理员授权，本次未注册（{msg}）"));
                    }
                    ElevateOutcome::Failed(msg) => return Err(format!("提权注册失败: {msg}")),
                }
            }
            Err(e) => return Err(format!("创建符号链接失败: {e}")),
        }
        let status = build_status(app);
        if status.state != CliCommandLinkState::Installed {
            return Err(format!(
                "注册后校验未通过，当前状态: {:?}",
                status.state
            ));
        }
        Ok(status)
    }

    /// 移除本 app 注册的命令（幂等）。仅删除指向本 app cli_bin 的 symlink；
    /// Stale/Conflict 拒删——symlink 指向别处或同名普通文件都不是本 app 的产物。
    pub fn uninstall(app: &AppHandle) -> Result<CliCommandStatus, String> {
        let bin = cli_bin_path(app)?;
        let link = link_path();
        match classify(&link, &bin) {
            CliCommandLinkState::NotInstalled => return Ok(build_status(app)),
            CliCommandLinkState::Stale | CliCommandLinkState::Conflict => {
                return Err(format!(
                    "{} 不是本 app 注册的符号链接（指向别处或为普通文件），拒绝删除",
                    link.display()
                ));
            }
            CliCommandLinkState::Installed => {}
        }
        match fs::remove_file(&link) {
            Ok(()) => {}
            Err(e) if e.kind() == io::ErrorKind::PermissionDenied => match remove_elevated(&link) {
                ElevateOutcome::Success => {}
                ElevateOutcome::Canceled(msg) => {
                    return Err(format!("已取消管理员授权，未删除（{msg}）"));
                }
                ElevateOutcome::Failed(msg) => return Err(format!("提权删除失败: {msg}")),
            },
            Err(e) => return Err(format!("删除符号链接失败: {e}")),
        }
        Ok(build_status(app))
    }

    /// setup 钩子：后台线程自动注册（幂等）。仅 NotInstalled 且本版本未拒绝提权时出手；
    /// Stale（指向别处）与 Conflict（用户自有文件）保持只读不动，避免误伤。
    pub fn init(app: &AppHandle) {
        let app = app.clone();
        thread::spawn(move || {
            let bin = match cli_bin_path(&app) {
                Ok(p) => p,
                Err(e) => {
                    log::warn!("[cli-register] 自动注册跳过: {e}");
                    return;
                }
            };
            let link = link_path();
            match classify(&link, &bin) {
                CliCommandLinkState::Installed => {
                    log::info!("[cli-register] {} 已注册", link.display());
                }
                CliCommandLinkState::Stale => {
                    log::info!(
                        "[cli-register] {} 指向别处（Stale），不自动改动；可调用 cli_link_install 修复",
                        link.display()
                    );
                }
                CliCommandLinkState::Conflict => {
                    log::warn!(
                        "[cli-register] {} 被同名普通文件占用（Conflict），不自动改动",
                        link.display()
                    );
                }
                CliCommandLinkState::NotInstalled => {
                    if is_declined_for_current_version(&app) {
                        log::info!(
                            "[cli-register] 本版本（{}）曾取消授权，跳过自动注册",
                            app.package_info().version
                        );
                        return;
                    }
                    match install(&app) {
                        Ok(_) => log::info!(
                            "[cli-register] 已注册命令 {} -> {}",
                            link.display(),
                            bin.display()
                        ),
                        Err(e) => log::warn!("[cli-register] 自动注册未完成: {e}"),
                    }
                }
            }
        });
    }
}

#[cfg(target_os = "macos")]
pub use imp::{init, install, status, uninstall};

// ---- 非 macOS：整体 no-op（命令仍需存在以保 specta/bindings 签名稳定） ----

#[cfg(not(target_os = "macos"))]
pub fn status(app: &AppHandle) -> CliCommandStatus {
    CliCommandStatus {
        state: CliCommandLinkState::NotInstalled,
        link_name: link_name().to_string(),
        link_path: link_path().to_string_lossy().to_string(),
        bin_path: cli_bin_path(app)
            .unwrap_or_default()
            .to_string_lossy()
            .to_string(),
        elevation_declined_version: declined_version(app),
    }
}

#[cfg(not(target_os = "macos"))]
pub fn init(_app: &AppHandle) {}

#[cfg(not(target_os = "macos"))]
pub fn install(_app: &AppHandle) -> Result<CliCommandStatus, String> {
    Err("CLI 命令注册仅支持 macOS".to_string())
}

#[cfg(not(target_os = "macos"))]
pub fn uninstall(_app: &AppHandle) -> Result<CliCommandStatus, String> {
    Err("CLI 命令注册仅支持 macOS".to_string())
}

// ---- IPC 命令（install/uninstall 含 osascript 密码框交互，可能长时间等待用户输入，
// 故 async + spawn_blocking 脱离主线程；status 纯文件元数据读取，同步即可） ----

/// 查询 CLI 命令注册状态（四态 + 路径 + 提权取消记录）。
#[tauri::command]
#[specta::specta]
pub fn cli_link_status(app: AppHandle) -> CliCommandStatus {
    status(&app)
}

/// 注册 CLI 命令（幂等）：/usr/local/bin 建 symlink，权限不足时弹管理员密码框。
#[tauri::command]
#[specta::specta]
pub async fn cli_link_install(app: AppHandle) -> Result<CliCommandStatus, String> {
    let res = tauri::async_runtime::spawn_blocking(move || install(&app))
        .await
        .map_err(|e| format!("CLI 注册任务执行失败: {e}"))??;
    Ok(res)
}

/// 移除 CLI 命令注册（幂等，仅删指向本 app 的 symlink）。
#[tauri::command]
#[specta::specta]
pub async fn cli_link_uninstall(app: AppHandle) -> Result<CliCommandStatus, String> {
    let res = tauri::async_runtime::spawn_blocking(move || uninstall(&app))
        .await
        .map_err(|e| format!("CLI 卸载任务执行失败: {e}"))??;
    Ok(res)
}
