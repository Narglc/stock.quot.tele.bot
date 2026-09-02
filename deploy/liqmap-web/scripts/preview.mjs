/**
 * 生成一份可离线打开的预览页：真实 worker.js 渲染的 HTML + 内联的快照数据。
 *
 * 用法：
 *   node scripts/preview.mjs                          # 用 test/fixtures/btc-snapshot.json
 *   node scripts/preview.mjs path/to/snapshot.json    # 指定别的快照
 *   输出到 dist/preview.html
 *
 * 快照数据从哪来：bot 侧跑一次
 *   LIVE=1 APIFY_TOKEN=xxx SNAPSHOT_OUT=deploy/liqmap-web/test/fixtures/btc-snapshot.json \
 *     go test ./stocks/ -run TestLive_DumpLiqSnapshot
 * 会真的打一次 Apify（计费），存下来后可反复使用。
 *
 * 注入方式是拦截 window.fetch 而不是改写页面源码——这样 load() 怎么改都不影响预览。
 */
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join, resolve } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, '..');
const worker = (await import(join(root, 'src/worker.js'))).default;

const snapPath = resolve(process.argv[2] || join(root, 'test/fixtures/btc-snapshot.json'));
let snap;
try {
  snap = JSON.parse(readFileSync(snapPath, 'utf8'));
} catch (e) {
  console.error(`读不到快照 ${snapPath}：${e.message}`);
  console.error('先按文件头注释里的命令生成一份真实快照。');
  process.exit(1);
}
if (!snap.updatedAt) snap.updatedAt = Date.now();
const symbol = (snap.symbol || 'BTC').toUpperCase();

const env = { ALLOWED_SYMBOLS: symbol, PUSH_TOKEN: 'x',
              LIQMAP: { async get() { return null; }, async put() {} } };
const res = await worker.fetch(new Request(`https://preview.local/${symbol}`), env);
let html = await res.text();

// 在页面脚本之前插一段 fetch 拦截：命中 /api/heatmap/ 就直接回内联数据。
const shim = `<script>
(() => {
  const DATA = ${JSON.stringify(snap)};
  const orig = window.fetch.bind(window);
  window.fetch = (input, init) => {
    const url = typeof input === 'string' ? input : input.url;
    if (url.startsWith('/api/heatmap/')) {
      return Promise.resolve(new Response(JSON.stringify(DATA), {
        status: 200, headers: { 'content-type': 'application/json' } }));
    }
    return orig(input, init);
  };
})();
</script>
<script>`;
const marker = '<script>\n/* --- PURE:START ---';
if (!html.includes(marker)) {
  console.error('worker.js 里找不到 PURE:START 标记，预览注入点丢了');
  process.exit(1);
}
html = html.replace(marker, shim + '\n/* --- PURE:START ---');

mkdirSync(join(root, 'dist'), { recursive: true });
const out = join(root, 'dist/preview.html');
writeFileSync(out, html);
console.log(`preview → ${out}  (${(html.length / 1024).toFixed(0)} KB, ${snap.cells.length} 个非零格, 数据截至 ${new Date(snap.times.at(-1)).toISOString()})`);
