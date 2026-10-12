package filer

import (
	"reflect"
	"slices"
	"testing"
)

// 全部のキーを取る層 (Model の `layer:"…"` の欄) の網羅テスト (issue 715 の案 A)。
// 層を足したら、ここの表に「開き方」と「ctrl+c で終わるか」を足すまで落ちる。各層について、開いている間
// OwnsKeys (glogx が横断キーを取らずに渡すか) と Binds が真で、木のキーが木へ届かないことを見る (OwnsKeys と HandleInput の対)。
// 🚨 拾えるのは `layer` の札を付けた欄だけ。札を付けずに層を足す形は検出しない (足すときは Model の panel の欄の注記を読む)。
func TestEveryInputLayerOwnsKeys(t *testing.T) {
	layers := map[string]struct {
		open     func(m *Model)
		quitOnCC bool // ctrl+c でアプリを終えるか (入力欄と確認の板は取り消し)
	}{
		"help":   {func(m *Model) { m.HandleKey("?") }, true},
		"panel":  {func(m *Model) { m.HandleKey(",") }, true},
		"search": {func(m *Model) { m.HandleKey("/") }, false},
		"prompt": {func(m *Model) { m.HandleKey("!") }, false},
		"ask":    {func(m *Model) { m.HandleKey("d") }, false},
	}
	var tagged []string
	rt := reflect.TypeFor[Model]()
	for i := range rt.NumField() {
		if v, ok := rt.Field(i).Tag.Lookup("layer"); ok {
			tagged = append(tagged, v)
		}
	}
	listed := make([]string, 0, len(layers))
	for k := range layers {
		listed = append(listed, k)
	}
	slices.Sort(tagged)
	slices.Sort(listed)
	if !slices.Equal(tagged, listed) {
		t.Fatalf("層の札 %v と、この表 %v が食い違う (足した層をここに足すこと)", tagged, listed)
	}
	for name, l := range layers {
		m, _ := newOps(t)
		cdTo(t, m, "c.txt")
		if m.OwnsKeys() {
			t.Fatalf("%s: 開く前から OwnsKeys", name)
		}
		l.open(m)
		if !m.OwnsKeys() || !m.Binds("R") || !m.Binds("D") {
			t.Errorf("%s: 開いているのに OwnsKeys / Binds が偽 (glogx が横断キーを先に取る)", name)
		}
		cur := m.cur
		if m.HandleKey("J"); m.cur != cur && name != "search" { // 検索は打った字で飛ぶ
			t.Errorf("%s: 開いている間に木のキーが木へ届いた", name)
		}
		if !m.OwnsKeys() { // キー一覧と確認の板は他のキーで閉じる (J で閉じた)。ctrl+c を見るために開き直す
			l.open(m)
		}
		if got := m.HandleKey("ctrl+c") == Quit; got != l.quitOnCC {
			t.Errorf("%s: ctrl+c で終わる = %v, want %v", name, got, l.quitOnCC)
		}
	}
}
