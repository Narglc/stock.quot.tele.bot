package handler

import (
	"strings"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	tele "gopkg.in/telebot.v3"
)

// apifyToken 由 main 在启动时经 SetApifyToken 注入，供 /liqmap 拉清算图数据。
var apifyToken string

// liqTopN 是清算地图文字摘要里上/下方各列出的簇数量。
const liqTopN = 6

// SetApifyToken 注入 Apify token（空则 /liqmap 提示未启用）。
func SetApifyToken(t string) { apifyToken = t }

// Liqmap 处理 /liqmap [币种]：拉清算热力图，渲染「热力图 + 清算地图」两张 PNG 作为相册发出，
// 首图附文字摘要。默认 BTC。
func Liqmap(c tele.Context) error {
	var (
		user = c.Sender()
		chat = c.Chat()
	)
	if apifyToken == "" {
		return c.Send("清算图未启用（服务端未配置 APIFY_TOKEN）")
	}
	symbol := strings.TrimSpace(c.Message().Payload)
	if symbol == "" {
		symbol = "BTC"
	}
	log.Infof("sender:[%d - %s] chat:[%d - %s] /liqmap %s", user.ID, user.FirstName, chat.ID, chat.Title, symbol)

	// 反馈：清算图数据接口较慢（可能几十秒），先给"上传图片"状态 + 一条占位消息，
	// 失败时把原因编辑进这条占位消息，避免像卡住/无响应。
	_ = c.Notify(tele.UploadingPhoto)
	ph, _ := c.Bot().Send(chat, "⏳ pulling data… 正在拉取清算图")

	// 拉数据 + 渲染两张图 + 生成摘要，与定时播报共用同一条链路（含 10min 缓存）。
	album, err := stocks.BuildLiqAlbum(symbol, apifyToken, liqTopN)
	if err != nil {
		log.Warnf("/liqmap %s 失败: %v", symbol, err)
		return updatePlaceholder(c, ph, "清算图拉取失败 🥲\n原因："+err.Error())
	}

	// 成功：删占位消息，发相册。
	if ph != nil {
		_ = c.Bot().Delete(ph)
	}
	_, err = c.Bot().SendAlbum(chat, stocks.LiqAlbumPhotos(album))
	return err
}

// updatePlaceholder 把占位消息编辑成结果/错误文本；无占位则直接发一条。
func updatePlaceholder(c tele.Context, ph *tele.Message, text string) error {
	if ph != nil {
		_, err := c.Bot().Edit(ph, text)
		return err
	}
	return c.Send(text)
}
