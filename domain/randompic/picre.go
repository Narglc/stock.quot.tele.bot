package randompic

// pic.re：GET https://pic.re/image 每次直接返回一张随机动漫图（图片本身，非 JSON）。
// 因此直接把该端点当作图片 URL 返回，由上层 fetchPic 下载即可，无需在此发请求/解析。

type PicReClt struct{}

var _ RandomSrv = &PicReClt{}

func init() {
	AllRandomPicSrv["picre"] = &PicReClt{}
}

func (p *PicReClt) GetRandomPic() (string, error) {
	return "https://pic.re/image", nil
}
