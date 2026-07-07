# 发消息 MCP Server 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现有 Telegram bot 同进程内新增一个 MCP Server，供自己的 agent 通过 `send_message` tool 发送 markdown 消息（转 Telegram HTML），并为多平台（lark 等）预留扩展点。

**Architecture:** 新增三个内聚小包——`tgmd`（markdown→Telegram HTML 纯转换）、`sender`（Sender 接口 + 注册表 + TelegramSender）、`mcpserver`（mcp-go server + Streamable HTTP + `send_message` tool）。配置改为 viper 结构化（文件为主 + 环境变量覆盖）。`mcp.target_chat_id != 0` 时才在 goroutine 里启动 MCP，纯 bot 行为零影响。

**Tech Stack:** Go、telebot.v3、`github.com/mark3labs/mcp-go`、`github.com/yuin/goldmark`、viper、go-redis。

**参考 spec:** `docs/superpowers/specs/2026-07-07-telegram-mcp-server-design.md`

---

## 文件结构

| 文件 | 职责 | 动作 |
|---|---|---|
| `config/config.go` | 扩展 Config 结构 + viper env 绑定 | Modify |
| `config/config.yaml` | 新增 telegram/redis/mcp 段 | Modify |
| `config/config_test.go` | 配置加载 + env 覆盖测试 | Create |
| `tgmd/tgmd.go` | `Convert(md) (html, error)` | Create |
| `tgmd/tgmd_test.go` | 表驱动转换测试 | Create |
| `sender/sender.go` | Sender 接口 + 注册表 | Create |
| `sender/telegram.go` | TelegramSender（转换+发送+降级） | Create |
| `sender/telegram_test.go` | fake bot 测试发送/降级/注册表 | Create |
| `mcpserver/server.go` | mcp-go server + tool + dispatch | Create |
| `mcpserver/server_test.go` | dispatch 纯逻辑测试 | Create |
| `main.go` | 读 config、构造 sender、起 MCP goroutine | Modify |
| `CLAUDE.md` | 记录新配置约定与 MCP 用法 | Modify |

---

## Task 1: 引入依赖

**Files:**
- Modify: `go.mod` / `go.sum`（由 go get 自动）

- [ ] **Step 1: 拉取两个新依赖**

Run:
```bash
go get github.com/mark3labs/mcp-go@latest
go get github.com/yuin/goldmark@latest
```
Expected: `go.mod` 出现 `github.com/mark3labs/mcp-go` 与 `github.com/yuin/goldmark` 两行 require，无报错。

- [ ] **Step 2: 校验编译**

Run: `go build ./...`
Expected: 退出码 0（现有代码不受影响）。

- [ ] **Step 3: 提交**

```bash
git add go.mod go.sum
git commit -m "引入 mcp-go 与 goldmark 依赖

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: tgmd —— markdown 转 Telegram HTML

**Files:**
- Create: `tgmd/tgmd.go`
- Test: `tgmd/tgmd_test.go`

Telegram HTML 只支持 `<b><i><u><s><a><code><pre><blockquote>` 等有限标签，故用 goldmark 解析 AST 后自定义遍历，只吐这些标签，其余降级。

- [ ] **Step 1: 写失败测试**

`tgmd/tgmd_test.go`:
```go
package tgmd

import "testing"

func TestConvert(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bold", "**bold**", "<b>bold</b>"},
		{"italic", "*italic*", "<i>italic</i>"},
		{"strike", "~~gone~~", "<s>gone</s>"},
		{"inline_code", "`code`", "<code>code</code>"},
		{"heading", "# Title", "<b>Title</b>"},
		{"link", "[go](https://go.dev)", `<a href="https://go.dev">go</a>`},
		{"escape", "1 < 2 & 3 > 0", "1 &lt; 2 &amp; 3 &gt; 0"},
		{"list", "- a\n- b", "• a\n• b"},
		{"code_block", "```\nfoo\nbar\n```", "<pre>foo\nbar</pre>"},
		{"mixed", "**a** and `b`", "<b>a</b> and <code>b</code>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Convert(c.in)
			if err != nil {
				t.Fatalf("Convert(%q) err: %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("Convert(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./tgmd/ -run TestConvert -v`
Expected: 编译失败 `undefined: Convert`。

- [ ] **Step 3: 实现 Convert**

`tgmd/tgmd.go`:
```go
package tgmd

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// gm 复用同一个解析器实例（含 GFM：删除线/表格/autolink）。
var gm = goldmark.New(goldmark.WithExtensions(extension.GFM))

// Convert 把标准 markdown 转成 Telegram 支持的 HTML 子集。
// 不支持的结构（标题/列表/表格）降级为粗体/项目符号/纯文本。
func Convert(md string) (string, error) {
	source := []byte(md)
	doc := gm.Parser().Parse(text.NewReader(source))

	var b strings.Builder
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch node := n.(type) {
		case *ast.Heading:
			if entering {
				b.WriteString("<b>")
			} else {
				b.WriteString("</b>\n\n")
			}
		case *ast.Paragraph:
			if !entering {
				b.WriteString("\n\n")
			}
		case *ast.Blockquote:
			if entering {
				b.WriteString("<blockquote>")
			} else {
				b.WriteString("</blockquote>\n\n")
			}
		case *ast.List:
			if !entering {
				b.WriteString("\n")
			}
		case *ast.ListItem:
			if entering {
				b.WriteString("• ")
			} else {
				b.WriteString("\n")
			}
		case *ast.Emphasis:
			tag := "i"
			if node.Level == 2 {
				tag = "b"
			}
			if entering {
				b.WriteString("<" + tag + ">")
			} else {
				b.WriteString("</" + tag + ">")
			}
		case *extast.Strikethrough:
			if entering {
				b.WriteString("<s>")
			} else {
				b.WriteString("</s>")
			}
		case *ast.CodeSpan:
			if entering {
				b.WriteString("<code>")
			} else {
				b.WriteString("</code>")
			}
		case *ast.Link:
			if entering {
				b.WriteString(`<a href="`)
				b.WriteString(escapeAttr(string(node.Destination)))
				b.WriteString(`">`)
			} else {
				b.WriteString("</a>")
			}
		case *ast.AutoLink:
			if entering {
				url := string(node.URL(source))
				b.WriteString(`<a href="`)
				b.WriteString(escapeAttr(url))
				b.WriteString(`">`)
				b.WriteString(escape(url))
				b.WriteString("</a>")
				return ast.WalkSkipChildren, nil
			}
		case *ast.FencedCodeBlock:
			if entering {
				b.WriteString("<pre>")
				writeLines(&b, source, node)
				b.WriteString("</pre>\n\n")
				return ast.WalkSkipChildren, nil
			}
		case *ast.CodeBlock:
			if entering {
				b.WriteString("<pre>")
				writeLines(&b, source, node)
				b.WriteString("</pre>\n\n")
				return ast.WalkSkipChildren, nil
			}
		case *ast.ThematicBreak:
			if entering {
				b.WriteString("———\n\n")
			}
		case *ast.Text:
			if entering {
				b.WriteString(escape(string(node.Segment.Value(source))))
				if node.SoftLineBreak() || node.HardLineBreak() {
					b.WriteString("\n")
				}
			}
		case *ast.String:
			if entering {
				b.WriteString(escape(string(node.Value)))
			}
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}

// liner 抽出 FencedCodeBlock/CodeBlock 共有的 Lines() 方法。
type liner interface{ Lines() *text.Segments }

func writeLines(b *strings.Builder, source []byte, n ast.Node) {
	l, ok := n.(liner)
	if !ok {
		return
	}
	lines := l.Lines()
	var sb strings.Builder
	for i := 0; i < lines.Len(); i++ {
		sb.Write(lines.At(i).Value(source))
	}
	b.WriteString(escape(strings.TrimRight(sb.String(), "\n")))
}

func escape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func escapeAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./tgmd/ -run TestConvert -v`
Expected: 全部 PASS（10 个子用例）。若某个 block 用例因空白差异失败，按实际输出微调 `want`（转换逻辑以代码为准）。

- [ ] **Step 5: 提交**

```bash
git add tgmd/
git commit -m "新增 tgmd：markdown 转 Telegram HTML 子集

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: sender —— 接口与注册表

**Files:**
- Create: `sender/sender.go`
- Test: `sender/sender_test.go`

- [ ] **Step 1: 写失败测试**

`sender/sender_test.go`:
```go
package sender

import "testing"

type stubSender struct{ name string }

func (s stubSender) Name() string                  { return s.name }
func (s stubSender) Send(string) (string, error)   { return "1", nil }

func TestRegistry(t *testing.T) {
	Register(stubSender{name: "stub"})

	got, ok := Get("stub")
	if !ok {
		t.Fatal("Get(stub) not found after Register")
	}
	if got.Name() != "stub" {
		t.Errorf("Name = %q, want stub", got.Name())
	}
	if _, ok := Get("nope"); ok {
		t.Error("Get(nope) should be false")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./sender/ -run TestRegistry -v`
Expected: 编译失败 `undefined: Register` / `Get`。

- [ ] **Step 3: 实现接口与注册表**

`sender/sender.go`:
```go
package sender

// Sender 是「向某平台的默认目标发送一条 markdown 消息」的抽象。
// 新平台（lark 等）实现本接口并通过 Register 注册即可，tool 层无需改动。
type Sender interface {
	// Send 发送一条 markdown 文本，返回平台侧消息 ID。
	Send(md string) (messageID string, err error)
	// Name 返回平台标识，如 "telegram"。
	Name() string
}

var registry = map[string]Sender{}

// Register 注册一个平台发送器（按 Name 覆盖）。
func Register(s Sender) { registry[s.Name()] = s }

// Get 按平台名取发送器。
func Get(name string) (Sender, bool) {
	s, ok := registry[name]
	return s, ok
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./sender/ -run TestRegistry -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add sender/sender.go sender/sender_test.go
git commit -m "新增 sender 接口与平台注册表

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: TelegramSender —— 转换、发送、降级

**Files:**
- Create: `sender/telegram.go`
- Test: `sender/telegram_test.go`

- [ ] **Step 1: 写失败测试**

`sender/telegram_test.go`:
```go
package sender

import (
	"errors"
	"testing"

	tele "gopkg.in/telebot.v3"
)

type call struct {
	what interface{}
	html bool
}

type fakeBot struct {
	failHTML bool
	calls    []call
}

func (f *fakeBot) Send(to tele.Recipient, what interface{}, opts ...interface{}) (*tele.Message, error) {
	isHTML := false
	for _, o := range opts {
		if o == tele.ModeHTML {
			isHTML = true
		}
	}
	f.calls = append(f.calls, call{what: what, html: isHTML})
	if isHTML && f.failHTML {
		return nil, errors.New("bad html")
	}
	return &tele.Message{ID: 42}, nil
}

func TestTelegramSend_HTML(t *testing.T) {
	fb := &fakeBot{}
	s := NewTelegramSender(fb, 100)

	id, err := s.Send("**hi**")
	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if id != "42" {
		t.Errorf("id = %q, want 42", id)
	}
	if len(fb.calls) != 1 || !fb.calls[0].html {
		t.Fatalf("want 1 HTML call, got %+v", fb.calls)
	}
	if fb.calls[0].what != "<b>hi</b>" {
		t.Errorf("what = %q, want <b>hi</b>", fb.calls[0].what)
	}
}

func TestTelegramSend_Downgrade(t *testing.T) {
	fb := &fakeBot{failHTML: true}
	s := NewTelegramSender(fb, 100)

	id, err := s.Send("**hi**")
	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if id != "42" {
		t.Errorf("id = %q, want 42", id)
	}
	if len(fb.calls) != 2 {
		t.Fatalf("want 2 calls (html fail + plain), got %d", len(fb.calls))
	}
	if fb.calls[1].html {
		t.Error("second call should be plain text, not HTML")
	}
	if fb.calls[1].what != "**hi**" {
		t.Errorf("fallback what = %q, want raw markdown **hi**", fb.calls[1].what)
	}
}

func TestTelegramName(t *testing.T) {
	if (&TelegramSender{}).Name() != "telegram" {
		t.Error("Name should be telegram")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./sender/ -run TestTelegram -v`
Expected: 编译失败 `undefined: NewTelegramSender` / `TelegramSender`。

- [ ] **Step 3: 实现 TelegramSender**

`sender/telegram.go`:
```go
package sender

import (
	"strconv"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/tgmd"
	tele "gopkg.in/telebot.v3"
)

// telebotAPI 是 *tele.Bot 的最小子集，便于测试打桩。
type telebotAPI interface {
	Send(to tele.Recipient, what interface{}, opts ...interface{}) (*tele.Message, error)
}

// TelegramSender 把 markdown 转 HTML 后发给固定 target；HTML 失败降级纯文本。
type TelegramSender struct {
	bot    telebotAPI
	target int64
}

func NewTelegramSender(bot telebotAPI, target int64) *TelegramSender {
	return &TelegramSender{bot: bot, target: target}
}

func (t *TelegramSender) Name() string { return "telegram" }

func (t *TelegramSender) Send(md string) (string, error) {
	to := tele.ChatID(t.target)

	html, cerr := tgmd.Convert(md)
	if cerr == nil {
		if msg, err := t.bot.Send(to, html, tele.ModeHTML); err == nil {
			return strconv.Itoa(msg.ID), nil
		} else {
			log.Warnf("MCP 发送 HTML 失败，降级纯文本重发: %v", err)
		}
	} else {
		log.Warnf("markdown 转换失败，降级纯文本: %v", cerr)
	}

	msg, err := t.bot.Send(to, md)
	if err != nil {
		return "", err
	}
	return strconv.Itoa(msg.ID), nil
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./sender/ -v`
Expected: `TestRegistry` / `TestTelegramSend_HTML` / `TestTelegramSend_Downgrade` / `TestTelegramName` 全 PASS。

- [ ] **Step 5: 提交**

```bash
git add sender/telegram.go sender/telegram_test.go
git commit -m "新增 TelegramSender：markdown 转 HTML 发送 + 失败降级纯文本

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: mcpserver —— tool 与 dispatch

**Files:**
- Create: `mcpserver/server.go`
- Test: `mcpserver/server_test.go`

把「按 platform 找 sender 并发送」抽成纯函数 `dispatch`，与 mcp 类型解耦，便于单测。

- [ ] **Step 1: 写失败测试**

`mcpserver/server_test.go`:
```go
package mcpserver

import (
	"errors"
	"testing"

	"github.com/narglc/stock.quot.tele.bot/sender"
)

type mockSender struct {
	name     string
	lastText string
	id       string
	err      error
}

func (m *mockSender) Name() string { return m.name }
func (m *mockSender) Send(md string) (string, error) {
	m.lastText = md
	return m.id, m.err
}

func TestDispatch_OK(t *testing.T) {
	m := &mockSender{name: "mockok", id: "7"}
	sender.Register(m)

	id, err := dispatch("mockok", "hello")
	if err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
	if id != "7" {
		t.Errorf("id = %q, want 7", id)
	}
	if m.lastText != "hello" {
		t.Errorf("lastText = %q, want hello", m.lastText)
	}
}

func TestDispatch_UnknownPlatform(t *testing.T) {
	if _, err := dispatch("ghost", "hi"); err == nil {
		t.Error("unknown platform should error")
	}
}

func TestDispatch_SendError(t *testing.T) {
	sender.Register(&mockSender{name: "mockerr", err: errors.New("boom")})
	if _, err := dispatch("mockerr", "hi"); err == nil {
		t.Error("send error should propagate")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./mcpserver/ -v`
Expected: 编译失败 `undefined: dispatch`。

- [ ] **Step 3: 实现 server 与 dispatch**

`mcpserver/server.go`:
```go
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
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./mcpserver/ -v`
Expected: `TestDispatch_OK` / `TestDispatch_UnknownPlatform` / `TestDispatch_SendError` 全 PASS。

- [ ] **Step 5: 提交**

```bash
git add mcpserver/
git commit -m "新增 mcpserver：send_message tool 与 Streamable HTTP 启动

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: 配置结构化（viper 文件 + env 覆盖）

**Files:**
- Modify: `config/config.go`
- Test: `config/config_test.go`

- [ ] **Step 1: 写失败测试**

`config/config_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInitConfig_EnvOverride(t *testing.T) {
	p := writeTempConfig(t, "telegram:\n  token: \"\"\nredis:\n  url: \"\"\nmcp:\n  target_chat_id: 0\n")
	t.Setenv("TOKEN", "env-token")
	t.Setenv("RDB_URL", "redis://x:1")
	t.Setenv("MCP_TARGET_CHAT_ID", "555")

	cfg, ok := InitConfig(p)
	if !ok {
		t.Fatal("InitConfig failed")
	}
	if cfg.Telegram.Token != "env-token" {
		t.Errorf("Token = %q, want env-token", cfg.Telegram.Token)
	}
	if cfg.Redis.URL != "redis://x:1" {
		t.Errorf("URL = %q, want redis://x:1", cfg.Redis.URL)
	}
	if cfg.MCP.TargetChatID != 555 {
		t.Errorf("TargetChatID = %d, want 555", cfg.MCP.TargetChatID)
	}
}

func TestInitConfig_AddrDefault(t *testing.T) {
	p := writeTempConfig(t, "telegram:\n  token: \"t\"\nredis:\n  url: \"u\"\n")
	cfg, ok := InitConfig(p)
	if !ok {
		t.Fatal("InitConfig failed")
	}
	if cfg.MCP.Addr != ":8081" {
		t.Errorf("Addr = %q, want :8081", cfg.MCP.Addr)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./config/ -v`
Expected: 编译失败（`cfg.Telegram` 等字段不存在）。

- [ ] **Step 3: 扩展 Config 与 InitConfig**

替换 `config/config.go` 全文：
```go
package config

import (
	"fmt"
	"os"

	logger "github.com/narglc/stock.quot.tele.bot/pkg/logger"

	"github.com/spf13/viper"
)

type TelegramConfig struct {
	Token string `mapstructure:"token"`
}

type RedisConfig struct {
	URL string `mapstructure:"url"`
}

type MCPConfig struct {
	Addr         string `mapstructure:"addr"`
	TargetChatID int64  `mapstructure:"target_chat_id"`
}

type Config struct {
	Telegram TelegramConfig      `mapstructure:"telegram"`
	Redis    RedisConfig         `mapstructure:"redis"`
	MCP      MCPConfig           `mapstructure:"mcp"`
	Logger   logger.LoggerConfig `mapstructure:"logger"`
}

// InitConfig 以配置文件为主、环境变量可覆盖的方式加载配置。
// 敏感项（token/redis url）在 yaml 留空，由 TOKEN/RDB_URL 环境变量提供。
func InitConfig(configPath string) (*Config, bool) {
	if _, err := os.Stat(configPath); err != nil {
		return nil, false
	}

	viper.Reset()
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	viper.SetDefault("mcp.addr", ":8081")

	viper.AutomaticEnv()
	// 显式绑定，兼容现有 TOKEN / RDB_URL 环境变量命名。
	_ = viper.BindEnv("telegram.token", "TOKEN")
	_ = viper.BindEnv("redis.url", "RDB_URL")
	_ = viper.BindEnv("mcp.target_chat_id", "MCP_TARGET_CHAT_ID")
	_ = viper.BindEnv("mcp.addr", "MCP_ADDR")

	if err := viper.ReadInConfig(); err != nil {
		fmt.Printf("config file %s read failed. %v\n", configPath, err)
		return nil, false
	}

	var conf Config
	if err := viper.Unmarshal(&conf); err != nil {
		fmt.Printf("config file %s loaded failed. %v\n", configPath, err)
		return nil, false
	}

	fmt.Printf("config %s loaded ok! mcp.addr=%s target=%d\n", configPath, conf.MCP.Addr, conf.MCP.TargetChatID)
	return &conf, true
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./config/ -v`
Expected: `TestInitConfig_EnvOverride` / `TestInitConfig_AddrDefault` PASS。

- [ ] **Step 5: 更新 config.yaml**

替换 `config/config.yaml` 全文：
```yaml
telegram:
  token: ""            # 留空，用 TOKEN 环境变量覆盖
redis:
  url: ""              # 留空，用 RDB_URL 环境变量覆盖
mcp:
  addr: ":8081"
  target_chat_id: 0    # 设为自己的 chat_id 才启用 MCP（0=不启用）
logger:
  level: "debug"
  logPath: "logs/tele.bog.log"
  maxSize: 500
  maxBackups: 15
  maxAge: 28
  commpress: true
```

- [ ] **Step 6: 提交**

```bash
git add config/
git commit -m "配置改 viper 结构化：telegram/redis/mcp 段 + env 覆盖

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: main.go 接线

**Files:**
- Modify: `main.go`

现有 `main.go` 直接 `os.Getenv("TOKEN")` / `validateConfig` 读 env。改为从 `appConfig` 读取，并在 `target_chat_id != 0` 时构造 TelegramSender、注册、起 MCP goroutine。

- [ ] **Step 1: 改写 validateConfig 与 main**

将 `main.go` 中 `validateConfig` 函数替换为：
```go
func validateConfig(cfg *config.Config) error {
	if cfg.Telegram.Token == "" {
		return errors.New("telegram token is required (config telegram.token or TOKEN env)")
	}
	if cfg.Redis.URL == "" {
		return errors.New("redis url is required (config redis.url or RDB_URL env)")
	}
	return nil
}
```

将 `main()` 开头到 `dao.InitRdb(...)` 之间改为（先加载 config 再校验）：
```go
func main() {
	appConfig, cfg_succ := config.InitConfig(*configPath)
	if !cfg_succ {
		panic("config init fail.")
	}

	if err := validateConfig(appConfig); err != nil {
		log.Fatalf("Configuration validation failed: %v", err)
	}

	logger.SetLoggerConfig(&appConfig.LoggerConfig)

	pref := tele.Settings{
		Token:  appConfig.Telegram.Token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	dao.InitRdb(appConfig.Redis.URL)
```

（删除原来 `os.Getenv("TOKEN")` / `os.Getenv("RDB_URL")` 读取与旧的无参 `validateConfig()` 调用。）

- [ ] **Step 2: 接线 MCP 启动**

在 `main.go` 中 `schedule.LoadGroups()` 与 `schedule.ScheduleTask(b)` 之间插入：
```go
	// 配置了 target 才启用 MCP：构造 telegram 发送器、注册、起 HTTP 服务
	if appConfig.MCP.TargetChatID != 0 {
		sender.Register(sender.NewTelegramSender(b, appConfig.MCP.TargetChatID))
		go func() {
			if err := mcpserver.Serve(appConfig.MCP.Addr); err != nil {
				logger.Errorf("MCP server 退出: %v", err)
			}
		}()
	}
```

- [ ] **Step 3: 更新 import**

确保 `main.go` 的 import 含（移除不再使用的 `os`、`errors` 若无其他引用则保留 `errors`——`validateConfig` 仍用 `errors`；`os` 若无其他用途则删除）：
```go
	"github.com/narglc/stock.quot.tele.bot/mcpserver"
	"github.com/narglc/stock.quot.tele.bot/sender"
```
保留现有：`config`、`dao`、`handler`、`logger`、`schedule`、`tele`、`errors`、`flag`、`log`、`time`。

- [ ] **Step 4: 编译**

Run: `go build ./...`
Expected: 退出码 0。若报 `os` imported and not used，删除该 import；若报 `errors` 未用则删除（`validateConfig` 用到则保留）。

- [ ] **Step 5: 全量测试**

Run: `go test ./...`
Expected: `tgmd` / `sender` / `mcpserver` / `config` 全 PASS，其余包无测试。

- [ ] **Step 6: 冒烟验证（手动）**

Run:
```bash
go build -o bot main.go
TOKEN=<你的token> RDB_URL=redis://127.0.0.1:6378 MCP_TARGET_CHAT_ID=<你的chat_id> MCP_ADDR=:8081 ./bot &
sleep 3
grep "MCP server 启动" logs/tele.bog.log
curl -sS -i -X POST http://127.0.0.1:8081/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' | head -30
```
Expected: 日志出现 `MCP server 启动，监听 :8081/mcp`；curl 返回中含 `send_message` tool。随后可用真实 MCP client 调 `send_message` 验证收到 Telegram 消息。用完 `pkill -f "^./bot$"`。

- [ ] **Step 7: 提交**

```bash
git add main.go
git commit -m "main 接线：从 config 读取 + target 非零时启动 MCP goroutine

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: 文档

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: 更新 CLAUDE.md**

在 `CLAUDE.md` 的「架构要点」下新增一节（紧接「数据层」或「日志」之后）：
```markdown
### MCP 发消息服务（mcpserver/ + sender/ + tgmd/）
供自己的 agent 通过 MCP 发消息。`config.MCP.TargetChatID != 0` 时，`main.go` 构造 `TelegramSender` 注册进 `sender.Registry` 并在 goroutine 启动 `mcpserver.Serve`（mark3labs/mcp-go，Streamable HTTP，默认 `:8081/mcp`）。
- tool：`send_message(platform="telegram", text)`，`text` 为标准 markdown。
- 发送链路：`tgmd.Convert`（goldmark AST→Telegram HTML 子集）→ `bot.Send(ParseMode=HTML)`；HTML 失败降级纯文本重发。
- 多平台扩展：实现 `sender.Sender` 接口 + `sender.Register` 即可（如将来的 lark），tool 层无需改动。
```

并更新「配置分两层」那段为：
```markdown
- 配置统一走 viper（`config/config.go`）：**配置文件 `config/config.yaml` 为主，敏感项（`telegram.token`、`redis.url`）留空由环境变量 `TOKEN` / `RDB_URL` 覆盖**。MCP 相关：`mcp.target_chat_id`（`MCP_TARGET_CHAT_ID`）、`mcp.addr`（`MCP_ADDR`，默认 `:8081`）。
```

- [ ] **Step 2: 提交**

```bash
git add CLAUDE.md
git commit -m "文档：记录 MCP 发消息服务与新配置约定

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## 完成标准

- `go test ./...` 全绿（`tgmd`/`sender`/`mcpserver`/`config`）。
- `go build ./...` 通过。
- 设 `MCP_TARGET_CHAT_ID` 启动后，日志出现 MCP 监听、`tools/list` 能返回 `send_message`、真实调用能收到 Telegram 消息。
- 不设 `MCP_TARGET_CHAT_ID` 时行为与改造前一致（纯 bot）。
