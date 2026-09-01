package claims

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Store 是 claims 的持久化接口。抽成接口是为了让检查逻辑能脱离 Redis 测试。
type Store interface {
	Save(c *Claim) error
	Get(id string) (*Claim, error)
	All() ([]*Claim, error)
	Delete(id string) error
}

// ByChat 过滤出某个 chat 的 claims，按创建时间倒序（新的在前）。
func ByChat(all []*Claim, chatID int64) []*Claim {
	out := make([]*Claim, 0, len(all))
	for _, c := range all {
		if c.ChatID == chatID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Open 过滤出仍需关注的（追踪中 + 到期待结案）。
func Open(cs []*Claim) []*Claim {
	out := make([]*Claim, 0, len(cs))
	for _, c := range cs {
		if c.Status == StatusPending || c.Status == StatusExpired {
			out = append(out, c)
		}
	}
	return out
}

// ExportJSONL 导出成每行一条 JSON 的格式，字段名对齐 claims.jsonl 的约定
// （basis / falsify_cond / conclusion）。
func ExportJSONL(cs []*Claim) (string, error) {
	var b strings.Builder
	for _, c := range cs {
		line, err := json.Marshal(c)
		if err != nil {
			return "", err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// Stats 是一批 claims 的准确率统计。
type Stats struct {
	Total     int
	Pending   int
	Falsified int // 自动判定为错
	Correct   int
	Wrong     int
	Partial   int
}

// Resolved 返回已有结论的条数（自动证伪的也算）。
func (s Stats) Resolved() int { return s.Falsified + s.Correct + s.Wrong + s.Partial }

// Summarize 统计一批 claims 的结果分布。
func Summarize(cs []*Claim) Stats {
	var s Stats
	s.Total = len(cs)
	for _, c := range cs {
		switch {
		case c.Status == StatusFalsified:
			s.Falsified++
		case c.Status == StatusPending || c.Status == StatusExpired:
			s.Pending++
		case c.Outcome == OutcomeCorrect:
			s.Correct++
		case c.Outcome == OutcomeWrong:
			s.Wrong++
		case c.Outcome == OutcomePartial:
			s.Partial++
		}
	}
	return s
}

// Due 返回已过期但还挂在 pending 的 claims。
func Due(cs []*Claim, now time.Time) []*Claim {
	out := make([]*Claim, 0)
	for _, c := range cs {
		if c.Status == StatusPending && !now.Before(c.DueAt) {
			out = append(out, c)
		}
	}
	return out
}
