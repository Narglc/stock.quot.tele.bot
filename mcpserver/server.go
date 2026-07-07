package mcpserver

import (
	"context"
	"fmt"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/sender"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// dispatch 按平台取发送器并发送，纯逻辑、与 mcp 类型解耦，便于测试。
func dispatch(platform, text string) (string, error) {
	snd, ok := sender.Get(platform)
	if !ok {
		return "", fmt.Errorf("unknown platform: %s", platform)
	}
	return snd.Send(text)
}

// handleSendMessage 是 send_message 的 mcp tool handler。
func handleSendMessage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	text, err := req.RequireString("text")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	platform := req.GetString("platform", "telegram")

	id, err := dispatch(platform, text)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("ok platform=%s message_id=%s", platform, id)), nil
}

// Serve 阻塞式启动 Streamable HTTP MCP server（endpoint 默认 /mcp）。
func Serve(addr string) error {
	s := server.NewMCPServer(
		"gohome-bot",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	tool := mcp.NewTool("send_message",
		mcp.WithDescription("发送一条 markdown 消息到默认目标（自己）"),
		mcp.WithString("platform", mcp.Description("目标平台，默认 telegram")),
		mcp.WithString("text", mcp.Required(), mcp.Description("markdown 文本")),
	)
	s.AddTool(tool, handleSendMessage)

	log.Infof("MCP server 启动，监听 %s/mcp", addr)
	return server.NewStreamableHTTPServer(s).Start(addr)
}
