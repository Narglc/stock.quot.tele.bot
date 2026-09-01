package claims

import (
	"encoding/json"
	"fmt"

	"github.com/narglc/stock.quot.tele.bot/dao"
	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
)

// RedisStore 是 Store 的 Redis 实现（全部 claim 存在一个 Hash 里）。
// 量级预期是几百条，全量读出来在内存里过滤足够快，不值得再建索引。
type RedisStore struct{}

func NewRedisStore() *RedisStore { return &RedisStore{} }

func (RedisStore) Save(c *Claim) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return dao.SaveClaim(c.ID, string(b))
}

func (RedisStore) Get(id string) (*Claim, error) {
	raw, err := dao.GetClaim(id)
	if err != nil {
		return nil, fmt.Errorf("没找到 claim %s", id)
	}
	var c Claim
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (RedisStore) All() ([]*Claim, error) {
	raw, err := dao.LoadClaims()
	if err != nil {
		return nil, err
	}
	out := make([]*Claim, 0, len(raw))
	for id, v := range raw {
		var c Claim
		if uerr := json.Unmarshal([]byte(v), &c); uerr != nil {
			// 单条坏数据不该让整个列表读不出来。
			log.Warnf("claim %s 反序列化失败，跳过: %v", id, uerr)
			continue
		}
		out = append(out, &c)
	}
	return out, nil
}

func (RedisStore) Delete(id string) error { return dao.DeleteClaim(id) }
