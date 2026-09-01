package claims

import (
	"strings"
	"testing"
	"time"
)

var base = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func TestParseInput_Full(t *testing.T) {
	c, err := ParseInput("BTC 会去扫上方 12.4万 | 证伪 BTC<118000 | 到期 3d | 依据 清算地图15m", base)
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if c.Text != "BTC 会去扫上方 12.4万" {
		t.Errorf("判断内容 = %q", c.Text)
	}
	if c.Basis != "清算地图15m" {
		t.Errorf("依据 = %q", c.Basis)
	}
	if c.Falsify != "BTC<118000" {
		t.Errorf("证伪原文 = %q", c.Falsify)
	}
	if !c.Auto() {
		t.Fatal("BTC<118000 应当能被解析成自动条件")
	}
	if c.Cond.Symbol != "BTC" || c.Cond.Op != "<" || c.Cond.Price != 118000 {
		t.Errorf("条件解析错误: %+v", c.Cond)
	}
	if !c.DueAt.Equal(base.AddDate(0, 0, 3)) {
		t.Errorf("到期 = %v", c.DueAt)
	}
}

// 证伪条件是这套东西的全部意义，缺了必须拒收——
// 否则又退回成「偏多/偏空」那种涨了算对跌了能找补的话。
func TestParseInput_RequiresFalsifyCond(t *testing.T) {
	if _, err := ParseInput("BTC 要涨", base); err == nil {
		t.Error("没有证伪条件应当被拒")
	}
	if _, err := ParseInput("", base); err == nil {
		t.Error("空判断应当被拒")
	}
	if _, err := ParseInput("  |  证伪 BTC<1", base); err == nil {
		t.Error("判断为空应当被拒")
	}
}

// 无法自动验证的条件照样要能记，只是走人工结案。
func TestParseInput_NonAutoStillAccepted(t *testing.T) {
	c, err := ParseInput("成交量会放大 | 证伪 三天内日均量不超过上周", base)
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if c.Auto() {
		t.Error("这个条件不该被解析成价格条件")
	}
	if c.Falsify == "" {
		t.Error("证伪原文仍应保留")
	}
}

func TestParseInput_DefaultDue(t *testing.T) {
	c, _ := ParseInput("X | 证伪 BTC<1", base)
	if !c.DueAt.Equal(base.AddDate(0, 0, 7)) {
		t.Errorf("默认到期应为 7 天，实得 %v", c.DueAt)
	}
}

func TestParseInput_SegmentSeparators(t *testing.T) {
	// 冒号（中英）和空格都该认
	for _, in := range []string{
		"X | 证伪:BTC<1 | 到期:2d",
		"X | 证伪：BTC<1 | 到期：2d",
		"X | 证伪 BTC<1 | 到期 2d",
		"X | falsify BTC<1 | due 2d",
	} {
		c, err := ParseInput(in, base)
		if err != nil {
			t.Errorf("%q 解析失败: %v", in, err)
			continue
		}
		if c.Cond == nil || c.Cond.Price != 1 {
			t.Errorf("%q 条件未解析: %+v", in, c.Cond)
		}
		if !c.DueAt.Equal(base.AddDate(0, 0, 2)) {
			t.Errorf("%q 到期错误: %v", in, c.DueAt)
		}
	}
}

func TestParseCond(t *testing.T) {
	cases := []struct {
		in     string
		symbol string
		op     string
		price  float64
	}{
		{"BTC<118000", "BTC", "<", 118000},
		{"btc >= 124000", "BTC", ">=", 124000},
		{"ETH<=3000", "ETH", "<=", 3000},
		{"BTC>12.4万", "BTC", ">", 124000},
		{"BTC<118k", "BTC", "<", 118000},
		{"BTC > 11.8w", "BTC", ">", 118000},
		{"600519<1700", "600519", "<", 1700},
	}
	for _, c := range cases {
		got, err := ParseCond(c.in)
		if err != nil {
			t.Errorf("ParseCond(%q) 报错: %v", c.in, err)
			continue
		}
		if got.Symbol != c.symbol || got.Op != c.op || got.Price != c.price {
			t.Errorf("ParseCond(%q) = %+v, want %s %s %v", c.in, got, c.symbol, c.op, c.price)
		}
	}

	for _, in := range []string{"", "BTC", "涨很多", "BTC=118000", "BTC<<1"} {
		if _, err := ParseCond(in); err == nil {
			t.Errorf("ParseCond(%q) 应当失败", in)
		}
	}
}

func TestPriceCond_Met(t *testing.T) {
	lt := PriceCond{Symbol: "BTC", Op: "<", Price: 100}
	if !lt.Met(99) || lt.Met(100) || lt.Met(101) {
		t.Error("< 判断错误")
	}
	ge := PriceCond{Symbol: "BTC", Op: ">=", Price: 100}
	if !ge.Met(100) || !ge.Met(101) || ge.Met(99) {
		t.Error(">= 判断错误")
	}
	bad := PriceCond{Op: "=="}
	if bad.Met(1) {
		t.Error("未知操作符不该判定为满足")
	}
}

func TestParseDue(t *testing.T) {
	if d, _ := ParseDue("12h", base); !d.Equal(base.Add(12 * time.Hour)) {
		t.Errorf("12h → %v", d)
	}
	if d, _ := ParseDue("2w", base); !d.Equal(base.AddDate(0, 0, 14)) {
		t.Errorf("2w → %v", d)
	}
	// 绝对日期算到当天结束
	if d, _ := ParseDue("2026-09-10", base); !d.Equal(time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("绝对日期 → %v", d)
	}
	for _, in := range []string{"0d", "abc", "3", "-1d", ""} {
		if _, err := ParseDue(in, base); err == nil {
			t.Errorf("ParseDue(%q) 应当失败", in)
		}
	}
}

func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		id := NewID()
		if len(id) != 6 {
			t.Fatalf("id 长度应为 6，实得 %q", id)
		}
		// 这个 id 要用户手打进 /resolve，不能含易混字符
		if strings.ContainsAny(id, "01OIL") {
			t.Errorf("id 含易混字符: %q", id)
		}
		seen[id] = true
	}
	if len(seen) < 1900 {
		t.Errorf("2000 次只生成了 %d 个不同 id，碰撞过多", len(seen))
	}
}

func TestParseOutcome(t *testing.T) {
	for _, in := range []string{"correct", "对", "RIGHT"} {
		if o, ok := ParseOutcome(in); !ok || o != OutcomeCorrect {
			t.Errorf("ParseOutcome(%q) = %v %v", in, o, ok)
		}
	}
	for _, in := range []string{"wrong", "错"} {
		if o, ok := ParseOutcome(in); !ok || o != OutcomeWrong {
			t.Errorf("ParseOutcome(%q) = %v %v", in, o, ok)
		}
	}
	if o, ok := ParseOutcome("partial"); !ok || o != OutcomePartial {
		t.Errorf("partial → %v %v", o, ok)
	}
	if _, ok := ParseOutcome("maybe"); ok {
		t.Error("未知结论应当被拒")
	}
}
