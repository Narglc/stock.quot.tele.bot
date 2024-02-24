package randompic

import (
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
)

const (
	DefaultPics = "http://img5.adesk.com/605455dae7bce72db9fefd3c?sign=8fa8c7f1efd9741a1c529daca53e68c8&t=65d8a9d1"
)

type RandomSrv interface {
	GetRandomPic() (string, error)
}

var AllRandomPicSrv map[string]RandomSrv = make(map[string]RandomSrv)

func GetRandomPic(srvType string) (string, error) {

	rsrv, ok := AllRandomPicSrv[srvType]

	if !ok {
		log.Warnf("AllRandomPicSrv not find %s srvType", srvType)
		return DefaultPics, nil
	}

	picUrl, err := rsrv.GetRandomPic()

	if err != nil {
		log.Warnf("RandomSrv Req PicUrl Fail, err:%+v", err)
		return DefaultPics, nil
	}

	return picUrl, nil
}
