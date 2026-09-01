package stocks

import (
	"strings"
	"testing"
	"time"
)

func TestFormatSpan(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, ""},
		{-time.Minute, ""},
		{42 * time.Minute, "42m"},
		{59 * time.Minute, "59m"},
		{90 * time.Minute, "1.5h"},
		{25 * time.Hour, "1.0d"},
	}
	for _, c := range cases {
		if got := formatSpan(c.in); got != c.want {
			t.Errorf("formatSpan(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 持仓变化必须带时间跨度：不标出来会被读成小时环比，
// 但它实际上是「距上次观测」，而上次观测时刻取决于调用方。
func TestFormatCryptoMessage_OIChangeCarriesSpan(t *testing.T) {
	q := CryptoQuote{
		Name: "BTC", Price: 60000, ChangePercent: 1.5,
		OpenInterestUSD: 12e9,
		OIChangePct:     2.4,
		OIChangeKnown:   true,
		OISpan:          58 * time.Minute,
	}
	msg := FormatCryptoMessage([]CryptoQuote{q}, nil)
	if !strings.Contains(msg, "+2.4% / 58m") {
		t.Errorf("持仓变化应带时间跨度，实得:\n%s", msg)
	}
}

// 首次观测没有基线，不应凭空显示一个变化幅度。
func TestFormatCryptoMessage_NoBaselineNoChange(t *testing.T) {
	q := CryptoQuote{
		Name: "BTC", Price: 60000,
		OpenInterestUSD: 12e9,
		OIChangeKnown:   false,
	}
	msg := FormatCryptoMessage([]CryptoQuote{q}, nil)
	if strings.Contains(msg, "%)") && strings.Contains(msg, "持仓 $12.00B(") {
		t.Errorf("无基线时不应显示持仓变化，实得:\n%s", msg)
	}
}

func TestFormatPrice(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{60123.456, "60,123.46"},
		{1234567.0, "1,234,567.00"},
		{0.5, "0.5000"},
		{0.00001234, "0.000012"},
		{-1234.5, "-1,234.50"},
		{0, "0.0000"}, // 价格为 0 只在数据缺失时出现，走 av<1 分支给 4 位小数
	}
	for _, c := range cases {
		if got := formatPrice(c.in); got != c.want {
			t.Errorf("formatPrice(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatCompact(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1.5e12, "1.50T"},
		{2.3e9, "2.30B"},
		{4.5e6, "4.50M"},
		{6700, "6.70K"},
		{123, "123"},
	}
	for _, c := range cases {
		if got := formatCompact(c.in); got != c.want {
			t.Errorf("formatCompact(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestOIInsight(t *testing.T) {
	if got := oiInsight(1.0, 0.2); got != "" {
		t.Errorf("变化过小不该解读，实得 %q", got)
	}
	if got := oiInsight(1.0, 2.0); !strings.Contains(got, "增仓上涨") {
		t.Errorf("涨+增仓，实得 %q", got)
	}
	if got := oiInsight(-1.0, 2.0); !strings.Contains(got, "增仓下跌") {
		t.Errorf("跌+增仓，实得 %q", got)
	}
}

func TestResolveSymbols(t *testing.T) {
	coins, unknown := ResolveSymbols([]string{"BTC", "btc", " eth ", "notacoin"})
	if len(coins) != 2 {
		t.Errorf("应去重且大小写不敏感，实得 %d 个: %+v", len(coins), coins)
	}
	if len(unknown) != 1 || unknown[0] != "notacoin" {
		t.Errorf("未识别符号应原样返回，实得 %v", unknown)
	}

	// 空输入回落默认币种
	def, _ := ResolveSymbols(nil)
	if len(def) != len(cryptoCoins) {
		t.Errorf("空输入应回落默认列表，实得 %+v", def)
	}
}
