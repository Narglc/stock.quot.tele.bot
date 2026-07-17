# TODO / 待办

## ✅ 已完成

- **清算图**：`/liqmap [币种]`（拉 CoinAnk 热力图 → 纯 Go 渲染热力图+清算地图 PNG 相册）+ 早八/晚八 BTC 清算图 cron（需 `APIFY_TOKEN`）。数据源用 Apify actor（免费 5 次/UTC 天）。
- **行情增强**：OKX 衍生品——持仓量及**变化解读**、多空比、**资金费率**；全市场概览（总市值/BTC.D/ETH.D）；恐惧贪婪。
- **整点播报不刷屏**：删掉上一条 + 静音发新（群里始终一条、在最底部）。
- **图源**：超时 + 错误上抛（失败原因带进消息）+ 成功率加权选源；新增 waifu.pics、pic.re。
- **/dice** 动画骰子；**优雅退出**（SIGINT/SIGTERM）。
- **MCP 语音** `send_voice`（可带 caption）+ 可插拔 TTS（azure/edge/http 委托本地 worker）。

## 🟡 待办 backlog（按价值）

### 行情 / 指标
- 🟡 **K线 + 技术指标**（RSI/MACD/MA200/布林带）——清算图已叠 K线；播报文字加 RSI/均线待做。
- 🟡 **清算历史**（Coinalyze 免费 API：24h 多空爆仓额），与清算地图互补。
- 🟡 **/price 实时曲线图**（sparkline/小 K线 PNG）。
- 🟢 宏观：ETF 净流入 / 稳定币供给。
- 🟡 **转正到 CoinAnk 官方 API**（替代 Apify 社区爬虫求稳；base `https://open-api.coinank.com`，header `apikey`，热力图端点 `/api/liqMap/getLiqHeatMapSymbol`，非会员走 x402 按次付）。

### 交互 / 形态
- 🔥 **Inline 按钮**：行情/清算图挂「🔄刷新 / 切币种」回调按钮。
- 🟡 群里「看涨/看跌」**Poll**。

### 语音 / TTS
- 🟡 **Redis 队列异步补发**（本地 worker 离线时 send_voice 入队，上线补发）。
- 🟡 **本地 GPU 模型（ChatTTS）接 worker 的 `_gpu` 后端**（stub 已留）。
- 🟢 **aliyun CosyVoice provider**（国内实名可用，中文质量高；注意仅 90 天新人额度非长期免费）。

### 无 VPS / 内容源
- 🟡 **GitHub Actions 定时播报**（`cmd/broadcast` + workflow，serverless 推行情/清算图/语音）。
- 🟡 **RSS → Telegram 通用推送器**（推特大V via RSSHub/Apify/或直接订 TG 频道 + Redis 去重）。

### 多用户 / MCP
- 🟢 多用户 `/token` 自助注册；`send_liqmap` MCP 工具。

### 工程 / 质量
- 🟡 stocks/handler 补更多单测（crypto 格式化、handler）。
- 🟢 CI（build+test+vet）；`proxy-data` 改 bind mount；deploy 真实 IP → 占位符。
