package stocks

import (
	"math"
	"sort"
)

// 技术指标。全是纯函数：输入 K 线，输出数值，不碰网络，便于测试。
//
// 只收录**互相不重复**的指标。RSI / MACD / Stochastic / Williams %R 都是从
// OHLCV 推出来的动量，彼此高度相关，全加一遍等于把同一个视角看了五遍。
// 动量留 RSI 一个，其余的名额留给真正换了数据维度的东西（basis、OI、清算簇）。

// SMA 简单移动平均，取最后 period 根的收盘价。
func SMA(cs []Candle, period int) (float64, bool) {
	if period <= 0 || len(cs) < period {
		return 0, false
	}
	sum := 0.0
	for _, c := range cs[len(cs)-period:] {
		sum += c.C
	}
	return sum / float64(period), true
}

// RSI 相对强弱指标，Wilder 平滑（教科书标准算法，与 TradingView 一致）。
//
// 至少需要 period+1 根 K 线：第一根只用来算首个涨跌幅。
func RSI(cs []Candle, period int) (float64, bool) {
	if period <= 0 || len(cs) < period+1 {
		return 0, false
	}

	// 首个平均涨跌幅用简单平均
	var gain, loss float64
	for i := 1; i <= period; i++ {
		ch := cs[i].C - cs[i-1].C
		if ch > 0 {
			gain += ch
		} else {
			loss -= ch
		}
	}
	avgGain, avgLoss := gain/float64(period), loss/float64(period)

	// 之后用 Wilder 平滑递推
	p := float64(period)
	for i := period + 1; i < len(cs); i++ {
		ch := cs[i].C - cs[i-1].C
		g, l := 0.0, 0.0
		if ch > 0 {
			g = ch
		} else {
			l = -ch
		}
		avgGain = (avgGain*(p-1) + g) / p
		avgLoss = (avgLoss*(p-1) + l) / p
	}

	// 全程无下跌：RSI 定义为 100
	if avgLoss == 0 {
		if avgGain == 0 {
			return 50, true // 完全没波动，给中性值
		}
		return 100, true
	}
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs), true
}

// ATR 真实波幅均值，Wilder 平滑。衡量"噪声有多大"。
//
// 这个指标在本项目里有个特殊用途：判断一条 claim 的证伪条件是否离现价太近。
// 证伪线设在 0.3×ATR 以内的话，它一天之内就会被随机波动触发，
// 那条 claim 测的是噪声而不是判断。见 FalsifyDistanceOK。
func ATR(cs []Candle, period int) (float64, bool) {
	if period <= 0 || len(cs) < period+1 {
		return 0, false
	}

	trueRange := func(cur, prev Candle) float64 {
		hl := cur.H - cur.L
		hc := math.Abs(cur.H - prev.C)
		lc := math.Abs(cur.L - prev.C)
		return math.Max(hl, math.Max(hc, lc))
	}

	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += trueRange(cs[i], cs[i-1])
	}
	atr := sum / float64(period)

	p := float64(period)
	for i := period + 1; i < len(cs); i++ {
		atr = (atr*(p-1) + trueRange(cs[i], cs[i-1])) / p
	}
	return atr, true
}

// Percentile 返回 v 在 sample 中的百分位（0~100）：sample 里有多大比例严格小于 v。
//
// 用于资金费率：「费率 0.01%」是个没有参照的数字，
// 「过去 30 天的第 95 百分位」才可比、可证伪。
func Percentile(v float64, sample []float64) (float64, bool) {
	if len(sample) == 0 {
		return 0, false
	}
	s := append([]float64(nil), sample...)
	sort.Float64s(s)
	below := 0
	for _, x := range s {
		if x < v {
			below++
		}
	}
	return float64(below) / float64(len(s)) * 100, true
}

// minFalsifyATRRatio 是证伪条件与现价的最小距离，单位为 ATR 倍数。
// 低于这个值就该提醒用户：这个条件测的是噪声。
const minFalsifyATRRatio = 0.5

// FalsifyDistanceOK 判断某个证伪价位离现价是否足够远（超过 minFalsifyATRRatio 倍 ATR）。
// atr <= 0（拿不到）时一律返回 true，不因为缺数据就拦人记录判断。
func FalsifyDistanceOK(price, falsifyPrice, atr float64) (ok bool, ratio float64) {
	if atr <= 0 || price <= 0 {
		return true, 0
	}
	ratio = math.Abs(falsifyPrice-price) / atr
	return ratio >= minFalsifyATRRatio, ratio
}
