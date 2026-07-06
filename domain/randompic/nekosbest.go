package randompic

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
)

// nekos.best 返回的图片尺寸适中（多为千级像素），Telegram 可直接接收。
// 响应示例：{"results":[{"url":"https://nekos.best/api/v2/neko/xxxx.png", ...}]}

type NekosBestResult struct {
	Url string `json:"url"`
}

type NekosBestRsp struct {
	Results []NekosBestResult `json:"results"`
}

type NekosBestClt struct {
	Url string
}

var _ RandomSrv = &NekosBestClt{}

func init() {
	AllRandomPicSrv["nekos"] = GetNekosBestClt()
}

var (
	nekosBestClt     *NekosBestClt
	onceNekosBestClt sync.Once
)

func GetNekosBestClt() *NekosBestClt {
	onceNekosBestClt.Do(func() {
		nekosBestClt = &NekosBestClt{
			Url: "https://nekos.best/api/v2/neko",
		}
	})
	return nekosBestClt
}

func (n *NekosBestClt) GetRandomPic() (string, error) {
	response, err := http.Get(n.Url)
	if err != nil {
		log.Errorf("请求失败: %v", err)
		return "", err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Errorf("读取响应失败: %v", err)
		return "", err
	}

	var rsp NekosBestRsp
	if err := json.Unmarshal(body, &rsp); err != nil {
		return DefaultPics, nil
	}

	if len(rsp.Results) == 0 {
		log.Warnf("nekos.best 返回空数据")
		return DefaultPics, nil
	}

	return rsp.Results[0].Url, nil
}
