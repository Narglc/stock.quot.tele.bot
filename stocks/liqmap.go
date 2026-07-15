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

// GetLiqMap 通过 Apify(CoinAnk) 拉取某币种清算热力图，取最新时间列聚合成清算地图。
// symbol 形如 "BTC"，内部拼成 "BTCUSDT"。apifyToken 走环境/配置传入，勿硬编码。
func GetLiqMap(symbol, apifyToken string) (*LiqMap, error) {
	if apifyToken == "" {
		return nil, fmt.Errorf("apify token 为空")
	}
	pair := strings.ToUpper(symbol) + "USDT"

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
		return nil, fmt.Errorf("apify bad status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var arr []liqMapResponse
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("解析失败: %v, body: %s", err, truncate(string(raw), 200))
	}
	if len(arr) == 0 || !arr[0].Success {
		return nil, fmt.Errorf("apify 返回无有效数据")
	}
	hm := arr[0].LiqHeatMap
	if len(hm.PriceArray) == 0 || len(hm.ChartTimeArray) == 0 {
		// actor 在配额用尽等情况下也回 success:true 但无数据，把 message 带出来。
		return nil, fmt.Errorf("热力图数据为空: %s", arr[0].Message)
	}

	prices := hm.PriceArray
	lastX := strconv.Itoa(len(hm.ChartTimeArray) - 1) // 最新时间列索引

	// 只取最新时间列，聚合出「价位索引 -> 清算额」。
	byPrice := map[int]float64{}
	for _, cell := range hm.Data {
		if len(cell) != 3 || cell[0] != lastX {
			continue
		}
		v, _ := strconv.ParseFloat(cell[2], 64)
		if v <= 0 {
			continue
		}
		yi, _ := strconv.Atoi(cell[1])
		if yi >= 0 && yi < len(prices) {
			byPrice[yi] += v
		}
	}

	cur, err := fetchLastPrice(symbol)
	if err != nil {
		log.Warnf("清算地图取现价失败 %s: %v，回落价格轴中点", symbol, err)
		cur = prices[len(prices)/2]
	}

	m := &LiqMap{
		Symbol:       strings.ToUpper(symbol),
		CurrentPrice: cur,
		Interval:     arr[0].Interval,
		MaxValue:     hm.MaxLiqValue,
	}
	for yi, v := range byPrice {
		c := LiqCluster{Price: prices[yi], Value: v}
		if c.Price >= cur {
			m.Above = append(m.Above, c)
		} else {
			m.Below = append(m.Below, c)
		}
	}
	// 按清算额降序：最大的磁吸簇排前面。
	sort.Slice(m.Above, func(i, j int) bool { return m.Above[i].Value > m.Above[j].Value })
	sort.Slice(m.Below, func(i, j int) bool { return m.Below[i].Value > m.Below[j].Value })
	return m, nil
}

// fetchLastPrice 取某币种 OKX 永续最新价，用于把清算簇拆成现价上/下方。
func fetchLastPrice(symbol string) (float64, error) {
	var env okxEnvelope[[]struct {
		Last string `json:"last"`
	}]
	url := "https://www.okx.com/api/v5/market/ticker?instId=" + strings.ToUpper(symbol) + "-USDT-SWAP"
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
	b.WriteString(fmt.Sprintf("🔥 %s 清算地图 · 现价 $%s (%s)\n", m.Symbol, FormatPrice(m.CurrentPrice), m.Interval))
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

// truncate 截断字符串用于错误日志，避免打印超大 body。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
