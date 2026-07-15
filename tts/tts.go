// Package tts 提供文本转语音的可插拔抽象。
// 上层（MCP send_voice）只依赖 Synthesizer 接口，切换实现（azure/edge/…）
// 只需改配置 tts.provider，代码零改动。
package tts

import (
	"context"
	"strings"
)

// Audio 是合成出的音频。Format 决定发送方式：
//   - "ogg"（Ogg/Opus）→ Telegram 语音气泡 sendVoice
//   - "mp3"            → Telegram 音频文件 sendAudio
type Audio struct {
	Data   []byte
	Format string
}

// Synthesizer 把文本合成为音频。实现可插拔，上层只依赖此接口。
type Synthesizer interface {
	Synthesize(ctx context.Context, text string) (*Audio, error)
	Name() string
}

var registry = map[string]Synthesizer{}

// Register 注册一个合成器（按 Name 覆盖）。
func Register(s Synthesizer) { registry[s.Name()] = s }

// Get 按名取合成器。
func Get(name string) (Synthesizer, bool) {
	s, ok := registry[name]
	return s, ok
}

// SplitText 把长文本按句子边界切成不超过 maxRunes 的片段，供分段合成、逐条发送。
// 这样既回避单次请求超限，也回避 Ogg 流拼接的麻烦（每段单独发一条语音）。
// maxRunes<=0 或文本本身不超限时，原样返回单段。
func SplitText(text string, maxRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxRunes <= 0 || len([]rune(text)) <= maxRunes {
		return []string{text}
	}

	var chunks []string
	var cur strings.Builder
	curLen := 0
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			chunks = append(chunks, s)
		}
		cur.Reset()
		curLen = 0
	}
	for _, s := range splitSentences(text) {
		sl := len([]rune(s))
		if sl > maxRunes { // 单句超长：先冲掉累积，再硬切这句
			flush()
			for _, part := range hardSplit(s, maxRunes) {
				chunks = append(chunks, part)
			}
			continue
		}
		if curLen > 0 && curLen+sl > maxRunes {
			flush()
		}
		cur.WriteString(s)
		curLen += sl
	}
	flush()
	return chunks
}

// splitSentences 按中英文句末标点/换行切句（保留标点）。
func splitSentences(text string) []string {
	var out []string
	var cur []rune
	for _, r := range text {
		cur = append(cur, r)
		switch r {
		case '。', '！', '？', '；', '\n', '.', '!', '?', ';':
			out = append(out, string(cur))
			cur = cur[:0]
		}
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

// hardSplit 按 rune 数硬切超长单句。
func hardSplit(s string, maxRunes int) []string {
	r := []rune(s)
	var out []string
	for i := 0; i < len(r); i += maxRunes {
		end := min(i+maxRunes, len(r))
		if seg := strings.TrimSpace(string(r[i:end])); seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// truncate 截断字符串用于错误日志。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
