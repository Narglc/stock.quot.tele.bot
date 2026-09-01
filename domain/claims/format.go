package claims

import (
	"fmt"
	"strings"
	"time"
)

// FormatOne 把一条 claim 排成多行文本（详情视图）。
func FormatOne(c *Claim, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s [%s] %s\n", statusIcon(c), c.ID, c.Text)
	if c.Basis != "" {
		fmt.Fprintf(&b, "   依据：%s\n", c.Basis)
	}
	fmt.Fprintf(&b, "   证伪：%s", c.Falsify)
	if c.Auto() {
		b.WriteString("（可自动检查）")
	} else {
		b.WriteString("（需手动结案）")
	}
	b.WriteString("\n")

	switch c.Status {
	case StatusPending:
		fmt.Fprintf(&b, "   到期：%s（%s）\n", c.DueAt.Format("01-02 15:04"), humanUntil(c.DueAt, now))
	case StatusFalsified:
		when := ""
		if c.TriggeredAt != nil {
			when = c.TriggeredAt.Format("01-02 15:04")
		}
		fmt.Fprintf(&b, "   ❌ 已被证伪 %s\n", when)
	case StatusExpired:
		fmt.Fprintf(&b, "   ⏰ 已到期未证伪，等你结案：/resolve %s correct|wrong|partial\n", c.ID)
	case StatusResolved:
		fmt.Fprintf(&b, "   ✅ 结论：%s", outcomeCN(c.Outcome))
		if c.Note != "" {
			fmt.Fprintf(&b, "（%s）", c.Note)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// FormatList 把一批 claim 排成列表。
func FormatList(cs []*Claim, now time.Time, title string) string {
	if len(cs) == 0 {
		return title + "\n\n（空）\n\n用 /claim 记一条：\n/claim BTC 会去扫上方大簇 | 证伪 BTC<118000 | 到期 3d"
	}
	var b strings.Builder
	b.WriteString(title + "\n")
	for _, c := range cs {
		b.WriteString("\n" + FormatOne(c, now) + "\n")
	}

	s := Summarize(cs)
	if s.Resolved() > 0 {
		b.WriteString(fmt.Sprintf("\n—— 共 %d 条，已有结论 %d 条：对 %d · 错 %d（其中自动证伪 %d）· 半对 %d",
			s.Total, s.Resolved(), s.Correct, s.Wrong+s.Falsified, s.Falsified, s.Partial))
	}
	return strings.TrimRight(b.String(), "\n")
}

// FormatEvent 把一次状态变化排成通知文案。
func FormatEvent(e Event) string {
	c := e.Claim
	switch e.Kind {
	case EventFalsified:
		return fmt.Sprintf("❌ 判断被证伪 [%s]\n\n「%s」\n\n证伪条件 %s 已触发（现价 %s）。\n记录时间：%s",
			c.ID, c.Text, c.Falsify, trimFloat(e.Price), c.CreatedAt.Format("01-02 15:04"))
	case EventExpired:
		return fmt.Sprintf("⏰ 判断到期 [%s]\n\n「%s」\n\n到期仍未触发证伪条件（%s）。结果如何？\n/resolve %s correct|wrong|partial [说明]",
			c.ID, c.Text, c.Falsify, c.ID)
	}
	return ""
}

func statusIcon(c *Claim) string {
	switch c.Status {
	case StatusFalsified:
		return "❌"
	case StatusExpired:
		return "⏰"
	case StatusResolved:
		switch c.Outcome {
		case OutcomeCorrect:
			return "✅"
		case OutcomeWrong:
			return "❌"
		default:
			return "🟡"
		}
	}
	return "🔵"
}

func outcomeCN(o Outcome) string {
	switch o {
	case OutcomeCorrect:
		return "对"
	case OutcomeWrong:
		return "错"
	case OutcomePartial:
		return "半对"
	}
	return string(o)
}

// humanUntil 把「距到期还有多久」说成人话。
func humanUntil(due, now time.Time) string {
	d := due.Sub(now)
	if d <= 0 {
		return "已过期"
	}
	if d < time.Hour {
		return fmt.Sprintf("还剩 %d 分钟", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("还剩 %.1f 小时", d.Hours())
	}
	return fmt.Sprintf("还剩 %.1f 天", d.Hours()/24)
}

// trimFloat 去掉价格尾部多余的 0。
func trimFloat(v float64) string {
	s := fmt.Sprintf("%.4f", v)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}
