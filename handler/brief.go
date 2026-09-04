package handler

import (
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	tele "gopkg.in/telebot.v3"
)

// Brief 处理 /brief [币种]：一次性给出完整盘面（指标 + 衍生品 + 清算簇 + 宏观）。
func Brief(c tele.Context) error {
	symbol := "BTC"
	if p := c.Message().Payload; p != "" {
		symbol = p
	}
	log.Infof("sender:[%d - %s] chat:[%d] /brief %s", c.Sender().ID, c.Sender().FirstName, c.Chat().ID, symbol)

	b, err := stocks.GetMarketBrief(symbol)
	if err != nil {
		return c.Send("盘面拉取失败 🥲：" + err.Error())
	}
	return c.Send(stocks.FormatBrief(b))
}
