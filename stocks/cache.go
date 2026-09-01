package stocks

import (
	"sync"
	"time"
)

// ttlCache 是一个极简的进程内 TTL 缓存，用于给行情类接口挡住重复请求。
//
// 为什么需要：CoinGecko / alternative.me 都是免费无鉴权额度（CoinGecko 免费档
// 约 10~30 次/分钟），而 inline 查询是「用户每敲一个字符触发一次」的高频路径，
// 不缓存的话打一次 "@bot bitcoin" 就能发出十几个请求，极易被限流。
// 播报（每小时一次）不受 TTL 影响，/price 在 TTL 内复用结果也完全够用。
type ttlCache[T any] struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]cacheEntry[T]
}

type cacheEntry[T any] struct {
	val T
	exp time.Time
}

func newTTLCache[T any](ttl time.Duration) *ttlCache[T] {
	return &ttlCache[T]{ttl: ttl, m: make(map[string]cacheEntry[T])}
}

// get 返回未过期的缓存值；不存在或已过期返回 ok=false。
func (c *ttlCache[T]) get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Now().After(e.exp) {
		var zero T
		return zero, false
	}
	return e.val, true
}

// set 写入一个缓存值，有效期为 c.ttl。
func (c *ttlCache[T]) set(key string, val T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = cacheEntry[T]{val: val, exp: time.Now().Add(c.ttl)}
}
