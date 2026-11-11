package subs

import (
	"fmt"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"strings"
	"testing"

	"slack-bbs/source"
)

// 一覧は 購読中 → 参加している → 参加していない の順 (それぞれ名前順)。購読しているのに一覧に無いチャンネルは先頭に出す。
// 切り替えても並べ直さない (カーソルの下の行が入れ替わらない)。
func TestPickerOrder(t *testing.T) {
	all := []source.Channel{
		{ID: "C1", Name: "alpha", IsMember: true}, {ID: "C2", Name: "beta"}, {ID: "C3", Name: "zeta", IsMember: true},
		{ID: "C4", Name: "gamma", IsMember: true},
	}
	p := NewPicker(all, []Channel{{ID: "C3", Name: "zeta"}, {ID: "C9", Name: "gone"}})
	var got []string
	for _, it := range p.items {
		got = append(got, it.ch.Name)
	}
	if strings.Join(got, ",") != "gone,zeta,alpha,gamma,beta" {
		t.Fatalf("並び: %v", got)
	}
	p.Key("j")
	p.Key("x") // zeta を外す
	if p.items[1].ch.Name != "zeta" || p.items[1].on {
		t.Errorf("切り替えで並べ直した、または外れていない: %+v", p.items[1])
	}
}

// 一覧はページで区切って出す: 窓は 1 行ずつ流さずページの区切りで動き、n / p でページを送る。見出しに今のページを出す。
func TestPickerPages(t *testing.T) {
	var all []source.Channel
	for i := range 25 {
		all = append(all, source.Channel{ID: fmt.Sprintf("C%02d", i), Name: fmt.Sprintf("ch%02d", i), IsMember: true})
	}
	p := NewPicker(all, nil)
	p.Resize(80, 14) // 1 ページ 10 行 → 3 ページ
	shown := func() string {
		var names []string
		for _, l := range p.Lines()[2:12] {
			if i := strings.Index(l, "#"); i >= 0 {
				names = append(names, l[i+1:])
			}
		}
		return strings.Join(names, ",")
	}
	if h := p.Lines()[0]; !strings.Contains(h, "ページ 1/3") {
		t.Fatalf("見出しにページが無い: %q", h)
	}
	for range 9 {
		p.Key("j")
	}
	if s := shown(); !strings.HasPrefix(s, "ch00,") {
		t.Fatalf("同じページの中で窓が動いた: %s", s)
	}
	p.Key("j") // 11 件目 → 2 ページ目
	if s := shown(); !strings.HasPrefix(s, "ch10,") || !strings.Contains(p.Lines()[0], "ページ 2/3") {
		t.Fatalf("ページの境で次のページへ移らない: %s / %q", s, p.Lines()[0])
	}
	p.Key("n")
	if s := shown(); s != "ch20,ch21,ch22,ch23,ch24" || p.list.Cursor != 20 {
		t.Fatalf("n で次のページの先頭へ移らない: %s cursor=%d", s, p.list.Cursor)
	}
	p.Key("n") // 最後のページでは末尾へ
	if p.list.Cursor != 24 {
		t.Errorf("最後のページの n: cursor=%d, want 24", p.list.Cursor)
	}
	p.Key("p")
	p.Key("p")
	if p.list.Cursor != 0 || !strings.Contains(p.Lines()[0], "ページ 1/3") {
		t.Errorf("p で前のページの先頭へ戻らない: cursor=%d", p.list.Cursor)
	}
}

// 検索は空白で区切った語をすべて名前に含むチャンネル (大文字と小文字・先頭の # は問わない)。件数を出し、ページは 1 から数え直す。
func TestPickerSearch(t *testing.T) {
	all := []source.Channel{{ID: "C1", Name: "team-dev-core", IsMember: true}, {ID: "C2", Name: "team-ops", IsMember: true}, {ID: "C3", Name: "Dev-random", IsMember: true}}
	p := NewPicker(all, nil)
	p.Resize(80, 14)
	for _, k := range []string{"/", "#", "D", "E", "V", "space", "t", "e", "a", "m"} {
		p.Key(k)
	}
	lines := strings.Join(p.Lines(), "\n")
	if !strings.Contains(lines, "#team-dev-core") || strings.Contains(lines, "#team-ops") || strings.Contains(lines, "#Dev-random") || !strings.Contains(lines, "(1 件が一致)") {
		t.Errorf("「#DEV team」の検索:\n%s", lines)
	}
	p.Key("esc")
	if !strings.Contains(strings.Join(p.Lines(), "\n"), "#team-ops") {
		t.Error("Esc で検索が消えない")
	}
}

// 案内とページ番号は幅 80 に収まる (q で保存せずに閉じる操作を切らない)。検索の Enter / Esc で語が変わらなければページに留まる。
func TestPickerFitsWidthAndKeepsPage(t *testing.T) {
	var all []source.Channel
	for i := range 25 {
		all = append(all, source.Channel{ID: fmt.Sprintf("C%02d", i), Name: fmt.Sprintf("ch%02d", i), IsMember: true})
	}
	p := NewPicker(all, nil)
	p.Resize(80, 14)
	lines := p.Lines()
	for i, l := range lines {
		if strings.Contains(l, "…") {
			t.Errorf("%d 行目が幅 80 で切れた: %q", i, l)
		}
	}
	if help := strings.Join(lines[len(lines)-2:], "\n"); !strings.Contains(help, "q: 保存せずに閉じる") || !strings.Contains(help, "Enter: 保存して閉じる") {
		t.Errorf("案内に閉じ方が無い: %q", help)
	}
	p.Key("n")
	p.Key("/")
	p.Key("esc")
	if p.list.Cursor != 10 {
		t.Errorf("検索で語を変えずに Esc でページを失った: cursor=%d", p.list.Cursor)
	}
	p.Resize(30, 14)
	if !strings.HasPrefix(p.Lines()[0], "ページ 2/3") {
		t.Errorf("狭い端末でページ番号が先に切れた: %q", p.Lines()[0])
	}
}

// 幅 40 の端末でも、購読の画面の閉じ方 (保存して閉じる・保存せずに閉じる) は両方とも案内に出る。
func TestPickerClosingHintsAt40(t *testing.T) {
	p := NewPicker([]source.Channel{{ID: "C1", Name: "a", IsMember: true}}, nil)
	p.Resize(40, 10)
	lines := p.Lines()
	help := strings.Join(lines[len(lines)-2:], "\n")
	if !strings.Contains(help, "Enter: 保存して閉じる") || !strings.Contains(help, "q: 保存せずに閉じる") {
		t.Errorf("幅 40 の閉じ方の案内: %q", help)
	}
}

// 検索の欄は入力欄の編集キー (TUI ガイド §7) で直せ、打っている間だけ端末のカーソルをキャレットに置く (日本語の変換中の文字が欄に出る)。
func TestPickerSearchIsLineEdit(t *testing.T) {
	p := NewPicker([]source.Channel{{ID: "C1", Name: "dev-ops", IsMember: true}, {ID: "C2", Name: "design", IsMember: true}}, nil)
	p.Resize(80, 12)
	if p.Cursor() != nil {
		t.Error("検索していないのにカーソルを出す")
	}
	for _, k := range []string{"/", "o", "p", "s", "space", "x", "ctrl+w"} { // ctrl+w で後ろの語 (x) を消す
		p.Key(k)
	}
	if got := p.filter.String(); got != "ops " {
		t.Fatalf("ctrl+w の後の検索の語 = %q, want %q", got, "ops ")
	}
	p.Key("left")
	p.Key("left")
	c := p.Cursor()
	if c == nil || c.Y != 1 || c.X != termwidth.Of(searchLabel)+2 {
		t.Fatalf("キャレットの位置 = %+v, want (%d, 1)", c, termwidth.Of(searchLabel)+2)
	}
	if !strings.Contains(p.Lines()[1], searchLabel+"ops ") {
		t.Errorf("検索の行に語が出ない: %q", p.Lines()[1])
	}
	p.Key("ctrl+u") // キャレットの前 (op) を消す
	if got := p.filter.String(); got != "s " {
		t.Errorf("ctrl+u の後の検索の語 = %q, want %q", got, "s ")
	}
	p.Key("enter")
	if p.Cursor() != nil || len(p.visible()) != 2 {
		t.Errorf("Enter の後: カーソル %v / 一致 %d 件 (dev-ops と design)", p.Cursor(), len(p.visible()))
	}
}

// 検索していない購読の画面では、Ctrl-C は保存せずに閉じ、Ctrl-F は → (次のページ) の別名。
func TestPickerCtrlKeys(t *testing.T) {
	var all []source.Channel
	for i := range 25 {
		all = append(all, source.Channel{ID: fmt.Sprintf("C%02d", i), Name: fmt.Sprintf("ch%02d", i), IsMember: true})
	}
	p := NewPicker(all, nil)
	p.Resize(80, 14)
	p.Key("ctrl+f")
	if p.list.Cursor != p.rows() {
		t.Errorf("Ctrl-F で次のページへ進まない: cursor=%d", p.list.Cursor)
	}
	p.Key("ctrl+c")
	if !p.Done() || p.Saved() {
		t.Errorf("Ctrl-C: done=%v saved=%v, want 保存せずに閉じる", p.Done(), p.Saved())
	}
}

// 検索の語を打っている間も、↑↓ (Ctrl-N/Ctrl-P) で絞り込んだ一覧を動き、Tab で付け外しできる (語は欄に残る)。
func TestPickerSelectWhileSearching(t *testing.T) {
	all := []source.Channel{{ID: "C1", Name: "times_alice", IsMember: true}, {ID: "C2", Name: "general", IsMember: true}, {ID: "C3", Name: "times_bob", IsMember: true}}
	p := NewPicker(all, nil)
	p.Resize(80, 12)
	for _, k := range []string{"/", "t", "i", "m", "e", "s", "down", "tab"} {
		p.Key(k)
	}
	if !p.filtering || p.filter.String() != "times" {
		t.Fatalf("移動と付け外しで検索の欄が閉じた・語が変わった: filtering=%v %q", p.filtering, p.filter.String())
	}
	if sel := p.Selected(); len(sel) != 1 || sel[0].Name != "times_bob" {
		t.Fatalf("絞り込んだ 2 件目 (times_bob) を Tab で付けたい: %+v", sel)
	}
	p.Key("ctrl+p")
	p.Key("tab")
	p.Key("ctrl+n")
	p.Key("tab") // times_bob を外す
	if sel := p.Selected(); len(sel) != 1 || sel[0].Name != "times_alice" {
		t.Errorf("Ctrl-P / Ctrl-N で動いて付け外したい: %+v", sel)
	}
	if help := strings.Join(p.Lines(), "\n"); !strings.Contains(help, "Tab: 購読を付け外し") {
		t.Errorf("入力中の案内に Tab が無い:\n%s", help)
	}
}
