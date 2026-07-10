package schedule

import "sync"

// TaskList 是工作日定时提醒清单，时间为东八区（Asia/Shanghai）挂钟时间，
// 由 ScheduleTask 用 cron 注册（见 schedule.go）。
var TaskList = []Task{
	{"起床", "08:00", "懒虫们，该起床上班啦!!!"},
	{"午饭", "11:50", "该吃午饭了，兄弟们!!!"},
	{"中场", "15:00", "三点几啦！饮茶先啦！"},
	{"下班", "17:50", "准备下班了，老铁们!!!"},
	{"下班", "17:55", "仲有伍魂钟，激不激动，兴不兴奋???"},
	{"下班", "18:00", "那个女人在召唤! O神启动! "},
}

type ChatInfo struct {
	Type      string
	Title     string
	FirstName string
	LastName  string
	UserName  string
}

// groupMap 是已注册 chat 的内存态，被 handler（写）与 cron 定时任务（读）并发访问，
// 统一由 groupMu 保护。外部只能经本包导出的线程安全函数访问，不直接触碰 map。
var (
	groupMu  sync.RWMutex
	groupMap = make(map[int64]ChatInfo)
)
