package subs

import (
	"fmt"
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
	if !strings.Contains(lines, "#team-dev-core") || strings.Contains(lines, "#team-ops") || strings.Contains(lines, "#Dev-random") || !strings.Contains(lines, "(1 件)") {
		t.Errorf("「#DEV team」の検索:\n%s", lines)
	}
	p.Key("esc")
	if !strings.Contains(strings.Join(p.Lines(), "\n"), "#team-ops") {
		t.Error("Esc で検索が消えない")
	}
}

// 案内とページ番号は幅 80 に収まる (q で保存せずに終わる操作を切らない)。検索の Enter / Esc で語が変わらなければページに留まる。
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
	if !strings.Contains(lines[len(lines)-1], "q 保存せずに終わる") {
		t.Errorf("案内に q が無い: %q", lines[len(lines)-1])
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
