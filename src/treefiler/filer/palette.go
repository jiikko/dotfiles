// Package filer は treefiler の画面の部品 (木・タイル・キー・描画)。glogx の全画面の 1 枚としても、
// 単体の bin/treefiler としても使う。glogx に依存しない。
//
// 見た目と挙動の正本は docs/treefiler-spec.md (§0 = glogx での決定、§1〜 = treebeard のソースからの抜き出し)。
// 配色・線・配置の規則は treebeard (https://github.com/freakinfrick/treebeard、commit 691788d、
// MIT OR Apache-2.0) を写している。
//
// タイマーは自分で張らない: 時間は呼び出し側が Advance(now) で進める (glogx の「動くものがある間だけ
// tick を回す」に乗るため)。
package filer

import "math"

type rgb [3]float64

func mix(a, b rgb, t float64) rgb {
	return rgb{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t}
}

// 配色は spec §1.1 (ground = dark) / §1.2 (accent = indigo)。
var (
	cBg       = rgb{9, 10, 15}
	cBar      = rgb{17, 19, 29}
	cPop      = rgb{13, 15, 23}
	cText     = rgb{238, 240, 250}
	cRouteTxt = rgb{238, 240, 250}
	cMuted    = rgb{110, 118, 150}
	cAccDim   = rgb{33, 39, 68}
	cAccAct   = rgb{68, 86, 168}
	cAccRoute = rgb{140, 168, 255}
	// cCursorBg は glogx の ansiCursorBg (256 色の 24 番) と同じ色。treebeard の pill (accent.pill) は
	// 地の色に近すぎてカーソルが分からなかった (spec §0.1。ユーザー選定 2026-10-08)
	cCursorBg = rgb{0, 95, 135}
)

// git の印の色 (spec §1.1 の dark)。
var gitColor = map[byte]rgb{'?': {110, 200, 225}, '+': {120, 215, 130}, 'M': {240, 200, 90}, '!': {255, 90, 120}}

type stop struct {
	age float64 // 秒
	c   rgb
}

// ember は熱の色 (spec §1.3)。age は log 時間で補間する。
var ember = []stop{
	{60, rgb{255, 70, 60}}, {3600, rgb{255, 118, 48}}, {86400, rgb{255, 172, 64}},
	{7 * 86400, rgb{240, 190, 70}}, {30 * 86400, rgb{196, 112, 170}},
	{365 * 86400, rgb{108, 118, 196}}, {5 * 365 * 86400, rgb{66, 80, 214}},
}

// heat は更新からの経過秒 age の色。区間の中は ln(age) の上で smoothstep (spec §1.3)。
func heat(age float64) rgb {
	a := math.Log(math.Max(age, 1))
	if a <= math.Log(ember[0].age) {
		return ember[0].c
	}
	for i := 0; i+1 < len(ember); i++ {
		l0, l1 := math.Log(ember[i].age), math.Log(ember[i+1].age)
		if a <= l1 {
			t := (a - l0) / (l1 - l0)
			t = t * t * (3 - 2*t)
			return mix(ember[i].c, ember[i+1].c, t)
		}
	}
	return ember[len(ember)-1].c
}
