package stocks

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
)

// 关注的币种：CoinGecko id -> 展示名。顺序固定，便于稳定排版。
var cryptoCoins = []struct {
	ID   string
	Name string
}{
	{"bitcoin", "BTC"},
	{"ethereum", "ETH"},
}

// CryptoQuote 单个币种的行情。
type CryptoQuote struct {
	Name          string
	Price         float64
	ChangePercent float64
}

// coingeckoPrice 对应 simple/price 接口的返回值：{"bitcoin":{"usd":..,"usd_24h_change":..}}
type coingeckoPrice struct {
	USD       float64 `json:"usd"`
	USD24hChg float64 `json:"usd_24h_change"`
}

// GetCryptoQuotes 拉取关注币种的最新价与 24h 涨跌幅。
// 使用 CoinGecko 公共接口，无需鉴权，且不受 Binance 的地区限制影响。
func GetCryptoQuotes() ([]CryptoQuote, error) {
	url := "https://api.coingecko.com/api/v3/simple/price" +
		"?ids=bitcoin,ethereum&vs_currencies=usd&include_24hr_change=true"

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Errorf("crypto 行情请求失败: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("crypto 行情读取失败: %v", err)
		return nil, err
	}

	var raw map[string]coingeckoPrice
	if err := json.Unmarshal(body, &raw); err != nil {
		log.Errorf("crypto 行情解析失败: %v, body: %s", err, string(body))
		return nil, err
	}

	quotes := make([]CryptoQuote, 0, len(cryptoCoins))
	for _, coin := range cryptoCoins {
		if p, ok := raw[coin.ID]; ok {
			quotes = append(quotes, CryptoQuote{
				Name:          coin.Name,
				Price:         p.USD,
				ChangePercent: p.USD24hChg,
			})
		}
	}
	return quotes, nil
}

// FormatCryptoMessage 把行情整理成一条可读的播报文案。
func FormatCryptoMessage(quotes []CryptoQuote) string {
	msg := "📊 币圈行情播报\n"
	for _, q := range quotes {
		emoji := "🔼"
		if q.ChangePercent < 0 {
			emoji = "🔽"
		}
		msg += fmt.Sprintf("%s  $%s  %s%.2f%%\n", q.Name, formatPrice(q.Price), emoji, q.ChangePercent)
	}
	return msg
}

// formatPrice 按千分位格式化价格，保留两位小数。
func formatPrice(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	dot := len(s) - 3 // 小数点位置（".xx"）
	intPart := s[:dot]
	decPart := s[dot:]

	// 处理负号，避免把 '-' 也参与千分位分组
	sign := ""
	if len(intPart) > 0 && intPart[0] == '-' {
		sign = "-"
		intPart = intPart[1:]
	}

	var out []byte
	for i := 0; i < len(intPart); i++ {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, intPart[i])
	}
	return sign + string(out) + decPart
}
