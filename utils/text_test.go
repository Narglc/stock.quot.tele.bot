package utils

import "testing"

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"不超长原样返回", "hello", 10, "hello"},
		{"恰好等于上限", "hello", 5, "hello"},
		{"纯 ASCII 截断", "hello world", 5, "hello…"},
		{"n 为 0", "hello", 0, ""},
		{"n 为负", "hello", -1, ""},
		// 关键用例：中文一个字 3 字节，从第 4 字节切会切出半个字符。
		// 应退回到字符边界，只保留完整的「限」。
		{"中文退回字符边界", "限流了", 4, "限…"},
		{"中文恰好边界", "限流了", 6, "限流…"},
		{"中英混排", "错误 error", 3, "错…"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Truncate(c.in, c.n); got != c.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
			}
		})
	}
}

func TestTruncate_AlwaysValidUTF8(t *testing.T) {
	s := "限流了，请稍后重试"
	for n := 0; n <= len(s)+2; n++ {
		got := Truncate(s, n)
		for i, r := range got {
			if r == '�' {
				t.Fatalf("Truncate(%q, %d) = %q 在偏移 %d 产生了无效 UTF-8", s, n, got, i)
			}
		}
	}
}

func TestGetRandomIndex_InRange(t *testing.T) {
	// 主要是防回归：曾经每次调用都 rand.Seed(time.Now())，
	// 同一纳秒内的连续调用会返回相同结果。
	const size = 8
	seen := map[int]bool{}
	for i := 0; i < 500; i++ {
		got := GetRandomIndex(size)
		if got < 0 || got >= size {
			t.Fatalf("GetRandomIndex(%d) = %d，越界", size, got)
		}
		seen[got] = true
	}
	if len(seen) < size/2 {
		t.Errorf("500 次调用只取到 %d 种结果，随机性可疑", len(seen))
	}
}
