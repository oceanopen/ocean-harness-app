package wecom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeTencentStatus(t *testing.T) {
	cases := map[string]qrPollStatus{
		"success":   qrPollSuccess,
		"expired":   qrPollExpired,
		"timeout":   qrPollExpired,
		"fail":      qrPollFailed,
		"failed":    qrPollFailed,
		"error":     qrPollFailed,
		"waiting":   qrPollWaiting,
		"":          qrPollWaiting,
		"whatever":  qrPollWaiting, // 未知值宽松兜底
		"scanned_x": qrPollWaiting,
	}
	for raw, want := range cases {
		if got := normalizeTencentStatus(raw); got != want {
			t.Fatalf("normalize(%q)=%s, want %s", raw, got, want)
		}
	}
}

func TestSafeAuthURL(t *testing.T) {
	// 生产 base：官方域 https。
	t.Run("生产语义", func(t *testing.T) {
		if _, err := safeAuthURL("https://work.weixin.qq.com/ai/qc/auth?ticket=x"); err != nil {
			t.Fatalf("官方域应通过: %v", err)
		}
		for _, bad := range []string{
			"http://work.weixin.qq.com/ai/qc/auth",     // 非 https
			"https://evil.example.com/ai/qc/auth",      // 异域
			"https://work.weixin.qq.com.evil.com/auth", // 伪域
			"https://work.weixin.qq.com:8443/auth",     // 异端口
			"::::not-a-url",
		} {
			if _, err := safeAuthURL(bad); err == nil {
				t.Fatalf("应拒绝 %q", bad)
			}
		}
	})
	// 测试 base 覆盖：对照伪端点同源。
	t.Run("测试端点对照", func(t *testing.T) {
		old := qrBaseOverride
		qrBaseOverride = "http://127.0.0.1:1"
		defer func() { qrBaseOverride = old }()
		if _, err := safeAuthURL("http://127.0.0.1:1/ai/qc/auth?t=1"); err != nil {
			t.Fatalf("同源伪端点应通过: %v", err)
		}
		if _, err := safeAuthURL("https://work.weixin.qq.com/auth"); err == nil {
			t.Fatalf("覆盖后官方域反而应拒绝")
		}
	})
}

// fakeTencentQR 起伪腾讯端点：generate 返回固定 scode+authURL；query 按 pollResponses 序列出牌。
func fakeTencentQR(t *testing.T, pollResponses []string) *httptest.Server {
	t.Helper()
	i := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/ai/qc/generate", func(w http.ResponseWriter, r *http.Request) {
		// auth_url 动态回显本服务器地址（与 qrBase 对照校验同源）。
		_, _ = w.Write([]byte(`{"code":0,"data":{"scode":"sc-1","auth_url":"http://` + r.Host + `/ai/qc/auth?ticket=t-1"}}`))
	})
	mux.HandleFunc("/ai/qc/query_result", func(w http.ResponseWriter, _ *http.Request) {
		body := `{"code":0,"data":{"status":"waiting"}}`
		if i < len(pollResponses) {
			body = pollResponses[i]
			i++
		}
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	// generate 的 auth_url 占位在注入 base 后生成（StartProvisioning 内部走 qrBase()）。
	return srv
}

func TestProvisionFlow(t *testing.T) {
	restore := func(t *testing.T, base string) {
		old := qrBaseOverride
		qrBaseOverride = base
		t.Cleanup(func() {
			qrBaseOverride = old
			provisionMu.Lock()
			activeAttempt = nil
			provisionMu.Unlock()
		})
	}

	t.Run("pending→waiting 轮询保持→success 激活→connected", func(t *testing.T) {
		srv := fakeTencentQR(t, []string{
			`{"code":0,"data":{"status":"waiting"}}`,
			`{"code":0,"data":{"status":"success","bot_info":{"botid":"wb-123","secret":"sec-abc"}}}`,
		})
		restore(t, srv.URL)

		var activatedBotID int
		var gotID, gotSecret string
		SetProvisionActivator(func(remoteBotID, secret string) (int, error) {
			gotID, gotSecret = remoteBotID, secret
			activatedBotID = 42
			return 42, nil
		})

		view, err := StartProvisioning()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if view.State != string(provPending) || view.QRContent == "" || view.PollIntervalMs != qrPollIntervalMs {
			t.Fatalf("begin 视图异常: %+v", view)
		}

		// 第一轮 waiting：仍 pending、无终态。
		v1, err := RegistrationStatus(view.AttemptID)
		if err != nil || v1.State != string(provPending) {
			t.Fatalf("waiting 轮: %+v err=%v", v1, err)
		}
		// 第二轮 success：激活回调收到凭据，状态 connected 且带 bot 行 id。
		v2, err := RegistrationStatus(view.AttemptID)
		if err != nil {
			t.Fatalf("success 轮: %v", err)
		}
		if v2.State != string(provConnected) || v2.BotID != 42 {
			t.Fatalf("connected 视图: %+v", v2)
		}
		if gotID != "wb-123" || gotSecret != "sec-abc" {
			t.Fatalf("激活回调凭据不符: %s/%s", gotID, gotSecret)
		}
		// connected 后二维码已清（防过期内容复用）。
		if v2.QRContent != "" {
			t.Fatalf("终态应清空 qrContent: %+v", v2)
		}
		_ = activatedBotID
	})

	t.Run("expired 终态与本地 TTL", func(t *testing.T) {
		srv := fakeTencentQR(t, []string{`{"code":0,"data":{"status":"expired"}}`})
		restore(t, srv.URL)
		SetProvisionActivator(func(string, string) (int, error) {
			t.Fatal("expired 不应触发激活")
			return 0, nil
		})
		view, err := StartProvisioning()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		v, err := RegistrationStatus(view.AttemptID)
		if err != nil || v.State != string(provExpired) || v.ErrCode != "expired" {
			t.Fatalf("expired 视图: %+v err=%v", v, err)
		}
		// 终态后再 poll：不再出牌（幂等返回终态）。
		v2, err := RegistrationStatus(view.AttemptID)
		if err != nil || v2.State != string(provExpired) {
			t.Fatalf("终态幂等: %+v err=%v", v2, err)
		}
	})

	t.Run("cancel 后 poll 报不存在", func(t *testing.T) {
		srv := fakeTencentQR(t, nil)
		restore(t, srv.URL)
		view, err := StartProvisioning()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if err := CancelProvisioning(view.AttemptID); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		if _, err := RegistrationStatus(view.AttemptID); err != ErrProvisionNotFound {
			t.Fatalf("cancel 后应 not found, got %v", err)
		}
	})

	t.Run("新 begin 自动取消旧会话", func(t *testing.T) {
		srv := fakeTencentQR(t, nil)
		restore(t, srv.URL)
		v1, err := StartProvisioning()
		if err != nil {
			t.Fatalf("begin1: %v", err)
		}
		if _, err := StartProvisioning(); err != nil {
			t.Fatalf("begin2: %v", err)
		}
		if _, err := RegistrationStatus(v1.AttemptID); err != ErrProvisionNotFound {
			t.Fatalf("旧会话应被取消: %v", err)
		}
	})

	t.Run("激活失败落 failed/activation-failed", func(t *testing.T) {
		srv := fakeTencentQR(t, []string{
			`{"code":0,"data":{"status":"success","bot_info":{"botid":"wb-x","secret":"s"}}}`,
		})
		restore(t, srv.URL)
		SetProvisionActivator(func(string, string) (int, error) { return 0, context.DeadlineExceeded })
		view, err := StartProvisioning()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		v, err := RegistrationStatus(view.AttemptID)
		if err != nil || v.State != string(provFailed) || v.ErrCode != "activation-failed" {
			t.Fatalf("failed 视图: %+v err=%v", v, err)
		}
	})
}

func TestQRSourceAndPlat(t *testing.T) {
	if qrSource == "" || qrSource == "deepseek-harness" {
		t.Fatalf("source 应为本项目标识: %q", qrSource)
	}
	if defaultPlat() != "1" {
		t.Fatalf("非 win/linux 平台 plat 应为 1: %s", defaultPlat())
	}
	if strings.Contains(qrGenerateURL, qrQueryURL) {
		t.Fatal("端点常量不应混用")
	}
}
