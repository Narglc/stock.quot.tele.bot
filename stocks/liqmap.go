package stocks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/utils"
)

// LiqCluster 是某个价位的清算簇（该价位待爆的名义额，USD）。
type LiqCluster struct {
	Price float64
	Value float64
}

// LiqMap 是某币种的清算地图快照（取热力图最新时间列，按现价拆上/下方）。
type LiqMap struct {
	Symbol       string
	CurrentPrice float64
	Interval     string       // 数据时间粒度，如 "15m"
	Above        []LiqCluster // 现价上方：空头爆仓区（价格上行会去扫），按额降序
	Below        []LiqCluster // 现价下方：多头爆仓区（价格下行会去扫），按额降序
	MaxValue     float64      // 最新列峰值，用于归一化/判断强弱
}

// LiqHeatmap 是完整的清算热力图网格（时间 × 价格）。
type LiqHeatmap struct {
	Symbol       string
	Interval     string
	Prices       []float64   // y 轴价格阶梯，长度 P
	Times        []int64     // x 轴时间戳(ms)，长度 T
	Values       [][]float64 // [价格索引][时间索引] 清算额(USD)，P×T
	CurrentPrice float64
	MaxValue     float64
	Candles      []Candle // 叠加在热力图上的 K线（best-effort，可能为空）
}

// apifyClient：Apify actor 同步运行较慢（约 60~90s），用长超时。
var apifyClient = &http.Client{Timeout: 120 * time.Second}

// liqMapResponse 对应 Apify(CoinAnk) actor 返回体（数组取首元素）。
type liqMapResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"` // 成功描述，或"配额用尽"等原因
	Interval   string `json:"chartInterval"`
	LiqHeatMap struct {
		Data           [][]string `json:"data"`           // 每格 [xIdx, yIdx, value]（均为字符串）
		ChartTimeArray []int64    `json:"chartTimeArray"` // x -> 时间戳(ms)
		PriceArray     []float64  `json:"priceArray"`     // y -> 价格（数字）
		MaxLiqValue    float64    `json:"maxLiqValue"`
	} `json:"liqHeatMap"`
}

// GetLiqHeatmap 通过 Apify(CoinAnk) 拉取某币种完整清算热力图网格 + OKX 现价。
// symbol 形如 "BTC"，内部拼成 "BTCUSDT"。apifyToken 走环境/配置传入，勿硬编码。
//
// 带 10 分钟缓存 + 同币种串行化（见 liqalbum.go）：Apify actor 单次运行 60~90s
// 且计费，热力图粒度本身就是 15m，10 分钟内重复拉没有任何新信息。
func GetLiqHeatmap(symbol, apifyToken string) (*LiqHeatmap, error) {
	sym, err := NormalizeSymbol(symbol)
	if err != nil {
		return nil, err
	}

	if h, ok := liqCache.get(sym); ok {
		logLiqCacheHit(sym)
		return h, nil
	}

	// 同币种串行：并发穿透意味着多次计费调用。
	mu := liqLock(sym)
	mu.Lock()
	defer mu.Unlock()
	// 等锁期间别人可能已经填好了缓存，拿到锁后再查一次。
	if h, ok := liqCache.get(sym); ok {
		logLiqCacheHit(sym)
		return h, nil
	}

	h, err := fetchLiqHeatmap(sym, apifyToken)
	if err != nil {
		return nil, err
	}
	liqCache.set(sym, h)

	// 只在真的拉到新数据时推快照：缓存命中说明数据没变，推了也是覆盖成同一份。
	go PushHeatmapSnapshot(h)

	return h, nil
}

// fetchLiqHeatmap 真正打 Apify 拉一次热力图并补齐现价与 K线（无缓存）。
// sym 必须是已经过 NormalizeSymbol 的大写符号。
func fetchLiqHeatmap(sym, apifyToken string) (*LiqHeatmap, error) {
	r, err := fetchLiqRaw(sym, apifyToken)
	if err != nil {
		return nil, err
	}
	h, err := buildHeatmap(sym, r)
	if err != nil {
		return nil, err
	}
	cur, err := fetchLastPrice(sym)
	if err != nil {
		log.Warnf("清算图取现价失败 %s: %v，回落价格轴中点", sym, err)
		cur = h.Prices[len(h.Prices)/2]
	}
	h.CurrentPrice = cur

	// K线（best-effort，用于热力图叠蜡烛）。粒度与热力图一致。
	if kl, kerr := GetKlines(sym, klineBar(h.Interval), 300); kerr == nil {
		h.Candles = kl
	} else {
		log.Warnf("清算图 K线获取失败 %s: %v", sym, kerr)
	}
	return h, nil
}

// GetLiqMap 取清算地图（现价上/下方清算簇），底层复用 GetLiqHeatmap 的最新时间列。
func GetLiqMap(symbol, apifyToken string) (*LiqMap, error) {
	h, err := GetLiqHeatmap(symbol, apifyToken)
	if err != nil {
		return nil, err
	}
	return h.ToMap(), nil
}

// fetchLiqRaw 调 Apify 同步接口并反序列化出首条有效响应。symbol 须已归一化。
func fetchLiqRaw(symbol, apifyToken string) (*liqMapResponse, error) {
	if apifyToken == "" {
		return nil, fmt.Errorf("apify token 为空")
	}
	pair := symbol + "USDT"
	url := "https://api.apify.com/v2/acts/api_merge~coinank-liquidation-heatmap/run-sync-get-dataset-items?token=" + apifyToken
	reqBody, _ := json.Marshal(map[string]string{"symbol": pair})
	resp, err := apifyClient.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// Apify run-sync 成功可能返回 200 或 201(Created)，接受整个 2xx。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("apify bad status %d: %s", resp.StatusCode, utils.Truncate(string(raw), 200))
	}
	return parseLiqResponse(raw)
}

// parseLiqResponse 把 Apify 响应体解析成首条有效记录（与网络解耦，便于测试）。
func parseLiqResponse(raw []byte) (*liqMapResponse, error) {
	var arr []liqMapResponse
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("解析失败: %v, body: %s", err, utils.Truncate(string(raw), 200))
	}
	if len(arr) == 0 || !arr[0].Success {
		return nil, fmt.Errorf("apify 返回无有效数据")
	}
	if len(arr[0].LiqHeatMap.PriceArray) == 0 || len(arr[0].LiqHeatMap.ChartTimeArray) == 0 {
		// 配额用尽等情况下 actor 也回 success:true 但无数据，把 message 带出来。
		return nil, fmt.Errorf("热力图数据为空: %s", arr[0].Message)
	}
	return &arr[0], nil
}

// buildHeatmap 把稀疏三元组展开成 P×T 稠密网格。
func buildHeatmap(symbol string, r *liqMapResponse) (*LiqHeatmap, error) {
	hm := r.LiqHeatMap
	P, T := len(hm.PriceArray), len(hm.ChartTimeArray)
	if P == 0 || T == 0 {
		return nil, fmt.Errorf("热力图数据为空")
	}
	values := make([][]float64, P)
	for i := range values {
		values[i] = make([]float64, T)
	}
	for _, cell := range hm.Data {
		if len(cell) != 3 {
			continue
		}
		x, _ := strconv.Atoi(cell[0])
		y, _ := strconv.Atoi(cell[1])
		v, _ := strconv.ParseFloat(cell[2], 64)
		if x >= 0 && x < T && y >= 0 && y < P {
			values[y][x] = v
		}
	}
	return &LiqHeatmap{
		Symbol:   symbol,
		Interval: r.Interval,
		Prices:   hm.PriceArray,
		Times:    hm.ChartTimeArray,
		Values:   values,
		MaxValue: hm.MaxLiqValue,
	}, nil
}

// ToMap 从热力图最新时间列派生清算地图（按现价拆上/下方，按额降序）。
func (h *LiqHeatmap) ToMap() *LiqMap {
	m := &LiqMap{
		Symbol:       h.Symbol,
		CurrentPrice: h.CurrentPrice,
		Interval:     h.Interval,
		MaxValue:     h.MaxValue,
	}
	if len(h.Times) == 0 {
		return m
	}
	lastX := len(h.Times) - 1
	for yi, price := range h.Prices {
		v := h.Values[yi][lastX]
		if v <= 0 {
			continue
		}
		c := LiqCluster{Price: price, Value: v}
		if price >= h.CurrentPrice {
			m.Above = append(m.Above, c)
		} else {
			m.Below = append(m.Below, c)
		}
	}
	sort.Slice(m.Above, func(i, j int) bool { return m.Above[i].Value > m.Above[j].Value })
	sort.Slice(m.Below, func(i, j int) bool { return m.Below[i].Value > m.Below[j].Value })
	return m
}

// fetchLastPrice 取某币种 OKX 永续最新价，用于把清算簇拆成现价上/下方。symbol 须已归一化。
func fetchLastPrice(symbol string) (float64, error) {
	var env okxEnvelope[[]struct {
		Last string `json:"last"`
	}]
	url := "https://www.okx.com/api/v5/market/ticker?instId=" + symbol + "-USDT-SWAP"
	if err := getJSON(url, &env); err != nil {
		return 0, err
	}
	if env.Code != "0" || len(env.Data) == 0 {
		return 0, fmt.Errorf("okx ticker code=%s", env.Code)
	}
	return strconv.ParseFloat(env.Data[0].Last, 64)
}

// FormatLiqMap 把清算地图排成文案，上/下方各取额最大的 topN 个簇。
func FormatLiqMap(m *LiqMap, topN int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🔥 %s 清算地图 · 现价 $%s (%s)\n", m.Symbol, FormatPrice(m.CurrentPrice), m.Interval)
	b.WriteString("\n↑ 上方空头爆仓区（价格上行会去扫）\n")
	writeClusters(&b, m.Above, topN)
	b.WriteString("\n↓ 下方多头爆仓区（价格下行会去扫）\n")
	writeClusters(&b, m.Below, topN)
	return strings.TrimRight(b.String(), "\n")
}

func writeClusters(b *strings.Builder, cs []LiqCluster, topN int) {
	if len(cs) == 0 {
		b.WriteString("  （无显著簇）\n")
		return
	}
	if len(cs) > topN {
		cs = cs[:topN]
	}
	for _, c := range cs {
		fmt.Fprintf(b, "  $%s  %s\n", FormatPrice(c.Price), formatCompact(c.Value))
	}
}
