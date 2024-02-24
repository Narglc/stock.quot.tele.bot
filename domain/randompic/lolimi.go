package randompic

import (
	"encoding/json"
	"io"
	"net/http"

	log "github.com/sirupsen/logrus"
)

type LolimiData struct {
	Img string `json:"image"`
}

type LolimiRsp struct {
	Code int        `json:"code"`
	Text string     `json:"text"`
	Data LolimiData `json:"data"`
}

type LolimiClt struct {
	Url string
}

var _ RandomSrv = &LolimiClt{}

func init() {
	AllRandomPicSrv["lolimi"] = LolimiClt{
		Url: "https://api.lolimi.cn/API/meinv/api.php?type=json",
	}
}

func (l LolimiClt) GetRandomPic() (string, error) {
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

	var llrsp LolimiRsp
	if err := json.Unmarshal(body, &llrsp); err != nil {
		return DefaultPics, nil
	}

	return llrsp.Data.Img, nil
}
