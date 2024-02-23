package main

import (
	"log"
	"os"
	"time"

	"github.com/narglc/stock.quot.tele.bot/handler"
	"github.com/narglc/stock.quot.tele.bot/schedule"
	tele "gopkg.in/telebot.v3"
)

func main() {
	pref := tele.Settings{
		Token:  os.Getenv("TOKEN"),
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return
	}

	b.Handle("/register", handler.Register)

	// b.Handle(tele.OnText, func(c tele.Context) error {
	// 	// All the text messages that weren't
	// 	// captured by existing handlers.

	// 	var (
	// 		user = c.Sender()
	// 		chat = c.Chat()
	// 		text = c.Text()
	// 	)

	// 	fmt.Printf("sender:[%d - %s] chat:[%d - %s], text:%+v", user.ID, user.FirstName, chat.ID, chat.Username, text)

	// 	_, err := b.Send(user, text)
	// 	if err != nil {
	// 		return err
	// 	}

	// 	return nil
	// })

	schedule.ScheduleTask(b)

	b.Start()
}
