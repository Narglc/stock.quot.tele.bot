# stock.quot.tele.bot

一个 Telegram 机器人（telebot.v3）。名义上是"股市行情"机器人，当前实际功能：

- **群聊定时提醒**：工作日起床/午饭/下班等（`schedule/`）
- **随机图片 / 表情包**：`/wakeup` 拉随机图、`/sticker` 随机表情，用户发来的图/表情自动收藏进 Redis
- **行情播报 + `/price` 即时查询**：CoinGecko `/coins/markets` 数据（价、1h/24h/7d/30d 涨跌、24h 成交额、距 ATH、市值排名）+ 恐惧贪婪指数（alternative.me）。每小时整点向已注册群播报，`/price [币种...]` 随时查（`stocks/` + `schedule/crypto.go` + `handler/price.go`）
- **MCP 发消息服务**：自己的 agent 可通过 MCP `send_message` 工具发 markdown 消息到 Telegram（`mcpserver/`）
- 已注册群持久化到 Redis，重启自动恢复；bot 被踢自动注销

> 股票行情能力（`stocks/`）已有骨架，`main.go` 中尚未接入。

## 环境要求

- **Go ≥ 1.25.5**（`mark3labs/mcp-go` 的最低要求；`GOTOOLCHAIN=auto` 会自动切换/下载）
- Redis

## 运行与构建

```bash
# 构建
go build -o bot main.go

# 运行（敏感项走环境变量，其余走 config/config.yaml）
TOKEN=<telegram_bot_token> RDB_URL=redis://127.0.0.1:6379 ./bot

# 指定配置文件（默认 ./config/config.yaml）
./bot -f ./config/config.yaml
```

## 配置（viper：文件为主 + 环境变量覆盖）

`config/config.yaml` 为主，**敏感项留空、由环境变量覆盖**（不进仓库）：

| 配置项 | 环境变量 | 说明 |
|---|---|---|
| `telegram.token` | `TOKEN` | bot token（必填） |
| `redis.url` | `RDB_URL` | 如 `redis://127.0.0.1:6379`（必填） |
| `mcp.target_chat_id` | `MCP_TARGET_CHAT_ID` | 设为你的 chat_id 才启用 MCP（0=不启用）；也是免鉴权时的默认发送目标 |
| `mcp.addr` | `MCP_ADDR` | MCP 监听地址，默认 `127.0.0.1:8081`（回环） |
| `mcp.jwt_secret` | `MCP_JWT_SECRET` | 非空则启用 JWT 鉴权；空则免鉴权（仅回环用） |

## MCP 发消息服务

`MCP_TARGET_CHAT_ID` 非零时，bot 同进程启动一个 MCP server（`mark3labs/mcp-go`，Streamable HTTP，endpoint `/mcp`）。

- **tool**：`send_message(platform="telegram", text)` —— `text` 为标准 markdown，server 内转 Telegram HTML 发送（`parse_mode=HTML`），转换失败/发送失败降级为纯文本。
- **多平台预留**：`platform` 走 sender 注册表，目前只实现 `telegram`，将来加 `lark` 只需实现 `sender.Sender` 接口。

### 鉴权（JWT Bearer）

`MCP_JWT_SECRET` 为空 → 免鉴权，**仅适合绑定回环地址 `127.0.0.1`**（自用 agent 走 localhost）。

`MCP_JWT_SECRET` 非空 → 要求请求头 `Authorization: Bearer <jwt>`：
- HS256 对称签名，中间件显式拒 `alg=none`、校验 `exp`；
- **发送目标 = JWT 的 `sub`(chat_id)**（每个 token 决定发给哪个 chat）。

#### 签发 token（`-mktoken`）

```bash
# 用 MCP_JWT_SECRET 签发一个指定 chat_id 的长期 JWT，打印后退出（不启动 bot）
MCP_JWT_SECRET=<你的密钥> ./bot -mktoken <chat_id>
# 输出最后一行即 token
```

### 客户端接入

**Claude Code**（`.mcp.json`，`${VAR}` 环境变量展开，密钥不进文件）：
```json
{
  "mcpServers": {
    "gohome": {
      "type": "http",
      "url": "https://your-host/mcp",
      "headers": { "Authorization": "Bearer ${MCP_TOKEN}" }
    }
  }
}
```

**Anthropic API 的 MCP connector**（`authorization_token` 直接作为 Bearer）：
```json
{ "type": "url", "url": "https://your-host/mcp", "name": "gohome", "authorization_token": "<你的jwt>" }
```

> ⚠️ **claude.ai 网页版 / Claude Desktop 的自定义连接器目前只支持 OAuth，不支持静态 Bearer/自定义 Header**（Anthropic 官方 by design，见 [claude-ai-mcp#10](https://github.com/anthropics/claude-ai-mcp/issues/10)）。本项目的静态 JWT 方案可直接用于 **Claude Code / Claude API**；要接 claude.ai 网页版需在前面加一层 OAuth 网关，或改用上述客户端。

## 云端部署要点

1. **反代 + HTTPS**：MCP server 建议仍绑回环（`MCP_ADDR=127.0.0.1:8081`），前面用 nginx/Caddy 反代到 `https://your-host/mcp`，由反代做 TLS。远程客户端要求 HTTPS。
2. **务必开鉴权**：一旦对公网可达，`MCP_JWT_SECRET` 必须设置（否则任何人都能借 bot 发消息）。密钥用足够长的随机串。
3. 用 `-mktoken <chat_id>` 为每个使用方签发 token，客户端按上面的 `headers` / `authorization_token` 配置携带。

## 架构 / 开发说明

详见 [CLAUDE.md](CLAUDE.md)。设计与实现计划见 `docs/superpowers/`。
