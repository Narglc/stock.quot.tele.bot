package dao

import (
	"context"
	"strconv"
	"strings"
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
	// ClaimsKey 保存所有 claim（Hash field=claimID, value=Claim 的 JSON）
	ClaimsKey = "tg:gohome:claims"

	// WatchlistKey 保存各 chat 的 A股自选列表（Hash field=chatID, value=逗号分隔的 6 位代码）
	WatchlistKey = "tg:gohome:watchlist"

	// BroadcastMsgKey 保存各群最近一条整点行情消息 id（Hash field=chatID, value=messageID）。
	// 下次整点先删掉这条旧消息再静音发新的：群里任意时刻只有一条行情且始终在最底部，
	// 既不刷屏堆积，也不会像原地编辑那样被埋在聊天历史里看不见。
	BroadcastMsgKey = "tg:gohome:bcast"

	// opTimeout 单次 Redis 操作的超时，避免 Redis 卡住拖垮调用方 goroutine。
	opTimeout = 5 * time.Second

	// maxCollectionSize 是贡品集合（表情包/图片）的容量上限。
	// 这两个 Set 原本只增不减，长期运行会无限膨胀；超上限时随机弹掉多余成员。
	maxCollectionSize = 5000
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

// SaveSticker 收藏一个 sticker 的 fileID（带容量上限）。
func SaveSticker(fileId string) error {
	return addCapped(StickerKey, fileId)
}

// addCapped 把 member 加入 Set，并在超出 maxCollectionSize 时随机弹掉多余成员。
// 修剪失败不算写入失败——收藏本身已经成功了。
func addCapped(key, member string) error {
	ctx, cancel := opCtx()
	defer cancel()

	if err := rdb.SAdd(ctx, key, member).Err(); err != nil {
		return err
	}

	n, err := rdb.SCard(ctx, key).Result()
	if err != nil || n <= maxCollectionSize {
		return nil
	}
	if err := rdb.SPopN(ctx, key, n-maxCollectionSize).Err(); err != nil {
		return nil
	}
	return nil
}

func GetRandomSticker() (string, error) {
	// 从Redis的集合类型中随机获取一个值
	ctx, cancel := opCtx()
	defer cancel()
	return rdb.SRandMember(ctx, StickerKey).Result()
}

// SavePhotos 收藏一张用户发来的图片 fileID（带容量上限）。
func SavePhotos(fileId string) error {
	return addCapped(PhotoKey, fileId)
}

// GetRandomPhoto 从收藏的图片里随机取一个 fileID。
func GetRandomPhoto() (string, error) {
	ctx, cancel := opCtx()
	defer cancel()
	return rdb.SRandMember(ctx, PhotoKey).Result()
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

// SaveBroadcastMsg 记录某群最近一条整点行情消息 id（供下次整点删除旧消息）。
func SaveBroadcastMsg(chatID int64, msgID string) error {
	ctx, cancel := opCtx()
	defer cancel()
	return rdb.HSet(ctx, BroadcastMsgKey, strconv.FormatInt(chatID, 10), msgID).Err()
}

// GetBroadcastMsg 取某群最近一条整点行情消息 id；不存在返回 redis.Nil 错误。
func GetBroadcastMsg(chatID int64) (string, error) {
	ctx, cancel := opCtx()
	defer cancel()
	return rdb.HGet(ctx, BroadcastMsgKey, strconv.FormatInt(chatID, 10)).Result()
}

// DelBroadcastMsg 删除某群的整点行情消息 id 记录（群注销/消息失效时）。
func DelBroadcastMsg(chatID int64) {
	ctx, cancel := opCtx()
	defer cancel()
	_ = rdb.HDel(ctx, BroadcastMsgKey, strconv.FormatInt(chatID, 10)).Err()
}

// SaveWatchlist 保存某 chat 的 A股自选列表；codes 为空则删除该条目。
func SaveWatchlist(chatID int64, codes []string) error {
	ctx, cancel := opCtx()
	defer cancel()
	field := strconv.FormatInt(chatID, 10)
	if len(codes) == 0 {
		return rdb.HDel(ctx, WatchlistKey, field).Err()
	}
	return rdb.HSet(ctx, WatchlistKey, field, strings.Join(codes, ",")).Err()
}

// GetWatchlist 读取某 chat 的 A股自选列表；未设置返回空切片（不是错误）。
func GetWatchlist(chatID int64) ([]string, error) {
	ctx, cancel := opCtx()
	defer cancel()
	v, err := rdb.HGet(ctx, WatchlistKey, strconv.FormatInt(chatID, 10)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return splitCodes(v), nil
}

// LoadAllWatchlists 读取所有 chat 的自选列表，供收盘播报遍历。
func LoadAllWatchlists() (map[int64][]string, error) {
	ctx, cancel := opCtx()
	defer cancel()
	raw, err := rdb.HGetAll(ctx, WatchlistKey).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]string, len(raw))
	for idStr, v := range raw {
		id, perr := strconv.ParseInt(idStr, 10, 64)
		if perr != nil {
			continue
		}
		if codes := splitCodes(v); len(codes) > 0 {
			out[id] = codes
		}
	}
	return out, nil
}

// splitCodes 把逗号分隔的代码串拆成切片，忽略空项。
func splitCodes(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SaveClaim 写入/更新一条 claim。
func SaveClaim(id, jsonStr string) error {
	ctx, cancel := opCtx()
	defer cancel()
	return rdb.HSet(ctx, ClaimsKey, id, jsonStr).Err()
}

// GetClaim 读取一条 claim；不存在返回 redis.Nil。
func GetClaim(id string) (string, error) {
	ctx, cancel := opCtx()
	defer cancel()
	return rdb.HGet(ctx, ClaimsKey, id).Result()
}

// LoadClaims 读取全部 claim（id -> JSON）。
func LoadClaims() (map[string]string, error) {
	ctx, cancel := opCtx()
	defer cancel()
	return rdb.HGetAll(ctx, ClaimsKey).Result()
}

// DeleteClaim 删除一条 claim。
func DeleteClaim(id string) error {
	ctx, cancel := opCtx()
	defer cancel()
	return rdb.HDel(ctx, ClaimsKey, id).Err()
}

// CloseRdb 关闭 redis 连接（优雅退出时调用）。
func CloseRdb() error {
	if rdb != nil {
		return rdb.Close()
	}
	return nil
}
