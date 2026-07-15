package tts

import (
	"bytes"
	"context"
	"os/exec"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
)

// ToOggOpus 确保音频为 Ogg/Opus（Telegram 语音气泡 sendVoice 要的格式）。
//   - 已是 ogg/opus（如 Azure 直出）→ 原样返回，不转码；
//   - 其它（如 edge-tts 的 mp3）→ 用 ffmpeg 转码为 ogg/opus。
//
// best-effort：未装 ffmpeg 或转码失败时原样返回，由发送端按原格式当音频文件发送，不阻断。
func ToOggOpus(ctx context.Context, a *Audio) *Audio {
	if a == nil || a.Format == "ogg" || a.Format == "opus" {
		return a // 无必要，直接用
	}
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		log.Warnf("未找到 ffmpeg，跳过转码，按 %s 原样发送（将作为音频文件而非语音气泡）", a.Format)
		return a
	}
	cmd := exec.CommandContext(ctx, ff,
		"-hide_banner", "-loglevel", "error",
		"-i", "pipe:0",
		"-c:a", "libopus", "-b:a", "32k", "-ar", "48000", "-ac", "1",
		"-f", "ogg", "pipe:1",
	)
	cmd.Stdin = bytes.NewReader(a.Data)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil || out.Len() == 0 {
		log.Warnf("ffmpeg 转码失败，按 %s 原样发送: %v %s", a.Format, err, truncate(errb.String(), 150))
		return a
	}
	return &Audio{Data: out.Bytes(), Format: "ogg"}
}
