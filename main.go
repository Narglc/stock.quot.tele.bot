package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"gopkg.in/telebot.v3"
	tele "gopkg.in/telebot.v3"
)

const (
	GoHomeTime = "09:55"
	LunchTime  = "03:50"
)

type Task struct {
	Name string
	Time string
	Msg  string
}

var TaskList = []Task{
	{"午饭", "03:50", "该吃午饭了，兄弟们!!!"},
	{"中场", "07:00", "三点几啦！饮茶先啦！"},
	{"下班", "09:55", "准备下班了，老铁们!!!"},
	{"下班", "09:57", "仲有三魂钟，激不激动，兴不兴奋???"},
	{"下班", "10:00", "GUN!! YOU CAN!! SEE YOU TOMMOROW"},
}

type ChatInfo struct {
	Type      string
	Title     string
	FirstName string
	LastName  string
	UserName  string
}

var GroupMap map[int64]ChatInfo

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
	GroupMap = make(map[int64]ChatInfo)

	b.Handle("/register", func(c tele.Context) error {
		// All the text messages that weren't
		// captured by existing handlers.

		var (
			user = c.Sender()
			chat = c.Chat()
			text = c.Text()
		)
		chatinfo := ChatInfo{
			Type:      string(chat.Type),
			Title:     chat.Title,
			FirstName: chat.FirstName,
			LastName:  chat.LastName,
			UserName:  chat.Username,
		}

		fmt.Printf("sender:[%d - %s] chatinfo:[%+v], text:%+v\n", user.ID, user.FirstName, chatinfo, text)

		var err error
		if _, ok := GroupMap[chat.ID]; ok {
			_, err = b.Send(chat, getRandomResponse())
		} else {
			_, err = b.Send(chat, "大师已就位！敬请期待！")
			if err == nil {
				GroupMap[chat.ID] = chatinfo
			}
			fmt.Printf("register GroupList: %+v\n", GroupMap)
		}

		return err
	})

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

	go sendGoHomeNotifications(b)

	b.Start()
}

func sendGoHomeNotifications(bot *telebot.Bot) {
	var err error
	for {
		now := time.Now()
		weekday := now.Weekday()

		// 检查是否为工作日（周一至周五）
		if weekday >= time.Monday && weekday <= time.Friday {
			currentTime := now.Format("15:04")
			for _, task := range TaskList {
				if currentTime == task.Time {
					fmt.Printf("GroupMap: %+v\n", GroupMap)
					for group := range GroupMap {
						_, err = bot.Send(telebot.ChatID(group), task.Msg)
					}
					fmt.Printf("sendNotification: Task:%s Time:%s, Msg:%s, err:%+v", task.Name, task.Time, task.Msg, err)
					time.Sleep(time.Minute) // 避免重复发送通知
				}
			}
		}

		time.Sleep(30 * time.Second) // 每 30 秒检查一次当前时间
	}
}

// getRandomResponse 从预定义的字符串切片中随机选择一个字符串并返回
func getRandomResponse() string {
	responses := []string{
		"不要调戏为湿啦！",
		"有完没完了？",
		"没想到你是这样的人！渣男！",
		"马楼，别玩了！",
		"叼毛，好好工作！",
		"Goodbye!",
	}

	// 初始化随机数种子
	rand.Seed(time.Now().UnixNano())

	// 随机选择一个索引
	index := rand.Intn(len(responses))

	// 返回随机选择的字符串
	return responses[index]
}
