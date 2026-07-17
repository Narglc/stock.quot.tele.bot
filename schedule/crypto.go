package schedule

import (
	"strconv"
	"strings"

	"github.com/narglc/stock.quot.tele.bot/dao"
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	"gopkg.in/telebot.v3"
)

// broadcastCrypto 播报一次 BTC/ETH 行情（cron 每小时整点触发 + 启动时立即一次）。
// 采用"原地编辑同一条消息"而非每次新发，避免每小时刷屏。
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
		editOrSendBroadcast(bot, id, msg)
	}
	log.Infof("crypto 播报完成")
}

// editOrSendBroadcast 优先原地编辑上次那条整点行情消息；无记录/编辑失败则新发并记录 id。
func editOrSendBroadcast(bot *telebot.Bot, chatID int64, msg string) {
	if mid, err := dao.GetBroadcastMsg(chatID); err == nil && mid != "" {
		stored := &telebot.StoredMessage{MessageID: mid, ChatID: chatID}
		if _, eerr := bot.Edit(stored, msg); eerr == nil {
			return // 原地更新成功，不产生新通知、不刷屏
		} else if strings.Contains(eerr.Error(), "not modified") {
			return // 内容与上次一致，也算成功
		} else {
			log.Warnf("整点行情编辑失败 group:%d msg:%s err:%v，改为新发", chatID, mid, eerr)
		}
	}

	sent, err := bot.Send(telebot.ChatID(chatID), msg)
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
