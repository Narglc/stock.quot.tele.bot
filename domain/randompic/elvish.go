package randompic

import (
	"fmt"
	"net/http"
	"net/url"
)

// pic.elvish.me（基于腾讯 EdgeOne Pages 的随机图 API）：
//   GET /api/?type=pc  → 302 跳到真实图片（横屏 webp，如 /images/pc/xxx.webp）
//   type 可选 pc(横屏) / pe(竖屏) / ua(按 UA 自适应)。这里用 pc 更适合聊天展示。
// 端点只返回 302，不返回图片体，故这里解析出 Location 的真实图片 URL 再交给上层 fetchPic 下载。
// 返回的是 webp，由 handler 的 webpToPNG 转成 Telegram 可发的 PNG。

const elvishEndpoint = "https://pic.elvish.me/api/?type=pc"

type ElvishClt struct {
	Endpoint string
}

var _ RandomSrv = &ElvishClt{}

func init() {
	AllRandomPicSrv["elvish"] = &ElvishClt{Endpoint: elvishEndpoint}
}

func (e *ElvishClt) GetRandomPic() (string, error) {
	// 用不跟随重定向的 client，取首跳 Location 即真实图片地址。
	client := &http.Client{
		Timeout: httpClient.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(e.Endpoint)
	if err != nil {
		return "", fmt.Errorf("elvish 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("elvish 期望 302，实得 %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("elvish 缺少 Location 头")
	}
	// Location 可能是相对路径，按请求 URL 解析成绝对地址。
	ref, err := url.Parse(loc)
	if err != nil {
		return "", fmt.Errorf("elvish Location 解析失败: %w", err)
	}
	return resp.Request.URL.ResolveReference(ref).String(), nil
}
