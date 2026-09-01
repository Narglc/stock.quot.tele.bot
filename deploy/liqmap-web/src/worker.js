/**
 * liqmap-web —— 清算热力图的只读网页。
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

    if (path.startsWith("/api/heatmap/")) {
      const symbol = path.slice("/api/heatmap/".length).toUpperCase();
      if (!SYMBOL_RE.test(symbol)) return json({ error: "bad symbol" }, 400);

      if (request.method === "PUT") return handlePut(request, env, symbol);
      if (request.method === "GET") return handleGet(env, symbol);
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

/** 写入快照。用固定长度比较避免时序侧信道。 */
async function handlePut(request, env, symbol) {
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
  if (!Array.isArray(body.prices) || !Array.isArray(body.times) || !Array.isArray(body.cells)) {
    return json({ error: "missing prices/times/cells" }, 400);
  }

  body.symbol = symbol;
  body.updatedAt = Date.now();
  // 快照 24h 过期：拿不到新数据时宁可显示"无数据"，也不要展示一张看不出有多旧的图。
  await env.LIQMAP.put(`heatmap:${symbol}`, JSON.stringify(body), { expirationTtl: 86400 });
  return json({ ok: true, symbol, cells: body.cells.length });
}

async function handleGet(env, symbol) {
  const raw = await env.LIQMAP.get(`heatmap:${symbol}`);
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
  .price .sub { font-size:11px; color:var(--dim2); }

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
    <span class="badge" id="intervalBadge">—</span>
  </div>
  ${nav}
  <div class="price">
    <span class="n" id="nowPrice">—</span>
    <span class="sub" id="updated">加载中…</span>
  </div>
</header>

<div class="controls">
  <div class="seg" id="rangeSeg">
    <button data-range="last" class="on">最近一格</button>
    <button data-range="all">累计全部</button>
  </div>
  <div class="seg" id="viewSeg">
    <button data-view="map" class="on">地图</button>
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
    <div class="card">
      <h2>走到这里会扫掉多少</h2>
      <table class="dist"><tbody id="distTable"></tbody></table>
    </div>
    <div class="card">
      <h2>两侧合计</h2>
      <div class="split">
        <div class="stat"><div class="lbl">↓ 下跌方向</div><div class="val long" id="sumBelow">–</div></div>
        <div class="stat"><div class="lbl">↑ 上涨方向</div><div class="val short" id="sumAbove">–</div></div>
      </div>
      <div class="ratio" id="ratio"></div>
    </div>
    <div class="card">
      <h2>清算簇 TOP</h2>
      <div id="clusters"></div>
    </div>
    <div class="card">
      <h2>怎么看</h2>
      <div class="note">
        横轴价格，<b>柱子高度</b>是该价位堆积的待爆仓名义额。<br><br>
        两条<b>曲线</b>是从当前价往两边的累计量：「价格走到这里，一路上一共会扫掉多少」。
        单档柱子高不代表什么，<b>曲线斜率陡的那一段</b>才是真正的密集区。<br><br>
        上表把这件事直接算好了——不用自己去图上找。
      </div>
    </div>
  </aside>
</main>

<footer>
  数据来源 CoinAnk，经 bot 推送；「最近一格」是最新一个时间粒度的快照，「累计全部」是整段时间叠加。<br>
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
 * 按价格档位聚合清算额。
 * range='last' 取最新一个时间格；range='all' 把所有时间列累加。
 */
function aggregate(grid, T, P, range) {
  const out = new Float64Array(P);
  if (range === 'last') {
    const x = T - 1;
    for (let y = 0; y < P; y++) out[y] = grid[y * T + x];
  } else {
    for (let y = 0; y < P; y++) {
      let s = 0;
      for (let x = 0; x < T; x++) s += grid[y * T + x];
      out[y] = s;
    }
  }
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

let D = null, grid = null, buckets = [], cum = null, box = null;
let range = 'last', view = 'map', hoverIdx = -1, raf = 0;

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
  if (!D) return;
  layout();
  ctx.clearRect(0, 0, cv.width, cv.height);
  view === 'heat' ? drawHeat() : drawMap();
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
  const agg = aggregate(grid, D.times.length, D.prices.length, range);
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

document.getElementById('rangeSeg').addEventListener('click', (e) => {
  const b = e.target.closest('button'); if (!b) return;
  range = b.dataset.range;
  [...e.currentTarget.children].forEach(x => x.classList.toggle('on', x === b));
  rebuild();
});
document.getElementById('viewSeg').addEventListener('click', (e) => {
  const b = e.target.closest('button'); if (!b) return;
  view = b.dataset.view;
  [...e.currentTarget.children].forEach(x => x.classList.toggle('on', x === b));
  document.getElementById('legend').style.display = view === 'heat' ? 'none' : '';
  hoverIdx = -1; hideTip(); draw();
});

async function load() {
  const res = await fetch('/api/heatmap/' + SYMBOL);
  if (!res.ok) {
    document.getElementById('updated').textContent = '暂无快照';
    document.getElementById('chartWrap').innerHTML =
      '<div class="empty">还没有 ' + SYMBOL + ' 的快照<br>在 Telegram 里发一次 <b>/liqmap ' + SYMBOL + '</b> 就会推上来</div>';
    document.getElementById('legend').style.display = 'none';
    return;
  }
  D = await res.json();
  grid = expandGrid(D.cells, D.times.length, D.prices.length);

  const age = Math.round((Date.now() - D.updatedAt) / 60000);
  document.getElementById('nowPrice').textContent = '$' + fmtPrice(D.currentPrice);
  document.getElementById('updated').textContent = age <= 0 ? '刚刚更新' : age + ' 分钟前更新';
  document.getElementById('intervalBadge').textContent = D.interval || '—';

  rebuild();
}

let resizeTimer = 0;
window.addEventListener('resize', () => {
  if (!D) return;
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(rebuild, 120);
});
load();
</script>
</body>
</html>`;
}
