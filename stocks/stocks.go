package stocks

import (
	"io/ioutil"
	"log"
	"net/http"
)

func GetStock() {
	url := "https://stock.xueqiu.com/v5/stock/realtime/quotec.json?symbol=SH601001,SZ002617"

	response, err := http.Get(url)
	if err != nil {
		log.Fatalf("请求失败: %v", err)
	}
	defer response.Body.Close()

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Fatalf("读取响应失败: %v", err)
	}

	log.Println("响应：", string(body))
}
