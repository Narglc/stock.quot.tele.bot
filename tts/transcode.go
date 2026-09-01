package tts

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/utils"
)

// ToOggOpus 确保音频为 Ogg/Opus（Telegram 语音气泡 sendVoice 要的格式）。
//   - 已是 ogg/opus（如 Azure 直出）→ 原样返回，不转码；
//   - 其它（如 edge-tts 的 mp3）→ 自动挑一个可用后端转码：
//     1) ffmpeg（若安装，一步转）；
//     2) mpg123 + opusenc（opus-tools，二者合计 ~700KB，远小于 ffmpeg）；
//   - 都没有 → 原样返回，由发送端按原格式当「音频文件」发（仍可用，只是不是语音气泡）。
//
// best-effort：任何一步失败都不阻断，回退到原格式。
func ToOggOpus(ctx context.Context, a *Audio) *Audio {
	if a == nil || a.Format == "ogg" || a.Format == "opus" {
		return a // 无必要，直接用
	}
	if out, ok := ffmpegToOgg(ctx, a.Data); ok {
		return &Audio{Data: out, Format: "ogg"}
	}
	if out, ok := mpg123OpusencToOgg(ctx, a.Data); ok {
		return &Audio{Data: out, Format: "ogg"}
	}
	log.Warnf("未找到转码工具(ffmpeg 或 mpg123+opusenc)，按 %s 原样发送（作为音频文件而非语音气泡）", a.Format)
	return a
}

// ffmpegToOgg 用 ffmpeg 一步把音频转成 Ogg/Opus（管道进出，无临时文件）。
func ffmpegToOgg(ctx context.Context, data []byte) ([]byte, bool) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, false
	}
	cmd := exec.CommandContext(ctx, ff,
		"-hide_banner", "-loglevel", "error",
		"-i", "pipe:0",
		"-c:a", "libopus", "-b:a", "32k", "-ar", "48000", "-ac", "1",
		"-f", "ogg", "pipe:1",
	)
	cmd.Stdin = bytes.NewReader(data)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil || out.Len() == 0 {
		log.Warnf("ffmpeg 转码失败: %v %s", err, utils.Truncate(errb.String(), 150))
		return nil, false
	}
	return out.Bytes(), true
}

// mpg123OpusencToOgg 轻量后端：mpg123 解 mp3→wav，opusenc 编 wav→Ogg/Opus。
// 走临时文件（自解释的 WAV，规避管道流式 WAV 头长度未知的问题）。
func mpg123OpusencToOgg(ctx context.Context, data []byte) ([]byte, bool) {
	mp, err1 := exec.LookPath("mpg123")
	oe, err2 := exec.LookPath("opusenc")
	if err1 != nil || err2 != nil {
		return nil, false
	}
	dir, err := os.MkdirTemp("", "tts-*")
	if err != nil {
		return nil, false
	}
	defer os.RemoveAll(dir)
	mp3p := filepath.Join(dir, "a.mp3")
	wavp := filepath.Join(dir, "a.wav")
	oggp := filepath.Join(dir, "a.ogg")

	if err := os.WriteFile(mp3p, data, 0o600); err != nil {
		return nil, false
	}
	if err := exec.CommandContext(ctx, mp, "-q", "-w", wavp, mp3p).Run(); err != nil {
		log.Warnf("mpg123 解码失败: %v", err)
		return nil, false
	}
	if err := exec.CommandContext(ctx, oe, "--quiet", wavp, oggp).Run(); err != nil {
		log.Warnf("opusenc 编码失败: %v", err)
		return nil, false
	}
	out, err := os.ReadFile(oggp)
	if err != nil || len(out) == 0 {
		return nil, false
	}
	return out, true
}
