package dao

import (
	"strconv"

	"github.com/go-redis/redis"
)

var (
	rdb *redis.Client
)

const (
	StickerKey     = "tg:gohome:stickers"
	DefaultSticker = "CAACAgUAAxkBAANfZdi2KCTWFlynDPTbSoFw_rgEROUAAiYHAAJ9YeBUYjyPIdkXdGA0BA"
	PhotoKey       = "tg:gohome:pics"
	// GroupKey 保存已 /register 的 chat：Hash field=chatID, value=ChatInfo 的 JSON
	GroupKey = "tg:gohome:groups"
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

func SavePhotos(fileId string) error {
	// 将sticker fileId存储到Redis的集合类型中
	_, err := rdb.SAdd(PhotoKey, fileId).Result()
	if err != nil {
		return err
	}
	return nil
}

// SaveGroup 持久化一个已注册 chat，field 为 chatID，value 为序列化后的 ChatInfo。
func SaveGroup(chatID int64, infoJSON string) error {
	return rdb.HSet(GroupKey, strconv.FormatInt(chatID, 10), infoJSON).Err()
}

// LoadGroups 读取所有已注册 chat，返回 chatID字符串 -> ChatInfo JSON。
func LoadGroups() (map[string]string, error) {
	return rdb.HGetAll(GroupKey).Result()
}

// RemoveGroup 注销一个 chat（bot 被踢/被拉黑时清理）。
func RemoveGroup(chatID int64) error {
	return rdb.HDel(GroupKey, strconv.FormatInt(chatID, 10)).Err()
}
