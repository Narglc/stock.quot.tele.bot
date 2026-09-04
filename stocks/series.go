package stocks

import (
	"fmt"
	"strconv"
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
)

// 长周期序列：给网页画曲线用。
//
// 与 MarketBrief 分开推送，因为两者的变化频率差一个数量级：
// brief 每小时变，长周期序列一天才多一个点。混在一起会让每小时的推送
// 白白带上几十 KB 不变的历史数据。

// SeriesPoint 是曲线上的一个点。用定长数组而不是结构体：
// 序列化成 [ts, v1, v2] 比 {"ts":...,"close":...} 省一半体积，1800 个点差别很明显。
type SeriesPoint [3]float64 // [时间戳ms, 值1, 值2]

// PriceSeries 是价格与 MA200 的长周期序列。
type PriceSeries struct {
	Symbol string        `json:"symbol"`
	Bar    string        `json:"bar"`
	Points []SeriesPoint `json:"points"` // [ts, close, ma200]（ma200 未成形时为 0）
}

// OISeries 是持仓量的历史序列。
//
// 单看当前持仓量是个没有参照的数字——2.39B 是高是低？只有放进曲线里才看得出来。
type OISeries struct {
	Symbol string        `json:"symbol"`
	Points []SeriesPoint `json:"points"` // [ts, 持仓USD, 成交额USD]
}

// MarketSeries 是推给网页的长周期数据包。
type MarketSeries struct {
	Symbol      string       `json:"symbol"`
	GeneratedAt time.Time    `json:"generated_at"`
	Price       *PriceSeries `json:"price,omitempty"`
	OI          *OISeries    `json:"oi,omitempty"`
}

// seriesCache：一天才多一个点，缓存 12 小时。
var seriesCache = newTTLCache[*MarketSeries](12 * time.Hour)

// maxHistoryPages 限制分页次数：每页 100 根日线，20 页 ≈ 5.5 年，够画 5 年图。
const maxHistoryPages = 20

// GetMarketSeries 组装长周期序列（价格+MA200、持仓量）。
func GetMarketSeries(symbol string) (*MarketSeries, error) {
	sym, err := NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if s, ok := seriesCache.get(sym); ok {
		return s, nil
	}

	ms := &MarketSeries{Symbol: sym, GeneratedAt: time.Now()}

	if ps, err := buildPriceSeries(sym); err != nil {
		log.Warnf("价格序列构建失败 %s: %v", sym, err)
	} else {
		ms.Price = ps
	}
	if os, err := buildOISeries(sym); err != nil {
		log.Warnf("持仓量序列构建失败 %s: %v", sym, err)
	} else {
		ms.OI = os
	}

	if ms.Price == nil && ms.OI == nil {
		return nil, fmt.Errorf("两条序列都没拿到")
	}
	seriesCache.set(sym, ms)
	return ms, nil
}

// buildPriceSeries 分页拉日线并算出滚动 MA200。
//
// 用现货（BTC-USDT）而不是永续：永续合约上线晚，历史短；
// 而 MA200 这种长周期指标看的是现货的价格结构。
func buildPriceSeries(sym string) (*PriceSeries, error) {
	cs, err := fetchDailyHistory(sym, maxHistoryPages)
	if err != nil {
		return nil, err
	}
	if len(cs) < 200 {
		return nil, fmt.Errorf("日线只有 %d 根，不足以算 MA200", len(cs))
	}

	pts := make([]SeriesPoint, 0, len(cs))
	// 滚动窗口求和，避免每个点都重新加 200 次
	var sum float64
	for i, c := range cs {
		sum += c.C
		if i >= 200 {
			sum -= cs[i-200].C
		}
		ma := 0.0
		if i >= 199 {
			ma = sum / 200
		}
		pts = append(pts, SeriesPoint{float64(c.Ts), c.C, ma})
	}
	return &PriceSeries{Symbol: sym, Bar: "1D", Points: pts}, nil
}

// fetchDailyHistory 用 after 游标分页拉历史日线，返回时间升序。
//
// OKX 的 /market/candles 只给最近 300 根，要更久得走 /market/history-candles，
// 且 after 的语义是「返回早于该时间戳的记录」，所以要拿每页最后一条的 ts 当下一页的游标。
func fetchDailyHistory(sym string, pages int) ([]Candle, error) {
	seen := map[int64]bool{}
	var all []Candle
	var after string

	for p := 0; p < pages; p++ {
		url := "https://www.okx.com/api/v5/market/history-candles?instId=" + sym + "-USDT&bar=1D&limit=100"
		if after != "" {
			url += "&after=" + after
		}

		var env okxEnvelope[[][]string]
		if err := getJSON(url, &env); err != nil {
			if len(all) > 0 {
				break // 已经拿到一些就用着，别因为最后一页失败前功尽弃
			}
			return nil, err
		}
		if env.Code != "0" || len(env.Data) == 0 {
			break
		}

		for _, r := range env.Data {
			if len(r) < 5 {
				continue
			}
			ts, err := strconv.ParseInt(r[0], 10, 64)
			if err != nil || seen[ts] {
				continue
			}
			seen[ts] = true
			c := Candle{Ts: ts}
			c.O, _ = strconv.ParseFloat(r[1], 64)
			c.H, _ = strconv.ParseFloat(r[2], 64)
			c.L, _ = strconv.ParseFloat(r[3], 64)
			c.C, _ = strconv.ParseFloat(r[4], 64)
			all = append(all, c)
		}
		after = env.Data[len(env.Data)-1][0]
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("历史日线为空")
	}
	// OKX 返回新→旧，翻成旧→新
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	return all, nil
}

// buildOISeries 拉持仓量与成交额的日线历史（OKX rubik，约 180 天）。
func buildOISeries(sym string) (*OISeries, error) {
	var env okxEnvelope[[][]string]
	url := "https://www.okx.com/api/v5/rubik/stat/contracts/open-interest-volume?ccy=" + sym + "&period=1D"
	if err := getJSON(url, &env); err != nil {
		return nil, err
	}
	if env.Code != "0" || len(env.Data) == 0 {
		return nil, fmt.Errorf("okx oi-history code=%s", env.Code)
	}

	pts := make([]SeriesPoint, 0, len(env.Data))
	for _, r := range env.Data {
		if len(r) < 3 {
			continue
		}
		ts, err := strconv.ParseInt(r[0], 10, 64)
		if err != nil {
			continue
		}
		oi, _ := strconv.ParseFloat(r[1], 64)
		vol, _ := strconv.ParseFloat(r[2], 64)
		pts = append(pts, SeriesPoint{float64(ts), oi, vol})
	}
	if len(pts) == 0 {
		return nil, fmt.Errorf("持仓量历史为空")
	}
	// rubik 也是新→旧
	for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
		pts[i], pts[j] = pts[j], pts[i]
	}
	return &OISeries{Symbol: sym, Points: pts}, nil
}
