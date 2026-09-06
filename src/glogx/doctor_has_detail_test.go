package main

import (
	"testing"

	"doctor/disk"
)

// hasDetail は「Enter で展開できる行か」を **展開せずに** 答える (270 で detail を遅延化した
// ため、行を作る時点では中身が無い)。無条件に true を立てると、展開しても 0 行の行が
// 「展開できる」ことになり、Enter が無反応なのに expanded だけ立つ。その状態では
// **次の q が畳む側に取られて doctor が 1 回目で閉じない**。
//
// 🚨 述語 (diskHasDetail) と builder (diskDetail) を別々に育てると、この保証が静かに崩れる。
// 下の 2 本目がその一致を突き合わせる。
func TestBlockedDiskRowIsNotExpandable(t *testing.T) {
	mk := func() *doctorView {
		v := &doctorView{shown: true, expanded: map[string]bool{}}
		v.diskRep = &disk.Report{Results: []disk.Result{{
			Entry:  disk.Entry{ID: "blocked-x", Label: "blocked", Risk: disk.RiskConfirm, DeleteVia: "rm"},
			Status: disk.StatusBlocked, Size: 4096, Reason: "今は対象外"}}}
		v.tab = tabDisk
		_ = v.lines(doctorTestOpts(40))
		return v
	}

	v := mk()
	var found bool
	for _, r := range v.rows {
		if r.key == "disk:blocked-x" {
			found = true
			if r.hasDetail {
				t.Errorf("展開しても 0 行の行に hasDetail が立っている (Enter が無反応なのに expanded が立つ)")
			}
		}
	}
	if !found {
		t.Fatal("blocked の行が出ていない (fixture が壊れている)")
	}

	// 本題: Enter を押しても、その次の q は doctor を閉じること
	before := len(v.rows)
	v.handleKey("enter", 40)
	if after := len(v.rows); after != before {
		t.Fatalf("fixture が想定と違う: Enter で行数が %d -> %d に増えた", before, after)
	}
	if act := v.handleKey("q", 40); act != doctorClosed {
		t.Errorf("Enter の後の q が飲まれた (act=%v)。畳むものが無いので閉じるべき", act)
	}

	// 対照: Enter を押していなければ 1 回目の q で閉じる (この対照が無いと、q が常に
	// 閉じるだけの実装でも上の assert が通る)
	if act := mk().handleKey("q", 40); act != doctorClosed {
		t.Fatalf("対照が壊れている: Enter 無しの q でも閉じない (act=%v)", act)
	}
}

// diskHasDetail (述語) と diskDetail (builder) が一致すること。
// 🚨 builder に行を足したのに述語を直し忘れる / その逆を検出する。片方だけ育てると
// 「展開できると言うのに 0 行」か「中身があるのに展開できない」のどちらかになる。
func TestDiskHasDetailMatchesBuilder(t *testing.T) {
	item := disk.Item{Path: "/tmp/x", Size: 10}
	cases := []struct {
		name string
		r    disk.Result
	}{
		{"ok/空", disk.Result{Entry: disk.Entry{ID: "a", DeleteVia: "rm"}, Status: disk.StatusOK}},
		{"ok/items あり", disk.Result{Entry: disk.Entry{ID: "b", DeleteVia: "rm"}, Status: disk.StatusOK, Items: []disk.Item{item}}},
		{"blocked/空", disk.Result{Entry: disk.Entry{ID: "c"}, Status: disk.StatusBlocked, Reason: "対象外"}},
		{"blocked/items あり", disk.Result{Entry: disk.Entry{ID: "d"}, Status: disk.StatusBlocked, Items: []disk.Item{item}}},
		{"failed/空", disk.Result{Entry: disk.Entry{ID: "e"}, Status: disk.StatusFailed}},
		{"failed/Detail あり", disk.Result{Entry: disk.Entry{ID: "f", Detail: "説明"}, Status: disk.StatusFailed}},
		{"blocked/Inspect", disk.Result{Entry: disk.Entry{ID: "g", Inspect: true}, Status: disk.StatusBlocked}},
		{"blocked/Contents あり", disk.Result{Entry: disk.Entry{ID: "h"}, Status: disk.StatusBlocked, Contents: []string{"x"}}},
	}
	v := &doctorView{expanded: map[string]bool{}}
	empties, nonEmpties := 0, 0
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			built := len(v.diskDetail(doctorTestOpts(60), c.r)) > 0
			if got := diskHasDetail(c.r); got != built {
				t.Errorf("diskHasDetail=%v だが diskDetail は %d 行", got, len(v.diskDetail(doctorTestOpts(60), c.r)))
			}
			if built {
				nonEmpties++
			} else {
				empties++
			}
		})
	}
	// 🚨 両方向の例が実際に居ることを確かめる。片側だけの表だと、述語が定数を返す実装でも通る
	if empties == 0 || nonEmpties == 0 {
		t.Fatalf("表が片側に寄っている (空 %d 件 / 非空 %d 件)。両方向の例が要る", empties, nonEmpties)
	}
	t.Logf("突き合わせ %d 件 (展開して 0 行 %d 件 / 1 行以上 %d 件)", len(cases), empties, nonEmpties)
}
