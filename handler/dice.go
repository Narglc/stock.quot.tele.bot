package handler

import (
	"strconv"
	"strings"
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	tele "gopkg.in/telebot.v3"
)

// Dice 处理 /dice [类型]：发一个动画骰子/飞镖/老虎机等，随机点数由 Telegram 结算，
// 动画播完后再补一句应景点评。默认 🎲。
//
//	/dice            🎲 骰子
//	/dice 飞镖/dart   🎯
//	/dice 篮球        🏀
//	/dice 足球        ⚽
//	/dice 老虎机/slot 🎰
//	/dice 保龄球      🎳
func Dice(c tele.Context) error {
	payload := strings.ToLower(strings.TrimSpace(c.Message().Payload))
	d := pickDice(payload)

	msg, err := c.Bot().Send(c.Chat(), d)
	if err != nil {
		return err
	}
	val := 0
	if msg.Dice != nil {
		val = msg.Dice.Value
	}
	log.Infof("sender:[%d - %s] /dice %s value=%d", c.Sender().ID, c.Sender().FirstName, d.Type, val)

	// 骰子动画在客户端要播几秒；延迟补点评，避免提前剧透结果。
	if comment := diceComment(d.Type, val); comment != "" {
		bot, chat := c.Bot(), c.Chat()
		go func() {
			time.Sleep(4 * time.Second)
			if _, err := bot.Send(chat, comment); err != nil {
				log.Warnf("/dice 点评发送失败: %v", err)
			}
		}()
	}
	return nil
}

// pickDice 按 payload 关键词选骰子类型，未命中用 🎲。
func pickDice(payload string) *tele.Dice {
	switch {
	case strings.ContainsAny(payload, "🎯") || strings.Contains(payload, "飞镖") || strings.Contains(payload, "dart"):
		return tele.Dart
	case strings.ContainsAny(payload, "🏀") || strings.Contains(payload, "篮球") || strings.Contains(payload, "basket"):
		return tele.Ball
	case strings.ContainsAny(payload, "⚽") || strings.Contains(payload, "足球") || strings.Contains(payload, "soccer") || strings.Contains(payload, "football"):
		return tele.Goal
	case strings.ContainsAny(payload, "🎰") || strings.Contains(payload, "老虎机") || strings.Contains(payload, "slot"):
		return tele.Slot
	case strings.ContainsAny(payload, "🎳") || strings.Contains(payload, "保龄") || strings.Contains(payload, "bowl"):
		return tele.Bowl
	default:
		return tele.Cube
	}
}

// diceComment 按类型和点数给一句应景点评（粤语梗调性）。
func diceComment(t tele.DiceType, v int) string {
	switch t {
	case "🎲": // 1-6
		switch v {
		case 6:
			return "六点！大师附体，今日无敌 🎉"
		case 1:
			return "一点…细佬今日水逆，饮杯茶定下神先"
		default:
			return "掷出咗 " + strconv.Itoa(v) + " 点"
		}
	case "🎯": // 1-6，6=正中红心
		if v == 6 {
			return "正中红心！稳如老狗 🎯"
		}
		return "差少少，再嚟一镖？"
	case "🏀": // 1-5，4/5=入
		if v >= 4 {
			return "入樽！靓仔 🏀"
		}
		return "打铁咗，练下先啦"
	case "⚽": // 1-5，4/5=入
		if v >= 4 {
			return "波入啦！⚽ 世界波"
		}
		return "射失咗，可惜可惜"
	case "🎰": // 1-64，64=777头奖
		switch v {
		case 64:
			return "777！头奖！发达啦 🎰💰"
		case 1, 22, 43:
			return "三个一样！细奖到手 🎰"
		default:
			return "差少少中奖，再嚟一铺？"
		}
	case "🎳": // 1-6，6=全中
		if v == 6 {
			return "全中 Strike！🎳 一击必杀"
		}
		return "剩返几支，补中啦"
	}
	return ""
}
