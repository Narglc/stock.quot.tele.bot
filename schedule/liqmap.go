package schedule

import (
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	"gopkg.in/telebot.v3"
)

// liqTopN 是清算地图文字摘要里上/下方各列出的簇数量。
const liqTopN = 6

// broadcastLiqmap 向已注册群播报一次清算图（热力图 + 清算地图相册 + 文字摘要）。
// 由早八/晚八两条 cron 触发（见 ScheduleTask）。apifyToken 为空则跳过。
func broadcastLiqmap(bot *telebot.Bot, symbol, apifyToken string) {
	if apifyToken == "" || GroupCount() == 0 {
		return
	}

	// 与 /liqmap 共用同一条链路（含 10min 缓存）：如果刚有人手动查过，这里直接命中缓存。
	album, err := stocks.BuildLiqAlbum(symbol, apifyToken, liqTopN)
	if err != nil {
		log.Warnf("清算图播报失败 %s: %v", symbol, err)
		return
	}

	for _, id := range SnapshotGroupIDs() {
		if _, err := bot.SendAlbum(telebot.ChatID(id), stocks.LiqAlbumPhotos(album)); err != nil {
			log.Warnf("清算图相册发送失败 group:%d: %v", id, err)
			if IsBotEvicted(err) {
				UnregisterGroup(id)
			}
		}
	}
	log.Infof("清算图播报完成 symbol=%s", symbol)
}
