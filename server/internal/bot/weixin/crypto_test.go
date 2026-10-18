package weixin

import (
	"bytes"
	"testing"
)

func TestPaddedSize(t *testing.T) {
	cases := map[int]int{0: 16, 1: 16, 15: 16, 16: 32, 17: 32, 100: 112, 1024: 1040}
	for in, want := range cases {
		if got := paddedSize(in); got != want {
			t.Fatalf("paddedSize(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestAESRoundTrip(t *testing.T) {
	key := newAESKey()
	for _, size := range []int{0, 1, 15, 16, 17, 100, 4096} {
		plain := bytes.Repeat([]byte("x"), size)
		ciphertext, err := aesECBEncrypt(plain, key)
		if err != nil {
			t.Fatalf("size %d encrypt: %v", size, err)
		}
		if len(ciphertext) != paddedSize(size) {
			t.Fatalf("size %d ciphertext len = %d, want %d", size, len(ciphertext), paddedSize(size))
		}
		decoded, err := aesECBDecrypt(ciphertext, key)
		if err != nil {
			t.Fatalf("size %d decrypt: %v", size, err)
		}
		if !bytes.Equal(decoded, plain) {
			t.Fatalf("size %d roundtrip mismatch", size)
		}
	}
}

func TestPKCS7PadAlwaysFullBlock(t *testing.T) {
	padded := pkcs7Pad(nil, 16)
	if len(padded) != 16 || padded[15] != 16 {
		t.Fatalf("empty input must pad a full block, got %v", padded)
	}
	padded = pkcs7Pad(bytes.Repeat([]byte{0x01}, 16), 16)
	if len(padded) != 32 || padded[31] != 16 {
		t.Fatalf("aligned input must take a full extra pad block, got %v", padded)
	}
}

func TestAESDecryptRejectsCorrupt(t *testing.T) {
	key := newAESKey()
	if _, err := aesECBDecrypt([]byte("not-block-aligned"), key); err == nil {
		t.Fatal("expected error for non-block-aligned ciphertext")
	}
	ciphertext, _ := aesECBEncrypt([]byte("hello"), key)
	ciphertext[len(ciphertext)-1] ^= 0xFF
	if _, err := aesECBDecrypt(ciphertext, key); err == nil {
		t.Fatal("expected error for corrupted padding")
	}
}

func TestParseMediaAESKey(t *testing.T) {
	raw := newAESKey()
	if key, err := parseMediaAESKey(hexEncode(raw), ""); err != nil || !bytes.Equal(key, raw) {
		t.Fatalf("direct 32-hex: %v", err)
	}
	if key, err := parseMediaAESKey("", base64StdEncode(raw)); err != nil || !bytes.Equal(key, raw) {
		t.Fatalf("base64 raw 16: %v", err)
	}
	if key, err := parseMediaAESKey("", base64StdEncode([]byte(hexEncode(raw)))); err != nil || !bytes.Equal(key, raw) {
		t.Fatalf("base64 hex 32: %v", err)
	}
	for _, bad := range [][2]string{
		{"zz", ""},                             // 非 hex
		{"abcd", ""},                           // 长度不足
		{"", "not-base64!!"},                   // 坏 base64
		{"", base64StdEncode([]byte("short"))}, // 解码后长度不对
	} {
		if _, err := parseMediaAESKey(bad[0], bad[1]); err == nil {
			t.Fatalf("expected error for %v", bad)
		}
	}
}

func TestSplitText(t *testing.T) {
	if got := splitText("short", maxMessageChars); len(got) != 1 || got[0] != "short" {
		t.Fatalf("short text must pass through, got %v", got)
	}

	// 换行断点在 60% 之外：按最后一个换行切。
	long := append(append(bytes.Repeat([]byte("a"), 1200), '\n'), bytes.Repeat([]byte("b"), 1200)...)
	got := splitText(string(long), maxMessageChars)
	if len(got) != 2 || got[0] != string(long[:1200]) || got[1] != string(long[1201:]) {
		t.Fatalf("newline split mismatch: %v", got)
	}

	// 无换行：硬切。
	got = splitText(string(bytes.Repeat([]byte("c"), 4000)), maxMessageChars)
	if len(got) != 3 || len(got[0]) != maxMessageChars {
		t.Fatalf("hard split mismatch: %d chunks", len(got))
	}

	// 断点太靠前（<60%）：硬切不认。
	withEarlyNewline := append(append(bytes.Repeat([]byte("d"), 100), '\n'), bytes.Repeat([]byte("e"), 3000)...)
	got = splitText(string(withEarlyNewline), maxMessageChars)
	if len(got) != 2 || len(got[0]) != maxMessageChars {
		t.Fatalf("early newline must hard split, got chunk0 len %d", len(got[0]))
	}

	// 多字节字符不劈半。
	multibyte := bytes.Repeat([]byte("微"), maxMessageChars) // 每字 3 字节
	for _, chunk := range splitText(string(multibyte), maxMessageChars) {
		if len([]rune(chunk))*3 != len(chunk) {
			t.Fatalf("rune split across bytes: %q", chunk[:min(9, len(chunk))])
		}
	}
}
