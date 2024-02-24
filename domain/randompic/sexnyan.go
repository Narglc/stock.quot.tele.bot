package randompic

import (
	"encoding/json"
	"io"
	"net/http"

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
	AllRandomPicSrv["sexnyan"] = SexNyanClt{
		Url: "https://sex.nyan.xyz/api/v2/?r18=true",
	}
}

func (l SexNyanClt) GetRandomPic() (string, error) {
	response, err := http.Get(l.Url)
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

	var llrsp SexNyanRsp
	if err := json.Unmarshal(body, &llrsp); err != nil {
		return DefaultPics, nil
	}

	return llrsp.Data[0].Url, nil
}
