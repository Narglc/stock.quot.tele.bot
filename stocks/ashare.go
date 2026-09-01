package stocks

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A 股行情数据源用东方财富 push2 而不是雪球：
// 雪球 quotec 需要先拿 xq_a_token cookie，且对机房 IP 直接 403；
// 腾讯/新浪返回的是 GBK 编码的分隔字符串，还要额外转码和按位解析。
// 东财这个接口免鉴权、返回 UTF-8 JSON、一次能查多只，字段也齐。

// StockQuote 是一只 A 股的行情快照。
type StockQuote struct {
	Code         string
	Name         string
	Price        float64
	ChangePct    float64 // 涨跌幅（%）
	Change       float64 // 涨跌额
	PrevClose    float64
	Open         float64
	High         float64
	Low          float64
	Volume       float64 // 成交量（手）
	Amount       float64 // 成交额（元）
	TurnoverRate float64 // 换手率（%）
}

// Traded 判断这只票当日是否有成交。
// 全市场都没有成交通常意味着当天不是交易日（节假日），据此跳过播报。
func (q StockQuote) Traded() bool { return q.Volume > 0 }

// flexFloat 兼容东方财富在停牌/无数据时把数字字段返回成 "-" 的情况。
// 单个字段解析不了就当 0，不因此毁掉整条行情。
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "-" || s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		*f = 0
		return nil
	}
	*f = flexFloat(v)
	return nil
}

// emQuote 对应东方财富 push2 ulist 接口的单条返回。
type emQuote struct {
	Code      string    `json:"f12"`
	Name      string    `json:"f14"`
	Price     flexFloat `json:"f2"`
	ChangePct flexFloat `json:"f3"`
	Change    flexFloat `json:"f4"`
	Volume    flexFloat `json:"f5"`
	Amount    flexFloat `json:"f6"`
	Turnover  flexFloat `json:"f8"`
	High      flexFloat `json:"f15"`
	Low       flexFloat `json:"f16"`
	Open      flexFloat `json:"f17"`
	PrevClose flexFloat `json:"f18"`
}

type emResponse struct {
	Data struct {
		Diff []emQuote `json:"diff"`
	} `json:"data"`
}

// MaxWatchlistSize 是单个 chat 的自选股上限。
// 上限存在的理由：一条 Telegram 消息有长度限制，而且自选越多播报越没人看。
const MaxWatchlistSize = 20

// NormalizeStockCode 校验并归一化 A 股代码：必须是 6 位数字。
// 和币种符号一样，校验放在最前面——避免把用户输入直接拼进第三方接口的查询串。
func NormalizeStockCode(s string) (string, error) {
	code := strings.TrimSpace(s)
	// 容忍用户带市场前缀输入，如 sh600519 / SZ000001
	upper := strings.ToUpper(code)
	if len(upper) == 8 && (strings.HasPrefix(upper, "SH") || strings.HasPrefix(upper, "SZ") || strings.HasPrefix(upper, "BJ")) {
		code = upper[2:]
	}
	if len(code) != 6 {
		return "", fmt.Errorf("股票代码要 6 位数字: %q", s)
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("股票代码要 6 位数字: %q", s)
		}
	}
	return code, nil
}

// secid 把 6 位代码转成东财的 secid（市场前缀 + 代码）。
// 6/5/9 开头是上交所（含 60 主板、688 科创、5xx 沪市 ETF），其余归深/北。
func secid(code string) string {
	switch code[0] {
	case '6', '5', '9':
		return "1." + code
	default:
		return "0." + code
	}
}

// stockCache 缓存 A 股行情。盘中 15s 足够新鲜，也挡住了重复查询。
var stockCache = newTTLCache[[]StockQuote](15 * time.Second)

// GetStockQuotes 批量查询 A 股行情，按传入顺序返回（查不到的会被跳过）。
func GetStockQuotes(codes []string) ([]StockQuote, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, len(codes))
	for _, c := range codes {
		ids = append(ids, secid(c))
	}
	cacheKey := strings.Join(ids, ",")

	if cached, ok := stockCache.get(cacheKey); ok {
		return cached, nil
	}

	url := "https://push2.eastmoney.com/api/qt/ulist.np/get?fltt=2&secids=" + cacheKey +
		"&fields=f2,f3,f4,f5,f6,f8,f12,f14,f15,f16,f17,f18"

	var resp emResponse
	if err := fetchJSON(marketClient, "A股行情", url, &resp); err != nil {
		return nil, err
	}

	// 按代码建索引，保持调用方传入的顺序。
	byCode := make(map[string]emQuote, len(resp.Data.Diff))
	for _, q := range resp.Data.Diff {
		byCode[q.Code] = q
	}

	out := make([]StockQuote, 0, len(codes))
	for _, c := range codes {
		q, ok := byCode[c]
		if !ok {
			continue
		}
		out = append(out, StockQuote{
			Code:         q.Code,
			Name:         q.Name,
			Price:        float64(q.Price),
			ChangePct:    float64(q.ChangePct),
			Change:       float64(q.Change),
			PrevClose:    float64(q.PrevClose),
			Open:         float64(q.Open),
			High:         float64(q.High),
			Low:          float64(q.Low),
			Volume:       float64(q.Volume),
			Amount:       float64(q.Amount),
			TurnoverRate: float64(q.Turnover),
		})
	}

	stockCache.set(cacheKey, out)
	return out, nil
}

// AShareSession 描述某个时刻相对 A 股交易时段的位置。
type AShareSession int

const (
	SessionBeforeOpen AShareSession = iota // 开盘前
	SessionIntraday                        // 盘中（含午休）
	SessionClosed                          // 收盘后
)

// aShareSession 判断给定时刻处于哪个时段。t 须是东八区时间。
func aShareSession(t time.Time) AShareSession {
	hm := t.Hour()*60 + t.Minute()
	switch {
	case hm < 9*60+30:
		return SessionBeforeOpen
	case hm >= 15*60:
		return SessionClosed
	default:
		return SessionIntraday
	}
}

// FormatStockMessage 把自选股行情排成一条消息。
//
// 时间口径是强制的，不是可选的装饰：
// 收盘后标「收盘」，盘中标「截至 HH:MM · 盘中」并附一句提醒。
// 盘中数字随时会变，不标时间的话，事后回看根本无法判断当时看到的是什么。
func FormatStockMessage(quotes []StockQuote, asOf time.Time) string {
	session := aShareSession(asOf)

	var b strings.Builder
	switch session {
	case SessionClosed:
		b.WriteString(fmt.Sprintf("📈 A股收盘（%s）\n", asOf.Format("2006-01-02")))
	case SessionBeforeOpen:
		b.WriteString(fmt.Sprintf("📈 A股 · 未开盘（%s %s）\n", asOf.Format("2006-01-02"), asOf.Format("15:04")))
	default:
		b.WriteString(fmt.Sprintf("📈 A股 · 截至 %s（盘中）\n", asOf.Format("15:04")))
	}

	if len(quotes) == 0 {
		b.WriteString("\n自选股是空的，用 /watch add 600519 添加")
		return b.String()
	}

	for _, q := range quotes {
		b.WriteString(fmt.Sprintf("\n%s %s\n", q.Name, q.Code))
		if !q.Traded() {
			b.WriteString("   停牌 / 无成交\n")
			continue
		}
		b.WriteString(fmt.Sprintf("   ¥%s  %s %+.2f%%（%+.2f）\n",
			formatPrice(q.Price), arrow(q.ChangePct), q.ChangePct, q.Change))
		b.WriteString(fmt.Sprintf("   高低 ¥%s~¥%s · 额 ¥%s",
			formatPrice(q.Low), formatPrice(q.High), formatCompact(q.Amount)))
		if q.TurnoverRate > 0 {
			b.WriteString(fmt.Sprintf(" · 换手 %.2f%%", q.TurnoverRate))
		}
		b.WriteString("\n")
	}

	if session == SessionIntraday {
		b.WriteString("\n⚠️ 盘中数据，随时会变；结论请等收盘后再下。")
	}
	return strings.TrimRight(b.String(), "\n")
}

// AnyTraded 判断这批行情里是否有任何成交。
// 全部无成交 = 当天多半不是交易日（cron 的 1-5 排得掉周末，排不掉节假日）。
func AnyTraded(quotes []StockQuote) bool {
	for _, q := range quotes {
		if q.Traded() {
			return true
		}
	}
	return false
}
