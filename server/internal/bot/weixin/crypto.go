package weixin

import (
	"bytes"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"
)

/*
 * AES-128-ECB 加解密（Go 标准库无 ECB 模式，逐块手写）+ PKCS7 恒填充。
 * iLink 媒体密文按 paddedSize = ceil((n+1)/16)*16 对齐——与 PKCS7 恒填充一致（空输入也补满块），
 * CDN 上传 Content-Length 必须用补齐后精确值。
 */
func paddedSize(n int) int {
	return (n + aes.BlockSize) / aes.BlockSize * aes.BlockSize
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padLen := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(padLen)}, padLen)...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("invalid pkcs7 payload")
	}
	padLen := int(data[len(data)-1])
	if padLen == 0 || padLen > blockSize || padLen > len(data) {
		return nil, errors.New("invalid pkcs7 padding")
	}
	for _, b := range data[len(data)-padLen:] {
		if int(b) != padLen {
			return nil, errors.New("invalid pkcs7 padding")
		}
	}
	return data[:len(data)-padLen], nil
}

func aesECBEncrypt(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plain, block.BlockSize())
	out := make([]byte, len(padded))
	for i := 0; i < len(padded); i += block.BlockSize() {
		block.Encrypt(out[i:i+block.BlockSize()], padded[i:i+block.BlockSize()])
	}
	return out, nil
}

func aesECBDecrypt(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) == 0 || len(ciphertext)%block.BlockSize() != 0 {
		return nil, apiErr("invalid-ciphertext", "微信媒体的加密数据无效。")
	}
	out := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += block.BlockSize() {
		block.Decrypt(out[i:i+block.BlockSize()], ciphertext[i:i+block.BlockSize()])
	}
	return pkcs7Unpad(out, block.BlockSize())
}

/*
 * parseMediaAESKey iLink 媒体密钥双编码兼容：aeskey 直给 32-hex，或 media.aes_key
 * base64（16 字节原文 / 32-hex 字符串）。
 */
func parseMediaAESKey(directHex, encodedB64 string) ([]byte, error) {
	invalid := apiErr("invalid-image-key", "微信媒体的加密密钥无效。")
	if directHex != "" {
		raw, err := hex.DecodeString(directHex)
		if err != nil || len(raw) != aes.BlockSize {
			return nil, invalid
		}
		return raw, nil
	}
	raw, err := base64.StdEncoding.DecodeString(encodedB64)
	if err != nil {
		return nil, invalid
	}
	if len(raw) == aes.BlockSize {
		return raw, nil
	}
	if len(raw) == 2*aes.BlockSize {
		if key, err := hex.DecodeString(string(raw)); err == nil {
			return key, nil
		}
	}
	return nil, invalid
}

/*
 * splitText ≤maxChars 单条返回；超长按换行优先切分（断点靠前于 60% 上限则硬切），
 * 按 rune 切保证多字节字符不劈半。
 */
func splitText(text string, maxChars int) []string {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return []string{text}
	}
	chunks := make([]string, 0, len(runes)/maxChars+1)
	remaining := runes
	for len(remaining) > maxChars {
		splitAt := maxChars
		threshold := maxChars * 6 / 10
		for i := maxChars; i > threshold; i-- {
			if remaining[i-1] == '\n' {
				splitAt = i - 1
				break
			}
		}
		if splitAt <= 0 {
			splitAt = maxChars
		}
		chunks = append(chunks, string(remaining[:splitAt]))
		rest := remaining[splitAt:]
		for len(rest) > 0 && rest[0] == '\n' {
			rest = rest[1:]
		}
		remaining = rest
	}
	if len(remaining) > 0 {
		chunks = append(chunks, string(remaining))
	}
	return chunks
}

// ---------- 编码/摘要小工具 ----------

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func newAESKey() []byte {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return b
}

func hexEncode(b []byte) string       { return hex.EncodeToString(b) }
func base64StdEncode(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
func md5Sum(b []byte) []byte          { sum := md5.Sum(b); return sum[:] }
func sha256Sum(b []byte) []byte       { sum := sha256.Sum256(b); return sum[:] }

// wechatUIN 随机 4 字节 uint32 十进制字符串的 base64（协议要求的会话设备标识）。
func wechatUIN() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	uin := binary.BigEndian.Uint32(b[:])
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(uin), 10)))
}
