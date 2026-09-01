package schedule

import (
	"time"

	"github.com/narglc/stock.quot.tele.bot/dao"
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	"gopkg.in/telebot.v3"
)

// broadcastAShare 收盘后播报各 chat 的 A股自选股表现（每交易日 15:05）。
//
// 为什么定在收盘后而不是盘中：盘中数字随时会变，基于它下的结论没法事后验证。
// 15:05 拿到的就是当日定盘数据，这也是唯一值得写进记录的口径。
func broadcastAShare(bot *telebot.Bot) {
	lists, err := dao.LoadAllWatchlists()
	if err != nil {
		log.Warnf("读取自选股失败，跳过 A股播报: %v", err)
		return
	}
	if len(lists) == 0 {
		return
	}

	asOf := time.Now().In(beijingLoc)

	for chatID, codes := range lists {
		quotes, qerr := stocks.GetStockQuotes(codes)
		if qerr != nil {
			log.Warnf("A股播报拉行情失败 chat:%d: %v", chatID, qerr)
			continue
		}
		// 全部无成交说明当天不是交易日——cron 的 1-5 排得掉周末，排不掉节假日。
		if !stocks.AnyTraded(quotes) {
			log.Infof("A股全无成交，判定非交易日，跳过播报 chat:%d", chatID)
			continue
		}

		msg := stocks.FormatStockMessage(quotes, asOf)
		if _, serr := bot.Send(telebot.ChatID(chatID), msg, telebot.Silent); serr != nil {
			log.Warnf("A股播报发送失败 chat:%d: %v", chatID, serr)
			if IsBotEvicted(serr) {
				UnregisterGroup(chatID)
			}
		}
	}
	log.Infof("A股收盘播报完成，覆盖 %d 个 chat", len(lists))
}
