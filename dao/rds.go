package dao

import (
	"github.com/go-redis/redis"
)

var (
	rdb *redis.Client
)

const (
	StickerKey     = "tg:gohome:stickers"
	DefaultSticker = "CAACAgUAAxkBAANfZdi2KCTWFlynDPTbSoFw_rgEROUAAiYHAAJ9YeBUYjyPIdkXdGA0BA"
)

func InitRdb(rdbUrl string) {
	opt, _ := redis.ParseURL(rdbUrl)
	rdb = redis.NewClient(opt)
}

func SaveSticker(fileId string) error {
	// 将sticker fileId存储到Redis的集合类型中
	_, err := rdb.SAdd(StickerKey, fileId).Result()
	if err != nil {
		return err
	}
	return nil
}

func GetRandomSticker() (string, error) {
	// 从Redis的集合类型中随机获取一个值
	fileId, err := rdb.SRandMember(StickerKey).Result()
	if err != nil {
		return "", err
	}
	return fileId, nil
}
