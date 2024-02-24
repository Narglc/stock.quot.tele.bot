package randompic

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
)

type UrlData struct {
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
	AllRandomPicSrv["lolicon"] = LoliconClt{
		Url: "https://api.lolicon.app/setu/v2",
	}
}

func (l LoliconClt) GetRandomPic() (string, error) {
	data := []byte(`{"r18": 1}`)

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

	return llrsp.Data[0].Urls.Original, nil
}
