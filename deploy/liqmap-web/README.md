# liqmap-web —— 清算热力图网页

`/liqmap` 发出去的是两张静态 PNG，看不了具体数值。这个 Worker 提供一个可交互的网页版。

主视图是**爆仓地图**：横轴价格，柱子高度是该价位堆积的待爆仓名义额（左轴）。叠加两条
**累计曲线**（右轴），从当前价分别向两边累计——读的是「价格走到这里，一路上一共会扫掉多少」。

累计曲线比单档柱子有用得多：单个价位堆了多少本身说明不了什么，**曲线斜率陡的那一段**
才是真正的密集区。hover 会同时给出本档量和从现价累计到该处的总量。

右侧「走到这里会扫掉多少」把 ±1/2/5% 对应的价格和累计量直接算好——这是最常问的问题，
不该让人自己去图上比划。数值后带 `+` 表示目标价超出了图表覆盖范围，实际只会更多。

顶部可切「最近一格 / 累计全部」和「地图 / 热力图」。手机上点按图表即可看数值（有触摸支持）。

### 币种

`ALLOWED_SYMBOLS` 默认只有 `BTC`，顶部标签栏在只有一个币种时自动隐藏。
这只控制标签显示——直接访问 `/ETH` 仍然可用，前提是 bot 推过该币种的快照
（发一次 `/liqmap ETH` 就有了）。

## 数据怎么流的

```
bot ──拉 Apify (10min 缓存)──> 热力图
 │
 └── PUT /api/heatmap/BTC ──> Worker ──> KV
                                         │
                              网页 GET ──┘（纯读，不碰 Apify）
```

**关键点：Worker 不持有 `APIFY_TOKEN`，也不会去调 Apify。**

Apify 的 CoinAnk actor 单次运行 60~90 秒且计费。如果让网页直连，每个访客打开页面都可能
烧掉一次配额——而 bot 侧本来就有 10 分钟缓存和同币种串行锁。所以保持 bot 作为唯一的
调用方，配额完全可控，Worker 只是个读缓存的壳。

代价是网页数据的新鲜度取决于 bot 上次拉取的时间（页面右上角会显示"N 分钟前更新"）。
想要刷新，在 Telegram 里发一次 `/liqmap BTC` 即可。定时播报（早八/晚八）也会自动推。

## 部署

```bash
cd deploy/liqmap-web
npm i -g wrangler        # 如果还没装

# 1. 建 KV namespace，把输出的 id 填进 wrangler.toml
wrangler kv namespace create LIQMAP

# 2. 设写入密钥（bot 侧要用同一个值）
wrangler secret put PUSH_TOKEN

# 3. 部署
wrangler deploy
```

部署完拿到形如 `https://liqmap-web.<你的账号>.workers.dev` 的地址。

## bot 侧配置

`config/config.yaml`：

```yaml
liqweb:
  url: "https://liqmap-web.xxx.workers.dev"   # 不要带结尾斜杠
  token: "与上面 PUSH_TOKEN 相同的值"
```

或者用环境变量 `LIQWEB_URL` / `LIQWEB_TOKEN`（优先级更高）。

**留空则完全不推送**，`/liqmap` 照常发 PNG，互不影响。

## 配置项

| 位置 | 名称 | 说明 |
|---|---|---|
| `wrangler.toml` `[vars]` | `ALLOWED_SYMBOLS` | 网页顶部展示哪些币种的切换标签，逗号分隔 |
| `wrangler secret` | `PUSH_TOKEN` | 写入密钥。缺失时 PUT 一律返回 500 |
| `wrangler.toml` KV | `LIQMAP` | 快照存储，单条 24h 过期 |

## 路由

| 方法 | 路径 | 说明 |
|---|---|---|
| `PUT` | `/api/heatmap/:symbol` | 写入快照，需 `X-Auth-Token` |
| `GET` | `/api/heatmap/:symbol` | 读快照 JSON（边缘缓存 60s） |
| `GET` | `/:symbol` | 热力图页面 |
| `GET` | `/` | 跳转到 `ALLOWED_SYMBOLS` 的第一个 |

## 快照格式

网格用**稀疏三元组**而非稠密二维数组——热力图绝大多数格子是 0，稠密序列化能到几 MB，
稀疏通常只有几十 KB：

```jsonc
{
  "symbol": "BTC",
  "interval": "15m",
  "currentPrice": 65000,
  "maxValue": 2100000000,
  "prices": [60000, 60100, ...],       // y 轴价格阶梯
  "times":  [1735689600000, ...],      // x 轴时间戳(ms)
  "cells":  [[12, 34, 21000000], ...], // [时间索引, 价格索引, 清算额]
  "candles":[{"ts":..,"o":..,"h":..,"l":..,"c":..}],
  "updatedAt": 1735689600000           // Worker 侧写入时补上
}
```

## 测试

```bash
npm test        # 页面纯逻辑：坐标换算、稀疏展开、颜色映射
```

canvas 的实际绘制没法在无头环境验证，但真正容易出错的是坐标换算（价格轴自下而上，
y 要从底部量起）、稀疏展开的越界处理、颜色的对数映射——这些都在 `src/worker.js` 的
`PURE:START/END` 标记块里，测试会把那段原样提取出来执行，保证测的就是线上跑的代码。

**改动那个块时不要删掉这两个标记**，也不要在块内引用 `document` / `window`。

## 排错

**页面显示「还没有快照」** —— bot 还没推过。在 TG 里发一次 `/liqmap BTC`，然后刷新。

**bot 日志里 `清算图快照推送被拒 ... HTTP 401`** —— `LIQWEB_TOKEN` 和 Worker 的
`PUSH_TOKEN` 不一致。

**HTTP 500 `server missing PUSH_TOKEN`** —— Worker 那边没设 secret，跑一次
`wrangler secret put PUSH_TOKEN`。

**顶部标签没有想看的币种** —— 改 `wrangler.toml` 的 `ALLOWED_SYMBOLS` 再 `wrangler deploy`。
注意直接访问 `/SOL` 是能用的，`ALLOWED_SYMBOLS` 只控制标签栏显示什么。
