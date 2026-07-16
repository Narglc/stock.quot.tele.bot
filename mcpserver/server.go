package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/sender"
	"github.com/narglc/stock.quot.tele.bot/tts"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// maxTTSChunk 单段 TTS 的最大字符数：超长文本按句子切分、逐条发送语音。
const maxTTSChunk = 1500

// dispatch 按平台取发送器并发往 recipient，纯逻辑、与 mcp 类型解耦，便于测试。
func dispatch(platform, text, recipient string) (string, error) {
	snd, ok := sender.Get(platform)
	if !ok {
		return "", fmt.Errorf("unknown platform: %s", platform)
	}
	return snd.Send(text, recipient)
}

// resolveRecipient 计算发送目标：优先取鉴权 JWT 注入的 chat_id，无鉴权时回落 defaultTarget。
func resolveRecipient(ctx context.Context, defaultTarget int64) (string, bool) {
	target := defaultTarget
	if v, ok := ctx.Value(chatIDKey).(int64); ok && v != 0 {
		target = v
	}
	if target == 0 {
		return "", false
	}
	return strconv.FormatInt(target, 10), true
}

// makeSendHandler 构造 send_message 的 handler。
func makeSendHandler(defaultTarget int64) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		text, err := req.RequireString("text")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		platform := req.GetString("platform", "telegram")

		recipient, ok := resolveRecipient(ctx, defaultTarget)
		if !ok {
			return mcp.NewToolResultError("no target: 未鉴权且未配置默认目标"), nil
		}

		id, err := dispatch(platform, text, recipient)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("ok platform=%s recipient=%s message_id=%s", platform, recipient, id)), nil
	}
}

// makeVoiceHandler 构造 send_voice 的 handler：把文本 TTS 后作为语音发送。
// 长文本按句子切分、逐条发送；某段 TTS 失败时降级为发该段纯文本，保证内容不丢。
func makeVoiceHandler(defaultTarget int64, synth tts.Synthesizer) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		text, err := req.RequireString("text")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		platform := req.GetString("platform", "telegram")
		caption := req.GetString("caption", "") // 可选：附带在（首条）语音气泡旁的文本

		recipient, ok := resolveRecipient(ctx, defaultTarget)
		if !ok {
			return mcp.NewToolResultError("no target: 未鉴权且未配置默认目标"), nil
		}

		snd, ok := sender.Get(platform)
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("unknown platform: %s", platform)), nil
		}
		vs, ok := snd.(sender.VoiceSender)
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("platform %s 不支持语音发送", platform)), nil
		}

		chunks := tts.SplitText(text, maxTTSChunk)
		if len(chunks) == 0 {
			return mcp.NewToolResultError("text 为空"), nil
		}

		var ids []string
		for i, chunk := range chunks {
			audio, serr := synth.Synthesize(ctx, chunk)
			if serr != nil {
				// TTS 失败降级：把该段当纯文本发出去，内容不丢。
				log.Warnf("TTS 第%d段失败，降级发文本: %v", i+1, serr)
				if id, terr := snd.Send(chunk, recipient); terr == nil {
					ids = append(ids, id+"(text)")
					continue
				} else {
					return mcp.NewToolResultError(fmt.Sprintf("TTS 及文本降级均失败: %v / %v", serr, terr)), nil
				}
			}
			audio = tts.ToOggOpus(ctx, audio) // 有必要才转码（Azure 已是 ogg 直接过；edge 的 mp3 转 ogg 发语音气泡）
			capText := "" // caption 只挂首条，避免多段时重复刷屏
			if i == 0 {
				capText = caption
			}
			id, verr := vs.SendVoice(audio.Data, audio.Format, capText, recipient)
			if verr != nil {
				return mcp.NewToolResultError(verr.Error()), nil
			}
			ids = append(ids, id)
		}
		return mcp.NewToolResultText(fmt.Sprintf("ok voice platform=%s recipient=%s segments=%d message_ids=%s",
			platform, recipient, len(ids), strings.Join(ids, ","))), nil
	}
}

// Serve 阻塞式启动 Streamable HTTP MCP server（endpoint /mcp）。
// jwtSecret 非空则启用 JWT Bearer 鉴权，发送目标取 token 里的 chat_id；
// 为空则免鉴权，发送目标用 defaultTarget。
// synth 非 nil 时额外注册 send_voice 工具（文本转语音发送）。
func Serve(addr, jwtSecret string, defaultTarget int64, synth tts.Synthesizer) error {
	s := server.NewMCPServer(
		"gohome-bot",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	tool := mcp.NewTool("send_message",
		mcp.WithDescription("发送一条 markdown 消息（目标为鉴权 JWT 的 chat_id，或配置的默认目标）"),
		mcp.WithString("platform", mcp.Description("目标平台，默认 telegram")),
		mcp.WithString("text", mcp.Required(), mcp.Description("markdown 文本")),
	)
	s.AddTool(tool, makeSendHandler(defaultTarget))

	if synth != nil {
		voiceTool := mcp.NewTool("send_voice",
			mcp.WithDescription("把文本转成语音发送（TTS，目标同 send_message；长文本自动分段逐条发）"),
			mcp.WithString("platform", mcp.Description("目标平台，默认 telegram")),
			mcp.WithString("text", mcp.Required(), mcp.Description("要朗读的文本")),
			mcp.WithString("caption", mcp.Description("可选：附带在语音气泡旁的文本（≤1024 字符，只挂首条）")),
		)
		s.AddTool(voiceTool, makeVoiceHandler(defaultTarget, synth))
		log.Infof("MCP send_voice 已启用（TTS provider=%s）", synth.Name())
	}

	// 把鉴权中间件注入的 chat_id 从 request context 透传到 tool handler 的 context。
	httpServer := server.NewStreamableHTTPServer(s,
		server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			if v := r.Context().Value(chatIDKey); v != nil {
				return context.WithValue(ctx, chatIDKey, v)
			}
			return ctx
		}),
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", withAuth(jwtSecret, httpServer))

	log.Infof("MCP server 启动，监听 %s/mcp（鉴权:%v）", addr, jwtSecret != "")
	return http.ListenAndServe(addr, mux)
}
