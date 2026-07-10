package schedule

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/narglc/stock.quot.tele.bot/dao"
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"gopkg.in/telebot.v3"
)

// RegisterGroup 把 chat 记入内存 groupMap 并持久化到 Redis，进程重启后可恢复。
func RegisterGroup(chatID int64, info ChatInfo) {
	groupMu.Lock()
	groupMap[chatID] = info
	groupMu.Unlock()

	data, err := json.Marshal(info)
	if err != nil {
		log.Warnf("序列化 ChatInfo 失败 chat:%d err:%v", chatID, err)
		return
	}
	if err := dao.SaveGroup(chatID, string(data)); err != nil {
		log.Warnf("持久化群失败 chat:%d err:%v", chatID, err)
	}
}

// LoadGroups 启动时从 Redis 恢复已注册 chat 到内存 groupMap。
func LoadGroups() {
	groups, err := dao.LoadGroups()
	if err != nil {
		log.Warnf("加载已注册群失败: %v", err)
		return
	}
	groupMu.Lock()
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
		groupMap[id] = info
	}
	n := len(groupMap)
	groupMu.Unlock()
	log.Infof("已从 Redis 恢复 %d 个已注册群", n)
}

// UnregisterGroup 把 chat 从内存 groupMap 与 Redis 同时移除（bot 被踢/被拉黑时调用）。
func UnregisterGroup(chatID int64) {
	groupMu.Lock()
	delete(groupMap, chatID)
	groupMu.Unlock()
	if err := dao.RemoveGroup(chatID); err != nil {
		log.Warnf("注销群失败 chat:%d err:%v", chatID, err)
	}
}

// IsRegistered 判断某 chat 是否已注册（线程安全）。
func IsRegistered(chatID int64) bool {
	groupMu.RLock()
	defer groupMu.RUnlock()
	_, ok := groupMap[chatID]
	return ok
}

// GroupCount 返回已注册群数量（线程安全）。
func GroupCount() int {
	groupMu.RLock()
	defer groupMu.RUnlock()
	return len(groupMap)
}

// SnapshotGroupIDs 返回当前所有已注册 chat id 的快照副本。
// 广播时先取快照再逐个发送，避免在持锁期间做网络 I/O，也避免遍历中调用
// UnregisterGroup 造成的自死锁。
func SnapshotGroupIDs() []int64 {
	groupMu.RLock()
	defer groupMu.RUnlock()
	ids := make([]int64, 0, len(groupMap))
	for id := range groupMap {
		ids = append(ids, id)
	}
	return ids
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
