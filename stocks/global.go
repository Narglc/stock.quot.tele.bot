package stocks

import (
	"fmt"
	"strings"
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
)

// GlobalMarket 是加密全市场概览（CoinGecko /global，免费免鉴权）。
type GlobalMarket struct {
	TotalMarketCap     float64 // 全市场总市值（USD）
	TotalVolume24h     float64 // 全市场 24h 成交额（USD）
	MarketCapChange24h float64 // 总市值 24h 涨跌幅（%）
	BTCDominance       float64 // BTC 市值占比（%）
	ETHDominance       float64 // ETH 市值占比（%）
}

// globalResponse 对应 /global 接口，只取用得到的字段。
type globalResponse struct {
	Data struct {
		TotalMarketCap     map[string]float64 `json:"total_market_cap"`
		TotalVolume        map[string]float64 `json:"total_volume"`
		MarketCapPct       map[string]float64 `json:"market_cap_percentage"`
		MarketCapChange24h float64            `json:"market_cap_change_percentage_24h_usd"`
	} `json:"data"`
}

// globalCache 缓存全市场概览：总市值/占比变化很慢，60s 内复用足够。
var globalCache = newTTLCache[*GlobalMarket](60 * time.Second)

// GetGlobalMarket 拉取加密全市场概览（best-effort，失败上层忽略、不影响行情）。
func GetGlobalMarket() (*GlobalMarket, error) {
	if v, ok := globalCache.get("global"); ok {
		return v, nil
	}

	var g globalResponse
	if err := fetchJSON(marketClient, "全市场概览", "https://api.coingecko.com/api/v3/global", &g); err != nil {
		log.Warnf("%v", err)
		return nil, err
	}

	res := &GlobalMarket{
		TotalMarketCap:     g.Data.TotalMarketCap["usd"],
		TotalVolume24h:     g.Data.TotalVolume["usd"],
		MarketCapChange24h: g.Data.MarketCapChange24h,
		BTCDominance:       g.Data.MarketCapPct["btc"],
		ETHDominance:       g.Data.MarketCapPct["eth"],
	}
	globalCache.set("global", res)
	return res, nil
}

// FormatGlobalMarket 把全市场概览排成两行文案，供整点播报附加在行情之后。
// g 为 nil（获取失败）时返回空串，调用方据此不追加该段。
func FormatGlobalMarket(g *GlobalMarket) string {
	if g == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🌐 全市场 $%s %s (24h) · 量 $%s\n",
		formatCompact(g.TotalMarketCap), pct(g.MarketCapChange24h), formatCompact(g.TotalVolume24h)))
	b.WriteString(fmt.Sprintf("   BTC.D %.1f%% · ETH.D %.1f%%", g.BTCDominance, g.ETHDominance))
	return b.String()
}
