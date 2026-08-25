// hook 脚本生成（T1.2）：claude hooks → append spool JSONL 的 /bin/sh 脚本。
//
// 模板行序是 orca #11549/#8110 契约，不可调换：
//   1. env guard 必须在读 stdin 之前——无标（外部终端）路径不读即退，
//      防 claude 向无人消费的 stdin 写载荷时管道挂起；
//   2. stdin 读取用 `command -p cat`（shell 内建默认 PATH）——防 PATH 被
//      剥后 exit 127（#8110）、防 repo-local cat 劫持载荷；
//   3. append 失败静默（claude 无感，不因 hook 报错阻塞工具链）。
// 脚本本体落 app_data_dir/claude-hooks/hook.sh，由 ensure_workspace_hooks
// 命令按需落盘（未启用 chat 模式则零文件产生）。

use std::path::{Path, PathBuf};

/// 脚本文件名（settings.json 自有条目识别 needle 的一部分）。
pub const HOOK_SCRIPT_FILE_NAME: &str = "hook.sh";

/// 脚本目录名（app_data_dir 下）。
pub const HOOK_SCRIPT_DIR_NAME: &str = "claude-hooks";

/// 生成 hook 脚本内容。无参数插值（spool 路径/pane 全部经 env 运行时传入），
/// 因此内容恒定、内容相同跳过天然成立。
pub fn hook_script_content() -> String {
    [
        "#!/bin/sh",
        "# we-claude-terminal claude hook：载荷 append spool（env guard 在读 stdin 之前，行序不可调换）",
        // env guard：无标路径（iTerm2 等外部终端跑的 claude）不读即退。
        "[ -z \"$WE_TERM_SPOOL_DIR\" ] && exit 0",
        // stdin 读取（#8110）：command -p 走 shell 内建默认 PATH，剥 PATH/劫持 cat 都不影响。
        "payload=$({ command -p cat 2>/dev/null || cat; })",
        "[ -z \"$payload\" ] && exit 0",
        // append：pane 锚点即文件名（:: sanitize 为 __，防路径段歧义）；失败静默。
        "printf '%s\\n' \"$payload\" >> \"$WE_TERM_SPOOL_DIR/${WE_TERM_PANE//::/__}.jsonl\" 2>/dev/null || exit 0",
        "exit 0",
        "",
    ]
    .join("\n")
}

/// settings.json 里 hook command 的守卫包裹形态（对齐 orca wrapPosixHookCommand）：
/// 脚本缺失/不可执行时静默 no-op + drain stdin——claude transcript 不报
/// non-blocking hook error，升级换路径后的陈旧条目也无害。
pub fn hook_command(script_path: &Path) -> String {
    let quoted = quote_posix_single(&script_path.to_string_lossy());
    format!(
        "if [ -f {quoted} ] && [ -r {quoted} ] && [ -x {quoted} ]; then /bin/sh {quoted}; else {{ command -p cat 2>/dev/null || cat; }} >/dev/null 2>&1 || :; fi"
    )
}

/// POSIX 单引号转义：'…' 内除单引号外全字面（' → '\'' 关闭-转义-重开）。
/// 同款实现在 pty/shell_ready.rs（私有）；此处独立副本避免跨域 pub 化。
fn quote_posix_single(value: &str) -> String {
    format!("'{}'", value.replace('\'', "'\\''"))
}

/// 幂等落盘脚本（base_dir/claude-hooks/hook.sh）：内容相同跳过；否则
/// temp+rename 原子写，chmod 755 在 rename 之前（对齐 orca writeManagedScript
/// ——canonical 路径永不以不可执行形态暴露，守卫包裹不会因此跳过）。
pub fn ensure_hook_script(base_dir: &Path) -> Result<PathBuf, String> {
    let dir = base_dir.join(HOOK_SCRIPT_DIR_NAME);
    let path = dir.join(HOOK_SCRIPT_FILE_NAME);
    let content = hook_script_content();

    std::fs::create_dir_all(&dir).map_err(|e| format!("mkdir {} failed: {e}", dir.display()))?;

    if let Ok(existing) = std::fs::read_to_string(&path) {
        if existing == content {
            // 已在场且内容一致：确保可执行位（用户可能手动 chmod 过）后直接复用。
            make_executable(&path)?;
            return Ok(path);
        }
    }

    // tmp 名带 pid+纳秒（对齐 orca randomUUID 防碰撞）：同毫秒并发 install 不互踩。
    let tmp = dir.join(format!(
        ".{}.{}.{}.tmp",
        HOOK_SCRIPT_FILE_NAME,
        std::process::id(),
        nanos_suffix()
    ));
    std::fs::write(&tmp, &content).map_err(|e| format!("write {} failed: {e}", tmp.display()))?;
    make_executable(&tmp)?;
    std::fs::rename(&tmp, &path).map_err(|e| format!("rename {} failed: {e}", path.display()))?;
    Ok(path)
}

/// chmod 755（unix）。失败即整体失败——守卫包裹依赖 -x 位。
#[cfg(unix)]
fn make_executable(path: &Path) -> Result<(), String> {
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o755))
        .map_err(|e| format!("chmod {} failed: {e}", path.display()))
}

/// 纳秒时戳（tmp 文件名后缀用，并发防碰撞）。pub(crate) 供 installer 复用。
pub(crate) fn nanos_suffix() -> u128 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0)
}

#[cfg(not(unix))]
fn make_executable(_path: &Path) -> Result<(), String> {
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    /// env guard 必须在 stdin 读取之前（#11549）：断言行序，防模板重构时调换。
    #[test]
    fn env_guard_precedes_stdin_read() {
        let binding = hook_script_content();
        let lines: Vec<&str> = binding.lines().collect();
        let guard = lines
            .iter()
            .position(|l| l.contains("WE_TERM_SPOOL_DIR"))
            .expect("env guard line present");
        let stdin_read = lines
            .iter()
            .position(|l| l.contains("command -p cat"))
            .expect("stdin read line present");
        assert!(
            guard < stdin_read,
            "guard must precede stdin read"
        );
    }

    #[test]
    fn script_content_is_stable() {
        // 内容恒定（无插值）——幂等安装的内容相同跳过依赖此性质。
        assert_eq!(hook_script_content(), hook_script_content());
        assert!(hook_script_content().contains("#!/bin/sh"));
        // pane 文件名 sanitize 在脚本内完成。
        assert!(hook_script_content().contains("${WE_TERM_PANE//::/__}.jsonl"));
    }

    #[test]
    fn hook_command_wraps_with_guard_and_escapes_quotes() {
        let cmd = hook_command(Path::new("/opt/app data/hook.sh"));
        assert!(cmd.starts_with("if [ -f '/opt/app data/hook.sh' ]"));
        assert!(cmd.contains("/bin/sh '/opt/app data/hook.sh'"));
        assert!(cmd.ends_with("fi"));

        // 路径含单引号：'\'' 转义后守卫与执行段仍是同一字面路径。
        let tricky = hook_command(Path::new("/tmp/it's.sh"));
        assert!(
            tricky.contains("'/tmp/it'\\''s.sh'"),
            "got: {tricky}"
        );
    }

    #[test]
    fn ensure_hook_script_idempotent_and_executable() {
        let dir = std::env::temp_dir().join("claude-runtime-hook-script-test");
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();

        let path = ensure_hook_script(&dir).unwrap();
        assert_eq!(
            path,
            dir.join(HOOK_SCRIPT_DIR_NAME)
                .join(HOOK_SCRIPT_FILE_NAME)
        );
        assert_eq!(
            std::fs::read_to_string(&path).unwrap(),
            hook_script_content()
        );

        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = std::fs::metadata(&path)
                .unwrap()
                .permissions()
                .mode();
            assert_eq!(mode & 0o111, 0o111, "script must be executable");
        }

        // 二次调用：内容相同跳过（无 tmp 残留）、路径不变。
        let again = ensure_hook_script(&dir).unwrap();
        assert_eq!(again, path);
        let entries: Vec<_> = std::fs::read_dir(dir.join(HOOK_SCRIPT_DIR_NAME))
            .unwrap()
            .filter_map(|e| e.ok())
            .collect();
        assert_eq!(entries.len(), 1, "no tmp leftover");

        let _ = std::fs::remove_dir_all(&dir);
    }
}
