package handler

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/narglc/stock.quot.tele.bot/dao"
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/narglc/stock.quot.tele.bot/stocks"
	tele "gopkg.in/telebot.v3"
)

// beijingLoc A股的时间口径固定为东八区，与运行机器的系统时区无关。
var beijingLoc = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

const watchUsage = `用法：
/watch              查看自选股行情
/watch add 600519 000001   加入自选（可一次多个）
/watch del 600519          移出自选
/watch clear               清空自选`

// Watch 处理 /watch：管理并查询 A股自选股。
//
// 自选列表按 chat 存（不是按人）：群里是共享的一份，私聊是你自己的一份。
func Watch(c tele.Context) error {
	chat := c.Chat()
	fields := strings.Fields(strings.TrimSpace(c.Message().Payload))

	log.Infof("sender:[%d - %s] chat:[%d - %s] /watch %v",
		c.Sender().ID, c.Sender().FirstName, chat.ID, chat.Title, fields)

	if len(fields) == 0 {
		return showWatchlist(c, chat.ID)
	}

	switch strings.ToLower(fields[0]) {
	case "add":
		return addWatch(c, chat.ID, fields[1:])
	case "del", "rm", "remove":
		return delWatch(c, chat.ID, fields[1:])
	case "clear":
		if err := dao.SaveWatchlist(chat.ID, nil); err != nil {
			return c.Send("清空失败了 🥲：" + err.Error())
		}
		return c.Send("自选股已清空")
	default:
		return c.Send(watchUsage)
	}
}

// showWatchlist 查询并展示当前自选股行情。
func showWatchlist(c tele.Context, chatID int64) error {
	codes, err := dao.GetWatchlist(chatID)
	if err != nil {
		return c.Send("读取自选股失败了 🥲：" + err.Error())
	}
	if len(codes) == 0 {
		return c.Send("自选股是空的\n\n" + watchUsage)
	}

	quotes, err := stocks.GetStockQuotes(codes)
	if err != nil {
		log.Warnf("/watch 拉行情失败: %v", err)
		return c.Send("行情拉取失败了，稍后再试试 🥲")
	}
	return c.Send(stocks.FormatStockMessage(quotes, time.Now().In(beijingLoc)))
}

// addWatch 往自选里加代码，去重并受 MaxWatchlistSize 限制。
func addWatch(c tele.Context, chatID int64, args []string) error {
	if len(args) == 0 {
		return c.Send("要加哪只？例：/watch add 600519")
	}

	codes, err := dao.GetWatchlist(chatID)
	if err != nil {
		return c.Send("读取自选股失败了 🥲：" + err.Error())
	}

	var added, bad []string
	for _, a := range args {
		code, cerr := stocks.NormalizeStockCode(a)
		if cerr != nil {
			bad = append(bad, a)
			continue
		}
		if slices.Contains(codes, code) {
			continue
		}
		if len(codes) >= stocks.MaxWatchlistSize {
			return c.Send(fmt.Sprintf("自选股最多 %d 只，先删几只再加吧", stocks.MaxWatchlistSize))
		}
		codes = append(codes, code)
		added = append(added, code)
	}

	if len(added) > 0 {
		if err := dao.SaveWatchlist(chatID, codes); err != nil {
			return c.Send("保存失败了 🥲：" + err.Error())
		}
	}

	var b strings.Builder
	if len(added) > 0 {
		b.WriteString("已加入自选：" + strings.Join(added, " "))
	} else {
		b.WriteString("没有新增（可能已在自选里）")
	}
	if len(bad) > 0 {
		b.WriteString("\n❓ 这些不是合法代码：" + strings.Join(bad, " ") + "\nA股代码是 6 位数字，如 600519")
	}
	b.WriteString(fmt.Sprintf("\n当前共 %d 只", len(codes)))
	return c.Send(b.String())
}

// delWatch 从自选里移除代码。
func delWatch(c tele.Context, chatID int64, args []string) error {
	if len(args) == 0 {
		return c.Send("要删哪只？例：/watch del 600519")
	}

	codes, err := dao.GetWatchlist(chatID)
	if err != nil {
		return c.Send("读取自选股失败了 🥲：" + err.Error())
	}
	if len(codes) == 0 {
		return c.Send("自选股本来就是空的")
	}

	var removed []string
	for _, a := range args {
		code, cerr := stocks.NormalizeStockCode(a)
		if cerr != nil {
			continue
		}
		if i := slices.Index(codes, code); i >= 0 {
			codes = slices.Delete(codes, i, i+1)
			removed = append(removed, code)
		}
	}

	if len(removed) == 0 {
		return c.Send("没找到要删的代码")
	}
	if err := dao.SaveWatchlist(chatID, codes); err != nil {
		return c.Send("保存失败了 🥲：" + err.Error())
	}
	return c.Send(fmt.Sprintf("已移出自选：%s\n当前共 %d 只", strings.Join(removed, " "), len(codes)))
}
