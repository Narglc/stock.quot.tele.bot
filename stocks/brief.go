package stocks

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MarketBrief 是一份完整的盘面快照：指标 + 衍生品 + 情绪 + 清算 + 宏观 + 数据健康度。
//
// 设计上有两条硬约束：
//
//  1. **每个来源带自己的时间戳**，不是一个全局时间。否则下游（网页徽章、
//     Claude routine 的门禁）无法判断到底是哪部分数据陈旧了。
//  2. **组装过程绝不触发 Apify 调用**。清算簇只用已经在缓存里的数据（见
//     liqMapCachedOnly），拿不到就留空。这个 brief 每小时跑一次，
//     真去打 Apify 一天就是 24 次，而免费档一天只有 5 次。
type MarketBrief struct {
	Symbol      string    `json:"symbol"`
	GeneratedAt time.Time `json:"generated_at"`

	// 价格与基差
	Price     float64 `json:"price"` // 永续
	SpotPrice float64 `json:"spot_price,omitempty"`
	BasisPct  float64 `json:"basis_pct,omitempty"` // (永续-现货)/现货*100，正=升水

	// 趋势与动量（日线）
	RSI14      float64 `json:"rsi14,omitempty"`
	MA50       float64 `json:"ma50,omitempty"`
	MA200      float64 `json:"ma200,omitempty"`
	VsMA200Pct float64 `json:"vs_ma200_pct,omitempty"` // 现价相对 MA200 的偏离%

	// 波动
	ATR14  float64 `json:"atr14,omitempty"`
	ATRPct float64 `json:"atr_pct,omitempty"` // ATR/价格*100

	// 衍生品
	OpenInterestUSD float64 `json:"open_interest_usd,omitempty"`
	OIChangePct     float64 `json:"oi_change_pct,omitempty"`
	OISpanMinutes   int     `json:"oi_span_minutes,omitempty"`
	// OI/市值：注意分子只是 **OKX 单家**的持仓，不是全市场，别当成系统总杠杆读
	OIToMarketCapPct float64 `json:"oi_to_mcap_pct,omitempty"`
	FundingRate      float64 `json:"funding_rate,omitempty"`
	FundingPctile    float64 `json:"funding_percentile,omitempty"` // 相对过去约 33 天
	LongShortRatio   float64 `json:"long_short_ratio,omitempty"`

	// 情绪
	FearGreed      int    `json:"fear_greed,omitempty"`
	FearGreedLabel string `json:"fear_greed_label,omitempty"`

	// 最近的清算簇（仅在缓存里有热力图时填充）
	NearestAbove *ClusterRef `json:"nearest_cluster_above,omitempty"`
	NearestBelow *ClusterRef `json:"nearest_cluster_below,omitempty"`

	// 宏观
	UpcomingEvents []MacroEvent `json:"upcoming_events,omitempty"`
	BLSLatest      []BLSSeries  `json:"bls_latest,omitempty"`

	Sources []SourceStatus `json:"sources"`
	Health  Health         `json:"health"`
}

// ClusterRef 是距现价最近的一个显著清算簇。
type ClusterRef struct {
	Price    float64 `json:"price"`
	ValueUSD float64 `json:"value_usd"`
	DistPct  float64 `json:"dist_pct"` // 相对现价的距离%
	DistATR  float64 `json:"dist_atr"` // 距离相当于几倍 ATR，判断"够不够得着"
	DataAt   string  `json:"data_at"`  // 热力图数据截至时刻
}

// SourceStatus 记录单个数据源这次取得如何。
type SourceStatus struct {
	Name      string    `json:"name"`
	OK        bool      `json:"ok"`
	FetchedAt time.Time `json:"fetched_at,omitempty"`
	Err       string    `json:"error,omitempty"`
	Critical  bool      `json:"critical"` // 失败就不该在这份数据上下判断
}

// Health 是这份 brief 的整体可用性判定。
//
// 存在的意义：让下游能**拒绝**在坏数据上工作。网页显示徽章，
// Claude routine 看到 ok=false 就只报数据质量问题、不产出行情判断——
// 在坏数据上写出来的判断比没有判断更糟。
type Health struct {
	OK      bool     `json:"ok"`
	Reasons []string `json:"reasons,omitempty"`
}

// briefSources 声明每个来源的最大可接受陈旧度与是否关键。
var briefSources = map[string]struct {
	maxAge   time.Duration
	critical bool
}{
	"okx_klines":       {2 * time.Hour, true},
	"okx_spot":         {2 * time.Hour, false},
	"okx_derivs":       {2 * time.Hour, true},
	"okx_funding_hist": {24 * time.Hour, false},
	"coingecko":        {2 * time.Hour, false},
	"fear_greed":       {24 * time.Hour, false},
	"liqmap_cache":     {24 * time.Hour, false},
	"macro_calendar":   {24 * time.Hour, false},
	"bls":              {7 * 24 * time.Hour, false},
}

// briefCache：brief 组装涉及多个外部请求，10 分钟内复用。
var briefCache = newTTLCache[*MarketBrief](10 * time.Minute)

// GetMarketBrief 组装一份完整盘面快照。各来源并发拉取，任一失败只影响自己那部分。
func GetMarketBrief(symbol string) (*MarketBrief, error) {
	sym, err := NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if b, ok := briefCache.get(sym); ok {
		return b, nil
	}

	b := &MarketBrief{Symbol: sym, GeneratedAt: time.Now()}
	var mu sync.Mutex
	note := func(name string, err error) {
		mu.Lock()
		defer mu.Unlock()
		st := SourceStatus{Name: name, OK: err == nil, FetchedAt: time.Now(),
			Critical: briefSources[name].critical}
		if err != nil {
			st.Err = err.Error()
		}
		b.Sources = append(b.Sources, st)
	}

	var wg sync.WaitGroup
	wg.Add(6)

	// 日线：RSI / MA / ATR 全靠它
	go func() {
		defer wg.Done()
		cs, err := GetKlines(sym, "1D", 300)
		note("okx_klines", err)
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if v, ok := RSI(cs, 14); ok {
			b.RSI14 = v
		}
		if v, ok := SMA(cs, 50); ok {
			b.MA50 = v
		}
		if v, ok := SMA(cs, 200); ok {
			b.MA200 = v
		}
		if v, ok := ATR(cs, 14); ok {
			b.ATR14 = v
		}
		if len(cs) > 0 {
			b.Price = cs[len(cs)-1].C
		}
	}()

	// 现货价：算 basis
	go func() {
		defer wg.Done()
		p, err := fetchSpotPrice(sym)
		note("okx_spot", err)
		if err == nil {
			mu.Lock()
			b.SpotPrice = p
			mu.Unlock()
		}
	}()

	// 衍生品
	go func() {
		defer wg.Done()
		d := fetchDerivatives(sym, OIScopeQuery)
		if d == nil {
			note("okx_derivs", fmt.Errorf("全部子项失败"))
			return
		}
		note("okx_derivs", nil)
		mu.Lock()
		defer mu.Unlock()
		b.OpenInterestUSD = d.OpenInterestUSD
		b.OIChangePct, b.LongShortRatio = d.OIChangePct, d.LongShortRatio
		b.OISpanMinutes = int(d.OISpan.Minutes())
		if d.FundingKnown {
			b.FundingRate = d.FundingRate
		}
	}()

	// 资金费率历史 → 百分位
	go func() {
		defer wg.Done()
		hist, err := fetchFundingHistory(sym, 100)
		note("okx_funding_hist", err)
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if p, ok := Percentile(b.FundingRate, hist); ok {
			b.FundingPctile = p
		}
	}()

	// 恐惧贪婪
	go func() {
		defer wg.Done()
		f, err := GetFearGreed()
		note("fear_greed", err)
		if err == nil && f != nil {
			mu.Lock()
			b.FearGreed, b.FearGreedLabel = f.Value, f.LabelCN()
			mu.Unlock()
		}
	}()

	// 宏观：日历 + BLS
	go func() {
		defer wg.Done()
		ev, err := GetMacroCalendar("High", []string{"USD"})
		note("macro_calendar", err)
		if err == nil {
			mu.Lock()
			b.UpcomingEvents = UpcomingWithin(ev, 48*time.Hour)
			mu.Unlock()
		}
		bls, err := GetBLSLatest()
		note("bls", err)
		if err == nil {
			mu.Lock()
			b.BLSLatest = bls
			mu.Unlock()
		}
	}()

	wg.Wait()

	// 市值：用来算 OI/市值，顺带兜底价格
	if qs, _, err := GetCryptoQuotesBySymbols([]string{sym}, false); err == nil && len(qs) > 0 {
		note("coingecko", nil)
		if b.Price == 0 {
			b.Price = qs[0].Price
		}
		if qs[0].MarketCap > 0 && b.OpenInterestUSD > 0 {
			b.OIToMarketCapPct = b.OpenInterestUSD / qs[0].MarketCap * 100
		}
	} else {
		note("coingecko", err)
	}

	b.derive()
	b.fillClusters(note)
	b.Health = evalHealth(b.Sources)

	briefCache.set(sym, b)
	return b, nil
}

// derive 计算依赖多个字段的派生值。
func (b *MarketBrief) derive() {
	if b.SpotPrice > 0 && b.Price > 0 {
		b.BasisPct = (b.Price - b.SpotPrice) / b.SpotPrice * 100
	}
	if b.MA200 > 0 && b.Price > 0 {
		b.VsMA200Pct = (b.Price - b.MA200) / b.MA200 * 100
	}
	if b.Price > 0 && b.ATR14 > 0 {
		b.ATRPct = b.ATR14 / b.Price * 100
	}
}

// minClusterUSD 是「显著簇」的门槛：太小的簇被扫掉也没什么意义。
const minClusterUSD = 5e7

// fillClusters 从**缓存里已有的**热力图取最近的显著清算簇。绝不触发 Apify 调用。
func (b *MarketBrief) fillClusters(note func(string, error)) {
	h, ok := liqMapCachedOnly(b.Symbol)
	if !ok {
		note("liqmap_cache", fmt.Errorf("缓存中无热力图（等下次 /liqmap 或定时刷新）"))
		return
	}
	note("liqmap_cache", nil)

	m := h.ToMap()
	dataAt := ""
	if len(h.Times) > 0 {
		dataAt = time.UnixMilli(h.Times[len(h.Times)-1]).Format("01-02 15:04")
	}
	mk := func(cs []LiqCluster) *ClusterRef {
		// cs 按额降序，这里要的是「最近的显著簇」，所以按距离重新挑
		var best *LiqCluster
		for i := range cs {
			if cs[i].Value < minClusterUSD {
				continue
			}
			if best == nil || absf(cs[i].Price-b.Price) < absf(best.Price-b.Price) {
				best = &cs[i]
			}
		}
		if best == nil {
			return nil
		}
		ref := &ClusterRef{Price: best.Price, ValueUSD: best.Value, DataAt: dataAt}
		if b.Price > 0 {
			ref.DistPct = (best.Price - b.Price) / b.Price * 100
		}
		if b.ATR14 > 0 {
			ref.DistATR = absf(best.Price-b.Price) / b.ATR14
		}
		return ref
	}
	b.NearestAbove, b.NearestBelow = mk(m.Above), mk(m.Below)
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// evalHealth 由各来源状态推出整体健康度。
func evalHealth(sources []SourceStatus) Health {
	h := Health{OK: true}
	now := time.Now()
	for _, s := range sources {
		cfg := briefSources[s.Name]
		if !s.OK {
			if cfg.critical {
				h.OK = false
				h.Reasons = append(h.Reasons, fmt.Sprintf("关键数据源 %s 失败: %s", s.Name, s.Err))
			} else {
				h.Reasons = append(h.Reasons, fmt.Sprintf("次要数据源 %s 缺失: %s", s.Name, s.Err))
			}
			continue
		}
		if cfg.maxAge > 0 && now.Sub(s.FetchedAt) > cfg.maxAge {
			msg := fmt.Sprintf("%s 数据陈旧（超过 %s）", s.Name, cfg.maxAge)
			h.Reasons = append(h.Reasons, msg)
			if cfg.critical {
				h.OK = false
			}
		}
	}
	return h
}

// fetchSpotPrice 取现货最新价（算 basis 用）。
func fetchSpotPrice(symbol string) (float64, error) {
	var env okxEnvelope[[]struct {
		Last string `json:"last"`
	}]
	if err := getJSON("https://www.okx.com/api/v5/market/ticker?instId="+symbol+"-USDT", &env); err != nil {
		return 0, err
	}
	if env.Code != "0" || len(env.Data) == 0 {
		return 0, fmt.Errorf("okx spot code=%s", env.Code)
	}
	return strconv.ParseFloat(env.Data[0].Last, 64)
}

// fetchFundingHistory 取历史资金费率（8h 一次，limit=100 约 33 天），用于算百分位。
func fetchFundingHistory(symbol string, limit int) ([]float64, error) {
	var env okxEnvelope[[]struct {
		FundingRate string `json:"fundingRate"`
	}]
	url := fmt.Sprintf("https://www.okx.com/api/v5/public/funding-rate-history?instId=%s-USDT-SWAP&limit=%d", symbol, limit)
	if err := getJSON(url, &env); err != nil {
		return nil, err
	}
	if env.Code != "0" {
		return nil, fmt.Errorf("okx funding-hist code=%s", env.Code)
	}
	out := make([]float64, 0, len(env.Data))
	for _, d := range env.Data {
		if v, err := strconv.ParseFloat(d.FundingRate, 64); err == nil {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("费率历史为空")
	}
	return out, nil
}

// FormatBrief 把 brief 排成给人看的文本（Telegram 播报用）。
func FormatBrief(b *MarketBrief) string {
	var s strings.Builder
	fmt.Fprintf(&s, "📊 %s 盘面\n\n", b.Symbol)
	fmt.Fprintf(&s, "价格 $%s", formatPrice(b.Price))
	if b.BasisPct != 0 {
		fmt.Fprintf(&s, " · 基差 %+.3f%%", b.BasisPct)
	}
	s.WriteString("\n")

	if b.RSI14 > 0 {
		fmt.Fprintf(&s, "RSI(14) %.1f · 距MA200 %+.1f%% · ATR %.2f%%\n", b.RSI14, b.VsMA200Pct, b.ATRPct)
	}
	if b.OpenInterestUSD > 0 {
		fmt.Fprintf(&s, "持仓 $%s", formatCompact(b.OpenInterestUSD))
		if b.OIChangePct != 0 {
			fmt.Fprintf(&s, "(%+.1f%%/%dm)", b.OIChangePct, b.OISpanMinutes)
		}
		if b.OIToMarketCapPct > 0 {
			fmt.Fprintf(&s, " · OI/市值 %.2f%%(仅OKX)", b.OIToMarketCapPct)
		}
		s.WriteString("\n")
	}
	if b.FundingRate != 0 {
		fmt.Fprintf(&s, "费率 %+.4f%%（%.0f 百分位）", b.FundingRate*100, b.FundingPctile)
		if b.LongShortRatio > 0 {
			fmt.Fprintf(&s, " · 多空比 %.2f", b.LongShortRatio)
		}
		s.WriteString("\n")
	}
	if b.FearGreed > 0 {
		fmt.Fprintf(&s, "恐惧贪婪 %d · %s\n", b.FearGreed, b.FearGreedLabel)
	}

	if b.NearestAbove != nil || b.NearestBelow != nil {
		s.WriteString("\n最近的清算簇：\n")
		if c := b.NearestBelow; c != nil {
			fmt.Fprintf(&s, "  ↓ $%s  $%s（%.1f%% / %.1f×ATR）\n", formatPrice(c.Price), formatCompact(c.ValueUSD), c.DistPct, c.DistATR)
		}
		if c := b.NearestAbove; c != nil {
			fmt.Fprintf(&s, "  ↑ $%s  $%s（+%.1f%% / %.1f×ATR）\n", formatPrice(c.Price), formatCompact(c.ValueUSD), c.DistPct, c.DistATR)
		}
	}

	if len(b.UpcomingEvents) > 0 {
		s.WriteString("\n未来 48h 宏观：\n")
		for _, e := range b.UpcomingEvents {
			fmt.Fprintf(&s, "  %s %s（预测 %s / 前值 %s）\n", e.At.Local().Format("01-02 15:04"), e.Title, e.Forecast, e.Previous)
		}
	}

	if !b.Health.OK {
		s.WriteString("\n⚠️ 数据不完整：" + strings.Join(b.Health.Reasons, "；"))
	}
	return strings.TrimRight(s.String(), "\n")
}
