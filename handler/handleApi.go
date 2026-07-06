package handler

import (
	"bytes"
	"io"
	"math/rand"
	"net/http"
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
		// 大图兜底：直接按 URL 发送常因超过 5MB / 尺寸限制失败，
		// 这里下载后作为文件（Document，上限 50MB 且不校验图片尺寸）重发。
		if picUrl != "" {
			if doc, derr := downloadAsDocument(picUrl); derr == nil {
				if _, serr := c.Bot().Send(chat, doc); serr == nil {
					return nil
				} else {
					log.Warnf("Send %s as document fail, err:%+v", payload, serr)
				}
			} else {
				log.Warnf("download %s fail, err:%+v", payload, derr)
			}
		}
		_, err = c.Bot().Send(chat, "图图太大了，小老弟我搬不动呀！")
		return err
	}
	// 图片存储到redis
	dao.SavePhotos(msg.Photo.File.FileID)

	return nil
}

// downloadAsDocument 下载图片并封装为 Telegram 文件（Document）。
// 用于原图过大、无法按图片发送时的兜底，可绕过 sendPhoto 的尺寸/大小限制。
func downloadAsDocument(url string) (*tele.Document, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return &tele.Document{
		File:     tele.FromReader(bytes.NewReader(data)),
		FileName: "wakeup.jpg",
		Caption:  "大师助你提神醒脑（原图）",
	}, nil
}

func Sticker(c tele.Context) error {
	var (
		user = c.Sender() // 私聊时, user == chat
		chat = c.Chat()   // 群聊时, user = sender, chat=group
		text = c.Text()
	)

	fileid, err := dao.GetRandomSticker()
	if err != nil {
		log.Infof("rds err: %v", err.Error())
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
