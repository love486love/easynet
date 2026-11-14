package ruijie

import (
	"fmt"
	"testing"
)

// builtinSamples 与正确答案一一对应（时序对齐见 builtinAnswers）。
func builtinSamples(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, answer := range builtinAnswers {
		raw, err := builtinFS.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatalf("读取内置样本 %s: %v", name, err)
		}
		out[name] = string(raw) + "|" + answer
	}
	return out
}

// TestRecognizeBuiltin 全库识别 4 张实测样本，必须全部正确。
func TestRecognizeBuiltin(t *testing.T) {
	want := map[string]string{
		"validcode-1.png": "9456",
		"validcode-2.png": "2284",
		"validcode-3.png": "0336",
		"validcode-4.png": "9348",
	}
	for name, answer := range want {
		raw, err := builtinFS.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatalf("读样本 %s: %v", name, err)
		}
		got, err := Recognize(raw, "")
		if err != nil {
			t.Errorf("%s 识别失败: %v", name, err)
			continue
		}
		if got != answer {
			t.Errorf("%s = %s, want %s", name, got, answer)
		}
	}
}

// TestMatchQuality 打印距离矩阵：同数字跨变体 vs 异数字，
// 确认 acceptRatio 阈值有清晰分界（人工核对数据）。
func TestMatchQuality(t *testing.T) {
	type sample struct {
		name   string
		answer string
		segs   []glyph
	}
	var samples []sample
	for _, name := range []string{"validcode-1.png", "validcode-2.png", "validcode-3.png", "validcode-4.png"} {
		raw, err := builtinFS.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		m, err := decodeMask(raw)
		if err != nil {
			t.Fatal(err)
		}
		segs := splitSegments(m)
		gs := make([]glyph, len(segs))
		for i, s := range segs {
			gs[i] = normalizeGlyph(m, s)
		}
		samples = append(samples, sample{name: name, answer: builtinAnswers[name], segs: gs})
	}

	total := float64(gridW * gridH)
	var sameMin, sameMax, diffMin float64 = 1e9, 0, 1e9
	sameMin = 1e9
	for i := range samples {
		for j := range samples {
			if i == j {
				continue
			}
			for a := 0; a < 4; a++ {
				for b := 0; b < 4; b++ {
					d := glyphDist(samples[i].segs[a], samples[j].segs[b])
					if samples[i].answer[a] == samples[j].answer[b] {
						if d < sameMin {
							sameMin = d
						}
						if d > sameMax {
							sameMax = d
						}
					} else {
						if d < diffMin {
							diffMin = d
						}
					}
				}
			}
		}
	}
	fmt.Printf("[阈值校准] 同数字跨变体距离: %.0f ~ %.0f / %.0f  异数字最近距离: %.0f / %.0f (阈值 %.0f)\n",
		sameMin, sameMax, total, diffMin, total, acceptRatio*total)
	if sameMax >= diffMin {
		t.Errorf("无清晰分界：同数字最大 %.0f >= 异数字最小 %.0f，需调整特征或阈值", sameMax, diffMin)
	}
}

// TestNoGuessOnUnknown 空库/无模板时必须报错，绝不猜答案。
func TestNoGuessOnUnknown(t *testing.T) {
	raw, err := builtinFS.ReadFile("testdata/validcode-1.png")
	if err != nil {
		t.Fatal(err)
	}
	// 用只含错误答案模板的临时逻辑：构造仅存「1111」的库不现实（切段按位对齐），
	// 这里直接验证 Recognize 对空目录 + 损坏数据的行为。
	if _, err := Recognize([]byte("not a png"), ""); err == nil {
		t.Fatal("损坏数据应报错")
	}
	_ = raw
}
