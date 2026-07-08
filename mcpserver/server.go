package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/sender"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// dispatch 按平台取发送器并发往 recipient，纯逻辑、与 mcp 类型解耦，便于测试。
func dispatch(platform, text, recipient string) (string, error) {
	snd, ok := sender.Get(platform)
	if !ok {
		return "", fmt.Errorf("unknown platform: %s", platform)
	}
	return snd.Send(text, recipient)
}

// makeSendHandler 构造 send_message 的 handler。发送目标优先取鉴权 JWT 注入的 chat_id，
// 无鉴权时回落到配置的 defaultTarget。
func makeSendHandler(defaultTarget int64) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		text, err := req.RequireString("text")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		platform := req.GetString("platform", "telegram")

		target := defaultTarget
		if v, ok := ctx.Value(chatIDKey).(int64); ok && v != 0 {
			target = v
		}
		if target == 0 {
			return mcp.NewToolResultError("no target: 未鉴权且未配置默认目标"), nil
		}
		recipient := strconv.FormatInt(target, 10)

		id, err := dispatch(platform, text, recipient)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("ok platform=%s recipient=%s message_id=%s", platform, recipient, id)), nil
	}
}

// Serve 阻塞式启动 Streamable HTTP MCP server（endpoint /mcp）。
// jwtSecret 非空则启用 JWT Bearer 鉴权，发送目标取 token 里的 chat_id；
// 为空则免鉴权，发送目标用 defaultTarget。
func Serve(addr, jwtSecret string, defaultTarget int64) error {
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
