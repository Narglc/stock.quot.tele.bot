/**
 * 页面里的纯逻辑测试。
 *
 * canvas 的实际绘制没法在无头环境验证，但坐标换算、稀疏展开、颜色映射这些
 * 才是真正容易出错的地方——它们全在 worker.js 的 PURE 标记块里，
 * 这里把那段原样提取出来执行，保证测的就是线上跑的那份代码。
 *
 * 跑：node --test test/
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(here, '../src/worker.js'), 'utf8');

const m = src.match(/\/\* --- PURE:START ---[\s\S]*?\*\/([\s\S]*?)\/\* --- PURE:END --- \*\//);
assert.ok(m, 'worker.js 里找不到 PURE 标记块——改动时不要删掉那两个标记');

const pure = {};
new Function(`${m[1]}; Object.assign(this, { fmtNum, fmtPrice, fmtTime, heatColor, expandGrid, latestColumn, bucketize, bucketAtX, cumulate, pivotIndex, cumAtPrice });`).call(pure);
const { fmtNum, fmtPrice, heatColor, expandGrid, latestColumn, bucketize, bucketAtX, cumulate, pivotIndex, cumAtPrice } = pure;

test('fmtNum 按量级压缩', () => {
  assert.equal(fmtNum(2.1e9), '2.10B');
  assert.equal(fmtNum(4.5e6), '4.50M');
  assert.equal(fmtNum(6700), '6.7K');   // K 档 1 位小数，标签更紧凑
  assert.equal(fmtNum(1.5e12), '1.50T');
  assert.equal(fmtNum(123), '123');
});

test('fmtPrice 按量级决定小数位', () => {
  // 图表标签上 "65,000" 比 "65,000.00" 清爽，低价币则必须保留小数否则全是 0
  assert.equal(fmtPrice(65000), '65,000');
  assert.equal(fmtPrice(48.2), '48.20');
  assert.equal(fmtPrice(0.5), '0.5000');
});

test('heatColor：零值不上色，值越大越靠暖端', () => {
  assert.equal(heatColor(0, 100), null);
  assert.equal(heatColor(-5, 100), null);

  const lo = heatColor(1, 1e9);
  const hi = heatColor(1e9, 1e9);
  assert.ok(lo.startsWith('rgb('), '应返回 rgb 字符串');
  assert.notEqual(lo, hi, '不同量级应有不同颜色');

  const red = (c) => Number(c.match(/rgb\((\d+)/)[1]);
  assert.ok(red(hi) > red(lo), '高值应更偏红');
});

test('heatColor 用对数映射：跨数量级仍能区分', () => {
  // 线性映射下 1e6 和 1e7 在 1e9 的尺度上会挤成同一个色。
  const a = heatColor(1e6, 1e9);
  const b = heatColor(1e7, 1e9);
  assert.notEqual(a, b, '相差一个数量级的值必须可区分');
});

test('expandGrid 还原稀疏三元组', () => {
  const g = expandGrid([[0, 0, 5], [2, 1, 9]], 3, 2);
  assert.equal(g.length, 6);
  assert.equal(g[0 * 3 + 0], 5);
  assert.equal(g[1 * 3 + 2], 9);
  assert.equal(g[1 * 3 + 0], 0, '未填充处应为 0');
});

test('expandGrid 丢弃越界格子而不是崩掉', () => {
  const g = expandGrid([[99, 99, 1], [-1, 0, 2], [0, 0, 3]], 2, 2);
  assert.equal(g[0], 3);
  assert.equal(g.reduce((a, b) => a + b, 0), 3, '越界数据不应被写入');
});

test('latestColumn：只取最新一个时间格', () => {
  // grid 索引 y*T+x，T=3 P=2：每个价格档有 3 个时间格，取最后一个
  const g = Float64Array.from([1, 2, 3, 10, 20, 30]);
  const out = latestColumn(g, 3, 2);
  assert.equal(out[0], 3);
  assert.equal(out[1], 30);
});

// 这条测试守住的是一个曾经犯过的概念错误：把历史列求和当成"累计"。
// 每列都是那一时刻的完整快照，同一笔仓位没被扫掉就会在每列重复出现，
// 求和等于把它算 N 遍。正确的"累计"是 cumulate——从现价沿价格轴向外累加。
test('latestColumn 不把历史列加进来', () => {
  const g = Float64Array.from([100, 100, 100]); // 同一价位在 3 个时间格里都是 100
  const out = latestColumn(g, 3, 1);
  assert.equal(out[0], 100, '应是 100 而不是 300');
});

test('bucketize 把逐档并成价格桶，总额守恒', () => {
  const prices = [100, 110, 120, 130, 140, 150];
  const values = Float64Array.from([1, 2, 3, 4, 5, 6]);
  const bs = bucketize(prices, values, 3);
  assert.equal(bs.length, 3);
  const total = bs.reduce((a, b) => a + b.value, 0);
  assert.equal(total, 21, '合并后总额不能丢');
  // 桶按价格升序，且区间连续
  for (let i = 1; i < bs.length; i++) {
    assert.ok(bs[i].lo >= bs[i-1].lo, '桶应按价格升序');
    assert.ok(bs[i].mid > bs[i-1].mid);
  }
});

test('bucketize 桶数不超过原始档位数', () => {
  const bs = bucketize([1, 2], Float64Array.from([5, 5]), 50);
  assert.equal(bs.length, 2, '只有 2 档时不该造出 50 个桶');
});

test('bucketize 处理空输入', () => {
  assert.deepEqual(bucketize([], Float64Array.from([]), 10), []);
});

test('bucketize 所有价格相同时不除零', () => {
  const bs = bucketize([100, 100, 100], Float64Array.from([1, 2, 3]), 4);
  assert.ok(bs.length >= 1);
  assert.equal(bs.reduce((a, b) => a + b.value, 0), 6);
});

test('bucketAtX：价格轴从左到右递增', () => {
  const box = { x: 0, y: 0, w: 100, h: 100 };
  assert.equal(bucketAtX(1, box, 10), 0, '最左 = 第 0 个桶（最低价）');
  assert.equal(bucketAtX(99, box, 10), 9, '最右 = 最后一个桶（最高价）');
  assert.equal(bucketAtX(50, box, 10), 5);
});

test('bucketAtX：区域外返回 -1', () => {
  const box = { x: 10, y: 0, w: 100, h: 100 };
  assert.equal(bucketAtX(5, box, 10), -1);
  assert.equal(bucketAtX(200, box, 10), -1);
});

test('bucketAtX：每个像素都落在合法桶内', () => {
  const box = { x: 0, y: 0, w: 100, h: 100 };
  const n = 37;
  for (let mx = 0; mx <= 100; mx++) {
    const i = bucketAtX(mx, box, n);
    if (i < 0) continue;
    assert.ok(i >= 0 && i < n, `越界: ${i}`);
  }
});

const mkBuckets = (mids, vals) => mids.map((mid, i) => ({ lo: mid - 5, hi: mid + 5, mid, value: vals[i] }));

test('pivotIndex 找到现价分界桶', () => {
  const bs = mkBuckets([10, 20, 30, 40], [1, 1, 1, 1]);
  assert.equal(pivotIndex(bs, 25), 2, '第一个 mid >= 25 的是索引 2');
  assert.equal(pivotIndex(bs, 10), 0, '正好等于第一个 mid');
  assert.equal(pivotIndex(bs, 5), 0, '现价低于全部');
  assert.equal(pivotIndex(bs, 100), 4, '现价高于全部时返回长度');
});

test('cumulate：从现价向两边各自独立累计', () => {
  //        mid: 10  20  30  40  50
  //      value:  1   2   3   4   5
  // 现价 25 → pivot=2（mid=30）
  const bs = mkBuckets([10, 20, 30, 40, 50], [1, 2, 3, 4, 5]);
  const c = cumulate(bs, 25);

  // 向右（涨）：30 → 3, 40 → 3+4=7, 50 → 3+4+5=12
  assert.equal(c[2], 3);
  assert.equal(c[3], 7);
  assert.equal(c[4], 12);
  // 向左（跌）：20 → 2, 10 → 2+1=3
  assert.equal(c[1], 2);
  assert.equal(c[0], 3);
});

test('cumulate：两侧末端等于该侧总量', () => {
  const bs = mkBuckets([10, 20, 30, 40, 50], [1, 2, 3, 4, 5]);
  const c = cumulate(bs, 25);
  const below = 1 + 2, above = 3 + 4 + 5;
  assert.equal(c[0], below, '最左端 = 下跌方向总量');
  assert.equal(c[c.length - 1], above, '最右端 = 上涨方向总量');
});

test('cumulate：现价在区间外时一侧为空', () => {
  const bs = mkBuckets([10, 20, 30], [1, 2, 3]);

  const allAbove = cumulate(bs, 5);   // 现价低于全部 → 全在上涨方向
  assert.equal(allAbove[0], 1);
  assert.equal(allAbove[2], 6);

  const allBelow = cumulate(bs, 100); // 现价高于全部 → 全在下跌方向
  assert.equal(allBelow[2], 3);
  assert.equal(allBelow[0], 6);
});

test('cumulate：单调不减（沿远离现价的方向）', () => {
  const bs = mkBuckets([10, 20, 30, 40, 50, 60], [2, 0, 5, 1, 0, 3]);
  const c = cumulate(bs, 35);
  const pivot = pivotIndex(bs, 35);
  for (let i = pivot + 1; i < bs.length; i++) {
    assert.ok(c[i] >= c[i - 1], `向右累计不该下降: ${c[i-1]} → ${c[i]}`);
  }
  for (let i = pivot - 2; i >= 0; i--) {
    assert.ok(c[i] >= c[i + 1], `向左累计不该下降: ${c[i+1]} → ${c[i]}`);
  }
});

test('cumulate：空输入不崩', () => {
  assert.equal(cumulate([], 100).length, 0);
});

test('cumAtPrice：目标价越远累计越多', () => {
  //  mid: 90 95 105 110 (桶宽 5)
  // value: 1  2   3   4
  const bs = [90, 95, 105, 110].map((mid, i) => ({ lo: mid - 2.5, hi: mid + 2.5, mid, value: [1,2,3,4][i] }));
  const c = cumulate(bs, 100);

  const up1 = cumAtPrice(bs, c, 100, 106);
  assert.equal(up1.value, 3, '涨到 106 只扫到 105 那档');
  const up2 = cumAtPrice(bs, c, 100, 111);
  assert.equal(up2.value, 7, '涨到 111 扫到两档');

  const dn1 = cumAtPrice(bs, c, 100, 94);
  assert.equal(dn1.value, 2, '跌到 94 只扫到 95 那档');
  const dn2 = cumAtPrice(bs, c, 100, 89);
  assert.equal(dn2.value, 3, '跌到 89 扫到两档');
});

test('cumAtPrice：超出图表范围要标 clipped', () => {
  const bs = [90, 110].map((mid, i) => ({ lo: mid - 5, hi: mid + 5, mid, value: [1, 2][i] }));
  const c = cumulate(bs, 100);

  const far = cumAtPrice(bs, c, 100, 500);
  assert.equal(far.value, 2, '超出范围返回该侧总量');
  assert.equal(far.clipped, true, '必须标记 clipped，否则会被误读成"再远就没有了"');

  const near = cumAtPrice(bs, c, 100, 108);
  assert.equal(near.clipped, false);
});

test('cumAtPrice：目标价就是现价时为 0', () => {
  const bs = [90, 110].map((mid, i) => ({ lo: mid - 5, hi: mid + 5, mid, value: [1, 2][i] }));
  const c = cumulate(bs, 100);
  assert.equal(cumAtPrice(bs, c, 100, 100).value, 0);
});

test('cumAtPrice：空桶不崩', () => {
  const r = cumAtPrice([], new Float64Array(0), 100, 110);
  assert.equal(r.value, 0);
  assert.equal(r.clipped, false);
});

test('cumAtPrice 与 cumulate 末端一致', () => {
  const mids = [80, 90, 110, 120];
  const bs = mids.map((mid, i) => ({ lo: mid - 5, hi: mid + 5, mid, value: [1,2,3,4][i] }));
  const c = cumulate(bs, 100);
  // 走到最边缘应等于该侧总量
  assert.equal(cumAtPrice(bs, c, 100, 125).value, c[c.length - 1]);
  assert.equal(cumAtPrice(bs, c, 100, 75).value, c[0]);
});

// ---- 以下用真实快照跑：合成数据能过的不变量，真实分布未必能过 ----
import { existsSync } from 'node:fs';
const fixturePath = join(here, 'fixtures/btc-snapshot.json');
const hasFixture = existsSync(fixturePath);
const real = hasFixture ? JSON.parse(readFileSync(fixturePath, 'utf8')) : null;

test('真实快照：形状自洽', { skip: !hasFixture && '缺 fixtures/btc-snapshot.json' }, () => {
  const T = real.times.length, P = real.prices.length;
  assert.ok(T > 0 && P > 0);
  for (let i = 1; i < P; i++) assert.ok(real.prices[i] > real.prices[i - 1], '价格轴应严格递增');
  for (let i = 1; i < T; i++) assert.ok(real.times[i] > real.times[i - 1], '时间轴应严格递增');
  for (const [x, y, v] of real.cells) {
    assert.ok(x >= 0 && x < T && y >= 0 && y < P, `格子越界 [${x},${y}]`);
    assert.ok(v > 0, '稀疏格式里不该有 0 值');
  }
  assert.ok(real.currentPrice > real.prices[0] && real.currentPrice < real.prices[P - 1], '现价应落在价格轴范围内');
});

test('真实快照：整条管线跑通且累计守恒', { skip: !hasFixture && '缺 fixture' }, () => {
  const T = real.times.length, P = real.prices.length;
  const grid = expandGrid(real.cells, T, P);
  const col = latestColumn(grid, T, P);
  const buckets = bucketize(real.prices, col, 90);
  const cum = cumulate(buckets, real.currentPrice);

  const colTotal = col.reduce((a, b) => a + b, 0);
  const bucketTotal = buckets.reduce((a, b) => a + b.value, 0);
  assert.ok(Math.abs(colTotal - bucketTotal) < 1e-6 * colTotal, '分桶不能丢量');

  const pivot = pivotIndex(buckets, real.currentPrice);
  const above = buckets.slice(pivot).reduce((a, b) => a + b.value, 0);
  const below = buckets.slice(0, pivot).reduce((a, b) => a + b.value, 0);
  assert.ok(Math.abs(cum[buckets.length - 1] - above) < 1e-6 * (above || 1), '右端累计 = 上方总量');
  assert.ok(pivot === 0 || Math.abs(cum[0] - below) < 1e-6 * (below || 1), '左端累计 = 下方总量');
  assert.ok(colTotal > 0, '最新列不该全零');
});

test('真实快照：±5% 累计不超过该侧总量', { skip: !hasFixture && '缺 fixture' }, () => {
  const T = real.times.length, P = real.prices.length;
  const buckets = bucketize(real.prices, latestColumn(expandGrid(real.cells, T, P), T, P), 90);
  const cum = cumulate(buckets, real.currentPrice);
  const pivot = pivotIndex(buckets, real.currentPrice);
  const above = buckets.slice(pivot).reduce((a, b) => a + b.value, 0);
  const below = buckets.slice(0, pivot).reduce((a, b) => a + b.value, 0);
  const up = cumAtPrice(buckets, cum, real.currentPrice, real.currentPrice * 1.05);
  const dn = cumAtPrice(buckets, cum, real.currentPrice, real.currentPrice * 0.95);
  assert.ok(up.value <= above + 1e-6, '+5% 累计不能超过上方总量');
  assert.ok(dn.value <= below + 1e-6, '-5% 累计不能超过下方总量');
});
