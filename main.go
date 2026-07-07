package main

import (
	"errors"
	"flag"
	"log"
	"time"

	"github.com/narglc/stock.quot.tele.bot/config"
	"github.com/narglc/stock.quot.tele.bot/dao"
	"github.com/narglc/stock.quot.tele.bot/handler"
	"github.com/narglc/stock.quot.tele.bot/mcpserver"
	logger "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/schedule"
	"github.com/narglc/stock.quot.tele.bot/sender"
	tele "gopkg.in/telebot.v3"
)

var configPath = flag.String("f", "./config/config.yaml", "config file")

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

	b, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return
	}

	b.Handle("/register", handler.Register)
	b.Handle("/wakeup", handler.Wakeup)
	b.Handle("/sticker", handler.Sticker)

	b.Handle(tele.OnText, handler.OnText)
	b.Handle(tele.OnSticker, handler.OnSticker)
	b.Handle(tele.OnPhoto, handler.OnPhoto)
	b.Handle(tele.OnMyChatMember, handler.OnMyChatMember)

	// 从 Redis 恢复已注册群，再启动定时任务（否则重启后 GroupMap 为空、无人收播报）
	schedule.LoadGroups()

	// 配置了 target 才启用 MCP：构造 telegram 发送器、注册、起 HTTP 服务
	if appConfig.MCP.TargetChatID != 0 {
		sender.Register(sender.NewTelegramSender(b, appConfig.MCP.TargetChatID))
		go func() {
			if err := mcpserver.Serve(appConfig.MCP.Addr); err != nil {
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
