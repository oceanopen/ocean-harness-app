// 浏览器 OS 层动作（纯桌面便利，不经 Go sidecar 数据面——D7 注明的例外项）。
//
// focus_browser_window：把指定 profile 的 Chrome 窗口组前置到桌面最前（有头模式
// 介入场景的一键定位）。定位依据 = 进程 cmdline 含 `--user-data-dir=<profiles 根>/
// <profile>`——T1.1 实测 Chrome 进程树全成员 argv 均含此参数，按它反查进程可靠；
// 且 Chrome per user-data-dir 单实例（T1.2 detect 依据），一个 profile 唯一对应一个
// Chrome 主进程及其整棵窗口组。
//
// profiles 根目录 = ~/.ocean-harness/browser/profiles（Go 侧 paths.go 生产默认；
// Go 侧根目录可被测试环境变量覆写，属测试态，生产唯一）。
//
// 错误纪律（对齐 terminal/ 跳转先例）：未找到进程/窗口显式中文报错，不静默、不做
// 无谓拉起；动作失败附 stderr 原文。

use dirs::home_dir;

/// profile 名守卫（Go 侧已有完整校验，此处防御性复查——拼 user-data-dir 路径前
/// 拒绝路径穿越与分隔符）。
fn validate_profile(profile: &str) -> Result<(), String> {
    if profile.is_empty() || profile == "." || profile == ".." || profile.contains(['/', '\\']) {
        return Err(format!("非法的 profile 名：{profile}"));
    }
    Ok(())
}

/// profile 的 user-data-dir 绝对路径（字符串，供 cmdline 匹配）。
fn user_data_dir(profile: &str) -> Result<String, String> {
    validate_profile(profile)?;
    let root = home_dir().ok_or_else(|| "无法定位用户主目录".to_string())?;
    Ok(root
        .join(".ocean-harness")
        .join("browser")
        .join("profiles")
        .join(profile)
        .to_string_lossy()
        .into_owned())
}

/// 在 PIDs 集合中判定主进程：父 PID 不在集合内者为树根（Chrome 主进程）。
/// 理论上恰有一个；防御性取最小 PID（主进程先于其子进程创建）。
/// （仅 macOS 消费——Windows 侧主进程判定在 PowerShell 脚本内完成。）
#[cfg(target_os = "macos")]
fn main_pid_of(pids: &[u32], ppid_of: &dyn Fn(u32) -> Option<u32>) -> Option<u32> {
    let set: std::collections::HashSet<u32> = pids.iter().copied().collect();
    pids.iter()
        .copied()
        .filter(|pid| match ppid_of(*pid) {
            Some(ppid) => !set.contains(&ppid),
            // 父 PID 查询失败（进程竞态消亡等）：不作为主进程候选，靠其余成员兜底。
            None => false,
        })
        .min()
}

#[tauri::command]
#[specta::specta]
pub fn focus_browser_window(profile: String) -> Result<(), String> {
    let dir = user_data_dir(&profile)?;
    #[cfg(target_os = "macos")]
    {
        macos::focus(&profile, &dir)
    }
    #[cfg(target_os = "windows")]
    {
        windows::focus(&profile, &dir)
    }
    #[cfg(all(unix, not(target_os = "macos")))]
    {
        let _ = dir;
        Err("聚焦窗口当前仅支持 macOS 与 Windows".to_string())
    }
}

#[cfg(target_os = "macos")]
mod macos {
    use std::process::Command;

    use super::main_pid_of;

    /// pgrep 的 -f 模式是 ERE：转义路径中的元字符防注入/误匹配。
    fn regex_escape(s: &str) -> String {
        let mut out = String::with_capacity(s.len());
        for c in s.chars() {
            if matches!(
                c,
                '\\' | '^' | '$' | '.' | '|' | '?' | '*' | '+' | '(' | ')' | '[' | ']' | '{' | '}'
            ) {
                out.push('\\');
            }
            out.push(c);
        }
        out
    }

    /// ps 查父 PID（进程竞态消亡返回 None）。
    fn ppid_of(pid: u32) -> Option<u32> {
        let out = Command::new("ps")
            .args(["-o", "ppid=", "-p", &pid.to_string()])
            .output()
            .ok()?;
        let text = String::from_utf8_lossy(&out.stdout);
        text.trim().parse().ok()
    }

    /// System Events 按 unix id 前置进程（Chrome per-profile 独立实例，其整棵
    /// 窗口组随进程 frontmost 一并前置）。时序固定：先解最小化（frontmost 不会
    /// 还原 miniaturized 窗口——最小化窗口只留在 Dock，表现为「点了没反应」），
    /// 再 set frontmost。
    fn frontmost_by_pid(pid: u32) -> Result<(), String> {
        let script = format!(
            r#"tell application "System Events"
    tell (first process whose unix id is {pid})
        repeat with w in windows
            try
                if value of attribute "AXMinimized" of w is true then
                    set value of attribute "AXMinimized" of w to false
                end if
            end try
        end repeat
        set frontmost to true
    end tell
end tell"#
        );
        let out = Command::new("osascript")
            .args(["-e", &script])
            .output()
            .map_err(|e| format!("无法执行 osascript：{e}"))?;
        if out.status.success() {
            return Ok(());
        }
        Err(format!(
            "前置浏览器窗口失败：{}",
            String::from_utf8_lossy(&out.stderr).trim()
        ))
    }

    pub fn focus(profile: &str, dir: &str) -> Result<(), String> {
        let pattern = format!("--user-data-dir={}", regex_escape(dir));
        let out = Command::new("pgrep")
            .args(["-f", "--", &pattern])
            .output()
            .map_err(|e| format!("无法执行 pgrep：{e}"))?;
        if !out.status.success() {
            // pgrep 退出码 1 = 无匹配（未运行或无头模式无 Chrome 进程树）。
            return Err(format!(
                "未找到该 profile 的浏览器进程：「{profile}」未运行或为无头模式"
            ));
        }
        let pids: Vec<u32> = String::from_utf8_lossy(&out.stdout)
            .split_whitespace()
            .filter_map(|s| s.parse().ok())
            .collect();
        let Some(main_pid) = main_pid_of(&pids, &ppid_of) else {
            return Err(format!(
                "未找到该 profile 的浏览器主进程：「{profile}」"
            ));
        };
        frontmost_by_pid(main_pid)
    }
}

#[cfg(target_os = "windows")]
mod windows {
    use std::os::windows::process::CommandExt;
    use std::process::Command;

    /// 单次 PowerShell 完成全部动作（进程发现 → 主进程判定 → 窗口前置），零新增
    /// Rust 依赖（对齐 Go recoverOrphans「shell 工具探查 Windows 进程」先例）。
    /// 退出码约定：0 成功 / 2 无浏览器进程 / 3 无可见窗口 / 其他 = 脚本自身失败。
    /// 中文错误统一由 Rust 侧按退出码映射（-Command 传中文有代码页风险，不传）。
    const PS_SCRIPT_TEMPLATE: &str = r#"
$ErrorActionPreference = 'Stop'
$marker = '--user-data-dir=__DIR__'
$all = Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -and $_.CommandLine.Contains($marker) }
if (-not $all -or $all.Count -eq 0) { exit 2 }
$set = @{}
$all | ForEach-Object { $set[[uint32]$_.ProcessId] = $true }
$roots = $all | Where-Object { -not $set.ContainsKey([uint32]$_.ParentProcessId) }
$main = $roots | Sort-Object CreationDate | Select-Object -First 1
if (-not $main) { $main = $all | Sort-Object CreationDate | Select-Object -First 1 }
$targetPid = [uint32]$main.ProcessId
Add-Type @'
using System;
using System.Runtime.InteropServices;
public class OceanWin32 {
    public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lp);
    [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc cb, IntPtr lp);
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint pid);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr hWnd);
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
}
'@
$script:focusDone = $false
$cb = [OceanWin32+EnumWindowsProc]{ param($h, $l)
    if ($script:focusDone) { return $true }
    $owner = [uint32]0
    [OceanWin32]::GetWindowThreadProcessId($h, [ref]$owner) | Out-Null
    if ($owner -eq $targetPid -and [OceanWin32]::IsWindowVisible($h)) {
        if ([OceanWin32]::IsIconic($h)) { [OceanWin32]::ShowWindow($h, 9) | Out-Null }
        [OceanWin32]::SetForegroundWindow($h) | Out-Null
        $script:focusDone = $true
        return $false
    }
    return $true
}
[OceanWin32]::EnumWindows($cb, [IntPtr]::Zero) | Out-Null
if (-not $script:focusDone) { exit 3 }
exit 0
"#;

    pub fn focus(profile: &str, dir: &str) -> Result<(), String> {
        let script = PS_SCRIPT_TEMPLATE.replace("__DIR__", dir);
        let out = Command::new("powershell")
            .args([
                "-NoProfile",
                "-NonInteractive",
                "-Command",
                &script,
            ])
            .creation_flags(0x0800_0000) // CREATE_NO_WINDOW：不闪控制台
            .output()
            .map_err(|e| format!("无法执行 PowerShell：{e}"))?;
        match out.status.code() {
            Some(0) => Ok(()),
            Some(2) => Err(format!(
                "未找到该 profile 的浏览器进程：「{profile}」未运行或为无头模式"
            )),
            Some(3) => Err(format!(
                "未找到该 profile 的浏览器窗口：「{profile}」的 Chrome 进程在运行但没有可见窗口"
            )),
            other => Err(format!(
                "前置浏览器窗口失败（exit={other:?}）：{}",
                String::from_utf8_lossy(&out.stderr).trim()
            )),
        }
    }
}
