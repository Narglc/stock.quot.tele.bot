package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
	// 内嵌 IANA 时区库：保证无系统 tzdata 的精简容器也能 LoadLocation("Asia/Shanghai")
	_ "time/tzdata"

	"github.com/narglc/stock.quot.tele.bot/config"
	"github.com/narglc/stock.quot.tele.bot/dao"
	"github.com/narglc/stock.quot.tele.bot/handler"
	"github.com/narglc/stock.quot.tele.bot/mcpserver"
	logger "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/schedule"
	"github.com/narglc/stock.quot.tele.bot/sender"
	"github.com/narglc/stock.quot.tele.bot/tts"
	tele "gopkg.in/telebot.v3"
)

var (
	configPath = flag.String("f", "./config/config.yaml", "config file")
	// -mktoken <chat_id>：用 MCP_JWT_SECRET 签发一个该 chat_id 的 MCP JWT 后退出，不启动 bot。
	mkToken = flag.Int64("mktoken", 0, "签发一个 chat_id 的 MCP JWT 并退出")
)

// buildSynthesizer 按配置构造 TTS 合成器；provider 空=不启用语音（返回 nil）。
// 新增实现时在这里加一个 case，其余（send_voice / sender）零改动。
func buildSynthesizer(cfg *config.TTSConfig) tts.Synthesizer {
	switch cfg.Provider {
	case "":
		return nil
	case "azure":
		return tts.NewAzure(cfg.Azure.Key, cfg.Azure.Region, cfg.Azure.Voice)
	case "edge":
		return tts.NewEdge(cfg.Edge.Voice)
	case "http":
		return tts.NewHTTP(cfg.Http.URL, cfg.Http.Voice, cfg.Http.Token)
	default:
		logger.Warnf("未知 tts.provider=%q，语音功能不启用", cfg.Provider)
		return nil
	}
}

func validateConfig(cfg *config.Config) error {
	if cfg.Telegram.Token == "" {
		return errors.New("telegram token is required (config telegram.token or TOKEN env)")
	}
	if cfg.Redis.URL == "" {
		return errors.New("redis url is required (config redis.url or RDB_URL env)")
	}
	return nil
}

func main() {
	flag.Parse()

	appConfig, cfg_succ := config.InitConfig(*configPath)
	if !cfg_succ {
		panic("config init fail.")
	}

	// -mktoken：仅签发 JWT 后退出（不需要 TOKEN/RDB_URL，故在校验前处理）
	if *mkToken != 0 {
		tok, err := mcpserver.SignToken(appConfig.MCP.JWTSecret, *mkToken)
		if err != nil {
			log.Fatalf("签发 token 失败: %v", err)
		}
		fmt.Println(tok)
		return
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

	b, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return
	}

	b.Handle("/register", handler.Register)
	b.Handle("/wakeup", handler.Wakeup)
	b.Handle("/sticker", handler.Sticker)
	b.Handle("/price", handler.Price)
	b.Handle("/liqmap", handler.Liqmap)
	b.Handle("/dice", handler.Dice)
	b.Handle("/gen", handler.Gen)
	b.Handle("/help", handler.Help)

	b.Handle(tele.OnText, handler.OnText)
	b.Handle(tele.OnSticker, handler.OnSticker)
	b.Handle(tele.OnPhoto, handler.OnPhoto)
	b.Handle(tele.OnMyChatMember, handler.OnMyChatMember)
	b.Handle(tele.OnQuery, handler.InlineQuery) // inline 行情查询（需 @BotFather /setinline 开启）

	// 注入 /liqmap 用的 Apify token、/gen 用的生图 worker 端点（空则对应命令提示未启用）
	handler.SetApifyToken(appConfig.Apify.Token)
	handler.SetImageEndpoint(appConfig.Image.URL, appConfig.Image.Token)

	// 从 Redis 恢复已注册群，再启动定时任务（否则重启后 GroupMap 为空、无人收播报）
	schedule.LoadGroups()

	// 配置了 target 才启用 MCP：构造 telegram 发送器、注册、起 HTTP 服务
	// 发送目标：鉴权开启时取 JWT 的 chat_id，否则回落 TargetChatID
	if appConfig.MCP.TargetChatID != 0 {
		sender.Register(sender.NewTelegramSender(b))
		synth := buildSynthesizer(&appConfig.TTS)
		go func() {
			if err := mcpserver.Serve(appConfig.MCP.Addr, appConfig.MCP.JWTSecret, appConfig.MCP.TargetChatID, synth); err != nil {
				logger.Errorf("MCP server 退出: %v", err)
			}
		}()
	}

	schedule.ScheduleTask(b, appConfig.Apify.Token)

	// go func() {
	// 	for {
	// 		stocks.GetStock()
	// 		time.Sleep(3 * time.Second)
	// 	}
	// }()

	// 对话输入框设置默认命令按钮
	commands := []tele.Command{
		{Text: "/register", Description: "下班提醒"},
		{Text: "/wakeup", Description: "提神醒脑"},
		{Text: "/sticker", Description: "精选表情包"},
		{Text: "/price", Description: "查行情（可加币种，如 /price btc eth）"},
		{Text: "/liqmap", Description: "清算图（可加币种，如 /liqmap btc）"},
		{Text: "/dice", Description: "掷骰子（可选 飞镖/篮球/足球/老虎机/保龄球）"},
		{Text: "/gen", Description: "AI 生图（/gen a cyberpunk city）"},
		{Text: "/help", Description: "命令说明"},
	}

	if err = b.SetCommands(commands); err != nil {
		log.Fatal(err)
	}

	// 优雅退出：收到 SIGINT/SIGTERM 时停止长轮询、停定时任务、关 redis。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go b.Start()
	logger.Infof("bot 已启动，等待退出信号…")
	<-ctx.Done()

	logger.Infof("收到退出信号，优雅关闭…")
	b.Stop()
	schedule.StopTasks()
	if cerr := dao.CloseRdb(); cerr != nil {
		logger.Warnf("关闭 redis 失败: %v", cerr)
	}
	logger.Infof("已退出")
}

// func initScheduler() *cron.Cron {
// 	c := cron.New()
// 	c.AddFunc("0 18 * * 1-5", sendGoHomeNotification)
// 	return c
// }
