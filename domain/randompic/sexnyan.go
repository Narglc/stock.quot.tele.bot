package randompic

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
)

type SexNyanData struct {
	Pid    int      `json:"pid"`
	Title  string   `json:"title"`
	Author string   `json:"Author"`
	Tags   []string `json:"tags"`
	Url    string   `json:"url"`
}

type SexNyanRsp struct {
	Succ     bool          `json:"succes"`
	Status   int           `json:"status"`
	Msg      string        `json:"message"`
	CodeName string        `json:"codename"`
	Time     int64         `json:"time"`
	Data     []SexNyanData `json:"data"`
}

type SexNyanClt struct {
	Url string
}

var _ RandomSrv = &SexNyanClt{}

func init() {
	AllRandomPicSrv["sexnyan"] = GetSexNyanClt()
	AllRandomPicSrv["nfsw"] = GetSexNyanClt()
}

var (
	sexnayClt     *SexNyanClt
	oncesexnayClt sync.Once
)

func GetSexNyanClt() *SexNyanClt {
	oncesexnayClt.Do(func() {
		sexnayClt = &SexNyanClt{
			Url: "https://sex.nyan.xyz/api/v2/?r18=true",
		}
	})
	return sexnayClt
}

func (l *SexNyanClt) GetRandomPic() (string, error) {
	response, err := httpClient.Get(l.Url)
	if err != nil {
		log.Errorf("请求失败: %v", err)
		return "", fmt.Errorf("sexnyan 请求失败: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Errorf("读取响应失败: %v", err)
		return "", fmt.Errorf("sexnyan 读取响应失败: %w", err)
	}

	var llrsp SexNyanRsp
	if err := json.Unmarshal(body, &llrsp); err != nil {
		return "", fmt.Errorf("sexnyan 响应解析失败: %w", err)
	}

	if len(llrsp.Data) == 0 {
		log.Warnf("sexnyan 返回空数据, msg:%s", llrsp.Msg)
		return "", fmt.Errorf("sexnyan 返回空数据: %s", llrsp.Msg)
	}

	return llrsp.Data[0].Url, nil
}
