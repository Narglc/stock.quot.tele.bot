package handler

import (
	"bytes"
	"fmt"
	"strings"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	tele "gopkg.in/telebot.v3"
)

// apifyToken 由 main 在启动时经 SetApifyToken 注入，供 /liqmap 拉清算图数据。
var apifyToken string

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
	symbol := strings.ToUpper(strings.TrimSpace(c.Message().Payload))
	if symbol == "" {
		symbol = "BTC"
	}
	log.Infof("sender:[%d - %s] chat:[%d - %s] /liqmap %s", user.ID, user.FirstName, chat.ID, chat.Title, symbol)

	// 反馈：清算图数据接口较慢（可能几十秒），先给"上传图片"状态 + 一条占位消息，
	// 失败时把原因编辑进这条占位消息，避免像卡住/无响应。
	_ = c.Notify(tele.UploadingPhoto)
	ph, _ := c.Bot().Send(chat, "⏳ pulling data… 正在拉取清算图")

	h, err := stocks.GetLiqHeatmap(symbol, apifyToken)
	if err != nil {
		log.Warnf("/liqmap %s 拉取失败: %v", symbol, err)
		return updatePlaceholder(c, ph, "清算图拉取失败 🥲\n原因："+err.Error())
	}
	m := h.ToMap()

	heatPNG, err1 := stocks.RenderLiqHeatmapPNG(h)
	mapPNG, err2 := stocks.RenderLiqMapPNG(m)
	if err1 != nil || err2 != nil {
		log.Warnf("/liqmap %s 渲染失败: %v %v", symbol, err1, err2)
		return updatePlaceholder(c, ph, fmt.Sprintf("清算图渲染失败 🥲\n原因：%v %v", err1, err2))
	}

	// 成功：删占位消息，发相册。
	if ph != nil {
		_ = c.Bot().Delete(ph)
	}
	album := tele.Album{
		&tele.Photo{File: tele.FromReader(bytes.NewReader(heatPNG)), Caption: stocks.FormatLiqMap(m, 6)},
		&tele.Photo{File: tele.FromReader(bytes.NewReader(mapPNG))},
	}
	_, err = c.Bot().SendAlbum(chat, album)
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
