# stock.quot.tele.bot

一个 Telegram 机器人（telebot.v3）。名义上是"股市行情"机器人，当前实际功能：

- **群聊定时提醒**：工作日起床/午饭/下班等（`schedule/`）
- **随机图片 / 表情包**：`/wakeup` 拉随机图、`/sticker` 随机表情，用户发来的图/表情自动收藏进 Redis
- **行情播报 + `/price` 即时查询 + inline 查询**：CoinGecko `/coins/markets`（价、1h/24h/7d/30d 涨跌、高低、成交额、距 ATH、市值）+ 恐惧贪婪指数（alternative.me）+ OKX 衍生品（持仓量及变化、多空比、资金费率、持仓/价格组合解读）+ 全市场概览（总市值、BTC.D/ETH.D）。**整点播报采用"原地编辑同一条消息"刷新，不刷屏**；`/price [币种...]` 随时查；inline 输入 `@bot [币种...]` 也能查（需 @BotFather `/setinline` 开启）。
- **清算图 `/liqmap [币种]`**：拉 CoinAnk 清算热力图，纯 Go 渲染「热力图 + 清算地图」两张 PNG 相册（`stocks/liqmap.go` + `stocks/liqrender.go`）；配 `APIFY_TOKEN` 后还会早八/晚八各定时播报一次 BTC。
- **娱乐 `/dice`**：Telegram 动画骰子/飞镖/老虎机等。
- **MCP 发消息服务**：agent 通过 MCP `send_message`（markdown）发消息（`mcpserver/`）。
- **语音播报（TTS）**：MCP `send_voice`（可带 `caption`）把文本转语音发到 Telegram（`tts/`）；TTS 可插拔（Azure / edge-tts / http 委托本地 worker），换 `TTS_PROVIDER` 即切换。
- 已注册群、整点消息 id 持久化到 Redis，重启自动恢复；bot 被踢自动注销；SIGINT/SIGTERM 优雅退出。

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

启用 **MCP + 语音播报**（`send_voice` 需 `TTS_PROVIDER`）：

```bash
# edge-tts（免费、无 key；语音气泡需装 mpg123+opus-tools 或 ffmpeg 做转码）
TOKEN=<token> RDB_URL=redis://127.0.0.1:6379 \
MCP_TARGET_CHAT_ID=<你的chat_id> \
TTS_PROVIDER=edge EDGE_TTS_VOICE=zh-CN-XiaoyiNeural \
./bot

# 或 Azure（要 key/region；ogg 直出，免转码）
TOKEN=<token> RDB_URL=redis://127.0.0.1:6379 \
MCP_TARGET_CHAT_ID=<你的chat_id> \
TTS_PROVIDER=azure AZURE_TTS_KEY=<key> AZURE_TTS_REGION=eastasia \
./bot
```

> `TTS_PROVIDER` 留空则不启用语音，`send_voice` 工具不注册；`send_message` 不受影响。

## 配置（viper：文件为主 + 环境变量覆盖）

`config/config.yaml` 为主，**敏感项留空、由环境变量覆盖**（不进仓库）：

| 配置项 | 环境变量 | 说明 |
|---|---|---|
| `telegram.token` | `TOKEN` | bot token（必填） |
| `redis.url` | `RDB_URL` | 如 `redis://127.0.0.1:6379`（必填） |
| `mcp.target_chat_id` | `MCP_TARGET_CHAT_ID` | 设为你的 chat_id 才启用 MCP（0=不启用）；也是免鉴权时的默认发送目标 |
| `mcp.addr` | `MCP_ADDR` | MCP 监听地址，默认 `127.0.0.1:8081`（回环） |
| `mcp.jwt_secret` | `MCP_JWT_SECRET` | 非空则启用 JWT 鉴权；空则免鉴权（仅回环用） |
| `tts.provider` | `TTS_PROVIDER` | 语音引擎：`azure` / `edge`；**留空=不启用语音**（`send_voice` 不注册） |
| `tts.azure.key` | `AZURE_TTS_KEY` | Azure Speech 密钥（provider=azure 时必填） |
| `tts.azure.region` | `AZURE_TTS_REGION` | Azure 区域，如 `eastasia` / `eastus` |
| `tts.azure.voice` | `AZURE_TTS_VOICE` | 声色，默认 `zh-CN-XiaoxiaoNeural` |
| `tts.edge.voice` | `EDGE_TTS_VOICE` | edge-tts 声色，默认 `zh-CN-XiaoxiaoNeural` |
| `tts.http.url` | `TTS_HTTP_URL` | 委托外部 HTTP TTS（provider=http 时），如本地 worker 经 ssh -R 映射的 `http://127.0.0.1:5000/tts` |
| `tts.http.token` | `TTS_HTTP_TOKEN` | 与 worker 约定的共享密钥（可空） |
| `apify.token` | `APIFY_TOKEN` | 清算图（/liqmap 与早八晚八 cron）数据源，留空则不可用 |

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

## 语音播报（TTS）

`TTS_PROVIDER` 非空时，MCP 额外注册 **`send_voice(platform="telegram", text)`** 工具：把文本转语音后发到 Telegram（目标同 `send_message`，走 JWT 的 chat_id 或默认目标）。长文本按句子自动分段、逐条发送；某段 TTS 失败自动降级为发该段纯文本，内容不丢。

- **可插拔**：TTS 走 `tts.Synthesizer` 接口，换 `TTS_PROVIDER` 即切换，上层零改动。新增引擎只需实现该接口 + 在 `main.go:buildSynthesizer` 加一个 case。

| 引擎 | key | 输出 | 转码 | 适用 |
|---|---|---|---|---|
| `azure` | 需 `AZURE_TTS_KEY`+region | Ogg/Opus 直出 | 无需 | 官方稳定、语音气泡免转码；需信用卡 |
| `edge` | 免 key | mp3 | 需转 Ogg 才是语音气泡 | 免费、同款微软神经语音；非官方接口 |
| `http` | 可选共享密钥 | 由 worker 决定 | worker 侧做 | **委托本地 worker 合成**（edge/ChatTTS+GPU），VPS 只转发，最省 VPS 依赖 |

> **`http` provider（split 架构，推荐 VPS 磁盘/依赖紧张时用）**：把重依赖放本地 worker，经 `ssh -R` 反向隧道映射到 VPS 回环，VPS 设 `TTS_PROVIDER=http` + `TTS_HTTP_URL` 即可。VPS 完全不需要 Python/ffmpeg。详见 [`deploy/tts-worker/`](deploy/tts-worker/README.md)。

### 转码（edge 用；发语音气泡需要）

Telegram 语音气泡（`sendVoice`）要 Ogg/Opus，而 edge-tts 只出 mp3。`tts/transcode.go` **自动挑一个可用后端**：

```
ffmpeg（若装） → mpg123 + opusenc（opus-tools，二者合计 ~700KB）→ 都没有则原样发「音频文件」
```

```bash
# 推荐：极小，能发语音气泡
sudo apt install -y mpg123 opus-tools
# 或什么都不装：自动降级为发「音频文件」（带播放器，仍可用，只是不是气泡）
```

Azure 直出 Ogg，无需任何转码工具。

## 云端部署要点

1. **反代 + HTTPS**：MCP server 建议仍绑回环（`MCP_ADDR=127.0.0.1:8081`），前面用 nginx/Caddy 反代到 `https://your-host/mcp`，由反代做 TLS。远程客户端要求 HTTPS。
2. **务必开鉴权**：一旦对公网可达，`MCP_JWT_SECRET` 必须设置（否则任何人都能借 bot 发消息）。密钥用足够长的随机串。
3. 用 `-mktoken <chat_id>` 为每个使用方签发 token，客户端按上面的 `headers` / `authorization_token` 配置携带。

## 架构 / 开发说明

详见 [CLAUDE.md](CLAUDE.md)。设计与实现计划见 `docs/superpowers/`。
