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

// cronRunner 持有当前 cron 实例，供优雅退出时 StopTasks 停止。
var cronRunner *cron.Cron

// StopTasks 停止所有定时任务（优雅退出用），并等待正在执行的任务真正结束。
//
// 必须等待：cron.Stop() 只是停止调度、立即返回，正在跑的 broadcastCrypto 仍在
// 访问 Redis。若不等就 CloseRdb()，那个 goroutine 会拿到一个已关闭的客户端。
func StopTasks() {
	if cronRunner == nil {
		return
	}
	ctx := cronRunner.Stop()
	select {
	case <-ctx.Done():
	case <-time.After(stopTimeout):
		log.Warnf("等待定时任务结束超时(%s)，继续关闭", stopTimeout)
	}
}

// stopTimeout 是优雅退出时等待在途任务的上限，避免某个卡住的任务拖住整个进程退出。
const stopTimeout = 10 * time.Second

// ScheduleTask 用 cron（东八区）注册所有定时任务并启动。
//   - 工作日 TaskList 的每个 HH:MM 各一条提醒任务；
//   - 每小时整点检测一次行情事件（有异动才播报），启动时先跑一次建立基线；
//   - 每天早八一条完整行情面板（保底心跳）；
//   - 每交易日 15:05 一次 A股自选股收盘播报；
//   - 每小时 30 分检查一次 claims 的证伪条件与到期；
//   - apifyToken 非空时，早八/晚八各一次 BTC 清算图播报。
func ScheduleTask(bot *telebot.Bot, apifyToken string) {
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

	// 每小时整点检测行情事件（无异动则静默）。
	if _, err := c.AddFunc("0 * * * *", func() { checkCryptoAlerts(bot) }); err != nil {
		log.Warnf("注册行情事件检测任务失败: %v", err)
	}

	// 每天早八一条完整面板作为保底心跳（纯静默模式下无法区分「没异动」和「bot 挂了」）。
	if _, err := c.AddFunc("0 8 * * *", func() { dailyDigest(bot) }); err != nil {
		log.Warnf("注册每日行情面板任务失败: %v", err)
	}

	// claims 自动检查：每小时一次，与行情事件检测同频。
	if _, err := c.AddFunc("30 * * * *", func() { checkClaims(bot) }); err != nil {
		log.Warnf("注册 claims 检查任务失败: %v", err)
	}

	// A股收盘播报：每交易日 15:05（收盘后 5 分钟，确保拿到定盘数据）。
	// 节假日靠「全市场无成交」在 broadcastAShare 里再判一次。
	if _, err := c.AddFunc("5 15 * * 1-5", func() { broadcastAShare(bot) }); err != nil {
		log.Warnf("注册 A股收盘播报任务失败: %v", err)
	}

	// 早八/晚八清算图播报（需配置 APIFY_TOKEN）。
	if apifyToken != "" {
		for _, spec := range []string{"0 8 * * *", "0 20 * * *"} {
			s := spec
			if _, err := c.AddFunc(s, func() { broadcastLiqmap(bot, "BTC", apifyToken) }); err != nil {
				log.Warnf("注册清算图任务失败 %s: %v", s, err)
			}
		}
	}

	cronRunner = c
	c.Start()
	log.Infof("cron 定时任务已启动（时区 %s），共 %d 个提醒 + 每小时事件检测 + 每日面板 + 清算图:%v",
		beijingLoc, len(TaskList), apifyToken != "")

	// 启动后立即跑一次事件检测：此时没有基线，只会记录当前状态、不产生事件
	// （见 stocks.DetectAlerts），所以重启不会刷一堆「进入极端区」的误报。
	go checkCryptoAlerts(bot)
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
