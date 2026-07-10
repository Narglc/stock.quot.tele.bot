package schedule

import (
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	"gopkg.in/telebot.v3"
)

// broadcastCrypto 向已注册的群播报一次 BTC / ETH 行情（由 cron 每小时整点触发，
// 以及启动时立即触发一次，见 ScheduleTask）。
func broadcastCrypto(bot *telebot.Bot) {
	if GroupCount() == 0 {
		return
	}

	tickers, err := stocks.GetCryptoQuotes()
	if err != nil {
		log.Warnf("crypto 播报获取行情失败，跳过本次: %v", err)
		return
	}
	fng, _ := stocks.GetFearGreed() // best-effort，失败则不带情绪指标
	msg := stocks.FormatCryptoMessage(tickers, fng)

	broadcastToGroups(bot, msg)
	log.Infof("crypto 播报完成: %s", msg)
}
