package stocks

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNormalizeStockCode(t *testing.T) {
	ok := []struct{ in, want string }{
		{"600519", "600519"},
		{" 000001 ", "000001"},
		{"sh600519", "600519"},
		{"SZ000001", "000001"},
		{"bj430047", "430047"},
	}
	for _, c := range ok {
		got, err := NormalizeStockCode(c.in)
		if err != nil || got != c.want {
			t.Errorf("NormalizeStockCode(%q) = %q, %v; want %q, nil", c.in, got, err, c.want)
		}
	}

	bad := []string{"", "60051", "6005199", "60051a", "600519&x=1", "../etc", "茅台"}
	for _, in := range bad {
		if got, err := NormalizeStockCode(in); err == nil {
			t.Errorf("NormalizeStockCode(%q) 应被拒，实得 %q", in, got)
		}
	}
}

func TestSecid(t *testing.T) {
	cases := []struct{ code, want string }{
		{"600519", "1.600519"}, // 沪主板
		{"688981", "1.688981"}, // 科创板
		{"510300", "1.510300"}, // 沪 ETF
		{"000001", "0.000001"}, // 深主板
		{"300750", "0.300750"}, // 创业板
		{"159915", "0.159915"}, // 深 ETF
		{"430047", "0.430047"}, // 北交所
	}
	for _, c := range cases {
		if got := secid(c.code); got != c.want {
			t.Errorf("secid(%q) = %q, want %q", c.code, got, c.want)
		}
	}
}

func TestAShareSession(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	at := func(h, m int) time.Time { return time.Date(2026, 9, 1, h, m, 0, 0, loc) }

	cases := []struct {
		t    time.Time
		want AShareSession
	}{
		{at(8, 0), SessionBeforeOpen},
		{at(9, 29), SessionBeforeOpen},
		{at(9, 30), SessionIntraday},
		{at(11, 40), SessionIntraday},
		{at(13, 0), SessionIntraday}, // 午休后
		{at(14, 59), SessionIntraday},
		{at(15, 0), SessionClosed},
		{at(20, 0), SessionClosed},
	}
	for _, c := range cases {
		if got := aShareSession(c.t); got != c.want {
			t.Errorf("aShareSession(%s) = %d, want %d", c.t.Format("15:04"), got, c.want)
		}
	}
}

// 时间口径是硬要求：盘中数据必须标出「截至 HH:MM」并给出提醒，
// 否则事后回看根本判断不出当时看到的是什么。
func TestFormatStockMessage_IntradayMustCarryTimestamp(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	q := StockQuote{Code: "600519", Name: "贵州茅台", Price: 1635, ChangePct: 1.24, Change: 20, Volume: 100, Amount: 1e9}

	intraday := FormatStockMessage([]StockQuote{q}, time.Date(2026, 9, 1, 14, 32, 0, 0, loc))
	if !strings.Contains(intraday, "截至 14:32") {
		t.Errorf("盘中必须标出时间，实得:\n%s", intraday)
	}
	if !strings.Contains(intraday, "盘中") {
		t.Errorf("盘中必须标明口径，实得:\n%s", intraday)
	}
	if !strings.Contains(intraday, "结论请等收盘后再下") {
		t.Errorf("盘中必须带提醒，实得:\n%s", intraday)
	}

	closed := FormatStockMessage([]StockQuote{q}, time.Date(2026, 9, 1, 15, 5, 0, 0, loc))
	if !strings.Contains(closed, "收盘") {
		t.Errorf("收盘后应标『收盘』，实得:\n%s", closed)
	}
	if strings.Contains(closed, "盘中数据") {
		t.Errorf("收盘后不该再有盘中警示，实得:\n%s", closed)
	}
}

func TestFormatStockMessage_Suspended(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	q := StockQuote{Code: "600519", Name: "贵州茅台", Volume: 0}
	out := FormatStockMessage([]StockQuote{q}, time.Date(2026, 9, 1, 15, 5, 0, 0, loc))
	if !strings.Contains(out, "停牌") {
		t.Errorf("无成交应标停牌，实得:\n%s", out)
	}
}

func TestFormatStockMessage_Empty(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	out := FormatStockMessage(nil, time.Date(2026, 9, 1, 15, 5, 0, 0, loc))
	if !strings.Contains(out, "/watch add") {
		t.Errorf("空自选应给出引导，实得:\n%s", out)
	}
}

// 东财在停牌时会把数字字段返回成 "-"，不能让它毁掉整条行情。
func TestFlexFloat(t *testing.T) {
	var v struct {
		A flexFloat `json:"a"`
		B flexFloat `json:"b"`
		C flexFloat `json:"c"`
		D flexFloat `json:"d"`
	}
	if err := json.Unmarshal([]byte(`{"a":12.34,"b":"-","c":null,"d":"56.7"}`), &v); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if float64(v.A) != 12.34 {
		t.Errorf("数字应正常解析，实得 %v", v.A)
	}
	if float64(v.B) != 0 || float64(v.C) != 0 {
		t.Errorf(`"-" 和 null 应当成 0，实得 %v %v`, v.B, v.C)
	}
	if float64(v.D) != 56.7 {
		t.Errorf("字符串数字应能解析，实得 %v", v.D)
	}
}

func TestAnyTraded(t *testing.T) {
	if AnyTraded([]StockQuote{{Volume: 0}, {Volume: 0}}) {
		t.Error("全无成交应返回 false（判定非交易日）")
	}
	if !AnyTraded([]StockQuote{{Volume: 0}, {Volume: 100}}) {
		t.Error("有成交应返回 true")
	}
	if AnyTraded(nil) {
		t.Error("空列表应返回 false")
	}
}
