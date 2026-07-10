package handler

import (
	"fmt"
	"strings"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	tele "gopkg.in/telebot.v3"
)

// Price 处理 /price 指令：即时查询行情。
//   - /price          —— 查默认币种（BTC/ETH）
//   - /price btc eth sol —— 查指定币种（符号大小写不敏感，空格分隔）
func Price(c tele.Context) error {
	var (
		user    = c.Sender()
		chat    = c.Chat()
		payload = strings.TrimSpace(c.Message().Payload)
	)

	var symbols []string
	if payload != "" {
		symbols = strings.Fields(payload)
	}

	quotes, unknown, err := stocks.GetCryptoQuotesBySymbols(symbols)
	log.Infof("sender:[%d - %s] chat:[%d - %s] /price payload:%q unknown:%v err:%v",
		user.ID, user.FirstName, chat.ID, chat.Title, payload, unknown, err)

	if err != nil {
		return c.Send("行情拉取失败了，稍后再试试 🥲")
	}

	var b strings.Builder
	if len(quotes) > 0 {
		fng, _ := stocks.GetFearGreed() // best-effort，失败则不带情绪指标
		b.WriteString(stocks.FormatCryptoMessage(quotes, fng))
	}
	if len(unknown) > 0 {
		b.WriteString(fmt.Sprintf("\n❓ 不认识这些符号：%s\n试试 btc eth sol bnb xrp doge 等主流币", strings.Join(unknown, " ")))
	}
	if b.Len() == 0 {
		return c.Send("没查到行情，试试 /price btc eth")
	}

	return c.Send(strings.TrimRight(b.String(), "\n"))
}
