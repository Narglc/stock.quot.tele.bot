package handler

import (
	"fmt"

	"github.com/narglc/stock.quot.tele.bot/schedule"
	"github.com/narglc/stock.quot.tele.bot/utils"
	tele "gopkg.in/telebot.v3"
)

func Register(c tele.Context) error {
	// All the text messages that weren't
	// captured by existing handlers.

	var (
		user = c.Sender()
		chat = c.Chat()
		text = c.Text()
	)
	chatinfo := schedule.ChatInfo{
		Type:      string(chat.Type),
		Title:     chat.Title,
		FirstName: chat.FirstName,
		LastName:  chat.LastName,
		UserName:  chat.Username,
	}

	fmt.Printf("sender:[%d - %s] chatinfo:[%+v], text:%+v\n", user.ID, user.FirstName, chatinfo, text)

	var err error
	if _, ok := schedule.GroupMap[chat.ID]; ok {
		_, err = c.Bot().Send(chat, utils.GetRandomResponse())
	} else {
		_, err = c.Bot().Send(chat, "大师已就位！敬请期待！")
		if err == nil {
			schedule.GroupMap[chat.ID] = chatinfo
		}
		fmt.Printf("register GroupList: %+v\n", schedule.GroupMap)
	}

	return err
}
