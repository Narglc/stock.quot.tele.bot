// Package claims 记录「带证伪条件的判断」并追踪它后来对不对。
//
// 存在的理由：判断往往是刷手机看行情时冒出来的，等坐到电脑前打开编辑器写 JSON，
// 八成已经忘了或懒得写。Telegram 是这个场景摩擦最小的入口——发一条消息就记下了。
//
// 核心不是"记下来"，是**可证伪**：每条判断必须带一个能被推翻的条件，
// 否则就是「偏多/偏空」那种涨了算对跌了能找补的话，没有信息价值。
// 能解析成价格条件的会被自动检查，剩下的到期问本人。
package claims

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Status 是一条 claim 的生命周期状态。
type Status string

const (
	StatusPending   Status = "pending"   // 追踪中
	StatusFalsified Status = "falsified" // 证伪条件被触发——判断错了，自动判定
	StatusExpired   Status = "expired"   // 到期未被证伪，等本人结案
	StatusResolved  Status = "resolved"  // 本人已结案
)

// Outcome 是人工结案给出的结论。
type Outcome string

const (
	OutcomeCorrect Outcome = "correct"
	OutcomeWrong   Outcome = "wrong"
	OutcomePartial Outcome = "partially_wrong"
)

// ParseOutcome 把用户输入映射成结论，支持中英两种写法。
func ParseOutcome(s string) (Outcome, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "correct", "right", "ok", "对", "正确":
		return OutcomeCorrect, true
	case "wrong", "bad", "错", "错误":
		return OutcomeWrong, true
	case "partial", "partially_wrong", "半对", "部分正确", "部分":
		return OutcomePartial, true
	}
	return "", false
}

// PriceCond 是一个可被自动验证的价格条件，如 BTC < 118000。
type PriceCond struct {
	Symbol string  `json:"symbol"`
	Op     string  `json:"op"` // ">" ">=" "<" "<="
	Price  float64 `json:"price"`
}

// Met 判断当前价是否满足该条件。
func (c PriceCond) Met(price float64) bool {
	switch c.Op {
	case ">":
		return price > c.Price
	case ">=":
		return price >= c.Price
	case "<":
		return price < c.Price
	case "<=":
		return price <= c.Price
	}
	return false
}

func (c PriceCond) String() string {
	return fmt.Sprintf("%s %s %s", c.Symbol, c.Op, strconv.FormatFloat(c.Price, 'f', -1, 64))
}

// Claim 是一条带证伪条件的判断记录。
type Claim struct {
	ID       string `json:"id"`
	ChatID   int64  `json:"chat_id"`
	UserID   int64  `json:"user_id"`
	UserName string `json:"user_name"`

	Text    string     `json:"text"`            // 判断内容
	Basis   string     `json:"basis,omitempty"` // 依据（数据来源/推理依据）
	Falsify string     `json:"falsify_cond"`    // 证伪条件原文
	Cond    *PriceCond `json:"cond,omitempty"`  // 解析出的可自动验证条件

	CreatedAt   time.Time  `json:"created_at"`
	DueAt       time.Time  `json:"due_at"`
	Status      Status     `json:"status"`
	Outcome     Outcome    `json:"conclusion,omitempty"`
	Note        string     `json:"note,omitempty"`
	TriggeredAt *time.Time `json:"triggered_at,omitempty"` // 证伪条件被触发的时刻
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
}

// Auto 判断这条 claim 能否被自动验证。
func (c *Claim) Auto() bool { return c.Cond != nil }

// idAlphabet 去掉了容易看混的 0/O/1/I/L——这个 id 是要用户手打进 /resolve 的。
const idAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// NewID 生成一个 6 位的短 id。
func NewID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败极罕见；退化成时间戳派生，保证仍能产出一个 id。
		ns := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(ns >> (i * 8))
		}
	}
	out := make([]byte, 6)
	for i, v := range b {
		out[i] = idAlphabet[int(v)%len(idAlphabet)]
	}
	return string(out)
}

// 段前缀，中英各一套。第一段永远是判断内容本身。
var (
	falsifyKeys = []string{"证伪", "falsify", "f"}
	dueKeys     = []string{"到期", "due", "by"}
	basisKeys   = []string{"依据", "basis", "b"}
)

// ParseInput 解析 /claim 的参数。
//
// 形如：判断内容 | 证伪 BTC<118000 | 到期 3d | 依据 清算地图15m
// 用 `|` 分段而不是 --flag：手机上打 `|` 比打 `--falsify` 省事得多。
// 除判断内容外都可省略，到期默认 7 天。
func ParseInput(payload string, now time.Time) (*Claim, error) {
	parts := strings.Split(payload, "|")
	text := strings.TrimSpace(parts[0])
	if text == "" {
		return nil, fmt.Errorf("判断内容不能为空")
	}

	c := &Claim{
		Text:      text,
		CreatedAt: now,
		DueAt:     now.AddDate(0, 0, 7),
		Status:    StatusPending,
	}

	for _, seg := range parts[1:] {
		key, val, ok := splitSegment(seg)
		if !ok {
			continue
		}
		switch {
		case matchKey(key, falsifyKeys):
			c.Falsify = val
			if cond, cerr := ParseCond(val); cerr == nil {
				c.Cond = cond
			}
		case matchKey(key, dueKeys):
			due, derr := ParseDue(val, now)
			if derr != nil {
				return nil, derr
			}
			c.DueAt = due
		case matchKey(key, basisKeys):
			c.Basis = val
		}
	}

	if c.Falsify == "" {
		return nil, fmt.Errorf("缺少证伪条件")
	}
	return c, nil
}

// splitSegment 把 "证伪 BTC<118000" 拆成 key 和 value。
// 兼容 "证伪:" / "证伪：" / 空格三种写法。
func splitSegment(seg string) (key, val string, ok bool) {
	seg = strings.TrimSpace(seg)
	if seg == "" {
		return "", "", false
	}
	for _, sep := range []string{":", "：", " ", "\t"} {
		if i := strings.Index(seg, sep); i > 0 {
			return strings.TrimSpace(seg[:i]), strings.TrimSpace(seg[i+len(sep):]), true
		}
	}
	return "", "", false
}

func matchKey(key string, keys []string) bool {
	k := strings.ToLower(key)
	for _, want := range keys {
		if k == want {
			return true
		}
	}
	return false
}

// condRe 匹配 "BTC<118000" / "600519 >= 1700" / "BTC<11.8万" 这类条件。
var condRe = regexp.MustCompile(`^\s*([A-Za-z0-9]{1,10})\s*(>=|<=|>|<)\s*([0-9.]+)\s*([万wWkK])?\s*$`)

// ParseCond 尝试把证伪条件解析成可自动验证的价格条件。
// 解析不了返回错误——调用方据此把这条 claim 归入「到期问本人」那一类，而不是报错拒收。
func ParseCond(s string) (*PriceCond, error) {
	m := condRe.FindStringSubmatch(s)
	if m == nil {
		return nil, fmt.Errorf("无法解析成价格条件: %q", s)
	}
	price, err := strconv.ParseFloat(m[3], 64)
	if err != nil {
		return nil, err
	}
	switch m[4] {
	case "万":
		price *= 1e4
	case "w", "W":
		price *= 1e4
	case "k", "K":
		price *= 1e3
	}
	return &PriceCond{Symbol: strings.ToUpper(m[1]), Op: m[2], Price: price}, nil
}

// dueRe 匹配相对时长，如 3d / 12h / 2w。
var dueRe = regexp.MustCompile(`^\s*([0-9]+)\s*([hdwHDW])\s*$`)

// ParseDue 解析到期时间：相对时长（3d/12h/2w）或绝对日期（2026-09-10）。
func ParseDue(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if m := dueRe.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			return time.Time{}, fmt.Errorf("到期时长非法: %q", s)
		}
		switch strings.ToLower(m[2]) {
		case "h":
			return now.Add(time.Duration(n) * time.Hour), nil
		case "d":
			return now.AddDate(0, 0, n), nil
		case "w":
			return now.AddDate(0, 0, n*7), nil
		}
	}
	// 绝对日期按当地零点算，到期检查在当天结束时才触发，所以加一天。
	if t, err := time.ParseInLocation("2006-01-02", s, now.Location()); err == nil {
		return t.AddDate(0, 0, 1), nil
	}
	return time.Time{}, fmt.Errorf("到期格式不认识: %q（试试 3d / 12h / 2w / 2026-09-10）", s)
}
