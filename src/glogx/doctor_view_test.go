package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
	"doctor/svc"
	"glogx/internal/doctorview"
	"glogx/internal/doctorview/doctortest"
	"glogx/internal/term"
)

// doctorTestView は走査を fake に差し替えた doctor (実データ・実 launchd・実 brew を触らない)。
// disk は偽 HOME の下に 1 エントリだけのカタログ、svc は空ディレクトリ、brew は fake runner。

// browseModel への配線: D で開く / Msg が届く / cancelAll で走査が止まる / hint と viewLines が doctor になる。
func TestDoctorWiredThroughBrowseModel(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.width, m.height = 100, 30
	tv := doctortest.DoctorTestView(t)
	m.doctorOv = *tv
	_, cmd := m.handleKey("D")
	if !m.doctorOv.Visible() {
		t.Fatal("D で doctor が開かない")
	}
	if !strings.Contains(m.hintLine(), "再スキャン") {
		t.Errorf("doctor の hint が出ない: %q", m.hintLine())
	}
	if !strings.Contains(term.StripANSI(m.View().Content), "ディスク占有") {
		t.Error("View が doctor を描いていない")
	}
	if !m.spinnerActive() {
		t.Error("走査中なのに spinnerActive が false (進捗が止まって見える)")
	}
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				pending = append(pending, sub)
			}
		case nil:
		case tickMsg:
		default:
			_, next := m.Update(msg)
			pending = append(pending, next)
		}
	}
	if m.doctorOv.Scanning() {
		t.Fatal("Update 経由で全セクションが完了しない (case の配線漏れ)")
	}
	before := m.cursor
	m.handleKey("j")
	if m.cursor != before {
		t.Error("doctor 表示中に j が裏の一覧を動かした")
	}
	m.handleKey("r")
	if !m.doctorOv.Scanning() {
		t.Fatal("r で再スキャンが始まらない")
	}
	m.cancelAll()
	if m.doctorOv.Cancel != nil {
		t.Error("cancelAll が doctor の走査を止めていない")
	}
	m.handleKey("esc")
	if m.doctorOv.Visible() {
		t.Error("esc で閉じない")
	}
	if strings.Contains(m.hintLine(), "再スキャン") {
		t.Error("閉じた後も doctor の hint が残る")
	}
}

// browseModel 経由: y がクリップボード (差し替え) に届き、トーストが出る。
func TestDoctorCopyThroughBrowseModel(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.width, m.height = 100, 30
	var copied string
	orig := copyToClipboard
	copyToClipboard = func(s string) error { copied = s; return nil }
	t.Cleanup(func() { copyToClipboard = orig })
	m.doctorOv = doctorview.DoctorView{Shown: true, Expanded: map[doctorview.RowKey]bool{}}
	m.doctorOv.DiskRep = &disk.Report{Results: []disk.Result{{Entry: disk.Entry{ID: "a", Label: "A", Risk: disk.RiskSafe, Recover: "r"},
		Status: disk.StatusOK, Size: 1, Items: []disk.Item{{Path: "/p/a", Size: 1}}}}}
	m.doctorOv.SvcRep = &svc.Report{}
	m.doctorOv.Brew = &doctorview.BrewDoctorResult{Clean: true}
	_ = m.View()
	m.handleKey("y")
	if copied != "/p/a" {
		t.Fatalf("y でパスがクリップボードに届かない: %q", copied)
	}
	if !m.toast.Visible() || !strings.Contains(m.toast.Text(), "コピー") {
		t.Errorf("コピーのトーストが出ない: %q", m.toast.Text())
	}
	if !strings.Contains(m.hintLine(), "y: パスをコピー") {
		t.Errorf("hint に y/Y が無い: %q", m.hintLine())
	}
}
