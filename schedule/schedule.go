package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"github.com/robfig/cron/v3"
	"gopkg.in/telebot.v3"
)

type Task struct {
	Name string
	Time string
	Msg  string
}

// beijingLoc 是所有定时任务使用的时区（东八区）。
// TaskList / 整点播报均按此时区的挂钟时间触发，与运行机器的系统时区无关。
// 依赖 time/tzdata（main.go 内嵌）保证无系统 tz 库的容器也能加载。
var beijingLoc = mustLoadBeijing()

func mustLoadBeijing() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		// 理论上内嵌 tzdata 后不会失败；兜底用固定 +8 偏移，保证仍是东八区。
		log.Warnf("加载 Asia/Shanghai 失败，回落固定 UTC+8: %v", err)
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

// ScheduleTask 用 cron（东八区）注册所有定时任务并启动。
//   - 工作日 TaskList 的每个 HH:MM 各一条提醒任务；
//   - 每小时整点一次 BTC/ETH 行情播报，并在启动时立即播报一次。
func ScheduleTask(bot *telebot.Bot) {
	c := cron.New(cron.WithLocation(beijingLoc))

	// 工作日提醒：每个 Task 按 HH:MM 注册一条 cron（周一至周五）。
	for _, task := range TaskList {
		spec, err := hhmmToCronSpec(task.Time)
		if err != nil {
			log.Warnf("提醒任务时间非法，跳过 task:%s time:%s err:%v", task.Name, task.Time, err)
			continue
		}
		t := task // 捕获副本，避免闭包共享循环变量
		if _, err := c.AddFunc(spec, func() {
			log.Infof("触发提醒 task:%s time:%s", t.Name, t.Time)
			broadcastToGroups(bot, t.Msg)
		}); err != nil {
			log.Warnf("注册提醒任务失败 task:%s spec:%s err:%v", t.Name, spec, err)
		}
	}

	// 每小时整点行情播报。
	if _, err := c.AddFunc("0 * * * *", func() { broadcastCrypto(bot) }); err != nil {
		log.Warnf("注册 crypto 播报任务失败: %v", err)
	}

	c.Start()
	log.Infof("cron 定时任务已启动（时区 %s），共 %d 个提醒 + 每小时行情播报", beijingLoc, len(TaskList))

	// 启动后立即播报一次行情，方便验证、避免干等到下一个整点。
	go broadcastCrypto(bot)
}

// hhmmToCronSpec 把 "HH:MM" 转成工作日触发的 5 字段 cron 表达式（"M H * * 1-5"）。
func hhmmToCronSpec(hhmm string) (string, error) {
	parts := strings.Split(hhmm, ":")
	if len(parts) != 2 {
		return "", fmt.Errorf("非 HH:MM 格式: %q", hhmm)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return "", fmt.Errorf("小时非法: %q", hhmm)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return "", fmt.Errorf("分钟非法: %q", hhmm)
	}
	return fmt.Sprintf("%d %d * * 1-5", m, h), nil
}

// broadcastToGroups 向所有已注册群广播一条文本；发送失败且判定 bot 已被移出时自动注销。
// 先取 id 快照再发送，避免持锁做网络 I/O 及遍历中注销导致的死锁。
func broadcastToGroups(bot *telebot.Bot, msg string) {
	for _, group := range SnapshotGroupIDs() {
		if _, err := bot.Send(telebot.ChatID(group), msg); err != nil {
			log.Warnf("广播发送失败 group:%d err:%v", group, err)
			if IsBotEvicted(err) {
				UnregisterGroup(group)
				log.Infof("广播判定 bot 已被移出 group:%d，已注销", group)
			}
		}
	}
}
