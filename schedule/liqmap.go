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

// refreshLiqSnapshot 定时拉一次热力图，目的只是让网页的快照保持新鲜。
//
// 拉取本身会经 GetLiqHeatmap 触发推送（见 stocks/liqmap.go），这里不需要再做什么。
// 与早八/晚八的播报 cron 对齐到同一分钟时，同币种串行锁 + 10min 缓存保证只打一次 Apify。
func refreshLiqSnapshot(symbol, apifyToken string) {
	if apifyToken == "" || !stocks.LiqPushEnabled() {
		return
	}
	if _, err := stocks.GetLiqHeatmap(symbol, apifyToken); err != nil {
		log.Warnf("定时刷新清算图快照失败 %s: %v", symbol, err)
		return
	}
	log.Infof("定时刷新清算图快照完成 symbol=%s", symbol)
}
