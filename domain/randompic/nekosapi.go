package randompic

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// nekosapi.com v4 random 端点：返回一个 JSON 数组，每个元素含 url（webp，cdn.nekosapi.com）。
// 接口支持 limit 一次返回多张；这里取 limit=1 的首张即可（random 每次都不同）。
// 文档：https://nekosapi.com/docs/images/random

type NekosApiImage struct {
	Url string `json:"url"`
}

type NekosApiClt struct {
	Url string
}

var _ RandomSrv = &NekosApiClt{}

func init() {
	AllRandomPicSrv["nekos"] = GetNekosApiClt()
}

var (
	nekosApiClt  *NekosApiClt
	onceNekosApi sync.Once
)

func GetNekosApiClt() *NekosApiClt {
	onceNekosApi.Do(func() {
		// 如需 nsfw 可加 &rating=explicit（safe/suggestive/borderline/explicit）。
		nekosApiClt = &NekosApiClt{Url: "https://api.nekosapi.com/v4/images/random?limit=1"}
	})
	return nekosApiClt
}

func (n *NekosApiClt) GetRandomPic() (string, error) {
	resp, err := httpClient.Get(n.Url)
	if err != nil {
		return "", fmt.Errorf("nekosapi 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("nekosapi 读取响应失败: %w", err)
	}
	var arr []NekosApiImage
	if err := json.Unmarshal(body, &arr); err != nil {
		return "", fmt.Errorf("nekosapi 响应解析失败: %w", err)
	}
	if len(arr) == 0 || arr[0].Url == "" {
		return "", fmt.Errorf("nekosapi 返回空数据")
	}
	return arr[0].Url, nil
}
