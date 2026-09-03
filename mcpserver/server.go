package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/sender"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	"github.com/narglc/stock.quot.tele.bot/tts"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// maxTTSChunk 单段 TTS 的最大字符数：超长文本按句子切分、逐条发送语音。
const maxTTSChunk = 1500

// shutdownTimeout 是优雅关停时等待在途请求的上限。
const shutdownTimeout = 5 * time.Second

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
			capText := ""                     // caption 只挂首条，避免多段时重复刷屏
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

// liqmapResult 是 get_liqmap 返回给 agent 的结构化清算数据。
// 刻意不返回 PNG：agent 要的是能参与推理的数字，图给人看就够了。
type liqmapResult struct {
	Symbol       string       `json:"symbol"`
	CurrentPrice float64      `json:"current_price"`
	Interval     string       `json:"interval"`
	MaxValue     float64      `json:"max_value"`
	Above        []liqCluster `json:"above"` // 现价上方：空头爆仓区
	Below        []liqCluster `json:"below"` // 现价下方：多头爆仓区
	Note         string       `json:"note"`
}

type liqCluster struct {
	Price float64 `json:"price"`
	Value float64 `json:"value_usd"`
}

// makeLiqmapHandler 构造 get_liqmap 的 handler：返回结构化清算簇数据。
// apifyToken 为空时该工具不注册，所以这里不必再判空。
func makeLiqmapHandler(apifyToken string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		symbol := req.GetString("symbol", "BTC")
		topN := req.GetInt("top_n", 10)
		if topN <= 0 || topN > 50 {
			topN = 10
		}

		m, err := stocks.GetLiqMap(symbol, apifyToken)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		res := liqmapResult{
			Symbol:       m.Symbol,
			CurrentPrice: m.CurrentPrice,
			Interval:     m.Interval,
			MaxValue:     m.MaxValue,
			Above:        topClusters(m.Above, topN),
			Below:        topClusters(m.Below, topN),
			// 双边都返回是有意的：只看一边会得出无法证伪的方向性结论。
			Note: "above=现价上方空头爆仓区(价格上行会去扫)，below=现价下方多头爆仓区(价格下行会去扫)；" +
				"两侧均按名义额降序取前 N。数据粒度见 interval，来源 CoinAnk，带 10 分钟缓存。",
		}

		payload, err := json.Marshal(res)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(payload)), nil
	}
}

// topClusters 取前 n 个簇并转成对外的 JSON 结构。
func topClusters(cs []stocks.LiqCluster, n int) []liqCluster {
	if len(cs) > n {
		cs = cs[:n]
	}
	out := make([]liqCluster, 0, len(cs))
	for _, c := range cs {
		out = append(out, liqCluster{Price: c.Price, Value: c.Value})
	}
	return out
}

// Serve 阻塞式启动 Streamable HTTP MCP server（endpoint /mcp）。
// jwtSecret 非空则启用 JWT Bearer 鉴权，发送目标取 token 里的 chat_id；
// 为空则免鉴权，发送目标用 defaultTarget。
// synth 非 nil 时额外注册 send_voice 工具（文本转语音发送）。
// apifyToken 非空时额外注册 get_liqmap 工具（清算图数据查询）。
// ctx 取消时优雅关停（等待在途请求，最多 shutdownTimeout）。
// newMCPServer 组装 MCP server 并注册工具：send_message 恒有，
// send_voice 需 synth，get_liqmap 需 apifyToken。抽出来是为了让测试能直接起 httptest。
func newMCPServer(defaultTarget int64, synth tts.Synthesizer, apifyToken string) *server.MCPServer {
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

	if apifyToken != "" {
		liqTool := mcp.NewTool("get_liqmap",
			mcp.WithDescription("查询某币种的清算地图：现价上方(空头爆仓区)与下方(多头爆仓区)的清算簇，"+
				"返回结构化 JSON。数据来自 CoinAnk 热力图最新时间列，带 10 分钟缓存。"),
			mcp.WithString("symbol", mcp.Description("币种符号，如 BTC / ETH / SOL，默认 BTC")),
			mcp.WithNumber("top_n", mcp.Description("上/下方各返回多少个簇（按名义额降序），默认 10，上限 50")),
		)
		s.AddTool(liqTool, makeLiqmapHandler(apifyToken))
		log.Infof("MCP get_liqmap 已启用")
	}

	return s
}

func Serve(ctx context.Context, addr, jwtSecret string, defaultTarget int64, synth tts.Synthesizer, apifyToken string) error {
	s := newMCPServer(defaultTarget, synth, apifyToken)

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

	// 用显式的 http.Server 而非 ListenAndServe：一是能优雅关停，
	// 二是 ReadHeaderTimeout 挡住 Slowloris（配了 caddy 反代就意味着可能公网可达）。
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// ctx 取消时关停；ListenAndServe 会随之返回 ErrServerClosed。
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Warnf("MCP server 关停超时: %v", err)
		}
	}()

	log.Infof("MCP server 启动，监听 %s/mcp（鉴权:%v）", addr, jwtSecret != "")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Infof("MCP server 已关停")
	return nil
}
