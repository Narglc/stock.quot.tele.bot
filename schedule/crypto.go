package schedule

import (
	"strconv"

	"github.com/narglc/stock.quot.tele.bot/dao"
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	"gopkg.in/telebot.v3"
)

// 播报分两条线，刻意不同：
//
//   - checkCryptoAlerts（每小时）—— 只在行情**跨越**某个状态时才发（费率翻转、
//     持仓骤变、突破 24h 高低、恐惧贪婪跨档）。平时完全静默。
//     事件消息**不静音、不删旧**：它是稀疏的、有历史价值的，该留在群里。
//
//   - dailyDigest（每天早八）—— 无条件发一次完整面板，作为保底心跳。
//     纯静默模式下「没消息」和「bot 挂了」看起来一模一样，所以需要它。
//     面板消息**静音 + 删旧发新**：它是例行的，群里只保留最新一条。
//
// 原来的做法是每小时无条件发完整面板——大部分整点数字并无实质变化，
// 久了就自动忽略了，等于没有播报。

// fetchMarket 拉一次行情快照（含衍生品）与情绪指标。
func fetchMarket() ([]stocks.CryptoQuote, *stocks.FearGreed, error) {
	quotes, err := stocks.GetCryptoQuotes()
	if err != nil {
		return nil, nil, err
	}
	fng, _ := stocks.GetFearGreed() // best-effort，失败则不带情绪指标
	return quotes, fng, nil
}

// checkCryptoAlerts 每小时跑一次：拉行情、检测事件，只有真的发生了跨越才播报。
func checkCryptoAlerts(bot *telebot.Bot) {
	if GroupCount() == 0 {
		return
	}

	quotes, fng, err := fetchMarket()
	if err != nil {
		log.Warnf("行情事件检测获取行情失败，跳过本次: %v", err)
		return
	}

	alerts := stocks.DetectAlerts(quotes, fng)
	if len(alerts) == 0 {
		log.Infof("行情事件检测完成：无异动，保持静默")
		return
	}

	msg := stocks.FormatAlerts(alerts)
	for _, id := range SnapshotGroupIDs() {
		// 不静音：这是「值得打断」的事件，静音就失去意义了。
		// 不删旧：事件是历史记录，删掉就没法回溯今天发生过什么。
		if _, err := bot.Send(telebot.ChatID(id), msg); err != nil {
			log.Warnf("行情异动发送失败 group:%d err:%v", id, err)
			if IsBotEvicted(err) {
				UnregisterGroup(id)
			}
		}
	}
	log.Infof("行情异动播报完成，共 %d 条事件", len(alerts))
}

// dailyDigest 每天早八发一次完整面板（保底心跳）。
func dailyDigest(bot *telebot.Bot) {
	if GroupCount() == 0 {
		return
	}

	quotes, fng, err := fetchMarket()
	if err != nil {
		log.Warnf("每日面板获取行情失败，跳过本次: %v", err)
		return
	}
	msg := stocks.FormatCryptoMessage(quotes, fng)

	// 附加全市场概览（总市值/占比，best-effort；失败则不追加该段）。
	if g, gerr := stocks.GetGlobalMarket(); gerr == nil {
		msg += "\n\n" + stocks.FormatGlobalMarket(g)
	}

	for _, id := range SnapshotGroupIDs() {
		pushFreshBroadcast(bot, id, msg)
	}
	log.Infof("每日行情面板播报完成")
}

// pushFreshBroadcast 删掉上一条每日面板（若有），再静音发一条新的并记录 id。
// 效果：群里任意时刻只有一条面板、且始终在最底部；静音发送不刷通知。
func pushFreshBroadcast(bot *telebot.Bot, chatID int64, msg string) {
	if mid, err := dao.GetBroadcastMsg(chatID); err == nil && mid != "" {
		// 删旧失败忽略（可能已被用户删/太旧），不影响发新。
		_ = bot.Delete(&telebot.StoredMessage{MessageID: mid, ChatID: chatID})
	}

	sent, err := bot.Send(telebot.ChatID(chatID), msg, telebot.Silent)
	if err != nil {
		log.Warnf("每日面板发送失败 group:%d err:%v", chatID, err)
		if IsBotEvicted(err) {
			UnregisterGroup(chatID)
			dao.DelBroadcastMsg(chatID)
		}
		return
	}
	if serr := dao.SaveBroadcastMsg(chatID, strconv.Itoa(sent.ID)); serr != nil {
		log.Warnf("记录每日面板消息 id 失败 group:%d: %v", chatID, serr)
	}
}
