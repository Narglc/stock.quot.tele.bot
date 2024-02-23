package schedule

import "gopkg.in/telebot.v3"

type Task struct {
	Name string
	Time string
	Msg  string
}

func ScheduleTask(bot *telebot.Bot) {
	go sendGoHomeNotifications(bot)
}
