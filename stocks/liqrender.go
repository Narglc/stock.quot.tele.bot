package stocks

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// 纯 Go（image + image/png + basicfont）渲染清算图，无需 ffmpeg/matplotlib，输出 PNG 字节。

var (
	colBg    = color.RGBA{20, 20, 26, 255}
	colText  = color.RGBA{225, 225, 225, 255}
	colAxis  = color.RGBA{150, 150, 160, 255}
	colNow   = color.RGBA{0, 229, 255, 255}  // 现价线：青
	colShort = color.RGBA{231, 76, 60, 255}  // 上方空头爆仓：红
	colLong  = color.RGBA{46, 204, 113, 255} // 下方多头爆仓：绿
)

// RenderLiqHeatmapPNG 渲染完整清算热力图（时间×价格网格 + 现价线）为 PNG。
func RenderLiqHeatmapPNG(h *LiqHeatmap) ([]byte, error) {
	P, T := len(h.Prices), len(h.Times)
	if P == 0 || T == 0 {
		return nil, fmt.Errorf("空热力图")
	}
	const mL, mR, mT, mB = 66, 16, 22, 18
	const plotW, plotH = 880, 440
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
	// 价格→像素 y（高价在上）
	priceY := func(p float64) int { return mT + int((pMax-p)/(pMax-pMin)*float64(plotH)) }

	// 逐格填色
	for yi := 0; yi < P; yi++ {
		yTop := mT + int(float64(P-1-yi)/float64(P)*float64(plotH))
		yBot := mT + int(float64(P-yi)/float64(P)*float64(plotH))
		row := h.Values[yi]
		for xi := 0; xi < T; xi++ {
			v := row[xi]
			if v <= 0 {
				continue
			}
			x0 := mL + int(float64(xi)/float64(T)*float64(plotW))
			x1 := mL + int(float64(xi+1)/float64(T)*float64(plotW))
			fillRect(img, x0, yTop, x1, yBot, heatColor(v/maxV))
		}
	}

	// 现价线 + 标注
	if h.CurrentPrice > pMin && h.CurrentPrice < pMax {
		cy := priceY(h.CurrentPrice)
		drawHLine(img, mL, mL+plotW, cy, colNow)
		drawText(img, mL+plotW-110, cy-3, "now $"+priceLabel(h.CurrentPrice), colNow)
	}
	// y 轴价格刻度
	for i := 0; i <= 6; i++ {
		p := pMin + (pMax-pMin)*float64(i)/6
		drawText(img, 2, priceY(p)+4, "$"+priceLabel(p), colAxis)
	}
	// 标题 + 时间轴说明
	drawText(img, mL, 14, fmt.Sprintf("%s Liquidation Heatmap  %dx%d  %s", h.Symbol, P, T, h.Interval), colText)
	drawText(img, mL, H-5, "<- older        time        now ->", colAxis)

	return encodePNG(img)
}

// RenderLiqMapPNG 渲染清算地图（最新时间列的横向柱状分布）为 PNG。
func RenderLiqMapPNG(m *LiqMap) ([]byte, error) {
	all := append(append([]LiqCluster{}, m.Above...), m.Below...)
	if len(all) == 0 {
		return nil, fmt.Errorf("清算地图无数据")
	}
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

	const mL, mR, mT, mB = 70, 70, 22, 18
	const plotW, plotH = 740, 760
	W, H := mL+plotW+mR, mT+plotH+mB
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	fillRect(img, 0, 0, W, H, colBg)

	priceY := func(p float64) int { return mT + int((pMax-p)/(pMax-pMin)*float64(plotH)) }
	barH := plotH / (len(all) + 1)
	if barH < 2 {
		barH = 2
	}
	if barH > 8 {
		barH = 8
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

	// 现价线
	if m.CurrentPrice > pMin && m.CurrentPrice < pMax {
		cy := priceY(m.CurrentPrice)
		drawHLine(img, mL, mL+plotW, cy, colNow)
		drawText(img, mL+plotW-120, cy-3, "now $"+priceLabel(m.CurrentPrice), colNow)
	}
	// y 轴价格刻度
	for i := 0; i <= 8; i++ {
		p := pMin + (pMax-pMin)*float64(i)/8
		drawText(img, 2, priceY(p)+4, "$"+priceLabel(p), colAxis)
	}
	// 标注上/下方最大簇的额度
	labelTop := func(cs []LiqCluster) {
		if len(cs) == 0 {
			return
		}
		c := cs[0] // 已按额降序
		y := priceY(c.Price)
		w := int(c.Value / maxV * float64(plotW))
		drawText(img, mL+w+3, y+4, formatCompact(c.Value), colText)
	}
	labelTop(m.Above)
	labelTop(m.Below)

	drawText(img, mL, 14, fmt.Sprintf("%s Liquidation Map  now $%s  %s", m.Symbol, priceLabel(m.CurrentPrice), m.Interval), colText)
	drawText(img, mL, H-5, "red=short liqs above  /  green=long liqs below   (bar = notional)", colAxis)

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

func drawText(img *image.RGBA, x, y int, s string, col color.Color) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: basicfont.Face7x13,
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
