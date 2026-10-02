// CLI 直启二进制解析（P5 定稿终态：vendored 自带 claude 全链路 SSOT）。
//
// 直启 token 一期仅 "claude" → 解析 vendored 自带 claude 二进制绝对路径：
//   <resources>/acp-adapters/<id>/<version>/node_modules/@anthropic-ai/claude-agent-sdk-<triple>/claude[.exe]
// resources 基准同 http_server.rs 口径（ensure_resources_base 装配期解析一次）：dev =
// 仓库 staging（CARGO_MANIFEST_DIR），release = BaseDirectory::Resource 打包资源目录
// （macOS = .app/Contents/Resources、Windows = exe 目录——Resource 基准非全平台 exe
// 目录，不可用 current_exe 拼接）。adapter id / version 目录
// 不硬编码（catalog pin 升级只动 Go 侧内嵌产物与 staging，不改 Rust）：全量扫描取
// semver 最高的含平台二进制条目。解析失败回落普通裸 shell——终端手动路径语义
// （用户 shell 自解析本机 claude）。其余 token（新 CLI 直启，TODO(P4)）暂不解析。
//
// login PATH 探测保留（PATH-only）：vendored claude 为平台原生二进制不依赖 node，
// 但 GUI app env 的 PATH 缺 nvm/volta 等目录，claude 派生的工具子进程（Bash 工具跑
// npm 等）需要用户环境——经 login+interactive shell 跑一次 `/usr/bin/env` 收割 PATH。
// 探测失败 best-effort 保留 app 继承 PATH（同 Go 侧 turnEnv 惯例），不阻断直启。
//
// 缓存：只缓存成功结果；失败不缓存（vendoring 就绪后无需重启 app 即生效）。
//
// login PATH 探测为同步子进程（同 claude_state.rs 的 ps 探测范式），rc 文件异常
// 拖慢/挂起的极端情形不设超时兜底——失败保留原 PATH 的语义保底可用性。

use std::path::{Path, PathBuf};
use std::process::Stdio;

/// 平台 triple（npm os/cpu 命名约定，同 Go agentcatalog.claudePlatformTriple 口径）。
/// musl 变体暂不区分（TODO 同 Go 侧随 Linux 分发面评估）。
pub(crate) fn platform_triple() -> Option<&'static str> {
    match (std::env::consts::OS, std::env::consts::ARCH) {
        ("macos", "aarch64") => Some("darwin-arm64"),
        ("macos", "x86_64") => Some("darwin-x64"),
        ("linux", "x86_64") => Some("linux-x64"),
        ("linux", "aarch64") => Some("linux-arm64"),
        ("windows", "x86_64") => Some("win32-x64"),
        ("windows", "aarch64") => Some("win32-arm64"),
        _ => None,
    }
}

/// 平台二进制文件名（npm 包 files 命名约定：Windows 带 .exe 后缀）。
pub(crate) fn claude_bin_name() -> &'static str {
    if std::env::consts::OS == "windows" {
        "claude.exe"
    } else {
        "claude"
    }
}

/// semver 三元组解析（严格 major.minor.patch 纯数字；npm 版本目录名形态）。
fn parse_semver(name: &str) -> Option<(u64, u64, u64)> {
    let mut parts = name.split('.');
    let v = (
        parts.next()?.parse().ok()?,
        parts.next()?.parse().ok()?,
        parts.next()?.parse().ok()?,
    );
    if parts.next().is_some() {
        return None;
    }
    Some(v)
}

/// login PATH 探测（进程级成功缓存）：login+interactive shell 带全量 rc 启动，
/// 毫秒到百毫秒级，成功值缓存复用；失败不缓存，下次 spawn 重探。
static LOGIN_PATH_CACHE: std::sync::LazyLock<std::sync::Mutex<Option<String>>> =
    std::sync::LazyLock::new(|| std::sync::Mutex::new(None));

/// vendored claude 路径解析成功缓存（只存 bin——login PATH 的缓存语义独立：成功
/// 走 LOGIN_PATH_CACHE，失败不固化、下次 spawn 重探）。vendored 版本随 app 发版
/// 固定，bin 进程内一次扫描足够；失败不落缓存。pub(crate) 供 local_provider 测试
/// 在注入 fake staging 前复位（进程级状态不跨测试漂移）。
pub(crate) static DIRECT_BIN_CACHE: std::sync::LazyLock<std::sync::Mutex<Option<String>>> =
    std::sync::LazyLock::new(|| std::sync::Mutex::new(None));

/// 装配期 vendored 资源源基准（ensure_resources_base 填充一次；None = release 解析
/// 失败，恒回落裸 shell）。
static RESOURCES_BASE: std::sync::OnceLock<Option<PathBuf>> = std::sync::OnceLock::new();

/// 装配期解析一次 vendored 资源源基准（pty_spawn 命令链持有 AppHandle，幂等）。
/// 口径同 http_server.rs：dev = 仓库 staging；release = BaseDirectory::Resource
/// （macOS 打包资源在 .app/Contents/Resources，与 exe 同 bundle 但不同目录）。
pub(crate) fn ensure_resources_base(app: &tauri::AppHandle) {
    use tauri::Manager;
    let resolved = if tauri::is_dev() {
        Some(Path::new(env!("CARGO_MANIFEST_DIR")).join("resources/acp-adapters"))
    } else {
        app.path()
            .resolve(
                "resources/acp-adapters",
                tauri::path::BaseDirectory::Resource,
            )
            .ok()
    };
    let _ = RESOURCES_BASE.set(resolved);
}

/// 测试注入点（仅测试构建）：fake staging 根，优先于装配期基准。测试进程不经
/// pty_spawn 命令链（RESOURCES_BASE 不会被填充），且不用进程 env 注入——并行
/// 测试下 set_var 与其它测试的 env 读取存在数据竞争（edition 2024 据此标 unsafe）。
#[cfg(test)]
pub(crate) static TEST_RESOURCES_ROOT: std::sync::LazyLock<std::sync::Mutex<Option<PathBuf>>> =
    std::sync::LazyLock::new(|| std::sync::Mutex::new(None));

/// login shell 环境收割：`$SHELL -l -i -c /usr/bin/env`（外部二进制，输出格式跨
/// shell 一致；stdin 关死——rc 脚本若读 stdin 会挂起探测进程）。
fn login_path() -> Option<String> {
    {
        let cache = LOGIN_PATH_CACHE
            .lock()
            .expect("cli_bin login path cache mutex poisoned");
        if let Some(hit) = cache.as_ref() {
            return Some(hit.clone());
        }
    }
    let shell = std::env::var("SHELL").unwrap_or_else(|_| "/bin/zsh".to_string());
    let out = std::process::Command::new(shell)
        .arg("-l")
        .arg("-i")
        .arg("-c")
        .arg("/usr/bin/env")
        .stdin(Stdio::null())
        .output()
        .ok()?;
    if !out.status.success() {
        return None;
    }
    let path = last_env_path(&String::from_utf8_lossy(&out.stdout))?;
    *LOGIN_PATH_CACHE
        .lock()
        .expect("cli_bin login path cache mutex poisoned") = Some(path.clone());
    Some(path)
}

/// 取输出中最后一条 `PATH=` 行的值（env 输出在脚本末尾，覆盖 rc 噪声可能的
/// `PATH=` 行）；缺失或空值 → None。
fn last_env_path(stdout: &str) -> Option<String> {
    stdout
        .lines()
        .rev()
        .find(|line| line.starts_with("PATH="))
        .map(|line| line["PATH=".len()..].trim().to_string())
        .filter(|p| !p.is_empty())
}

/// vendored 资源源根：测试注入（TEST_RESOURCES_ROOT）优先，否则取装配期基准
///（ensure_resources_base）；目录不存在 = vendoring 缺失（staging 未跑
/// server:acp:vendor / 打包资源缺失）→ None，调用方回落裸 shell。
fn acp_resources_root() -> Option<PathBuf> {
    #[cfg(test)]
    if let Some(root) = TEST_RESOURCES_ROOT
        .lock()
        .expect("cli_bin test resources root mutex poisoned")
        .clone()
    {
        return root.is_dir().then_some(root);
    }
    let root = RESOURCES_BASE.get().cloned()??;
    root.is_dir().then_some(root)
}

/// 单个 vendored 安装（installDir）内定位平台原生 claude 二进制并校验存在性
///（node_modules/@anthropic-ai/claude-agent-sdk-<triple>/<bin>，同 Go VendoredClaudeBin
/// 布局——SDK 自身对 CLAUDE_CODE_EXECUTABLE 缺省时的回落解析即同款布局）。
fn claude_bin_in(install_dir: &Path) -> Option<PathBuf> {
    let triple = platform_triple()?;
    let bin = install_dir
        .join("node_modules")
        .join("@anthropic-ai")
        .join(format!("claude-agent-sdk-{triple}"))
        .join(claude_bin_name());
    if bin.is_file() {
        Some(bin)
    } else {
        None
    }
}

/// 子目录全收（含过滤非目录项；目录不可读 = None，与「空」区分）。
fn subdirs(dir: &Path) -> Option<Vec<PathBuf>> {
    let entries = std::fs::read_dir(dir).ok()?;
    Some(
        entries
            .filter_map(|e| e.ok().map(|e| e.path()))
            .filter(|p| p.is_dir())
            .collect(),
    )
}

/// 扫描 <root>/<id>/<version>/：version 解析 semver 取最高且内含平台二进制的条目
/// （adapter id / version 不硬编码——catalog pin 升级不改 Rust；多 id 并存时按
/// 版本号全局取最高，一期单 adapter 语义等价）。
fn resolve_vendored_claude_in(root: &Path) -> Option<PathBuf> {
    let mut best: Option<((u64, u64, u64), PathBuf)> = None;
    for id in subdirs(root)? {
        for version in subdirs(&id)? {
            let Some(v) = version
                .file_name()
                .and_then(|n| n.to_str())
                .and_then(parse_semver)
            else {
                continue;
            };
            let Some(bin) = claude_bin_in(&version) else {
                continue;
            };
            if best.as_ref().map_or(true, |(bv, _)| v > *bv) {
                best = Some((v, bin));
            }
        }
    }
    best.map(|(_, bin)| bin)
}

/// 解析直启所需信息。返回 (vendored claude 绝对路径, login PATH best-effort)。
/// None = 不可直启（token 不在白名单 / vendored 资源缺失 / 平台无 SDK 二进制
/// 形态），调用方回落普通裸 shell。
pub(crate) fn resolve_direct_bin(token: &str) -> Option<(String, Option<String>)> {
    // 直启 token 白名单：一期 autoCommand 固定 claude；新 CLI 直启在此扩展（TODO(P4)）。
    if token != "claude" {
        return None;
    }
    // bin 进程内一次扫描（成功缓存，失败不缓存）；login PATH 每次现查——成功命中
    // LOGIN_PATH_CACHE 廉价，失败不固化、下次 spawn 重探（缓存只存 bin 的原因）。
    let bin = {
        let mut cache = DIRECT_BIN_CACHE
            .lock()
            .expect("cli_bin direct bin cache mutex poisoned");
        if let Some(hit) = cache.as_ref() {
            hit.clone()
        } else {
            let root = acp_resources_root()?;
            let bin = resolve_vendored_claude_in(&root)?
                .to_string_lossy()
                .into_owned();
            *cache = Some(bin.clone());
            bin
        }
    };
    Some((bin, login_path()))
}

#[cfg(test)]
mod tests {
    use super::*;

    /// semver 解析：标准三元组 / 多段 / 非数字 / 空段拒绝。
    #[test]
    fn parse_semver_rules() {
        assert_eq!(parse_semver("0.84.0"), Some((0, 84, 0)));
        assert_eq!(parse_semver("1.2.3"), Some((1, 2, 3)));
        assert_eq!(parse_semver("1.2"), None);
        assert_eq!(parse_semver("1.2.3.4"), None);
        assert_eq!(parse_semver("1.2.x"), None);
        assert_eq!(parse_semver("latest"), None);
        assert_eq!(parse_semver(""), None);
    }

    /// last PATH= 行胜出（覆盖 rc 噪声 PATH= 行）/ 缺失或空值 None。
    #[test]
    fn last_env_path_rules() {
        assert_eq!(
            last_env_path("PATH=/a:/b\nHOME=/x\nPATH=/usr/bin:/bin\n"),
            Some("/usr/bin:/bin".into())
        );
        assert_eq!(last_env_path("HOME=/x\n"), None);
        assert_eq!(last_env_path("PATH=\n"), None);
        assert_eq!(last_env_path(""), None);
    }

    /// token 白名单：一期仅 claude 可直启；其余（含绝对路径/注入形态）一律 None
    /// 且不发起任何探测。
    #[test]
    fn resolve_direct_bin_whitelist() {
        assert!(resolve_direct_bin("cat").is_none());
        assert!(resolve_direct_bin("echo").is_none());
        assert!(resolve_direct_bin("/bin/echo").is_none());
        assert!(resolve_direct_bin("cla';ude").is_none());
        assert!(resolve_direct_bin("").is_none());
    }

    /// vendored 扫描：多版本取 semver 最高且以「内含平台二进制」为入选前提；
    /// 非 semver 目录（.DS_Store 等）忽略；无任何合法条目 None。
    #[test]
    fn resolve_vendored_claude_in_rules() {
        let Some(triple) = platform_triple() else {
            // 无 SDK 二进制形态的平台：扫描恒 None。
            let root = std::env::temp_dir().join("cli-bin-no-triple");
            std::fs::create_dir_all(&root).unwrap();
            assert!(resolve_vendored_claude_in(&root).is_none());
            return;
        };
        let root = std::env::temp_dir().join(format!("cli-bin-scan-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&root);
        let sdk_dir = |id: &str, ver: &str| {
            root.join(id)
                .join(ver)
                .join("node_modules")
                .join("@anthropic-ai")
                .join(format!("claude-agent-sdk-{triple}"))
        };
        // 旧版本带二进制、新版本缺失二进制 → 只会命中旧版本（新版本不入选）。
        let old = sdk_dir("claude-acp", "0.83.0");
        std::fs::create_dir_all(&old).unwrap();
        std::fs::write(old.join(claude_bin_name()), "#!/bin/sh\n").unwrap();
        std::fs::create_dir_all(sdk_dir("claude-acp", "0.84.0")).unwrap();
        // 杂项目录（非 semver）+ 空的其它 id 条目。
        std::fs::create_dir_all(root.join("claude-acp").join(".DS_Store")).unwrap();
        std::fs::create_dir_all(root.join("other-agent").join("1.0.0")).unwrap();

        let got = resolve_vendored_claude_in(&root).expect("应命中 0.83.0 的二进制");
        assert!(
            got.ends_with(claude_bin_name()),
            "应定位到平台二进制: {}",
            got.display()
        );
        assert!(
            got.to_string_lossy().contains("0.83.0"),
            "缺失二进制的高版本不应入选: {}",
            got.display()
        );

        // 补齐新版本二进制 → semver 最高胜出。
        let new_bin = sdk_dir("claude-acp", "0.84.0").join(claude_bin_name());
        std::fs::write(&new_bin, "#!/bin/sh\n").unwrap();
        let got = resolve_vendored_claude_in(&root).expect("应命中 0.84.0");
        assert!(
            got.to_string_lossy().contains("0.84.0"),
            "got: {}",
            got.display()
        );

        let _ = std::fs::remove_dir_all(&root);
    }
}
