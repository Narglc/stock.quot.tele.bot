package stocks

import (
	"fmt"
	"math"
	"strings"
	"sync"
)

// 事件阈值。调这几个数就能改灵敏度——调之前先想清楚：
// 阈值太低会退化成每小时都响（等于没改），太高则等你看到时已经发生完了。
const (
	// fundingExtreme 资金费率绝对值阈值。0.05%/8h 年化约 55%，已属显著。
	fundingExtreme = 0.0005
	// oiJumpPct 持仓量单次观测变化阈值（%）。播报每小时一次，即小时环比。
	oiJumpPct = 5.0
)

// AlertKind 是事件类型，用于去重与展示。
type AlertKind string

const (
	AlertFunding  AlertKind = "funding"  // 资金费率异动
	AlertOI       AlertKind = "oi"       // 持仓量骤变
	AlertBreakout AlertKind = "breakout" // 价格突破 24h 高/低
	AlertFNG      AlertKind = "fng"      // 恐惧贪婪跨区间
)

// Alert 是一次值得打断用户的行情事件。
//
// 刻意只描述「发生了什么」，不给「所以该做什么」——
// 方向性结论无法证伪，涨了算对跌了能找补，没有信息价值。
type Alert struct {
	Symbol string
	Kind   AlertKind
	Text   string // 已排版好的一行文案
}

// alertState 记录上一次观测到的状态，用于识别「跨越」而不是「持续处于某状态」。
//
// 这是激进版播报的核心：如果只判断「当前费率 > 阈值」，价格在高位横盘时
// 每小时都会响一次，跟原来的定点播报没有区别。只有状态发生**变化**才算事件。
type alertState struct {
	fundingSign    int  // 上次费率符号：-1 负 / 0 未知或零 / 1 正
	fundingExtreme bool // 上次是否处于极端费率
	atHigh         bool // 上次是否处于 24h 新高
	atLow          bool // 上次是否处于 24h 新低
	fngBand        int  // 上次恐惧贪婪所处档位，-1 表示未知
}

var (
	alertMu     sync.Mutex
	alertStates = map[string]*alertState{} // key: 币种符号
	lastFNGBand = -1                       // 恐惧贪婪是全市场指标，不分币种
)

// ResetAlertState 清空事件基线（仅供测试使用）。
func ResetAlertState() {
	alertMu.Lock()
	defer alertMu.Unlock()
	alertStates = map[string]*alertState{}
	lastFNGBand = -1
}

// DetectAlerts 比对本次观测与上次状态，返回发生了跨越的事件。
//
// 进程重启后首次调用只会记录基线、不产生事件——否则重启瞬间会把当前所有
// 「处于极端状态」的币全报一遍，而它们可能已经在那个状态待了一整天。
func DetectAlerts(quotes []CryptoQuote, fng *FearGreed) []Alert {
	alertMu.Lock()
	defer alertMu.Unlock()

	var alerts []Alert

	for _, q := range quotes {
		st, known := alertStates[q.Name]
		if !known {
			st = &alertState{fngBand: -1}
			alertStates[q.Name] = st
		}

		alerts = append(alerts, detectFunding(q, st, known)...)
		alerts = append(alerts, detectOI(q, known)...)
		alerts = append(alerts, detectBreakout(q, st, known)...)
	}

	if fng != nil {
		band := fngBand(fng.Value)
		if lastFNGBand >= 0 && band != lastFNGBand {
			alerts = append(alerts, Alert{
				Kind: AlertFNG,
				Text: fmt.Sprintf("%s 恐惧贪婪 %d · %s（从「%s」跨入）",
					fng.Emoji(), fng.Value, fng.LabelCN(), fngBandName(lastFNGBand)),
			})
		}
		lastFNGBand = band
	}

	return alerts
}

// detectFunding 检测资金费率翻转与进入/离开极端区。
func detectFunding(q CryptoQuote, st *alertState, known bool) []Alert {
	if !q.FundingKnown {
		return nil
	}

	sign := 0
	switch {
	case q.FundingRate > 0:
		sign = 1
	case q.FundingRate < 0:
		sign = -1
	}
	extreme := math.Abs(q.FundingRate) >= fundingExtreme

	prevSign, prevExtreme := st.fundingSign, st.fundingExtreme
	st.fundingSign, st.fundingExtreme = sign, extreme
	if !known {
		return nil // 首次观测只记基线
	}

	var out []Alert
	// 翻转：多空付费方调转，是杠杆情绪转向最直接的读数。
	if prevSign != 0 && sign != 0 && sign != prevSign {
		payer, prevPayer := "多头付费", "空头付费"
		if sign < 0 {
			payer, prevPayer = "空头付费", "多头付费"
		}
		out = append(out, Alert{
			Symbol: q.Name, Kind: AlertFunding,
			Text: fmt.Sprintf("💱 %s 资金费率翻转：%s → %s（现 %+.4f%%）",
				q.Name, prevPayer, payer, q.FundingRate*100),
		})
	}
	// 进入极端区（离开不报，避免一次异动响两次）。
	if extreme && !prevExtreme {
		side := "多头"
		if q.FundingRate < 0 {
			side = "空头"
		}
		out = append(out, Alert{
			Symbol: q.Name, Kind: AlertFunding,
			Text: fmt.Sprintf("💱 %s 资金费率进入极端区 %+.4f%%（%s付费，超过 %.2f%% 阈值）",
				q.Name, q.FundingRate*100, side, fundingExtreme*100),
		})
	}
	return out
}

// detectOI 检测持仓量骤变。
// 这里不需要 alertState：OIChangePct 本身就是「相比上次观测」的增量，天然只报变化。
func detectOI(q CryptoQuote, known bool) []Alert {
	if !known || !q.OIChangeKnown || math.Abs(q.OIChangePct) < oiJumpPct {
		return nil
	}
	text := fmt.Sprintf("📊 %s 持仓量 %+.1f%%", q.Name, q.OIChangePct)
	if span := formatSpan(q.OISpan); span != "" {
		text += " / " + span
	}
	text += fmt.Sprintf("（现 $%s）", formatCompact(q.OpenInterestUSD))
	// 价格方向 + 持仓变化的组合解读，仍然只描述状态、不给方向建议。
	if s := oiInsight(q.Change1h, q.OIChangePct); s != "" {
		text += "\n   " + s
	}
	return []Alert{{Symbol: q.Name, Kind: AlertOI, Text: text}}
}

// detectBreakout 检测价格突破 24h 高/低。
// 只在「从非突破变成突破」时报一次，否则单边行情里每小时都会响。
func detectBreakout(q CryptoQuote, st *alertState, known bool) []Alert {
	if q.High24h <= 0 || q.Low24h <= 0 || q.Price <= 0 {
		return nil
	}

	atHigh := q.Price >= q.High24h
	atLow := q.Price <= q.Low24h
	prevHigh, prevLow := st.atHigh, st.atLow
	st.atHigh, st.atLow = atHigh, atLow
	if !known {
		return nil
	}

	var out []Alert
	if atHigh && !prevHigh {
		out = append(out, Alert{
			Symbol: q.Name, Kind: AlertBreakout,
			Text: fmt.Sprintf("🔼 %s 创 24h 新高 $%s（24h %+.2f%%）",
				q.Name, formatPrice(q.Price), q.ChangePercent),
		})
	}
	if atLow && !prevLow {
		out = append(out, Alert{
			Symbol: q.Name, Kind: AlertBreakout,
			Text: fmt.Sprintf("🔽 %s 创 24h 新低 $%s（24h %+.2f%%）",
				q.Name, formatPrice(q.Price), q.ChangePercent),
		})
	}
	return out
}

// fngBand 把恐惧贪婪指数分档，档位边界与 FearGreed.Emoji 保持一致。
func fngBand(v int) int {
	switch {
	case v < 25:
		return 0
	case v < 50:
		return 1
	case v < 55:
		return 2
	case v < 75:
		return 3
	default:
		return 4
	}
}

func fngBandName(band int) string {
	switch band {
	case 0:
		return "极度恐惧"
	case 1:
		return "恐惧"
	case 2:
		return "中性"
	case 3:
		return "贪婪"
	case 4:
		return "极度贪婪"
	}
	return "未知"
}

// FormatAlerts 把一批事件排成一条消息。无事件返回空串。
func FormatAlerts(alerts []Alert) string {
	if len(alerts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("⚡ 行情异动\n")
	for _, a := range alerts {
		b.WriteString("\n" + a.Text + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
