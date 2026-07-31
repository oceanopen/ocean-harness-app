use tauri::{AppHandle, Monitor, Position, Rect};

/// 默认/回退窗口尺寸（逻辑像素）。
pub const DEFAULT_SIZE: (f64, f64) = (800.0, 600.0);

/// panel 窗口（控制台）占目标屏的比例。
pub const PANEL_RATIO: f64 = 0.85;

/// settings 窗口占目标屏的比例。
pub const SETTINGS_RATIO: f64 = 0.7;

/// 显示器逻辑化几何：把 Tauri 物理像素几何 + work_area 除以 scale_factor 转成逻辑像素，
/// 供窗口定位算法统一消费，避免各调用方重复 `/ sf`。
///
/// 字段全 pub：work_area_center / ratio_size 等几何工具跨模块读取。
#[derive(Clone, Copy)]
pub struct MonitorInfo {
    pub width: f64,
    pub height: f64,
    pub wa_x: f64,
    pub wa_y: f64,
    pub wa_width: f64,
    pub wa_height: f64,
}

impl MonitorInfo {
    /// 从 Tauri Monitor 构造逻辑化几何（size + work_area 均除以 scale_factor）。
    pub fn from_monitor(m: &Monitor) -> Self {
        let sf = m.scale_factor();
        let ms = m.size();
        let wa = m.work_area();
        MonitorInfo {
            width: ms.width as f64 / sf,
            height: ms.height as f64 / sf,
            wa_x: wa.position.x as f64 / sf,
            wa_y: wa.position.y as f64 / sf,
            wa_width: wa.size.width as f64 / sf,
            wa_height: wa.size.height as f64 / sf,
        }
    }
}

/// 用 rect（通常是 tray.rect()）的物理坐标定位所在显示器，返回逻辑化几何。
/// 多屏 / DPI 不一致时用矩形包含判断绕开 monitor_from_point 的识别坑；
/// 找不到（rect 为 Logical 或不在任何屏内）返回 None，调用方自行兜底。
pub fn find_monitor_for_rect(app: &AppHandle, rect: &Rect) -> Option<MonitorInfo> {
    let (x, y) = match &rect.position {
        Position::Physical(p) => (p.x, p.y),
        Position::Logical(_) => return None,
    };

    for m in app.available_monitors().ok()? {
        let mp = m.position();
        let ms = m.size();
        if x >= mp.x && x < mp.x + ms.width as i32 && y >= mp.y && y < mp.y + ms.height as i32 {
            return Some(MonitorInfo::from_monitor(&m));
        }
    }
    None
}

/// 通过 tray_id 取托盘图标几何，再用其物理坐标定位所在屏。
/// 找不到（tray 不存在 / rect 缺失 / 不在任何屏内）返回 None，调用方用 DEFAULT_SIZE 兜底。
pub fn find_monitor_for_tray(app: &AppHandle, tray_id: &str) -> Option<MonitorInfo> {
    let tray = app.tray_by_id(tray_id)?;
    let rect = tray.rect().ok()??;
    find_monitor_for_rect(app, &rect)
}

/// 在目标显示器的 work_area（扣除任务栏 / Dock）内按窗口尺寸算居中逻辑坐标。
/// `max(0.0)` 防止窗口大于 work_area 时偏移跑出可用区域（贴 work_area 左上角）。
pub fn work_area_center(monitor: &MonitorInfo, width: f64, height: f64) -> (f64, f64) {
    let x = monitor.wa_x + ((monitor.wa_width - width) / 2.0).max(0.0);
    let y = monitor.wa_y + ((monitor.wa_height - height) / 2.0).max(0.0);
    (x, y)
}

/// 按目标屏逻辑尺寸和给定比例算窗口尺寸（逻辑像素）。
pub fn ratio_size(monitor: &MonitorInfo, ratio: f64) -> (f64, f64) {
    (monitor.width * ratio, monitor.height * ratio)
}
