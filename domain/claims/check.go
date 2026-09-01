package claims

import (
	"time"
)

// PriceFn 查某标的当前价。注入而非直接依赖 stocks 包：
// 一是让「crypto 还是 A股」的分派留在调用方，二是检查逻辑能脱离网络测试。
type PriceFn func(symbol string) (float64, error)

// Event 是一次检查产生的状态变化，交给调用方去通知用户。
type Event struct {
	Claim *Claim
	Kind  EventKind
	Price float64 // 触发时的价格（Kind=Falsified 时有意义）
}

type EventKind string

const (
	// EventFalsified 证伪条件被触发——判断被推翻了，这是这套东西真正的价值所在。
	EventFalsified EventKind = "falsified"
	// EventExpired 到期仍未被证伪，需要本人来结案。
	EventExpired EventKind = "expired"
)

// Check 遍历所有追踪中的 claim，更新状态并返回需要通知的事件。
//
// 两类处理：
//   - 能自动验证的（有 Cond）：查现价，条件满足即判定证伪，立刻通知；
//   - 到期的：不管能不能自动验证，都转成 expired 让本人结案。
//
// 取价失败只跳过本轮，不改状态——下个小时还会再查一次。
func Check(store Store, price PriceFn, now time.Time) ([]Event, error) {
	all, err := store.All()
	if err != nil {
		return nil, err
	}

	var events []Event
	for _, c := range all {
		if c.Status != StatusPending {
			continue
		}

		// 先看是否已被证伪：这比「到期」优先，因为被推翻是确定结论，
		// 而到期只是「时间到了，你自己看着办」。
		if c.Cond != nil {
			p, perr := price(c.Cond.Symbol)
			if perr == nil && c.Cond.Met(p) {
				t := now
				c.Status = StatusFalsified
				c.TriggeredAt = &t
				c.Outcome = OutcomeWrong
				if serr := store.Save(c); serr != nil {
					return events, serr
				}
				events = append(events, Event{Claim: c, Kind: EventFalsified, Price: p})
				continue
			}
		}

		if !now.Before(c.DueAt) {
			c.Status = StatusExpired
			if serr := store.Save(c); serr != nil {
				return events, serr
			}
			events = append(events, Event{Claim: c, Kind: EventExpired})
		}
	}
	return events, nil
}

// Resolve 人工结案。
func Resolve(store Store, id string, outcome Outcome, note string, now time.Time) (*Claim, error) {
	c, err := store.Get(id)
	if err != nil {
		return nil, err
	}
	c.Status = StatusResolved
	c.Outcome = outcome
	c.Note = note
	c.ResolvedAt = &now
	if err := store.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}
