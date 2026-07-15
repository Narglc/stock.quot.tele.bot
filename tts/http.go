package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPSynthesizer 把合成委托给一个 HTTP TTS 服务——通常是本地 worker 经 ssh -R
// 反向隧道映射到 VPS 回环（如 http://127.0.0.1:5000/tts）。
// 好处：重依赖（edge-tts / ChatTTS+GPU / 转码工具）全留在本地，VPS 只发请求收音频，
// 保持精简（distroless 无需 Python/ffmpeg）。
//
// 约定：POST url，body = {"text","voice"}；响应体 = 音频字节，
// 格式取 X-Audio-Format 头或 Content-Type（audio/ogg→ogg，audio/mpeg→mp3），默认 ogg。
// worker 建议直接返回 Ogg/Opus，这样 VPS 端 ToOggOpus 直接放行、完全不需要转码工具。
type HTTPSynthesizer struct {
	url    string
	voice  string
	token  string
	client *http.Client
}

// NewHTTP 构造 HTTP 委托合成器。token 非空时带 X-Auth-Token 头（与 worker 约定的共享密钥）。
func NewHTTP(url, voice, token string) *HTTPSynthesizer {
	return &HTTPSynthesizer{
		url:    url,
		voice:  voice,
		token:  token,
		client: &http.Client{Timeout: 90 * time.Second}, // 本地 TTS（含 GPU 模型）可能较慢
	}
}

func (h *HTTPSynthesizer) Name() string { return "http" }

func (h *HTTPSynthesizer) Synthesize(ctx context.Context, text string) (*Audio, error) {
	if h.url == "" {
		return nil, fmt.Errorf("http tts 未配置 url")
	}
	reqBody, _ := json.Marshal(map[string]string{"text": text, "voice": h.voice})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("X-Auth-Token", h.token)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http tts status %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("http tts 返回空音频")
	}
	return &Audio{Data: data, Format: detectAudioFormat(resp.Header)}, nil
}

// detectAudioFormat 从响应头推断音频格式，默认 ogg（worker 约定直出 Ogg/Opus）。
func detectAudioFormat(h http.Header) string {
	if f := strings.TrimSpace(h.Get("X-Audio-Format")); f != "" {
		return strings.ToLower(f)
	}
	ct := strings.ToLower(h.Get("Content-Type"))
	switch {
	case strings.Contains(ct, "ogg"), strings.Contains(ct, "opus"):
		return "ogg"
	case strings.Contains(ct, "mpeg"), strings.Contains(ct, "mp3"):
		return "mp3"
	}
	return "ogg"
}
