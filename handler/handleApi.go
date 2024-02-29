package handler

import (
	"math/rand"
	"time"

	"github.com/narglc/stock.quot.tele.bot/dao"
	"github.com/narglc/stock.quot.tele.bot/domain/randompic"
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
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

	log.Infof("sender:[%d - %s] chatinfo:[%+v], text:%+v\n", user.ID, user.FirstName, chatinfo, text)

	var err error
	if _, ok := schedule.GroupMap[chat.ID]; ok {
		_, err = c.Bot().Send(chat, utils.GetRandomResponse())
	} else {
		_, err = c.Bot().Send(chat, "大师已就位！敬请期待！")
		if err == nil {
			schedule.GroupMap[chat.ID] = chatinfo
		}
		log.Infof("register GroupList: %+v\n", schedule.GroupMap)
	}

	return err
}

func OnText(c tele.Context) error {
	// All the text messages that weren't
	// captured by existing handlers.
	var (
		user = c.Sender()
		chat = c.Chat()
		text = c.Text()
	)

	log.Infof("sender:[%d - %s] chat:[%d - %s], text:%+v", user.ID, user.FirstName, chat.ID, chat.Title, text)

	_, err := c.Bot().Send(user, text)
	if err != nil {
		return err
	}

	return nil
}

func OnSticker(c tele.Context) error {
	var (
		user    = c.Sender()
		chat    = c.Chat()
		sticker = c.Message().Sticker
	)

	log.Infof("sender:[%d - %s] chat:[%d - %s], sticker[%s-%s]\n", user.ID, user.FirstName, chat.ID, chat.Title, sticker.File.FileID, sticker.File.UniqueID)

	// sticker存储到redis
	err := dao.SaveSticker(sticker.File.FileID)
	if err == nil {
		if _, err := c.Bot().Send(user, "你的贡品我收下了！"); err != nil {
			return err
		}
	}

	return nil
}

func OnPhoto(c tele.Context) error {
	var (
		user  = c.Sender()
		chat  = c.Chat()
		photo = c.Message().Photo
	)

	log.Infof("sender:[%d - %s] chat:[%d - %s], text:%+v\n", user.ID, user.FirstName, chat.ID, chat.Title, photo)

	// 图片存储到redis
	err := dao.SavePhotos(photo.File.FileID)
	if err == nil {
		if _, err := c.Bot().Send(user, "你的贡品我收下了！"); err != nil {
			return err
		}
	}

	return nil
}

func getRandomPicSrc() string {
	picSrc := []string{
		"lolimi",
		"lolicon",
		"sexnyan",
	}

	seed := time.Now().UnixNano()                                    // rand内部运算的随机数
	idx := rand.New(rand.NewSource(seed)).Int31n(int32(len(picSrc))) // rand计算得到的随机数

	return picSrc[idx]
}

func Wakeup(c tele.Context) error {
	var (
		user    = c.Sender() // 私聊时, user == chat
		chat    = c.Chat()   // 群聊时, user = sender, chat=group
		payload = c.Message().Payload
		text    = c.Text()
	)
	if payload == "" {
		payload = getRandomPicSrc()
	}

	var file tele.File
	picUrl, err := randompic.GetRandomPic(payload)
	if err != nil {
		file = tele.File{
			FileID: dao.DefaultSticker,
		}
	} else {
		file = tele.FromURL(picUrl)
	}

	log.Infof("sender:[%d - %s] chat:[%d - %s], text:%+v, payload:%+v picUrl:%+v \n", user.ID, user.FirstName, chat.ID, chat.Title, text, payload, picUrl)

	photo := &tele.Photo{
		File:    file,
		Caption: "大师助你提神醒脑",
	}

	msg, err := c.Bot().Send(chat, photo)
	if err != nil {
		log.Warnf("Send %s pic first time fail, err:%+v", payload, err)
		// 重发特定图一张图
		_, err = c.Bot().Send(chat, "图图太大了，小老弟我搬不动呀！")
		return err
	}
	// 图片存储到redis
	dao.SavePhotos(msg.Photo.File.FileID)

	return nil
}

func Sticker(c tele.Context) error {
	var (
		user = c.Sender() // 私聊时, user == chat
		chat = c.Chat()   // 群聊时, user = sender, chat=group
		text = c.Text()
	)

	fileid, err := dao.GetRandomSticker()
	if err != nil {
		fileid = dao.DefaultSticker
	}

	log.Infof("sender:[%d - %s] chat:[%d - %s], text:%+v \n", user.ID, user.FirstName, chat.ID, chat.Title, text)
	stker := &tele.Sticker{
		File: tele.File{
			FileID: fileid,
			// UniqueID: "AgADKAcAAt7x2Vc",
			// FileSize: 168005,
		},
		// Width:    416,
		// Height:   512,
		// Animated: false,
		// Video:    true,
		// Type:     "regular",
	}

	_, err = c.Bot().Send(c.Chat(), stker)
	if err != nil {
		return err
	}

	return nil
}
