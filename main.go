package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
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
	tele "gopkg.in/telebot.v3"
)

var (
	configPath = flag.String("f", "./config/config.yaml", "config file")
	// -mktoken <chat_id>：用 MCP_JWT_SECRET 签发一个该 chat_id 的 MCP JWT 后退出，不启动 bot。
	mkToken = flag.Int64("mktoken", 0, "签发一个 chat_id 的 MCP JWT 并退出")
)

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

	b.Handle(tele.OnText, handler.OnText)
	b.Handle(tele.OnSticker, handler.OnSticker)
	b.Handle(tele.OnPhoto, handler.OnPhoto)
	b.Handle(tele.OnMyChatMember, handler.OnMyChatMember)

	// 从 Redis 恢复已注册群，再启动定时任务（否则重启后 GroupMap 为空、无人收播报）
	schedule.LoadGroups()

	// 配置了 target 才启用 MCP：构造 telegram 发送器、注册、起 HTTP 服务
	// 发送目标：鉴权开启时取 JWT 的 chat_id，否则回落 TargetChatID
	if appConfig.MCP.TargetChatID != 0 {
		sender.Register(sender.NewTelegramSender(b))
		go func() {
			if err := mcpserver.Serve(appConfig.MCP.Addr, appConfig.MCP.JWTSecret, appConfig.MCP.TargetChatID); err != nil {
				logger.Errorf("MCP server 退出: %v", err)
			}
		}()
	}

	schedule.ScheduleTask(b)

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
	}

	err = b.SetCommands(commands)
	if err != nil {
		log.Fatal(err)
	}

	b.Start()
}

// func initScheduler() *cron.Cron {
// 	c := cron.New()
// 	c.AddFunc("0 18 * * 1-5", sendGoHomeNotification)
// 	return c
// }
