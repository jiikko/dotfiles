package termsync

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestProgramOptions(t *testing.T) {
	if n := len(ProgramOptions(false)); n != 0 {
		t.Errorf("同期出力の効く端末で %d 個の設定を足した", n)
	}
	if n := len(ProgramOptions(true)); n != 1 {
		t.Errorf("同期出力の無い端末で設定が %d 個 (filter の 1 個のはず)", n)
	}
}

func TestDropSyncReportOnlyDropsSynchronizedOutputReport(t *testing.T) {
	if got := DropSyncReport(nil, tea.ModeReportMsg{Mode: ansi.ModeSynchronizedOutput, Value: ansi.ModeReset}); got != nil {
		t.Errorf("同期出力の答えを通した: %#v (bubbletea が同期出力を使い始める)", got)
	}
	for _, msg := range []tea.Msg{
		tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeReset}, // 同じ問い合わせで返る幅の答えは要る
		tea.KeyPressMsg{Code: 'j', Text: "j"},
	} {
		if got := DropSyncReport(nil, msg); got != msg {
			t.Errorf("同期出力の答え以外を捨てた: %#v → %#v", msg, got)
		}
	}
}
