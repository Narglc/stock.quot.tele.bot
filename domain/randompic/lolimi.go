package randompic

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

type LolimiData struct {
	Img string `json:"image"`
}

type LolimiRsp struct {
	Code int        `json:"code"`
	Text string     `json:"text"`
	Data LolimiData `json:"data"`
}

const (
	DefaultPics = "http://img5.adesk.com/605455dae7bce72db9fefd3c?sign=8fa8c7f1efd9741a1c529daca53e68c8&t=65d8a9d1"
)

func GetRandomPic() string {
	url := "https://api.lolimi.cn/API/meinv/api.php?type=json"

	response, err := http.Get(url)
	if err != nil {
		log.Fatalf("请求失败: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatalf("读取响应失败: %v", err)
	}

	var llrsp LolimiRsp
	if err := json.Unmarshal(body, &llrsp); err != nil {
		return DefaultPics
	}

	return llrsp.Data.Img
}
