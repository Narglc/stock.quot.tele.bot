# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目简介

一个 Telegram 机器人（telebot.v3），名义上是"股市行情"机器人，实际功能：群聊定时提醒（起床/午饭/下班等）、随机图片/表情包收藏与发送、**加密行情播报/查询/inline（含衍生品：持仓量、多空比、资金费率、全市场概览）**、**清算图 `/liqmap`（热力图+清算地图 PNG）**、动画骰子 `/dice`、**MCP 发消息/语音服务**。`stocks/` 已全面接入（整点播报 + `/price` + inline）。

## 运行与构建

```bash
# 运行（必须提供两个环境变量，否则启动即 log.Fatal）
TOKEN=<telegram_bot_token> RDB_URL=redis://127.0.0.1:6379 go run main.go

# 指定配置文件（默认 ./config/config.yaml）
go run main.go -f ./config/config.yaml

# 构建
go build -o bot main.go

# 依赖整理
go mod tidy
```

- 本地调试用 `.vscode/launch.json`，其中已内联 `TOKEN` / `RDB_URL` 环境变量。
- 单元测试：`tgmd` / `sender` / `mcpserver` / `config` 有测试（`go test ./...`）；无 lint 配置。
- **需 Go ≥ 1.25.5**（`mark3labs/mcp-go` 的最低要求，`go.mod` 已声明）；`GOTOOLCHAIN=auto`（默认）会自动切换/下载对应工具链。
- 配置统一走 viper（`config/config.go`）：**配置文件 `config/config.yaml` 为主，敏感项（`telegram.token`、`redis.url`、`mcp.jwt_secret`）留空由环境变量 `TOKEN` / `RDB_URL` / `MCP_JWT_SECRET` 覆盖**（`viper.AutomaticEnv` + 显式 `BindEnv`）。MCP 相关：`mcp.target_chat_id`（`MCP_TARGET_CHAT_ID`）、`mcp.addr`（`MCP_ADDR`，默认 `127.0.0.1:8081` 回环）、`mcp.jwt_secret`（`MCP_JWT_SECRET`）。`main.go:validateConfig` 校验合并后的 token / redis url 非空。`main.go` 起始调 `flag.Parse()`（支持 `-f` 与 `-mktoken`）。

## 架构要点

### 启动流程（main.go）
加载 viper 配置（`config.InitConfig`）→ 校验 token/redis → 初始化 logger → 初始化 Redis（`dao.InitRdb`）→ 创建 bot 并注册 handler → 从 Redis 恢复群列表（`schedule.LoadGroups`）→（若 `mcp.target_chat_id != 0`）注册 `TelegramSender` 并起 MCP goroutine（`mcpserver.Serve`）→ 启动定时任务 goroutine（`schedule.ScheduleTask`）→ `b.Start()` 长轮询。

### 命令与 handler（handler/handleApi.go）
- `/register` — 把当前 chat 登记进 `schedule.GroupMap`，之后才会收到定时提醒。
- `/wakeup [源]` — 从随机图源拉一张图发出；`payload` 为空时随机选源。
- `/sticker` — 从 Redis 随机取一张收藏的表情包。
- `/price [币种...]` — 即时查行情（`handler/price.go`）。不带参查默认 BTC/ETH，带参如 `/price btc eth sol` 查指定币种（`stocks.symbolToCoin` 里的符号，大小写不敏感）。与整点播报共用 `stocks` 底层。
- `/liqmap [币种]` — 清算图（`handler/liqmap.go`）：拉 CoinAnk 热力图 → 纯 Go 渲染热力图+清算地图 PNG 相册。需 `APIFY_TOKEN`（`handler.SetApifyToken` 注入）。
- `/dice [类型]` — 动画骰子/飞镖/篮球/足球/老虎机/保龄球（`handler/dice.go`）；点数由 Telegram 结算，延迟补一句点评。
- `OnSticker` / `OnPhoto` — 用户发来的表情包/图片会被"收藏"进 Redis（当作贡品）。
- `OnQuery`（inline 模式，`handler/inline.go`）— 在**任意聊天**输入 `@bot [币种...]` 查行情，返回「全部」+ 每币种各一条 `ArticleResult`，点选即把该行情发到当前聊天。与 `/price` 共用 `stocks` 底层。**前置**：需在 @BotFather 用 `/setinline` 为 bot 开启 inline 模式，否则 `@bot` 不出结果。

### 随机图源：注册表 + 单例 + init 自注册（domain/randompic/）
这是本项目最重要的扩展模式。新增一个图源时：
1. 实现 `RandomSrv` 接口（`GetRandomPic() (string, error)`，见 `common.go`）。
2. 用 `sync.Once` 提供单例构造函数。
3. 在该文件的 `init()` 里 `AllRandomPicSrv["<名字>"] = Get<Name>Clt()` 自注册到全局 map。
4. 调用方通过 `randompic.GetRandomPic("<名字>")` 按 key 派发；任何失败都降级返回 `DefaultPics` 兜底 URL，不向上抛错。

同时注意：`handler/handleApi.go:picCandidates` 是参与随机的图源名单（`lolicon`/`sexnyan`/`nekos`/`waifu`），新增图源若要参与随机需同步这里。选源用 `randompic.WeightedPick`（按成功率加权，见 `stats.go`；`Wakeup` 每次取图后 `randompic.Record`）。现有实现：`lolicon.go`、`sexnyan.go`、`nekosbest.go`、`waifupics.go`。取图失败会如实上抛错误（不再吞成默认图），`Wakeup` 兜底时把原因带进消息。

### 定时提醒 + 行情播报（schedule/）
`ScheduleTask` 用 `robfig/cron/v3` 统一调度，时区固定为**东八区 `Asia/Shanghai`**（`schedule.go:beijingLoc`；`main.go` 内嵌 `time/tzdata` 保证精简容器也能加载）：
- 工作日提醒：`TaskList`（`gohome.go`）里每个 `HH:MM` 注册一条 cron（`M H * * 1-5`），触发时经 `broadcastToGroups` 向 `GroupMap` 内所有群广播。`TaskList` 的时间即**东八区挂钟时间**（如"起床"= `08:00`），改时间直接按北京时间填，无需再折算时区。
- 行情播报：`0 * * * *` 每小时整点调 `broadcastCrypto`（`schedule/crypto.go`），启动时立即一次。数据 = CoinGecko `/coins/markets` + OKX 衍生品（`stocks/derivatives.go`：持仓量及变化、多空比、资金费率）+ 全市场概览（`stocks/global.go`）+ 恐惧贪婪（`stocks/sentiment.go`），均 best-effort。**播报采用"原地编辑上次那条消息"刷新（消息 id 存 Redis `tg:gohome:bcast`），不刷屏**；编辑失败/无记录才新发。`/price` 与播报共用 `stocks.FormatCryptoMessage`。
- 清算图播报：配 `APIFY_TOKEN` 时，`0 8/20 * * *` 各一次 BTC 清算图（`schedule/liqmap.go` → `stocks.GetLiqHeatmap` + `RenderLiq*PNG` → 相册）。
- 发送失败且 `IsBotEvicted` 判定 bot 已被移出时自动 `UnregisterGroup`（提醒与行情共用 `broadcastToGroups`）。
- 群列表是内存态（`gohome.go:groupMap`，进程重启后由 `LoadGroups()` 从 Redis 恢复），由 `groupMu sync.RWMutex` 保护，只经导出的线程安全函数访问：`RegisterGroup`/`UnregisterGroup`/`IsRegistered`/`GroupCount`/`SnapshotGroupIDs`。广播先 `SnapshotGroupIDs()` 取 id 快照再逐个发送，避免持锁做网络 I/O、以及遍历中调 `UnregisterGroup` 自死锁（并发安全由 `schedule/group_test.go` 的 `-race` 压测覆盖）。

### 数据层（dao/rds.go）
基于 `redis/go-redis/v9`，全局单例 `rdb`；每次操作走 `opCtx()` 带 5s 超时的 context。`InitRdb` 解析 URL 失败或启动 `Ping` 不通直接 panic（fail-fast）。表情包和图片分别存 Redis Set（`tg:gohome:stickers` / `tg:gohome:pics`），用 `SAdd` 收藏、`SRandMember` 随机取；已注册群存 Hash（`tg:gohome:groups`）。`DefaultSticker` 是取不到时的兜底 fileID。

### 日志（pkg/logger/logger.go）
自封装的 logrus 包装层，包级函数 `log.Infof` 等直接可用（`GetLogger()` 通过 `runtime.Caller` 注入调用处 `__src`）。支持 lumberjack 滚动切割，由 `config.yaml` 的 logger 段驱动。全项目统一以 `log "github.com/narglc/stock.quot.tele.bot/pkg/logger"` 别名导入。

### MCP 发消息服务（mcpserver/ + sender/ + tgmd/）
供自己的 agent 通过 MCP 发消息。`config.MCP.TargetChatID != 0` 时，`main.go` 构造 `TelegramSender` 注册进 `sender` 注册表并在 goroutine 启动 `mcpserver.Serve`（`mark3labs/mcp-go`，Streamable HTTP，默认 `127.0.0.1:8081/mcp`）。
- tool：`send_message(platform="telegram", text)`，`text` 为标准 markdown。
- 发送链路：`tgmd.Convert`（`goldmark` 解析 AST → 只输出 Telegram 支持的 HTML 标签子集，标题/列表/表格降级）→ `bot.Send(ParseMode=HTML)`；HTML 失败降级为原始 markdown 纯文本重发。
- **鉴权（`mcpserver/jwtauth.go`）**：`mcp.jwt_secret`（`MCP_JWT_SECRET`）非空则启用 JWT Bearer 鉴权，中间件校验 HS256 签名（显式拒 `alg=none`）+ `exp`；为空则免鉴权（仅回环用，向后兼容）。
- **发送目标**：鉴权开启时目标 = JWT 的 `sub`(chat_id)，经 request context 透传到 handler；免鉴权时回落 `MCP_TARGET_CHAT_ID`。`Sender.Send(md, recipient)` 的 recipient 即目标（Telegram 为 chat_id 字符串）。
- **签发 token**：`MCP_JWT_SECRET=xxx go run main.go -mktoken <chat_id>`（`mcpserver.SignToken`），打印后退出、不启动 bot。
- 多平台扩展：实现 `sender.Sender` 接口（`Send(md, recipient)/Name()`）+ `sender.Register` 即可（如将来的 lark），tool 层无需改动。与图源的注册表模式一脉相承，但 sender 在 `main.go` 运行期注册（依赖 bot 实例），不用 `init()` 自注册。

## 约定

- 私聊时 `c.Sender()` == `c.Chat()`；群聊时 sender 是发言人、chat 是群。handler 里普遍用 `c.Bot().Send(chat, ...)` 回群。
- 面向用户的文案是中文/粤语梗风格，保持该调性。
- `图源api.md` 记录了各随机图源第三方 API 的请求样例，新增/调试图源时参考。
