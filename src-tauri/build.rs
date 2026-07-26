/*
 * build.rs — 构建脚本（Build Script）
 *
 * 【什么是 build.rs？】
 * 在 Rust 项目中，build.rs 是一个特殊的文件，叫做"构建脚本"。
 * 它会在 `cargo build` 编译主代码 **之前** 自动被编译和执行。
 * 相当于 C/C++ 项目中的 Makefile 预处理步骤，但用的是 Rust 语法。
 *
 * 【这个文件在哪被调用的？】
 * Cargo 会自动检测项目根目录（src-tauri/）下的 build.rs 文件，
 * 不需要在 Cargo.toml 中手动配置。
 *
 * 【它的运行时机】
 * 1. 你执行 `cargo build` 或 `cargo run`
 * 2. Cargo 先编译 build.rs → 生成一个可执行文件
 * 3. 运行这个可执行文件（即调用下面的 main()）
 * 4. 然后再编译项目的实际代码（src/main.rs、src/lib.rs 等）
 *
 * 【Tauri 为什么需要构建脚本？】
 * Tauri 框架在编译前需要做很多准备工作，比如：
 *   - 解析 tauri.conf.json 配置文件
 *   - 生成前端资源的嵌入代码（把 HTML/CSS/JS 打包进二进制）
 *   - 生成平台相关的绑定代码
 *   - 处理图标等资源文件
 * `tauri_build::build()` 这一个调用就帮我们完成了所有这些事。
 *
 * 【Rust 语法知识】
 * fn main() → 定义函数，fn 是 function 的缩写
 *   Rust 程序的入口点就是 main() 函数，和 C/C++ 一样
 * tauri_build::build() → 调用 tauri_build 包的 build() 函数
 *   :: 是路径分隔符，类似其他语言的 . 或 /
 *   这里 tauri_build 是一个外部 crate（Rust 的"包"），在 Cargo.toml 的 [build-dependencies] 中声明
 */
fn main() {
    /* 调用 Tauri 提供的构建函数，它会：
     * 1. 读取 tauri.conf.json 配置
     * 2. 生成必要的 Rust 代码（比如前端资源的嵌入代码）
     * 3. 为当前操作系统平台准备编译环境
     */
    tauri_build::build()
}
