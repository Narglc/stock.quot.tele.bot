package main

import (
	"os"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/narglc/stock.quot.tele.bot/dao"
	"github.com/narglc/stock.quot.tele.bot/handler"
	"github.com/narglc/stock.quot.tele.bot/schedule"
	tele "gopkg.in/telebot.v3"
)

func main() {
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
