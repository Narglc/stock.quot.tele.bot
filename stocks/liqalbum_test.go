package stocks

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNormalizeSymbol(t *testing.T) {
	ok := []struct{ in, want string }{
		{"btc", "BTC"},
		{"BTC", "BTC"},
		{"  eth  ", "ETH"},
		{"1inch", "1INCH"},
	}
	for _, c := range ok {
		got, err := NormalizeSymbol(c.in)
		if err != nil || got != c.want {
			t.Errorf("NormalizeSymbol(%q) = %q, %v; want %q, nil", c.in, got, err, c.want)
		}
	}

	// 这些必须被拒：拼进第三方接口的查询串会污染参数，
	// 而且无效输入走到 Apify 会白烧一次配额（90 秒 + 计费）。
	bad := []string{
		"",
		"   ",
		"BTC&instId=X",
		"BTC-USDT",
		"../../etc/passwd",
		"BTC USDT",
		"TOOLONGSYMBOL",
		"btc?a=1",
	}
	for _, in := range bad {
		if got, err := NormalizeSymbol(in); err == nil {
			t.Errorf("NormalizeSymbol(%q) 应当被拒，实得 %q", in, got)
		}
	}
}

// 无效符号必须在打 Apify 之前就被拒掉——这正是省配额的关键。
// 传空 token：如果校验没起作用，会走到 fetchLiqRaw 报「apify token 为空」。
func TestGetLiqHeatmap_RejectsBadSymbolBeforeAPICall(t *testing.T) {
	_, err := GetLiqHeatmap("BTC&evil=1", "")
	if err == nil {
		t.Fatal("非法符号应当返回错误")
	}
	if strings.Contains(err.Error(), "token") {
		t.Errorf("应在符号校验阶段就被拒，而不是走到 Apify 调用: %v", err)
	}
	if !strings.Contains(err.Error(), "不认识这个币种符号") {
		t.Errorf("错误信息应指出符号非法，实得: %v", err)
	}
}

func TestLiqCache_HitAvoidsRefetch(t *testing.T) {
	liqCache = newTTLCache[*LiqHeatmap](10 * time.Minute)
	h := &LiqHeatmap{Symbol: "TESTCOIN", Interval: "15m", CurrentPrice: 100}
	liqCache.set("TESTCOIN", h)

	// token 传空：只有命中缓存才可能成功返回（否则会因 token 为空而失败）。
	got, err := GetLiqHeatmap("testcoin", "")
	if err != nil {
		t.Fatalf("应命中缓存，实得错误 %v", err)
	}
	if got.Symbol != "TESTCOIN" {
		t.Errorf("拿到的不是缓存对象: %+v", got)
	}
}

func TestLiqLock_SamePerSymbol(t *testing.T) {
	if liqLock("AAA") != liqLock("AAA") {
		t.Error("同一符号应复用同一把锁，否则串行化失效")
	}
	if liqLock("AAA") == liqLock("BBB") {
		t.Error("不同符号不该共用锁，否则查 ETH 会被查 BTC 阻塞")
	}
}

func TestLiqLock_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			liqLock("RACE").Lock()
			liqLock("RACE").Unlock()
		}()
	}
	wg.Wait()
}

func TestLiqAlbumPhotos_FreshReaderEachCall(t *testing.T) {
	a := &LiqAlbum{HeatmapPNG: []byte("heat"), MapPNG: []byte("map"), Caption: "cap"}
	first := LiqAlbumPhotos(a)
	second := LiqAlbumPhotos(a)
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("相册应有两张图，实得 %d / %d", len(first), len(second))
	}
	// 多群广播时每次都必须是新 reader，否则第二个群收到空文件。
	if first[0] == second[0] {
		t.Error("两次调用返回了同一个 Photo 对象，reader 会被复用")
	}
}
