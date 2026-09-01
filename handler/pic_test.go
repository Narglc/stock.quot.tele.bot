package handler

import "testing"

func TestPicFileName(t *testing.T) {
	cases := []struct{ url, want string }{
		{"https://example.com/a/b/cat.jpg", "cat.jpg"},
		{"https://example.com/cat.PNG", "cat.PNG"},
		{"https://example.com/cat.webp?w=100", "cat.webp"},
		{"https://example.com/cat.jpg#frag", "cat.jpg"},
		{"https://example.com/noext", "wakeup.jpg"},
		{"https://example.com/a.bin", "wakeup.jpg"},
		{"https://example.com/", "wakeup.jpg"},
	}
	for _, c := range cases {
		if got := picFileName(c.url); got != c.want {
			t.Errorf("picFileName(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestWebpToPNG_PassthroughNonWebp(t *testing.T) {
	// 非 webp 数据应原样返回（按原格式交给下游处理）
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}
	if got := webpToPNG(png); string(got) != string(png) {
		t.Error("非 webp 应原样返回")
	}

	short := []byte{1, 2, 3}
	if got := webpToPNG(short); string(got) != string(short) {
		t.Error("过短数据应原样返回，不能 panic")
	}

	if got := webpToPNG(nil); got != nil {
		t.Error("nil 应原样返回")
	}
}

func TestWebpToPNG_BadWebpFallsBack(t *testing.T) {
	// 有 RIFF/WEBP 魔数但内容损坏：解码失败时应原样返回而不是 panic 或返回空
	bad := append([]byte("RIFF\x00\x00\x00\x00WEBP"), 0xff, 0xfe, 0xfd)
	got := webpToPNG(bad)
	if string(got) != string(bad) {
		t.Error("损坏的 webp 应原样返回，交由下游降级处理")
	}
}
