package stocks

import (
	"fmt"
	"strings"
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
)

// 宏观数据两个来源，分工不同：
//
//   - ForexFactory 免费日历：**什么时候有什么事件**，带预测值和前值。
//     注意它的免费 feed 里**没有 actual 字段**，事件公布后也不会回填实际值。
//   - BLS 官方 API：**已公布的实际值**（失业率、非农就业人数）。免费、无需 key。
//
// 所以「非农预测 55K」来自前者，「7 月非农实际 158,858K」来自后者，别搞混。

// MacroEvent 是一条宏观日历事件。
type MacroEvent struct {
	Title    string    `json:"title"`
	Country  string    `json:"country"`
	Impact   string    `json:"impact"` // High / Medium / Low
	At       time.Time `json:"at"`
	Forecast string    `json:"forecast,omitempty"`
	Previous string    `json:"previous,omitempty"`
}

// ffEvent 对应 ForexFactory 周历 JSON 的单条。
type ffEvent struct {
	Title    string `json:"title"`
	Country  string `json:"country"`
	Date     string `json:"date"`
	Impact   string `json:"impact"`
	Forecast string `json:"forecast"`
	Previous string `json:"previous"`
}

// macroCache 缓存日历：周历一天变不了几次，1 小时足够。
var macroCache = newTTLCache[[]MacroEvent](time.Hour)

// GetMacroCalendar 拉本周宏观日历，只保留指定重要度及以上、且尚未过去太久的事件。
// countries 为空表示不过滤国家。
func GetMacroCalendar(minImpact string, countries []string) ([]MacroEvent, error) {
	const key = "ff:thisweek"
	if v, ok := macroCache.get(key); ok {
		return filterEvents(v, minImpact, countries), nil
	}

	var raw []ffEvent
	if err := fetchJSON(marketClient, "宏观日历", "https://nfs.faireconomy.media/ff_calendar_thisweek.json", &raw); err != nil {
		log.Warnf("%v", err)
		return nil, err
	}

	out := make([]MacroEvent, 0, len(raw))
	for _, e := range raw {
		at, err := parseFFDate(e.Date)
		if err != nil {
			continue // 单条时间格式异常不该毁掉整个日历
		}
		out = append(out, MacroEvent{
			Title: e.Title, Country: e.Country, Impact: e.Impact,
			At: at, Forecast: e.Forecast, Previous: e.Previous,
		})
	}
	macroCache.set(key, out)
	return filterEvents(out, minImpact, countries), nil
}

// parseFFDate 解析 ForexFactory 的时间戳（带时区偏移的 RFC3339 变体）。
func parseFFDate(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05-07:00", "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析时间: %q", s)
}

// impactRank 把重要度转成可比较的数值。
func impactRank(s string) int {
	switch strings.ToLower(s) {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	}
	return 0
}

// filterEvents 按重要度与国家过滤，并丢掉已经过去超过 24 小时的事件（保留刚发生的，
// 因为刚公布的数据往往正是当下行情的直接原因）。结果按时间升序。
func filterEvents(all []MacroEvent, minImpact string, countries []string) []MacroEvent {
	min := impactRank(minImpact)
	want := map[string]bool{}
	for _, c := range countries {
		want[strings.ToUpper(c)] = true
	}
	cutoff := time.Now().Add(-24 * time.Hour)

	out := make([]MacroEvent, 0, len(all))
	for _, e := range all {
		if impactRank(e.Impact) < min || e.At.Before(cutoff) {
			continue
		}
		if len(want) > 0 && !want[strings.ToUpper(e.Country)] {
			continue
		}
		out = append(out, e)
	}
	// fetchJSON 返回的顺序不保证，显式排序
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].At.Before(out[j-1].At); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// UpcomingWithin 返回未来 d 时间内的事件（用于「今天有什么」）。
func UpcomingWithin(events []MacroEvent, d time.Duration) []MacroEvent {
	now := time.Now()
	deadline := now.Add(d)
	out := make([]MacroEvent, 0, len(events))
	for _, e := range events {
		if !e.At.Before(now) && e.At.Before(deadline) {
			out = append(out, e)
		}
	}
	return out
}

// ---- BLS：已公布的实际值 ----

// BLSSeries 是一条 BLS 时间序列的最新值。
type BLSSeries struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Period string `json:"period"` // 如 "July 2026"
	Value  string `json:"value"`
}

// BLS 常用序列 ID。
const (
	blsUnemploymentRate = "LNS14000000"   // 失业率（%）
	blsNonFarmPayroll   = "CES0000000001" // 非农就业总人数（千人）
)

var blsNames = map[string]string{
	blsUnemploymentRate: "失业率",
	blsNonFarmPayroll:   "非农就业人数",
}

// blsCache 缓存 BLS：月度数据，缓存 6 小时。BLS 免费档有日请求上限，别频繁打。
var blsCache = newTTLCache[[]BLSSeries](6 * time.Hour)

type blsResponse struct {
	Status  string `json:"status"`
	Results struct {
		Series []struct {
			ID   string `json:"seriesID"`
			Data []struct {
				Year       string `json:"year"`
				PeriodName string `json:"periodName"`
				Value      string `json:"value"`
			} `json:"data"`
		} `json:"series"`
	} `json:"Results"`
}

// GetBLSLatest 拉失业率与非农的最新已公布值（免费无需 key）。
func GetBLSLatest() ([]BLSSeries, error) {
	const key = "bls:latest"
	if v, ok := blsCache.get(key); ok {
		return v, nil
	}

	body := fmt.Sprintf(`{"seriesid":[%q,%q],"latest":true}`, blsUnemploymentRate, blsNonFarmPayroll)
	var resp blsResponse
	if err := postJSON(marketClient, "BLS", "https://api.bls.gov/publicAPI/v2/timeseries/data/", body, &resp); err != nil {
		log.Warnf("%v", err)
		return nil, err
	}
	if resp.Status != "REQUEST_SUCCEEDED" {
		return nil, fmt.Errorf("BLS 返回状态 %s", resp.Status)
	}

	out := make([]BLSSeries, 0, len(resp.Results.Series))
	for _, s := range resp.Results.Series {
		if len(s.Data) == 0 {
			continue
		}
		d := s.Data[0]
		out = append(out, BLSSeries{
			ID: s.ID, Name: blsNames[s.ID],
			Period: d.PeriodName + " " + d.Year, Value: d.Value,
		})
	}
	blsCache.set(key, out)
	return out, nil
}
