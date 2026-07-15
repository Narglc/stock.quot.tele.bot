package tts

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AzureSynthesizer 调用 Azure 认知服务 TTS REST 接口。
// 输出 ogg-48khz-16bit-mono-opus，正好是 Telegram 语音气泡(sendVoice)要的 Ogg/Opus，免转码。
type AzureSynthesizer struct {
	key    string
	region string
	voice  string
	client *http.Client
}

// NewAzure 构造 Azure 合成器。voice 为空时默认 zh-CN-XiaoxiaoNeural。
func NewAzure(key, region, voice string) *AzureSynthesizer {
	if voice == "" {
		voice = "zh-CN-XiaoxiaoNeural"
	}
	return &AzureSynthesizer{
		key:    key,
		region: region,
		voice:  voice,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *AzureSynthesizer) Name() string { return "azure" }

func (a *AzureSynthesizer) Synthesize(ctx context.Context, text string) (*Audio, error) {
	if a.key == "" || a.region == "" {
		return nil, fmt.Errorf("azure tts 缺少 key/region")
	}
	endpoint := fmt.Sprintf("https://%s.tts.speech.microsoft.com/cognitiveservices/v1", a.region)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte(buildSSML(a.voice, text))))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", a.key)
	req.Header.Set("Content-Type", "application/ssml+xml")
	req.Header.Set("X-Microsoft-OutputFormat", "ogg-48khz-16bit-mono-opus")
	req.Header.Set("User-Agent", "gohome-bot")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("azure tts status %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	return &Audio{Data: data, Format: "ogg"}, nil
}

// buildSSML 组装 SSML，并对文本做 XML 转义（防止 &<> 破坏结构）。
func buildSSML(voice, text string) string {
	var esc bytes.Buffer
	_ = xml.EscapeText(&esc, []byte(text))
	return fmt.Sprintf(
		`<speak version='1.0' xmlns='http://www.w3.org/2001/10/synthesis' xml:lang='zh-CN'><voice name='%s'>%s</voice></speak>`,
		voice, esc.String())
}
