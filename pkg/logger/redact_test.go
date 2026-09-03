package base

import (
	"bytes"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
)

func TestRedact_TelegramBotToken(t *testing.T) {
	in := `telebot: Post "https://api.telegram.org/bot8149162400:AAHxBdnO4wWvsl4HJnjp-xP5X-ksLunGbzQ/sendMediaGroup": timeout`
	out := Redact(in)
	if strings.Contains(out, "AAHxBdnO4wWvsl4HJnjp") {
		t.Fatalf("token 未脱敏: %s", out)
	}
	if !strings.Contains(out, "bot[REDACTED]/sendMediaGroup") {
		t.Errorf("应保留方法名便于排障，实得: %s", out)
	}
	// 正常文本不该被误伤
	if got := Redact("robot 123: ok"); got != "robot 123: ok" {
		t.Errorf("误伤普通文本: %q", got)
	}
}

// 走完整 logger 出口：Infof 里的 token 到序列化后必须已经没了。
func TestLogger_RedactsAtOutput(t *testing.T) {
	var buf bytes.Buffer
	old := basicLogger.Out
	basicLogger.Out = &buf
	t.Cleanup(func() { basicLogger.Out = old })

	Warnf("发送失败: %s", `Post "https://api.telegram.org/bot123456789:ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcd/sendPhoto"`)
	if strings.Contains(buf.String(), "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Fatalf("日志出口未脱敏:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "bot[REDACTED]") {
		t.Errorf("应有占位符:\n%s", buf.String())
	}
	_ = log.InfoLevel
}
