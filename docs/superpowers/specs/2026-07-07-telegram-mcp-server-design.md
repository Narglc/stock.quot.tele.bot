# 设计：发消息 MCP Server（Telegram，预留多平台）

- 日期：2026-07-07
- 分支：gohomebot
- 状态：待评审

## 1. 背景与目标

当前项目是一个 telebot.v3 的 Telegram 机器人，已具备发送消息能力（`b.Send`）、Redis 存储、以及持久化的已注册 chat 列表（`GroupMap`）。

目标：在**同一进程**内新增一个 MCP（Model Context Protocol）Server，供**自己使用**的第三方 agent（Claude / Cursor 等 MCP client）通过标准 tool 调用来发送消息。第一期只做 Telegram，接口为将来接入 lark 等平台预留扩展点。

### 目标（In Scope）
- 通过 MCP tool `send_message` 发送一条 **markdown** 消息，默认发给「自己」。
- markdown 在 server 内转换为 Telegram 支持的 HTML 后发送（`parse_mode=HTML`）。
- `platform` 参数预留多平台，第一期只实现 `telegram`。
- 配置文件为主（viper），敏感项支持环境变量覆盖。

### 非目标（Out of Scope，YAGNI）
- 鉴权 / 多租户 / 限流（自用，暂不做）。
- 给任意第三方 chat 发消息（只发默认 target）。
- lark 等其他平台的**实现**（仅预留接口）。
- 富媒体（图片 / 文件 / sticker）——第一期只发文本。

## 2. 已定决策汇总

| 维度 | 选择 |
|---|---|
| MCP SDK | `github.com/mark3labs/mcp-go` |
| 传输 | Streamable HTTP，endpoint `/mcp` |
| 消息格式 | 标准 markdown → Telegram HTML（`parse_mode=HTML`） |
| markdown 转换 | `github.com/yuin/goldmark` 解析 AST + 自定义 renderer |
| 鉴权 | 无（自用） |
| 默认 target | 配置项 `mcp.target_chat_id`，默认发给自己 |
| 进程结构 | 与现有 bot 同进程，MCP HTTP 跑在独立 goroutine |
| tool | `send_message(platform="telegram", text)` |
| 多平台 | sender 注册表（`map[string]Sender` + init 自注册） |
| 配置 | viper 配置文件为主 + 环境变量可覆盖 |

## 3. 架构与组件

```
main.go
 ├─ config.InitConfig()          扩展后的 Config（telegram/redis/mcp/logger）
 ├─ 初始化 logger / dao / bot（现有）
 ├─ if cfg.MCP.TargetChatID != 0:
 │     go mcpserver.Serve(cfg.MCP, senderRegistry)   ← 新增 goroutine
 └─ b.Start()                    现有，阻塞收消息

新增包：
 • tgmd/       纯函数 Convert(md string) (string, error)：goldmark AST → Telegram HTML
 • sender/     Sender 接口 + 注册表 + TelegramSender 实现
 • mcpserver/  构建 mcp-go server + Streamable HTTP + 注册 send_message tool
```

各单元职责与边界：

### 3.1 `tgmd`（markdown → Telegram HTML，核心可测单元）
- 对外只有 `Convert(md string) (string, error)`，无外部业务依赖。
- 用 goldmark 解析成 AST，自定义 renderer **只输出 Telegram 支持的标签**，其余降级：
  - `#/##/### 标题` → `<b>…</b>`
  - `**加粗**` → `<b>`；`*斜体*` → `<i>`；`~~删除线~~` → `<s>`
  - `` `行内代码` `` → `<code>`；围栏代码块 → `<pre>`
  - `[文本](url)` → `<a href="url">文本</a>`
  - `- / * / 1. 列表` → `• 文本\n`（纯文本项目符号）
  - `> 引用` → `<blockquote>`（Telegram 支持）
  - 表格 / 图片等不支持结构 → 降级为纯文本或 `<pre>` 兜底
  - 文本节点转义 `<`、`>`、`&`
- 理由：Telegram HTML 仅支持 `<b><i><u><s><a><code><pre><blockquote><span class="tg-spoiler">` 等有限标签；通用 markdown→HTML 会产出 `<h1><ul><table>` 等不被接受的标签。Go 生态无成熟的「markdown→Telegram-safe」库，故自实现。

### 3.2 `sender`（多平台发送抽象）
```go
type Sender interface {
    // Send 发送一条已按目标平台格式化好的消息到默认 target。
    Send(text string) (messageID string, err error)
    // Name 返回平台标识，如 "telegram"。
    Name() string
}
```
- 全局注册表 `var Registry = map[string]Sender{}`，各实现在 `init()` 里自注册（与项目现有 `AllRandomPicSrv` 模式一致）。
- 第一期只实现 `TelegramSender`：
  - 持有现有 `*telebot.Bot` 实例与 `targetChatID`。
  - `Send` 内部：`tgmd.Convert(md)` → `bot.Send(ChatID(target), html, ParseMode=HTML)`；转换或发送失败按「§6 错误处理」降级。
- `lark` 将来只需新增文件实现 `Sender` 并 `Registry["lark"]=…`，tool 层零改动。

### 3.3 `mcpserver`（MCP 服务）
- 用 mark3labs/mcp-go 构建 server，注册 tool `send_message`。
- 以 Streamable HTTP 在 `cfg.MCP.Addr`（默认 `:8081`）的 `/mcp` 暴露。
- tool handler 从注册表按 `platform` 取 `Sender` 并调用，不直接耦合 telebot。

## 4. MCP Tool 定义

```
send_message
  参数：
    platform: string  枚举，默认 "telegram"（预留 "lark" 等）
    text:     string  必填，标准 markdown
  返回：
    成功 → { ok: true, platform, message_id }
    失败 → tool error（含原因，如 unknown platform / send failed）
```
- 无 `target` 参数：默认发给 `mcp.target_chat_id`（你自己）。

## 5. 配置设计（viper：文件为主 + env 覆盖）

扩展 `config.Config`：
```go
type Config struct {
    Telegram TelegramConfig      `mapstructure:"telegram"`
    Redis    RedisConfig         `mapstructure:"redis"`
    MCP      MCPConfig           `mapstructure:"mcp"`
    Logger   logger.LoggerConfig `mapstructure:"logger"`
}
type TelegramConfig struct { Token string `mapstructure:"token"` }
type RedisConfig    struct { URL   string `mapstructure:"url"` }
type MCPConfig struct {
    Addr         string `mapstructure:"addr"`           // 默认 ":8081"
    TargetChatID int64  `mapstructure:"target_chat_id"` // 0 = 不启用 MCP
}
```

`config.yaml`（敏感项留空，走 env 覆盖，可继续被 git 追踪而不泄露）：
```yaml
telegram:
  token: ""            # 用 TOKEN 环境变量覆盖
redis:
  url: ""              # 用 RDB_URL 环境变量覆盖
mcp:
  addr: ":8081"
  target_chat_id: 0    # 设为自己的 chat_id 才启用 MCP
logger:
  level: "debug"
  logPath: "logs/tele.bog.log"
  maxSize: 500
  maxBackups: 15
  maxAge: 28
  commpress: true
```

环境变量覆盖（`viper.AutomaticEnv` + 显式 `BindEnv` 保证向后兼容）：

| 配置项 | 环境变量 | 说明 |
|---|---|---|
| `telegram.token` | `TOKEN` | 向后兼容现有变量 |
| `redis.url` | `RDB_URL` | 向后兼容现有变量 |
| `mcp.target_chat_id` | `MCP_TARGET_CHAT_ID` | 可选 |
| `mcp.addr` | `MCP_ADDR` | 可选 |

- `validateConfig` 改为校验**合并后**的 `cfg.Telegram.Token` / `cfg.Redis.URL` 非空（而非直接读 env）。
- 向后兼容：现有仅用 `TOKEN`/`RDB_URL` 环境变量的启动方式（含 `.vscode/launch.json`）继续可用。

## 6. 错误处理

- **HTML 发送失败降级**：转换产出的 HTML 若被 Telegram 拒（HTTP 400 解析错误），自动去掉 `parse_mode`，用**原始 markdown 纯文本**重发——保证消息送达，仅丢格式。
- `tgmd.Convert` 自身返回 error → 同样降级为纯文本发送。
- 其他 `bot.Send` 错误（网络等）→ `Sender.Send` 返回 error，tool handler 转成 MCP tool error 回给 agent。
- 未知 `platform` → tool 返回明确 error。

## 7. 测试策略

- `tgmd.Convert`：**表驱动单元测试**，覆盖标题/加粗/斜体/删除线/行内码/代码块/链接/列表/引用/嵌套，以及 `< > &` 转义、代码块内特殊字符等边界。这是本项目质量重心。
- `sender`：用 mock bot（或对 `telebot.Bot` 的最小接口打桩）验证「转换→发送」链路与降级逻辑，不打真实 Telegram。
- `mcpserver` tool handler：用 mock `Sender` 验证 platform 路由、参数校验、错误映射。

## 8. 新增依赖

- `github.com/mark3labs/mcp-go`
- `github.com/yuin/goldmark`

## 9. 向后兼容与影响面

- MCP 是**可选特性**：`mcp.target_chat_id == 0` 时完全不启动 MCP goroutine，退化为纯 bot，现有行为零影响。
- 配置改为 viper 结构化 + env 覆盖，不改变现有 env 启动方式。
- 不改动现有 handler / schedule / 图源逻辑。

## 10. 实现步骤概要（细化留给 writing-plans）

1. 扩展 `config` 包：新增 struct 段 + viper env 绑定；`main.go` 改从 config 读取并校验。
2. 新增 `tgmd` 包 + 表驱动测试。
3. 新增 `sender` 包：接口 + 注册表 + `TelegramSender` + 测试。
4. 新增 `mcpserver` 包：mcp-go server + `send_message` tool + Streamable HTTP。
5. `main.go` 接线：`target_chat_id` 非零时 `go mcpserver.Serve(...)`。
6. 更新 `config.yaml`、`CLAUDE.md`（记录新配置约定与 MCP 用法）。
