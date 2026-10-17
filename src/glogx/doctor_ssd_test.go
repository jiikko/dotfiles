package main

import (
	"strings"
	"testing"

	"doctor/ssd"
	"glogx/internal/doctorview"
)

// smartctl が無ければ、doctor を開いた直後に toast で入れ方を出す (ユーザー指定 2026-09-29)。
func TestDoctorSSDMissingSmartctlShowsToast(t *testing.T) {
	for _, tc := range []struct {
		name      string
		missing   bool
		wantToast bool
	}{{"無い", true, true}, {"在る", false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := func() *browseModel {
				m := newTestBrowse(t, 3, map[string]CIState{}, nil)
				m.doctorOv.Shown = true
				m.doctorOv.Gen = 7
				return m
			}
			m := fresh()
			m.Update(doctorview.DoctorSSDMsg{Gen: 7, Rep: ssd.Report{SmartctlMissing: tc.missing, SMART: ssd.StatusOK}})
			if got := strings.Contains(m.toast.Text(), ssd.InstallHint); got != tc.wantToast {
				t.Fatalf("toast = %q (出るべき: %v)", m.toast.Text(), tc.wantToast)
			}
			// 古い世代 (開き直す前) に届いた結果では出さない
			m = fresh()
			m.Update(doctorview.DoctorSSDMsg{Gen: 6, Rep: ssd.Report{SmartctlMissing: true}})
			if m.toast.Text() != "" {
				t.Fatalf("古い世代の結果で toast が出た: %q", m.toast.Text())
			}
		})
	}
}
