package main

import (
	"log"
	"os"
	"time"

	"gopkg.in/telebot.v3"
	tele "gopkg.in/telebot.v3"
)

const (
	notificationTime = "09:00"
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

	b.Handle("/hello", func(c tele.Context) error {
		return c.Send("Hello!")
	})

	b.Handle(tele.OnText, func(c tele.Context) error {
		// All the text messages that weren't
		// captured by existing handlers.

		// var (
		// 	user = c.Sender()
		// 	text = c.Text()
		// )

		// Use full-fledged bot's functions
		// only if you need a result:
		// _, err := b.Send(user, text)
		// if err != nil {
		// 	return err
		// }

		// return nil
		// // Instead, prefer a context short-hand:
		return c.Send("反弹")
	})

	go sendDailyNotifications(b)

	b.Start()
}

func sendDailyNotifications(bot *telebot.Bot) {
	for {
		now := time.Now().Format("15:04")
		if now == notificationTime {
			// 发送每日通知消息给所有用户
			users := []string{"m4kvkg1x0gucjhcceikj"} // 在这里添加你想要通知的用户的 ID
			for _, userID := range users {
				msg := "这是每日通知！"
				_, err := bot.Send(telebot.ChatID(userID), msg)
				if err != nil {
					log.Printf("无法发送消息给用户 %s: %v", userID, err)
				}
			}
			time.Sleep(time.Minute) // 避免重复发送通知
		} else {
			time.Sleep(30 * time.Second) // 每 30 秒检查一次当前时间
		}
	}
}
