package stocks

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
)

// Derivatives 是某币种 USDT 永续合约的衍生品指标（OKX，best-effort）。
// 选 OKX 而非 Binance：Binance 的 fapi 对不少云厂商 IP 返回 451（地区限制），
// OKX 在这些环境通常可达，且 open-interest 接口直接给出美元计持仓量。
type Derivatives struct {
	OpenInterestUSD float64 // 未平仓名义价值（USD）
	LongShortRatio  float64 // 全市场多空账户比，>1 偏多
	FundingRate     float64 // 资金费率（如 0.0001=0.01%），正=多头付费
	FundingKnown    bool    // 是否取到资金费率
	OIChangePct     float64 // 相比上次观测的持仓量变化百分比
	OIChangeKnown   bool    // 是否有上次观测可比
}

// derivClient 衍生品接口用较短超时：拿不到数据时不拖慢行情主体。
var derivClient = &http.Client{Timeout: 6 * time.Second}

// lastOI 记录各币种上一次观测到的持仓量，用于算持仓变化（进程内，重启后重新累积）。
var (
	lastOIMu sync.Mutex
	lastOI   = map[string]float64{}
)

// fetchDerivatives 拉取某币种永续合约的持仓量与多空比。
// symbol 形如 "BTC"，内部拼成 OKX 的 instId "BTC-USDT-SWAP" / ccy "BTC"。
// best-effort：两个子项各自失败互不影响；全失败返回 nil（上层据此省略衍生品行）。
func fetchDerivatives(symbol string, _ float64) *Derivatives {
	ccy := strings.ToUpper(symbol)
	d := &Derivatives{}
	got := false

	if oi, err := fetchOpenInterestUSD(ccy); err != nil {
		log.Warnf("持仓量获取失败 %s: %v", ccy, err)
	} else {
		d.OpenInterestUSD = oi
		// 与上次观测比较，算持仓变化百分比。
		lastOIMu.Lock()
		if prev, ok := lastOI[ccy]; ok && prev > 0 {
			d.OIChangePct = (oi - prev) / prev * 100
			d.OIChangeKnown = true
		}
		lastOI[ccy] = oi
		lastOIMu.Unlock()
		got = true
	}

	if lsr, err := fetchLongShortRatio(ccy); err != nil {
		log.Warnf("多空比获取失败 %s: %v", ccy, err)
	} else {
		d.LongShortRatio = lsr
		got = true
	}

	if fr, err := fetchFundingRate(ccy); err != nil {
		log.Warnf("资金费率获取失败 %s: %v", ccy, err)
	} else {
		d.FundingRate = fr
		d.FundingKnown = true
		got = true
	}

	if !got {
		return nil
	}
	return d
}

// fetchFundingRate 拉取某永续合约当前资金费率（如 0.0001 表示 0.01%）。
func fetchFundingRate(ccy string) (float64, error) {
	var env okxEnvelope[[]struct {
		FundingRate string `json:"fundingRate"`
	}]
	url := "https://www.okx.com/api/v5/public/funding-rate?instId=" + ccy + "-USDT-SWAP"
	if err := getJSON(url, &env); err != nil {
		return 0, err
	}
	if env.Code != "0" || len(env.Data) == 0 {
		return 0, fmt.Errorf("okx funding code=%s msg=%q", env.Code, env.Msg)
	}
	return strconv.ParseFloat(env.Data[0].FundingRate, 64)
}

// okxEnvelope 是 OKX v5 接口的统一外层：code="0" 表示成功。
type okxEnvelope[T any] struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

// getJSON 以 derivClient GET 一个 URL 并反序列化到 v。
func getJSON(url string, v any) error {
	resp, err := derivClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

// fetchOpenInterestUSD 拉取某永续合约当前未平仓合约的美元名义价值。
func fetchOpenInterestUSD(ccy string) (float64, error) {
	var env okxEnvelope[[]struct {
		OiUsd string `json:"oiUsd"`
	}]
	url := "https://www.okx.com/api/v5/public/open-interest?instType=SWAP&instId=" + ccy + "-USDT-SWAP"
	if err := getJSON(url, &env); err != nil {
		return 0, err
	}
	if env.Code != "0" || len(env.Data) == 0 {
		return 0, fmt.Errorf("okx oi code=%s msg=%q", env.Code, env.Msg)
	}
	return strconv.ParseFloat(env.Data[0].OiUsd, 64)
}

// fetchLongShortRatio 拉取某币种最新的多空账户比。
// OKX 返回按时间倒序的 [ts, ratio] 数组，取首元素即最新。
func fetchLongShortRatio(ccy string) (float64, error) {
	var env okxEnvelope[[][]string]
	url := "https://www.okx.com/api/v5/rubik/stat/contracts/long-short-account-ratio?ccy=" + ccy + "&period=5m"
	if err := getJSON(url, &env); err != nil {
		return 0, err
	}
	if env.Code != "0" || len(env.Data) == 0 || len(env.Data[0]) < 2 {
		return 0, fmt.Errorf("okx lsr code=%s msg=%q", env.Code, env.Msg)
	}
	return strconv.ParseFloat(env.Data[0][1], 64)
}
