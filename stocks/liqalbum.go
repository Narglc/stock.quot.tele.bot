package stocks

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	log "github.com/narglc/stock.quot.tele.bot/pkg/logger"
	"gopkg.in/telebot.v3"
)

// liqCache 缓存清算热力图。
//
// 为什么这个缓存比别处更重要：Apify 的 CoinAnk actor 是**计费/计配额**的接口
// （免费档每 UTC 日 5 次），单次实测约 10 秒。热力图本身的时间粒度就是 15m——10 分钟内重复拉
// 取拿到的是同一份数据，纯属白烧配额。
//
// 注意：缓存里存的是 *LiqHeatmap 共享指针。ToMap / RenderLiq*PNG 都只读，
// 新增调用方时不要就地修改它，否则会污染所有后续读者。
var liqCache = newTTLCache[*LiqHeatmap](10 * time.Minute)

// liqInflight 保证同一币种同时只有一个 Apify 请求在跑。
// 并发穿透在这里代价特别高：两个人同时敲 /liqmap btc 就是两次计费调用。
var (
	liqInflightMu sync.Mutex
	liqInflight   = map[string]*sync.Mutex{}
)

// liqLock 取某币种专用的串行锁（不存在则创建）。
func liqLock(symbol string) *sync.Mutex {
	liqInflightMu.Lock()
	defer liqInflightMu.Unlock()
	mu, ok := liqInflight[symbol]
	if !ok {
		mu = &sync.Mutex{}
		liqInflight[symbol] = mu
	}
	return mu
}

// liqMapCachedOnly 只从缓存取热力图，**绝不触发 Apify 调用**。
//
// 给 MarketBrief 这类高频组装用：brief 每小时跑一次，真去打 Apify 一天就是 24 次，
// 而免费档一天只有 5 次。拿不到就让调用方留空。
func liqMapCachedOnly(symbol string) (*LiqHeatmap, bool) {
	sym, err := NormalizeSymbol(symbol)
	if err != nil {
		return nil, false
	}
	return liqCache.get(sym)
}

// symbolPattern 限制币种符号的形状：只允许字母数字、1~10 位。
var symbolPattern = regexp.MustCompile(`^[A-Z0-9]{1,10}$`)

// NormalizeSymbol 把用户输入的币种符号归一化成大写形式并校验形状。
//
// 校验放在最前面有两个作用：一是避免把用户输入直接拼进第三方接口的查询串，
// 二是无效输入能立刻被拒，不会白烧一次 Apify 配额。
func NormalizeSymbol(s string) (string, error) {
	sym := strings.ToUpper(strings.TrimSpace(s))
	if !symbolPattern.MatchString(sym) {
		return "", fmt.Errorf("不认识这个币种符号: %q（只支持字母数字，如 BTC / ETH / SOL）", s)
	}
	return sym, nil
}

// LiqAlbum 是清算图的一次完整产物：两张 PNG + 文字摘要 + 结构化数据。
type LiqAlbum struct {
	HeatmapPNG []byte
	MapPNG     []byte
	Caption    string
	Map        *LiqMap
}

// BuildLiqAlbum 拉数据 + 渲染两张图 + 生成摘要，供 /liqmap 与定时播报共用。
// 数据层带 10 分钟缓存（见 liqCache），重复调用不会重复消耗 Apify 配额。
func BuildLiqAlbum(symbol, apifyToken string, topN int) (*LiqAlbum, error) {
	h, err := GetLiqHeatmap(symbol, apifyToken)
	if err != nil {
		return nil, err
	}
	m := h.ToMap()

	heat, err := RenderLiqHeatmapPNG(h)
	if err != nil {
		return nil, fmt.Errorf("热力图渲染失败: %w", err)
	}
	mp, err := RenderLiqMapPNG(m)
	if err != nil {
		return nil, fmt.Errorf("清算地图渲染失败: %w", err)
	}

	return &LiqAlbum{
		HeatmapPNG: heat,
		MapPNG:     mp,
		Caption:    FormatLiqMap(m, topN),
		Map:        m,
	}, nil
}

// logLiqCacheHit 记一条缓存命中日志，方便观察配额到底省了多少。
func logLiqCacheHit(symbol string) {
	log.Infof("清算图命中缓存 symbol=%s（省下一次 Apify 调用）", symbol)
}

// LiqAlbumPhotos 把渲染产物转成 Telegram 相册。
//
// 每次调用都新建 bytes.Reader：Reader 上传一次就耗尽了，多群广播时不能复用同一个，
// 否则第二个群开始发出去的就是空文件。
func LiqAlbumPhotos(a *LiqAlbum) telebot.Album {
	return telebot.Album{
		&telebot.Photo{File: telebot.FromReader(bytes.NewReader(a.HeatmapPNG)), Caption: a.Caption},
		&telebot.Photo{File: telebot.FromReader(bytes.NewReader(a.MapPNG))},
	}
}
