package randompic

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
)

type UrlData struct {
	Regular  string `json:"regular"`
	Original string `json:"original"`
}

type LoliconData struct {
	Pid    int     `json:"pid"`
	Title  string  `json:"title"`
	Author string  `json:"Author"`
	Urls   UrlData `json:"urls"`
}

type LoliconRsp struct {
	Error string        `json:"error"`
	Data  []LoliconData `json:"data"`
}

type LoliconClt struct {
	Url string
}

var _ RandomSrv = &LoliconClt{}

func init() {
	AllRandomPicSrv["lolicon"] = GetLoliconClt()
}

var (
	loliconClt    *LoliconClt
	oneloliconClt sync.Once
)

func GetLoliconClt() *LoliconClt {
	oneloliconClt.Do(func() {
		loliconClt = &LoliconClt{
			Url: "https://api.lolicon.app/setu/v2",
		}
	})
	return loliconClt
}

func (l LoliconClt) GetRandomPic() (string, error) {
	// 请求 regular 尺寸（长边最大 1000px），避免 original 原图过大导致
	// Telegram 发送时超过 5MB / 尺寸限制而失败。
	data := []byte(`{"r18": 1, "size": ["regular", "original"]}`)

	// 发送POST请求
	response, err := http.Post(l.Url, "application/json", bytes.NewBuffer(data))

	if err != nil {
		log.Errorf("POST请求失败: %v", err)
		return "", err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Errorf("读取响应失败: %v", err)
		return "", err
	}

	var llrsp LoliconRsp
	if err := json.Unmarshal(body, &llrsp); err != nil {
		return DefaultPics, nil
	}

	if len(llrsp.Data) == 0 {
		log.Warnf("lolicon 返回空数据, error:%s", llrsp.Error)
		return DefaultPics, nil
	}

	// 优先使用较小的 regular 尺寸，缺失时回退 original
	if url := llrsp.Data[0].Urls.Regular; url != "" {
		return url, nil
	}
	return llrsp.Data[0].Urls.Original, nil
}
