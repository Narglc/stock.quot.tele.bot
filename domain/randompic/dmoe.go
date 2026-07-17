package randompic

// www.dmoe.cc（樱花随机二次元图片 API）：GET https://www.dmoe.cc/random.php
// 直接返回一张随机二次元图（图片本身，非 JSON；也支持 ?return=json 拿 url，这里用直图更省事）。
// 直接把该端点当图片 URL 返回，由上层 fetchPic 下载即可，无需在此发请求/解析。

type DmoeClt struct{}

var _ RandomSrv = &DmoeClt{}

func init() {
	AllRandomPicSrv["dmoe"] = &DmoeClt{}
}

func (d *DmoeClt) GetRandomPic() (string, error) {
	return "https://www.dmoe.cc/random.php", nil
}
