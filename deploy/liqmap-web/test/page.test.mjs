/**
 * 页面脚本的语法检查。
 *
 * 为什么单独测：页面 HTML 是 worker.js 里的一个模板字符串，`node --check src/worker.js`
 * 只检查外层模块，**模板字符串的内容它不看**。而且模板字符串会自己解析转义——
 * 源码里写 '\n' 会在生成的 HTML 里变成真实换行，直接造成语法错误。
 * 这类 bug 只有把渲染出来的脚本单独编译一次才能抓到（踩过一次，页面白屏）。
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const worker = (await import(join(here, '../src/worker.js'))).default;

const env = {
  ALLOWED_SYMBOLS: 'BTC',
  PUSH_TOKEN: 'x',
  LIQMAP: { async get() { return null; }, async put() {} },
};

async function renderPage(symbol = 'BTC') {
  const res = await worker.fetch(new Request(`https://t.local/${symbol}`), env);
  assert.equal(res.status, 200);
  return res.text();
}

test('渲染出来的页面脚本语法合法', async () => {
  const html = await renderPage();
  const blocks = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map((m) => m[1]);
  assert.ok(blocks.length > 0, '页面里应有 script 块');

  for (const [i, js] of blocks.entries()) {
    // new Function 只编译不执行，正好用来验证语法
    assert.doesNotThrow(() => new Function(js), `第 ${i + 1} 个 script 块语法错误`);
  }
});

test('页面脚本里没有被模板字符串吃掉的转义', async () => {
  const html = await renderPage();
  const js = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map((m) => m[1]).join('\n');
  // 单引号字符串里出现真实换行 = 转义被模板字符串解析掉了
  const badLines = js.split('\n').filter((line) => {
    const singles = (line.match(/'/g) || []).length;
    return singles % 2 === 1 && !line.trim().startsWith('//') && !line.includes('/*');
  });
  assert.deepEqual(badLines, [], '这些行的单引号没闭合，多半是 \\n 被吃了');
});

test('页面带上必要的结构与口径说明', async () => {
  const html = await renderPage();
  for (const marker of [
    'id="metrics"',        // 指标条
    'macroCard',           // 宏观卡片
    'infoTip',             // 说明浮层
    'data-view="trend"',   // 长期趋势标签页
    'data-view="oi"',      // 持仓量标签页
    'Binance 合约',        // 数据口径
    '预估待爆仓分布',
    'loadBrief()', 'loadSeries()',
  ]) {
    assert.ok(html.includes(marker), `页面缺少 ${marker}`);
  }
});

// 每个指标都必须有说明——数字不解释就是噪声。
test('每个指标条目都挂了说明', async () => {
  const html = await renderPage();
  const js = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map((m) => m[1]).join('\n');

  // 从 renderMetrics 里抽出用到的 data-tip key
  const used = [...js.matchAll(/add\('([a-z0-9]+)',/g)].map((m) => m[1]);
  assert.ok(used.length >= 6, `指标数量偏少: ${used.length}`);

  // 每个 key 都要在 INFO 里有条目
  const defined = [...js.matchAll(/^  ([a-z0-9]+): \[/gm)].map((m) => m[1]);
  for (const k of used) {
    assert.ok(defined.includes(k), `指标 ${k} 没有对应的说明文案`);
  }
});
