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
- 单元测试：`tgmd` / `sender` / `mcpserver` / `config` / `stocks` / `handler` / `randompic` / `utils` / `schedule` 有测试（`go test ./...`，CI 跑 `-race`）。
- CI：`.github/workflows/ci.yml` 跑 gofmt 检查 / `go vet` / `go build` / `go test -race`，外加 golangci-lint（配置见 `.golangci.yml`，重点是 staticcheck 的废弃 API 检查）。
- **需 Go ≥ 1.25.5**（`mark3labs/mcp-go` 的最低要求，`go.mod` 已声明）；`GOTOOLCHAIN=auto`（默认）会自动切换/下载对应工具链。
- 配置统一走 viper（`config/config.go`）：**配置文件 `config/config.yaml` 为主，敏感项（`telegram.token`、`redis.url`、`mcp.jwt_secret`）留空由环境变量 `TOKEN` / `RDB_URL` / `MCP_JWT_SECRET` 覆盖**（`viper.AutomaticEnv` + 显式 `BindEnv`）。MCP 相关：`mcp.target_chat_id`（`MCP_TARGET_CHAT_ID`）、`mcp.addr`（`MCP_ADDR`，默认 `127.0.0.1:8081` 回环）、`mcp.jwt_secret`（`MCP_JWT_SECRET`）。`main.go:validateConfig` 校验合并后的 token / redis url 非空。`main.go` 起始调 `flag.Parse()`（支持 `-f` 与 `-mktoken`）。

## 架构要点

### 启动流程（main.go）
加载 viper 配置（`config.InitConfig`）→ 校验 token/redis → 初始化 logger → 初始化 Redis（`dao.InitRdb`）→ 创建 bot 并注册 handler → 从 Redis 恢复群列表（`schedule.LoadGroups`）→ 建立退出信号 context（`signal.NotifyContext`）→（若 `mcp.target_chat_id != 0`）注册 `TelegramSender` 并起 MCP goroutine（`mcpserver.Serve(ctx, ...)`）→ 启动定时任务 goroutine（`schedule.ScheduleTask`）→ `b.Start()` 长轮询。

**优雅退出的顺序有依赖，别随意调换**：收到 SIGINT/SIGTERM → `b.Stop()` 停长轮询 → `schedule.StopTasks()`（`<-cron.Stop().Done()`，**必须等在途任务真正结束**，最多 `stopTimeout`）→ `dao.CloseRdb()`。不等 cron 就关 Redis 的话，正在跑的 `checkCryptoAlerts` / `dailyDigest` 会拿到一个已关闭的客户端。MCP server 由同一个 ctx 驱动 `srv.Shutdown` 自行关停。

### 命令与 handler（handler/handleApi.go）
- `/register` — 把当前 chat 登记进 `schedule.GroupMap`，之后才会收到定时提醒。
- `/wakeup [源]` — 从随机图源拉一张图发出；`payload` 为空时随机选源。
- `/sticker` — 从 Redis 随机取一张收藏的表情包。
- `/price [币种...]` — 即时查行情（`handler/price.go`）。不带参查默认 BTC/ETH，带参如 `/price btc eth sol` 查指定币种（`stocks.symbolToCoin` 里的符号，大小写不敏感）。与整点播报共用 `stocks` 底层。
- `/liqmap [币种]` — 清算图（`handler/liqmap.go`）：拉 CoinAnk 热力图 → 纯 Go 渲染热力图+清算地图 PNG 相册。需 `APIFY_TOKEN`（`handler.SetApifyToken` 注入）。**与定时播报共用 `stocks.BuildLiqAlbum`**，别再各写一遍。Apify actor 单次 60~90s 且计费，所以数据层带 10min 缓存 + 同币种串行锁（`stocks/liqalbum.go`），symbol 先过 `NormalizeSymbol` 校验——无效输入不该白烧一次配额。
- `/dice [类型]` — 动画骰子/飞镖/篮球/足球/老虎机/保龄球（`handler/dice.go`）；点数由 Telegram 结算，延迟补一句点评。
- `/gen <描述>` — AI 生图（`handler/gen.go`）：把提示词 POST 给本地 GPU worker，取回图片作为 Photo 发出。需 `IMAGE_URL`（`handler.SetImageEndpoint` 注入），未配置时提示未启用。
- `/watch [add|del|clear] [代码...]` — A股自选（`handler/watch.go`）。不带参展示自选行情，`add`/`del` 增删、`clear` 清空。列表按 **chat** 存（群共享一份、私聊各自一份），上限 `stocks.MaxWatchlistSize`。
- `/help` — 命令说明（`handler/help.go`）。
- `OnSticker` / `OnPhoto` — 用户发来的表情包/图片会被"收藏"进 Redis（当作贡品）。**确认消息只在私聊回**（群里静默收下，否则群里刷九宫格会私聊发言人九次，且对方没私聊过 bot 时必然失败）；两个集合都有 `dao.maxCollectionSize` 容量上限，超出随机弹出。
- `OnMyChatMember` — 监听 bot 自身成员状态：被踢出/移出群时实时 `UnregisterGroup`。
- `OnQuery`（inline 模式，`handler/inline.go`）— 在**任意聊天**输入 `@bot [币种...]` 查行情，返回「全部」+ 每币种各一条 `ArticleResult`，点选即把该行情发到当前聊天。与 `/price` 共用 `stocks` 底层。**前置**：需在 @BotFather 用 `/setinline` 为 bot 开启 inline 模式，否则 `@bot` 不出结果。

### 随机图源：注册表 + 单例 + init 自注册（domain/randompic/）
这是本项目最重要的扩展模式。新增一个图源时：
1. 实现 `RandomSrv` 接口（`GetRandomPic() (string, error)`，见 `common.go`）。
2. 用 `sync.Once` 提供单例构造函数。
3. 在该文件的 `init()` 里 `AllRandomPicSrv["<名字>"] = Get<Name>Clt()` 自注册到全局 map。
4. 调用方通过 `randompic.GetRandomPic("<名字>")` 按 key 派发；**失败如实上抛错误**（不再吞成默认图），由 `Wakeup` 兜底（回落默认 sticker）并把原因带进消息。

同时注意：`handler/handleApi.go:picCandidates` 是参与随机的图源名单（`lolicon`/`nekos`/`waifu`/`picre`/`elvish`/`elaina`/`dmoe`），新增图源若要参与随机需同步这里。选源用 `randompic.WeightedPick`（按成功率加权，见 `stats.go`；`Wakeup` 每次取图后 `randompic.Record`），发图 caption 会标注**实际命中的图源名**。现有实现：`lolicon.go`（POST setu API）、`nekosapi.go`（nekosapi.com v4，JSON 数组取首 url）、`waifupics.go`、`picre.go`（直图端点）、`elvish.go`（pic.elvish.me，解析 302 拿真实图片 url）、`elaina.go`（api.elaina.cat 直图）、`dmoe.go`（dmoe.cc 樱花 API 直图）。**注意**：不少源（pic.re/nekosapi/waifu.pics/elvish）返回 **webp**，而 Telegram sendPhoto 不吃 webp，故 `fetchPic` 下载后经 `webpToPNG` 统一转 PNG 再发。已移除的源：`sexnyan`（TLS/SNI 证书错误 `tlsv1 unrecognized name`）、`nekosbest`（被 nekosapi 取代）。

### 定时提醒 + 行情播报（schedule/）
`ScheduleTask` 用 `robfig/cron/v3` 统一调度，时区固定为**东八区 `Asia/Shanghai`**（`schedule.go:beijingLoc`；`main.go` 内嵌 `time/tzdata` 保证精简容器也能加载）：
- 工作日提醒：`TaskList`（`gohome.go`）里每个 `HH:MM` 注册一条 cron（`M H * * 1-5`），触发时经 `broadcastToGroups` 向 `GroupMap` 内所有群广播。`TaskList` 的时间即**东八区挂钟时间**（如"起床"= `08:00`），改时间直接按北京时间填，无需再折算时区。
- **行情播报分两条线，别把它们合并**（`schedule/crypto.go`）：
  - `checkCryptoAlerts`（`0 * * * *`）—— 每小时检测，**只在行情跨越某个状态时才发**（见 `stocks/alerts.go`）。事件消息**不静音、不删旧**：稀疏且有回溯价值，该留在群里。启动时先跑一次建立基线（首次观测不产生事件，所以重启不会刷一屏误报）。
  - `dailyDigest`（`0 8 * * *`）—— 每天早八无条件发一次完整面板，作为**保底心跳**。纯静默模式下「没异动」和「bot 挂了」看起来一模一样。面板消息**静音 + 删旧发新**（上一条 id 存 Redis `tg:gohome:bcast`），群里只保留最新一条。
  - 数据 = CoinGecko `/coins/markets` + OKX 衍生品 + 全市场概览 + 恐惧贪婪，均 best-effort。`/price` 与面板共用 `stocks.FormatCryptoMessage`。
  - 历史：原来是每小时无条件发完整面板，大部分整点数字并无实质变化，久了就被自动忽略，等于没有播报。
- A股收盘播报：`5 15 * * 1-5` 调 `broadcastAShare`（`schedule/ashare.go`），逐 chat 播报自选股。cron 的 `1-5` 排得掉周末排不掉节假日，所以再用 `stocks.AnyTraded`（全市场无成交）判一次。
- 清算图播报：配 `APIFY_TOKEN` 时，`0 8 * * *` 与 `0 20 * * *` 两条独立 cron 各一次 BTC 清算图（`schedule/liqmap.go` → `stocks.BuildLiqAlbum` → 相册）。注意不能写成 `0 8/20 * * *`——那在 cron 语义里是「从 8 点起每 20 小时」，只会命中 8 点。
- 发送失败且 `IsBotEvicted` 判定 bot 已被移出时自动 `UnregisterGroup`（提醒与行情共用 `broadcastToGroups`）。
- 群列表是内存态（`gohome.go:groupMap`，进程重启后由 `LoadGroups()` 从 Redis 恢复），由 `groupMu sync.RWMutex` 保护，只经导出的线程安全函数访问：`RegisterGroup`/`UnregisterGroup`/`IsRegistered`/`GroupCount`/`SnapshotGroupIDs`。广播先 `SnapshotGroupIDs()` 取 id 快照再逐个发送，避免持锁做网络 I/O、以及遍历中调 `UnregisterGroup` 自死锁（并发安全由 `schedule/group_test.go` 的 `-race` 压测覆盖）。

### 行情数据层的三条约定（stocks/）
1. **出站请求统一走 `fetchJSON`（`stocks/httpx.go`）**，它会检查 HTTP 状态码并把错误响应体截断到 `maxErrBody`。直接 `Get → Unmarshal` 会把 CoinGecko 的 429 限流误报成「解析失败」，还会把整个错误页打进日志。
2. **HTTP client 一律用包级共享变量**（`marketClient` / `derivClient` / `picClient` / `imageClient` / `apifyClient`），不要在函数里现场 `&http.Client{}` —— 那样每个请求都是新的连接池，TLS 握手无法复用。
3. **行情接口都带 TTL 缓存**（`stocks/cache.go`）：`quotesCache` 30s、`globalCache` 60s、`fngCache` 10min。原因是 inline 查询会「每敲一个字符触发一次」，不缓存极易打满 CoinGecko 免费额度。注意 `fetchQuotes` 命中缓存时返回 `slices.Clone`——调用方 `enrichDerivatives` 会就地写入衍生品字段，直接返回缓存切片会让不同调用互相污染。

**持仓量变化（OIChangePct）必须区分 scope**：`OIScopeBroadcast` 和 `OIScopeQuery` 各记一份基线（`stocks/derivatives.go:lastOI`）。共用一份的话，只要有人在 10:30 敲过 `/price btc`，11:00 播报里的「持仓 +1.2%」就变成「距 10:30 的变化」而非小时环比。展示时一律经 `formatSpan` 标出时间跨度（如 `+2.4% / 58m`），不标就是个无法证伪的数字。基线是进程内状态，重启后首次观测没有可比基线，会省略变化部分。

### 行情事件检测（stocks/alerts.go）
激进版播报的核心是**「跨越」而不是「处于」**：只判断「当前费率 > 阈值」的话，价格在高位横盘时每小时都会响一次，跟定点播报没有区别。所以 `alertState` 记录上次观测到的状态（费率符号/是否极端、是否处于 24h 高低、恐惧贪婪档位），只有状态**发生变化**才产生事件。

- 四类事件：资金费率翻转/进入极端区、持仓量骤变（`oiJumpPct`）、突破 24h 高低、恐惧贪婪跨档。阈值是包级常量，调之前想清楚：太低会退化成每小时都响，太高则等你看到时已经发生完了。
- **首次观测只记基线、不报事件**——否则进程一重启就把所有「当前处于极端状态」的币全报一遍，而它们可能已经在那个状态待了一整天。
- 文案只描述「发生了什么」，不给「所以该做什么」。方向性结论无法证伪。

### A股行情（stocks/ashare.go）
数据源是**东方财富 push2**，不是雪球：雪球 `quotec` 要先拿 `xq_a_token` cookie 且对机房 IP 直接 403；腾讯/新浪返回 GBK 分隔字符串还要转码按位解析。东财免鉴权、UTF-8 JSON、一次可查多只。

- `secid` 前缀：6/5/9 开头归上交所（`1.`），其余归深/北（`0.`）。
- `flexFloat` 兼容东财在停牌时把数字字段返回成 `"-"`，单字段解析失败不毁掉整条行情。
- **时间口径是强制的**：`FormatStockMessage` 收盘后标「收盘」，盘中标「截至 HH:MM · 盘中」并附提醒。盘中数字随时会变，不标时间事后无法判断当时看到的是什么——这条有测试守着（`TestFormatStockMessage_IntradayMustCarryTimestamp`）。

### 判断追踪（domain/claims/）
把「带证伪条件的判断」记下来并追踪它后来对不对，命令是 `/claim` `/claims` `/resolve`（`handler/claim.go`）。

- **没有证伪条件就拒收**（`ParseInput` 返回错误）。这是整套东西的意义所在：不能证伪的判断等于「偏多/偏空」，涨了算对跌了能找补。
- 输入用 `|` 分段而不是 `--flag`：手机上打 `|` 比打 `--falsify` 省事。段前缀中英都认，分隔符 `:` `：` 空格都认。
- 证伪条件能解析成 `<SYMBOL><op><price>`（支持 `万`/`w`/`k` 单位）的进入**自动检查**：`schedule/claims.go` 每小时 30 分跑 `claims.Check`，触发即判定 `falsified` 并**不静音**通知（判断错了得知道）；解析不了的也照样记，到期时静音提醒本人 `/resolve`。
- **被证伪优先于到期**：前者是确定结论，后者只是「时间到了你自己看」。到期**不自动给结论**，留空等人填。
- 取价失败只跳过本轮，不改状态——不能因一次网络抖动就误判。取价函数 `handler.ClaimPrice` 按 symbol 分派（6 位数字→A股，其余→crypto），注入到 `schedule`，让 `claims` 包对数据源无知。
- 通知发回**记录这条 claim 的那个 chat**，不广播。
- `/claims export` 导出 jsonl，字段名对齐 `claims.jsonl` 的约定（`basis` / `falsify_cond` / `conclusion`）。

### 清算图网页（deploy/liqmap-web/）
Cloudflare Worker + KV 的只读网页。主视图是**爆仓地图**：横轴价格、柱子高度=该价位单档清算额（左轴），叠加两条**从现价向两边的累计曲线**（右轴）。累计曲线是这张图的重点——单档量本身说明不了什么，「价格走到这里一共会扫掉多少」才跟决策相关，曲线斜率陡的那段就是密集区。白线现价，右侧红=空头爆仓区、左侧绿=多头爆仓区。最高的几根直接标数值，hover/触摸同时给本档量与累计量。右侧「走到这里会扫掉多少」把 ±1/2/5% 的累计量预先算好（`cumAtPrice`，超出图表范围会标 `+`，否则会被误读成「再远就没有了」）。顶部可切「最近一格 / 累计全部」，热力图作为次要视图保留。`ALLOWED_SYMBOLS` 默认只有 BTC，单币种时标签栏自动隐藏。**bot 是唯一的 Apify 调用方**：拉到新数据后 `stocks.PushHeatmapSnapshot` 推快照给 Worker（`PUT /api/heatmap/:symbol`，`X-Auth-Token` 鉴权），网页只读 KV。让网页直连 Apify 的话每个访客都可能烧一次配额（单次 60~90s 且计费）。快照用稀疏三元组而非稠密数组，体积从几 MB 降到几十 KB。配 `liqweb.url`/`liqweb.token`（`LIQWEB_URL`/`LIQWEB_TOKEN`）才推送，留空则 `/liqmap` 照常工作。

### 数据层（dao/rds.go）
基于 `redis/go-redis/v9`，全局单例 `rdb`；每次操作走 `opCtx()` 带 5s 超时的 context。`InitRdb` 解析 URL 失败或启动 `Ping` 不通直接 panic（fail-fast）。表情包和图片分别存 Redis Set（`tg:gohome:stickers` / `tg:gohome:pics`），用 `SAdd` 收藏、`SRandMember` 随机取；已注册群存 Hash（`tg:gohome:groups`）。`DefaultSticker` 是取不到时的兜底 fileID。

### 日志（pkg/logger/logger.go）
自封装的 logrus 包装层，包级函数 `log.Infof` 等直接可用（`GetLogger()` 通过 `runtime.Caller` 注入调用处 `__src`）。支持 lumberjack 滚动切割，由 `config.yaml` 的 logger 段驱动。全项目统一以 `log "github.com/narglc/stock.quot.tele.bot/pkg/logger"` 别名导入。

### MCP 发消息服务（mcpserver/ + sender/ + tgmd/）
供自己的 agent 通过 MCP 发消息。`config.MCP.TargetChatID != 0` 时，`main.go` 构造 `TelegramSender` 注册进 `sender` 注册表并在 goroutine 启动 `mcpserver.Serve`（`mark3labs/mcp-go`，Streamable HTTP，默认 `127.0.0.1:8081/mcp`）。
- tool：`send_message(platform="telegram", text)`，`text` 为标准 markdown。
- tool：`get_liqmap(symbol, top_n)` —— 配了 `APIFY_TOKEN` 才注册。返回**结构化 JSON 而非 PNG**（agent 要的是能参与推理的数字），上下方清算簇强制两边都返回并附 `note` 说明口径：只看一边会得出无法证伪的方向结论。
- 发送链路：`tgmd.Convert`（`goldmark` 解析 AST → 只输出 Telegram 支持的 HTML 标签子集，标题/列表/表格降级）→ `bot.Send(ParseMode=HTML)`；HTML 失败降级为原始 markdown 纯文本重发。
- **鉴权（`mcpserver/jwtauth.go`）**：`mcp.jwt_secret`（`MCP_JWT_SECRET`）非空则启用 JWT Bearer 鉴权，中间件校验 HS256 签名（显式拒 `alg=none`）+ `exp`；为空则免鉴权（仅回环用，向后兼容）。
- **发送目标**：鉴权开启时目标 = JWT 的 `sub`(chat_id)，经 request context 透传到 handler；免鉴权时回落 `MCP_TARGET_CHAT_ID`。`Sender.Send(md, recipient)` 的 recipient 即目标（Telegram 为 chat_id 字符串）。
- **签发 token**：`MCP_JWT_SECRET=xxx go run main.go -mktoken <chat_id>`（`mcpserver.SignToken`），打印后退出、不启动 bot。
- 多平台扩展：实现 `sender.Sender` 接口（`Send(md, recipient)/Name()`）+ `sender.Register` 即可（如将来的 lark），tool 层无需改动。与图源的注册表模式一脉相承，但 sender 在 `main.go` 运行期注册（依赖 bot 实例），不用 `init()` 自注册。

## 约定

- 私聊时 `c.Sender()` == `c.Chat()`；群聊时 sender 是发言人、chat 是群。handler 里普遍用 `c.Bot().Send(chat, ...)` 回群。
- 面向用户的文案是中文/粤语梗风格，保持该调性。
- `图源api.md` 记录了各随机图源第三方 API 的请求样例，新增/调试图源时参考。
