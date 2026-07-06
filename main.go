package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"time"

	"github.com/narglc/stock.quot.tele.bot/config"
	"github.com/narglc/stock.quot.tele.bot/dao"
	"github.com/narglc/stock.quot.tele.bot/handler"
	logger "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/schedule"
	tele "gopkg.in/telebot.v3"
)

var configPath = flag.String("f", "./config/config.yaml", "config file")

func validateConfig() error {
	if os.Getenv("TOKEN") == "" {
		return errors.New("TOKEN environment variable is required")
	}
	if os.Getenv("RDB_URL") == "" {
		return errors.New("RDB_URL environment variable is required")
	}
	return nil
}

func main() {
	if err := validateConfig(); err != nil {
		log.Fatalf("Configuration validation failed: %v", err)
	}

	appConfig, cfg_succ := config.InitConfig(*configPath)
	if !cfg_succ {
		panic("config init fail.")
	}

	logger.SetLoggerConfig(&appConfig.LoggerConfig)

	pref := tele.Settings{
		Token:  os.Getenv("TOKEN"),
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	rdbUrl := os.Getenv("RDB_URL")
	dao.InitRdb(rdbUrl)

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
