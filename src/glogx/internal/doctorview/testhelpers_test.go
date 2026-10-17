package doctorview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
	"glogx/internal/doctorview/doctortest"
)

// doctor のテストが共有する部品のうち doctorview の型を使うもの (doctortest は doctorview を import できないので、ここに置く。issue 817)。

// testView はテスト用の DoctorView (外への口は doctortest.FakeDeps)。
func testView(t *testing.T) *DoctorView {
	t.Helper()
	v := &DoctorView{}
	v.SetDeps(Deps(doctortest.FakeDeps(t)))
	return v
}

// testProg はテスト用の進捗。表示の中身が主題でないテストの穴埋めに使う。
func testProg(i, total int, label string, ph disk.DeletePhase) DoctorProgress {
	return DoctorProgress{I: i, Total: total, Label: label, Phase: ph, Known: true}
}

func testOpts(page int) DoctorRenderOpts {
	return DoctorRenderOpts{Width: 100, Page: page, Colored: false, Spinner: "⠋", Now: time.Date(2026, 9, 2, 12, 0, 0, 0, time.Local)}
}

func testText(v *DoctorView, page int) string {
	return strings.Join(v.Lines(testOpts(page)), "\n")
}

// writeTestSnapshot は snapshot をキャッシュ位置へ書く (TTL 内 / 外の前提を作る)。
// 同じ 4〜10 行がこのファイルに 8 回あったのを 1 行にしたもの (issue 199)。
//
// 🚨 壊れた JSON を書くテスト (TestDoctorCacheCorruptAndAtomic) と、書き込み先を
// 直接触るテスト (os.Remove / os.Chmod) は path 自体が要るので寄せていない。
// そちらは DoctorSnapshotPath() を直接呼ぶ。
func writeTestSnapshot(t *testing.T, sn DoctorSnapshot) {
	t.Helper()
	data, err := json.Marshal(sn)
	if err != nil {
		t.Fatal(err)
	}
	path, err := DoctorSnapshotPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// runCmds は Cmd を実行して出た Msg を v へ届ける (doctortest.RunCmds)。
func runCmds(t *testing.T, v *DoctorView, cmd tea.Cmd) {
	t.Helper()
	doctortest.RunCmds(t, cmd, func(msg tea.Msg) (tea.Cmd, bool) {
		next, _, ok := v.Receive(msg)
		return next, ok
	})
}
