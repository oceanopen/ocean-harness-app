package weixin

import (
	"net/url"
	"strings"
)

// ilinkClientVersion 客户端版本号（与 protocolVersion 2.4.6 同源：(2<<16)|(4<<8)|6）。
const ilinkClientVersion = "132102"

// isWeixinHost 腾讯域白名单（weixin.qq.com / wechat.com 及子域，去尾点、小写）。
func isWeixinHost(hostname string) bool {
	normalized := strings.ToLower(strings.TrimSuffix(hostname, "."))
	return normalized == "weixin.qq.com" || strings.HasSuffix(normalized, ".weixin.qq.com") ||
		normalized == "wechat.com" || strings.HasSuffix(normalized, ".wechat.com")
}

// normalizeAPIBaseURL 业务 API 基址强校验：https + 腾讯域 + 443，剥 query/userinfo，尾补 /。
func normalizeAPIBaseURL(v string) (string, error) {
	parsed, err := normalizeBaseURLFormat(v)
	if err != nil {
		return "", err
	}
	if !trustedWeixinAPIHost(parsed) {
		return "", apiErr("untrusted-base-url", "微信服务返回了不受信任的连接地址。")
	}
	return parsed.String(), nil
}

// normalizeBaseURLFormat 格式规范化（仅 https + 剥 query/userinfo + 尾补 /），域信任由调用方校验。
func normalizeBaseURLFormat(v string) (*url.URL, error) {
	parsed, err := url.Parse(v)
	if err != nil {
		return nil, apiErr("invalid-base-url", "微信服务返回了无效的连接地址。")
	}
	if parsed.Scheme != "https" {
		return nil, apiErr("untrusted-base-url", "微信服务返回了不受信任的连接地址。")
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}
	return parsed, nil
}

// trustedWeixinAPIHost API 基址域信任：腾讯域 + 443。
func trustedWeixinAPIHost(parsed *url.URL) bool {
	return isWeixinHost(parsed.Hostname()) && (parsed.Port() == "" || parsed.Port() == "443")
}

// normalizeQRURL 扫码链接校验（编码进二维码的内容，https + 腾讯域；公开可回传前端渲染）。
func normalizeQRURL(v string) (string, error) {
	if v == "" {
		return "", apiErr("invalid-qr", "微信服务没有返回扫码地址。")
	}
	parsed, err := url.Parse(v)
	if err != nil || parsed.Scheme != "https" || !isWeixinHost(parsed.Hostname()) {
		return "", apiErr("untrusted-qr", "微信服务返回了不受信任的扫码地址。")
	}
	return parsed.String(), nil
}

// cdnUploadURL 上传地址归一：upload_full_url 直用（强校验），否则 upload_param 拼标准上传端点。
func cdnUploadURL(fullURL, uploadParam, fileKey string) (string, error) {
	if fullURL != "" {
		return trustedCDNUploadURL(fullURL)
	}
	if uploadParam == "" {
		return "", apiErr("missing-upload-url", "微信服务没有返回文件上传地址。")
	}
	return trustedCDNUploadURL(cdnBaseURL + "/upload?encrypted_query_param=" + url.QueryEscape(uploadParam) +
		"&filekey=" + url.QueryEscape(fileKey))
}

// trustedCDNUploadURL 上传地址强校验：固定 CDN 域 + /c2c/upload 路径 + 无 userinfo。
func trustedCDNUploadURL(v string) (string, error) {
	parsed, err := url.Parse(v)
	if err != nil {
		return "", apiErr("invalid-upload-url", "微信服务返回了无效的文件上传地址。")
	}
	if parsed.Scheme != "https" || parsed.Hostname() != cdnHost ||
		(parsed.Port() != "" && parsed.Port() != "443") || parsed.Path != "/c2c/upload" || parsed.User != nil {
		return "", apiErr("untrusted-upload-url", "微信服务返回了不受信任的文件上传地址。")
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

/*
 * cdnDownloadURL 入站媒体下载地址：encrypt_query_param 优先拼标准下载端点，
 * 否则校验 full_url（仅固定 CDN 域 /c2c/ 路径）。
 */
func cdnDownloadURL(queryParam, fullURL string) (string, error) {
	if queryParam != "" {
		return cdnBaseURL + "/download?encrypted_query_param=" + url.QueryEscape(queryParam), nil
	}
	if fullURL == "" {
		return "", apiErr("missing-image-url", "微信媒体没有可用的下载地址。")
	}
	parsed, err := url.Parse(fullURL)
	if err != nil {
		return "", apiErr("invalid-image-url", "微信媒体的下载地址无效。")
	}
	if parsed.Scheme != "https" || parsed.Hostname() != cdnHost ||
		(parsed.Port() != "" && parsed.Port() != "443") || !strings.HasPrefix(parsed.Path, "/c2c/") {
		return "", apiErr("untrusted-image-url", "微信媒体的下载地址不受信任。")
	}
	parsed.User = nil
	parsed.Fragment = ""
	return parsed.String(), nil
}
