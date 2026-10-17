package main

import (
	"strings"
	"testing"

	"doctor/ssd"
	"glogx/internal/doctorview"
	"glogx/internal/doctorview/doctortest"
)

// smartctl が無ければ、doctor を開いた直後に toast で入れ方を出す (ユーザー指定 2026-09-29)。
func TestDoctorSSDMissingSmartctlShowsToast(t *testing.T) {
	for _, tc := range []struct {
		name      string
		missing   bool
		wantToast bool
	}{{"無い", true, true}, {"在る", false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := func(missing bool) *browseModel {
				m := newTestBrowse(t, 3, map[string]CIState{}, nil)
				d := doctortest.FakeDeps(t)
				d.SSD = doctortest.FakeSSDOptions(!missing)
				m.doctorOv.SetDeps(doctorview.Deps(d))
				return m
			}
			m := fresh(tc.missing)
			runDoctorCmdsViaUpdate(t, m, m.doctorOv.Toggle())
			if got := strings.Contains(m.toast.Text(), ssd.InstallHint); got != tc.wantToast {
				t.Fatalf("toast = %q (出るべき: %v)", m.toast.Text(), tc.wantToast)
			}
			// 古い世代の結果: 開いた後に閉じて開き直すと、最初に開いたときの Cmd の結果は捨てる
			m = fresh(true)
			old := m.doctorOv.Toggle()
			m.doctorOv.Toggle()
			_ = m.doctorOv.Toggle()
			runDoctorCmdsViaUpdate(t, m, old)
			if m.toast.Text() != "" {
				t.Fatalf("古い世代の結果で toast が出た: %q", m.toast.Text())
			}
		})
	}
}
