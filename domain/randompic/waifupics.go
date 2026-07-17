package randompic

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// waifu.pics：活跃、稳定、自带 CDN 的动漫图 API，返回单张图片直链。
// 响应示例：{"url":"https://i.waifu.pics/xxxx.png"}

type WaifuPicsRsp struct {
	Url string `json:"url"`
}

type WaifuPicsClt struct {
	Url string
}

var _ RandomSrv = &WaifuPicsClt{}

func init() {
	AllRandomPicSrv["waifu"] = GetWaifuPicsClt()
}

var (
	waifuPicsClt  *WaifuPicsClt
	onceWaifuPics sync.Once
)

func GetWaifuPicsClt() *WaifuPicsClt {
	onceWaifuPics.Do(func() {
		// 与现有图源一致走 nsfw 分类；要 SFW 改成 /sfw/waifu 即可。
		waifuPicsClt = &WaifuPicsClt{Url: "https://api.waifu.pics/nsfw/waifu"}
	})
	return waifuPicsClt
}

func (w *WaifuPicsClt) GetRandomPic() (string, error) {
	resp, err := httpClient.Get(w.Url)
	if err != nil {
		return "", fmt.Errorf("waifu.pics 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("waifu.pics 读取响应失败: %w", err)
	}

	var r WaifuPicsRsp
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("waifu.pics 响应解析失败: %w", err)
	}
	if r.Url == "" {
		return "", fmt.Errorf("waifu.pics 返回空 url")
	}
	return r.Url, nil
}
