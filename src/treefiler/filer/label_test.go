package filer

import (
	"strings"
	"testing"
)

// 名前の幅の覚え (cacheLabel) は、列の最大幅 (labelMax。設定の MaxName) が変わったら切り詰め直す。戻したときも同じ。
func TestLabelRetruncatesWhenMaxNameChanges(t *testing.T) {
	orig := labelMax
	t.Cleanup(func() { labelMax = orig })
	n := &node{name: strings.Repeat("あ", 20)} // 40 桁
	for _, c := range []struct {
		max, want int
		cut       bool
	}{{12, 12, true}, {60, 40, false}, {12, 12, true}, {28, 28, true}} {
		labelMax = c.max
		// 全角 (2 桁) を切ると「…」(1 桁) と合わせて上限より 1 桁少なくなりうる (Truncate の振る舞い)
		if got := n.labelCells(); got > c.want || got < c.want-1 || got != widthOf(n.label()) {
			t.Errorf("labelMax=%d: 幅 = %d (label の実際の幅 %d), want %d (か 1 少ない)", c.max, got, widthOf(n.label()), c.want)
		}
		if cut := strings.HasSuffix(n.label(), "…"); cut != c.cut {
			t.Errorf("labelMax=%d: 切り詰め = %v, want %v (%q)", c.max, cut, c.cut, n.label())
		}
	}
}
