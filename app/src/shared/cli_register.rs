// ocean-harness CLI 命令注册：把随包 sidecar 的 cli 二进制 symlink 到 ~/.local/bin，
// 让任意终端可直接执行 `ocean-harness-cli xxx`（release）/ `ocean-harness-dev-cli xxx`（debug）。
//
// 注册模型（参照 orca 四态状态机）：
//   classify 用 symlink_metadata + canonicalize 判定四态——Installed（symlink 指向本 app 的
//   cli_bin）/ Stale（symlink 指向别处，含悬空）/ Conflict（同名普通文件，用户自有文件，拒碰）/
//   NotInstalled。只在 NotInstalled/Stale 上操作，Conflict 永不改写。
//
// 免密设计：落点 ~/.local/bin 为用户级可写目录，建链/删除全程纯 fs 操作、无需提权
// （历史版本曾 symlink 到 root 属主的 /usr/local/bin 并经 osascript 弹管理员密码框，已废弃；
// 升级用户如存留 root 属主旧链接，需手动 sudo rm 一次）。PATH 依赖经 ~/.zshrc 幂等注入
// （marker 注释 + export PATH 行，ensure_path_line/remove_path_line，有单测），卸载时按
// marker 精确摘除、不碰用户自写的 PATH 行；fish 等其他 shell 需手动把 ~/.local/bin
// 加入 PATH。
//
// 生命周期：init 在 setup 末尾后台线程幂等执行（仅 NotInstalled 时自动装，免密故静默无
// 打扰；Stale/Conflict 只留日志不动手）；install/uninstall 以 IPC 命令暴露供前端/后续 bot
// 调用。非 macOS 平台整体 no-op（Windows 注册进用户级 PATH 属后续工作，同样免密）。

use std::path::{Path, PathBuf};

use tauri::AppHandle;

use crate::shared::types::{CliCommandLinkState, CliCommandStatus};

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

/// 用户主目录（HOME 环境变量；守护级缺失视为环境异常）。
fn home_dir() -> Result<PathBuf, String> {
    std::env::var_os("HOME")
        .map(PathBuf::from)
        .filter(|p| !p.as_os_str().is_empty())
        .ok_or_else(|| "未获取到 HOME 环境变量，无法定位用户目录".to_string())
}

/// symlink 落点目录：~/.local/bin（用户级可写，免提权）。
fn bin_dir() -> Result<PathBuf, String> {
    Ok(home_dir()?.join(".local").join("bin"))
}

/// symlink 完整路径。
fn link_path() -> Result<PathBuf, String> {
    Ok(bin_dir()?.join(link_name()))
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

// ---- PATH 注入/摘除（幂等，marker 精确配对） ----

/// 注入块的 marker 注释行：幂等判重与卸载摘除的唯一依据。
const PATH_MARKER: &str = "# added by Ocean Harness CLI registration";

/// 注入的 export 行（与 marker 成对写入）。普通转义字符串——裸字符串 r#"…"# 的终止
/// 序列 `"#` 会吞掉本行 shell 语法的收尾引号（内容以 `$PATH"` 结尾恰好命中），已踩坑。
const PATH_EXPORT_LINE: &str = "export PATH=\"$HOME/.local/bin:$PATH\"";

/// PATH 注入目标：~/.zshrc（zsh 交互 shell 通用读取，Terminal.app/iTerm2/VS Code 终端
/// 一致，与终端 App 无关）。
fn zshrc_path() -> Result<PathBuf, String> {
    Ok(home_dir()?.join(".zshrc"))
}

/// 幂等注入 PATH 块：文件缺失则创建；已含 marker 则跳过；否则尾部追加
/// marker 注释 + export PATH 两行（用户文件末行无换行时先补换行，避免拼接）。
fn ensure_path_line(rc: &Path) -> Result<(), String> {
    let existing = match std::fs::read_to_string(rc) {
        Ok(s) => s,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => String::new(),
        Err(e) => return Err(format!("读取 {} 失败: {e}", rc.display())),
    };
    if existing.contains(PATH_MARKER) {
        return Ok(());
    }
    let mut out = existing;
    if !out.is_empty() && !out.ends_with('\n') {
        out.push('\n');
    }
    out.push_str(PATH_MARKER);
    out.push('\n');
    out.push_str(PATH_EXPORT_LINE);
    out.push('\n');
    std::fs::write(rc, out).map_err(|e| format!("写入 {} 失败: {e}", rc.display()))
}

/// 按 marker 摘除注入块（marker 行及其后紧邻的 export 行成对删除，可多对；
/// 用户自写的其他 PATH 行原样保留）。文件缺失视为已摘除。
fn remove_path_line(rc: &Path) -> Result<(), String> {
    let existing = match std::fs::read_to_string(rc) {
        Ok(s) => s,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(e) => return Err(format!("读取 {} 失败: {e}", rc.display())),
    };
    if !existing.contains(PATH_MARKER) {
        return Ok(());
    }
    let mut kept: Vec<&str> = Vec::new();
    let mut lines = existing.lines().peekable();
    while let Some(line) = lines.next() {
        if line.trim_end() == PATH_MARKER {
            // 成对摘除：marker 后紧邻的 export 行一并跳过。
            if lines
                .peek()
                .is_some_and(|next| *next == PATH_EXPORT_LINE)
            {
                lines.next();
            }
            continue;
        }
        kept.push(line);
    }
    let mut out = kept.join("\n");
    if !out.is_empty() {
        out.push('\n');
    }
    std::fs::write(rc, out).map_err(|e| format!("写入 {} 失败: {e}", rc.display()))
}

// ---- macOS 实现（纯 fs，免提权） ----

#[cfg(target_os = "macos")]
mod imp {
    use std::fs;
    use std::os::unix::fs::symlink;
    use std::thread;

    use super::*;

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
            state: classify(&link_path().unwrap_or_default(), &bin),
            link_name: link_name().to_string(),
            link_path: link_path()
                .unwrap_or_default()
                .to_string_lossy()
                .to_string(),
            bin_path: bin.to_string_lossy().to_string(),
        }
    }

    pub fn status(app: &AppHandle) -> CliCommandStatus {
        build_status(app)
    }

    /// 确保命令已注册（幂等，纯 fs 免提权）。顺序：建链成功后再注入 PATH（失败不污染 rc）；
    /// PATH 注入失败时报「已注册但需手动加 PATH」，不回滚建链。
    pub fn install(app: &AppHandle) -> Result<CliCommandStatus, String> {
        let bin = cli_bin_path(app)?;
        if fs::symlink_metadata(&bin).is_err() {
            return Err(format!(
                "CLI 二进制不存在: {}（dev 模式请先执行 pnpm server:test 构建产物）",
                bin.display()
            ));
        }
        let dir = bin_dir()?;
        let link = dir.join(link_name());
        match classify(&link, &bin) {
            CliCommandLinkState::Conflict => {
                return Err(format!(
                    "{} 已存在且不是符号链接（可能是用户自有文件），拒绝覆盖；请手动处理后重试",
                    link.display()
                ));
            }
            CliCommandLinkState::Installed => return Ok(build_status(app)),
            // 旧 symlink 指向别处：先摘掉再建链。摘除失败（如 root 属主的历史残留）
            // 不盲目继续——用户级目录里不该出现摘不掉的条目，报错并给手动清理指引。
            CliCommandLinkState::Stale => {
                if let Err(e) = fs::remove_file(&link) {
                    return Err(format!(
                        "旧符号链接 {} 无法移除（{e}）；若为 root 属主的历史残留，请手动 sudo rm 后重试",
                        link.display()
                    ));
                }
            }
            CliCommandLinkState::NotInstalled => {}
        }
        fs::create_dir_all(&dir).map_err(|e| format!("创建 {} 失败: {e}", dir.display()))?;
        symlink(&bin, &link).map_err(|e| format!("创建符号链接失败: {e}"))?;
        if let Err(e) = ensure_path_line(&zshrc_path()?) {
            return Err(format!(
                "命令已注册（{} -> {}），但 PATH 注入 ~/.zshrc 失败: {e}；请手动执行 {} 后重开终端",
                link.display(),
                bin.display(),
                PATH_EXPORT_LINE
            ));
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

    /// 移除本 app 注册的命令（幂等）。仅删除指向本 app cli_bin 的 symlink + 按 marker
    /// 摘除 PATH 注入块；Stale/Conflict 拒删——symlink 指向别处或同名普通文件都不是
    /// 本 app 的产物。PATH 摘除失败不阻断卸载（链接已删，残留两行注释无害，留日志）。
    pub fn uninstall(app: &AppHandle) -> Result<CliCommandStatus, String> {
        let bin = cli_bin_path(app)?;
        let link = link_path()?;
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
        fs::remove_file(&link).map_err(|e| format!("删除符号链接失败: {e}"))?;
        if let Err(e) = remove_path_line(&zshrc_path()?) {
            log::warn!("[cli-register] ~/.zshrc PATH 块摘除失败（无害残留）: {e}");
        }
        Ok(build_status(app))
    }

    /// setup 钩子：后台线程自动注册（幂等、免密故静默无打扰）。仅 NotInstalled 时出手；
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
            let link = match link_path() {
                Ok(p) => p,
                Err(e) => {
                    log::warn!("[cli-register] 自动注册跳过: {e}");
                    return;
                }
            };
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
                CliCommandLinkState::NotInstalled => match install(&app) {
                    Ok(_) => log::info!(
                        "[cli-register] 已注册命令 {} -> {}",
                        link.display(),
                        bin.display()
                    ),
                    Err(e) => log::warn!("[cli-register] 自动注册未完成: {e}"),
                },
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
        link_path: link_path()
            .unwrap_or_default()
            .to_string_lossy()
            .to_string(),
        bin_path: cli_bin_path(app)
            .unwrap_or_default()
            .to_string_lossy()
            .to_string(),
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

// ---- IPC 命令（纯 fs 操作毫秒级完成，同步命令即可；osascript 提权交互已随免密设计移除） ----

/// 查询 CLI 命令注册状态（四态 + 路径）。
#[tauri::command]
#[specta::specta]
pub fn cli_link_status(app: AppHandle) -> CliCommandStatus {
    status(&app)
}

/// 注册 CLI 命令（幂等）：~/.local/bin 建 symlink + ~/.zshrc 注入 PATH，全程免提权。
#[tauri::command]
#[specta::specta]
pub fn cli_link_install(app: AppHandle) -> Result<CliCommandStatus, String> {
    install(&app)
}

/// 移除 CLI 命令注册（幂等，仅删指向本 app 的 symlink + 摘除 PATH 注入块）。
#[tauri::command]
#[specta::specta]
pub fn cli_link_uninstall(app: AppHandle) -> Result<CliCommandStatus, String> {
    uninstall(&app)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// 测试专用 rc 文件：tempdir 下唯一命名，测试结束自行清理由 temp_dir 惯例兜底。
    fn test_rc(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!(
            "ocean-cli-register-{}-{}",
            std::process::id(),
            name
        ));
        std::fs::create_dir_all(&dir).unwrap();
        dir.join("zshrc")
    }

    #[test]
    fn ensure_file_missing_creates_block() {
        let rc = test_rc("create");
        let _ = std::fs::remove_file(&rc);
        ensure_path_line(&rc).unwrap();
        let content = std::fs::read_to_string(&rc).unwrap();
        assert!(content.contains(PATH_MARKER));
        assert!(content.contains(PATH_EXPORT_LINE));
    }

    #[test]
    fn export_line_is_well_formed_shell() {
        // 钉死精确值：曾因裸字符串终止符 `"#` 吞掉收尾引号，注入了未闭合的 shell 行。
        assert_eq!(
            PATH_EXPORT_LINE,
            "export PATH=\"$HOME/.local/bin:$PATH\""
        );
    }

    #[test]
    fn ensure_idempotent_no_duplicate() {
        let rc = test_rc("idempotent");
        let _ = std::fs::remove_file(&rc);
        ensure_path_line(&rc).unwrap();
        let once = std::fs::read_to_string(&rc).unwrap();
        ensure_path_line(&rc).unwrap();
        let twice = std::fs::read_to_string(&rc).unwrap();
        assert_eq!(once, twice, "重复注入不得产生第二份块");
    }

    #[test]
    fn ensure_appends_after_user_lines_with_newline_fix() {
        let rc = test_rc("append");
        let _ = std::fs::remove_file(&rc);
        std::fs::write(&rc, "export EDITOR=vim").unwrap(); // 末行无换行
        ensure_path_line(&rc).unwrap();
        let content = std::fs::read_to_string(&rc).unwrap();
        assert!(
            content.starts_with("export EDITOR=vim\n"),
            "先补换行再拼接"
        );
        assert!(content.contains(PATH_EXPORT_LINE));
    }

    #[test]
    fn remove_keeps_user_lines_and_is_idempotent() {
        let rc = test_rc("remove");
        let _ = std::fs::remove_file(&rc);
        std::fs::write(
            &rc,
            "export EDITOR=vim\nexport PATH=$HOME/bin:$PATH\n",
        )
        .unwrap();
        ensure_path_line(&rc).unwrap();
        remove_path_line(&rc).unwrap();
        let content = std::fs::read_to_string(&rc).unwrap();
        assert_eq!(
            content, "export EDITOR=vim\nexport PATH=$HOME/bin:$PATH\n",
            "只摘注入块，用户自写行原样保留"
        );
        remove_path_line(&rc).unwrap(); // 再摘一次：无 marker 幂等
        assert_eq!(std::fs::read_to_string(&rc).unwrap(), content);
    }

    #[test]
    fn remove_missing_file_is_ok() {
        let rc = test_rc("remove-missing");
        let _ = std::fs::remove_file(&rc);
        remove_path_line(&rc).unwrap();
    }
}
