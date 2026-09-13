// macOS 悬浮辅助窗的不激活呈现（嵌入终端体验优化）。
//
// 背景：tao（tauri 窗口层）的 set_visible(true) 在 macOS 上是
// makeKeyAndOrderFront——任何 show() 都会把窗口变为 key window，从当前
// 窗口抢走键盘焦点。典型受害者：panel 终端里 claude 回车置 busy →
// claude-sessions 事件 → pet_task 面板 auto-show，每轮对话抢一次焦，
// 表现为「终端回车后光标消失，需重新点击」（WKWebView 让出 first
// responder 后，panel 重新成为 key window 也不会自动恢复 DOM 焦点）。
//
// 方案：pet / pet_task 这类对键盘无诉求的悬浮窗，呈现改走原生
// orderFront:——窗口上到前台但不成为 key window（tao 自身窗口操作同款
// 原语）。AppKit 窗口操作非线程安全（tao util/async.rs 同款约束）；当前
// 调用方（同步命令）经 wry IPC 链路本就在主线程执行，包一层
// run_on_main_thread 是不依赖调用方线程位置的防御（未来改 async 命令或
// 从后台线程调用仍安全；主线程调用走 tauri-runtime-wry 的快速路径同步
// 执行，不入队）。闭包捕获 WebviewWindow 句柄（Send + Clone）而非裸
// 指针，主线程内再取 ns_window，规避裸指针的 Send 限制与悬垂风险。

// app_handle 在 Manager trait 上，仅 macOS 分支使用，随分支一并门控。
#[cfg(target_os = "macos")]
use tauri::Manager;
use tauri::WebviewWindow;

/// 不激活呈现窗口：macOS 走原生 orderFront:（上前台不夺 key window）；
/// 非 macOS / 主线程派发失败回落常规 show()（仅损失不抢焦特性，显隐语义不变）。
pub fn show_no_activate(win: &WebviewWindow) {
    #[cfg(target_os = "macos")]
    {
        let w = win.clone();
        let dispatched = win.app_handle().run_on_main_thread(move || {
            // 主线程上取原生句柄；窗口已销毁（Err）则静默——此时显隐已无意义。
            if let Ok(ns) = w.ns_window() {
                let ns: *mut objc2::runtime::AnyObject = ns.cast();
                let _: () = unsafe {
                    objc2::msg_send![
                        ns,
                        orderFront: std::ptr::null_mut::<objc2::runtime::AnyObject>()
                    ]
                };
            }
        });
        if dispatched.is_ok() {
            return;
        }
        // 派发失败（极少：app 已在退出流程）回落常规 show，保底可见。
    }
    let _ = win.show();
}
