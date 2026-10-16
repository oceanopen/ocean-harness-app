package browser

/*
 * manager_live.go：CDP 观察通道的 manager 侧生命周期（T4.4）——per-profile 单订 viewer 槽、SSE 订阅引用计数与 idle 保活闸、远程桌面输入面；三处收敛点强拆（Close/headless 切换/watchExit → dropLiveViewer）分见 manager.go 对应路径。
 * 单订语义：DispatchInput 无 targetId、远程桌面画面唯一——每 profile 至多一个活跃 viewer；订阅目标与在订 viewer 不符即重建（旧连接强拆，在订方经 done 感知重订）。
 */

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	/*
	 * liveInputTimeout 输入分发单次预算（连接已死时快速失败；不受 per-call 120s 约束
	 * 语义的另一面是不搭车其预算——自带短预算防僵尸连接挂住 HTTP 请求）。
	 */
	liveInputTimeout = 5 * time.Second
)

// liveIssueTag 观察通道自愈唤醒调用在操作流水中的归因标签。
const liveIssueTag = "browser_live"

// ErrLiveNotActive 无活跃观察流时的输入分发错误（调用方按 errors.Is 判别）。
var ErrLiveNotActive = errors.New("浏览器未在投送：请先在面板建立实时画面后再发送输入")

/*
 * liveSlot 单 profile 活跃观察面：viewer + 订阅时 targetId 锚点 + 强拆 reason。reason 供
 * 在订 SSE 区分终态语义（管理面强拆非空；自然断连为空——可重订自愈）。v/targetID 建
 * 槽后只读，reason 仅在 viewerMu 下随强拆写入。
 */
type liveSlot struct {
	v        *liveViewer
	targetID string
	reason   string
}

/*
 * LiveStream 一路 SSE 订阅的观察面句柄（T4.5 消费）：帧箱按 gen 续等、target 通知、
 * 断连信号与强拆原因投影。重建换槽后旧流经旧 viewer 的 done 感知，重订得新流。
 */
type LiveStream struct {
	m       *Manager
	profile string
	slot    *liveSlot
}

// LatestFrame 当前帧与其代数（尚无帧 ok=false；SSE 写侧以 gen 判新帧）。
func (s *LiveStream) LatestFrame() (LiveFrame, uint64, bool) {
	return s.slot.v.frames.latest()
}

// FrameChanged 帧变更信号（deliver 关闭换新；select 收到后重取，LatestFrame 按 gen 判新取帧）。
func (s *LiveStream) FrameChanged() <-chan struct{} {
	return s.slot.v.frames.changedC()
}

// Targets target 变更通知流（导航回显消歧 / 页面关闭终态）。
func (s *LiveStream) Targets() <-chan LiveTarget {
	return s.slot.v.targets
}

// Done 观察面断连信号（连接关闭或管理面强拆）。
func (s *LiveStream) Done() <-chan struct{} {
	return s.slot.v.done
}

/*
 * CloseReason 强拆原因：非空 = 管理面强拆（会话关闭/无头切换/引擎退出，终态）；
 * 空 = 自然断连（后端可自愈，前端重订即可）。
 */
func (s *LiveStream) CloseReason() string {
	s.m.viewerMu.Lock()
	defer s.m.viewerMu.Unlock()
	return s.slot.reason
}

// Unsubscribe 退订本流（SSE 连接终结时调用，refs 减一）。
func (s *LiveStream) Unsubscribe() {
	s.m.UnsubscribeLive(s.profile)
}

/*
 * SubscribeLive 订阅实时画面（T4.5 SSE 端点消费）：targetId 投影解析 → 同目标复用在订
 * viewer / 异页或断连重建 → refs 计数 → 0→1 时停 idle 计时。连接阶段经 connectMu 串行
 * （并发订阅同页复用同一 viewer，不重复拨号）。
 */
func (m *Manager) SubscribeLive(ctx context.Context, profile string, targetID string) (*LiveStream, error) {
	if err := ValidateProfileName(profile); err != nil {
		return nil, err
	}
	page, err := m.resolveLiveTarget(profile, targetID)
	if err != nil {
		return nil, err
	}
	entry := m.entryOf(profile)

	m.connectMu.Lock()
	defer m.connectMu.Unlock()

	m.viewerMu.Lock()
	slot := m.viewers[profile]
	switch {
	case slot != nil && slot.targetID == targetID:
		select {
		case <-slot.v.done: // 断连残留槽：拆掉走重建
		default:
			entry.viewerRefs.Add(1)
			m.viewerMu.Unlock()
			return &LiveStream{m: m, profile: profile, slot: slot}, nil
		}
		m.dropViewerLocked(profile, "")
	case slot != nil:
		m.dropViewerLocked(profile, "") // 目标变更：重建（旧流经 done 感知）
	}
	refsBefore := entry.viewerRefs.Load()
	becameActive := refsBefore == 0
	entry.viewerRefs.Add(1)
	m.viewerMu.Unlock()

	v, err := connectLive(ctx, profile, page, func(c context.Context) error {
		return m.liveWakeProbe(c, profile)
	}, m.log)
	m.viewerMu.Lock()
	if err != nil {
		entry.viewerRefs.Add(-1)
		m.viewerMu.Unlock()
		if becameActive {
			m.onLiveRefsChange(profile)
		}
		return nil, err
	}
	if entry.viewerRefs.Load() == 0 {
		// 订阅方在连接期间全部退订：丢弃新建 viewer，不落槽。
		m.viewerMu.Unlock()
		_ = v.conn.close()
		return nil, errors.New("实时画面订阅已中断，请重试")
	}
	slot = &liveSlot{v: v, targetID: targetID}
	m.viewers[profile] = slot
	m.viewerMu.Unlock()

	if becameActive {
		m.onLiveRefsChange(profile)
	}
	return &LiveStream{m: m, profile: profile, slot: slot}, nil
}

// UnsubscribeLive 退订（refs 减一）：末订（1→0）拆连接并恢复 idle 计时。双退订防御。
func (m *Manager) UnsubscribeLive(profile string) {
	entry := m.entryOf(profile)
	m.viewerMu.Lock()
	if entry.viewerRefs.Load() <= 0 {
		m.viewerMu.Unlock()
		return
	}
	inactive := entry.viewerRefs.Add(-1) == 0
	if inactive {
		m.dropViewerLocked(profile, "")
	}
	m.viewerMu.Unlock()
	if inactive {
		m.onLiveRefsChange(profile)
	}
}

/*
 * DispatchInput 远程桌面输入分发：要求活跃观察面存在，经已 attach 会话直发 Input 域
 * 命令——不经 acceptCall、不占 callMu、不受 per-call 超时约束（与 AI Forward 并发；
 * 「AI 操作中」可见性靠页面操作流水）。
 */
func (m *Manager) DispatchInput(ctx context.Context, profile, method string, params any) error {
	if err := ValidateProfileName(profile); err != nil {
		return err
	}
	m.viewerMu.Lock()
	slot := m.viewers[profile]
	m.viewerMu.Unlock()
	if slot == nil {
		return ErrLiveNotActive
	}
	select {
	case <-slot.v.done:
		return ErrLiveNotActive
	default:
	}
	ictx, cancel := context.WithTimeout(ctx, liveInputTimeout)
	defer cancel()
	err := slot.v.DispatchInput(ictx, method, params)
	label := inputActionLabel(method, params)
	if err == nil && label == "" {
		return nil
	}
	outcome, errText := CallOK, ""
	if err != nil {
		outcome, errText = CallError, err.Error()
	}
	m.recordCall(profile, slot.targetID, label, "", outcome, errText)
	return err
}

/*
 * inputActionLabel 输入动作的流水展示名；返回空串 = 不落流水（鼠标移动/keyUp 高频噪声）。
 */
func inputActionLabel(method string, params any) string {
	action, _ := params.(map[string]any)["type"].(string)
	switch method {
	case "Input.dispatchKeyEvent":
		if action == "keyUp" {
			return ""
		}
		return "键盘按键"
	case "Input.insertText":
		return "输入文字"
	case "Input.dispatchMouseEvent":
		switch action {
		case "mousePressed":
			return "鼠标按下"
		case "mouseReleased":
			return "鼠标释放"
		}
		return ""
	}
	return ""
}

/*
 * dropLiveViewer 三处收敛点（Close/headless 切换/watchExit）的强拆入口：关连接 +
 * 删槽 + 记 reason（在订方经 done + CloseReason 感知终态）。
 */
func (m *Manager) dropLiveViewer(profile, reason string) {
	m.viewerMu.Lock()
	m.dropViewerLocked(profile, reason)
	m.viewerMu.Unlock()
}

/*
 * dropViewerLocked 拆槽实现（须持 viewerMu）。reason 非空优先保留：重建路径携空
 * reason 到达时，不得覆写管理面强拆（Close/无头切换/watchExit）已写入的终态语义。
 */
func (m *Manager) dropViewerLocked(profile, reason string) {
	slot, ok := m.viewers[profile]
	if !ok {
		return
	}
	delete(m.viewers, profile)
	if slot.reason == "" {
		slot.reason = reason
	}
	_ = slot.v.conn.close()
}

/*
 * onLiveRefsChange refs 越迁后的 idle 重估（viewerMu 锁外调用，双锁内执行）：按当前
 * refs 终值收敛——订户在场停计时、末订恢复计时；回调乱序也按同一判据收敛。
 */
func (m *Manager) onLiveRefsChange(profile string) {
	entry := m.entryOf(profile)
	m.mu.Lock()
	entry.mu.Lock()
	if entry.viewerRefs.Load() > 0 {
		entry.stopIdleTimerLocked()
	} else {
		entry.resetIdleTimerLocked(m)
	}
	entry.mu.Unlock()
	m.mu.Unlock()
}

/*
 * liveWakeProbe 观察通道自愈唤醒（connectLive wake 钩子）：Forward 一次 browser_tabs
 * list 触发引擎活性探测（busy 拒绝由 connectLive 按「浏览器活着」正交解读）；结果不回写
 * 投影——Pages SSOT 归页面观察者。
 */
func (m *Manager) liveWakeProbe(ctx context.Context, profile string) error {
	res, err := m.Forward(ctx, profile, EngineToolTabs, map[string]any{"action": "list"}, liveIssueTag)
	if err != nil {
		return err
	}
	if res.IsError {
		return fmt.Errorf("获取页面列表失败: %s", ResultText(res))
	}
	return nil
}

/*
 * resolveLiveTarget 订阅目标解析：按 targetId 查投影（页面观察者事件驱动维护，miss 即
 * 显式报错——投影随 SSE 自愈，前端未点选页时自动跟随激活页重订）。
 */
func (m *Manager) resolveLiveTarget(profile, targetID string) (PageView, error) {
	for _, p := range m.view.row(profile).Pages {
		if p.TargetID == targetID {
			return p, nil
		}
	}
	return PageView{}, fmt.Errorf("页面不存在或已关闭（targetId %s），请刷新页面列表后重试", targetID)
}
