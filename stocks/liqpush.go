package stocks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/utils"
)

// 清算图快照推送：bot 拉到新数据后 PUT 给 Cloudflare Worker，网页只读 KV。
//
// 为什么是 bot 推而不是网页自己拉：Apify actor 单次 60~90s 且计费，
// 让网页直连意味着每个访客都可能烧一次配额。bot 侧已经有 10 分钟缓存，
// 保持 bot 作为唯一的 Apify 调用方，配额就完全可控，Worker 也不必持有 APIFY_TOKEN。

var (
	liqPushURL   string
	liqPushToken string
)

// liqPushClient 推送用的客户端，短超时——推送失败不该拖慢发图。
var liqPushClient = &http.Client{Timeout: 10 * time.Second}

// SetLiqPushTarget 注入快照推送端点（url 为空则不推送）。
func SetLiqPushTarget(url, token string) {
	liqPushURL, liqPushToken = url, token
}

// liqSnapshot 是推给 Worker 的快照格式。
//
// 网格用稀疏三元组 [x, y, v] 而不是稠密二维数组：热力图绝大多数格子是 0，
// 稠密序列化出来能有几 MB，稀疏通常只有几十 KB。前端展开成稠密数组即可。
type liqSnapshot struct {
	Symbol       string       `json:"symbol"`
	Interval     string       `json:"interval"`
	CurrentPrice float64      `json:"currentPrice"`
	MaxValue     float64      `json:"maxValue"`
	Prices       []float64    `json:"prices"`
	Times        []int64      `json:"times"`
	Cells        [][3]float64 `json:"cells"` // [时间索引, 价格索引, 清算额]
	Candles      []snapCandle `json:"candles,omitempty"`
}

type snapCandle struct {
	Ts int64   `json:"ts"`
	O  float64 `json:"o"`
	H  float64 `json:"h"`
	L  float64 `json:"l"`
	C  float64 `json:"c"`
}

// buildSnapshot 把热力图转成推送格式（稀疏化）。
func buildSnapshot(h *LiqHeatmap) *liqSnapshot {
	cells := make([][3]float64, 0, 4096)
	for y, row := range h.Values {
		for x, v := range row {
			if v > 0 {
				cells = append(cells, [3]float64{float64(x), float64(y), v})
			}
		}
	}

	candles := make([]snapCandle, 0, len(h.Candles))
	for _, c := range h.Candles {
		candles = append(candles, snapCandle{Ts: c.Ts, O: c.O, H: c.H, L: c.L, C: c.C})
	}

	return &liqSnapshot{
		Symbol:       h.Symbol,
		Interval:     h.Interval,
		CurrentPrice: h.CurrentPrice,
		MaxValue:     h.MaxValue,
		Prices:       h.Prices,
		Times:        h.Times,
		Cells:        cells,
		Candles:      candles,
	}
}

// PushHeatmapSnapshot 把热力图快照推给 Worker。
// 未配置端点时静默跳过；失败只记日志，不影响 bot 主流程。
func PushHeatmapSnapshot(h *LiqHeatmap) {
	if liqPushURL == "" || h == nil {
		return
	}

	snap := buildSnapshot(h)
	body, err := json.Marshal(snap)
	if err != nil {
		log.Warnf("清算图快照序列化失败 %s: %v", h.Symbol, err)
		return
	}

	url := liqPushURL + "/api/heatmap/" + h.Symbol
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		log.Warnf("清算图快照请求构造失败 %s: %v", h.Symbol, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if liqPushToken != "" {
		req.Header.Set("X-Auth-Token", liqPushToken)
	}

	resp, err := liqPushClient.Do(req)
	if err != nil {
		log.Warnf("清算图快照推送失败 %s: %v", h.Symbol, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		log.Warnf("清算图快照推送被拒 %s: HTTP %d %s",
			h.Symbol, resp.StatusCode, utils.Truncate(buf.String(), maxErrBody))
		return
	}
	log.Infof("清算图快照已推送 %s（%d 个非零格，%s）", h.Symbol, len(snap.Cells), fmt.Sprintf("%.1fKB", float64(len(body))/1024))
}
