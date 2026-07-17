package stocks

import (
	"fmt"
	"strconv"
	"strings"
)

// Candle 是一根 K线（OHLC）。
type Candle struct {
	Ts         int64 // 开盘时间戳(ms)
	O, H, L, C float64
}

// GetKlines 拉取某币种 OKX 永续 K线（升序返回）。bar 如 "15m"/"1H"，limit≤300。best-effort。
func GetKlines(symbol, bar string, limit int) ([]Candle, error) {
	var env okxEnvelope[[][]string]
	url := "https://www.okx.com/api/v5/market/candles?instId=" +
		strings.ToUpper(symbol) + "-USDT-SWAP&bar=" + bar + "&limit=" + strconv.Itoa(limit)
	if err := getJSON(url, &env); err != nil {
		return nil, err
	}
	if env.Code != "0" {
		return nil, fmt.Errorf("okx candles code=%s", env.Code)
	}
	out := make([]Candle, 0, len(env.Data))
	for _, r := range env.Data {
		if len(r) < 5 {
			continue
		}
		ts, _ := strconv.ParseInt(r[0], 10, 64)
		o, _ := strconv.ParseFloat(r[1], 64)
		h, _ := strconv.ParseFloat(r[2], 64)
		l, _ := strconv.ParseFloat(r[3], 64)
		c, _ := strconv.ParseFloat(r[4], 64)
		out = append(out, Candle{Ts: ts, O: o, H: h, L: l, C: c})
	}
	// OKX 返回按时间倒序（新→旧），反转成升序。
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// klineBar 把热力图时间粒度映射成 OKX 的 bar（小时用大写 H）。
func klineBar(interval string) string {
	if interval == "" {
		return "15m"
	}
	if strings.HasSuffix(interval, "h") {
		return strings.TrimSuffix(interval, "h") + "H"
	}
	return interval
}
