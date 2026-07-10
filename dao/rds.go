package dao

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
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

	// opTimeout 单次 Redis 操作的超时，避免 Redis 卡住拖垮调用方 goroutine。
	opTimeout = 5 * time.Second
)

// opCtx 返回一次 Redis 操作用的带超时 context。调用方需 defer cancel()。
func opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), opTimeout)
}

// InitRdb 解析 redis URL 并建立客户端，启动时 Ping 验活；
// URL 非法或连不上直接 panic（fail-fast，避免带病启动到首次操作才暴露）。
func InitRdb(rdbUrl string) {
	opt, err := redis.ParseURL(rdbUrl)
	if err != nil {
		panic("invalid redis url: " + err.Error())
	}
	rdb = redis.NewClient(opt)

	ctx, cancel := opCtx()
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		panic("redis ping failed: " + err.Error())
	}
}

func SaveSticker(fileId string) error {
	// 将sticker fileId存储到Redis的集合类型中
	ctx, cancel := opCtx()
	defer cancel()
	return rdb.SAdd(ctx, StickerKey, fileId).Err()
}

func GetRandomSticker() (string, error) {
	// 从Redis的集合类型中随机获取一个值
	ctx, cancel := opCtx()
	defer cancel()
	return rdb.SRandMember(ctx, StickerKey).Result()
}

func SavePhotos(fileId string) error {
	// 将sticker fileId存储到Redis的集合类型中
	ctx, cancel := opCtx()
	defer cancel()
	return rdb.SAdd(ctx, PhotoKey, fileId).Err()
}

// SaveGroup 持久化一个已注册 chat，field 为 chatID，value 为序列化后的 ChatInfo。
func SaveGroup(chatID int64, infoJSON string) error {
	ctx, cancel := opCtx()
	defer cancel()
	return rdb.HSet(ctx, GroupKey, strconv.FormatInt(chatID, 10), infoJSON).Err()
}

// LoadGroups 读取所有已注册 chat，返回 chatID字符串 -> ChatInfo JSON。
func LoadGroups() (map[string]string, error) {
	ctx, cancel := opCtx()
	defer cancel()
	return rdb.HGetAll(ctx, GroupKey).Result()
}

// RemoveGroup 注销一个 chat（bot 被踢/被拉黑时清理）。
func RemoveGroup(chatID int64) error {
	ctx, cancel := opCtx()
	defer cancel()
	return rdb.HDel(ctx, GroupKey, strconv.FormatInt(chatID, 10)).Err()
}
