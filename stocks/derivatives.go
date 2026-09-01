package stocks

import (
	"fmt"
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
	OpenInterestUSD float64       // 未平仓名义价值（USD）
	LongShortRatio  float64       // 全市场多空账户比，>1 偏多
	FundingRate     float64       // 资金费率（如 0.0001=0.01%），正=多头付费
	FundingKnown    bool          // 是否取到资金费率
	OIChangePct     float64       // 相比上次观测的持仓量变化百分比
	OIChangeKnown   bool          // 是否有上次观测可比
	OISpan          time.Duration // 距上次观测的时间跨度（用于在文案里标明变化的时间口径）
}

// OIScope 区分持仓量基线的用途。
//
// 播报和用户查询必须各记一份基线：两者共用一份的话，只要有人在 10:30 敲过一次
// /price btc，11:00 播报里的「持仓 +1.2%」就变成了「距 10:30 的变化」而非小时环比，
// 而文案上完全看不出来——一个无法证伪的数字。
type OIScope string

const (
	OIScopeBroadcast OIScope = "bcast" // 整点播报
	OIScopeQuery     OIScope = "query" // /price、inline 等用户主动查询
)

// oiObservation 是一次持仓量观测（值 + 时刻），用于算变化幅度及其时间跨度。
type oiObservation struct {
	val float64
	at  time.Time
}

// derivClient 衍生品接口用较短超时：拿不到数据时不拖慢行情主体。
var derivClient = &http.Client{Timeout: 6 * time.Second}

// lastOI 记录各 (scope, 币种) 上一次观测到的持仓量，用于算持仓变化。
// 进程内状态，重启后重新累积——首次观测没有可比基线，会省略变化部分。
var (
	lastOIMu sync.Mutex
	lastOI   = map[string]oiObservation{}
)

// fetchDerivatives 拉取某币种永续合约的持仓量与多空比。
// symbol 形如 "BTC"，内部拼成 OKX 的 instId "BTC-USDT-SWAP" / ccy "BTC"。
// scope 决定用哪一份持仓量基线（见 OIScope）。
// best-effort：各子项失败互不影响；全失败返回 nil（上层据此省略衍生品行）。
func fetchDerivatives(symbol string, scope OIScope) *Derivatives {
	ccy := strings.ToUpper(symbol)
	d := &Derivatives{}
	got := false

	if oi, err := fetchOpenInterestUSD(ccy); err != nil {
		log.Warnf("持仓量获取失败 %s: %v", ccy, err)
	} else {
		d.OpenInterestUSD = oi
		// 与本 scope 的上次观测比较，算持仓变化百分比及其时间跨度。
		now := time.Now()
		key := string(scope) + ":" + ccy
		lastOIMu.Lock()
		if prev, ok := lastOI[key]; ok && prev.val > 0 {
			d.OIChangePct = (oi - prev.val) / prev.val * 100
			d.OIChangeKnown = true
			d.OISpan = now.Sub(prev.at)
		}
		lastOI[key] = oiObservation{val: oi, at: now}
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

// getJSON 以 derivClient（短超时）GET 一个 URL 并反序列化到 v。
func getJSON(url string, v any) error {
	return fetchJSON(derivClient, "okx", url, v)
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
