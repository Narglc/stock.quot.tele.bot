#!/usr/bin/env python3
"""用本地保存的响应渲染清算热力图 / 清算地图（离线、可复用）。
数据源：
  liqmap_sample.json  —— Apify(CoinAnk) 清算热力图响应
  klines_sample.json  —— OKX 15m K线（叠加真实价格蜡烛）
用法： python3 render_liqmap.py [liqmap.json] [klines.json]
依赖： pip install --break-system-packages matplotlib numpy
"""
import json, sys, os, numpy as np
import matplotlib; matplotlib.use('Agg')
import matplotlib.pyplot as plt

HERE = os.path.dirname(os.path.abspath(__file__))
liq_path = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, 'liqmap_sample.json')
kl_path  = sys.argv[2] if len(sys.argv) > 2 else os.path.join(HERE, 'klines_sample.json')

# ---- 清算热力图网格 ----
o = json.load(open(liq_path)); o = o[0] if isinstance(o, list) else o
lm = o['liqHeatMap']
prices = np.array(lm['priceArray'], dtype=float)
cta = np.array(lm['chartTimeArray'], dtype=np.int64)   # 每列时间戳(ms)
T, P = len(cta), len(prices)
grid = np.zeros((P, T))
for x, y, v in lm['data']:
    fv = float(v)
    if fv: grid[int(y), int(x)] = fv
grid /= 1e6   # -> 百万 USD
interval = o.get('chartInterval', '15m')

# ---- OKX K线，按时间戳对齐到热力图各列 ----
candles = None
if os.path.exists(kl_path):
    kd = json.load(open(kl_path)).get('data', [])
    if kd:
        arr = np.array([[int(r[0])] + [float(r[i]) for i in (1, 2, 3, 4)] for r in kd])  # ts,o,h,l,c
        arr = arr[arr[:, 0].argsort()]                     # 按时间升序
        kts, ko, kh, kl_, kc = arr[:, 0], arr[:, 1], arr[:, 2], arr[:, 3], arr[:, 4]
        # 每个热力图列取最近的一根K线
        idx = np.clip(np.searchsorted(kts, cta), 1, len(kts) - 1)
        left = cta - kts[idx - 1]; right = kts[idx] - cta
        idx = np.where(left <= right, idx - 1, idx)
        candles = (ko[idx], kh[idx], kl_[idx], kc[idx])

cur = float(os.environ.get('CUR', candles[3][-1] if candles else prices[len(prices)//2]))
print(f"grid {P}x{T}  price {prices.min():.0f}-{prices.max():.0f}  cur={cur:.1f}  candles={'yes' if candles else 'no'}")

def draw_candles(ax):
    if not candles: return
    o_, h_, l_, c_ = candles
    up = c_ >= o_
    x = np.arange(T)
    # 影线
    ax.vlines(x, l_, h_, color=np.where(up, '#26ffa0', '#ff5c7a'), lw=0.5, alpha=0.9)
    # 实体
    for i in range(T):
        lo, hi = min(o_[i], c_[i]), max(o_[i], c_[i])
        ax.add_patch(plt.Rectangle((i - 0.35, lo), 0.7, max(hi - lo, 1e-6),
                     color=('#26ffa0' if up[i] else '#ff5c7a'), alpha=0.9, lw=0))

def heatmap(path, dpi=130):
    fig, ax = plt.subplots(figsize=(12, 6.5))
    im = ax.pcolormesh(np.arange(T), prices, grid, cmap='inferno', shading='auto')
    draw_candles(ax)
    ax.axhline(cur, color='#00e5ff', lw=1.0, ls='--', alpha=0.7)
    ax.text(T*0.99, cur, f' now ${cur:,.0f}', color='#00e5ff', ha='right', va='bottom', fontweight='bold')
    fig.colorbar(im, ax=ax, label='Liq notional (M USD)')
    ax.set_xlim(0, T-1); ax.set_ylim(prices.min(), prices.max())
    ax.set_xlabel(f'Time ({T} x {interval} ~ 3 days ->)'); ax.set_ylabel('Price (USD)')
    ax.set_title(f'BTC Liquidation Heatmap {P}x{T} + OKX candles (REAL)')
    plt.tight_layout(); plt.savefig(path, dpi=dpi); plt.close()

def bars(path, dpi=120):
    snap = grid[:, -1]; fig, ax = plt.subplots(figsize=(9, 9))
    for p, v in zip(prices, snap):
        if v > 0:
            ax.barh(p, v, height=(prices[1]-prices[0])*0.9,
                    color=('#e74c3c' if p >= cur else '#2ecc71'), alpha=0.85)
    ax.axhline(cur, color='#3498db', lw=1.6, ls='--')
    ax.text(ax.get_xlim()[1]*0.98, cur, f' now ${cur:,.0f}', color='#3498db', ha='right', va='bottom', fontweight='bold')
    ax.set_xlabel('Liquidation notional (M USD)'); ax.set_ylabel('Price (USD)')
    ax.set_title('BTC Liquidation Map (REAL, latest column)\nred=short liqs above / green=long liqs below')
    ax.grid(axis='x', ls=':', alpha=0.4); plt.tight_layout(); plt.savefig(path, dpi=dpi); plt.close()

heatmap(os.path.join(HERE, 'real_heatmap.png'))
heatmap(os.path.join(HERE, 'real_heatmap.svg'))
bars(os.path.join(HERE, 'real_liqmap_bar.png'))
print("rendered -> heatmap/real_heatmap.{png,svg}, real_liqmap_bar.png")
