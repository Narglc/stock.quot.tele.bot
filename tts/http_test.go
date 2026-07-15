package tts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPSynthesizer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Token") != "sekret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["text"] == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "audio/ogg")
		w.Header().Set("X-Audio-Format", "ogg")
		_, _ = w.Write([]byte("OGG-OPUS-BYTES"))
	}))
	defer srv.Close()

	h := NewHTTP(srv.URL, "zh-CN-XiaoyiNeural", "sekret")
	a, err := h.Synthesize(context.Background(), "你好")
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if a.Format != "ogg" || string(a.Data) != "OGG-OPUS-BYTES" {
		t.Fatalf("unexpected audio: format=%q data=%q", a.Format, a.Data)
	}

	// 鉴权失败路径
	bad := NewHTTP(srv.URL, "", "wrong")
	if _, err := bad.Synthesize(context.Background(), "x"); err == nil {
		t.Fatalf("鉴权错误应返回 error")
	}
}

func TestDetectAudioFormat(t *testing.T) {
	cases := []struct{ ct, xf, want string }{
		{"audio/ogg", "", "ogg"},
		{"audio/mpeg", "", "mp3"},
		{"application/octet-stream", "mp3", "mp3"},
		{"", "", "ogg"},
	}
	for _, c := range cases {
		h := http.Header{}
		if c.ct != "" {
			h.Set("Content-Type", c.ct)
		}
		if c.xf != "" {
			h.Set("X-Audio-Format", c.xf)
		}
		if got := detectAudioFormat(h); got != c.want {
			t.Fatalf("ct=%q xf=%q → %q, want %q", c.ct, c.xf, got, c.want)
		}
	}
}
