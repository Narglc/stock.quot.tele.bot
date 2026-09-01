package stocks

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	High24h       float64 // 24h 最高价
	Low24h        float64 // 24h 最低价
	MarketCap     float64 // 市值（USD）
	ATHChangePct  float64 // 距历史最高点涨跌幅（通常为负，越接近 0 越接近历史高点）
	MarketCapRank int     // 市值排名

	// 衍生品指标（OKX 永续，best-effort；0 = 未取到，展示时省略）。
	OpenInterestUSD float64       // 未平仓合约名义价值（USD）
	LongShortRatio  float64       // 全市场多空账户比，>1 偏多
	FundingRate     float64       // 资金费率（如 0.0001=0.01%），正=多头付费
	FundingKnown    bool          // 是否取到资金费率
	OIChangePct     float64       // 持仓量相比上次观测的变化百分比
	OIChangeKnown   bool          // 是否有上次观测可比
	OISpan          time.Duration // 上述变化对应的时间跨度（展示时标出，避免被误读成小时环比）
}

// coingeckoMarket 对应 /coins/markets 接口的单条返回（只取用得到的字段）。
// 百分比字段可能为 null，反序列化后即为 0，可接受。
type coingeckoMarket struct {
	ID               string  `json:"id"`
	CurrentPrice     float64 `json:"current_price"`
	MarketCapRank    int     `json:"market_cap_rank"`
	TotalVolume      float64 `json:"total_volume"`
	High24h          float64 `json:"high_24h"`
	Low24h           float64 `json:"low_24h"`
	MarketCap        float64 `json:"market_cap"`
	ATHChangePct     float64 `json:"ath_change_percentage"`
	Change1hPct      float64 `json:"price_change_percentage_1h_in_currency"`
	Change24hPct     float64 `json:"price_change_percentage_24h_in_currency"`
	Change7dPct      float64 `json:"price_change_percentage_7d_in_currency"`
	Change30dPct     float64 `json:"price_change_percentage_30d_in_currency"`
	Change24hFallbck float64 `json:"price_change_percentage_24h"` // 无 in_currency 时兜底
}

// GetCryptoQuotes 拉取默认关注币种（BTC/ETH）的行情快照（含衍生品指标）。
// 供整点播报使用，可容忍衍生品带来的少量额外延迟。
func GetCryptoQuotes() ([]CryptoQuote, error) {
	quotes, err := fetchQuotes(cryptoCoins)
	if err != nil {
		return nil, err
	}
	enrichDerivatives(quotes, OIScopeBroadcast)
	return quotes, nil
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
// withDerivatives 为 true 时并发补充持仓量/多空比（/price 用）；
// inline 查询求快，传 false 跳过，避免被 Binance 阻塞拖慢。
func GetCryptoQuotesBySymbols(symbols []string, withDerivatives bool) ([]CryptoQuote, []string, error) {
	coins, unknown := ResolveSymbols(symbols)
	if len(coins) == 0 {
		return nil, unknown, nil
	}
	quotes, err := fetchQuotes(coins)
	if err != nil {
		return quotes, unknown, err
	}
	if withDerivatives {
		enrichDerivatives(quotes, OIScopeQuery)
	}
	return quotes, unknown, nil
}

// enrichDerivatives 并发为每个币种补充 OKX 永续合约的持仓量/多空比/资金费率（best-effort）。
// 任一币种失败只是留空对应字段，不影响行情主体。
// scope 决定用哪份持仓量基线：播报与用户查询各记一份，互不干扰。
func enrichDerivatives(quotes []CryptoQuote, scope OIScope) {
	var wg sync.WaitGroup
	for i := range quotes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if d := fetchDerivatives(quotes[i].Name, scope); d != nil {
				quotes[i].OpenInterestUSD = d.OpenInterestUSD
				quotes[i].LongShortRatio = d.LongShortRatio
				quotes[i].FundingRate = d.FundingRate
				quotes[i].FundingKnown = d.FundingKnown
				quotes[i].OIChangePct = d.OIChangePct
				quotes[i].OIChangeKnown = d.OIChangeKnown
				quotes[i].OISpan = d.OISpan
			}
		}(i)
	}
	wg.Wait()
}

// quotesCache 缓存 /coins/markets 的结果。
// inline 查询是「每敲一个字符触发一次」的高频路径，不缓存极易打满 CoinGecko 免费额度。
// 30s 对行情播报（每小时）无影响，对 /price 也足够新鲜。
var quotesCache = newTTLCache[[]CryptoQuote](30 * time.Second)

// fetchQuotes 用 CoinGecko /coins/markets 一次拉取多个币种的量价/动量/位置数据。
// 无需鉴权，且不受 Binance 的地区限制影响。
func fetchQuotes(coins []coin) ([]CryptoQuote, error) {
	ids := make([]string, 0, len(coins))
	for _, c := range coins {
		ids = append(ids, c.ID)
	}
	cacheKey := strings.Join(ids, ",")

	// 命中缓存也要返回副本：调用方（enrichDerivatives）会就地写入衍生品字段，
	// 直接返回缓存里的切片会让不同 scope 的调用互相污染。
	if cached, ok := quotesCache.get(cacheKey); ok {
		return slices.Clone(cached), nil
	}

	url := "https://api.coingecko.com/api/v3/coins/markets" +
		"?vs_currency=usd&ids=" + cacheKey +
		"&price_change_percentage=1h,24h,7d,30d"

	var markets []coingeckoMarket
	if err := fetchJSON(marketClient, "crypto 行情", url, &markets); err != nil {
		log.Errorf("%v", err)
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
			High24h:       m.High24h,
			Low24h:        m.Low24h,
			MarketCap:     m.MarketCap,
			ATHChangePct:  m.ATHChangePct,
			MarketCapRank: m.MarketCapRank,
		})
	}

	quotesCache.set(cacheKey, slices.Clone(quotes))
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
		if q.High24h > 0 || q.Low24h > 0 {
			b.WriteString(fmt.Sprintf("   高低 $%s~$%s · 量 $%s\n", formatPrice(q.Low24h), formatPrice(q.High24h), formatCompact(q.Volume24h)))
		} else {
			b.WriteString(fmt.Sprintf("   量 $%s\n", formatCompact(q.Volume24h)))
		}
		b.WriteString(fmt.Sprintf("   市值 $%s · 距ATH %s · #%d\n", formatCompact(q.MarketCap), pct(q.ATHChangePct), q.MarketCapRank))
		// 衍生品行仅在取到数据时出现（OKX 不可达则整行省略）。
		parts := make([]string, 0, 3)
		if q.OpenInterestUSD > 0 {
			oi := "持仓 $" + formatCompact(q.OpenInterestUSD)
			if q.OIChangeKnown {
				// 带上时间跨度：这个变化是「距上次观测」而非固定的小时环比，
				// 不标出来会被误读（播报间隔 1h，但 /price 的基线取决于上次查询时刻）。
				if span := formatSpan(q.OISpan); span != "" {
					oi += fmt.Sprintf("(%+.1f%% / %s)", q.OIChangePct, span)
				} else {
					oi += fmt.Sprintf("(%+.1f%%)", q.OIChangePct)
				}
			}
			parts = append(parts, oi)
		}
		if q.LongShortRatio > 0 {
			parts = append(parts, fmt.Sprintf("多空比 %.2f", q.LongShortRatio))
		}
		if q.FundingKnown {
			fr := fmt.Sprintf("费率 %+.4f%%", q.FundingRate*100)
			if q.FundingRate > 0 {
				fr += "(多头付费)"
			} else if q.FundingRate < 0 {
				fr += "(空头付费)"
			}
			parts = append(parts, fr)
		}
		if len(parts) > 0 {
			b.WriteString("   " + strings.Join(parts, " · ") + "\n")
		}
		// 持仓变化 + 价格方向的组合解读（有明显变化时给一句）。
		if q.OIChangeKnown && q.OpenInterestUSD > 0 {
			if s := oiInsight(q.Change1h, q.OIChangePct); s != "" {
				b.WriteString("   " + s + "\n")
			}
		}
	}
	if fng != nil {
		b.WriteString(fmt.Sprintf("\n%s 恐惧贪婪指数 %d · %s", fng.Emoji(), fng.Value, fng.LabelCN()))
	}
	return strings.TrimRight(b.String(), "\n")
}

// oiInsight 按"价格方向 + 持仓变化"给一句解读。变化太小（<0.5%）不解读，避免噪声。
func oiInsight(priceChg, oiChg float64) string {
	if math.Abs(oiChg) < 0.5 {
		return ""
	}
	switch {
	case priceChg >= 0 && oiChg > 0:
		return "↗ 增仓上涨：新多进场，趋势偏健康"
	case priceChg >= 0 && oiChg < 0:
		return "↗ 减仓上涨：空头回补，追高需谨慎"
	case priceChg < 0 && oiChg > 0:
		return "↘ 增仓下跌：空头进场施压"
	default:
		return "↘ 减仓下跌：多头离场"
	}
}

// formatSpan 把时间跨度排成紧凑可读的形式（"42m" / "1.5h" / "2.1d"）。
// 跨度不可用（零值）时返回空串，调用方据此省略。
func formatSpan(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%.1fh", d.Hours())
	default:
		return fmt.Sprintf("%.1fd", d.Hours()/24)
	}
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
