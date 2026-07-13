# TODO / 待办

## 🟡 暂缓：清算热力图（Liquidation Heatmap / 清算地图）接入

> 目标：把"价格到 X 会爆 Y 亿"的**预测性**清算热力图接进 bot。
> 关注范围：**仅 BTC**。推送方式：**单独做「早八 / 晚八」两个 cron 各拉一次**（2 次/天），**不进整点播报**。
> 状态：调研完成，实现暂缓（2026-07-11 记录）。

### 概念区分（别混）
- **清算热力图（预测）** = 未来地图，"上方 X 堆 Y 亿待爆"，是磁吸目标。← **我们要的**
- **清算历史（已发生）** = 后视镜，用于"爆仓瀑布=局部见底/顶"的瞬时信号。价值次之，可后置。

### 数据源调研结论（成本 / 稳定性）

| 优先级 | 方案 | 成本 | 稳定性 | 备注 |
|---|---|---|---|---|
| 🥇 先上（试水） | **Apify Actor** `api_merge/coinank-liquidation-heatmap` | **免费**（5 次/UTC 天，我们 2 次/天够用；超出 $10/千次） | 中（社区爬虫，2 用户/0 评价，随时可能失效） | 输入 `BTCUSDT`，输出 JSON（价位桶+杠杆+K线+更新时间）。调用：`POST https://api.apify.com/v2/acts/.../run-sync-get-dataset-items?token=xxx` |
| 🥈 转正（求稳） | **CoinAnk 官方 API** | 会员 or **x402 按次付费**（2 次/天极便宜；确切会员价在 coinank.com/vip，需登录） | 高 | base `https://open-api.coinank.com`，header `apikey: xxx`；热力图端点 `GET /api/liqMap/getLiqHeatMapSymbol`；BTC 全支持。热力图大概率不在免费档，非会员走 x402（服务器返回 402 时按次付），会员专属路由返回 `code:-3` |
| 🥉 备选 | **Hyblock** | 多半付费（免费档≈网页限 BTC，**API 未必给 key**）；或 Bybit 刷 $1M/月量换 Advanced | 高 | `GET https://api.hyblockcapital.com/v2/liquidationHeatmap`，参数含 **leverage**（能按杠杆档过滤，差异化）；鉴权重（OAuth2 client-credentials + Bearer + x-api-key）。只有要"精细杠杆过滤"才值 |

**其它免费源**：Coinalyze 有真·免费 API，但**只有清算历史**、没有预测热力图。

### 推荐路线
先用 **Apify 免费额度**接进来验证效果（早八/晚八各拉一次 BTC）→ 有用了 → 转 **CoinAnk 官方 API**（x402 按次或最低会员档）求长期稳定。Hyblock 除非要杠杆档过滤，否则跳过。

### 实现要点（拾起时）
- 返回是**价位桶 JSON**（哪个价位堆多少待爆）。推送两种形态：
  1. **纯文本**："↑ $X 上方堆 $Y亿 · ↓ $Z 下方堆 $W亿"（最省）
  2. **go-chart 画热力条 PNG** 图文一起发（复用 `handler/handleApi.go` 的发图链路）
- best-effort 接法，跟 `stocks/derivatives.go`（OKX）/`sentiment.go` 一套：失败只省略、不阻断。
- 单独 cron（`0 8 * * *` / `0 20 * * *`，东八区），不并入 `broadcastCrypto`。
- token/apikey 走环境变量，勿进仓库。

### 参考链接
- CoinAnk: openApi https://coinank.com/openApi · VIP https://coinank.com/vip · skill https://github.com/coinank/coinank-openapi-skill
- Hyblock: 文档 https://docs.hyblockcapital.com/liquidation-heatmap · 定价 https://hyblockcapital.com/pricing
- Apify Actor: https://apify.com/api_merge/coinank-liquidation-heatmap
