package stocks

import (
	"strings"
	"testing"
	"time"
)

func TestBrief_Derive(t *testing.T) {
	b := &MarketBrief{Price: 80000, SpotPrice: 80100, MA200: 70000, ATR14: 2400}
	b.derive()

	near(t, "basis", b.BasisPct, -0.1248, 0.001) // 永续贴水
	near(t, "vs MA200", b.VsMA200Pct, 14.2857, 0.001)
	near(t, "ATR%", b.ATRPct, 3.0, 0.001)

	// 缺字段时不产出垃圾数字，也不 panic
	empty := &MarketBrief{}
	empty.derive()
	if empty.BasisPct != 0 || empty.VsMA200Pct != 0 || empty.ATRPct != 0 {
		t.Errorf("缺数据时不该算出派生值: %+v", empty)
	}
}

// 关键源失败 → 不健康；非关键源失败 → 记录原因但仍可用。
// 这是下游门禁的依据：health.ok=false 时不该在这份数据上下判断。
func TestBrief_Health(t *testing.T) {
	now := time.Now()

	ok := evalHealth([]SourceStatus{
		{Name: "okx_klines", OK: true, FetchedAt: now, Critical: true},
		{Name: "okx_derivs", OK: true, FetchedAt: now, Critical: true},
	})
	if !ok.OK || len(ok.Reasons) != 0 {
		t.Errorf("全部正常应 ok 且无原因: %+v", ok)
	}

	critFail := evalHealth([]SourceStatus{
		{Name: "okx_klines", OK: false, Err: "timeout", Critical: true},
	})
	if critFail.OK {
		t.Error("关键源失败应判定不健康")
	}
	if len(critFail.Reasons) == 0 || !strings.Contains(critFail.Reasons[0], "关键") {
		t.Errorf("原因应指明是关键源: %+v", critFail.Reasons)
	}

	minorFail := evalHealth([]SourceStatus{
		{Name: "okx_klines", OK: true, FetchedAt: now, Critical: true},
		{Name: "liqmap_cache", OK: false, Err: "无缓存"},
	})
	if !minorFail.OK {
		t.Error("非关键源失败不该判定不健康")
	}
	if len(minorFail.Reasons) != 1 {
		t.Errorf("非关键失败仍应记录原因: %+v", minorFail.Reasons)
	}
}

// 陈旧的关键数据同样要拦。
func TestBrief_HealthStaleness(t *testing.T) {
	stale := time.Now().Add(-3 * time.Hour) // okx_klines 的 maxAge 是 2h
	h := evalHealth([]SourceStatus{
		{Name: "okx_klines", OK: true, FetchedAt: stale, Critical: true},
	})
	if h.OK {
		t.Error("关键数据陈旧应判定不健康")
	}
	if len(h.Reasons) == 0 || !strings.Contains(h.Reasons[0], "陈旧") {
		t.Errorf("原因应说明陈旧: %+v", h.Reasons)
	}
}

func TestBrief_FillClusters_PicksNearestNotLargest(t *testing.T) {
	// 上方两个簇：82000 有 6e7（近），90000 有 5e8（大）。要的是最近的显著簇，不是最大的。
	h := &LiqHeatmap{
		Symbol: "TESTBRIEF", CurrentPrice: 80000, Times: []int64{time.Now().UnixMilli()},
		Prices: []float64{70000, 82000, 90000},
		Values: [][]float64{{3e8}, {6e7}, {5e8}},
	}
	liqCache.set("TESTBRIEF", h)
	defer liqCache.set("TESTBRIEF", nil)

	b := &MarketBrief{Symbol: "TESTBRIEF", Price: 80000, ATR14: 2000}
	b.fillClusters(func(string, error) {})

	if b.NearestAbove == nil {
		t.Fatal("应找到上方簇")
	}
	if b.NearestAbove.Price != 82000 {
		t.Errorf("应取最近的显著簇 82000，实得 %v", b.NearestAbove.Price)
	}
	near(t, "距离%", b.NearestAbove.DistPct, 2.5, 0.01)
	near(t, "距离ATR倍数", b.NearestAbove.DistATR, 1.0, 0.01)

	if b.NearestBelow == nil || b.NearestBelow.Price != 70000 {
		t.Errorf("下方簇应为 70000，实得 %+v", b.NearestBelow)
	}
}

// 小于门槛的簇不算「显著」，扫掉也没意义。
func TestBrief_FillClusters_SkipsTinyClusters(t *testing.T) {
	h := &LiqHeatmap{
		Symbol: "TINY", CurrentPrice: 80000, Times: []int64{time.Now().UnixMilli()},
		Prices: []float64{81000, 85000},
		Values: [][]float64{{1e6}, {2e8}}, // 81000 太小，85000 才够格
	}
	liqCache.set("TINY", h)
	defer liqCache.set("TINY", nil)

	b := &MarketBrief{Symbol: "TINY", Price: 80000}
	b.fillClusters(func(string, error) {})
	if b.NearestAbove == nil || b.NearestAbove.Price != 85000 {
		t.Errorf("应跳过小簇取 85000，实得 %+v", b.NearestAbove)
	}
}

// brief 组装绝不能触发 Apify —— 一天 24 次会瞬间打爆免费档的 5 次配额。
func TestBrief_ClustersNeverTriggerApify(t *testing.T) {
	liqCache = newTTLCache[*LiqHeatmap](10 * time.Minute) // 清空
	SetLiqPushTarget("", "")

	var notes []string
	b := &MarketBrief{Symbol: "NOCACHE", Price: 80000}
	b.fillClusters(func(name string, err error) {
		if err != nil {
			notes = append(notes, name)
		}
	})
	if b.NearestAbove != nil || b.NearestBelow != nil {
		t.Error("无缓存时应留空而不是去拉数据")
	}
	if len(notes) != 1 || notes[0] != "liqmap_cache" {
		t.Errorf("应记录 liqmap_cache 缺失，实得 %v", notes)
	}
}

func TestFormatBrief_MarksDegradedData(t *testing.T) {
	b := &MarketBrief{
		Symbol: "BTC", Price: 80000, RSI14: 55,
		Health: Health{OK: false, Reasons: []string{"关键数据源 okx_klines 失败: timeout"}},
	}
	out := FormatBrief(b)
	if !strings.Contains(out, "数据不完整") {
		t.Errorf("不健康时必须显式标注，实得:\n%s", out)
	}
}
