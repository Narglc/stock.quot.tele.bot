package schedule

import (
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	"gopkg.in/telebot.v3"
)

// sendCryptoBroadcast 每小时整点向已注册的群播报 BTC / ETH 行情。
func sendCryptoBroadcast(bot *telebot.Bot) {
	// 先对齐到下一个整点，之后每小时触发一次
	now := time.Now()
	next := now.Truncate(time.Hour).Add(time.Hour)
	time.Sleep(time.Until(next))

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	broadcastCrypto(bot)
	for range ticker.C {
		broadcastCrypto(bot)
	}
}

func broadcastCrypto(bot *telebot.Bot) {
	if len(GroupMap) == 0 {
		return
	}

	tickers, err := stocks.GetCryptoQuotes()
	if err != nil {
		log.Warnf("crypto 播报获取行情失败，跳过本次: %v", err)
		return
	}
	msg := stocks.FormatCryptoMessage(tickers)

	for group := range GroupMap {
		if _, err := bot.Send(telebot.ChatID(group), msg); err != nil {
			log.Warnf("crypto 播报发送失败 group:%d err:%v", group, err)
		}
	}
	log.Infof("crypto 播报完成: %s", msg)
}
