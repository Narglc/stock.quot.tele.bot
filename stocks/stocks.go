package stocks

import (
	"io"
	"net/http"

	log "github.com/sirupsen/logrus"
)

func GetStock() error {
	url := "https://stock.xueqiu.com/v5/stock/realtime/quotec.json?symbol=SH601001,SZ002617"

	response, err := http.Get(url)
	if err != nil {
		log.Errorf("请求失败: %v", err)
		return err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Errorf("读取响应失败: %v", err)
		return err
	}

	log.Println("响应：", string(body))
	return nil
}
