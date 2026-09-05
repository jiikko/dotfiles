package main

import (
	"strings"
	"testing"
)

// 最下行の hint は **popup の幅に収まる**。
//
// 🚨 収まらない hint は `clipToWidth` で末尾が切られ、**常に並びの最後が消える**。
// そこに置かれているのは「どう抜けるか」なので、狭い端末ほど出口が分からなくなる
// (issue 155 が status viewer で実測し、issue 201 の監査で doctor / job パネル /
// detailOv の 3 箇所が同じ状態だったことが分かった。doctor は 112 桁 / 予算 82 桁)。
//
// 直し方は「切る」ではなく **`fitHintItems` で入らない項目を落とす**。優先度 1 に
// 「抜ける手段」を置けば、幅が狭くてもそれだけは残る。
//
// ⚠️ この検査は**静的な hint 文字列だけ**を見る。`m.spinner()` の連結や `🚨 ghErr` の
// 前置のような動的合成は測れない (偽陰性が残る)。「これで hint の幅は保証された」とは
// 言えないが、固定文字列の作り忘れは止まる。
func TestHintsFitPopupWidth(t *testing.T) {
	// popup の実効幅。🚨 production の hintWidth() から導く (tui_helpers_test.go の testHintBudget)
	width := testHintBudget(t)

	t.Run("doctor", func(t *testing.T) {
		v := &doctorView{}
		assertFits(t, "doctorView.hint", v.hint(width), width)
		// 狭くしても「抜ける手段」は残る (優先度 1)
		if got := v.hint(20); !strings.Contains(got, "esc") {
			t.Errorf("幅 20 で抜ける手段が消えた: %q", got)
		}
	})

	t.Run("job パネル (カーソルあり)", func(t *testing.T) {
		m := newTestBrowse(t, 3, map[string]CIState{}, nil)
		m.width = testPopupTermWidth
		m.panelSHA = m.commits[0].SHA
		m.panelCursor = 0
		// ⚠️ 予算は **m.hintWidth()** で測る (固定値を書くと外れる)。frame の有無で
		// 左右余白 2 桁を引くかが変わり、frame 無効なら m.width そのもの。
		// frame 有効時は左余白 1 桁が前置されるので剥がしてから測る。
		budget := m.hintWidth()
		got := strings.TrimPrefix(stripANSI(m.hintLine()), " ")
		assertFits(t, "job パネル", got, budget)
		if !strings.Contains(got, "閉じる") {
			t.Errorf("job パネルの hint に抜ける手段が無い: %q", got)
		}
	})

	// ⚠️ detailOv は開くのに CI の応答が要るので、hintLine 経由では組み立てにくい。
	// 生成している fitHintItems の呼び出しと同じ項目をここで直接測る
	// (自己言及にならないよう**優先度と幅の主張だけ**を見る。文言が変わればこの表も直す)。
	t.Run("detailOv の項目", func(t *testing.T) {
		got := fitHintItems(width, []hintItem{
			{"j/k: スクロール", 3},
			{"J/K: 隣の job", 4},
			{"v: nvim で開く", 4},
			{"r: 再実行", 3},
			{"Enter/h/q: 戻る", 1},
			{"o: ブラウザ", 4},
			{"y: URL", 5},
			{"Y: 詳細コピー", 5},
		})
		assertFits(t, "detailOv", got, width)
		if !strings.Contains(got, "戻る") {
			t.Errorf("detailOv の hint に抜ける手段が無い: %q", got)
		}
	})
}

func assertFits(t *testing.T, name, hint string, width int) {
	t.Helper()
	if w := dispWidth(hint); w > width {
		t.Errorf("%s が %d 桁で幅 %d に収まらない (末尾が切られ、並びの最後の項目が消える):\n  %s",
			name, w, width, hint)
	}
}

// commit diff の hint は fitHintItems で組み、狭い幅でも抜ける手段が残る (issue 264)。
// 幅を掃くのは TestStatusHintUsesRenderBudget と同じ理由 (1 点では予算のずれが余白に吸われる)。
func TestDiffHintUsesRenderBudget(t *testing.T) {
	for w := frameMinWidth; w <= 140; w++ {
		m := newTestBrowse(t, 1, map[string]CIState{}, nil)
		m.showFrame, m.width, m.height = true, w, 40
		m.diffOv.open(m.commits[0].SHA)
		line := m.hintLine()
		got := stripANSI(line)
		if dispWidth(got) > w {
			t.Errorf("w=%d: hint 行が端末幅を超えた (%d 桁): %q", w, dispWidth(got), line)
		}
		if strings.Contains(got, "…") || !strings.Contains(got, "q/h: 閉じる") {
			t.Errorf("w=%d: 切り詰め or 抜ける手段が消えた: %q", w, line)
		}
	}
}
