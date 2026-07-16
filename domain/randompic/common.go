package randompic

import (
	"fmt"
	"net/http"
	"time"
)

// httpClient 是所有图源共享的 HTTP 客户端。带超时避免图源慢/挂时请求无限阻塞
// （原来用 http.DefaultClient 无超时，是高频失败/卡顿的主因）。
var httpClient = &http.Client{Timeout: 10 * time.Second}

type RandomSrv interface {
	GetRandomPic() (string, error)
}

var AllRandomPicSrv map[string]RandomSrv = make(map[string]RandomSrv)

// GetRandomPic 按 key 派发到具体图源。失败时如实返回错误（不再吞成 DefaultPics），
// 由调用方决定兜底与提示（便于把失败原因带进消息）。
func GetRandomPic(srvType string) (string, error) {
	rsrv, ok := AllRandomPicSrv[srvType]
	if !ok {
		return "", fmt.Errorf("未知图源: %s", srvType)
	}
	return rsrv.GetRandomPic()
}
