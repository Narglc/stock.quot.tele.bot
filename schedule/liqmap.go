package schedule

import (
	"bytes"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	"gopkg.in/telebot.v3"
)

// broadcastLiqmap 向已注册群播报一次清算图（热力图 + 清算地图相册 + 文字摘要）。
// 由早八/晚八两条 cron 触发（见 ScheduleTask）。apifyToken 为空则跳过。
func broadcastLiqmap(bot *telebot.Bot, symbol, apifyToken string) {
	if apifyToken == "" || GroupCount() == 0 {
		return
	}
	h, err := stocks.GetLiqHeatmap(symbol, apifyToken)
	if err != nil {
		log.Warnf("清算图播报拉取失败 %s: %v", symbol, err)
		return
	}
	m := h.ToMap()
	heat, e1 := stocks.RenderLiqHeatmapPNG(h)
	mp, e2 := stocks.RenderLiqMapPNG(m)
	if e1 != nil || e2 != nil {
		log.Warnf("清算图渲染失败 %s: %v %v", symbol, e1, e2)
		return
	}
	caption := stocks.FormatLiqMap(m, 6)

	for _, id := range SnapshotGroupIDs() {
		// 每群用新的 reader（bytes.Reader 上传后即耗尽，不能跨群复用）。
		album := telebot.Album{
			&telebot.Photo{File: telebot.FromReader(bytes.NewReader(heat)), Caption: caption},
			&telebot.Photo{File: telebot.FromReader(bytes.NewReader(mp))},
		}
		if _, err := bot.SendAlbum(telebot.ChatID(id), album); err != nil {
			log.Warnf("清算图相册发送失败 group:%d: %v", id, err)
			if IsBotEvicted(err) {
				UnregisterGroup(id)
			}
		}
	}
	log.Infof("清算图播报完成 symbol=%s", symbol)
}
