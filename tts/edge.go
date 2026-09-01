package tts

import (
	"bytes"
	"context"
	"fmt"
	"github.com/narglc/stock.quot.tele.bot/utils"
	"os"
	"os/exec"
)

// EdgeSynthesizer 通过 edge-tts CLI 合成（免费、无 key，中文声线好）。
// 需服务器装有 edge-tts（pip install edge-tts）。输出 mp3。
// 作为 Azure 的可插拔备选：把 tts.provider 改成 edge 即切换，上层零改动。
type EdgeSynthesizer struct {
	voice string
	bin   string
}

// NewEdge 构造 edge-tts 合成器。voice 为空时默认 zh-CN-XiaoxiaoNeural。
func NewEdge(voice string) *EdgeSynthesizer {
	if voice == "" {
		voice = "zh-CN-XiaoxiaoNeural"
	}
	return &EdgeSynthesizer{voice: voice, bin: "edge-tts"}
}

func (e *EdgeSynthesizer) Name() string { return "edge" }

func (e *EdgeSynthesizer) Synthesize(ctx context.Context, text string) (*Audio, error) {
	f, err := os.CreateTemp("", "edgetts-*.mp3")
	if err != nil {
		return nil, err
	}
	tmp := f.Name()
	_ = f.Close()
	defer os.Remove(tmp)

	cmd := exec.CommandContext(ctx, e.bin, "--voice", e.voice, "--text", text, "--write-media", tmp)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("edge-tts 合成失败: %v: %s", err, utils.Truncate(stderr.String(), 200))
	}

	data, err := os.ReadFile(tmp)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("edge-tts 输出为空")
	}
	return &Audio{Data: data, Format: "mp3"}, nil
}
