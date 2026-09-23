// 应用元信息查询命令。

use tauri::AppHandle;

/// 当前应用名（panel 面包屑根 crumb 等 UI 展示用）。
/// 取自 config product_name（与窗口标题同源）：dev 构建经 tauri.dev.conf.json 覆盖为
/// "Ocean Harness [DEV]"，发布构建为 "Ocean Harness"——前端借此区分当前构建形态
/// （i18n common:brand 无此后缀，仅作异步回填前的同步初值）。缺失时回退品牌名。
#[tauri::command]
#[specta::specta]
pub fn get_app_name(app: AppHandle) -> String {
    app.config()
        .product_name
        .clone()
        .unwrap_or_else(|| "Ocean Harness".to_string())
}
