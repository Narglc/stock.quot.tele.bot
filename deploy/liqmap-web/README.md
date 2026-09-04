# liqmap-web —— 爆仓地图网页

`/liqmap` 发出去的是两张静态 PNG，看不了具体数值。这个 Worker 提供一个可交互的网页版。

## 数据是什么（先看这个）

CoinAnk 基于 **Binance 合约持仓推算的待爆仓分布** —— 是对"价格走到某处会触发多少清算"的
**预估**，不是已经发生的清算记录。Apify actor `api_merge/coinank-liquidation-heatmap`
只覆盖 Binance 一家，没有交易所选择参数；BTC 合约 Binance 大约占全市场三成多，
看的时候别当全市场。

actor 输出里**没有现价字段**，现价是 bot 拉 Apify 的同一刻从 OKX 永续取的，与快照时间一致。
页面顶部标了两个准确时刻：**数据截至**（热力图最后一列的时间戳）和**拉取于**（bot 什么时候拉的）。

## 页面读法

横轴价格，柱子高度是该价位堆积的待爆仓名义额（左轴）。叠加两条**累计曲线**（右轴），
从当前价分别向两边累加：现价 67,000，68,000 处有 10M、69,000 处有 20M，
那曲线在 68,000 读 10M、在 69,000 读 30M ——「价格走到这里，一路上一共会扫掉多少」。

整张图**只用最新一个时间格**的数据，是「当下」的分布。曾经有过一个"把历史列求和"的
错误视图，已删：每一列都是那一时刻的完整快照，同一笔仓位没被扫掉就会在每列重复出现，
求和等于把它算 N 遍。

右侧「走到这里会扫掉多少」把 ±1/2/5% 对应的价格和累计量直接算好。数值后带 `+` 表示
目标价超出了图表覆盖范围，实际只会更多。手机上点按图表即可看数值。

## 数据怎么流的

```
bot ──拉 Apify (10min 缓存)──> 热力图
 │
 └── PUT /api/heatmap/BTC ──> Worker ──> KV
                                         │
                              网页 GET ──┘（纯读，不碰 Apify）
```

**关键点：Worker 不持有 `APIFY_TOKEN`，也不会去调 Apify。**

Apify 的 CoinAnk actor 单次运行约 10 秒（实测）且计费。如果让网页直连，每个访客打开页面都可能
烧掉一次配额——而 bot 侧本来就有 10 分钟缓存和同币种串行锁。所以保持 bot 作为唯一的
调用方，配额完全可控，Worker 只是个读缓存的壳。

快照的更新时机：
- **每 6 小时自动刷新**（北京时间 2 / 8 / 14 / 20 点），其中 8 和 20 点与播报共用一次 Apify 调用；
- Telegram 里发 `/liqmap BTC` 会立刻拉一次并推上来（10 分钟内重复发命中缓存，不重复推）。

**Apify 配额**：$10 / 1000 次，免费档每 UTC 日只有 5 次。定时任务净消耗 4 次/天
（02 / 08 / 14 / 20），免费档只剩 1 次留给手动。要频繁手动查得升级 Apify 计划。

## 部署

无 GUI 的服务器上 `wrangler login` 走不通（OAuth 回调要打回本机 `localhost:8976`），用 **API Token**：

1. 打开 <https://dash.cloudflare.com/profile/api-tokens> → **Create Token**
2. 选模板 **Edit Cloudflare Workers**，权限不用改，Continue → Create Token，复制
3. 写进仓库根的 `deploy/bot.env`（已在 `.gitignore`，本来就是放密钥的地方）：
   ```
   CLOUDFLARE_API_TOKEN=xxx
   ```
   账号下有多个 account 的话再加一行 `CLOUDFLARE_ACCOUNT_ID=xxx`（dash 首页右侧能看到）
4. 一键部署：
   ```bash
   cd deploy/liqmap-web
   npm install          # 首次，装 wrangler
   npm run deploy       # = scripts/deploy.sh
   ```

脚本是幂等的，按需做四件事：验证凭证 → 建 KV namespace 并把 id 写进 `wrangler.toml` →
设 `PUSH_TOKEN` secret（没给就生成一个随机值并打印出来）→ `wrangler deploy`。
重复跑只补缺的步骤。最后会打印 Worker 地址。

`wrangler.toml` 里已经绑了自定义域名 `liqmap.narglc.eu.org`（`routes` 段）。
**换账号部署要先改成你自己 zone 下的域名，或者删掉 `routes` 段只用 workers.dev。**
`routes` 是顶层键，必须写在 `[[kv_namespaces]]` / `[vars]` 之前——放到文件末尾会被当成
`[vars]` 里的一个环境变量，deploy 输出里会出现 `env.routes`，路由却没生效（踩过）。

想自己指定 `PUSH_TOKEN`：`PUSH_TOKEN=你的值 npm run deploy`。

## `*.workers.dev` 打不开？

部署成功但页面超时，先判断是不是网络封锁：DNS 能解析、TCP 443 连不上、而 `api.cloudflare.com`
秒回——这是 `workers.dev` 在部分网络（国内常见）被整段封锁的典型症状，**不是部署问题**。

后果有两个：**你自己可能打不开页面**；**bot 所在机器如果也在这种网络里，推送快照会失败**
（日志里会看到 `清算图快照推送失败 ... i/o timeout`）。

解法是给 Worker 绑一个**自己的域名**（Cloudflare 上的 zone），走自己域名的 DNS 和 IP，
通常不受 workers.dev 封锁影响。在 `wrangler.toml` 加：

```toml
routes = [
  { pattern = "liqmap.你的域名", custom_domain = true }
]
```

然后 `npm run deploy`，Cloudflare 会自动配 DNS 和证书。bot 侧 `LIQWEB_URL` 换成新域名。

绕不开时的应急手段：直接通过 API 写 KV（走 api.cloudflare.com，不经过 workers.dev）：

```bash
npx wrangler kv key put --namespace-id=<KV id> --remote --ttl=86400 "heatmap:BTC" --path=快照.json
```

注意直写 KV 不会经过 Worker 的 `handlePut`，快照 JSON 里要自己带上 `updatedAt`（毫秒时间戳）。

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

网格用**稀疏三元组**而非稠密二维数组。实测 BTC 一份是 173 档 × 289 格、约 3.9 万个非零格、
715 KB（稠密会到 ~1 MB 且大半是 0）；KV 单值上限 25 MB，余量充足：

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

## 本地预览（不用部署就能看）

```bash
npm run preview          # → dist/preview.html，浏览器直接打开
```

它用真实的 `worker.js` 渲染页面，再把 `test/fixtures/btc-snapshot.json` 内联进去
（拦截 `window.fetch`，不改页面源码，所以 `load()` 怎么改都不影响预览）。

### 生成一份真实快照

fixture 是**真实的 Apify 数据**，拉一次存下来反复用（免费档每 UTC 日只有 5 次，别每次看预览都烧一次）：

```bash
cd ../..    # 回到仓库根目录
LIVE=1 APIFY_TOKEN=xxx SNAPSHOT_OUT=deploy/liqmap-web/test/fixtures/btc-snapshot.json \
  go test ./stocks/ -run TestLive_DumpLiqSnapshot -v
```

落盘的就是 bot 推给 Worker 的那份格式（`stocks.buildSnapshot`），所以预览看到的和线上完全一致。
想换币种加 `SNAPSHOT_SYMBOL=ETH`。快照文件建议入库，方便在没有 token 的机器上也能看预览、跑测试。

## 页面上的盘面面板

除爆仓地图外，页面右侧还有两张卡片，数据来自 bot 每小时推的 `brief` 快照
（`PUT /api/brief/:symbol`，与 heatmap 共用一套读写和鉴权）：

- **盘面**：RSI(14)、距 MA200、ATR、基差、持仓量、资金费率及其历史百分位、多空比、恐惧贪婪。
  标题右侧有健康度徽章，红色表示某个数据源失败或陈旧，鼠标悬停看原因。
- **未来 48h 宏观**：ForexFactory 的美国 High 级事件，带预测值与前值。

两份快照相互独立：brief 拿不到不影响爆仓地图，反之亦然。

## 测试

```bash
npm test        # 页面纯逻辑：坐标换算、稀疏展开、颜色映射
```

`page.test.mjs` 单独编译一次渲染出来的页面脚本。**这个测试是必须的**：页面 HTML 是
worker.js 里的一个模板字符串，`node --check src/worker.js` 只检查外层模块，看不进模板字符串；
而且模板字符串会自己解析转义，源码里写 `'\n'` 到了输出就变成真实换行，直接语法错误、页面白屏。
踩过一次，只有把渲染结果单独编译才能抓到。

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
