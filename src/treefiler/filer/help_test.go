package filer

import (
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

func helpScreen(t *testing.T, w, h int) []string {
	t.Helper()
	m := newTest(t)
	m.Resize(w, h)
	settle(m)
	m.HandleKey("?")
	v := m.View()
	for i := range v {
		v[i] = termwidth.StripSGR(v[i])
	}
	return v
}

// 広い端末では、節ごとに見出しを付けて 2 段に組み、どの説明も切らずに出す。
func TestHelpGroupsKeysIntoSectionsInTwoColumns(t *testing.T) {
	v := helpScreen(t, 160, 50)
	all := strings.Join(v, "\n")
	for _, s := range helpSections {
		if !strings.Contains(all, "── "+s.title+" ") {
			t.Errorf("節の見出し %q が無い", s.title)
		}
		for _, kv := range s.keys {
			if !strings.Contains(all, kv[1]) {
				t.Errorf("%s の %q の説明が切れずに出ていない: %q", s.title, kv[0], kv[1])
			}
		}
	}
	twoCols := false
	for _, row := range v { // 最初の節と後半の先頭の節が同じ行に並ぶ (2 段)
		if strings.Contains(row, "── 移動 ") && strings.Contains(row, "── ファイル ") {
			twoCols = true
		}
	}
	if !twoCols {
		t.Errorf("160 桁で 2 段に組まれていない:\n%s", all)
	}
}

// 2 段が入らない幅では 1 段 (幅 64) に戻し、高さが足りなければ残りの数を出す。
func TestHelpFallsBackToOneColumnWhenNarrow(t *testing.T) {
	v := helpScreen(t, 80, 70)
	all := strings.Join(v, "\n")
	for _, row := range v {
		if strings.Contains(row, "── 移動 ") && strings.Contains(row, "── ファイル ") {
			t.Fatalf("80 桁で 2 段に組んだ (1 段は幅 64 まで):\n%s", all)
		}
	}
	if strings.Contains(all, "残り") || !strings.Contains(all, "このキー一覧") {
		t.Fatalf("70 行あるのに 1 段で全部出ない:\n%s", all)
	}
}

// 2 段は節の並びのまま前後に割り、高い方の段が最も低くなる所で割る。
func TestHelpColumnsSplitKeepsOrderAndBalances(t *testing.T) {
	cols := helpColumns(helpSections, 2)
	if len(cols) != 2 {
		t.Fatalf("段の数 %d", len(cols))
	}
	one := helpColumns(helpSections, 1)[0]
	if got := len(cols[0]) + 1 + len(cols[1]); got != len(one) { // 割った所の空行 1 つだけが減る
		t.Fatalf("2 段の行の合計 %d / 1 段 %d", got, len(one))
	}
	best := max(len(cols[0]), len(cols[1]))
	for k := 1; k < len(helpSections); k++ {
		a, b := helpColumns(helpSections[:k], 1)[0], helpColumns(helpSections[k:], 1)[0]
		if max(len(a), len(b)) < best {
			t.Fatalf("%d 節目で割る方が低い (%d < %d)", k, max(len(a), len(b)), best)
		}
	}
}
