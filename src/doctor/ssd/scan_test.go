package ssd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// fixture は実機 (2026-09-29, APPLE SSD AP1024Z / macOS 27.0 / smartctl 7.5) の出力。
// smartctl の JSON はシリアル番号を FAKESERIAL0000 に差し替えてある (漏れの検査に使う)。
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type call struct {
	stdout, stderr string
	rc             int
	err            error
}

// fakeRun はコマンド名 (smartctl はパスの末尾) で応答を返す。呼ばれた引数も記録する。
func fakeRun(t *testing.T, calls map[string]call, seen *[]string) func(context.Context, string, ...string) (string, string, int, error) {
	return func(_ context.Context, name string, args ...string) (string, string, int, error) {
		base := name[strings.LastIndex(name, "/")+1:]
		*seen = append(*seen, base+" "+strings.Join(args, " "))
		c, ok := calls[base]
		if !ok {
			t.Fatalf("想定外のコマンド: %s %v", name, args)
		}
		return c.stdout, c.stderr, c.rc, c.err
	}
}

func found(string) (string, error) { return "/opt/homebrew/bin/smartctl", nil }
func missing(string) (string, error) {
	return "", errors.New("not found")
}

func realCalls(t *testing.T) map[string]call {
	return map[string]call{
		"diskutil": {stdout: fixture(t, "diskutil_info_disk0.txt")},
		"smartctl": {stdout: fixture(t, "smartctl_apple_rc4.json"), rc: 4},
	}
}

func scanWith(t *testing.T, calls map[string]call, look func(string) (string, error)) (Report, []string) {
	t.Helper()
	var seen []string
	rep := Scan(context.Background(), Options{Run: fakeRun(t, calls, &seen), LookPath: look})
	return rep, seen
}

func checkByLabel(t *testing.T, rep Report, label string) Check {
	t.Helper()
	for _, c := range rep.Checks {
		if c.Label == label {
			return c
		}
	}
	t.Fatalf("%q の行がありません: %+v", label, rep.Checks)
	return Check{}
}

// 実機の出力 (rc=4 + GetLogPage failed) は ok。rc=4 だけで異常にしない。
func TestScanRealAppleSSDIsOK(t *testing.T) {
	rep, seen := scanWith(t, realCalls(t), found)
	if rep.SMART != StatusOK {
		t.Fatalf("SMART = %q, want ok: %+v", rep.SMART, rep.Checks)
	}
	if rep.APFS != StatusUnchecked {
		t.Fatalf("APFS = %q, want unchecked (doctor は APFS を検査しない)", rep.APFS)
	}
	if rep.Model != "APPLE SSD AP1024Z" || !rep.Internal {
		t.Fatalf("model / internal = %q / %v", rep.Model, rep.Internal)
	}
	want := map[string]string{
		"SMART Status":     "Verified",
		"総合判定":             "PASSED",
		"Critical Warning": "0x00",
		"整合性エラー":           "0",
		"予備領域":             "100% (閾値 99%)",
		"摩耗":               "7% 使用",
		"書き込み総量":           "120 TB",
		"通電時間":             "3,993 h",
		"安全でない電源断":         "11 回",
	}
	for label, value := range want {
		if c := checkByLabel(t, rep, label); c.Value != value {
			t.Errorf("%s = %q, want %q", label, c.Value, value)
		}
	}
	if len(rep.Notes) != 1 || !strings.Contains(rep.Notes[0], "エラー履歴") {
		t.Errorf("rc=4 を無視した理由が Notes に無い: %q", rep.Notes)
	}
	// 読むだけ: 実行したのは 2 本の読み取りだけ (修復・sudo・verifyVolume を起こさない)
	wantSeen := []string{"diskutil info disk0", "smartctl -j -a disk0"}
	if strings.Join(seen, "\n") != strings.Join(wantSeen, "\n") {
		t.Fatalf("実行したコマンド = %q, want %q", seen, wantSeen)
	}
}

// 🚨 シリアル番号は Report のどこにも入らない (smartJSON に欄を置いていない)。
func TestScanDoesNotCarrySerialNumber(t *testing.T) {
	rep, _ := scanWith(t, realCalls(t), found)
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "FAKESERIAL0000") {
		t.Fatalf("シリアル番号が Report に入った: %s", b)
	}
}

// smartctl が無いときは判定不能 (diskutil が Verified でも ok に丸めない)。
func TestScanWithoutSmartctlIsUnknown(t *testing.T) {
	calls := realCalls(t)
	delete(calls, "smartctl") // 呼ばれたら fakeRun が落とす
	rep, _ := scanWith(t, calls, missing)
	if !rep.SmartctlMissing {
		t.Fatalf("SmartctlMissing が立っていない")
	}
	if rep.SMART != StatusUnknown {
		t.Fatalf("SMART = %q, want unknown (smartctl が無いのに ok に丸めた)", rep.SMART)
	}
	if c := checkByLabel(t, rep, "smartctl"); !strings.Contains(c.Note, InstallHint) {
		t.Fatalf("インストールの案内が無い: %q", c.Note)
	}
}

// diskutil が Verified 以外を言えば異常、欄が無い・失敗なら判定不能。
func TestDiskutilStatus(t *testing.T) {
	cases := []struct {
		name string
		c    call
		want Status
	}{
		{"failing", call{stdout: "   SMART Status:   Failing\n"}, StatusFail},
		{"about to fail", call{stdout: "   SMART Status:   About to Fail\n"}, StatusFail},
		// 知らない値は異常ではなく判定不能 (SMART を返さないディスクを壊れていると言わない)
		{"not supported", call{stdout: "   SMART Status:   Not Supported\n"}, StatusUnknown},
		{"欄が無い", call{stdout: "   Device Identifier: disk0\n"}, StatusUnknown},
		{"rc!=0", call{stderr: "Could not find disk", rc: 1}, StatusUnknown},
		{"起動できない", call{err: errors.New("timeout")}, StatusUnknown},
		{"verified", call{stdout: "   SMART Status:   Verified\n"}, StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := realCalls(t)
			calls["diskutil"] = tc.c
			rep, _ := scanWith(t, calls, found)
			if got := checkByLabel(t, rep, "SMART Status").Status; got != tc.want {
				t.Fatalf("SMART Status = %q, want %q", got, tc.want)
			}
			if tc.want != StatusOK && rep.SMART == StatusOK {
				t.Fatalf("総合が ok に丸まった")
			}
		})
	}
}

func int64p(v int64) *int64 { return &v }
func intp(v int) *int       { return &v }
func boolp(v bool) *bool    { return &v }

// okSmart は全部正常の smartJSON。各ケースが 1 欄だけを崩す。
func okSmart() smartJSON {
	var j smartJSON
	j.Smartctl.ExitStatus = intp(0)
	j.SmartStatus = &struct {
		Passed *bool `json:"passed"`
	}{Passed: boolp(true)}
	j.NVMe = &struct {
		CriticalWarning  *int64 `json:"critical_warning"`
		AvailableSpare   *int64 `json:"available_spare"`
		SpareThreshold   *int64 `json:"available_spare_threshold"`
		PercentageUsed   *int64 `json:"percentage_used"`
		MediaErrors      *int64 `json:"media_errors"`
		DataUnitsWritten *int64 `json:"data_units_written"`
		PowerOnHours     *int64 `json:"power_on_hours"`
		UnsafeShutdowns  *int64 `json:"unsafe_shutdowns"`
	}{
		CriticalWarning: int64p(0), AvailableSpare: int64p(100), SpareThreshold: int64p(99),
		PercentageUsed: int64p(7), MediaErrors: int64p(0), DataUnitsWritten: int64p(1),
		PowerOnHours: int64p(1), UnsafeShutdowns: int64p(0),
	}
	return j
}

// 状態の語彙を 1 つずつ区別する: 1 欄だけを崩したら、その行と総合が期待の状態になる。
func TestJudgeSmartDistinguishesStatuses(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*smartJSON)
		label  string
		want   Status
	}{
		{"正常", func(*smartJSON) {}, "", StatusOK},
		{"critical warning", func(j *smartJSON) { j.NVMe.CriticalWarning = int64p(1) }, "Critical Warning", StatusFail},
		{"整合性エラー 1", func(j *smartJSON) { j.NVMe.MediaErrors = int64p(1) }, "整合性エラー", StatusFail},
		{"予備領域が閾値を下回った", func(j *smartJSON) { j.NVMe.AvailableSpare = int64p(98) }, "予備領域", StatusFail},
		// 🚨 閾値ちょうどは異常にしない (spare < threshold。<= にすると実機が 1 目盛りで異常になる)
		{"予備領域が閾値ちょうど", func(j *smartJSON) { j.NVMe.AvailableSpare = int64p(99) }, "予備領域", StatusOK},
		{"摩耗 80%", func(j *smartJSON) { j.NVMe.PercentageUsed = int64p(80) }, "摩耗", StatusWarn},
		{"摩耗 79%", func(j *smartJSON) { j.NVMe.PercentageUsed = int64p(79) }, "摩耗", StatusOK},
		{"総合判定 FAILED", func(j *smartJSON) { j.SmartStatus.Passed = boolp(false) }, "総合判定", StatusFail},
		{"総合判定の欄が無い", func(j *smartJSON) { j.SmartStatus = nil }, "総合判定", StatusUnknown},
		{"整合性エラーの欄が無い", func(j *smartJSON) { j.NVMe.MediaErrors = nil }, "整合性エラー", StatusUnknown},
		{"NVMe の健康情報が無い", func(j *smartJSON) { j.NVMe = nil }, "NVMe の健康情報", StatusUnknown},
		{"DISK FAILING (bit 3)", func(j *smartJSON) { j.Smartctl.ExitStatus = intp(8) }, "smartctl の判定", StatusFail},
		{"知らない警告ビット (0x40)", func(j *smartJSON) { j.Smartctl.ExitStatus = intp(0x40) }, "smartctl の終了状態", StatusUnknown},
		{"デバイスを開けない (bit 1)", func(j *smartJSON) { j.Smartctl.ExitStatus = intp(2) }, "smartctl", StatusUnknown},
		// rc=4 (Apple のエラー履歴) だけなら正常のまま
		{"rc=4 のみ", func(j *smartJSON) { j.Smartctl.ExitStatus = intp(4) }, "", StatusOK},
		// 欄の欠落は判定不能 (ok に丸めない)
		{"予備領域の欄が無い", func(j *smartJSON) { j.NVMe.AvailableSpare = nil }, "予備領域", StatusUnknown},
		{"予備領域の閾値の欄が無い", func(j *smartJSON) { j.NVMe.SpareThreshold = nil }, "予備領域", StatusUnknown},
		{"Critical Warning の欄が無い", func(j *smartJSON) { j.NVMe.CriticalWarning = nil }, "Critical Warning", StatusUnknown},
		{"摩耗の欄が無い", func(j *smartJSON) { j.NVMe.PercentageUsed = nil }, "摩耗", StatusUnknown},
		// 負の値は読み違い
		{"整合性エラーが負", func(j *smartJSON) { j.NVMe.MediaErrors = int64p(-5) }, "整合性エラー", StatusUnknown},
		{"摩耗が負", func(j *smartJSON) { j.NVMe.PercentageUsed = int64p(-1) }, "摩耗", StatusUnknown},
		// Critical Warning: 温度のビットだけなら注意、ほかのビットが混ざれば異常
		{"温度のみ (0x02)", func(j *smartJSON) { j.NVMe.CriticalWarning = int64p(2) }, "Critical Warning", StatusWarn},
		{"温度 + 予備領域 (0x03)", func(j *smartJSON) { j.NVMe.CriticalWarning = int64p(3) }, "Critical Warning", StatusFail},
		{"信頼性の低下 (0x04)", func(j *smartJSON) { j.NVMe.CriticalWarning = int64p(4) }, "Critical Warning", StatusFail},
		// 読めなかったビット (1) と故障 (8) が同時に立っても、故障を落とさない
		{"開けない + DISK FAILING (0x0a)", func(j *smartJSON) { j.Smartctl.ExitStatus = intp(0x0a) }, "smartctl の判定", StatusFail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := okSmart()
			tc.mutate(&j)
			checks, _ := judgeSmart(j, 0)
			if got := overall(checks); got != tc.want {
				t.Fatalf("総合 = %q, want %q: %+v", got, tc.want, checks)
			}
			if tc.label == "" {
				return
			}
			for _, c := range checks {
				if c.Label == tc.label {
					if c.Status != tc.want {
						t.Fatalf("%s = %q, want %q", tc.label, c.Status, tc.want)
					}
					return
				}
			}
			t.Fatalf("%q の行がありません: %+v", tc.label, checks)
		})
	}
}

// smartctl の出力が JSON として読めない / 起動できないときは判定不能。
func TestReadSmartctlFailures(t *testing.T) {
	cases := map[string]call{
		"JSON でない": {stdout: "smartctl: command not supported", rc: 1},
		"起動できない":   {err: errors.New("context deadline exceeded")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			calls := realCalls(t)
			calls["smartctl"] = c
			rep, _ := scanWith(t, calls, found)
			if got := checkByLabel(t, rep, "smartctl").Status; got != StatusUnknown {
				t.Fatalf("smartctl = %q, want unknown", got)
			}
			if rep.SMART != StatusUnknown {
				t.Fatalf("総合 = %q, want unknown", rep.SMART)
			}
		})
	}
}

func TestOverallWithoutJudgedRowsIsUnknown(t *testing.T) {
	if got := overall([]Check{{Status: StatusInfo}}); got != StatusUnknown {
		t.Fatalf("判定する行が無いのに %q", got)
	}
}

func TestFormatting(t *testing.T) {
	for in, want := range map[int64]string{999: "999 B", 1_500_000: "1.5 MB", 120_019_701_760_000: "120 TB"} {
		if got := decimalBytes(in); got != want {
			t.Errorf("decimalBytes(%d) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[int64]string{0: "0", 999: "999", 3993: "3,993", 1234567: "1,234,567"} {
		if got := groupDigits(in); got != want {
			t.Errorf("groupDigits(%d) = %q, want %q", in, got, want)
		}
	}
}

// 表示の関門: diskutil 由来の値に制御文字が混ざっても画面へ出さない。
func TestScanSanitizesExternalStrings(t *testing.T) {
	calls := realCalls(t)
	calls["diskutil"] = call{stdout: "   Device / Media Name: EVIL\x1b]52;c;aGk=\x07\n   SMART Status: Bad\x1b[31m\n"}
	rep, _ := scanWith(t, calls, found)
	b, _ := json.Marshal(rep)
	if strings.ContainsRune(rep.Model, '\x1b') || strings.ContainsRune(checkByLabel(t, rep, "SMART Status").Value, '\x1b') {
		t.Fatalf("制御文字が残った: %s", b)
	}
}

// JSON に exit_status が無ければ、プロセスの rc で判定する (rc=8 の故障を落とさない)。
func TestJudgeSmartFallsBackToProcessRC(t *testing.T) {
	j := okSmart()
	j.Smartctl.ExitStatus = nil
	checks, _ := judgeSmart(j, exitFailing)
	if got := overall(checks); got != StatusFail {
		t.Fatalf("総合 = %q, want fail (プロセスの rc=8 を見ていない): %+v", got, checks)
	}
}

// rc=4 の「故障ではない」注記は、健康情報が読めたときだけ出す。
func TestJudgeSmartRC4NoteNeedsHealthLog(t *testing.T) {
	j := okSmart()
	j.Smartctl.ExitStatus = intp(exitCmdFail)
	j.NVMe = nil
	if _, notes := judgeSmart(j, 0); len(notes) != 0 {
		t.Fatalf("健康情報が無いのに故障ではないと注記した: %q", notes)
	}
}

// 書き込み総量が桁あふれする値でも、負の数を出さない。
func TestJudgeSmartDataUnitsOverflow(t *testing.T) {
	j := okSmart()
	j.NVMe.DataUnitsWritten = int64p(20_000_000_000_000)
	checks, _ := judgeSmart(j, 0)
	for _, c := range checks {
		if c.Label == "書き込み総量" {
			if strings.HasPrefix(c.Value, "-") || !strings.Contains(c.Value, "大きすぎ") {
				t.Fatalf("書き込み総量 = %q", c.Value)
			}
			return
		}
	}
	t.Fatal("書き込み総量の行がありません")
}

// JSON が読めなくても、プロセスの rc が故障 (bit 3) を申告していれば異常を落とさない。
func TestReadSmartctlUnparsableKeepsDiskFailing(t *testing.T) {
	calls := realCalls(t)
	calls["smartctl"] = call{stdout: "{\"nvme_smart_health_information_log\":", rc: exitFailing}
	rep, _ := scanWith(t, calls, found)
	if rep.SMART != StatusFail {
		t.Fatalf("総合 = %q, want fail (rc=8 を落とした): %+v", rep.SMART, rep.Checks)
	}
}

// rc=4 の「故障ではない」注記は、立っているのが 4 だけで、判定した行に異常・判定不能が無いときだけ。
func TestJudgeSmartRC4NoteOnlyWhenHealthy(t *testing.T) {
	cases := []struct {
		name     string
		rc       int
		mutate   func(*smartJSON)
		wantNote bool
	}{
		{"実機の形 (rc=4 だけ・全部正常)", exitCmdFail, func(*smartJSON) {}, true},
		{"rc=4 + 整合性エラー", exitCmdFail, func(j *smartJSON) { j.NVMe.MediaErrors = int64p(5) }, false},
		{"rc=4 + Critical Warning 0x04", exitCmdFail, func(j *smartJSON) { j.NVMe.CriticalWarning = int64p(4) }, false},
		{"rc=4 + Critical Warning が負", exitCmdFail, func(j *smartJSON) { j.NVMe.CriticalWarning = int64p(-1) }, false},
		{"rc=4 + Critical Warning の欄が無い", exitCmdFail, func(j *smartJSON) { j.NVMe.CriticalWarning = nil }, false},
		{"rc=4 + 整合性エラーの欄が無い", exitCmdFail, func(j *smartJSON) { j.NVMe.MediaErrors = nil }, false},
		{"rc=4 + 知らないビット (0x44)", exitCmdFail | 0x40, func(*smartJSON) {}, false},
		{"rc=0 (立っていない)", 0, func(*smartJSON) {}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := okSmart()
			j.Smartctl.ExitStatus = intp(tc.rc)
			tc.mutate(&j)
			_, notes := judgeSmart(j, 0)
			if got := len(notes) == 1; got != tc.wantNote {
				t.Fatalf("注記 = %q (出るべき: %v)", notes, tc.wantNote)
			}
		})
	}
}

// 表示だけの行も、負の値をそのまま出さない。
func TestJudgeSmartInfoRowsRejectNegative(t *testing.T) {
	j := okSmart()
	j.NVMe.PowerOnHours = int64p(-3)
	checks, _ := judgeSmart(j, 0)
	for _, c := range checks {
		if c.Label == "通電時間" {
			if strings.Contains(c.Value, "-3") {
				t.Fatalf("負の値を出した: %q", c.Value)
			}
			return
		}
	}
	t.Fatal("通電時間の行がありません")
}
