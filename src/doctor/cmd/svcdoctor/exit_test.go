package main

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"doctor/exitcode"
	"doctor/svc"
)

// issue 177: 「診断できず」を exit code で落とさない。特に BrewErr は以前まったく見ていなかった
// (画面に「🚨 診断できず (brew)」と「壊れた登録は見つかりませんでした」が同時に出て exit 0)。
func TestSvcExitCode(t *testing.T) {
	finding := svc.Finding{}
	for _, tc := range []struct {
		name string
		rep  svc.Report
		want int
	}{
		{"候補なし", svc.Report{}, exitcode.NoFindings},
		{"候補あり", svc.Report{Findings: []svc.Finding{finding}}, exitcode.Findings},
		{"brew 台帳が取れない", svc.Report{BrewErr: `exec: "brew": executable file not found in $PATH`}, exitcode.Undiagnosed},
		{"中断", svc.Report{Interrupted: true}, exitcode.Undiagnosed},
		{"launchctl の失敗", svc.Report{StatusErr: "exit 1"}, exitcode.Undiagnosed},
		{"未診断あり", svc.Report{Undiagnosed: []svc.Undiagnosed{{}}}, exitcode.Undiagnosed},
		{"読めないディレクトリ", svc.Report{DirErrs: []string{"permission denied"}}, exitcode.Undiagnosed},
		// 2 が 1 より優先する: 「候補が 1 件あった」より「一部を検査できなかった」の方が重い
		{"候補あり + brew 台帳が取れない", svc.Report{Findings: []svc.Finding{finding}, BrewErr: "x"}, exitcode.Undiagnosed},
	} {
		if got := svcExitCode(tc.rep); got != tc.want {
			t.Errorf("%s: got=%d want=%d", tc.name, got, tc.want)
		}
	}
}

// issue 177 (b): text と -json の**どちらの経路でも**同じ終了コードを返す。
// 以前は -json が return で判定を飛ばしていた。emit は出力の分岐の外で終了コードを決めるので、
// この性質は構造で守られている (この test はそれが崩れたら red になる)。
func TestSvcEmitReturnsSameExitCodeForBothOutputs(t *testing.T) {
	for _, rep := range []svc.Report{
		{},
		{Findings: []svc.Finding{{}}},
		{BrewErr: `exec: "brew": executable file not found in $PATH`},
		{Interrupted: true, Findings: []svc.Finding{{}}},
	} {
		var text, js, errBuf bytes.Buffer
		gotText := emit(rep, false, &text, &errBuf)
		gotJSON := emit(rep, true, &js, &errBuf)
		if gotText != gotJSON {
			t.Errorf("text=%d json=%d で食い違う: %+v", gotText, gotJSON, rep)
		}
		if want := svcExitCode(rep); gotText != want {
			t.Errorf("emit が判定を通っていない: got=%d want=%d", gotText, want)
		}
		if text.Len() == 0 || js.Len() == 0 {
			t.Errorf("出力が空: text=%d json=%d", text.Len(), js.Len())
		}
	}
	// エンコードに失敗したら 3 (出力先が書けない)
	if got := emit(svc.Report{}, true, failWriter{}, io.Discard); got != exitcode.EnvFailure {
		t.Errorf("JSON のエンコード失敗が %d (want %d)", got, exitcode.EnvFailure)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("no space left on device") }
