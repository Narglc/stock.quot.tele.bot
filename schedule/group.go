package schedule

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/narglc/stock.quot.tele.bot/dao"
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"gopkg.in/telebot.v3"
)

// RegisterGroup 把 chat 记入内存 GroupMap 并持久化到 Redis，进程重启后可恢复。
func RegisterGroup(chatID int64, info ChatInfo) {
	GroupMap[chatID] = info

	data, err := json.Marshal(info)
	if err != nil {
		log.Warnf("序列化 ChatInfo 失败 chat:%d err:%v", chatID, err)
		return
	}
	if err := dao.SaveGroup(chatID, string(data)); err != nil {
		log.Warnf("持久化群失败 chat:%d err:%v", chatID, err)
	}
}

// LoadGroups 启动时从 Redis 恢复已注册 chat 到内存 GroupMap。
func LoadGroups() {
	groups, err := dao.LoadGroups()
	if err != nil {
		log.Warnf("加载已注册群失败: %v", err)
		return
	}
	for idStr, data := range groups {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			log.Warnf("解析群ID失败 id:%s err:%v", idStr, err)
			continue
		}
		var info ChatInfo
		if err := json.Unmarshal([]byte(data), &info); err != nil {
			log.Warnf("反序列化 ChatInfo 失败 id:%s err:%v", idStr, err)
			continue
		}
		GroupMap[id] = info
	}
	log.Infof("已从 Redis 恢复 %d 个已注册群", len(GroupMap))
}

// UnregisterGroup 把 chat 从内存 GroupMap 与 Redis 同时移除（bot 被踢/被拉黑时调用）。
func UnregisterGroup(chatID int64) {
	delete(GroupMap, chatID)
	if err := dao.RemoveGroup(chatID); err != nil {
		log.Warnf("注销群失败 chat:%d err:%v", chatID, err)
	}
}

// IsBotEvicted 判断广播发送错误是否意味着 bot 已无法触达该 chat
// （被踢出群/频道、被用户拉黑、会话不存在、账号注销），据此自愈注销。
func IsBotEvicted(err error) bool {
	return errors.Is(err, telebot.ErrKickedFromGroup) ||
		errors.Is(err, telebot.ErrKickedFromSuperGroup) ||
		errors.Is(err, telebot.ErrKickedFromChannel) ||
		errors.Is(err, telebot.ErrBlockedByUser) ||
		errors.Is(err, telebot.ErrChatNotFound) ||
		errors.Is(err, telebot.ErrUserIsDeactivated)
}
