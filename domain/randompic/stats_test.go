package randompic

import "testing"

func resetStats() {
	statsMu.Lock()
	stats = map[string]*srcStat{}
	statsMu.Unlock()
}

func TestWeightedPick_Empty(t *testing.T) {
	if got := WeightedPick(nil); got != "" {
		t.Errorf("空候选应返回空串，实得 %q", got)
	}
}

func TestWeightedPick_AlwaysReturnsCandidate(t *testing.T) {
	resetStats()
	cands := []string{"a", "b", "c"}
	valid := map[string]bool{"a": true, "b": true, "c": true}
	for i := 0; i < 200; i++ {
		if got := WeightedPick(cands); !valid[got] {
			t.Fatalf("返回了候选之外的值: %q", got)
		}
	}
}

// 失败多的源应被明显降权，但不能被永久排除（拉普拉斯平滑保证仍有机会恢复）。
func TestWeightedPick_FavorsSuccessfulSource(t *testing.T) {
	resetStats()
	for i := 0; i < 100; i++ {
		Record("good", true)
		Record("bad", false)
	}

	counts := map[string]int{}
	for i := 0; i < 2000; i++ {
		counts[WeightedPick([]string{"good", "bad"})]++
	}

	if counts["good"] <= counts["bad"] {
		t.Errorf("成功率高的源应被更多选中，good=%d bad=%d", counts["good"], counts["bad"])
	}
	if counts["bad"] == 0 {
		t.Error("失败源不该被永久排除，应保留恢复机会")
	}
}

func TestRecord_And_Snapshot(t *testing.T) {
	resetStats()
	Record("x", true)
	Record("x", true)
	Record("x", false)

	snap := StatsSnapshot()
	if snap["x"] != [2]int64{2, 1} {
		t.Errorf("快照应为 [2 1]，实得 %v", snap["x"])
	}

	// 快照必须是副本，改它不应影响内部状态
	snap["x"] = [2]int64{99, 99}
	if StatsSnapshot()["x"] != [2]int64{2, 1} {
		t.Error("StatsSnapshot 返回的不是副本")
	}
}

func TestGetRandomPic_UnknownSource(t *testing.T) {
	if _, err := GetRandomPic("不存在的图源"); err == nil {
		t.Error("未知图源应返回错误")
	}
}
