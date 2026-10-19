package main

import (
	"glogx/internal/testenv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
	"glogx/internal/doctorview"
	"glogx/internal/doctorview/doctortest"
	"glogx/internal/extcmd"
	"glogx/internal/gitdata"
	"glogx/internal/term"
)

// doctorTestView は走査を fake に差し替えた doctor (実データ・実 launchd・実 brew を触らない)。
// disk は偽 HOME の下に 1 エントリだけのカタログ、svc は空ディレクトリ、brew は fake runner。

// browseModel への配線: D で開く / Msg が届く / cancelAll で走査が止まる / hint と viewLines が doctor になる。
func TestDoctorWiredThroughBrowseModel(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
	m.width, m.height = 100, 30
	m.doctorOv.SetDeps(doctorview.Deps(doctortest.FakeDeps(t)))
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
	runDoctorCmdsViaUpdate(t, m, cmd)
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
	if !m.doctorOv.Fixture().ScanStopped() {
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
	m := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
	m.width, m.height = 100, 30
	var copied string
	testenv.Restore(t, &extcmd.CopyToClipboard)
	extcmd.CopyToClipboard = func(s string) error { copied = s; return nil }
	m.doctorOv = doctorview.DoctorView{}
	m.doctorOv.Fixture().Show()
	m.doctorOv.Fixture().Reports(doctorview.TabDisk, &disk.Report{Results: []disk.Result{{Entry: disk.Entry{ID: "a", Label: "A", Risk: disk.RiskSafe, Recover: "r"},
		Status: disk.StatusOK, Size: 1, Items: []disk.Item{{Path: "/p/a", Size: 1}}}}}, nil, nil, nil, nil)
	_ = m.View()
	m.handleKey("y")
	if copied != "/p/a" {
		t.Fatalf("y でパスがクリップボードに届かない: %q", copied)
	}
	if !m.toast.Visible() || !strings.Contains(m.toast.Text(), "パスをコピーしました") {
		t.Errorf("コピーのトーストが出ない: %q", m.toast.Text())
	}
	if !strings.Contains(m.hintLine(), "y: パスをコピー") {
		t.Errorf("hint に y/Y が無い: %q", m.hintLine())
	}
}

// y の案内とトーストの語は、タブではなくカーソル行のコピーの中身から決まる (Homebrew の警告の行はパスではない)。
func TestDoctorCopyToastNamesWhatWasCopied(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
	m.width, m.height = 100, 30
	var copied string
	testenv.Restore(t, &extcmd.CopyToClipboard)
	extcmd.CopyToClipboard = func(s string) error { copied = s; return nil }
	m.doctorOv = doctorview.DoctorView{}
	m.doctorOv.Fixture().Show()
	m.doctorOv.Fixture().Reports(doctorview.TabBrew, nil, nil, []string{"Warning: Some installed formulae are deprecated.\n  foo"}, nil, nil)
	_ = m.View()
	if h := m.hintLine(); !strings.Contains(h, "y: 警告をコピー") || strings.Contains(h, "y: パスをコピー") {
		t.Fatalf("警告の行の hint = %q", h)
	}
	m.handleKey("y")
	if copied == "" {
		t.Fatal("y で何もコピーされない")
	}
	if got := m.toast.Text(); !strings.Contains(got, "警告をコピーしました") || strings.Contains(got, "パス") {
		t.Errorf("トースト = %q (警告をコピーしたのにパスと言っている)", got)
	}
}

// runDoctorCmdsViaUpdate は doctor の Cmd を実行し、出た Msg を browseModel.Update へ通す (配線を含めて見る)。
// 再描画の tick はここでは回さない (走査の Msg だけを届ける)。
func runDoctorCmdsViaUpdate(t *testing.T, m *browseModel, cmd tea.Cmd) {
	t.Helper()
	doctortest.RunCmds(t, cmd, func(msg tea.Msg) (tea.Cmd, bool) {
		if _, ok := msg.(tickMsg); ok {
			return nil, true
		}
		_, next := m.Update(msg)
		return next, true
	})
}
