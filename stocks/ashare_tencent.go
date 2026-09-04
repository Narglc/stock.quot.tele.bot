package stocks

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// 腾讯行情作为 A 股的独立备用源。
//
// 为什么值得单独写一个解析器：东财的两个主机共用一套后端，一起挂过（生产上遇到
// ulist.np 间歇性 502）。腾讯是完全独立的来源，且实测连续可用。
// 代价是它返回 GBK 编码的 `~` 分隔字符串，得自己转码按位解析。

// 腾讯响应的字段下标（`~` 分隔）。这些位置是固定的，但没有官方文档，
// 所以每个常量都标明含义，改动前先用真实响应核对。
const (
	txName      = 1
	txCode      = 2
	txPrice     = 3
	txPrevClose = 4
	txOpen      = 5
	txChange    = 31
	txChangePct = 32
	txHigh      = 33
	txLow       = 34
	txVolume    = 36 // 手
	txAmount    = 37 // 万元
	txTurnover  = 38 // 换手率 %
	txMinFields = 39
)

// tencentCode 把 6 位代码转成腾讯的 sh/sz 前缀形式。规则与东财 secid 一致。
func tencentCode(code string) string {
	switch code[0] {
	case '6', '5', '9':
		return "sh" + code
	default:
		return "sz" + code
	}
}

// fetchTencent 从腾讯拉 A 股行情。
func fetchTencent(codes []string) ([]StockQuote, error) {
	ids := make([]string, 0, len(codes))
	for _, c := range codes {
		ids = append(ids, tencentCode(c))
	}

	resp, err := marketClient.Get("https://qt.gtimg.cn/q=" + strings.Join(ids, ","))
	if err != nil {
		return nil, fmt.Errorf("腾讯行情请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("腾讯行情 HTTP %d", resp.StatusCode)
	}

	// 腾讯返回 GBK，股票名称是中文，不转码会是乱码
	raw, err := io.ReadAll(transform.NewReader(io.LimitReader(resp.Body, 1<<20), simplifiedchinese.GBK.NewDecoder()))
	if err != nil {
		return nil, fmt.Errorf("腾讯行情转码失败: %w", err)
	}

	byCode := parseTencent(string(raw))
	out := make([]StockQuote, 0, len(codes))
	for _, c := range codes {
		if q, ok := byCode[c]; ok {
			out = append(out, q)
		}
	}
	return out, nil
}

// parseTencent 解析腾讯行情响应，返回 代码 -> 行情。与网络解耦，便于测试。
//
// 每行形如：v_sh600519="1~贵州茅台~600519~1330.00~...";
func parseTencent(body string) map[string]StockQuote {
	out := map[string]StockQuote{}
	for _, line := range strings.Split(body, "\n") {
		i, j := strings.Index(line, `"`), strings.LastIndex(line, `"`)
		if i < 0 || j <= i {
			continue
		}
		f := strings.Split(line[i+1:j], "~")
		if len(f) < txMinFields || f[txCode] == "" {
			continue
		}
		num := func(idx int) float64 {
			v, _ := strconv.ParseFloat(strings.TrimSpace(f[idx]), 64)
			return v
		}
		out[f[txCode]] = StockQuote{
			Code:         f[txCode],
			Name:         f[txName],
			Price:        num(txPrice),
			ChangePct:    num(txChangePct),
			Change:       num(txChange),
			PrevClose:    num(txPrevClose),
			Open:         num(txOpen),
			High:         num(txHigh),
			Low:          num(txLow),
			Volume:       num(txVolume),
			Amount:       num(txAmount) * 1e4, // 万元 → 元，与东财口径对齐
			TurnoverRate: num(txTurnover),
		}
	}
	return out
}
