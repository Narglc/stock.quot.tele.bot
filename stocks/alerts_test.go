package stocks

import (
	"strings"
	"testing"
	"time"
)

// 首次观测只建立基线，不产生事件。
// 否则进程一重启就会把所有「当前处于极端状态」的币全报一遍，
// 而它们可能已经在那个状态待了一整天。
func TestDetectAlerts_FirstObservationIsSilent(t *testing.T) {
	ResetAlertState()
	q := CryptoQuote{
		Name: "BTC", Price: 70000,
		High24h: 70000, Low24h: 60000, // 正处于 24h 新高
		FundingRate: 0.001, FundingKnown: true, // 且费率极端
		OIChangePct: 20, OIChangeKnown: true, // 且持仓骤变
	}
	if got := DetectAlerts([]CryptoQuote{q}, &FearGreed{Value: 10}); len(got) != 0 {
		t.Errorf("首次观测应静默，实得 %d 条: %+v", len(got), got)
	}
}

// 核心：持续处于某状态不该反复报警，只有跨越才报。
func TestDetectAlerts_SustainedStateDoesNotRepeat(t *testing.T) {
	ResetAlertState()
	q := CryptoQuote{
		Name: "BTC", Price: 70000, High24h: 70000, Low24h: 60000,
		FundingRate: 0.001, FundingKnown: true,
	}

	DetectAlerts([]CryptoQuote{q}, nil) // 建立基线
	first := DetectAlerts([]CryptoQuote{q}, nil)
	if len(first) != 0 {
		t.Errorf("状态未变化不该报警，实得: %+v", first)
	}

	// 再来五轮，依然应该静默
	for i := 0; i < 5; i++ {
		if got := DetectAlerts([]CryptoQuote{q}, nil); len(got) != 0 {
			t.Fatalf("第 %d 轮仍在报警，说明退化成了定点播报: %+v", i+1, got)
		}
	}
}

func TestDetectAlerts_FundingFlip(t *testing.T) {
	ResetAlertState()
	pos := CryptoQuote{Name: "BTC", Price: 100, High24h: 200, Low24h: 50, FundingRate: 0.0001, FundingKnown: true}
	DetectAlerts([]CryptoQuote{pos}, nil) // 基线：正费率

	neg := pos
	neg.FundingRate = -0.0001
	got := DetectAlerts([]CryptoQuote{neg}, nil)

	if len(got) != 1 || got[0].Kind != AlertFunding {
		t.Fatalf("费率翻转应产生一条事件，实得 %+v", got)
	}
	if !strings.Contains(got[0].Text, "翻转") || !strings.Contains(got[0].Text, "空头付费") {
		t.Errorf("文案应说明翻转方向，实得: %s", got[0].Text)
	}
}

func TestDetectAlerts_FundingEntersExtreme(t *testing.T) {
	ResetAlertState()
	mild := CryptoQuote{Name: "BTC", Price: 100, High24h: 200, Low24h: 50, FundingRate: 0.0001, FundingKnown: true}
	DetectAlerts([]CryptoQuote{mild}, nil)

	extreme := mild
	extreme.FundingRate = 0.0008 // 超过 0.05% 阈值
	got := DetectAlerts([]CryptoQuote{extreme}, nil)
	if len(got) != 1 || !strings.Contains(got[0].Text, "极端区") {
		t.Fatalf("进入极端区应报警，实得 %+v", got)
	}

	// 留在极端区不该再报
	if again := DetectAlerts([]CryptoQuote{extreme}, nil); len(again) != 0 {
		t.Errorf("持续处于极端区不该重复报警，实得 %+v", again)
	}
}

func TestDetectAlerts_OIJump(t *testing.T) {
	ResetAlertState()
	base := CryptoQuote{Name: "BTC", Price: 100, High24h: 200, Low24h: 50}
	DetectAlerts([]CryptoQuote{base}, nil)

	jump := base
	jump.OIChangePct = 7.5
	jump.OIChangeKnown = true
	jump.OpenInterestUSD = 12e9
	jump.OISpan = 58 * time.Minute

	got := DetectAlerts([]CryptoQuote{jump}, nil)
	if len(got) != 1 || got[0].Kind != AlertOI {
		t.Fatalf("持仓骤变应报警，实得 %+v", got)
	}
	// 变化幅度必须带时间跨度，否则无法证伪
	if !strings.Contains(got[0].Text, "58m") {
		t.Errorf("持仓变化应带时间跨度，实得: %s", got[0].Text)
	}

	// 小幅变化不该报
	small := base
	small.OIChangePct = 2.0
	small.OIChangeKnown = true
	if got := DetectAlerts([]CryptoQuote{small}, nil); len(got) != 0 {
		t.Errorf("低于阈值不该报警，实得 %+v", got)
	}
}

func TestDetectAlerts_Breakout(t *testing.T) {
	ResetAlertState()
	mid := CryptoQuote{Name: "BTC", Price: 65000, High24h: 70000, Low24h: 60000}
	DetectAlerts([]CryptoQuote{mid}, nil)

	high := mid
	high.Price = 70000 // 触及 24h 高
	high.High24h = 70000
	got := DetectAlerts([]CryptoQuote{high}, nil)
	if len(got) != 1 || !strings.Contains(got[0].Text, "新高") {
		t.Fatalf("突破 24h 高应报警，实得 %+v", got)
	}

	// 回落再突破，应当再报一次
	DetectAlerts([]CryptoQuote{mid}, nil)
	if again := DetectAlerts([]CryptoQuote{high}, nil); len(again) != 1 {
		t.Errorf("回落后再次突破应重新报警，实得 %+v", again)
	}
}

func TestDetectAlerts_FNGBandCross(t *testing.T) {
	ResetAlertState()
	q := CryptoQuote{Name: "BTC", Price: 100, High24h: 200, Low24h: 50}

	DetectAlerts([]CryptoQuote{q}, &FearGreed{Value: 60, Classification: "Greed"})
	// 同档内波动不报
	if got := DetectAlerts([]CryptoQuote{q}, &FearGreed{Value: 70, Classification: "Greed"}); len(got) != 0 {
		t.Errorf("同档内波动不该报警，实得 %+v", got)
	}
	// 跨档才报
	got := DetectAlerts([]CryptoQuote{q}, &FearGreed{Value: 80, Classification: "Extreme Greed"})
	if len(got) != 1 || got[0].Kind != AlertFNG {
		t.Fatalf("跨档应报警，实得 %+v", got)
	}
	if !strings.Contains(got[0].Text, "贪婪") {
		t.Errorf("文案应含来源档位，实得: %s", got[0].Text)
	}
}

func TestFngBand(t *testing.T) {
	cases := []struct{ v, band int }{{0, 0}, {24, 0}, {25, 1}, {49, 1}, {50, 2}, {54, 2}, {55, 3}, {74, 3}, {75, 4}, {100, 4}}
	for _, c := range cases {
		if got := fngBand(c.v); got != c.band {
			t.Errorf("fngBand(%d) = %d, want %d", c.v, got, c.band)
		}
	}
}

func TestFormatAlerts_Empty(t *testing.T) {
	if got := FormatAlerts(nil); got != "" {
		t.Errorf("无事件应返回空串，实得 %q", got)
	}
}

func TestFormatAlerts_Multiple(t *testing.T) {
	out := FormatAlerts([]Alert{
		{Symbol: "BTC", Kind: AlertOI, Text: "📊 BTC 持仓量 +7.5%"},
		{Symbol: "ETH", Kind: AlertBreakout, Text: "🔼 ETH 创 24h 新高"},
	})
	if !strings.Contains(out, "BTC") || !strings.Contains(out, "ETH") {
		t.Errorf("应包含全部事件，实得:\n%s", out)
	}
	if strings.HasSuffix(out, "\n") {
		t.Error("尾部换行应被裁掉")
	}
}
