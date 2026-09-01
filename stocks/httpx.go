package stocks

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/narglc/stock.quot.tele.bot/utils"
)

// marketClient 是行情主体接口（CoinGecko / alternative.me）共享的 HTTP 客户端。
// 复用同一个 client（而不是每次调用现场 new 一个）才能复用底层连接池，
// 否则每个请求都要重做 TCP + TLS 握手。
var marketClient = &http.Client{Timeout: 10 * time.Second}

// maxErrBody 是错误信息里携带的响应体上限（字节）。
// 限流/故障页可能有几 KB，整段打进日志会把日志文件冲爆。
const maxErrBody = 200

// fetchJSON GET 一个 URL，校验 HTTP 状态码后把响应体反序列化到 v。
//
// 显式检查状态码很重要：CoinGecko 免费额度被打满时返回 429 + 一段说明文本，
// 若直接 Unmarshal 只会得到一个「解析失败」的误导性错误，看不出真实原因是限流。
// name 用于在错误信息里标明是哪个接口。
func fetchJSON(client *http.Client, name, url string, v any) error {
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("%s 请求失败: %w", name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("%s 读取失败: %w", name, err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s HTTP %d: %s", name, resp.StatusCode, utils.Truncate(string(body), maxErrBody))
	}

	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("%s 解析失败: %w, body: %s", name, err, utils.Truncate(string(body), maxErrBody))
	}
	return nil
}
