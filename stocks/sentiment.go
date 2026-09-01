package stocks

import (
	"errors"
	"strconv"
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
)

// FearGreed 是加密市场「恐惧贪婪指数」的一条快照（0-100，越低越恐惧）。
// 常用作反向情绪指标：极度恐惧往往接近底部，极度贪婪往往接近顶部。
type FearGreed struct {
	Value          int
	Classification string // 原始英文分类，如 "Extreme Fear"
}

// Emoji 按数值返回情绪 emoji。
func (f *FearGreed) Emoji() string {
	switch {
	case f.Value < 25:
		return "😱"
	case f.Value < 50:
		return "😨"
	case f.Value < 55:
		return "😐"
	case f.Value < 75:
		return "🤑"
	default:
		return "🥳"
	}
}

// LabelCN 返回中文情绪分类。
func (f *FearGreed) LabelCN() string {
	switch f.Classification {
	case "Extreme Fear":
		return "极度恐惧"
	case "Fear":
		return "恐惧"
	case "Neutral":
		return "中性"
	case "Greed":
		return "贪婪"
	case "Extreme Greed":
		return "极度贪婪"
	default:
		return f.Classification
	}
}

// fngResponse 对应 alternative.me /fng 接口：{"data":[{"value":"51","value_classification":"Neutral"}]}
type fngResponse struct {
	Data []struct {
		Value          string `json:"value"`
		ValueClassText string `json:"value_classification"`
	} `json:"data"`
}

// fngCache 缓存恐惧贪婪指数：该指数一天只更新一次，缓存 10 分钟毫无信息损失，
// 却能挡掉 inline 打字期间的高频重复请求。
var fngCache = newTTLCache[*FearGreed](10 * time.Minute)

// GetFearGreed 拉取最新的加密恐惧贪婪指数（alternative.me，免费免鉴权）。
// 属于「best-effort」增强项：失败时上层可忽略、不影响行情播报。
func GetFearGreed() (*FearGreed, error) {
	if v, ok := fngCache.get("fng"); ok {
		return v, nil
	}

	var fng fngResponse
	if err := fetchJSON(marketClient, "恐惧贪婪指数", "https://api.alternative.me/fng/?limit=1", &fng); err != nil {
		log.Warnf("%v", err)
		return nil, err
	}
	if len(fng.Data) == 0 {
		log.Warnf("恐惧贪婪指数返回为空")
		return nil, errors.New("恐惧贪婪指数返回为空")
	}

	v, err := strconv.Atoi(fng.Data[0].Value)
	if err != nil {
		log.Warnf("恐惧贪婪指数值非法 %q: %v", fng.Data[0].Value, err)
		return nil, err
	}

	res := &FearGreed{Value: v, Classification: fng.Data[0].ValueClassText}
	fngCache.set("fng", res)
	return res, nil
}
