/*
 * main.rs — 程序入口。Tauri v2 把实际逻辑放在 lib.rs（库 crate），
 * main.rs 仅作启动器调用 ocean_harness_lib::run()；
 * 移动端（iOS/Android）也能复用同一 lib.rs 代码。
 */

/*
 * windows_subsystem = "windows"：release 模式在 Windows 上隐藏控制台窗口（GUI 程序专属属性），
 * debug 模式保留控制台方便看日志；删掉会导致 Windows 用户看到多余的黑窗口，
 * 原注释 "DO NOT REMOVE!!" 即此意。
 */
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

/*
 * 程序入口函数：双击应用图标或执行 `cargo run` 时从这里开始执行。
 * 只做一件事：调用 lib.rs 中定义的 `run()` 函数；
 * 使用 `ocean_harness_lib::` 前缀是因为 lib.rs 被编译为该名字的库 crate（Cargo.toml [lib] name）。
 */
fn main() {
    /*
     * 调用 lib.rs 中定义的 run() 函数来启动 Tauri 应用
     * `ocean_harness_lib` 是 crate 名（Cargo.toml [lib].name），
     * `::run()` 是该 crate 导出的公开函数
     */
    ocean_harness_lib::run();
}
