package handler

import (
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	tele "gopkg.in/telebot.v3"
)

// helpText 是 /help 的说明文案。纯文本（不走 HTML/Markdown，避免转义问题）。
const helpText = `🤖 大师使用说明

📈 行情
/price [币种]   查即时行情，不带参默认 BTC/ETH
              例：/price btc eth sol（含涨跌/成交额/持仓量/多空比）
              也可在任意聊天里输入 @大师 btc eth 直接查（inline）
/liqmap [币种]  清算图：热力图 + 清算地图两张图，默认 BTC（如 /liqmap eth）

⏰ 定时提醒
/register     把本群登记进提醒名单，之后才会收到：
              · 工作日起床/午饭/下班等提醒
              · 每小时整点的币圈行情播报

🖼 图 & 表情
/wakeup [图源]  随机来一张图提神醒脑，不带参随机选源
/sticker      随机发一张精选表情包

🎲 娱乐
/dice [类型]   掷动画骰子，默认 🎲；可选 飞镖/篮球/足球/老虎机/保龄球
/gen <描述>    AI 生图（本地 GPU），例：/gen a cyberpunk city at night
（你发给我的图片/表情会被自动收藏进库当贡品）

ℹ️ 其它
/help         就是这个说明啦`

// Help 处理 /help 指令：打印命令与功能说明。
func Help(c tele.Context) error {
	var (
		user = c.Sender()
		chat = c.Chat()
	)
	log.Infof("sender:[%d - %s] chat:[%d - %s] /help", user.ID, user.FirstName, chat.ID, chat.Title)
	return c.Send(helpText)
}
