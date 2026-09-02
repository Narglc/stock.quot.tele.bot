package stocks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestLive_DumpLiqSnapshot 真的打一次 Apify，把推送给网页的那份快照原样落盘。
//
// 用途：给 deploy/liqmap-web 的预览页和测试提供一份**真实**数据，存下来反复用，
// 不用每次看预览都烧一次配额（Apify 免费档每 UTC 日只有 5 次）。
//
// 默认跳过。触发方式：
//
//	LIVE=1 APIFY_TOKEN=xxx SNAPSHOT_OUT=deploy/liqmap-web/test/fixtures/btc-snapshot.json \
//	  go test ./stocks/ -run TestLive_DumpLiqSnapshot -v
func TestLive_DumpLiqSnapshot(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip("设 LIVE=1 才跑真实网络测试")
	}
	token := os.Getenv("APIFY_TOKEN")
	if token == "" {
		t.Skip("需要 APIFY_TOKEN")
	}
	out := os.Getenv("SNAPSHOT_OUT")
	if out == "" {
		t.Skip("需要 SNAPSHOT_OUT 指定输出路径")
	}
	// go test 的 cwd 是包目录而不是仓库根，相对路径按仓库根解析，否则写到 stocks/ 下面去了。
	if !filepath.IsAbs(out) {
		out = filepath.Join(repoRoot(t), out)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	symbol := os.Getenv("SNAPSHOT_SYMBOL")
	if symbol == "" {
		symbol = "BTC"
	}

	h, err := GetLiqHeatmap(symbol, token)
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}

	snap := buildSnapshot(h)
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	t.Logf("已写入 %s：%d 个价格档 × %d 个时间格，%d 个非零格，%.1f KB，现价 %.2f",
		out, len(snap.Prices), len(snap.Times), len(snap.Cells), float64(len(data))/1024, snap.CurrentPrice)
}

// repoRoot 从当前目录向上找 go.mod 所在目录。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("找不到 go.mod，无法定位仓库根")
		}
		dir = parent
	}
}
