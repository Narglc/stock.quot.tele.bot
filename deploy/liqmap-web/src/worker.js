/**
 * liqmap-web —— 爆仓地图的只读网页。
 *
 * 数据是 CoinAnk 基于 **Binance 合约持仓推算的待爆仓分布**（预估，非已成交清算），
 * 由 Apify actor api_merge/coinank-liquidation-heatmap 提供，只覆盖 Binance 一家。
 *
 * 数据流：bot 拉 Apify(带 10min 缓存) → PUT 到本 Worker → KV → 网页读 KV。
 * Worker 本身不碰 Apify，也不持有 APIFY_TOKEN：配额只由 bot 侧消耗，完全可控。
 *
 * 路由：
 *   PUT  /api/heatmap/:symbol   写入快照（需 X-Auth-Token: PUSH_TOKEN）
 *   GET  /api/heatmap/:symbol   读取快照 JSON
 *   GET  /:symbol               热力图页面
 *   GET  /                      跳到默认币种
 */

const SYMBOL_RE = /^[A-Z0-9]{1,10}$/;

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const path = url.pathname.replace(/\/+$/, "") || "/";

    // 两类快照共用一套读写：heatmap（清算热力图）与 brief（盘面指标）
    for (const [prefix, kind] of [["/api/heatmap/", "heatmap"], ["/api/brief/", "brief"], ["/api/series/", "series"]]) {
      if (!path.startsWith(prefix)) continue;
      const symbol = path.slice(prefix.length).toUpperCase();
      if (!SYMBOL_RE.test(symbol)) return json({ error: "bad symbol" }, 400);

      if (request.method === "PUT") return handlePut(request, env, kind, symbol);
      if (request.method === "GET") return handleGet(env, kind, symbol);
      return json({ error: "method not allowed" }, 405);
    }

    if (request.method !== "GET") return json({ error: "method not allowed" }, 405);

    const allowed = allowedSymbols(env);
    if (path === "/") {
      return Response.redirect(`${url.origin}/${allowed[0] || "BTC"}`, 302);
    }

    const symbol = path.slice(1).toUpperCase();
    if (!SYMBOL_RE.test(symbol)) return new Response("not found", { status: 404 });
    return new Response(renderPage(symbol, allowed), {
      headers: { "content-type": "text/html; charset=utf-8" },
    });
  },
};

function allowedSymbols(env) {
  return (env.ALLOWED_SYMBOLS || "BTC")
    .split(",")
    .map((s) => s.trim().toUpperCase())
    .filter(Boolean);
}

function json(obj, status = 200, extraHeaders = {}) {
  return new Response(JSON.stringify(obj), {
    status,
    headers: { "content-type": "application/json; charset=utf-8", ...extraHeaders },
  });
}

/** 每类快照的必需字段与过期时间。 */
const SNAPSHOT_KINDS = {
  // 拿不到新数据时宁可显示"无数据"，也不要展示一张看不出有多旧的图
  heatmap: { ttl: 86400, hint: "missing prices/times/cells",
             required: (b) => Array.isArray(b.prices) && Array.isArray(b.times) && Array.isArray(b.cells) },
  // 盘面变化快，过期短一些；缺 health 说明推的不是我们认识的格式
  brief:   { ttl: 21600, hint: "missing health",
             required: (b) => b && typeof b === "object" && b.health },
  // 长周期序列一天才多一个点，给 3 天余量足够扛住偶尔一次推送失败
  series:  { ttl: 259200, hint: "missing price/oi series",
             required: (b) => b && (b.price || b.oi) },
};

/** 写入快照。用固定长度比较避免时序侧信道。 */
async function handlePut(request, env, kind, symbol) {
  const spec = SNAPSHOT_KINDS[kind];
  const expected = env.PUSH_TOKEN;
  if (!expected) return json({ error: "server missing PUSH_TOKEN" }, 500);
  if (!timingSafeEqual(request.headers.get("X-Auth-Token") || "", expected)) {
    return json({ error: "unauthorized" }, 401);
  }

  let body;
  try {
    body = await request.json();
  } catch {
    return json({ error: "invalid json" }, 400);
  }
  if (!spec.required(body)) return json({ error: spec.hint }, 400);

  body.symbol = symbol;
  body.updatedAt = Date.now();
  await env.LIQMAP.put(`${kind}:${symbol}`, JSON.stringify(body), { expirationTtl: spec.ttl });
  return json({ ok: true, kind, symbol });
}

async function handleGet(env, kind, symbol) {
  const raw = await env.LIQMAP.get(`${kind}:${symbol}`);
  if (!raw) return json({ error: "no snapshot yet" }, 404);
  return new Response(raw, {
    headers: {
      "content-type": "application/json; charset=utf-8",
      // 快照最快也是 10 分钟一换（bot 侧缓存），边缘缓存 60s 足够。
      "cache-control": "public, max-age=60",
    },
  });
}

function timingSafeEqual(a, b) {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return diff === 0;
}

function renderPage(symbol, allowed) {
  // 只有一个币种时不显示切换标签——一个孤零零的标签是纯噪声。
  const nav = allowed.length > 1
    ? `<nav>${allowed.map((s) => `<a href="/${s}" class="${s === symbol ? "on" : ""}">${s}</a>`).join("")}</nav>`
    : "";

  return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark">
<title>${symbol} 爆仓地图</title>
<style>
  :root {
    --bg:#080b11; --panel:#111722; --panel2:#0c111a; --line:#1c2432;
    --fg:#e6ebf2; --dim:#78839a; --dim2:#4d5668;
    --short:#ff4d5e; --long:#22d67f;
    --shortDim:#ff8b96; --longDim:#6fe8ac;
  }
  * { box-sizing:border-box; -webkit-tap-highlight-color:transparent; }
  html, body { overflow-x:hidden; }
  body { margin:0; background:var(--bg); color:var(--fg);
    font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;
    -webkit-font-smoothing:antialiased; }

  header { display:flex; align-items:baseline; gap:12px 18px; flex-wrap:wrap;
    padding:14px 18px 12px; border-bottom:1px solid var(--line); }
  .brand { display:flex; align-items:baseline; gap:9px; }
  h1 { font-size:15px; margin:0; font-weight:600; letter-spacing:.01em; }
  .badge { font-size:10px; color:var(--dim2); border:1px solid var(--line);
    border-radius:4px; padding:1px 5px; letter-spacing:.05em; }
  nav { display:flex; gap:2px; }
  nav a { color:var(--dim); text-decoration:none; padding:3px 9px; border-radius:6px; font-size:12px; }
  nav a.on, nav a:hover { color:var(--fg); background:var(--panel); }

  .price { display:flex; align-items:baseline; gap:8px; margin-left:auto; }
  .price .n { font-size:22px; font-weight:650; font-variant-numeric:tabular-nums; letter-spacing:-.01em; }
  .price .sub { font-size:11px; color:var(--dim2); font-variant-numeric:tabular-nums; }
  .scope { flex-basis:100%; font-size:11.5px; color:var(--dim); line-height:1.6; margin-top:-2px; }
  .scope b { color:var(--fg); font-weight:600; }

  .controls { display:flex; gap:8px; padding:10px 18px 0; flex-wrap:wrap; }
  .seg { display:flex; background:var(--panel); border:1px solid var(--line); border-radius:8px; overflow:hidden; }
  .seg button { background:none; border:0; color:var(--dim); padding:6px 13px;
    font-size:12px; cursor:pointer; font-family:inherit; transition:background .12s,color .12s; }
  .seg button:hover { color:var(--fg); }
  .seg button.on { background:#1b2333; color:var(--fg); }

  main { display:grid; grid-template-columns:minmax(0,1fr) 300px; gap:16px; padding:14px 18px 24px; align-items:start; }
  @media (max-width:900px) { main { grid-template-columns:minmax(0,1fr); } }

  #chartWrap { position:relative; min-width:0; }
  canvas { width:100%; display:block; border-radius:12px; background:var(--panel2);
    border:1px solid var(--line); cursor:crosshair; touch-action:pan-y; }
  #tip { position:absolute; pointer-events:none; opacity:0; transition:opacity .1s; z-index:5;
    background:rgba(14,19,29,.98); border:1px solid #2a3446; border-radius:9px;
    padding:10px 12px; font-size:12px; white-space:nowrap;
    box-shadow:0 10px 32px rgba(0,0,0,.6); }
  #tip .p { font-size:13px; font-weight:600; margin-bottom:6px; }
  #tip .k { color:var(--dim); }
  #tip .v { font-weight:600; font-variant-numeric:tabular-nums; }
  #tip .line { margin-top:3px; }

  .legend { display:flex; gap:16px; flex-wrap:wrap; padding:9px 2px 0; font-size:11px; color:var(--dim); }
  .legend i { display:inline-block; width:9px; height:9px; border-radius:2px; margin-right:5px; vertical-align:-1px; }
  .legend .bar-l { background:linear-gradient(180deg,rgba(34,214,127,.95),rgba(34,214,127,.35)); }
  .legend .bar-s { background:linear-gradient(180deg,rgba(255,77,94,.95),rgba(255,77,94,.35)); }
  .legend .ln { width:14px; height:2px; border-radius:1px; }

  .card { background:var(--panel); border:1px solid var(--line); border-radius:12px;
    padding:14px; margin-bottom:12px; }
  .card h2 { font-size:10.5px; margin:0 0 10px; color:var(--dim2); font-weight:600;
    letter-spacing:.08em; text-transform:uppercase; }

  .split { display:grid; grid-template-columns:1fr 1fr; gap:9px; }
  .stat { background:var(--panel2); border-radius:9px; padding:10px 11px; }
  .stat .lbl { font-size:10.5px; color:var(--dim); margin-bottom:3px; }
  .stat .val { font-size:17px; font-weight:650; font-variant-numeric:tabular-nums; letter-spacing:-.01em; }
  .ratio { margin-top:9px; font-size:11.5px; color:var(--dim); text-align:center;
    background:var(--panel2); border-radius:8px; padding:7px; }
  .ratio b { color:var(--fg); font-variant-numeric:tabular-nums; }

  table.dist { width:100%; border-collapse:collapse; font-variant-numeric:tabular-nums; }
  table.dist td { padding:4.5px 0; font-size:12.5px; border-bottom:1px solid rgba(28,36,50,.6); }
  table.dist tr:last-child td { border-bottom:0; }
  table.dist td.pc { width:44px; color:var(--dim); font-size:11.5px; }
  table.dist td.px { color:var(--dim); font-size:11.5px; }
  table.dist td.cv { text-align:right; font-weight:600; }

  /* 指标条：横向铺开，放在最上面——数字是先看的东西，不该挤在窄边栏里 */
  .metrics { display:grid; gap:1px; background:var(--line); border-top:1px solid var(--line);
    border-bottom:1px solid var(--line);
    grid-template-columns:repeat(auto-fit,minmax(158px,1fr)); }
  .metric { background:var(--bg); padding:9px 14px; min-width:0; }
  .metric .k { font-size:10.5px; color:var(--dim); display:flex; align-items:center; gap:4px; }
  .metric .v { font-size:17px; font-weight:650; font-variant-numeric:tabular-nums;
    letter-spacing:-.01em; margin-top:2px; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .metric .sub { font-size:10.5px; color:var(--dim2); }

  .info { display:inline-flex; align-items:center; justify-content:center; width:13px; height:13px;
    border:1px solid var(--dim2); border-radius:50%; color:var(--dim2); font-size:9px;
    font-style:normal; cursor:help; flex:none; user-select:none; }
  .info:hover { color:var(--fg); border-color:var(--fg); }
  #infoTip { position:fixed; z-index:50; max-width:330px; opacity:0; pointer-events:none;
    transition:opacity .1s; background:rgba(14,19,29,.985); border:1px solid #2a3446;
    border-radius:9px; padding:11px 13px; font-size:12px; line-height:1.75;
    box-shadow:0 12px 40px rgba(0,0,0,.65); }
  #infoTip h4 { margin:0 0 5px; font-size:12.5px; color:var(--fg); font-weight:650; }
  #infoTip p { margin:0 0 6px; color:#b9c2d2; }
  #infoTip p:last-child { margin-bottom:0; }
  #infoTip b { color:var(--fg); }
  #infoTip .warn { color:#f0b429; }

  .kv { display:flex; justify-content:space-between; gap:10px; padding:4.5px 0;
    font-size:12.5px; font-variant-numeric:tabular-nums; border-bottom:1px solid rgba(28,36,50,.6); }
  .kv:last-child { border-bottom:0; }
  .kv .k { color:var(--dim); }
  .kv .v { font-weight:600; }
  .badge-ok { font-size:9.5px; color:var(--long); border:1px solid rgba(34,214,127,.35);
    border-radius:4px; padding:1px 5px; letter-spacing:0; text-transform:none; }
  .badge-bad { font-size:9.5px; color:var(--short); border:1px solid rgba(255,77,94,.4);
    border-radius:4px; padding:1px 5px; letter-spacing:0; text-transform:none; }
  .ev { padding:5px 0; font-size:12px; border-bottom:1px solid rgba(28,36,50,.6); }
  .ev:last-child { border-bottom:0; }
  .ev .t { color:var(--dim); font-size:11px; }
  .ev .n { display:block; margin:1px 0; }
  .ev .f { color:var(--dim); font-size:11px; }

  .row { display:flex; justify-content:space-between; align-items:baseline; gap:10px; padding:4.5px 0;
    font-variant-numeric:tabular-nums; font-size:12.5px; border-bottom:1px solid rgba(28,36,50,.6); }
  .row:last-child { border-bottom:0; }
  .row .d { font-size:10.5px; color:var(--dim2); }
  .short { color:var(--short); } .long { color:var(--long); }
  .note { color:var(--dim); font-size:12px; line-height:1.8; }
  .note b { color:var(--fg); font-weight:600; }
  .empty { color:var(--dim); padding:70px 16px; text-align:center; line-height:2.1;
    border:1px solid var(--line); border-radius:12px; background:var(--panel2); }
  footer { color:var(--dim2); font-size:11px; padding:0 18px 26px; line-height:1.7; }
</style>
</head>
<body>
<header>
  <div class="brand">
    <h1>${symbol} 爆仓地图</h1>
    <span class="badge">Binance 合约</span>
    <span class="badge" id="intervalBadge">—</span>
  </div>
  ${nav}
  <div class="price">
    <span class="n" id="nowPrice">—</span>
    <span class="sub" id="updated">加载中…</span>
    <span class="badge-ok" id="healthBadge" hidden></span>
    <i class="info" data-tip="price">i</i>
  </div>
  <div class="scope">
    预估待爆仓分布（按当前持仓推算），<b>不是</b>已成交的清算记录。现价为拉取快照那一刻的 OKX 永续价。
  </div>
</header>

<div class="metrics" id="metrics"></div>

<div class="controls">
  <div class="seg" id="viewSeg">
    <button data-view="map" class="on">爆仓地图</button>
    <button data-view="trend">长期趋势</button>
    <button data-view="oi">持仓量</button>
    <button data-view="heat">热力图</button>
  </div>
</div>

<main>
  <div>
    <div id="chartWrap">
      <canvas id="cv"></canvas>
      <div id="tip"></div>
    </div>
      <div class="legend" id="legend">
      <span><i class="bar-l"></i>下跌方向 · 多头爆仓</span>
      <span><i class="bar-s"></i>上涨方向 · 空头爆仓</span>
      <span><i class="ln" style="background:#5ae6a0"></i>向下累计</span>
      <span><i class="ln" style="background:#ff7884"></i>向上累计</span>
      <span style="color:var(--dim2)">柱=单档（左轴） · 线=累计（右轴）</span>
    </div>
  </div>

  <aside>
    <div class="card" id="macroCard" hidden>
      <h2>未来 48h 宏观事件（美国）</h2>
      <div id="macroBody"></div>
    </div>
    <div class="card liq-only">
      <h2>走到这里会扫掉多少
        <i class="info" data-tip="cumdist">i</i>
      </h2>
      <table class="dist"><tbody id="distTable"></tbody></table>
    </div>
    <div class="card liq-only">
      <h2>两侧合计</h2>
      <div class="split">
        <div class="stat"><div class="lbl">↓ 下跌方向</div><div class="val long" id="sumBelow">–</div></div>
        <div class="stat"><div class="lbl">↑ 上涨方向</div><div class="val short" id="sumAbove">–</div></div>
      </div>
      <div class="ratio" id="ratio"></div>
    </div>
    <div class="card liq-only">
      <h2>清算簇 TOP</h2>
      <div id="clusters"></div>
    </div>
    <div class="card liq-only">
      <h2>怎么看</h2>
      <div class="note">
        横轴价格，<b>柱子高度</b>是该价位堆积的待爆仓名义额。<br><br>
        两条<b>曲线</b>是从当前价往两边的<b>累计</b>：比如现价 67,000，68,000 处有 10M、69,000 处有 20M，
        那曲线在 68,000 读 10M、在 69,000 读 30M——「价格走到这里，一路上一共会扫掉多少」。
        单档柱子高不代表什么，<b>曲线斜率陡的那一段</b>才是真正的密集区。<br><br>
        右上表把这件事直接算好了。<br><br>
        整张图只用<b>最新一个时间格</b>的数据，是「当下」的分布；页面顶部标了数据截至的准确时刻。
      </div>
    </div>
  </aside>
</main>

<div id="infoTip"></div>

<footer>
  数据来源 CoinAnk（Binance 合约，基于持仓推算的待爆仓分布），经 bot 推送，每 6 小时自动刷新一次，Telegram 里发 /liqmap 也会触发。<br>
  两侧数据一并列出，只看一边会得出无法证伪的方向结论。仅供参考，不构成任何建议。
</footer>

<script>
/* --- PURE:START ---
   这一段是不依赖 DOM/canvas 的纯函数，被 test/pure.test.mjs 提取出来单独测试。
   改动时请保持这两个标记，且不要在块内引用 document / window。 */
function fmtNum(v) {
  const a = Math.abs(v);
  if (a >= 1e12) return (v/1e12).toFixed(2) + 'T';
  if (a >= 1e9)  return (v/1e9 ).toFixed(2) + 'B';
  if (a >= 1e6)  return (v/1e6 ).toFixed(2) + 'M';
  if (a >= 1e3)  return (v/1e3 ).toFixed(1) + 'K';
  return v.toFixed(0);
}
function fmtPrice(v) {
  const d = v < 1 ? 4 : (v < 100 ? 2 : 0);
  return v.toLocaleString('en-US', { minimumFractionDigits: d, maximumFractionDigits: d });
}
function fmtTime(ms) {
  const d = new Date(ms);
  const p = (n) => String(n).padStart(2, '0');
  return p(d.getMonth()+1) + '-' + p(d.getDate()) + ' ' + p(d.getHours()) + ':' + p(d.getMinutes());
}

/** 稀疏三元组 [x,y,v] 展开成稠密网格，索引 y*T+x。越界的格子直接丢弃。 */
function expandGrid(cells, T, P) {
  const g = new Float64Array(T * P);
  for (const [x, y, v] of cells) {
    if (x >= 0 && x < T && y >= 0 && y < P) g[y * T + x] = v;
  }
  return g;
}

/**
 * 取最新一个时间格里各价位的待爆仓量——这就是「当下」的爆仓地图。
 *
 * 只用最新列，不把历史列相加：每一列都是那一时刻的完整快照，
 * 同一笔仓位只要没被扫掉就会在连续多列里重复出现，求和等于把它算 N 遍。
 */
function latestColumn(grid, T, P) {
  const out = new Float64Array(P);
  const x = T - 1;
  for (let y = 0; y < P; y++) out[y] = grid[y * T + x];
  return out;
}

/** 把逐档清算额并成 nBuckets 个价格桶（按价格升序）。 */
function bucketize(prices, values, nBuckets) {
  const P = prices.length;
  if (P === 0) return [];
  const n = Math.max(1, Math.min(nBuckets, P));
  const lo = prices[0], hi = prices[P - 1];
  const span = (hi - lo) || 1;

  const buckets = Array.from({ length: n }, (_, i) => ({
    lo: lo + (span * i) / n,
    hi: lo + (span * (i + 1)) / n,
    mid: lo + (span * (i + 0.5)) / n,
    value: 0,
  }));
  for (let y = 0; y < P; y++) {
    const v = values[y];
    if (v <= 0) continue;
    let bi = Math.floor(((prices[y] - lo) / span) * n);
    if (bi < 0) bi = 0;
    if (bi >= n) bi = n - 1;
    buckets[bi].value += v;
  }
  return buckets;
}

/** 找到当前价所在的分界桶索引：第一个 mid >= currentPrice 的桶。 */
function pivotIndex(buckets, currentPrice) {
  for (let i = 0; i < buckets.length; i++) {
    if (buckets[i].mid >= currentPrice) return i;
  }
  return buckets.length;
}

/**
 * 从当前价向两边累计清算额。
 *
 * 这是比单档柱子更有用的读数：单个价位堆了多少本身说明不了什么；
 * 「价格走到这里，一路上一共会扫掉多少」才跟决策相关。
 * 曲线斜率陡的那一段，就是真正的密集区。
 */
function cumulate(buckets, currentPrice) {
  const n = buckets.length;
  const cum = new Float64Array(n);
  const pivot = pivotIndex(buckets, currentPrice);

  let s = 0;
  for (let i = pivot; i < n; i++) { s += buckets[i].value; cum[i] = s; }
  s = 0;
  for (let i = pivot - 1; i >= 0; i--) { s += buckets[i].value; cum[i] = s; }
  return cum;
}

/**
 * 价格走到 targetPrice 时，从现价起累计会扫掉多少。
 * 目标价超出图表范围时返回该侧总量，并标记 clipped——
 * 不标的话会让人误以为「再远就没有了」，而其实只是数据没覆盖到。
 */
function cumAtPrice(buckets, cum, currentPrice, targetPrice) {
  const n = buckets.length;
  if (!n) return { value: 0, clipped: false };
  const up = targetPrice >= currentPrice;
  const lo = buckets[0].lo, hi = buckets[n - 1].hi;

  if (up && targetPrice > hi) return { value: cum[n - 1], clipped: true };
  if (!up && targetPrice < lo) return { value: cum[0], clipped: true };

  const pivot = pivotIndex(buckets, currentPrice);
  let total = 0;
  if (up) {
    for (let i = pivot; i < n; i++) {
      if (buckets[i].lo > targetPrice) break;
      total = cum[i];
    }
  } else {
    for (let i = pivot - 1; i >= 0; i--) {
      if (buckets[i].hi < targetPrice) break;
      total = cum[i];
    }
  }
  return { value: total, clipped: false };
}

/** 取序列里第 idx 列的最小/最大值，忽略 <=0（MA200 未成形时是 0）。 */
function minMax(points, idx) {
  let lo = Infinity, hi = -Infinity;
  for (const p of points) {
    const v = p[idx];
    if (!(v > 0)) continue;
    if (v < lo) lo = v;
    if (v > hi) hi = v;
  }
  return lo === Infinity ? null : { lo, hi };
}

/**
 * 对数刻度上的相对位置（0=底 1=顶）。
 * 5 年 BTC 价格跨了好几倍，线性刻度会把早期低价区压成一条线，看不出结构。
 */
function logPos(v, lo, hi) {
  if (!(v > 0) || !(lo > 0) || !(hi > lo)) return 0;
  return (Math.log(v) - Math.log(lo)) / (Math.log(hi) - Math.log(lo));
}

/** 鼠标 x 坐标 → 序列索引（等距点）。 */
function indexAtX(mx, box, n) {
  if (n <= 0 || mx < box.x || mx > box.x + box.w) return -1;
  const i = Math.round(((mx - box.x) / box.w) * (n - 1));
  return Math.min(n - 1, Math.max(0, i));
}

/** 鼠标 x 坐标 → 桶索引。价格轴从左到右递增。 */
function bucketAtX(mx, box, n) {
  if (mx < box.x || mx > box.x + box.w) return -1;
  const i = Math.floor(((mx - box.x) / box.w) * n);
  return Math.min(n - 1, Math.max(0, i));
}

/** 热力图色阶：对数归一化，跨数量级仍可区分。 */
function heatColor(v, max) {
  if (v <= 0) return null;
  const t = Math.min(1, Math.log10(1 + v) / Math.log10(1 + max));
  const stops = [
    [0.00, [10, 16, 48]], [0.20, [18, 48, 107]], [0.40, [27, 126, 168]],
    [0.60, [57, 185, 138]], [0.75, [201, 210, 74]], [0.88, [240, 136, 62]],
    [1.00, [224, 49, 49]],
  ];
  for (let i = 1; i < stops.length; i++) {
    if (t <= stops[i][0]) {
      const [t0, c0] = stops[i-1], [t1, c1] = stops[i];
      const k = (t - t0) / (t1 - t0);
      const c = c0.map((v0, j) => Math.round(v0 + (c1[j] - v0) * k));
      return 'rgb(' + c.join(',') + ')';
    }
  }
  return 'rgb(224,49,49)';
}
/* --- PURE:END --- */

const SYMBOL = ${JSON.stringify(symbol)};
const DIST_PCTS = [-5, -2, -1, 1, 2, 5];

const cv = document.getElementById('cv');
const ctx = cv.getContext('2d');
const tip = document.getElementById('tip');

let D = null, B = null, S = null, grid = null, buckets = [], cum = null, box = null;
let view = 'map', hoverIdx = -1, raf = 0;

const PAD = { l: 56, r: 56, t: 16, b: 30 };

function layout() {
  const wrap = cv.parentElement;
  const dpr = window.devicePixelRatio || 1;
  const w = wrap.clientWidth;
  // 窄屏给更高的比例，否则柱子挤成一团看不出形状
  const ratio = w < 560 ? 0.85 : (w < 820 ? 0.62 : 0.52);
  const h = Math.max(300, Math.min(540, Math.round(w * ratio)));
  cv.width = w * dpr; cv.height = h * dpr;
  cv.style.height = h + 'px';
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  box = { x: PAD.l, y: PAD.t, w: w - PAD.l - PAD.r, h: h - PAD.t - PAD.b };
}

function bucketX(i) { return box.x + ((i + 0.5) / buckets.length) * box.w; }
function priceX(p) {
  if (!buckets.length) return box.x;
  const lo = buckets[0].lo, hi = buckets[buckets.length - 1].hi;
  return box.x + ((p - lo) / ((hi - lo) || 1)) * box.w;
}

/** 重绘节流：hover 每次移动都重画整图，用 rAF 合并到一帧。 */
function scheduleDraw() {
  if (raf) return;
  raf = requestAnimationFrame(() => { raf = 0; draw(); });
}

function draw() {
  layout();
  ctx.clearRect(0, 0, cv.width, cv.height);
  if (view === 'trend') return drawSeries('price');
  if (view === 'oi') return drawSeries('oi');
  if (!D) return;
  view === 'heat' ? drawHeat() : drawMap();
}

/**
 * 长周期曲线。price：收盘价 + MA200，对数刻度（5 年跨好几倍，线性会把早期压成一条线）。
 * oi：持仓量，线性刻度。
 */
function drawSeries(kind) {
  const src = kind === 'price' ? (S && S.price) : (S && S.oi);
  if (!src || !src.points || src.points.length < 2) {
    ctx.fillStyle = '#79839a';
    ctx.font = '13px -apple-system,sans-serif';
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
    ctx.fillText(S ? '暂无数据' : '加载中…', box.x + box.w / 2, box.y + box.h / 2);
    return;
  }
  const pts = src.points;
  const n = pts.length;
  const useLog = kind === 'price';

  // price 图要把收盘价和 MA200 一起纳入范围
  const a = minMax(pts, 1);
  const b2 = kind === 'price' ? minMax(pts, 2) : null;
  let lo = a.lo, hi = a.hi;
  if (b2) { lo = Math.min(lo, b2.lo); hi = Math.max(hi, b2.hi); }
  if (useLog) { lo *= 0.95; hi *= 1.05; } else { lo = 0; hi *= 1.08; }

  const yOf = (v) => box.y + box.h - (useLog ? logPos(v, lo, hi) : (v - lo) / (hi - lo)) * box.h;
  const xOf = (i) => box.x + (i / (n - 1)) * box.w;

  // 网格与纵轴
  ctx.font = '10.5px ui-monospace,monospace';
  ctx.fillStyle = '#4d5668';
  ctx.textAlign = 'right'; ctx.textBaseline = 'middle';
  for (let t = 0; t <= 4; t++) {
    const v = useLog ? Math.exp(Math.log(lo) + (Math.log(hi) - Math.log(lo)) * t / 4) : lo + (hi - lo) * t / 4;
    const y = yOf(v);
    ctx.strokeStyle = t === 0 ? 'rgba(40,50,68,.9)' : 'rgba(28,36,50,.5)';
    ctx.beginPath(); ctx.moveTo(box.x, y); ctx.lineTo(box.x + box.w, y); ctx.stroke();
    ctx.fillText(kind === 'price' ? fmtPrice(v) : fmtNum(v), box.x - 7, y);
  }

  const line = (idx, color, width) => {
    ctx.strokeStyle = color; ctx.lineWidth = width; ctx.lineJoin = 'round'; ctx.lineCap = 'round';
    ctx.beginPath();
    let started = false;
    for (let i = 0; i < n; i++) {
      const v = pts[i][idx];
      if (!(v > 0)) continue;
      const x = xOf(i), y = yOf(v);
      started ? ctx.lineTo(x, y) : (ctx.moveTo(x, y), started = true);
    }
    ctx.stroke();
  };

  if (kind === 'price') {
    line(1, 'rgba(120,180,255,.95)', 1.5);          // 收盘价
    line(2, 'rgba(240,180,60,.95)', 2);             // MA200
  } else {
    // 持仓量画成带填充的面积，强调"体量"
    ctx.beginPath();
    ctx.moveTo(xOf(0), box.y + box.h);
    for (let i = 0; i < n; i++) ctx.lineTo(xOf(i), yOf(pts[i][1]));
    ctx.lineTo(xOf(n - 1), box.y + box.h); ctx.closePath();
    ctx.fillStyle = 'rgba(120,180,255,.12)'; ctx.fill();
    line(1, 'rgba(120,180,255,.95)', 2);
  }

  // 时间轴
  ctx.textAlign = 'center'; ctx.textBaseline = 'top'; ctx.fillStyle = '#4d5668';
  const ticks = Math.max(2, Math.min(7, Math.floor(box.w / 110)));
  for (let t = 0; t <= ticks; t++) {
    const i = Math.round((n - 1) * t / ticks);
    const d = new Date(pts[i][0]);
    const label = d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0');
    ctx.fillText(label, Math.min(Math.max(xOf(i), box.x + 28), box.x + box.w - 28), box.y + box.h + 8);
  }

  drawSeriesHover(kind, pts, n, xOf, yOf);
  drawSeriesLegend(kind);
}

function drawSeriesLegend(kind) {
  const items = kind === 'price'
    ? [['rgba(120,180,255,.95)', '收盘价'], ['rgba(240,180,60,.95)', 'MA200']]
    : [['rgba(120,180,255,.95)', '持仓量（OKX，日线）']];
  ctx.font = '11px -apple-system,sans-serif';
  ctx.textAlign = 'left'; ctx.textBaseline = 'middle';
  let x = box.x + 6;
  for (const [c, label] of items) {
    ctx.fillStyle = c; ctx.fillRect(x, box.y + 7, 12, 2);
    ctx.fillStyle = '#79839a'; ctx.fillText(label, x + 17, box.y + 8);
    x += ctx.measureText(label).width + 34;
  }
}

function drawSeriesHover(kind, pts, n, xOf, yOf) {
  if (hoverIdx < 0 || hoverIdx >= n) return;
  const x = xOf(hoverIdx);
  ctx.strokeStyle = 'rgba(255,255,255,.28)'; ctx.lineWidth = 1;
  ctx.beginPath(); ctx.moveTo(x, box.y); ctx.lineTo(x, box.y + box.h); ctx.stroke();
  for (const idx of (kind === 'price' ? [1, 2] : [1])) {
    const v = pts[hoverIdx][idx];
    if (!(v > 0)) continue;
    ctx.fillStyle = idx === 1 ? 'rgba(120,180,255,1)' : 'rgba(240,180,60,1)';
    ctx.beginPath(); ctx.arc(x, yOf(v), 3, 0, Math.PI * 2); ctx.fill();
  }
}

function drawMap() {
  const n = buckets.length;
  if (!n) return;
  const maxV = Math.max(...buckets.map(b => b.value)) || 1;
  const maxC = Math.max(...cum) || 1;
  const bw = box.w / n;

  ctx.font = '10.5px ui-monospace,SFMono-Regular,Menlo,monospace';

  // 横向网格 + 左右轴刻度
  for (let i = 0; i <= 4; i++) {
    const y = box.y + box.h - (i / 4) * box.h;
    ctx.strokeStyle = i === 0 ? 'rgba(40,50,68,.9)' : 'rgba(28,36,50,.5)';
    ctx.beginPath(); ctx.moveTo(box.x, y); ctx.lineTo(box.x + box.w, y); ctx.stroke();

    ctx.fillStyle = '#4d5668';
    ctx.textBaseline = 'middle';
    ctx.textAlign = 'right';
    ctx.fillText(i === 0 ? '0' : fmtNum((maxV * i) / 4), box.x - 7, y);
    if (i > 0) {
      ctx.textAlign = 'left';
      ctx.fillText(fmtNum((maxC * i) / 4), box.x + box.w + 7, y);
    }
  }

  // 柱子
  for (let i = 0; i < n; i++) {
    const b = buckets[i];
    if (b.value <= 0) continue;
    const h = Math.max((b.value / maxV) * box.h, 1);
    const above = b.mid >= D.currentPrice;
    const dim = hoverIdx >= 0 && i !== hoverIdx;
    const a = dim ? 0.4 : 1;

    const g = ctx.createLinearGradient(0, box.y + box.h - h, 0, box.y + box.h);
    if (above) {
      g.addColorStop(0, 'rgba(255,77,94,' + 0.95 * a + ')');
      g.addColorStop(1, 'rgba(255,77,94,' + 0.3 * a + ')');
    } else {
      g.addColorStop(0, 'rgba(34,214,127,' + 0.95 * a + ')');
      g.addColorStop(1, 'rgba(34,214,127,' + 0.3 * a + ')');
    }
    ctx.fillStyle = g;
    ctx.fillRect(box.x + i * bw + bw * 0.12, box.y + box.h - h, Math.max(bw * 0.76, 1), h);
  }

  drawCumCurves(maxC);
  drawPriceAxis(n);
  drawNowLine();
  drawTopLabels(maxV);
  drawCrosshair();
}

/** 两条累计曲线，各自从现价分界点起算。 */
function drawCumCurves(maxC) {
  const n = buckets.length;
  const pivot = pivotIndex(buckets, D.currentPrice);
  const cy = (v) => box.y + box.h - (v / maxC) * box.h;
  const nowX = priceX(D.currentPrice);
  const baseY = box.y + box.h;

  // 向上（空头爆仓累计）：从现价点出发向右
  if (pivot < n) {
    const pts = [[nowX, cy(0)]];
    for (let i = pivot; i < n; i++) pts.push([bucketX(i), cy(cum[i])]);
    paintCurve(pts, 'rgba(255,120,132,', baseY);
  }
  // 向下（多头爆仓累计）：按 x 递增顺序画，最后接到现价点
  if (pivot > 0) {
    const pts = [];
    for (let i = 0; i < pivot; i++) pts.push([bucketX(i), cy(cum[i])]);
    pts.push([nowX, cy(0)]);
    paintCurve(pts, 'rgba(90,230,160,', baseY);
  }
}

function paintCurve(pts, rgbaPrefix, baseY) {
  if (pts.length < 2) return;
  ctx.beginPath();
  ctx.moveTo(pts[0][0], pts[0][1]);
  for (let i = 1; i < pts.length; i++) ctx.lineTo(pts[i][0], pts[i][1]);

  // 曲线下方淡填充，强调累计的体量感
  const fill = new Path2D();
  fill.moveTo(pts[0][0], baseY);
  for (const [x, y] of pts) fill.lineTo(x, y);
  fill.lineTo(pts[pts.length - 1][0], baseY);
  fill.closePath();
  ctx.fillStyle = rgbaPrefix + '.09)';
  ctx.fill(fill);

  ctx.strokeStyle = rgbaPrefix + '1)';
  ctx.lineWidth = 2;
  ctx.lineJoin = 'round';
  ctx.lineCap = 'round';
  ctx.stroke();
}

function drawPriceAxis(n) {
  ctx.font = '10.5px ui-monospace,monospace';
  ctx.fillStyle = '#4d5668';
  ctx.textAlign = 'center'; ctx.textBaseline = 'top';
  const ticks = Math.max(2, Math.min(8, Math.floor(box.w / 100)));
  for (let i = 0; i <= ticks; i++) {
    const bi = Math.round((n - 1) * i / ticks);
    const x = Math.min(Math.max(bucketX(bi), box.x + 28), box.x + box.w - 28);
    ctx.fillText(fmtPrice(buckets[bi].mid), x, box.y + box.h + 8);
  }
}

function drawNowLine() {
  if (!D.currentPrice || !buckets.length) return;
  const lo = buckets[0].lo, hi = buckets[buckets.length - 1].hi;
  if (D.currentPrice < lo || D.currentPrice > hi) return;
  const x = priceX(D.currentPrice);

  ctx.strokeStyle = 'rgba(255,255,255,.85)';
  ctx.lineWidth = 1; ctx.setLineDash([4, 3]);
  ctx.beginPath(); ctx.moveTo(x, box.y + 8); ctx.lineTo(x, box.y + box.h); ctx.stroke();
  ctx.setLineDash([]);

  const label = fmtPrice(D.currentPrice);
  ctx.font = '600 10.5px ui-monospace,monospace';
  const w = ctx.measureText(label).width + 12;
  const lx = Math.min(Math.max(x - w / 2, box.x), box.x + box.w - w);
  ctx.fillStyle = '#fff';
  roundRect(lx, box.y - 4, w, 16, 4); ctx.fill();
  ctx.fillStyle = '#080b11';
  ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
  ctx.fillText(label, lx + w / 2, box.y + 4);
}

function drawTopLabels(maxV) {
  if (hoverIdx >= 0) return; // hover 时让位给 tooltip，避免叠字
  const top = buckets.map((b, i) => ({ b, i })).filter(o => o.b.value > 0)
    .sort((a, z) => z.b.value - a.b.value).slice(0, 4);
  ctx.font = '600 10px ui-monospace,monospace';
  ctx.textAlign = 'center'; ctx.textBaseline = 'bottom';
  for (const { b, i } of top) {
    const h = (b.value / maxV) * box.h;
    ctx.fillStyle = b.mid >= D.currentPrice ? '#ff9aa3' : '#7deab4';
    ctx.fillText(fmtNum(b.value),
      Math.min(Math.max(bucketX(i), box.x + 22), box.x + box.w - 22),
      box.y + box.h - h - 4);
  }
}

function drawCrosshair() {
  if (hoverIdx < 0) return;
  const x = bucketX(hoverIdx);
  ctx.strokeStyle = 'rgba(255,255,255,.25)';
  ctx.lineWidth = 1;
  ctx.beginPath(); ctx.moveTo(x, box.y); ctx.lineTo(x, box.y + box.h); ctx.stroke();
}

function roundRect(x, y, w, h, r) {
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.arcTo(x + w, y, x + w, y + h, r);
  ctx.arcTo(x + w, y + h, x, y + h, r);
  ctx.arcTo(x, y + h, x, y, r);
  ctx.arcTo(x, y, x + w, y, r);
  ctx.closePath();
}

function drawHeat() {
  const T = D.times.length, P = D.prices.length;
  const cw = box.w / T, ch = box.h / P;
  for (let y = 0; y < P; y++) {
    for (let x = 0; x < T; x++) {
      const v = grid[y * T + x];
      if (v <= 0) continue;
      const col = heatColor(v, D.maxValue);
      if (!col) continue;
      ctx.fillStyle = col;
      ctx.fillRect(box.x + x * cw, box.y + box.h - (y + 1) * ch, Math.max(cw, 1), Math.max(ch, 1));
    }
  }
  ctx.font = '10.5px ui-monospace,monospace';
  ctx.fillStyle = '#4d5668';
  ctx.textAlign = 'right'; ctx.textBaseline = 'middle';
  for (let i = 0; i <= 6; i++) {
    const idx = Math.round((P - 1) * i / 6);
    const y = box.y + box.h - ((idx + 0.5) / P) * box.h;
    ctx.fillText(fmtPrice(D.prices[idx]), box.x - 7, y);
  }
  ctx.textAlign = 'center'; ctx.textBaseline = 'top';
  const tn = Math.max(2, Math.min(5, Math.floor(box.w / 110)));
  for (let i = 0; i <= tn; i++) {
    const idx = Math.round((T - 1) * i / tn);
    const x = box.x + (idx + 0.5) * cw;
    ctx.fillText(fmtTime(D.times[idx]), Math.min(Math.max(x, box.x + 34), box.x + box.w - 34), box.y + box.h + 8);
  }
  const lo = D.prices[0], hi = D.prices[D.prices.length - 1];
  if (D.currentPrice > lo && D.currentPrice < hi) {
    const y = box.y + box.h - ((D.currentPrice - lo) / (hi - lo)) * box.h;
    ctx.strokeStyle = 'rgba(255,255,255,.8)';
    ctx.setLineDash([4, 3]);
    ctx.beginPath(); ctx.moveTo(box.x, y); ctx.lineTo(box.x + box.w, y); ctx.stroke();
    ctx.setLineDash([]);
  }
}

/** 指针位置 → 更新 hover 状态与 tooltip。鼠标和触摸共用。 */
function onPointer(clientX, clientY) {
  if (view === 'trend' || view === 'oi') return onSeriesPointer(clientX, clientY);
  if (!D || view !== 'map' || !buckets.length) return hideTip();
  const r = cv.getBoundingClientRect();
  const mx = clientX - r.left, my = clientY - r.top;
  const i = bucketAtX(mx, box, buckets.length);
  if (i < 0 || my < box.y || my > box.y + box.h) return hideTip();

  const b = buckets[i];
  const above = b.mid >= D.currentPrice;
  const pct = ((b.mid - D.currentPrice) / (D.currentPrice || 1)) * 100;
  const col = above ? 'var(--short)' : 'var(--long)';

  tip.innerHTML =
    '<div class="p">$' + fmtPrice(b.lo) + ' – $' + fmtPrice(b.hi) +
      ' <span class="k">' + (pct >= 0 ? '+' : '') + pct.toFixed(2) + '%</span></div>' +
    '<div class="line"><span class="k">本档</span> <span class="v">$' + fmtNum(b.value) + '</span></div>' +
    '<div class="line"><span class="k">' + (above ? '涨到这里累计扫掉' : '跌到这里累计扫掉') +
      '</span> <span class="v" style="color:' + col + '">$' + fmtNum(cum[i]) + '</span></div>';
  tip.style.opacity = 1;

  const tw = tip.offsetWidth, th = tip.offsetHeight;
  tip.style.left = Math.min(Math.max(mx - tw / 2, 4), r.width - tw - 4) + 'px';
  tip.style.top = Math.max(my - th - 14, 4) + 'px';

  if (i !== hoverIdx) { hoverIdx = i; scheduleDraw(); }
}

/** 长期图的 hover：给出该日的精确数值。 */
function onSeriesPointer(clientX, clientY) {
  const src = view === 'trend' ? (S && S.price) : (S && S.oi);
  if (!src || !src.points || !src.points.length) return hideTip();
  const pts = src.points;
  const r = cv.getBoundingClientRect();
  const mx = clientX - r.left, my = clientY - r.top;
  const i = indexAtX(mx, box, pts.length);
  if (i < 0 || my < box.y || my > box.y + box.h) return hideTip();

  const p = pts[i];
  const d = new Date(p[0]);
  const pad = (x) => String(x).padStart(2, '0');
  let html = '<div class="p">' + d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) + '</div>';
  if (view === 'trend') {
    html += '<div class="line"><span class="k">收盘</span> <span class="v">$' + fmtPrice(p[1]) + '</span></div>';
    if (p[2] > 0) {
      const dev = (p[1] - p[2]) / p[2] * 100;
      html += '<div class="line"><span class="k">MA200</span> <span class="v">$' + fmtPrice(p[2]) + '</span></div>' +
        '<div class="line"><span class="k">偏离</span> <span class="v" style="color:' +
        (dev >= 0 ? 'var(--long)' : 'var(--short)') + '">' + (dev >= 0 ? '+' : '') + dev.toFixed(1) + '%</span></div>';
    }
  } else {
    html += '<div class="line"><span class="k">持仓量</span> <span class="v">$' + fmtNum(p[1]) + '</span></div>';
    if (p[2] > 0) html += '<div class="line"><span class="k">成交额</span> <span class="v">$' + fmtNum(p[2]) + '</span></div>';
  }
  tip.innerHTML = html;
  tip.style.opacity = 1;
  const tw = tip.offsetWidth, th = tip.offsetHeight;
  tip.style.left = Math.min(Math.max(mx - tw / 2, 4), r.width - tw - 4) + 'px';
  tip.style.top = Math.max(my - th - 14, 4) + 'px';
  if (i !== hoverIdx) { hoverIdx = i; scheduleDraw(); }
}

function hideTip() {
  tip.style.opacity = 0;
  if (hoverIdx !== -1) { hoverIdx = -1; scheduleDraw(); }
}

cv.addEventListener('mousemove', (e) => onPointer(e.clientX, e.clientY));
cv.addEventListener('mouseleave', hideTip);
// 触摸支持：手机上没有 hover，不接这个就完全看不到数值
cv.addEventListener('touchstart', (e) => {
  const t = e.touches[0]; if (t) onPointer(t.clientX, t.clientY);
}, { passive: true });
cv.addEventListener('touchmove', (e) => {
  const t = e.touches[0]; if (t) onPointer(t.clientX, t.clientY);
}, { passive: true });
cv.addEventListener('touchend', hideTip);

function rebuild() {
  layout();
  const agg = latestColumn(grid, D.times.length, D.prices.length);
  // 桶数跟着画布宽度走：每根柱子至少 8px 才看得清
  const nb = Math.max(20, Math.min(120, Math.floor(box.w / 9)));
  buckets = bucketize(D.prices, agg, nb);
  cum = cumulate(buckets, D.currentPrice);
  hoverIdx = -1;
  draw();
  renderSide();
}

function renderSide() {
  // 距现价 ±N% 的累计量——最常问的问题，直接算好，不用自己去图上找
  const rows = DIST_PCTS.map((p) => {
    const target = D.currentPrice * (1 + p / 100);
    const { value, clipped } = cumAtPrice(buckets, cum, D.currentPrice, target);
    return { p, target, value, clipped };
  });
  document.getElementById('distTable').innerHTML = rows.map((r) => {
    const cls = r.p >= 0 ? 'short' : 'long';
    return '<tr><td class="pc ' + cls + '">' + (r.p > 0 ? '+' : '') + r.p + '%</td>' +
      '<td class="px">$' + fmtPrice(r.target) + '</td>' +
      '<td class="cv">$' + fmtNum(r.value) + (r.clipped ? '<span class="d">+</span>' : '') + '</td></tr>';
  }).join('');

  let sa = 0, sb = 0;
  for (const b of buckets) (b.mid >= D.currentPrice ? (sa += b.value) : (sb += b.value));
  document.getElementById('sumAbove').textContent = '$' + fmtNum(sa);
  document.getElementById('sumBelow').textContent = '$' + fmtNum(sb);

  const el = document.getElementById('ratio');
  if (sa > 0 && sb > 0) {
    const heavier = sa >= sb ? '上方' : '下方';
    const r = sa >= sb ? sa / sb : sb / sa;
    el.innerHTML = heavier + '堆积是另一侧的 <b>' + r.toFixed(2) + 'x</b>';
  } else {
    el.textContent = '—';
  }

  const top = buckets.map((b, i) => ({ b, i })).filter(o => o.b.value > 0)
    .sort((a, z) => z.b.value - a.b.value).slice(0, 10);
  const cel = document.getElementById('clusters');
  cel.innerHTML = !top.length ? '<div class="note">当前无显著簇</div>' : top.map(({ b, i }) => {
    const above = b.mid >= D.currentPrice;
    const pct = ((b.mid - D.currentPrice) / (D.currentPrice || 1)) * 100;
    return '<div class="row"><span class="' + (above ? 'short' : 'long') + '">' +
      (above ? '↑' : '↓') + ' $' + fmtPrice(b.mid) +
      ' <span class="d">' + (pct >= 0 ? '+' : '') + pct.toFixed(1) + '%</span></span>' +
      '<span>$' + fmtNum(b.value) + ' <span class="d">累计 $' + fmtNum(cum[i]) + '</span></span></div>';
  }).join('');
}

document.getElementById('viewSeg').addEventListener('click', (e) => {
  const b = e.target.closest('button'); if (!b) return;
  view = b.dataset.view;
  [...e.currentTarget.children].forEach(x => x.classList.toggle('on', x === b));
  // 清算图专属的几张卡片只在相关标签页显示，否则是干扰
  const liqView = view === 'map' || view === 'heat';
  document.querySelectorAll('.liq-only').forEach((el) => { el.hidden = !liqView; });
  document.getElementById('legend').style.display = view === 'map' ? '' : 'none';
  hoverIdx = -1; hideTip(); draw();
});

function showEmpty(msg) {
  document.getElementById('updated').textContent = '暂无快照';
  document.getElementById('chartWrap').innerHTML = '<div class="empty">' + msg + '</div>';
  document.getElementById('legend').style.display = 'none';
}

async function load() {
  let res;
  try {
    res = await fetch('/api/heatmap/' + SYMBOL);
  } catch (e) {
    // 请求本身失败（断网/被拦），不能让页面停在"加载中…"
    showEmpty('加载失败：' + (e && e.message ? e.message : '网络错误') + '<br>刷新试试');
    return;
  }
  if (!res.ok) {
    showEmpty('还没有 ' + SYMBOL + ' 的快照<br>在 Telegram 里发一次 <b>/liqmap ' + SYMBOL + '</b> 就会推上来');
    return;
  }
  D = await res.json();
  grid = expandGrid(D.cells, D.times.length, D.prices.length);

  // 两个时间要分开标：数据时刻是热力图最后一列的时间戳（数据本身"截至"何时），
  // 推送时刻是 bot 什么时候拉的。"N 分钟前"这种相对时间看不出到底是几点的数据。
  const dataTs = D.times.length ? D.times[D.times.length - 1] : D.updatedAt;
  document.getElementById('nowPrice').textContent = '$' + fmtPrice(D.currentPrice);
  document.getElementById('updated').textContent =
    '数据截至 ' + fmtTime(dataTs) + ' · 拉取于 ' + fmtTime(D.updatedAt);
  document.getElementById('intervalBadge').textContent = (D.interval || '—') + ' 粒度';

  rebuild();
}

/**
 * 每个指标的说明。写清楚三件事：**是什么、怎么算、怎么用**，
 * 以及它不能说明什么——后者往往比前者更重要。
 */
const INFO = {
  rsi: ['RSI(14) 相对强弱指标',
    '把最近 14 天的平均涨幅和平均跌幅相比，换算成 0~100 的数。用 Wilder 平滑，和 TradingView 一致。',
    '<b>怎么用</b>：传统上 &gt;70 叫超买、&lt;30 叫超卖。',
    '<span class="warn">但强趋势里 RSI 可以在 70 以上待好几周</span>，"超买"不等于要跌，它只说明近期涨得比跌得多，不说明贵不贵。'],
  ma200: ['距 MA200',
    '现价相对 200 日均线的偏离百分比。MA200 是常用的牛熊分界线。',
    '<b>怎么用</b>：正值说明在长期均线之上，负值在其下。偏离越大，均值回归的拉力理论上越强。',
    '<span class="warn">但偏离可以持续很久</span>——2021 年 BTC 在 MA200 上方 +100% 待了两个月。它是位置参照，不是择时信号。'],
  atr: ['ATR(14) 平均真实波幅',
    '最近 14 天平均每天波动多大，含跳空缺口。换算成百分比更好比较。',
    '<b>怎么用</b>：它是"多远算远"的标尺。ATR 2.9% 意味着一天内价格走 3% 属于正常波动。',
    '所以 ① 判断某个价位是"够得着"还是"随手就能到"；② 设止损或证伪条件时，<b>设在 0.5×ATR 以内的条件测的是噪声不是判断</b>。',
    'ATR 不看方向，只看幅度。'],
  basis: ['基差 Basis',
    '（永续合约价 − 现货价）÷ 现货价。这里用 OKX 的两个价格。',
    '<b>正（升水）</b>：合约比现货贵，杠杆多头愿意付溢价，情绪偏热。',
    '<b>负（贴水）</b>：合约比现货便宜，通常是空头占优或现货有抛压。',
    '<b>怎么用</b>：它和价格涨跌是两回事。价格在涨但基差转负，说明这波是现货买盘推的、杠杆没跟上——性质和杠杆推动的上涨完全不同。'],
  oi: ['持仓量 Open Interest（OI）',
    '市场上尚未平仓的合约总名义价值。<span class="warn">注意是 OI，不是 IO。</span>',
    '<b>怎么用</b>：<b>单看数值几乎没有意义</b>，必须看变化方向，且要和价格方向组合起来读：',
    '· 增仓上涨 = 新多进场，趋势相对健康<br>· 减仓上涨 = 空头回补，追高要谨慎<br>· 增仓下跌 = 空头进场施压<br>· 减仓下跌 = 多头离场',
    '切到「持仓量」标签页可以看 180 天曲线——那才看得出当前是高位还是低位。',
    '<span class="warn">本站显示的是 OKX 单家，不是全市场。</span>'],
  funding: ['资金费率 Funding Rate',
    '永续合约每 8 小时结算一次的多空互付费用，作用是把合约价格拉回现货。',
    '<b>正</b>：多头付给空头，做多的更多或更急。<b>负</b>：反之。',
    '<b>怎么用</b>：<b>看百分位比看绝对值重要得多</b>。0.01% 听起来很小，但如果是过去 33 天的第 95 百分位，说明杠杆多头已经拥挤到极值；反过来费率为正但只在第 3 百分位，说明杠杆情绪其实是冷的。',
    '括号里的百分位就是这个意思，基准是最近约 33 天（100 次结算）。'],
  lsr: ['多空比 Long/Short Ratio',
    'OKX 上持多头仓位的<b>账户数</b> ÷ 持空头仓位的账户数。',
    '<span class="warn">是账户数量的比值，不是持仓金额的比值</span>——一个大户和一个散户各算一个账户。',
    '<b>怎么用</b>：&gt;1 表示做多的账户更多。常被当<b>反向指标</b>：某一侧极端拥挤时，那一侧更容易被清算，因为止损单密集。'],
  fng: ['恐惧贪婪指数',
    '0~100 的市场情绪综合指标，由波动率、成交量、社交热度、市场占比等加权而成（来源 alternative.me）。',
    '&lt;25 极度恐惧，&gt;75 极度贪婪。',
    '<b>怎么用</b>：常作为反向参考——极度恐惧时往往接近阶段底，极度贪婪时风险累积。但它反应偏慢，且极端值可以维持很久。'],
  price: ['现价',
    'OKX 永续合约最新价。',
    '页面右上角还标了两个时刻：<b>数据截至</b>是清算图最后一列的时间戳，<b>拉取于</b>是 bot 什么时候取的这份数据。'],
  cumdist: ['累计清算量',
    '从现价往某个方向走，一路上会累计扫过多少待爆仓量。',
    '例：现价 80,000，81,000 有 1 亿、82,000 有 2 亿，那么走到 82,000 的累计值是 3 亿。',
    '数值后面带 <b>+</b> 表示目标价已超出图表覆盖范围，实际只会更多。'],
};

/** 说明浮层：桌面悬停显示，移动端点按显示。 */
const tipBox = document.getElementById('infoTip');
let tipSticky = false;

function showInfo(key, x, y) {
  const d = INFO[key];
  if (!d) return;
  tipBox.innerHTML = '<h4>' + d[0] + '</h4>' + d.slice(1).map((p) => '<p>' + p + '</p>').join('');
  tipBox.style.opacity = 1;
  const w = tipBox.offsetWidth, h = tipBox.offsetHeight;
  tipBox.style.left = Math.min(Math.max(x - w / 2, 8), window.innerWidth - w - 8) + 'px';
  tipBox.style.top = (y + h + 16 > window.innerHeight ? y - h - 12 : y + 18) + 'px';
}
function hideInfo() { if (!tipSticky) tipBox.style.opacity = 0; }

document.addEventListener('mouseover', (e) => {
  const el = e.target.closest('[data-tip]');
  if (el && !tipSticky) { const r = el.getBoundingClientRect(); showInfo(el.dataset.tip, r.left + r.width / 2, r.bottom); }
});
document.addEventListener('mouseout', (e) => { if (e.target.closest('[data-tip]')) hideInfo(); });
document.addEventListener('click', (e) => {
  const el = e.target.closest('[data-tip]');
  if (!el) { tipSticky = false; tipBox.style.opacity = 0; return; }
  const r = el.getBoundingClientRect();
  tipSticky = false;
  showInfo(el.dataset.tip, r.left + r.width / 2, r.bottom);
  tipSticky = true;
  e.stopPropagation();
});

/** 指标条：把关键数字横向铺在最上面。 */
function renderMetrics() {
  if (!B) return;
  const pct = (v, d = 1) => (v >= 0 ? '+' : '') + v.toFixed(d) + '%';
  const cells = [];
  const add = (key, label, value, sub, cls) => cells.push(
    '<div class="metric"><div class="k">' + label + ' <i class="info" data-tip="' + key + '">i</i></div>' +
    '<div class="v ' + (cls || '') + '">' + value + '</div>' +
    (sub ? '<div class="sub">' + sub + '</div>' : '') + '</div>');

  if (B.rsi14) {
    const cls = B.rsi14 >= 70 ? 'short' : (B.rsi14 <= 30 ? 'long' : '');
    add('rsi', 'RSI(14)', B.rsi14.toFixed(1), B.rsi14 >= 70 ? '超买区' : (B.rsi14 <= 30 ? '超卖区' : '中性'), cls);
  }
  if (B.vs_ma200_pct) add('ma200', '距 MA200', pct(B.vs_ma200_pct), 'MA200 $' + fmtPrice(B.ma200 || 0),
    B.vs_ma200_pct >= 0 ? 'long' : 'short');
  if (B.atr_pct) add('atr', 'ATR(14)', B.atr_pct.toFixed(2) + '%', '日均波幅 $' + fmtNum(B.atr14 || 0));
  if (B.basis_pct) add('basis', '基差', pct(B.basis_pct, 3), B.basis_pct >= 0 ? '合约升水' : '合约贴水',
    B.basis_pct >= 0 ? 'long' : 'short');
  if (B.open_interest_usd) {
    add('oi', '持仓量', '$' + fmtNum(B.open_interest_usd),
      B.oi_change_pct ? pct(B.oi_change_pct) + ' / ' + B.oi_span_minutes + '分钟' : '仅 OKX');
  }
  if (B.funding_rate) {
    const p = Math.round(B.funding_percentile || 0);
    add('funding', '资金费率', (B.funding_rate * 100 >= 0 ? '+' : '') + (B.funding_rate * 100).toFixed(4) + '%',
      p + ' 百分位' + (p >= 90 ? '（极高）' : p <= 10 ? '（极低）' : ''),
      p >= 90 ? 'short' : (p <= 10 ? 'long' : ''));
  }
  if (B.long_short_ratio) add('lsr', '多空比', B.long_short_ratio.toFixed(2),
    B.long_short_ratio >= 1 ? '多头账户更多' : '空头账户更多');
  if (B.fear_greed) add('fng', '恐惧贪婪', String(B.fear_greed), B.fear_greed_label || '');

  document.getElementById('metrics').innerHTML = cells.join('');
}

/** 盘面快照与热力图相互独立：brief 拿不到不该影响爆仓地图。 */
async function loadBrief() {
  try {
    const res = await fetch('/api/brief/' + SYMBOL);
    if (!res.ok) return;
    B = await res.json();
  } catch { return; }

  renderMetrics();

  // 健康度：不好的时候要显式说出来，藏起来会让人误以为一切正常
  const hb = document.getElementById('healthBadge');
  const ok = B.health && B.health.ok;
  hb.className = ok ? 'badge-ok' : 'badge-bad';
  hb.textContent = ok ? '数据完整' : '数据不完整';
  hb.title = ((B.health && B.health.reasons) || []).join(' / ');
  hb.hidden = false;

  const ev = B.upcoming_events || [];
  if (ev.length) {
    document.getElementById('macroBody').innerHTML = ev.map((e) => {
      const t = new Date(e.at);
      const p = (n) => String(n).padStart(2, '0');
      const name = e.title_cn ? e.title_cn + ' <span class="f">' + e.title + '</span>' : e.title;
      return '<div class="ev"><span class="t">' + (t.getMonth() + 1) + '-' + p(t.getDate()) + ' ' +
        p(t.getHours()) + ':' + p(t.getMinutes()) + ' · ' + (e.impact || '') + '</span>' +
        '<span class="n">' + name + '</span>' +
        '<span class="f">预测 ' + (e.forecast || '—') + ' · 前值 ' + (e.previous || '—') + '</span></div>';
    }).join('');
    document.getElementById('macroCard').hidden = false;
  }
}

/** 长周期序列：价格+MA200、持仓量曲线。拿不到就把对应标签页禁掉。 */
async function loadSeries() {
  try {
    const res = await fetch('/api/series/' + SYMBOL);
    if (!res.ok) return;
    S = await res.json();
  } catch { return; }
  if (view === 'trend' || view === 'oi') draw();
}

let resizeTimer = 0;
window.addEventListener('resize', () => {
  if (!D) return;
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(rebuild, 120);
});
load();
loadBrief();
loadSeries();
</script>
</body>
</html>`;
}
