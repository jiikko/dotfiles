package filer

import (
	"reflect"
	"slices"
	"testing"
)

// 全部のキーを取る層 (Model の `layer:"…"` の欄) の網羅テスト。
// 層を足したら、ここの表に「開き方」と「ctrl+c で終わるか」を足すまで落ちる。各層について、開いている間
// OwnsKeys (glogx が横断キーを取らずに渡すか) と Binds が真で、木のキーが木へ届かないことを見る (OwnsKeys と HandleInput の対)。
// 🚨 拾えるのは `layer` の札を付けた欄だけ。札を付けずに層を足す形は検出しない (足すときは Model の panel の欄の注記を読む)。
func TestEveryInputLayerOwnsKeys(t *testing.T) {
	layers := map[string]struct {
		open     func(m *Model)
		quitOnCC bool   // ctrl+c でアプリを終えるか (入力欄と確認の板は取り消し)
		exit     string // LayerHints の抜ける手段 (優先度 1) のキー (glogx がその層の間の最下行に出す)
	}{
		"help":   {func(m *Model) { m.HandleKey("?") }, true, "どのキーでも"},
		"panel":  {func(m *Model) { m.HandleKey(",") }, true, "esc/,/q"},
		"search": {func(m *Model) { m.HandleKey("/") }, false, "esc"},
		"prompt": {func(m *Model) { m.HandleKey("!") }, false, "esc"},
		"ask":    {func(m *Model) { m.HandleKey("d") }, false, "その他"},
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
		if m.OwnsKeys() || m.LayerHints() != nil {
			t.Fatalf("%s: 開く前から OwnsKeys / LayerHints がある", name)
		}
		l.open(m)
		if !m.OwnsKeys() || !m.Binds("R") || !m.Binds("D") {
			t.Errorf("%s: 開いているのに OwnsKeys / Binds が偽 (glogx が横断キーを先に取る)", name)
		}
		if i := slices.IndexFunc(m.LayerHints(), func(h Hint) bool { return h.Prio == 1 }); i < 0 || m.LayerHints()[i].Key != l.exit {
			t.Errorf("%s: LayerHints の抜ける手段が %q でない: %+v", name, l.exit, m.LayerHints())
		}
		cur := m.cur
		for _, k := range []string{"j", "J"} { // 小文字の移動と大文字の 10 件 (settings_test の TestPanelOwnsKeys をここへ畳んだ。issue 735)
			if m.HandleKey(k); m.cur != cur && name != "search" { // 検索は打った字で飛ぶ
				t.Errorf("%s: 開いている間に木のキー %s が木へ届いた", name, k)
			}
			if !m.OwnsKeys() { // キー一覧と確認の板は他のキーで閉じる。次のキーと ctrl+c を見るために開き直す
				l.open(m)
			}
		}
		if got := m.HandleKey("ctrl+c") == Quit; got != l.quitOnCC {
			t.Errorf("%s: ctrl+c で終わる = %v, want %v", name, got, l.quitOnCC)
		}
	}
}
