package stocks

import (
	"bytes"
	"testing"
)

// pngMagic 是 PNG 文件头，用于校验渲染确实输出了 PNG。
var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

func TestParseLiqResponse(t *testing.T) {
	// 正常：2 价位 × 2 时间
	ok := `[{"success":true,"chartInterval":"15m","liqHeatMap":{
		"data":[["0","0","0"],["1","0","100"],["0","1","250"],["1","1","0"]],
		"chartTimeArray":[1,2],"priceArray":[60000,60050],"maxLiqValue":250}}]`
	r, err := parseLiqResponse([]byte(ok))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h, err := buildHeatmap("BTC", r)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(h.Prices) != 2 || len(h.Times) != 2 {
		t.Fatalf("网格尺寸错: P=%d T=%d", len(h.Prices), len(h.Times))
	}
	// cell 是 [x, y, v]：["1","0","100"]→values[0][1]=100；["0","1","250"]→values[1][0]=250
	if h.Values[0][1] != 100 || h.Values[1][0] != 250 {
		t.Fatalf("网格取值错: %v", h.Values)
	}

	// 配额用尽：success:true 但无数据 → 报错并带 message
	limit := `[{"success":true,"message":"daily limit reached","liqHeatMap":{"data":[],"chartTimeArray":[],"priceArray":[],"maxLiqValue":0}}]`
	if _, err := parseLiqResponse([]byte(limit)); err == nil {
		t.Fatalf("空数据应报错")
	}
}

func TestToMapSplitsByCurrentPrice(t *testing.T) {
	h := &LiqHeatmap{
		Symbol: "BTC", Interval: "15m", CurrentPrice: 62000, MaxValue: 300,
		Prices: []float64{61000, 61500, 62500, 63000},
		Times:  []int64{1, 2},
		Values: [][]float64{
			{0, 100}, // 61000
			{0, 200}, // 61500
			{0, 300}, // 62500
			{0, 50},  // 63000
		},
	}
	m := h.ToMap()
	if len(m.Below) != 2 || len(m.Above) != 2 {
		t.Fatalf("上下拆分错: above=%d below=%d", len(m.Above), len(m.Below))
	}
	// 降序：below 最大应是 61500(200)
	if m.Below[0].Price != 61500 {
		t.Fatalf("below 未按额降序: %+v", m.Below)
	}
	if m.Above[0].Price != 62500 {
		t.Fatalf("above 未按额降序: %+v", m.Above)
	}
}

func TestRenderPNG(t *testing.T) {
	h := &LiqHeatmap{
		Symbol: "BTC", Interval: "15m", CurrentPrice: 62000, MaxValue: 300,
		Prices: []float64{61000, 61500, 62000, 62500, 63000},
		Times:  []int64{1, 2, 3},
		Values: [][]float64{
			{10, 50, 100},
			{0, 200, 20},
			{5, 5, 5},
			{300, 100, 60},
			{0, 0, 40},
		},
	}
	hb, err := RenderLiqHeatmapPNG(h)
	if err != nil || !bytes.HasPrefix(hb, pngMagic) || len(hb) < 100 {
		t.Fatalf("热力图 PNG 无效: err=%v len=%d", err, len(hb))
	}
	mb, err := RenderLiqMapPNG(h.ToMap())
	if err != nil || !bytes.HasPrefix(mb, pngMagic) || len(mb) < 100 {
		t.Fatalf("清算地图 PNG 无效: err=%v len=%d", err, len(mb))
	}
}
