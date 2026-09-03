package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"doctor/disk"
	"doctor/exitcode"
)

// issue 177: 候補ありを 1 で返す (svcdoctor と語彙を揃える)。走査できなかったものは 2。
func TestDiskExitCode(t *testing.T) {
	item := disk.Item{Path: "/x", Size: 1}
	ok := func(items ...disk.Item) disk.Result {
		return disk.Result{Status: disk.StatusOK, Items: items}
	}
	for _, tc := range []struct {
		name string
		rep  disk.Report
		want int
	}{
		{"候補なし", disk.Report{Results: []disk.Result{ok()}}, exitcode.NoFindings},
		// 🚨 検出条件そのものが未実測のエントリは、0 件でも「きれい」と言わない (issue 236 の P3-4)。
		// 人間向け出力と UI は「0 件ですが候補なしではありません」と言うのに rc だけが丸めていた
		{"未実測で 0 件は診断できず", disk.Report{Results: []disk.Result{
			{Status: disk.StatusOK, Entry: disk.Entry{ID: "x", Unverified: "glob の実名が未実測"}}}}, exitcode.Undiagnosed},
		{"未実測でも候補が出ていれば findings", disk.Report{Results: []disk.Result{
			{Status: disk.StatusOK, Entry: disk.Entry{ID: "x", Unverified: "未実測"}, Items: []disk.Item{item}}}}, exitcode.Findings},
		{"候補あり", disk.Report{Results: []disk.Result{ok(item)}}, exitcode.Findings},
		{"blocked は候補ではない", disk.Report{Results: []disk.Result{{Status: disk.StatusBlocked, Size: 99}}}, exitcode.NoFindings},
		{"走査できず", disk.Report{Results: []disk.Result{{Status: disk.StatusFailed}}}, exitcode.Undiagnosed},
		{"一部の Item が読めず", disk.Report{Results: []disk.Result{{Status: disk.StatusOK, Failures: []string{"permission denied"}}}}, exitcode.Undiagnosed},
		{"中断 (partial)", disk.Report{Partial: true, Results: []disk.Result{ok()}}, exitcode.Undiagnosed},
		// 2 が 1 より優先する
		{"候補あり + 走査できず", disk.Report{Results: []disk.Result{ok(item), {Status: disk.StatusFailed}}}, exitcode.Undiagnosed},
	} {
		if got := diskExitCode(tc.rep); got != tc.want {
			t.Errorf("%s: got=%d want=%d", tc.name, got, tc.want)
		}
	}
}

// 語彙の数字そのものは exitcode package のテストが守る (定数を各 CLI にコピーすると、
// 別 package なので片方だけ変えても機械的に検出できない: 敵対レビュー 2026-09-03)。

// issue 177 (b): text と -json のどちらの経路でも同じ終了コードを返す (svcdoctor と同じ規律)。
func TestDiskEmitReturnsSameExitCodeForBothOutputs(t *testing.T) {
	now := time.Now()
	for _, rep := range []disk.Report{
		{Results: []disk.Result{{Status: disk.StatusOK, Items: []disk.Item{}}}},
		{Results: []disk.Result{{Entry: disk.Entry{ID: "a", Label: "A", Recover: "x", DeleteVia: "rm"},
			Status: disk.StatusOK, Size: 1, Items: []disk.Item{{Path: "/x", Size: 1}}}}},
		{Results: []disk.Result{{Entry: disk.Entry{ID: "a", Label: "A"}, Status: disk.StatusFailed, Reason: "x"}}},
	} {
		var text, js, errBuf bytes.Buffer
		gotText := emit(rep, false, now, &text, &errBuf)
		gotJSON := emit(rep, true, now, &js, &errBuf)
		if gotText != gotJSON {
			t.Errorf("text=%d json=%d で食い違う: %+v", gotText, gotJSON, rep)
		}
		if want := diskExitCode(rep); gotText != want {
			t.Errorf("emit が判定を通っていない: got=%d want=%d", gotText, want)
		}
	}
	if got := emit(disk.Report{}, true, now, failWriter{}, io.Discard); got != exitcode.EnvFailure {
		t.Errorf("JSON のエンコード失敗が %d (want %d)", got, exitcode.EnvFailure)
	}
}

// 候補 0 件でも見出しだけで終わらせない (UI と svcdoctor は「見つかりませんでした」を出す)。
func TestDiskFormatSaysNoCandidates(t *testing.T) {
	now := time.Now()
	var b bytes.Buffer
	emit(disk.Report{Results: []disk.Result{{Status: disk.StatusOK, Items: []disk.Item{}}}}, false, now, &b, io.Discard)
	if !strings.Contains(b.String(), "掃除の候補はありませんでした") {
		t.Errorf("候補 0 件のとき見出しだけで終わっている:\n%s", b.String())
	}
	// 候補があるときは出さない
	var b2 bytes.Buffer
	emit(disk.Report{Results: []disk.Result{{Entry: disk.Entry{ID: "a", Label: "A", Recover: "x", DeleteVia: "rm"},
		Status: disk.StatusOK, Size: 1, Items: []disk.Item{{Path: "/x", Size: 1}}}}}, false, now, &b2, io.Discard)
	if strings.Contains(b2.String(), "掃除の候補はありませんでした") {
		t.Errorf("候補があるのに「ありません」と出た:\n%s", b2.String())
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("no space left on device") }
