package subs

import (
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
