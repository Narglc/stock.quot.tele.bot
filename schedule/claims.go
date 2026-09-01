package schedule

import (
	"time"

	"github.com/narglc/stock.quot.tele.bot/domain/claims"
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"gopkg.in/telebot.v3"
)

// claimStore / claimPrice 由 main 注入，避免 schedule 反向依赖 handler。
var (
	claimStore claims.Store
	claimPrice claims.PriceFn
)

// SetClaimDeps 注入 claims 检查所需的依赖（store 为 nil 则不启用检查）。
func SetClaimDeps(store claims.Store, price claims.PriceFn) {
	claimStore, claimPrice = store, price
}

// checkClaims 每小时跑一次：证伪条件触发就立刻通知，到期的转成待结案。
//
// 通知发回**记录这条 claim 的那个 chat**，而不是广播给所有群——
// 判断是谁记的就归谁跟进。
func checkClaims(bot *telebot.Bot) {
	if claimStore == nil || claimPrice == nil {
		return
	}

	events, err := claims.Check(claimStore, claimPrice, time.Now().In(beijingLoc))
	if err != nil {
		log.Warnf("claims 检查失败: %v", err)
		return
	}
	if len(events) == 0 {
		return
	}

	for _, e := range events {
		msg := claims.FormatEvent(e)
		if msg == "" {
			continue
		}
		// 被证伪是要打断人的（判断错了得知道）；到期提醒静音即可。
		var opts []interface{}
		if e.Kind == claims.EventExpired {
			opts = append(opts, telebot.Silent)
		}
		if _, serr := bot.Send(telebot.ChatID(e.Claim.ChatID), msg, opts...); serr != nil {
			log.Warnf("claim 通知发送失败 chat:%d id:%s: %v", e.Claim.ChatID, e.Claim.ID, serr)
			if IsBotEvicted(serr) {
				UnregisterGroup(e.Claim.ChatID)
			}
		}
	}
	log.Infof("claims 检查完成，%d 条状态变化", len(events))
}
