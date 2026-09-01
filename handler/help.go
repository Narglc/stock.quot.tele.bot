package handler

import (
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	tele "gopkg.in/telebot.v3"
)

// helpText 是 /help 的说明文案。纯文本（不走 HTML/Markdown，避免转义问题）。
const helpText = `🤖 大师使用说明

📈 币圈行情
/price [币种]   查即时行情，不带参默认 BTC/ETH
              例：/price btc eth sol（含涨跌/成交额/持仓量/多空比）
              也可在任意聊天里输入 @大师 btc eth 直接查（inline）
/liqmap [币种]  清算图：热力图 + 清算地图两张图，默认 BTC（如 /liqmap eth）

📊 A股自选
/watch              查看自选股行情
/watch add 600519   加入自选（可一次多个）
/watch del 600519   移出自选
/watch clear        清空
              每交易日 15:05 收盘后自动播报一次

🎯 判断追踪
/claim <判断> | 证伪 <条件> | 到期 <时长>
              记一条带证伪条件的判断，例：
              /claim BTC 会去扫上方大簇 | 证伪 BTC<118000 | 到期 3d
              证伪条件写成 BTC<118000 这种会自动检查，触发立刻通知
/claims       看追踪中的（加 all 看全部，export 导出 jsonl）
/resolve <id> correct|wrong|partial [说明]   到期后结案

⏰ 定时提醒
/register     把本群登记进提醒名单，之后才会收到：
              · 工作日起床/午饭/下班等提醒
              · 币圈行情异动提醒（费率翻转/持仓骤变/突破24h高低/情绪跨档）
              · 每天早八一条完整行情面板

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
