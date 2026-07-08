package sender

import (
	"fmt"
	"strconv"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/tgmd"
	tele "gopkg.in/telebot.v3"
)

// telebotAPI 是 *tele.Bot 的最小子集，便于测试打桩。
type telebotAPI interface {
	Send(to tele.Recipient, what interface{}, opts ...interface{}) (*tele.Message, error)
}

// TelegramSender 把 markdown 转 HTML 后发给指定 chat；HTML 失败降级纯文本。
// 目标 chat 由每次调用的 recipient 决定（来自鉴权 JWT 的 chat_id 或配置默认目标）。
type TelegramSender struct {
	bot telebotAPI
}

// NewTelegramSender 构造一个 TelegramSender。
func NewTelegramSender(bot telebotAPI) *TelegramSender {
	return &TelegramSender{bot: bot}
}

func (t *TelegramSender) Name() string { return "telegram" }

func (t *TelegramSender) Send(md, recipient string) (string, error) {
	chatID, err := strconv.ParseInt(recipient, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid telegram recipient %q: %w", recipient, err)
	}
	to := tele.ChatID(chatID)

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
