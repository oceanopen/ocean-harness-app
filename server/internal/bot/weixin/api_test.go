package weixin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newTestClient 起 TLS httptest 服务（域白名单放行 127.0.0.1，信任其自签证书）。
func newTestClient(t *testing.T, handler http.Handler) (*apiClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	client := &apiClient{http: srv.Client(), hostAllowed: func(u *url.URL) bool {
		return u.Hostname() == "127.0.0.1"
	}}
	return client, srv
}

func TestNumStringPreservesBigID(t *testing.T) {
	// 19 位大整数：数值化会静默丢精度（float64 解出 ...4688），numString 必须保真原文字节。
	var out struct {
		MessageID numString `json:"message_id"`
		Seq       numString `json:"seq"`
		Quoted    numString `json:"quoted_id"`
	}
	payload := `{"message_id": 7312345678901234567, "seq": "9007199254740993", "quoted_id": null}`
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.MessageID.String() != "7312345678901234567" {
		t.Fatalf("big id lost precision: %q", out.MessageID)
	}
	if out.Seq.String() != "9007199254740993" {
		t.Fatalf("string id mismatch: %q", out.Seq)
	}
	if out.Quoted.String() != "" {
		t.Fatalf("null must map to empty: %q", out.Quoted)
	}
}

func TestProviderEnvelopeRejected(t *testing.T) {
	if (providerEnvelope{Ret: "0", ErrCode: "0"}).rejected() != "" {
		t.Fatal("zero codes must pass")
	}
	if (providerEnvelope{}).rejected() != "" {
		t.Fatal("absent codes must pass")
	}
	if got := (providerEnvelope{Ret: "-14"}).rejected(); got != "-14" {
		t.Fatalf("ret -14: %q", got)
	}
	if got := (providerEnvelope{ErrCode: "42"}).rejected(); got != "42" {
		t.Fatalf("errcode fallback: %q", got)
	}
}

func TestRequestJSONAuthHeaders(t *testing.T) {
	var gotHeaders http.Header
	client, srv := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	err := client.requestJSON(context.Background(), requestSpec{
		method: "POST", baseURL: srv.URL, endpoint: "ilink/bot/getupdates",
		token: " tk ", body: map[string]any{"k": "v"},
	}, &struct{}{})
	if err != nil {
		t.Fatalf("requestJSON: %v", err)
	}
	if gotHeaders.Get("iLink-App-Id") != "bot" || gotHeaders.Get("iLink-App-ClientVersion") != ilinkClientVersion {
		t.Fatalf("common headers missing: %v", gotHeaders)
	}
	if gotHeaders.Get("Authorization") != "Bearer tk" || gotHeaders.Get("AuthorizationType") != "ilink_bot_token" {
		t.Fatalf("auth headers mismatch: %v", gotHeaders)
	}
	if gotHeaders.Get("X-WECHAT-UIN") == "" {
		t.Fatal("uin header missing")
	}
	if gotHeaders.Get("Content-Type") != "application/json" {
		t.Fatal("content-type missing")
	}
}

func TestGetUpdatesStaleToken(t *testing.T) {
	client, srv := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ret":-14}`))
	}))
	_, err := client.GetUpdates(context.Background(), "tk", srv.URL, "buf")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != errStaleToken {
		t.Fatalf("want stale-token, got %v", err)
	}
}

func TestGetUpdatesTimeoutYieldsEmptyBatch(t *testing.T) {
	client, srv := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte(`{}`))
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	resp, err := client.GetUpdates(ctx, "tk", srv.URL, "cursor")
	if err != nil {
		t.Fatalf("timeout must normalize to empty batch, got %v", err)
	}
	if len(resp.Msgs) != 0 || resp.GetUpdatesBuf != "cursor" {
		t.Fatalf("empty batch mismatch: %+v", resp)
	}
}

func TestRequestJSONHTTPError(t *testing.T) {
	client, srv := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	err := client.requestJSON(context.Background(), requestSpec{
		method: "GET", baseURL: srv.URL, endpoint: "ilink/bot/getconfig", token: "tk",
	}, &struct{}{})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != errHTTP || ae.Status != http.StatusBadGateway {
		t.Fatalf("want http-error 502, got %v", err)
	}
}

func TestArtifactClientIDDeterministic(t *testing.T) {
	first := artifactClientID("delivery-key-1", 3)
	if first != artifactClientID("delivery-key-1", 3) {
		t.Fatal("same delivery key + media type must derive same client_id")
	}
	if artifactClientID("delivery-key-1", 1) == first {
		t.Fatal("image must differ from file for the same key")
	}
	if artifactClientID("delivery-key-2", 3) == first {
		t.Fatal("different keys must differ")
	}
	if !strings.HasPrefix(first, "ocean-weixin-") {
		t.Fatalf("client_id prefix mismatch: %q", first)
	}
}

func TestSendArtifactRejectsEnvelope(t *testing.T) {
	client, srv := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ret":42001}`))
	}))
	_, err := client.SendArtifact(context.Background(), "tk", srv.URL, "user",
		&Artifact{FileName: "a.png", Bytes: []byte("data"), DeliveryKey: "k"}, 1, "")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != errRejected || ae.ProviderCode != "42001" {
		t.Fatalf("want rejected 42001, got %v", err)
	}
}

func TestSendTextRejectsEnvelope(t *testing.T) {
	client, srv := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ret":-14}`))
	}))
	_, err := client.SendText(context.Background(), "tk", srv.URL, "user", "hi", "")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != errStaleToken {
		t.Fatalf("want stale-token, got %v", err)
	}
}

func TestUncertainDelivery(t *testing.T) {
	for _, code := range []string{errNetwork, errTimeout} {
		if got := uncertainDelivery(apiErr(code, "x")); got.(*APIError).Code != errUncertain {
			t.Fatalf("%s must be uncertain", code)
		}
	}
	fiveOh := &APIError{Code: errHTTP, Status: 502}
	if uncertainDelivery(fiveOh).(*APIError).Code != errUncertain {
		t.Fatal("5xx must be uncertain")
	}
	if uncertainDelivery(apiErr(errRejected, "x")).(*APIError).Code != errRejected {
		t.Fatal("clear rejection must stay as-is")
	}
}

func TestNormalizeAPIBaseURL(t *testing.T) {
	got, err := normalizeAPIBaseURL("https://ilinkai.weixin.qq.com/v1/x?a=1")
	if err != nil || got != "https://ilinkai.weixin.qq.com/v1/x/" {
		t.Fatalf("normalize: %q %v", got, err)
	}
	for _, bad := range []string{
		"http://ilinkai.weixin.qq.com/", "https://evil.com/", "https://weixin.qq.com.evil.com/",
		"https://ilinkai.weixin.qq.com:8443/",
	} {
		if _, err := normalizeAPIBaseURL(bad); err == nil {
			t.Fatalf("must reject %q", bad)
		}
	}
}

func TestNormalizeQRURL(t *testing.T) {
	if _, err := normalizeQRURL("https://work.weixin.qq.com/qr"); err != nil {
		t.Fatalf("valid qr rejected: %v", err)
	}
	if _, err := normalizeQRURL("http://work.weixin.qq.com/qr"); err == nil {
		t.Fatal("http qr must be rejected")
	}
	if _, err := normalizeQRURL(""); err == nil {
		t.Fatal("empty qr must be rejected")
	}
}

func TestCDNUploadURLTrust(t *testing.T) {
	got, err := cdnUploadURL("", "param1", "filekey1")
	if err != nil || !strings.HasPrefix(got, cdnBaseURL+"/upload?encrypted_query_param=param1") {
		t.Fatalf("param upload url: %q %v", got, err)
	}
	if _, err := cdnUploadURL("https://novac2c.cdn.weixin.qq.com/c2c/upload?x=1", "", "k"); err != nil {
		t.Fatalf("full upload url rejected: %v", err)
	}
	for _, bad := range []string{
		"https://novac2c.cdn.weixin.qq.com/c2c/other",
		"https://evil.com/c2c/upload",
		"https://user:pass@novac2c.cdn.weixin.qq.com/c2c/upload",
	} {
		if _, err := cdnUploadURL(bad, "", "k"); err == nil {
			t.Fatalf("must reject %q", bad)
		}
	}
	if _, err := cdnUploadURL("", "", "k"); err == nil {
		t.Fatal("missing upload url must be rejected")
	}
}

func TestCDNDownloadURLTrust(t *testing.T) {
	got, err := cdnDownloadURL("q1", "")
	if err != nil || got != cdnBaseURL+"/download?encrypted_query_param=q1" {
		t.Fatalf("param download url: %q %v", got, err)
	}
	if _, err := cdnDownloadURL("", "https://novac2c.cdn.weixin.qq.com/c2c/abc"); err != nil {
		t.Fatalf("full download url rejected: %v", err)
	}
	if _, err := cdnDownloadURL("", "https://evil.com/c2c/abc"); err == nil {
		t.Fatal("untrusted download host must be rejected")
	}
	if _, err := cdnDownloadURL("", ""); err == nil {
		t.Fatal("missing download url must be rejected")
	}
}

func TestWechatUIN(t *testing.T) {
	uin := wechatUIN()
	if uin == "" {
		t.Fatal("uin must not be empty")
	}
	if _, err := base64.StdEncoding.DecodeString(uin); err != nil {
		t.Fatalf("uin must be base64: %v", err)
	}
}
