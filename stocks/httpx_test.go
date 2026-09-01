package stocks

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchJSON_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":42}`))
	}))
	defer srv.Close()

	var got struct {
		Value int `json:"value"`
	}
	if err := fetchJSON(srv.Client(), "测试", srv.URL, &got); err != nil {
		t.Fatalf("期望成功，实得 %v", err)
	}
	if got.Value != 42 {
		t.Errorf("value = %d, want 42", got.Value)
	}
}

// 这是本次改动的核心：限流(429)必须报成 HTTP 429，而不是含糊的「解析失败」。
func TestFetchJSON_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("You've exceeded the Rate Limit."))
	}))
	defer srv.Close()

	var got map[string]any
	err := fetchJSON(srv.Client(), "行情", srv.URL, &got)
	if err == nil {
		t.Fatal("429 应当返回错误")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("错误信息应指出 429，实得: %v", err)
	}
	if strings.Contains(err.Error(), "解析失败") {
		t.Errorf("429 不应被误报成解析失败: %v", err)
	}
}

// 超大响应体不能整段进日志/错误信息。
func TestFetchJSON_ErrBodyTruncated(t *testing.T) {
	huge := strings.Repeat("x", 10000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(huge))
	}))
	defer srv.Close()

	var got map[string]any
	err := fetchJSON(srv.Client(), "行情", srv.URL, &got)
	if err == nil {
		t.Fatal("500 应当返回错误")
	}
	if len(err.Error()) > maxErrBody+100 {
		t.Errorf("错误信息未截断，长度 %d", len(err.Error()))
	}
}

func TestFetchJSON_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	var got map[string]any
	err := fetchJSON(srv.Client(), "行情", srv.URL, &got)
	if err == nil || !strings.Contains(err.Error(), "解析失败") {
		t.Errorf("期望解析失败，实得 %v", err)
	}
}

func TestTTLCache(t *testing.T) {
	c := newTTLCache[int](50 * time.Millisecond)

	if _, ok := c.get("k"); ok {
		t.Error("空缓存不该命中")
	}

	c.set("k", 7)
	if v, ok := c.get("k"); !ok || v != 7 {
		t.Errorf("期望命中 7，实得 %v %v", v, ok)
	}

	time.Sleep(70 * time.Millisecond)
	if _, ok := c.get("k"); ok {
		t.Error("过期后不该命中")
	}
}

func TestTTLCache_Concurrent(t *testing.T) {
	c := newTTLCache[int](time.Minute)
	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			c.set("k", i)
			c.get("k")
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}
