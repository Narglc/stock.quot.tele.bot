package randompic

// api.elaina.cat：GET https://api.elaina.cat/random/pc/ 直接返回一张随机横屏图（图片本身，非 JSON）。
//   /random/pc/（横屏，末尾斜杠避免 301 跳转）、/random/mobile（竖屏）。
// 直接把该端点当图片 URL 返回，由上层 fetchPic 下载即可，无需在此发请求/解析。

type ElainaClt struct{}

var _ RandomSrv = &ElainaClt{}

func init() {
	AllRandomPicSrv["elaina"] = &ElainaClt{}
}

func (e *ElainaClt) GetRandomPic() (string, error) {
	return "https://api.elaina.cat/random/pc/", nil
}
