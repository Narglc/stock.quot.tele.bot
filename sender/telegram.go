package sender

import (
	"strconv"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/tgmd"
	tele "gopkg.in/telebot.v3"
)

// telebotAPI 是 *tele.Bot 的最小子集，便于测试打桩。
type telebotAPI interface {
	Send(to tele.Recipient, what interface{}, opts ...interface{}) (*tele.Message, error)
}

// TelegramSender 把 markdown 转 HTML 后发给固定 target；HTML 失败降级纯文本。
type TelegramSender struct {
	bot    telebotAPI
	target int64
}

// NewTelegramSender 构造一个 TelegramSender，向固定 target chat 发送消息。
func NewTelegramSender(bot telebotAPI, target int64) *TelegramSender {
	return &TelegramSender{bot: bot, target: target}
}

func (t *TelegramSender) Name() string { return "telegram" }

func (t *TelegramSender) Send(md string) (string, error) {
	to := tele.ChatID(t.target)

	html, cerr := tgmd.Convert(md)
	if cerr == nil {
		if msg, err := t.bot.Send(to, html, tele.ModeHTML); err == nil {
			return strconv.Itoa(msg.ID), nil
		} else {
			log.Warnf("Telegram 发送 HTML 失败，降级纯文本重发: %v", err)
		}
	} else {
		log.Warnf("markdown 转换失败，降级纯文本: %v", cerr)
	}

	msg, err := t.bot.Send(to, md)
	if err != nil {
		return "", err
	}
	return strconv.Itoa(msg.ID), nil
}
