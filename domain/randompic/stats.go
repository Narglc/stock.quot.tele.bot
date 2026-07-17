package randompic

import (
	"math/rand"
	"sync"
)

// 图源成功率统计：记录各源的成功/失败次数，据此加权选源——老失败的源被降权，
// 但用拉普拉斯平滑保证不会被永久排除（仍有机会恢复）。

type srcStat struct{ ok, fail int64 }

var (
	statsMu sync.Mutex
	stats   = map[string]*srcStat{}
)

// Record 记录一次图源使用结果（成功/失败）。
func Record(src string, success bool) {
	statsMu.Lock()
	defer statsMu.Unlock()
	s := stats[src]
	if s == nil {
		s = &srcStat{}
		stats[src] = s
	}
	if success {
		s.ok++
	} else {
		s.fail++
	}
}

// WeightedPick 按各源成功率加权随机选一个。无历史时近似等概率；
// 权重 = (ok+1)/(ok+fail+2)，成功率越高越可能被选中，失败多的被降权但不归零。
func WeightedPick(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	statsMu.Lock()
	weights := make([]float64, len(candidates))
	total := 0.0
	for i, c := range candidates {
		var ok, fail int64
		if s := stats[c]; s != nil {
			ok, fail = s.ok, s.fail
		}
		w := float64(ok+1) / float64(ok+fail+2)
		weights[i] = w
		total += w
	}
	statsMu.Unlock()

	r := rand.Float64() * total
	for i, w := range weights {
		r -= w
		if r <= 0 {
			return candidates[i]
		}
	}
	return candidates[len(candidates)-1]
}

// StatsSnapshot 返回各源的 [成功, 失败] 计数快照（供排错/展示）。
func StatsSnapshot() map[string][2]int64 {
	statsMu.Lock()
	defer statsMu.Unlock()
	out := make(map[string][2]int64, len(stats))
	for k, s := range stats {
		out[k] = [2]int64{s.ok, s.fail}
	}
	return out
}
