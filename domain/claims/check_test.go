package claims

import (
	"errors"
	"testing"
	"time"
)

// memStore 是内存版 Store，让检查逻辑能脱离 Redis 测试。
type memStore struct {
	m map[string]*Claim
}

func newMem(cs ...*Claim) *memStore {
	s := &memStore{m: map[string]*Claim{}}
	for _, c := range cs {
		s.m[c.ID] = c
	}
	return s
}
func (s *memStore) Save(c *Claim) error { s.m[c.ID] = c; return nil }
func (s *memStore) Get(id string) (*Claim, error) {
	c, ok := s.m[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return c, nil
}
func (s *memStore) All() ([]*Claim, error) {
	out := make([]*Claim, 0, len(s.m))
	for _, c := range s.m {
		out = append(out, c)
	}
	return out, nil
}
func (s *memStore) Delete(id string) error { delete(s.m, id); return nil }

func pending(id string, cond *PriceCond, due time.Time) *Claim {
	return &Claim{ID: id, ChatID: 1, Text: "t", Falsify: "f", Cond: cond,
		CreatedAt: base, DueAt: due, Status: StatusPending}
}

func fixedPrice(p float64) PriceFn {
	return func(string) (float64, error) { return p, nil }
}

func TestCheck_Falsified(t *testing.T) {
	c := pending("A", &PriceCond{Symbol: "BTC", Op: "<", Price: 100}, base.AddDate(0, 0, 7))
	s := newMem(c)

	events, err := Check(s, fixedPrice(99), base)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != EventFalsified {
		t.Fatalf("应产生一条证伪事件，实得 %+v", events)
	}
	if c.Status != StatusFalsified {
		t.Errorf("状态应为 falsified，实得 %s", c.Status)
	}
	if c.Outcome != OutcomeWrong {
		t.Errorf("被证伪即判定为错，实得 %s", c.Outcome)
	}
	if c.TriggeredAt == nil {
		t.Error("应记录触发时刻")
	}
}

func TestCheck_ConditionNotMet(t *testing.T) {
	c := pending("A", &PriceCond{Symbol: "BTC", Op: "<", Price: 100}, base.AddDate(0, 0, 7))
	s := newMem(c)

	events, _ := Check(s, fixedPrice(101), base)
	if len(events) != 0 {
		t.Errorf("条件未满足不该有事件，实得 %+v", events)
	}
	if c.Status != StatusPending {
		t.Errorf("状态不该变，实得 %s", c.Status)
	}
}

func TestCheck_Expired(t *testing.T) {
	c := pending("A", nil, base.Add(-time.Hour)) // 已过期，无自动条件
	s := newMem(c)

	events, _ := Check(s, fixedPrice(1), base)
	if len(events) != 1 || events[0].Kind != EventExpired {
		t.Fatalf("应产生到期事件，实得 %+v", events)
	}
	if c.Status != StatusExpired {
		t.Errorf("状态应为 expired，实得 %s", c.Status)
	}
	// 到期不等于错，结论要留空让本人来填
	if c.Outcome != "" {
		t.Errorf("到期不该自动给结论，实得 %s", c.Outcome)
	}
}

// 被证伪优先于到期：被推翻是确定结论，到期只是「时间到了你自己看」。
func TestCheck_FalsifiedBeatsExpired(t *testing.T) {
	c := pending("A", &PriceCond{Symbol: "BTC", Op: "<", Price: 100}, base.Add(-time.Hour))
	s := newMem(c)

	events, _ := Check(s, fixedPrice(99), base)
	if len(events) != 1 || events[0].Kind != EventFalsified {
		t.Fatalf("应判定为证伪而非到期，实得 %+v", events)
	}
}

// 取价失败只跳过本轮，下小时再查——不能因为一次网络抖动就误判。
func TestCheck_PriceErrorSkipsQuietly(t *testing.T) {
	c := pending("A", &PriceCond{Symbol: "BTC", Op: "<", Price: 100}, base.AddDate(0, 0, 7))
	s := newMem(c)

	failing := func(string) (float64, error) { return 0, errors.New("网络炸了") }
	events, err := Check(s, failing, base)
	if err != nil {
		t.Fatalf("单个取价失败不该让整轮检查失败: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("取价失败不该产生事件，实得 %+v", events)
	}
	if c.Status != StatusPending {
		t.Errorf("状态应保持 pending，实得 %s", c.Status)
	}
}

func TestCheck_SkipsNonPending(t *testing.T) {
	done := pending("A", &PriceCond{Symbol: "BTC", Op: "<", Price: 100}, base.Add(-time.Hour))
	done.Status = StatusResolved
	s := newMem(done)

	events, _ := Check(s, fixedPrice(99), base)
	if len(events) != 0 {
		t.Errorf("已结案的不该被重新处理，实得 %+v", events)
	}
}

func TestResolve(t *testing.T) {
	c := pending("A", nil, base)
	s := newMem(c)

	got, err := Resolve(s, "A", OutcomePartial, "方向对了但时间早了", base)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusResolved || got.Outcome != OutcomePartial {
		t.Errorf("结案结果错误: %+v", got)
	}
	if got.Note != "方向对了但时间早了" || got.ResolvedAt == nil {
		t.Errorf("说明/时间未记录: %+v", got)
	}

	if _, err := Resolve(s, "NOPE", OutcomeCorrect, "", base); err == nil {
		t.Error("不存在的 id 应当报错")
	}
}

func TestSummarize(t *testing.T) {
	mk := func(st Status, o Outcome) *Claim { return &Claim{Status: st, Outcome: o} }
	s := Summarize([]*Claim{
		mk(StatusPending, ""),
		mk(StatusExpired, ""),
		mk(StatusFalsified, OutcomeWrong),
		mk(StatusResolved, OutcomeCorrect),
		mk(StatusResolved, OutcomeCorrect),
		mk(StatusResolved, OutcomePartial),
	})
	if s.Total != 6 || s.Pending != 2 || s.Falsified != 1 || s.Correct != 2 || s.Partial != 1 {
		t.Errorf("统计错误: %+v", s)
	}
	if s.Resolved() != 4 {
		t.Errorf("已结论数 = %d, want 4", s.Resolved())
	}
}

func TestByChat_And_Open(t *testing.T) {
	a := &Claim{ID: "A", ChatID: 1, Status: StatusPending, CreatedAt: base}
	b := &Claim{ID: "B", ChatID: 2, Status: StatusPending, CreatedAt: base}
	c := &Claim{ID: "C", ChatID: 1, Status: StatusResolved, CreatedAt: base.Add(time.Hour)}

	mine := ByChat([]*Claim{a, b, c}, 1)
	if len(mine) != 2 {
		t.Fatalf("应只含 chat 1 的两条，实得 %d", len(mine))
	}
	if mine[0].ID != "C" {
		t.Errorf("应按创建时间倒序，实得首条 %s", mine[0].ID)
	}
	if open := Open(mine); len(open) != 1 || open[0].ID != "A" {
		t.Errorf("Open 应只留追踪中的，实得 %+v", open)
	}
}

func TestExportJSONL(t *testing.T) {
	out, err := ExportJSONL([]*Claim{
		{ID: "A", Text: "t1", Falsify: "f1", Basis: "b1"},
		{ID: "B", Text: "t2", Falsify: "f2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, l := range []byte(out) {
		if l == '\n' {
			lines++
		}
	}
	if lines != 2 {
		t.Errorf("应有 2 行，实得 %d", lines)
	}
	// 字段名要对齐 claims.jsonl 的约定
	for _, want := range []string{`"falsify_cond"`, `"basis"`, `"text"`} {
		if !contains(out, want) {
			t.Errorf("导出缺少字段 %s:\n%s", want, out)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestDue(t *testing.T) {
	a := pending("A", nil, base.Add(-time.Hour))
	b := pending("B", nil, base.Add(time.Hour))
	got := Due([]*Claim{a, b}, base)
	if len(got) != 1 || got[0].ID != "A" {
		t.Errorf("只有 A 过期，实得 %+v", got)
	}
}
