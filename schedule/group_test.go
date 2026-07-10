package schedule

import (
	"sync"
	"testing"
)

// TestGroupMapConcurrentAccess 并发地读写 groupMap，配合 `go test -race` 验证无数据竞争。
// 覆盖之前的 bug：cron 定时任务遍历 map 时 handler 同时增删导致的 concurrent map read/write。
func TestGroupMapConcurrentAccess(t *testing.T) {
	const workers = 8
	const iters = 500

	var wg sync.WaitGroup
	wg.Add(workers * 3)

	// 写：注册（只碰内存路径不触 Redis：直接操作受锁 map，与 RegisterGroup 同锁）
	for w := 0; w < workers; w++ {
		go func(base int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				id := int64(base*iters + i)
				groupMu.Lock()
				groupMap[id] = ChatInfo{Title: "t"}
				groupMu.Unlock()
			}
		}(w)
	}

	// 读：快照 + 计数 + 成员判断（模拟 cron 广播与 handler 查询）
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				_ = SnapshotGroupIDs()
				_ = GroupCount()
				_ = IsRegistered(int64(i))
			}
		}()
	}

	// 删：清理受锁 map（与 UnregisterGroup 同锁路径）
	for w := 0; w < workers; w++ {
		go func(base int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				id := int64(base*iters + i)
				groupMu.Lock()
				delete(groupMap, id)
				groupMu.Unlock()
			}
		}(w)
	}

	wg.Wait()
}
