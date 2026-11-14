package ruijie

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed testdata/*.png
var builtinFS embed.FS

// 验证码规格（2026-11 实测）：80x30 PNG、纯两色（白底 + 橙 #e39d41 点阵字体）、
// 4 位数字、无噪点无干扰线。识别策略：列投影切 4 段 → bounding box 归一化
// 到固定网格 → 与模板逐位比距离；任一位无高置信匹配就报错，由调用方
// 重新取一张验证码（宁可重取，绝不猜错）。
const (
	gridW, gridH = 16, 20
	// acceptRatio 是允许的位差异比例（0~1）。阈值由 4 张实测样本调校：
	// 自身模板距离为 0；跨变体（同数字不同样式）距离必须仍在阈值内。
	acceptRatio = 0.25
)

// builtinAnswers 是内置样本与其正确答案（时序与登录请求对齐得出）。
var builtinAnswers = map[string]string{
	"validcode-1.png": "9456",
	"validcode-2.png": "2284",
	"validcode-3.png": "0336",
	"validcode-4.png": "9348",
}

type glyph [gridW * gridH]bool

// templateSet 是数字模板库：每位数字可有多个变体。
type templateSet struct {
	byDigit map[byte][]glyph
}

var builtin = mustLoadBuiltin()

func mustLoadBuiltin() *templateSet {
	ts := newTemplateSet()
	entries, err := builtinFS.ReadDir("testdata")
	if err != nil {
		return ts
	}
	for _, e := range entries {
		answer, ok := builtinAnswers[e.Name()]
		if !ok {
			continue
		}
		raw, err := builtinFS.ReadFile("testdata/" + e.Name())
		if err != nil {
			continue
		}
		_ = ts.addSample(raw, answer)
	}
	return ts
}

func newTemplateSet() *templateSet {
	return &templateSet{byDigit: make(map[byte][]glyph)}
}

// addSample 把一张「4 位答案已知」的验证码图切段入库。
func (ts *templateSet) addSample(raw []byte, answer string) error {
	mask, err := decodeMask(raw)
	if err != nil {
		return err
	}
	segs := splitSegments(mask)
	if len(segs) != 4 || len(answer) != 4 {
		return fmt.Errorf("样本应为 4 段/4 位，实际 %d 段/%d 位", len(segs), len(answer))
	}
	for i, seg := range segs {
		ts.addDigit(answer[i], normalizeGlyph(mask, seg))
	}
	return nil
}

func (ts *templateSet) addDigit(d byte, g glyph) {
	for _, exist := range ts.byDigit[d] {
		if exist == g {
			return // 去重
		}
	}
	ts.byDigit[d] = append(ts.byDigit[d], g)
}

// loadDir 加载磁盘自举模板：<answer>.png（成功登录后由 SaveSample 落盘）。
func (ts *templateSet) loadDir(dir string) {
	if dir == "" {
		return
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".png") || len(name) != len(".png")+4 {
			continue
		}
		answer := strings.TrimSuffix(name, ".png")
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		_ = ts.addSample(raw, answer)
	}
}

// SaveSample 成功登录后把「图 + 答案」存进模板库（文件名即答案），
// 模板库随使用渐进覆盖全部数字与字形变体。
func SaveSample(dir string, raw []byte, answer string) error {
	if dir == "" || len(answer) != 4 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, answer+".png"), raw, 0o600)
}

// Recognize 识别一张验证码 PNG，返回 4 位数字。
// 任何一位不确定都返回错误——调用方应重新获取验证码再试。
func Recognize(raw []byte, dir string) (string, error) {
	ts := newTemplateSet()
	// 内置样本 + 磁盘自举模板
	for d, glyphs := range builtin.byDigit {
		for _, g := range glyphs {
			ts.addDigit(d, g)
		}
	}
	ts.loadDir(dir)

	mask, err := decodeMask(raw)
	if err != nil {
		return "", err
	}
	segs := splitSegments(mask)
	if len(segs) != 4 {
		return "", fmt.Errorf("验证码应切出 4 段，实际 %d 段", len(segs))
	}
	out := make([]byte, 4)
	for i, seg := range segs {
		g := normalizeGlyph(mask, seg)
		d, ok := ts.bestMatch(g)
		if !ok {
			return "", fmt.Errorf("第 %d 位无高置信模板（需重取验证码）", i+1)
		}
		out[i] = d
	}
	return string(out), nil
}

// bestMatch 找距离最近且在阈值内的模板数字。
func (ts *templateSet) bestMatch(g glyph) (byte, bool) {
	total := float64(gridW * gridH)
	bestD := total + 1
	var best byte
	found := false
	digits := make([]int, 0, len(ts.byDigit))
	for d := range ts.byDigit {
		digits = append(digits, int(d))
	}
	sort.Ints(digits) // 遍历顺序稳定，便于调试
	for _, di := range digits {
		d := byte(di)
		for _, t := range ts.byDigit[d] {
			dist := glyphDist(g, t)
			if dist < bestD {
				bestD = dist
				best = d
				found = true
			}
		}
	}
	if !found || bestD/total > acceptRatio {
		return 0, false
	}
	return best, true
}

func glyphDist(a, b glyph) float64 {
	n := 0
	for i := range a {
		if a[i] != b[i] {
			n++
		}
	}
	return float64(n)
}

// mask 是二值图：true = 数字像素（非白）。
type mask struct {
	w, h int
	px   []bool
}

func (m *mask) at(x, y int) bool { return m.px[y*m.w+x] }

func decodeMask(raw []byte) (*mask, error) {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("解码 PNG 失败: %w", err)
	}
	return toMask(img), nil
}

// toMask 把任意背景的图转为「非白即数字」的二值 mask。
// 实测验证码是纯白底，容差留给抗锯齿/压缩噪声。
func toMask(img image.Image) *mask {
	b := img.Bounds()
	m := &mask{w: b.Dx(), h: b.Dy(), px: make([]bool, b.Dx()*b.Dy())}
	for y := 0; y < m.h; y++ {
		for x := 0; x < m.w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			m.px[y*m.w+x] = r>>8 < 245 || g>>8 < 245 || bl>>8 < 245
		}
	}
	return m
}

// splitSegments 按空白列切成 4 段，返回每段的 bounding box。
func splitSegments(m *mask) []struct{ x0, y0, x1, y1 int } {
	type seg struct{ x0, y0, x1, y1 int }
	var segs []seg
	inGlyph := false
	var cur seg
	for x := 0; x < m.w; x++ {
		has := false
		for y := 0; y < m.h; y++ {
			if m.at(x, y) {
				has = true
				break
			}
		}
		if has {
			if !inGlyph {
				cur = seg{x0: x, y0: m.h, x1: x, y1: 0}
				inGlyph = true
			}
			if cur.y0 == m.h {
				cur.y0 = 0
				for cur.y0 < m.h && !hasRow(m, x, cur.y0) {
					cur.y0++
				}
			}
			for y := 0; y < m.h; y++ {
				if m.at(x, y) {
					if y < cur.y0 {
						cur.y0 = y
					}
					if y > cur.y1 {
						cur.y1 = y
					}
				}
			}
			cur.x1 = x
		} else if inGlyph {
			segs = append(segs, cur)
			inGlyph = false
		}
	}
	if inGlyph {
		segs = append(segs, cur)
	}
	out := make([]seg, len(segs))
	copy(out, segs)
	// 匿名结构体切片转出（Go 类型结构一致才可直接转，这里重包装）
	result := make([]struct{ x0, y0, x1, y1 int }, len(out))
	for i, s := range out {
		result[i] = struct{ x0, y0, x1, y1 int }{s.x0, s.y0, s.x1, s.y1}
	}
	return result
}

func hasRow(m *mask, x, y int) bool { return m.at(x, y) }

// normalizeGlyph 把一段 bounding box 最近邻缩放归一到 gridW×gridH。
func normalizeGlyph(m *mask, seg struct{ x0, y0, x1, y1 int }) glyph {
	var g glyph
	sw := seg.x1 - seg.x0 + 1
	sh := seg.y1 - seg.y0 + 1
	if sw <= 0 || sh <= 0 {
		return g
	}
	for gy := 0; gy < gridH; gy++ {
		srcY := seg.y0 + gy*sh/gridH
		if srcY > seg.y1 {
			srcY = seg.y1
		}
		for gx := 0; gx < gridW; gx++ {
			srcX := seg.x0 + gx*sw/gridW
			if srcX > seg.x1 {
				srcX = seg.x1
			}
			g[gy*gridW+gx] = m.at(srcX, srcY)
		}
	}
	return g
}
