package stocks

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func sampleHeatmap() *LiqHeatmap {
	return &LiqHeatmap{
		Symbol:       "BTC",
		Interval:     "15m",
		CurrentPrice: 65000,
		MaxValue:     900,
		Prices:       []float64{60000, 65000, 70000},
		Times:        []int64{1000, 2000},
		Values: [][]float64{
			{0, 100}, // price 60000
			{0, 0},   // price 65000（全零，不该出现在 cells 里）
			{900, 0}, // price 70000
		},
		Candles: []Candle{{Ts: 1000, O: 1, H: 2, L: 0.5, C: 1.5}},
	}
}

// 稀疏化是这个快照格式的关键：热力图绝大多数格子是 0，
// 稠密序列化能到几 MB，稀疏通常只有几十 KB。
func TestBuildSnapshot_SparseSkipsZeros(t *testing.T) {
	snap := buildSnapshot(sampleHeatmap())

	if len(snap.Cells) != 2 {
		t.Fatalf("应只保留 2 个非零格，实得 %d: %+v", len(snap.Cells), snap.Cells)
	}
	// cells 是 [时间索引, 价格索引, 值]
	want := map[[3]float64]bool{{1, 0, 100}: true, {0, 2, 900}: true}
	for _, c := range snap.Cells {
		if !want[c] {
			t.Errorf("出现了预期外的格子: %+v", c)
		}
	}
	if snap.Symbol != "BTC" || snap.CurrentPrice != 65000 || snap.Interval != "15m" {
		t.Errorf("元信息丢失: %+v", snap)
	}
	if len(snap.Candles) != 1 || snap.Candles[0].C != 1.5 {
		t.Errorf("K线未正确转换: %+v", snap.Candles)
	}
}

func TestPushHeatmapSnapshot_SendsPutWithToken(t *testing.T) {
	type captured struct {
		method, path, token string
		body                []byte
	}
	got := make(chan captured, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- captured{r.Method, r.URL.Path, r.Header.Get("X-Auth-Token"), b}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	SetLiqPushTarget(srv.URL, "secret123")
	defer SetLiqPushTarget("", "")

	PushHeatmapSnapshot(sampleHeatmap())
	c := <-got

	if c.method != http.MethodPut {
		t.Errorf("应当用 PUT，实得 %s", c.method)
	}
	if c.path != "/api/heatmap/BTC" {
		t.Errorf("路径应含币种，实得 %s", c.path)
	}
	if c.token != "secret123" {
		t.Errorf("应带鉴权头，实得 %q", c.token)
	}

	var snap liqSnapshot
	if err := json.Unmarshal(c.body, &snap); err != nil {
		t.Fatalf("推送的不是合法 JSON: %v", err)
	}
	if len(snap.Cells) != 2 {
		t.Errorf("推送内容应为稀疏格式，实得 %d 格", len(snap.Cells))
	}
}

// 未配置端点时必须静默跳过——大多数部署不会开这个网页。
func TestPushHeatmapSnapshot_NoopWhenUnconfigured(t *testing.T) {
	SetLiqPushTarget("", "")
	PushHeatmapSnapshot(sampleHeatmap()) // 不该 panic，也不该发请求
	PushHeatmapSnapshot(nil)
}
