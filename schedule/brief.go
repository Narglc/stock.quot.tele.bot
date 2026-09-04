package schedule

import (
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
)

// refreshBrief 每小时组装一次盘面快照并推给网页。
//
// 不发 Telegram：这是给网页和 agent 用的数据，不是给人看的播报。
// 人看的那条走 checkCryptoAlerts（有异动才响）和 dailyDigest（早八保底）。
func refreshBrief(symbol string) {
	if !stocks.LiqPushEnabled() {
		return
	}
	b, err := stocks.GetMarketBrief(symbol)
	if err != nil {
		log.Warnf("盘面快照组装失败 %s: %v", symbol, err)
		return
	}
	if !b.Health.OK {
		// 仍然推：网页需要显示"数据不完整"这件事本身，藏起来只会让人误以为一切正常
		log.Warnf("盘面快照 %s 健康度异常: %v", symbol, b.Health.Reasons)
	}
	stocks.PushBriefSnapshot(b)
}

// refreshSeries 每天刷新一次长周期序列（价格+MA200、持仓量）。
//
// 与 brief 分开：序列一天才多一个点，跟着每小时的 brief 一起推等于反复搬运
// 几十 KB 不变的历史数据。
func refreshSeries(symbol string) {
	if !stocks.LiqPushEnabled() {
		return
	}
	ms, err := stocks.GetMarketSeries(symbol)
	if err != nil {
		log.Warnf("长周期序列组装失败 %s: %v", symbol, err)
		return
	}
	stocks.PushSeriesSnapshot(ms)
}
