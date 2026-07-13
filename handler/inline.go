package handler

import (
	"fmt"
	"strings"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	tele "gopkg.in/telebot.v3"
)

// InlineQuery 处理 inline 查询（在任意聊天里输入 @bot [币种...]）。
//   - @bot            —— 默认 BTC/ETH
//   - @bot btc eth sol —— 指定币种
// 返回「全部」+ 每个币种各一条结果，用户点选后把对应行情发到当前聊天。
// 需先在 @BotFather 用 /setinline 为 bot 开启 inline 模式，否则 @bot 不出结果。
func InlineQuery(c tele.Context) error {
	q := c.Query()
	var symbols []string
	if t := strings.TrimSpace(q.Text); t != "" {
		symbols = strings.Fields(t)
	}

	quotes, unknown, err := stocks.GetCryptoQuotesBySymbols(symbols, false) // inline 求快，不拉衍生品
	log.Infof("inline sender:[%d - %s] query:%q unknown:%v err:%v",
		q.Sender.ID, q.Sender.FirstName, q.Text, unknown, err)

	if err != nil {
		return answerSingle(c, "err", "⚠️ 行情拉取失败", "稍后再试试", "行情拉取失败了，稍后再试试 🥲")
	}
	if len(quotes) == 0 {
		hint := "试试 btc eth sol bnb xrp doge 等主流币"
		return answerSingle(c, "empty", "❓ 没查到行情", hint, "没查到行情，"+hint)
	}

	fng, _ := stocks.GetFearGreed() // best-effort

	var results tele.Results

	// 多个币种时，置顶一条「全部」，一次发送整份面板。
	if len(quotes) > 1 {
		names := make([]string, len(quotes))
		for i, qt := range quotes {
			names[i] = qt.Name
		}
		all := &tele.ArticleResult{
			Title:       "📊 全部 · " + strings.Join(names, " "),
			Description: fmt.Sprintf("一次发送全部 %d 个币种行情", len(quotes)),
		}
		// 必须用 InputMessageContent；ArticleResult.Text（message_text）是已废弃字段，
		// 现代 Telegram API 会因缺 input_message_content 拒掉整个 answerInlineQuery。
		all.SetContent(&tele.InputTextMessageContent{Text: stocks.FormatCryptoMessage(quotes, fng)})
		all.SetResultID("all")
		results = append(results, all)
	}

	// 每个币种一条结果。
	for _, qt := range quotes {
		r := &tele.ArticleResult{
			Title: fmt.Sprintf("%s  $%s", qt.Name, stocks.FormatPrice(qt.Price)),
			Description: fmt.Sprintf("24h %+.2f%% · 7d %+.2f%% · 距ATH %+.2f%%",
				qt.ChangePercent, qt.Change7d, qt.ATHChangePct),
		}
		r.SetContent(&tele.InputTextMessageContent{Text: stocks.FormatCryptoMessage([]stocks.CryptoQuote{qt}, fng)})
		r.SetResultID(qt.Name)
		results = append(results, r)
	}

	return c.Answer(&tele.QueryResponse{
		Results:   results,
		CacheTime: 30, // 行情 30s 内可复用缓存
	})
}

// answerSingle 用一条文本结果回应 inline 查询（错误/空结果提示）。
func answerSingle(c tele.Context, id, title, desc, text string) error {
	r := &tele.ArticleResult{Title: title, Description: desc}
	r.SetContent(&tele.InputTextMessageContent{Text: text})
	r.SetResultID(id)
	return c.Answer(&tele.QueryResponse{
		Results:   tele.Results{r},
		CacheTime: 5,
	})
}
