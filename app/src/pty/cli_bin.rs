/*
 * CLI 直启二进制解析（P5 定稿终态：vendored 自带 claude 全链路 SSOT）。直启 token 一期仅 "claude" → 解析 vendored 自带 claude 二进制绝对路径：<resources>/acp-adapters/<id>/<version>/node_modules/@anthropic-ai/claude-agent-sdk-<triple>/claude[.exe]。
 * resources 基准同 http_server.rs 口径（ensure_resources_base 装配期解析一次：dev = 仓库 staging，release = BaseDirectory::Resource 打包资源目录，非全平台 exe 目录不可用 current_exe 拼接）；adapter id/version 不硬编码（catalog pin 升级只动 Go 侧产物，不改 Rust）——全量扫描取 semver 最高的含平台二进制条目。
 * 解析失败回落普通裸 shell（用户 shell 自解析本机 claude）；login PATH 探测保留（PATH-only：GUI env PATH 缺 nvm/volta，经 login+interactive shell 收割，失败 best-effort 保留 app 继承 PATH 不阻断直启）；缓存只存成功结果，失败不缓存（vendoring 就绪后无需重启 app）。
 */
use std::path::{Path, PathBuf};
use std::process::Stdio;

/*
 * 平台 triple（npm os/cpu 命名约定，同 Go agentcatalog.claudePlatformTriple 口径）。
 * musl 变体暂不区分（TODO 同 Go 侧随 Linux 分发面评估）。
 */
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

// 平台二进制文件名（npm 包 files 命名约定：Windows 带 .exe 后缀）。
pub(crate) fn claude_bin_name() -> &'static str {
    if std::env::consts::OS == "windows" {
        "claude.exe"
    } else {
        "claude"
    }
}

// 平台 node 二进制文件名（同 Go nodeBinFromDirs 命名：Windows 带 .exe 后缀）。
fn node_bin_name() -> &'static str {
    if std::env::consts::OS == "windows" {
        "node.exe"
    } else {
        "node"
    }
}

// semver 三元组解析（严格 major.minor.patch 纯数字；npm 版本目录名形态）。
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

/*
 * login PATH 探测（进程级成功缓存）：login+interactive shell 带全量 rc 启动，
 * 毫秒到百毫秒级，成功值缓存复用；失败不缓存，下次 spawn 重探。
 */
static LOGIN_PATH_CACHE: std::sync::LazyLock<std::sync::Mutex<Option<String>>> =
    std::sync::LazyLock::new(|| std::sync::Mutex::new(None));

/*
 * vendored claude 路径解析成功缓存（只存 bin；vendored 版本随 app 发版固定，进程内一次扫描足够，失败不落缓存、下次 spawn 重探——login PATH 的缓存语义独立走 LOGIN_PATH_CACHE）。
 * pub(crate) 供 local_provider 测试在注入 fake staging 前复位（进程级状态不跨测试漂移）。
 */
pub(crate) static DIRECT_BIN_CACHE: std::sync::LazyLock<std::sync::Mutex<Option<String>>> =
    std::sync::LazyLock::new(|| std::sync::Mutex::new(None));

/*
 * vendored PATH 注入目录解析成功缓存（claude 目录 + node 目录；语义同
 * DIRECT_BIN_CACHE：失败不落缓存，vendoring 就绪后无需重启 app）。
 */
pub(crate) static VENDORED_DIRS_CACHE: std::sync::LazyLock<std::sync::Mutex<Option<Vec<String>>>> =
    std::sync::LazyLock::new(|| std::sync::Mutex::new(None));

/*
 * 装配期 vendored 资源源基准（ensure_resources_base 填充一次；None = release 解析
 * 失败，恒回落裸 shell）。
 */
static RESOURCES_BASE: std::sync::OnceLock<Option<PathBuf>> = std::sync::OnceLock::new();

/*
 * 装配期解析一次 vendored 资源源基准（pty_spawn 命令链持有 AppHandle，幂等）。
 * 口径同 http_server.rs：dev = 仓库 staging；release = BaseDirectory::Resource
 * （macOS 打包资源在 .app/Contents/Resources，与 exe 同 bundle 但不同目录）。
 */
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

/*
 * 测试注入点（仅测试构建）：fake staging 根，优先于装配期基准。测试进程不经
 * pty_spawn 命令链（RESOURCES_BASE 不会被填充），且不用进程 env 注入——并行
 * 测试下 set_var 与其它测试的 env 读取存在数据竞争（edition 2024 据此标 unsafe）。
 */
#[cfg(test)]
pub(crate) static TEST_RESOURCES_ROOT: std::sync::LazyLock<std::sync::Mutex<Option<PathBuf>>> =
    std::sync::LazyLock::new(|| std::sync::Mutex::new(None));

/*
 * TEST_RESOURCES_ROOT 单槽注入互斥（仅测试构建）：多个测试并发注入会互相覆盖，
 * 解析窗口内扫到他人 root。持锁者独占整个「注入 → spawn/解析 → 断言 → 复位」窗口。
 */
#[cfg(test)]
pub(crate) static TEST_ROOT_GUARD: std::sync::LazyLock<std::sync::Mutex<()>> =
    std::sync::LazyLock::new(|| std::sync::Mutex::new(()));

/*
 * login shell 环境收割：`$SHELL -l -i -c /usr/bin/env`（外部二进制，输出格式跨
 * shell 一致；stdin 关死——rc 脚本若读 stdin 会挂起探测进程）。
 */
fn login_path() -> Option<String> {
    // Windows 无 $SHELL login 探测语义：GUI 进程本继承注册表 PATH，跳过免每次直启空跑失败进程。
    if cfg!(windows) {
        return None;
    }
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

/*
 * 取输出中最后一条 `PATH=` 行的值（env 输出在脚本末尾，覆盖 rc 噪声可能的
 * `PATH=` 行）；缺失或空值 → None。
 */
fn last_env_path(stdout: &str) -> Option<String> {
    stdout
        .lines()
        .rev()
        .find(|line| line.starts_with("PATH="))
        .map(|line| line["PATH=".len()..].trim().to_string())
        .filter(|p| !p.is_empty())
}

/*
 * vendored 资源源根：测试注入（TEST_RESOURCES_ROOT）优先，否则取装配期基准
 *（ensure_resources_base）；目录不存在 = vendoring 缺失（staging 未跑
 * server:vendor:prepare / 打包资源缺失）→ None，调用方回落裸 shell。
 */
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

/*
 * 单个 vendored 安装（installDir）内定位平台原生 claude 二进制并校验存在性
 *（node_modules/@anthropic-ai/claude-agent-sdk-<triple>/<bin>，同 Go VendoredClaudeBin
 * 布局——SDK 自身对 CLAUDE_CODE_EXECUTABLE 缺省时的回落解析即同款布局）。
 */
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

// 子目录全收（含过滤非目录项；目录不可读 = None，与「空」区分）。
fn subdirs(dir: &Path) -> Option<Vec<PathBuf>> {
    let entries = std::fs::read_dir(dir).ok()?;
    Some(
        entries
            .filter_map(|e| e.ok().map(|e| e.path()))
            .filter(|p| p.is_dir())
            .collect(),
    )
}

/*
 * 扫描 <root>/<id>/<version>/：version 解析 semver 取最高且内含平台二进制的条目
 * （adapter id / version 不硬编码——catalog pin 升级不改 Rust；多 id 并存时按
 * 版本号全局取最高，一期单 adapter 语义等价）。
 */
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

/*
 * 扫描 <root>/runtime/node/<semver>/：取版本最高且内含平台 node 二进制的目录
 * （布局同 Go embeddedNodeBin，node 运行时随包分发见 vendor-node-runtime.ts）。
 */
fn resolve_vendored_node_dir_in(root: &Path) -> Option<PathBuf> {
    let version_dir = root.join("runtime").join("node");
    let mut best: Option<((u64, u64, u64), PathBuf)> = None;
    for version in subdirs(&version_dir)? {
        let Some(v) = version
            .file_name()
            .and_then(|n| n.to_str())
            .and_then(parse_semver)
        else {
            continue;
        };
        if !version.join(node_bin_name()).is_file() {
            continue;
        }
        if best.as_ref().map_or(true, |(bv, _)| v > *bv) {
            best = Some((v, version));
        }
    }
    best.map(|(_, dir)| dir)
}

/*
 * vendored PATH 注入目录（先 claude 后 node）：任一命中即返回命中集合，全缺失返回
 * None 不注入。claude 目录即二进制所在目录（shell 据此解析裸 claude 命令）。
 */
pub(crate) fn vendored_path_dirs() -> Option<Vec<String>> {
    {
        let cache = VENDORED_DIRS_CACHE
            .lock()
            .expect("cli_bin vendored dirs cache mutex poisoned");
        if let Some(hit) = cache.as_ref() {
            return Some(hit.clone());
        }
    }
    let root = acp_resources_root()?;
    let mut dirs: Vec<String> = Vec::new();
    if let Some(bin) = resolve_vendored_claude_in(&root) {
        if let Some(dir) = bin.parent() {
            dirs.push(dir.to_string_lossy().into_owned());
        }
    }
    if let Some(dir) = resolve_vendored_node_dir_in(&root) {
        dirs.push(dir.to_string_lossy().into_owned());
    }
    if dirs.is_empty() {
        return None;
    }
    *VENDORED_DIRS_CACHE
        .lock()
        .expect("cli_bin vendored dirs cache mutex poisoned") = Some(dirs.clone());
    Some(dirs)
}

/*
 * 子进程 PATH 同源装配（裸 shell / direct 两分支统一）：vendored 目录前置 → login
 * PATH → 继承 PATH，保序去重。None = 无任何可注入成分（保持继承 PATH）。
 */
pub(crate) fn assembled_child_path() -> Option<String> {
    let sep = if cfg!(windows) {
        ";"
    } else {
        ":"
    };
    let mut parts: Vec<String> = Vec::new();
    if let Some(dirs) = vendored_path_dirs() {
        parts.extend(dirs);
    }
    if let Some(login) = login_path() {
        parts.push(login);
    }
    if let Ok(inherited) = std::env::var("PATH") {
        parts.push(inherited);
    }
    if parts.is_empty() {
        return None;
    }
    let mut entries: Vec<String> = Vec::new();
    let mut seen: std::collections::HashSet<String> = std::collections::HashSet::new();
    for part in &parts {
        for entry in part.split(sep) {
            if !entry.is_empty() && seen.insert(entry.to_string()) {
                entries.push(entry.to_string());
            }
        }
    }
    Some(entries.join(sep))
}

/*
 * 解析直启 vendored claude 绝对路径。
 * None = 不可直启（token 不在白名单 / vendored 资源缺失 / 平台无 SDK 二进制
 * 形态），调用方回落普通裸 shell。
 */
pub(crate) fn resolve_direct_bin(token: &str) -> Option<String> {
    // 直启 token 白名单：一期 autoCommand 固定 claude；新 CLI 直启在此扩展（TODO(P4)）。
    if token != "claude" {
        return None;
    }
    // bin 进程内一次扫描（成功缓存，失败不缓存）。
    let mut cache = DIRECT_BIN_CACHE
        .lock()
        .expect("cli_bin direct bin cache mutex poisoned");
    if let Some(hit) = cache.as_ref() {
        return Some(hit.clone());
    }
    let root = acp_resources_root()?;
    let bin = resolve_vendored_claude_in(&root)?
        .to_string_lossy()
        .into_owned();
    *cache = Some(bin.clone());
    Some(bin)
}

#[cfg(test)]
mod tests {
    use super::*;

    // semver 解析：标准三元组 / 多段 / 非数字 / 空段拒绝。
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

    // last PATH= 行胜出（覆盖 rc 噪声 PATH= 行）/ 缺失或空值 None。
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

    /*
     * token 白名单：一期仅 claude 可直启；其余（含绝对路径/注入形态）一律 None
     * 且不发起任何探测。
     */
    #[test]
    fn resolve_direct_bin_whitelist() {
        assert!(resolve_direct_bin("cat").is_none());
        assert!(resolve_direct_bin("echo").is_none());
        assert!(resolve_direct_bin("/bin/echo").is_none());
        assert!(resolve_direct_bin("cla';ude").is_none());
        assert!(resolve_direct_bin("").is_none());
    }

    /*
     * vendored 扫描：多版本取 semver 最高且以「内含平台二进制」为入选前提；
     * 非 semver 目录（.DS_Store 等）忽略；无任何合法条目 None。
     */
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

    /*
     * node 目录扫描：版本最高且平台二进制在位者入选，缺失二进制的
     * 高版本不入选（口径同 claude 扫描）。
     */
    #[test]
    fn resolve_vendored_node_dir_in_rules() {
        let root = std::env::temp_dir().join(format!(
            "cli-bin-node-scan-{}",
            std::process::id()
        ));
        let _ = std::fs::remove_dir_all(&root);
        let old = root.join("runtime").join("node").join("24.17.0");
        std::fs::create_dir_all(&old).unwrap();
        std::fs::write(old.join(node_bin_name()), "#!/bin/sh\n").unwrap();
        // 24.18.0 目录存在但缺二进制 → 不入选，命中 24.17.0。
        std::fs::create_dir_all(root.join("runtime").join("node").join("24.18.0")).unwrap();
        let got = resolve_vendored_node_dir_in(&root).expect("应命中 24.17.0");
        assert!(
            got.to_string_lossy().contains("24.17.0"),
            "缺失二进制的高版本不应入选: {}",
            got.display()
        );
        // 补齐二进制 → semver 最高胜出。
        std::fs::write(
            root.join("runtime")
                .join("node")
                .join("24.18.0")
                .join(node_bin_name()),
            "#!/bin/sh\n",
        )
        .unwrap();
        let got = resolve_vendored_node_dir_in(&root).expect("应命中 24.18.0");
        assert!(
            got.to_string_lossy().contains("24.18.0"),
            "got: {}",
            got.display()
        );
        let _ = std::fs::remove_dir_all(&root);
    }

    /*
     * vendored_path_dirs：fake staging 双条目命中返回 [claude 目录, node 目录]；
     * 全缺失返回 None。
     */
    #[test]
    fn vendored_path_dirs_rules() {
        let _guard = TEST_ROOT_GUARD
            .lock()
            .expect("cli_bin test root guard mutex poisoned");
        let Some(triple) = platform_triple() else {
            return;
        };
        let root = std::env::temp_dir().join(format!(
            "cli-bin-vendored-dirs-{}",
            std::process::id()
        ));
        let _ = std::fs::remove_dir_all(&root);
        let claude_dir = root
            .join("claude-acp")
            .join("0.84.0")
            .join("node_modules")
            .join("@anthropic-ai")
            .join(format!("claude-agent-sdk-{triple}"));
        std::fs::create_dir_all(&claude_dir).unwrap();
        std::fs::write(claude_dir.join(claude_bin_name()), "#!/bin/sh\n").unwrap();
        let node_dir = root.join("runtime").join("node").join("24.18.0");
        std::fs::create_dir_all(&node_dir).unwrap();
        std::fs::write(node_dir.join(node_bin_name()), "#!/bin/sh\n").unwrap();

        *VENDORED_DIRS_CACHE
            .lock()
            .expect("cli_bin vendored dirs cache mutex poisoned") = None;
        *TEST_RESOURCES_ROOT
            .lock()
            .expect("cli_bin test resources root mutex poisoned") = Some(root.clone());
        let dirs = vendored_path_dirs().expect("双目录应命中");
        assert_eq!(dirs.len(), 2, "dirs: {dirs:?}");
        assert!(
            dirs[0].contains(&format!("claude-agent-sdk-{triple}")),
            "首个应为 claude 目录: {dirs:?}"
        );
        assert!(
            dirs[1].contains("24.18.0"),
            "次个应为 node 目录: {dirs:?}"
        );

        // 全缺失（空根）→ None。
        let empty_root = std::env::temp_dir().join(format!(
            "cli-bin-vendored-empty-{}",
            std::process::id()
        ));
        std::fs::create_dir_all(&empty_root).unwrap();
        *VENDORED_DIRS_CACHE
            .lock()
            .expect("cli_bin vendored dirs cache mutex poisoned") = None;
        *TEST_RESOURCES_ROOT
            .lock()
            .expect("cli_bin test resources root mutex poisoned") = Some(empty_root.clone());
        assert!(
            vendored_path_dirs().is_none(),
            "全缺失应返回 None"
        );

        // 收尾复位（进程级状态不跨测试漂移）。
        *TEST_RESOURCES_ROOT
            .lock()
            .expect("cli_bin test resources root mutex poisoned") = None;
        *VENDORED_DIRS_CACHE
            .lock()
            .expect("cli_bin vendored dirs cache mutex poisoned") = None;
        let _ = std::fs::remove_dir_all(&root);
        let _ = std::fs::remove_dir_all(&empty_root);
    }
}
