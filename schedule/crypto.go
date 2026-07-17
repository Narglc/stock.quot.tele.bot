package schedule

import (
	"strconv"

	"github.com/narglc/stock.quot.tele.bot/dao"
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	"gopkg.in/telebot.v3"
)

// broadcastCrypto 播报一次 BTC/ETH 行情（cron 每小时整点触发 + 启动时立即一次）。
// 采用"删掉上一条 + 静音发新"：群里始终只有一条、且在最底部方便一眼看到，
// 既不像每次新发那样刷屏堆积，也不像原地编辑那样被埋在历史里看不到。
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

	// 附加全市场概览（总市值/占比，best-effort；失败则不追加该段）。
	if g, gerr := stocks.GetGlobalMarket(); gerr == nil {
		msg += "\n\n" + stocks.FormatGlobalMarket(g)
	}

	for _, id := range SnapshotGroupIDs() {
		pushFreshBroadcast(bot, id, msg)
	}
	log.Infof("crypto 播报完成")
}

// pushFreshBroadcast 删掉上一条整点行情（若有），再静音发一条新的并记录 id。
// 效果：群里任意时刻只有一条行情、且始终在最底部；静音发送不刷通知。
func pushFreshBroadcast(bot *telebot.Bot, chatID int64, msg string) {
	if mid, err := dao.GetBroadcastMsg(chatID); err == nil && mid != "" {
		// 删旧失败忽略（可能已被用户删/太旧），不影响发新。
		_ = bot.Delete(&telebot.StoredMessage{MessageID: mid, ChatID: chatID})
	}

	sent, err := bot.Send(telebot.ChatID(chatID), msg, telebot.Silent)
	if err != nil {
		log.Warnf("整点行情发送失败 group:%d err:%v", chatID, err)
		if IsBotEvicted(err) {
			UnregisterGroup(chatID)
			dao.DelBroadcastMsg(chatID)
		}
		return
	}
	if serr := dao.SaveBroadcastMsg(chatID, strconv.Itoa(sent.ID)); serr != nil {
		log.Warnf("记录整点行情消息 id 失败 group:%d: %v", chatID, serr)
	}
}
