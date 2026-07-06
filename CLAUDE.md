# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目简介

一个 Telegram 机器人（telebot.v3），名义上是"股市行情"机器人，实际当前主要功能是群聊定时提醒（起床/午饭/下班等）、随机图片/表情包收藏与发送。股票行情能力（`stocks/`）已有骨架但在 `main.go` 中被注释掉，尚未接入。

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
- 没有测试文件，无 lint 配置。
- 配置分两层：**敏感项走环境变量**（`TOKEN`、`RDB_URL`，在 `main.go:validateConfig` 校验），**非敏感项走 `config/config.yaml`**（目前仅日志配置）。

## 架构要点

### 启动流程（main.go）
校验环境变量 → 加载 yaml 配置 → 初始化 logger → 初始化 Redis（`dao.InitRdb`）→ 创建 bot 并注册 handler → 启动定时任务 goroutine（`schedule.ScheduleTask`）→ `b.Start()` 长轮询。

### 命令与 handler（handler/handleApi.go）
- `/register` — 把当前 chat 登记进 `schedule.GroupMap`，之后才会收到定时提醒。
- `/wakeup [源]` — 从随机图源拉一张图发出；`payload` 为空时随机选源。
- `/sticker` — 从 Redis 随机取一张收藏的表情包。
- `OnSticker` / `OnPhoto` — 用户发来的表情包/图片会被"收藏"进 Redis（当作贡品）。

### 随机图源：注册表 + 单例 + init 自注册（domain/randompic/）
这是本项目最重要的扩展模式。新增一个图源时：
1. 实现 `RandomSrv` 接口（`GetRandomPic() (string, error)`，见 `common.go`）。
2. 用 `sync.Once` 提供单例构造函数。
3. 在该文件的 `init()` 里 `AllRandomPicSrv["<名字>"] = Get<Name>Clt()` 自注册到全局 map。
4. 调用方通过 `randompic.GetRandomPic("<名字>")` 按 key 派发；任何失败都降级返回 `DefaultPics` 兜底 URL，不向上抛错。

同时注意：`handler/handleApi.go:getRandomPicSrc()` 里硬编码了可随机选中的图源名单（`lolimi`/`lolicon`/`sexnyan`），新增图源若要参与随机需同步这里。现有实现：`lolicon.go`、`lolimi.go`、`sexnyan.go`。

### 定时提醒（schedule/）
`sendGoHomeNotifications` 是一个常驻 goroutine：每 30 秒轮询一次系统时间，工作日（周一至五）匹配 `TaskList` 中的 `HH:MM` 就向 `GroupMap` 内所有群广播消息，命中后 sleep 1 分钟防重复。**注意**：比较的是 `time.Now()` 本地时区，且 `TaskList` 里的时间看起来是按 UTC/服务器时区写的（如"起床"= `00:00`），改动提醒时间要确认运行环境时区。`GroupMap` 是内存态，进程重启后需重新 `/register`。

### 数据层（dao/rds.go）
基于 `go-redis/redis` v6，全局单例 `rdb`。表情包和图片分别存 Redis Set（`tg:gohome:stickers` / `tg:gohome:pics`），用 `SAdd` 收藏、`SRandMember` 随机取。`DefaultSticker` 是取不到时的兜底 fileID。

### 日志（pkg/logger/logger.go）
自封装的 logrus 包装层，包级函数 `log.Infof` 等直接可用（`GetLogger()` 通过 `runtime.Caller` 注入调用处 `__src`）。支持 lumberjack 滚动切割，由 `config.yaml` 的 logger 段驱动。全项目统一以 `log "github.com/narglc/stock.quot.tele.bot/pkg/logger"` 别名导入。

## 约定

- 私聊时 `c.Sender()` == `c.Chat()`；群聊时 sender 是发言人、chat 是群。handler 里普遍用 `c.Bot().Send(chat, ...)` 回群。
- 面向用户的文案是中文/粤语梗风格，保持该调性。
- `图源api.md` 记录了各随机图源第三方 API 的请求样例，新增/调试图源时参考。
