// CLI 二进制路径探测（claude_orca T5.1 direct spawn 支撑）。
//
// GUI app 的 PATH 常缺用户 shell 的 nvm/volta 等目录（claude 常装在那里），
// 不能拿 app env 的 PATH 找 CLI——经 login+interactive shell 探测一次，同时拿：
//   1. CLI 绝对路径：哨兵标记之间的 `command -v <token>` 输出行（须 `/` 开头
//      ——POSIX shell 的 builtin/alias 返回名字本身而非路径，天然判失败 →
//      调用方回落 shell 注入路径。fish 例外：`command -v` 绕过 builtin 直搜
//      PATH 返回真路径，反而更优）。哨兵隔离 rc 文件噪声：rc 在 -c 脚本执行
//      前跑完，输出在时间上不可能插入哨兵之间（裸取「首个 / 开头行」会把
//      rc 的绝对路径报错行误判为 CLI，且误判被成功缓存钉死——审查修复）。
//   2. login shell 的完整 $PATH：`/usr/bin/env` 输出中的 `PATH=` 行（外部
//      二进制，输出格式跨 shell 一致；printf "$PATH" 在 fish 下按列表空格
//      连接会破坏 PATH）。注入 direct 子进程，防 npm/nvm 安装的 node
//      shebang CLI 因找不到 node 起不来（brew 自包含二进制不依赖，注入无害）。
//
// 缓存：只缓存成功结果（per token）；失败不缓存，下次 spawn 重探——用户装上
// CLI 后无需重启 app 即生效。OnceLock set 后不可覆盖（失败会被钉死），
// 故用 Mutex<HashMap>。
//
// 探测为同步子进程（同 claude_state.rs 的 ps 探测范式），rc 文件异常拖慢/挂起
// 的极端情形不设超时兜底——失败回落注入路径的语义保底可用性。

/// 哨兵标记：隔离 `command -v` 输出与 rc 文件噪声（见模块头注）。
const SENTINEL_OPEN: &str = "WE_TERM_CLI_PROBE_OPEN";
const SENTINEL_CLOSE: &str = "WE_TERM_CLI_PROBE_CLOSE";

use std::collections::HashMap;
use std::process::Stdio;

/// token 合法字符（CLI 名：字母/数字/下划线/连字符/点）。含引号/空格/斜杠等
/// 一律拒绝（探测脚本按字符串拼进 `-c`，这是唯一的注入面；绝对路径形态的
/// token 也拒——生产链路只传 CLI 名，如 "claude"）。
fn valid_token(token: &str) -> bool {
    !token.is_empty()
        && token
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || matches!(c, '_' | '-' | '.'))
}

/// 解析探测输出：哨兵之间的首个 `/` 开头行 = CLI 路径；最后一条 `PATH=` 行 =
/// login PATH（env 输出在脚本末尾，后者覆盖 rc 噪声可能的 `PATH=` 行）。
/// 哨兵缺失 / 块内无路径行 / 无 `PATH=` 行 → None。
fn parse_probe_output(stdout: &str) -> Option<(String, String)> {
    let mut in_block = false;
    let mut bin: Option<String> = None;
    let mut path: Option<String> = None;
    for line in stdout.lines() {
        if line == SENTINEL_OPEN {
            in_block = true;
        } else if line == SENTINEL_CLOSE {
            in_block = false;
        } else if in_block {
            if bin.is_none() && line.starts_with('/') {
                bin = Some(line.trim().to_string());
            }
        } else if let Some(rest) = line.strip_prefix("PATH=") {
            path = Some(rest.to_string());
        }
    }
    Some((bin?, path?))
}

/// 成功解析缓存（token → (CLI 绝对路径, login PATH)）。失败不落缓存。
/// HashMap::new 非 const，静态初始化走 LazyLock。
static CACHE: std::sync::LazyLock<std::sync::Mutex<HashMap<String, (String, String)>>> =
    std::sync::LazyLock::new(|| std::sync::Mutex::new(HashMap::new()));

/// 探测 CLI 绝对路径 + login shell PATH。
/// None = 不可直启（token 非法 / CLI 不在场 / 是 builtin 或 alias / shell
/// 探测失败），调用方回落 startup_command 注入路径。
fn probe(token: &str) -> Option<(String, String)> {
    // shell 同 resolve_shell 取向：$SHELL 优先回退 /bin/zsh。
    let shell = std::env::var("SHELL").unwrap_or_else(|_| "/bin/zsh".to_string());
    let script =
        format!("echo {SENTINEL_OPEN}; command -v {token}; echo {SENTINEL_CLOSE}; /usr/bin/env");
    // stdin 关死：rc 脚本若读 stdin 会挂起探测进程。
    let out = std::process::Command::new(shell)
        .arg("-l")
        .arg("-i")
        .arg("-c")
        .arg(&script)
        .stdin(Stdio::null())
        .output()
        .ok()?;
    if !out.status.success() {
        // command -v 未命中时 shell 仍退出 0（脚本末段 env 恒成功）；非 0 =
        // shell 自身异常（$SHELL 失效等），回落注入路径。
        return None;
    }
    parse_probe_output(&String::from_utf8_lossy(&out.stdout))
}

/// 解析 CLI 直启所需信息（带成功缓存）。见模块头注。
pub(crate) fn resolve_cli_bin(token: &str) -> Option<(String, String)> {
    if !valid_token(token) {
        return None;
    }
    {
        let cache = CACHE
            .lock()
            .expect("cli_bin cache mutex poisoned");
        if let Some(hit) = cache.get(token) {
            return Some(hit.clone());
        }
    }
    let resolved = probe(token);
    if let Some(hit) = &resolved {
        CACHE
            .lock()
            .expect("cli_bin cache mutex poisoned")
            .insert(token.to_string(), hit.clone());
    }
    resolved
}

#[cfg(test)]
mod tests {
    use super::*;

    /// 真实 shell 探测外部命令：绝对路径 + 含系统目录的 login PATH。
    #[test]
    fn resolve_external_bin() {
        let (bin, path) = resolve_cli_bin("cat").expect("cat 应可解析");
        assert!(bin.starts_with('/'), "bin 应为绝对路径: {bin}");
        assert!(bin.ends_with("/cat"), "bin 形态: {bin}");
        assert!(
            path.split(':').any(|p| p == "/usr/bin"),
            "login PATH 应含 /usr/bin: {path}"
        );
    }

    /// builtin：`command -v` 返回名字本身（非绝对路径）→ None（回落信号）。
    /// 仅 POSIX shell（zsh/bash）语义；fish 的 `command -v` 绕过 builtin 直搜
    /// PATH 返回真路径（直启更优，非缺陷）——本测试假定开发机 $SHELL 非 fish。
    #[test]
    fn resolve_builtin_returns_none() {
        assert!(resolve_cli_bin("echo").is_none());
    }

    /// 不存在的 CLI 名 → None。
    #[test]
    fn resolve_nonexistent_returns_none() {
        assert!(resolve_cli_bin("we_term_no_such_cli_xyz").is_none());
    }

    /// 非法 token（注入面防护 + 绝对路径拒绝）→ None 且不发起探测。
    #[test]
    fn reject_invalid_tokens() {
        assert!(resolve_cli_bin("").is_none());
        assert!(resolve_cli_bin("/bin/echo").is_none());
        assert!(resolve_cli_bin("cla';ude").is_none());
        assert!(resolve_cli_bin("cla ude").is_none());
    }

    /// 纯解析单测：哨兵之间的路径行命中 / 哨兵外 rc 噪声（含 `/` 开头行）不
    /// 误判 / 多条 PATH=（后者覆盖）/ 哨兵缺失或块内无路径行 → None。
    #[test]
    fn parse_probe_output_rules() {
        // 正常形态：rc 噪声（含绝对路径报错行）→ 哨兵 → 路径 → 哨兵 → env。
        let ok = parse_probe_output(
            "welcome from rc\n/usr/bin/python3: No module named pyenv\n\
             WE_TERM_CLI_PROBE_OPEN\n/opt/homebrew/bin/claude\nWE_TERM_CLI_PROBE_CLOSE\n\
             PATH=/a:/b\nPATH=/x:/usr/bin\n",
        );
        assert_eq!(
            ok,
            Some((
                "/opt/homebrew/bin/claude".into(),
                "/x:/usr/bin".into()
            ))
        );
        // builtin 形态：块内输出为名字（非 `/` 开头）→ None。
        assert!(
            parse_probe_output(
                "WE_TERM_CLI_PROBE_OPEN\necho\nWE_TERM_CLI_PROBE_CLOSE\nPATH=/usr/bin\n"
            )
            .is_none()
        );
        // 哨兵缺失（异常 shell 吞 echo）：路径行不裸取 → None（防噪声误判钉死缓存）。
        assert!(parse_probe_output("/opt/homebrew/bin/claude\nPATH=/usr/bin\n").is_none());
        // 块内无路径行（CLI 未安装）/ 无 PATH= 行 → None。
        assert!(
            parse_probe_output("WE_TERM_CLI_PROBE_OPEN\nWE_TERM_CLI_PROBE_CLOSE\nPATH=/usr/bin\n")
                .is_none()
        );
        assert!(
            parse_probe_output("WE_TERM_CLI_PROBE_OPEN\n/bin/cat\nWE_TERM_CLI_PROBE_CLOSE\n")
                .is_none()
        );
        // env 输出行（VAR=value）不参与路径判定。
        let env_line = parse_probe_output(
            "WE_TERM_CLI_PROBE_OPEN\n/bin/cat\nWE_TERM_CLI_PROBE_CLOSE\nHOME=/Users/x\nPATH=/usr/bin\n",
        );
        assert_eq!(
            env_line,
            Some(("/bin/cat".into(), "/usr/bin".into()))
        );
    }
}
