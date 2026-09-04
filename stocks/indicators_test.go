package stocks

import (
	"math"
	"testing"
)

// closes 用收盘价序列造 K 线（高低价按收盘价 ±0 处理，测 SMA/RSI 够用）。
func closes(vs ...float64) []Candle {
	cs := make([]Candle, len(vs))
	for i, v := range vs {
		cs[i] = Candle{Ts: int64(i) * 86400000, O: v, H: v, L: v, C: v}
	}
	return cs
}

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.4f, want %.4f (±%.4f)", name, got, want, tol)
	}
}

func TestSMA(t *testing.T) {
	cs := closes(1, 2, 3, 4, 5)
	if v, ok := SMA(cs, 5); !ok || v != 3 {
		t.Errorf("SMA(5) = %v %v, want 3 true", v, ok)
	}
	// 只取最后 period 根
	if v, ok := SMA(cs, 2); !ok || v != 4.5 {
		t.Errorf("SMA(2) = %v %v, want 4.5 true", v, ok)
	}
	if _, ok := SMA(cs, 6); ok {
		t.Error("数据不足应返回 ok=false")
	}
	if _, ok := SMA(cs, 0); ok {
		t.Error("period=0 应返回 ok=false")
	}
}

// Wilder《New Concepts in Technical Trading Systems》里的标准 14 期 RSI 样例，
// 公认结果约 70.53。用它验证 Wilder 平滑实现正确（而不是简单平均）。
func TestRSI_WilderReference(t *testing.T) {
	cs := closes(44.34, 44.09, 44.15, 43.61, 44.33, 44.83, 45.10, 45.42,
		45.84, 46.08, 45.89, 46.03, 45.61, 46.28, 46.28)
	v, ok := RSI(cs, 14)
	if !ok {
		t.Fatal("15 根 K 线算 RSI(14) 应当成功")
	}
	near(t, "RSI(14)", v, 70.53, 0.6)
}

func TestRSI_Extremes(t *testing.T) {
	// 单调上涨：无下跌，RSI 定义为 100
	up := closes(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	if v, _ := RSI(up, 14); v != 100 {
		t.Errorf("单调上涨 RSI = %v, want 100", v)
	}
	// 单调下跌：RSI = 0
	down := closes(15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1)
	if v, _ := RSI(down, 14); v != 0 {
		t.Errorf("单调下跌 RSI = %v, want 0", v)
	}
	// 完全不动：给中性 50，不能除零
	flat := closes(5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5)
	if v, ok := RSI(flat, 14); !ok || v != 50 {
		t.Errorf("横盘 RSI = %v %v, want 50 true", v, ok)
	}
}

func TestRSI_InsufficientData(t *testing.T) {
	if _, ok := RSI(closes(1, 2, 3), 14); ok {
		t.Error("数据不足应返回 ok=false")
	}
	// 恰好 period+1 根应当可算
	cs := closes(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	if _, ok := RSI(cs, 14); !ok {
		t.Error("period+1 根应当够")
	}
}

func TestATR(t *testing.T) {
	// 每根 K 线 高-低 = 2，且无跳空，则 TR 恒为 2，ATR 也该是 2
	cs := make([]Candle, 20)
	for i := range cs {
		cs[i] = Candle{O: 10, H: 11, L: 9, C: 10}
	}
	v, ok := ATR(cs, 14)
	if !ok {
		t.Fatal("20 根应当够算 ATR(14)")
	}
	near(t, "ATR 恒定波幅", v, 2, 1e-9)

	if _, ok := ATR(closes(1, 2, 3), 14); ok {
		t.Error("数据不足应返回 ok=false")
	}
}

// TR 要考虑跳空：跳空日的真实波幅应大于当日高低差。
func TestATR_CountsGaps(t *testing.T) {
	noGap := make([]Candle, 20)
	for i := range noGap {
		noGap[i] = Candle{O: 100, H: 101, L: 99, C: 100}
	}
	withGap := append([]Candle(nil), noGap...)
	// 最后一根整体跳空到 120，高低差仍是 2，但相对前收 100 的真实波幅是 21
	withGap[19] = Candle{O: 119, H: 121, L: 119, C: 120}

	a, _ := ATR(noGap, 14)
	b, _ := ATR(withGap, 14)
	if b <= a {
		t.Errorf("跳空后 ATR 应变大: 无跳空 %.4f, 有跳空 %.4f", a, b)
	}
}

func TestPercentile(t *testing.T) {
	s := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if v, _ := Percentile(5.5, s); v != 50 {
		t.Errorf("中位 = %v, want 50", v)
	}
	if v, _ := Percentile(0, s); v != 0 {
		t.Errorf("最小 = %v, want 0", v)
	}
	if v, _ := Percentile(100, s); v != 100 {
		t.Errorf("最大 = %v, want 100", v)
	}
	if _, ok := Percentile(1, nil); ok {
		t.Error("空样本应返回 ok=false")
	}
	// 不能改动调用方的切片
	orig := []float64{3, 1, 2}
	Percentile(2, orig)
	if orig[0] != 3 {
		t.Errorf("Percentile 修改了入参切片: %v", orig)
	}
}

// 证伪条件离现价太近就是在测噪声，不是在测判断。
func TestFalsifyDistanceOK(t *testing.T) {
	const price, atr = 80000, 2400.0

	// 距离 800 ≈ 0.33×ATR，太近
	ok, ratio := FalsifyDistanceOK(price, price-800, atr)
	if ok {
		t.Errorf("0.33×ATR 应判定太近，ratio=%.2f", ratio)
	}
	near(t, "ratio", ratio, 0.333, 0.01)

	// 距离 4000 ≈ 1.67×ATR，够远
	if ok, _ := FalsifyDistanceOK(price, price+4000, atr); !ok {
		t.Error("1.67×ATR 应判定足够远")
	}

	// 拿不到 ATR 时不拦人
	if ok, _ := FalsifyDistanceOK(price, price+1, 0); !ok {
		t.Error("ATR 不可用时应放行")
	}
	if ok, _ := FalsifyDistanceOK(0, 100, atr); !ok {
		t.Error("价格不可用时应放行")
	}
}
