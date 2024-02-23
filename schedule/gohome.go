package schedule

import (
	"fmt"
	"time"

	"gopkg.in/telebot.v3"
)

var TaskList = []Task{
	{"午饭", "03:50", "该吃午饭了，兄弟们!!!"},
	{"中场", "07:00", "三点几啦！饮茶先啦！"},
	{"下班", "09:55", "准备下班了，老铁们!!!"},
	{"下班", "09:57", "仲有三魂钟，激不激动，兴不兴奋???"},
	{"下班", "10:00", "滚吧！小老弟，明天见!"},
}

type ChatInfo struct {
	Type      string
	Title     string
	FirstName string
	LastName  string
	UserName  string
}

var GroupMap map[int64]ChatInfo = make(map[int64]ChatInfo)

// func init(){
// 	GroupMap = make(map[int64]ChatInfo)
// }

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
