package stocks

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
)

// coin 描述一个币种：CoinGecko id 与展示名。
type coin struct {
	ID   string
	Name string
}

// cryptoCoins 是默认关注的币种（整点播报 / 不带参数的 /price）。顺序固定，便于稳定排版。
var cryptoCoins = []coin{
	{"bitcoin", "BTC"},
	{"ethereum", "ETH"},
}

// symbolToCoin 把用户输入的符号（大小写不敏感）映射到 CoinGecko id。
// 只收录主流币，未命中的符号会在 /price 里提示「不认识」。
var symbolToCoin = map[string]coin{
	"btc":   {"bitcoin", "BTC"},
	"eth":   {"ethereum", "ETH"},
	"sol":   {"solana", "SOL"},
	"bnb":   {"binancecoin", "BNB"},
	"xrp":   {"ripple", "XRP"},
	"doge":  {"dogecoin", "DOGE"},
	"ada":   {"cardano", "ADA"},
	"ton":   {"the-open-network", "TON"},
	"trx":   {"tron", "TRX"},
	"avax":  {"avalanche-2", "AVAX"},
	"dot":   {"polkadot", "DOT"},
	"matic": {"matic-network", "MATIC"},
	"link":  {"chainlink", "LINK"},
	"ltc":   {"litecoin", "LTC"},
	"bch":   {"bitcoin-cash", "BCH"},
	"shib":  {"shiba-inu", "SHIB"},
	"uni":   {"uniswap", "UNI"},
	"atom":  {"cosmos", "ATOM"},
	"near":  {"near", "NEAR"},
	"apt":   {"aptos", "APT"},
	"arb":   {"arbitrum", "ARB"},
	"op":    {"optimism", "OP"},
	"sui":   {"sui", "SUI"},
	"pepe":  {"pepe", "PEPE"},
}

// CryptoQuote 单个币种的行情快照（用于辅助决策的量价/位置/动量维度）。
type CryptoQuote struct {
	Name          string
	Price         float64
	ChangePercent float64 // 24h 涨跌幅（保留原字段名，兼容旧调用）
	Change1h      float64 // 1h 涨跌幅
	Change7d      float64 // 7d 涨跌幅
	Change30d     float64 // 30d 涨跌幅
	Volume24h     float64 // 24h 成交额（USD）
	ATHChangePct  float64 // 距历史最高点涨跌幅（通常为负，越接近 0 越接近历史高点）
	MarketCapRank int     // 市值排名
}

// coingeckoMarket 对应 /coins/markets 接口的单条返回（只取用得到的字段）。
// 百分比字段可能为 null，反序列化后即为 0，可接受。
type coingeckoMarket struct {
	ID               string  `json:"id"`
	CurrentPrice     float64 `json:"current_price"`
	MarketCapRank    int     `json:"market_cap_rank"`
	TotalVolume      float64 `json:"total_volume"`
	ATHChangePct     float64 `json:"ath_change_percentage"`
	Change1hPct      float64 `json:"price_change_percentage_1h_in_currency"`
	Change24hPct     float64 `json:"price_change_percentage_24h_in_currency"`
	Change7dPct      float64 `json:"price_change_percentage_7d_in_currency"`
	Change30dPct     float64 `json:"price_change_percentage_30d_in_currency"`
	Change24hFallbck float64 `json:"price_change_percentage_24h"` // 无 in_currency 时兜底
}

// GetCryptoQuotes 拉取默认关注币种（BTC/ETH）的行情快照。
func GetCryptoQuotes() ([]CryptoQuote, error) {
	return fetchQuotes(cryptoCoins)
}

// ResolveSymbols 把用户给的符号列表解析成已知币种，返回命中的币种与未识别的符号。
// symbols 为空时回落到默认关注列表。
func ResolveSymbols(symbols []string) (coins []coin, unknown []string) {
	if len(symbols) == 0 {
		return cryptoCoins, nil
	}
	seen := map[string]bool{}
	for _, s := range symbols {
		key := strings.ToLower(strings.TrimSpace(s))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if c, ok := symbolToCoin[key]; ok {
			coins = append(coins, c)
		} else {
			unknown = append(unknown, s)
		}
	}
	return coins, unknown
}

// GetCryptoQuotesBySymbols 按用户给的符号查询行情；符号为空则查默认币种。
// 返回命中的行情与未识别的符号（供上层提示）。
func GetCryptoQuotesBySymbols(symbols []string) ([]CryptoQuote, []string, error) {
	coins, unknown := ResolveSymbols(symbols)
	if len(coins) == 0 {
		return nil, unknown, nil
	}
	quotes, err := fetchQuotes(coins)
	return quotes, unknown, err
}

// fetchQuotes 用 CoinGecko /coins/markets 一次拉取多个币种的量价/动量/位置数据。
// 无需鉴权，且不受 Binance 的地区限制影响。
func fetchQuotes(coins []coin) ([]CryptoQuote, error) {
	ids := make([]string, 0, len(coins))
	for _, c := range coins {
		ids = append(ids, c.ID)
	}
	url := "https://api.coingecko.com/api/v3/coins/markets" +
		"?vs_currency=usd&ids=" + strings.Join(ids, ",") +
		"&price_change_percentage=1h,24h,7d,30d"

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Errorf("crypto 行情请求失败: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("crypto 行情读取失败: %v", err)
		return nil, err
	}

	var markets []coingeckoMarket
	if err := json.Unmarshal(body, &markets); err != nil {
		log.Errorf("crypto 行情解析失败: %v, body: %s", err, string(body))
		return nil, err
	}

	// 按 id 建索引，保持 coins 的固定展示顺序。
	byID := make(map[string]coingeckoMarket, len(markets))
	for _, m := range markets {
		byID[m.ID] = m
	}

	quotes := make([]CryptoQuote, 0, len(coins))
	for _, c := range coins {
		m, ok := byID[c.ID]
		if !ok {
			continue
		}
		chg24 := m.Change24hPct
		if chg24 == 0 {
			chg24 = m.Change24hFallbck
		}
		quotes = append(quotes, CryptoQuote{
			Name:          c.Name,
			Price:         m.CurrentPrice,
			ChangePercent: chg24,
			Change1h:      m.Change1hPct,
			Change7d:      m.Change7dPct,
			Change30d:     m.Change30dPct,
			Volume24h:     m.TotalVolume,
			ATHChangePct:  m.ATHChangePct,
			MarketCapRank: m.MarketCapRank,
		})
	}
	return quotes, nil
}

// FormatCryptoMessage 把行情与情绪指标整理成一条可读的播报文案。
// fng 为 nil（获取失败）时跳过情绪行。
func FormatCryptoMessage(quotes []CryptoQuote, fng *FearGreed) string {
	var b strings.Builder
	b.WriteString("📊 币圈行情播报\n")
	for _, q := range quotes {
		b.WriteString(fmt.Sprintf("\n%s  $%s  %s %s (24h)\n", q.Name, formatPrice(q.Price), arrow(q.ChangePercent), pct(q.ChangePercent)))
		b.WriteString(fmt.Sprintf("   1h %s · 7d %s · 30d %s\n", pct(q.Change1h), pct(q.Change7d), pct(q.Change30d)))
		b.WriteString(fmt.Sprintf("   量 $%s · 距ATH %s · #%d\n", formatCompact(q.Volume24h), pct(q.ATHChangePct), q.MarketCapRank))
	}
	if fng != nil {
		b.WriteString(fmt.Sprintf("\n%s 恐惧贪婪指数 %d · %s", fng.Emoji(), fng.Value, fng.LabelCN()))
	}
	return strings.TrimRight(b.String(), "\n")
}

// arrow 按涨跌返回箭头 emoji。
func arrow(v float64) string {
	if v < 0 {
		return "🔽"
	}
	return "🔼"
}

// pct 把百分比格式化成带正负号的字符串，如 "+2.12%" / "-1.30%"。
func pct(v float64) string {
	return fmt.Sprintf("%+.2f%%", v)
}

// formatCompact 把大额数字压缩成 K/M/B/T 便于阅读（成交额、市值等）。
func formatCompact(v float64) string {
	av := math.Abs(v)
	switch {
	case av >= 1e12:
		return fmt.Sprintf("%.2fT", v/1e12)
	case av >= 1e9:
		return fmt.Sprintf("%.2fB", v/1e9)
	case av >= 1e6:
		return fmt.Sprintf("%.2fM", v/1e6)
	case av >= 1e3:
		return fmt.Sprintf("%.2fK", v/1e3)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

// FormatPrice 是 formatPrice 的导出版，供其它包（如 inline 结果）复用统一的价格排版。
func FormatPrice(v float64) string { return formatPrice(v) }

// formatPrice 按千分位格式化价格。价格越小保留的小数位越多，避免低价币显示成 0.00。
func formatPrice(v float64) string {
	decimals := 2
	switch av := math.Abs(v); {
	case av > 0 && av < 0.01:
		decimals = 6
	case av < 1:
		decimals = 4
	}

	s := strconv.FormatFloat(v, 'f', decimals, 64)
	intPart, decPart := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, decPart = s[:i], s[i:]
	}

	// 处理负号，避免把 '-' 也参与千分位分组
	sign := ""
	if len(intPart) > 0 && intPart[0] == '-' {
		sign = "-"
		intPart = intPart[1:]
	}

	var out []byte
	for i := 0; i < len(intPart); i++ {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, intPart[i])
	}
	return sign + string(out) + decPart
}
