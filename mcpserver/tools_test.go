package mcpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

// rpc 走一遍真实的 Streamable HTTP：initialize → initialized → 目标方法。
// 这和 scripts/mcp-smoke.sh 用的是同一套序列，测试过了说明脚本的协议部分也是对的。
func rpc(t *testing.T, url, method string, params any) map[string]any {
	t.Helper()
	call := func(body string, sid string) (*http.Response, map[string]any) {
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if sid != "" {
			req.Header.Set("Mcp-Session-Id", sid)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s 请求失败: %v", method, err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		// 可能是 SSE（data: 行）也可能是纯 JSON
		payload := raw
		for _, line := range bytes.Split(raw, []byte("\n")) {
			if bytes.HasPrefix(line, []byte("data: ")) {
				payload = bytes.TrimPrefix(line, []byte("data: "))
				break
			}
		}
		var out map[string]any
		if len(bytes.TrimSpace(payload)) > 0 {
			if err := json.Unmarshal(payload, &out); err != nil {
				t.Fatalf("%s 响应不是 JSON: %v\n%s", method, err, raw)
			}
		}
		return resp, out
	}

	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`
	resp, initOut := call(initBody, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize HTTP %d", resp.StatusCode)
	}
	if _, ok := initOut["result"]; !ok {
		t.Fatalf("initialize 无 result: %+v", initOut)
	}
	sid := resp.Header.Get("Mcp-Session-Id")
	call(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, sid)

	p, _ := json.Marshal(params)
	body := `{"jsonrpc":"2.0","id":2,"method":"` + method + `","params":` + string(p) + `}`
	if params == nil {
		body = `{"jsonrpc":"2.0","id":2,"method":"` + method + `"}`
	}
	_, out := call(body, sid)
	return out
}

func toolNames(t *testing.T, out map[string]any) []string {
	t.Helper()
	res, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list 无 result: %+v", out)
	}
	var names []string
	for _, x := range res["tools"].([]any) {
		names = append(names, x.(map[string]any)["name"].(string))
	}
	return names
}

func startTestServer(t *testing.T, apifyToken string) string {
	t.Helper()
	s := newMCPServer(12345, nil, apifyToken)
	ts := httptest.NewServer(server.NewStreamableHTTPServer(s))
	t.Cleanup(ts.Close)
	return ts.URL
}

func TestTools_GetLiqmapRegisteredOnlyWithApifyToken(t *testing.T) {
	with := toolNames(t, rpc(t, startTestServer(t, "apify-xyz"), "tools/list", nil))
	if !contains(with, "get_liqmap") {
		t.Errorf("配了 APIFY_TOKEN 应注册 get_liqmap，实得 %v", with)
	}
	if !contains(with, "send_message") {
		t.Errorf("send_message 应恒在，实得 %v", with)
	}

	without := toolNames(t, rpc(t, startTestServer(t, ""), "tools/list", nil))
	if contains(without, "get_liqmap") {
		t.Errorf("没配 APIFY_TOKEN 不该注册 get_liqmap，实得 %v", without)
	}
}

// get_liqmap 对非法 symbol 应在校验阶段就返回 tool 错误，不该走到 Apify（那会白烧配额）。
func TestTools_GetLiqmapRejectsBadSymbol(t *testing.T) {
	out := rpc(t, startTestServer(t, "apify-xyz"), "tools/call", map[string]any{
		"name": "get_liqmap", "arguments": map[string]any{"symbol": "BTC&x=1"},
	})
	res, _ := out["result"].(map[string]any)
	if res == nil || res["isError"] != true {
		t.Fatalf("非法 symbol 应返回 isError=true，实得 %+v", out)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "不认识这个币种符号") {
		t.Errorf("错误应来自 NormalizeSymbol，实得: %s", text)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
