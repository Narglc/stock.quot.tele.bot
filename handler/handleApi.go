package handler

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/narglc/stock.quot.tele.bot/dao"
	"github.com/narglc/stock.quot.tele.bot/domain/randompic"
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/schedule"
	"github.com/narglc/stock.quot.tele.bot/utils"
	tele "gopkg.in/telebot.v3"
)

const (
	// sendPhoto 内联图片：上传体积上限 10MB
	maxPhotoBytes = 10 << 20
	// sendPhoto 要求图片 宽 + 高 ≤ 10000
	maxDimensionSum = 10000
	// sendDocument 文件：上限 50MB，超过它才真的发不了
	maxDocumentBytes = 50 << 20
	// 取图重试次数：连文件都发不了(超50MB/网络失败)才换下一张
	maxPicRetry = 3
)

// errPicTooLarge 表示图片连 Document(50MB) 都装不下，需要跳过换一张。
var errPicTooLarge = errors.New("pic exceeds telegram document limit")

// fetchedPic 是下载并判级后的图片：asPhoto 决定内联图片还是文件发送。
type fetchedPic struct {
	data    []byte
	asPhoto bool // true=可作为内联图片; false=太大, 需作为文件(Document)发送
}

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
	if schedule.IsRegistered(chat.ID) {
		_, err = c.Bot().Send(chat, utils.GetRandomResponse())
	} else {
		_, err = c.Bot().Send(chat, "大师已就位！敬请期待！")
		if err == nil {
			schedule.RegisterGroup(chat.ID, chatinfo)
		}
		log.Infof("register GroupList count: %d\n", schedule.GroupCount())
	}

	return err
}

// OnMyChatMember 监听 bot 自身成员状态变化：被踢出/移出群时实时注销该 chat。
func OnMyChatMember(c tele.Context) error {
	upd := c.Update().MyChatMember
	if upd == nil || upd.NewChatMember == nil {
		return nil
	}

	role := upd.NewChatMember.Role
	chat := upd.Chat
	if role == tele.Kicked || role == tele.Left {
		schedule.UnregisterGroup(chat.ID)
		log.Infof("bot 被移出 chat:[%d - %s] role:%s，已注销", chat.ID, chat.Title, role)
	}

	return nil
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

	// 复读机行为先注释掉（保留 handler，便于以后接入有意义的响应）。
	// _, err := c.Bot().Send(user, text)
	// if err != nil {
	// 	return err
	// }

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

// picCandidates 是参与随机的图源名单；新增图源在此登记即可参与。
var picCandidates = []string{"lolicon", "sexnyan", "nekos", "waifu", "picre"}

// getRandomPicSrc 按各源近期成功率加权选一个（老失败的源自动降权）。
func getRandomPicSrc() string {
	return randompic.WeightedPick(picCandidates)
}

func Wakeup(c tele.Context) error {
	var (
		user    = c.Sender() // 私聊时, user == chat
		chat    = c.Chat()   // 群聊时, user = sender, chat=group
		payload = c.Message().Payload
		text    = c.Text()
	)
	userSpecified := payload != ""

	// 多试几次：拉到一张 Telegram 装得下（≤50MB）的图再发。
	// 超过 Photo 限制的会作为文件(Document)发送，只有连 50MB 都超才跳过换下一张。
	var (
		pic     *fetchedPic
		picUrl  string
		lastErr error // 最后一次失败原因，兜底时带进消息
	)
	for i := 0; i < maxPicRetry; i++ {
		src := payload
		if !userSpecified {
			src = getRandomPicSrc() // 未指定图源时每次重试都换一个源
		}

		url, gerr := randompic.GetRandomPic(src)
		if gerr != nil {
			lastErr = gerr
			randompic.Record(src, false)
			log.Warnf("图源[%s]第%d次取图失败: %v", src, i+1, gerr)
			continue
		}
		if url == "" {
			lastErr = fmt.Errorf("图源[%s]返回空 url", src)
			randompic.Record(src, false)
			continue
		}

		p, verr := fetchPic(url)
		if verr != nil {
			lastErr = fmt.Errorf("下载图片失败(%s): %w", src, verr)
			randompic.Record(src, false)
			log.Warnf("图源[%s]第%d次取图不可用: url=%s err=%v", src, i+1, url, verr)
			continue
		}
		randompic.Record(src, true)
		pic, picUrl = p, url
		break
	}

	log.Infof("sender:[%d - %s] chat:[%d - %s], text:%+v, payload:%+v picUrl:%+v asPhoto:%v\n", user.ID, user.FirstName, chat.ID, chat.Title, text, payload, picUrl, pic != nil && pic.asPhoto)

	// 几次都没拿到合适的图 → 回落一张默认表情，并把失败原因一起告知
	if pic == nil {
		_, _ = c.Bot().Send(chat, &tele.Sticker{File: tele.File{FileID: dao.DefaultSticker}})
		reason := "未知原因"
		if lastErr != nil {
			reason = lastErr.Error()
		}
		_, err := c.Bot().Send(chat, fmt.Sprintf("取图失败了 🥲 先给你张默认的。\n原因：%s", reason))
		return err
	}

	// 尺寸/体积超过内联图片限制的，直接走文件发送
	if !pic.asPhoto {
		return sendAsDocument(c, chat, picUrl, pic.data)
	}

	photo := &tele.Photo{
		File:    tele.FromReader(bytes.NewReader(pic.data)),
		Caption: "大师助你提神醒脑",
	}

	msg, err := c.Bot().Send(chat, photo)
	if err != nil {
		// 内联图片发送失败（可能被 Telegram 判定尺寸/比例不合规），兜底转文件发送
		log.Warnf("Send %s as photo fail, 转文件重发, err:%+v", picUrl, err)
		return sendAsDocument(c, chat, picUrl, pic.data)
	}
	// 图片存储到redis
	dao.SavePhotos(msg.Photo.File.FileID)

	return nil
}

// sendAsDocument 把图片作为文件(Document)发送，保留原图画质、绕过 sendPhoto 的尺寸限制。
func sendAsDocument(c tele.Context, chat tele.Recipient, url string, data []byte) error {
	doc := &tele.Document{
		File:     tele.FromReader(bytes.NewReader(data)),
		FileName: picFileName(url),
		Caption:  "大师助你提神醒脑（原图较大，以文件发送）",
	}
	if _, err := c.Bot().Send(chat, doc); err != nil {
		log.Warnf("Send %s as document fail, err:%+v", url, err)
		_, err = c.Bot().Send(chat, "图图太大了，小老弟我搬不动呀！")
		return err
	}
	return nil
}

// fetchPic 下载图片并判级：≤10MB 且 宽+高≤10000 可内联为图片，否则作为文件发送。
// 连 50MB 都超返回 errPicTooLarge，让调用方换一张。返回数据可直接上传。
func fetchPic(url string) (*fetchedPic, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status %d", resp.StatusCode)
	}
	// 有 Content-Length 时先快速拒绝超大文件
	if resp.ContentLength > maxDocumentBytes {
		return nil, errPicTooLarge
	}

	// 最多读到文件上限 + 1 字节，超出即判定连文件都发不了
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDocumentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDocumentBytes {
		return nil, errPicTooLarge
	}

	asPhoto := len(data) <= maxPhotoBytes
	// 只解析图片头拿尺寸，宽 + 高 超限则改走文件发送
	if asPhoto {
		if cfg, _, derr := image.DecodeConfig(bytes.NewReader(data)); derr == nil {
			if cfg.Width+cfg.Height > maxDimensionSum {
				asPhoto = false
			}
		}
	}

	return &fetchedPic{data: data, asPhoto: asPhoto}, nil
}

// picFileName 从 URL 推断文件名（含扩展名），供 Document 发送时正确识别图片类型。
func picFileName(url string) string {
	name := path.Base(url)
	// 去掉可能的查询串
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		name = name[:i]
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return name
	default:
		return "wakeup.jpg"
	}
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
