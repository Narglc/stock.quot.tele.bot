package stocks

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sort"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// 纯 Go（image + image/png + goregular 矢量字体）渲染清算图，无需 ffmpeg/matplotlib。
// 相比早期版本：分辨率提升、抗锯齿矢量字体、热力图叠加 OKX K线蜡烛。

var (
	colBg     = color.RGBA{18, 18, 24, 255}
	colText   = color.RGBA{228, 228, 232, 255}
	colAxis   = color.RGBA{150, 150, 162, 255}
	colNow    = color.RGBA{0, 229, 255, 255}  // 现价线：青
	colShort  = color.RGBA{231, 76, 60, 255}  // 上方空头爆仓：红
	colLong   = color.RGBA{46, 204, 113, 255} // 下方多头爆仓：绿
	colCandUp = color.RGBA{38, 255, 140, 255} // 上涨蜡烛：亮绿
	colCandDn = color.RGBA{255, 92, 122, 255} // 下跌蜡烛：亮红
)

// 矢量字体（goregular），parse 一次缓存；失败回落 basicfont。
var (
	fontOnce       sync.Once
	faceSm, faceLg font.Face
)

func loadFonts() {
	fontOnce.Do(func() {
		ft, err := opentype.Parse(goregular.TTF)
		if err != nil {
			faceSm, faceLg = basicfont.Face7x13, basicfont.Face7x13
			return
		}
		faceSm, _ = opentype.NewFace(ft, &opentype.FaceOptions{Size: 13, DPI: 72, Hinting: font.HintingFull})
		faceLg, _ = opentype.NewFace(ft, &opentype.FaceOptions{Size: 16, DPI: 72, Hinting: font.HintingFull})
		if faceSm == nil {
			faceSm = basicfont.Face7x13
		}
		if faceLg == nil {
			faceLg = basicfont.Face7x13
		}
	})
}

// RenderLiqHeatmapPNG 渲染完整清算热力图（时间×价格网格 + K线蜡烛 + 现价线）为 PNG。
func RenderLiqHeatmapPNG(h *LiqHeatmap) ([]byte, error) {
	P, T := len(h.Prices), len(h.Times)
	if P == 0 || T == 0 {
		return nil, fmt.Errorf("空热力图")
	}
	loadFonts()
	const mL, mR, mT, mB = 80, 18, 30, 26
	const plotW, plotH = 1280, 600
	W, H := mL+plotW+mR, mT+plotH+mB
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	fillRect(img, 0, 0, W, H, colBg)

	maxV := h.MaxValue
	if maxV <= 0 {
		maxV = 1
	}
	pMin, pMax := h.Prices[0], h.Prices[P-1]
	if pMax <= pMin {
		pMax = pMin + 1
	}
	priceY := func(p float64) int { return mT + int((pMax-p)/(pMax-pMin)*float64(plotH)) }
	clampY := func(y int) int {
		if y < mT {
			return mT
		}
		if y > mT+plotH {
			return mT + plotH
		}
		return y
	}

	// 逐格填色
	for yi := 0; yi < P; yi++ {
		yTop := mT + int(float64(P-1-yi)/float64(P)*float64(plotH))
		yBot := mT + int(float64(P-yi)/float64(P)*float64(plotH))
		row := h.Values[yi]
		for xi := 0; xi < T; xi++ {
			if v := row[xi]; v > 0 {
				x0 := mL + int(float64(xi)/float64(T)*float64(plotW))
				x1 := mL + int(float64(xi+1)/float64(T)*float64(plotW))
				fillRect(img, x0, yTop, x1, yBot, heatColor(v/maxV))
			}
		}
	}

	// 叠加 K线蜡烛（与时间轴对齐）
	drawCandles(img, h, mL, plotW, T, priceY, clampY)

	// 现价线 + 标注
	if h.CurrentPrice > pMin && h.CurrentPrice < pMax {
		cy := priceY(h.CurrentPrice)
		drawHLine(img, mL, mL+plotW, cy, colNow)
		drawText(img, mL+plotW-130, cy-4, "now $"+priceLabel(h.CurrentPrice), colNow, faceSm)
	}
	// y 轴价格刻度
	for i := 0; i <= 8; i++ {
		p := pMin + (pMax-pMin)*float64(i)/8
		drawText(img, 4, priceY(p)+5, "$"+priceLabel(p), colAxis, faceSm)
	}
	drawText(img, mL, 20, fmt.Sprintf("%s Liquidation Heatmap + candles   %dx%d   %s", h.Symbol, P, T, h.Interval), colText, faceLg)
	drawText(img, mL, H-8, "<- older        time        now ->", colAxis, faceSm)

	return encodePNG(img)
}

// drawCandles 把 K线蜡烛按时间对齐叠加到热力图上。
func drawCandles(img *image.RGBA, h *LiqHeatmap, mL, plotW, T int, priceY func(float64) int, clampY func(int) int) {
	if len(h.Candles) == 0 {
		return
	}
	kts := make([]int64, len(h.Candles))
	for i, c := range h.Candles {
		kts[i] = c.Ts
	}
	colW := float64(plotW) / float64(T)
	bodyW := int(colW * 0.7)
	if bodyW < 1 {
		bodyW = 1
	}
	for xi := 0; xi < T; xi++ {
		c := nearestCandle(h.Candles, kts, h.Times[xi])
		if c == nil {
			continue
		}
		cx := mL + int((float64(xi)+0.5)*colW)
		col := colCandUp
		if c.C < c.O {
			col = colCandDn
		}
		vLine(img, cx, clampY(priceY(c.H)), clampY(priceY(c.L)), col) // 影线
		yo, yc := clampY(priceY(c.O)), clampY(priceY(c.C))            // 实体
		if yo > yc {
			yo, yc = yc, yo
		}
		if yc == yo {
			yc = yo + 1
		}
		fillRect(img, cx-bodyW/2, yo, cx+bodyW/2+1, yc, col)
	}
}

// nearestCandle 找与时间 t 最接近的一根 K线（kts 升序）。
func nearestCandle(cs []Candle, kts []int64, t int64) *Candle {
	if len(cs) == 0 {
		return nil
	}
	i := sort.Search(len(kts), func(i int) bool { return kts[i] >= t })
	if i == 0 {
		return &cs[0]
	}
	if i >= len(cs) {
		return &cs[len(cs)-1]
	}
	if t-kts[i-1] <= kts[i]-t {
		return &cs[i-1]
	}
	return &cs[i]
}

// RenderLiqMapPNG 渲染清算地图（最新时间列的横向柱状分布）为 PNG。
func RenderLiqMapPNG(m *LiqMap) ([]byte, error) {
	all := append(append([]LiqCluster{}, m.Above...), m.Below...)
	if len(all) == 0 {
		return nil, fmt.Errorf("清算地图无数据")
	}
	loadFonts()
	pMin, pMax, maxV := all[0].Price, all[0].Price, 0.0
	for _, c := range all {
		if c.Price < pMin {
			pMin = c.Price
		}
		if c.Price > pMax {
			pMax = c.Price
		}
		if c.Value > maxV {
			maxV = c.Value
		}
	}
	if pMax <= pMin {
		pMax = pMin + 1
	}
	if maxV <= 0 {
		maxV = 1
	}

	const mL, mR, mT, mB = 84, 84, 30, 26
	const plotW, plotH = 900, 900
	W, H := mL+plotW+mR, mT+plotH+mB
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	fillRect(img, 0, 0, W, H, colBg)

	priceY := func(p float64) int { return mT + int((pMax-p)/(pMax-pMin)*float64(plotH)) }
	barH := plotH / (len(all) + 1)
	if barH < 2 {
		barH = 2
	}
	if barH > 10 {
		barH = 10
	}

	drawBars := func(cs []LiqCluster, col color.RGBA) {
		for _, c := range cs {
			y := priceY(c.Price)
			w := int(c.Value / maxV * float64(plotW))
			fillRect(img, mL, y-barH/2, mL+w, y+barH/2+1, col)
		}
	}
	drawBars(m.Above, colShort)
	drawBars(m.Below, colLong)

	if m.CurrentPrice > pMin && m.CurrentPrice < pMax {
		cy := priceY(m.CurrentPrice)
		drawHLine(img, mL, mL+plotW, cy, colNow)
		drawText(img, mL+plotW-140, cy-4, "now $"+priceLabel(m.CurrentPrice), colNow, faceSm)
	}
	for i := 0; i <= 10; i++ {
		p := pMin + (pMax-pMin)*float64(i)/10
		drawText(img, 4, priceY(p)+5, "$"+priceLabel(p), colAxis, faceSm)
	}
	labelTop := func(cs []LiqCluster) {
		if len(cs) == 0 {
			return
		}
		c := cs[0]
		y := priceY(c.Price)
		w := int(c.Value / maxV * float64(plotW))
		drawText(img, mL+w+4, y+5, formatCompact(c.Value), colText, faceSm)
	}
	labelTop(m.Above)
	labelTop(m.Below)

	drawText(img, mL, 20, fmt.Sprintf("%s Liquidation Map   now $%s   %s", m.Symbol, priceLabel(m.CurrentPrice), m.Interval), colText, faceLg)
	drawText(img, mL, H-8, "red = short liqs above   /   green = long liqs below   (bar = notional)", colAxis, faceSm)

	return encodePNG(img)
}

// ---- 绘图辅助 ----

func encodePNG(img *image.RGBA) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, col color.RGBA) {
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.SetRGBA(x, y, col) // 越界自动忽略
		}
	}
}

func drawHLine(img *image.RGBA, x0, x1, y int, col color.RGBA) {
	for x := x0; x < x1; x++ {
		img.SetRGBA(x, y, col)
	}
}

func vLine(img *image.RGBA, x, y0, y1 int, col color.RGBA) {
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	for y := y0; y <= y1; y++ {
		img.SetRGBA(x, y, col)
	}
}

func drawText(img *image.RGBA, x, y int, s string, col color.Color, face font.Face) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

// heatColor 把 [0,1] 映射成类 inferno 的黑→紫→橙→黄渐变。
func heatColor(t float64) color.RGBA {
	if t <= 0 {
		return color.RGBA{0, 0, 4, 255}
	}
	if t > 1 {
		t = 1
	}
	stops := []struct {
		pos     float64
		r, g, b uint8
	}{
		{0.0, 0, 0, 4},
		{0.25, 60, 12, 90},
		{0.5, 150, 34, 78},
		{0.75, 232, 100, 22},
		{1.0, 252, 232, 112},
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].pos {
			a, b := stops[i-1], stops[i]
			f := (t - a.pos) / (b.pos - a.pos)
			return color.RGBA{lerp8(a.r, b.r, f), lerp8(a.g, b.g, f), lerp8(a.b, b.b, f), 255}
		}
	}
	s := stops[len(stops)-1]
	return color.RGBA{s.r, s.g, s.b, 255}
}

func lerp8(a, b uint8, f float64) uint8 {
	return uint8(float64(a) + (float64(b)-float64(a))*f)
}

// priceLabel 把价格格式化成带千分位、无小数的短标签（如 "65,150"）。
func priceLabel(v float64) string {
	s := FormatPrice(v)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	return s
}
