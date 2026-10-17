package doctorview

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"doctor/disk"
	"doctor/docker"
	"doctor/svc"
	"glogx/internal/doctorview/doctortest"
	"glogx/internal/testenv"
)

// 外部由来の文字列が「無害化を通らずに端末へ出る」sink が doctor 画面に残っていないことを、sink ごとに固定する
// (main の untrusted_display_test.go から doctor の分を移した。issue 817)。
//
// doctor は「走査したて (live)」と「保存から復元」の 2 経路を持つが、無害化は復元側にしか
// 無かった。この節は **live 経路**を固定する。fixture は実際に制御文字入りのディレクトリを
// 作って走査させる: 復元経路からしか制御文字を入れないテストは、今回の穴 (live) を
// **構造的に一度も踏まない**まま緑になる (既存の TestDoctorSnapshotTrustBoundaryFreeText が
// まさにその形だった)。

// doctorAllRows は detail (畳まれている行) まで含めた全 row。
// 🚨 見えている行だけを見ると、Enter で開く中身 (Contents = ReadDir の名前) が検査から漏れる。
func doctorAllRows(rows []doctorRow) []doctorRow {
	out := make([]doctorRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, r)
		out = append(out, doctorAllRows(r.detail)...)
	}
	return out
}

// 実在のディレクトリ名に端末制御シーケンスを入れて走査させる。macOS のファイル名は `/` と NUL
// 以外の任意バイトを許し、カタログの対象 ($TMPDIR / ~/Library/Caches) は誰でも書ける場所なので、
// これは「攻撃者が置ける状態」そのもの。
func TestDoctorLiveDiskScanSanitizesRealFileNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	root := filepath.Join(home, "Library", "Caches", "evil")
	// ① 名前そのものに制御文字を含むディレクトリ (Item.Path になる)
	evilDir := filepath.Join(root, "ev"+testenv.OSC52Seq+"il")
	// ② 名前は綺麗だが、中身の名前に制御文字を含む (Contents になる)
	okDir := filepath.Join(root, "plain") // 走査結果のパスは /private 解決を挟むので比較は末尾で行う
	for _, d := range []string{evilDir, okDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "f"), make([]byte, 4096), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(okDir, "child"+testenv.ClearSeq+"name"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := &DoctorView{deps: Deps{
		DiskOpts: func() disk.Options {
			return disk.Options{
				Env: disk.Env{Home: home, TmpDir: home + "/", Getenv: func(string) string { return "" }},
				Run: func(context.Context, string, ...string) (string, string, int, error) { return "", "", 0, nil },
				Catalog: []disk.Entry{{ID: "evil", Label: "細工されたキャッシュ", Tier: 1, Risk: disk.RiskSafe,
					DeleteVia: "rm", Recover: "再生成されます", Inspect: true,
					Paths: []string{"~/Library/Caches/evil/*"}}},
				BootTime: func() (time.Time, error) { return time.Now(), nil },
			}
		},
		SvcOpts: func() svc.Options {
			return svc.Options{Dirs: []svc.LaunchDir{{Path: filepath.Join(home, "LaunchAgents"), Domain: "gui/501"}},
				Run: func(context.Context, string, ...string) (string, string, int, error) {
					return "PID\tStatus\tLabel\n", "", 0, nil
				}}
		},
		BrewRun:    func(context.Context, string, ...string) (string, string, int, error) { return "", "", 0, nil },
		DockerOpts: doctortest.NoDockerOptions, // 本物の docker system df を叩かない (issue 419)
		SSDOpts:    doctortest.FakeSSDOptions(true),
	}}
	runCmds(t, v, v.open())

	if len(v.diskResults) != 1 {
		t.Fatalf("前提が作れていない: Result が %d 件", len(v.diskResults))
	}
	r := v.diskResults[0]
	// 制御文字入りのパスは**書き換えずに落とす** (書き換えると削除の照合から外れて
	// 「見えているものと消えるものが違う」になる)。落としたことは Failures に残る
	// 🚨 パスは t.TempDir() の /var → /private/var 解決を挟むので末尾で見る
	if len(r.Items) != 1 || !strings.HasSuffix(r.Items[0].Path, "/Library/Caches/evil/plain") {
		t.Fatalf("残った Item が想定と違う: %+v", r.Items)
	}
	if r.Size != r.Items[0].Size {
		t.Errorf("落とした Item の分が合計から引かれていない: 合計 %d / 残った Item %d", r.Size, r.Items[0].Size)
	}
	var noted bool
	for _, f := range r.Failures {
		if strings.Contains(f, "制御文字") {
			noted = true
		}
	}
	if !noted {
		t.Errorf("落としたことが Failures に残っていない: %+v", r.Failures)
	}
	// 中身の名前 (Contents) は落とさず無害化する (Enter で開く行に出るだけで、照合には使わない)
	if len(r.Contents) == 0 {
		t.Fatal("前提が作れていない: Contents が空 (Inspect が効いていない)")
	}

	// 画面 (行) と `y` / `Y` のコピーに制御文字が出ない
	for _, line := range strings.Split(testText(v, 60), "\n") {
		if testenv.HasTerminalControl(line) {
			t.Errorf("doctor の行に制御シーケンスが残った: %q", line)
		}
	}
	for _, row := range doctorAllRows(v.rows) {
		if testenv.HasTerminalControl(row.text) {
			t.Errorf("row の text に制御シーケンスが残った: %q", row.text)
		}
		if testenv.HasControlExceptNewline(row.copyPath) {
			t.Errorf("y のコピー (pbcopy へ生で渡る) に制御シーケンスが残った: %q", row.copyPath)
		}
		if testenv.HasControlExceptNewline(row.copyText) {
			t.Errorf("Y のコピー (pbcopy へ生で渡る) に制御シーケンスが残った: %q", row.copyText)
		}
	}
}

// svc (plist の Label) / brew (警告本文) も live 経路で無害化する。
// 🚨 seam は Msg の受け口 (走査結果がモデルへ入る場所)。ここを通らない値は画面にもコピーにも
// 出ない、という形にしてある。
func TestDoctorLiveSvcAndBrewAreSanitized(t *testing.T) {
	v := testView(t)
	v.shown, v.gen = true, 1
	v.receiveSvc(doctorSvcMsg{gen: 1, rep: svc.Report{
		Scanned: 2,
		Findings: []svc.Finding{{
			// ① 識別子が細工されている = **落とす** (書き換えて出すと、提示する
			// `launchctl bootout` / `rm` が別のファイルを指す)
			Label: "com.evil" + testenv.OSC52Seq, PlistPath: "/Library/LaunchAgents/x" + testenv.ClearSeq + ".plist",
			Domain: "gui/501",
		}, {
			// ② 識別子は正当で、表示だけの自由文が細工されている = **残して無害化する**
			Label: "com.ok", PlistPath: "/Library/LaunchAgents/ok.plist", Domain: "gui/501",
			Reasons:     []string{"実行ファイルが無い\n偽の行" + testenv.ClearSeq},
			MissingExec: "/usr/local/bin/gone" + testenv.ClearSeq,
			BrewFormula: "formula" + testenv.OSC52Seq,
			RestartKeys: []string{"KeepAlive" + testenv.ClearSeq},
			Commands:    []string{"launchctl bootout gui/501/com.ok" + testenv.ClearSeq},
		}},
		Undiagnosed: []svc.Undiagnosed{{PlistPath: "/x" + testenv.OSC52Seq + ".plist", Reason: "壊れ" + testenv.ClearSeq + "た"}},
		StatusErr:   "launchctl 失敗" + testenv.ClearSeq,
		BrewErr:     "brew 失敗" + testenv.OSC52Seq,
		DirErrs:     []string{"読めず" + testenv.ClearSeq},
	}})
	// 🚨 Warnings と Unavailable を**同時に立てない**。brewSection は Unavailable != "" で
	// 早期 return するので、両方立てると警告に触れる row が 0 個になり、Warnings 側の
	// 無害化を外しても緑のまま通る (敵対レビュー 2026-09-04 が実測した false green)。
	v.receiveBrew(doctorBrewMsg{gen: 1, res: brewDoctorResult{
		Warnings: []string{"Warning: 危険" + testenv.OSC52Seq + "\n2 行目は正常" + testenv.ClearSeq},
	}})

	// 🚨 **全タブを描いて集める。** タブ分割後は 1 回の描画で 1 タブぶんの行しか出ないので、
	// 1 タブだけ見ると残りの節 (disk / svc / brew) が検査対象から外れる = 穴になる。
	// この検査の主題は「どの節の外部由来文字列も無害化されているか」なので全部通す
	var rows []doctorRow
	for tb := DoctorTab(0); tb < numDoctorTabs; tb++ {
		v.tab = tb
		for _, line := range strings.Split(testText(v, 60), "\n") {
			if testenv.HasTerminalControl(line) {
				t.Errorf("doctor の行に制御シーケンスが残った (tab=%d): %q", tb, line)
			}
		}
		rows = append(rows, doctorAllRows(v.rows)...)
	}
	for _, row := range rows {
		if testenv.HasTerminalControl(row.text) {
			t.Errorf("row の text に制御シーケンスが残った: %q", row.text)
		}
		if testenv.HasControlExceptNewline(row.copyPath) || testenv.HasControlExceptNewline(row.copyText) {
			t.Errorf("row のコピーに制御シーケンスが残った: %q / %q", row.copyPath, row.copyText)
		}
	}
	// brew の警告は**複数行が正常**なので改行は残す (1 行に潰すと段落が読めなくなる)
	if v.brew == nil || len(v.brew.Warnings) != 1 || !strings.Contains(v.brew.Warnings[0], "\n") {
		t.Fatalf("brew の改行まで落ちた: %+v", v.brew)
	}
	if testenv.HasControlExceptNewline(v.brew.Warnings[0]) {
		t.Errorf("brew の警告本文に制御シーケンスが残った: %q", v.brew.Warnings[0])
	}
	// 🚨 警告本文が**実際に row として描かれている**ことを確かめる (描かれていなければ
	// 上の assert は「モデルの値」しか見ておらず、表示 sink を 1 つも守っていない)
	var sawWarning bool
	for _, row := range rows {
		if strings.Contains(row.text, "危険") {
			sawWarning = true
		}
	}
	if !sawWarning {
		t.Error("brew の警告が row に出ていない (fixture が分岐を殺している = false green)")
	}

	// svc: 識別子が細工された Finding は落とし、件数を残す。自由文だけの Finding は残して無害化する
	if v.svcRep == nil {
		t.Fatal("svc の結果が入っていない")
	}
	if len(v.svcRep.Findings) != 1 || v.svcRep.Findings[0].Label != "com.ok" {
		t.Fatalf("識別子が細工された Finding を落としていない: %+v", v.svcRep.Findings)
	}
	if len(v.svcRep.Undiagnosed) != 0 {
		t.Errorf("識別子が細工された Undiagnosed を落としていない: %+v", v.svcRep.Undiagnosed)
	}
	var noted bool
	for _, e := range v.svcRep.DirErrs {
		if strings.Contains(e, "制御文字") {
			noted = true
		}
	}
	if !noted {
		t.Errorf("落としたことが DirErrs に残っていない (CLI の終了コードにも出なくなる): %+v", v.svcRep.DirErrs)
	}
	f := v.svcRep.Findings[0]
	// 🚨 フィールドごとに見る (連結して 1 回の assert にすると、どれか 1 つの無害化を外した
	// 変異でも red になり、**どのフィールドが守られているか**が区別できない)
	assertNoControl(t, map[string]string{
		"svc StatusErr":           v.svcRep.StatusErr,
		"svc BrewErr":             v.svcRep.BrewErr,
		"svc DirErrs[0]":          v.svcRep.DirErrs[0],
		"Finding.Reasons[0]":      f.Reasons[0],
		"Finding.MissingExec":     f.MissingExec,
		"Finding.BrewFormula":     f.BrewFormula,
		"Finding.RestartKeys[0]":  f.RestartKeys[0],
		"Finding.Commands (連結)":   strings.Join(f.Commands, "\x00"),
		"brew Unavailable (別ケース)": "",
	})
}

// assertNoControl は「フィールド名 → 値」を 1 つずつ見る。
// 🚨 連結して 1 回で見ると、どのフィールドの無害化を外しても同じ red になり、
// **フィールド単位の変異を区別できない** (敵対レビュー 2026-09-04 が、当初のテストで
// 半分のフィールドが無検査だったことを実測)。
func assertNoControl(t *testing.T, fields map[string]string) {
	t.Helper()
	for name, v := range fields {
		if testenv.HasTerminalControl(strings.ReplaceAll(v, "\x00", "")) {
			t.Errorf("%s に制御シーケンスが残った: %q", name, v)
		}
	}
}

// brew の Unavailable は**1 行**の row として描かれるので、改行を残す PlainBlock で通しては
// いけない (固定高パネルの行数が狂う)。
func TestDoctorBrewUnavailableIsSingleLine(t *testing.T) {
	v := testView(t)
	v.shown, v.gen = true, 1
	v.receiveBrew(doctorBrewMsg{gen: 1, res: brewDoctorResult{
		Unavailable: "brew 無し" + testenv.ClearSeq + "\n ⛔ 偽の行 (これは brew の出力ではない)",
	}})
	if v.brew == nil || strings.Contains(v.brew.Unavailable, "\n") {
		t.Errorf("1 行として描く値に改行が残った: %q", v.brew.Unavailable)
	}
	const page = 20
	lines := v.Lines(testOpts(page))
	if len(lines) != page {
		t.Fatalf("行数が page と違う: %d", len(lines))
	}
	// 🚨 **要素数ではなく実際の行数**を見る (要素の中に改行があると端末では行が増える)
	if got := strings.Count(strings.Join(lines, "\n"), "\n") + 1; got != page {
		t.Errorf("端末に出る行数が %d (page=%d): 要素の中に改行が残っている", got, page)
	}
}

// 削除の記録 (実行したコマンドの stdout / stderr と、触った対象のパス) も外部由来。
// パネルに描かれるだけでなく `y` でまるごとコピーできる = 貼った先の端末で発火する。
func TestDoctorDeleteLogAndReportAreSanitized(t *testing.T) {
	v := testView(t)
	v.shown, v.gen = true, 1
	v.del.running = true

	v.receiveDelete(doctorDeleteMsg{gen: 1, ev: doctorDeleteEvent{
		prog: &doctorProgress{i: 2, total: 3, label: "細工されたラベル" + testenv.ClearSeq, known: true},
	}})
	v.receiveDelete(doctorDeleteMsg{gen: 1, ev: doctorDeleteEvent{cmd: &disk.CommandRecord{
		Name: "xcrun" + testenv.ClearSeq, Args: []string{"simctl", "delete", "id" + testenv.ClearSeq},
		RC: 0, Stdout: "消しました" + testenv.OSC52Seq + "\n2 行目", Stderr: "警告" + testenv.ClearSeq,
		Err: "起動できず" + testenv.OSC52Seq,
	}}})
	v.receiveDelete(doctorDeleteMsg{gen: 1, ev: doctorDeleteEvent{rep: &disk.DeleteReport{
		HistoryPath: "/tmp/hist" + testenv.ClearSeq + ".json",
		// HistoryError は err.Error() (= ファイルシステムのエラー文字列) がそのまま入り、
		// 削除パネルの tail に「記録を書けませんでした: 」と続けて出る (issue 251)
		HistoryError: "書けません" + testenv.OSC52Seq,
		Entries: []disk.EntryOutcome{{
			ID: "evil", Label: "細工" + testenv.OSC52Seq, Outcome: disk.OutcomeDeleted,
			Reason: "理由" + testenv.ClearSeq,
			Items: []disk.ItemOutcome{{
				Path: "/tmp/ev" + testenv.OSC52Seq + "il", Outcome: disk.OutcomeDeleted, Reason: "消した" + testenv.ClearSeq,
			}},
		}},
	}}})

	if v.del.result == nil || len(v.del.log) == 0 {
		t.Fatal("前提が作れていない: 記録が入っていない")
	}
	assertNoControl(t, map[string]string{
		"del.progress.label": v.del.progress.label,
		"CommandRecord":      strings.Join(v.del.log, "\x00"),
	})
	for _, l := range v.del.log {
		if testenv.HasTerminalControl(l) {
			t.Errorf("実行の記録に制御シーケンスが残った: %q", l)
		}
	}
	// パネル (画面) と y のコピーの両方
	for _, line := range v.Lines(testOpts(40)) {
		if testenv.HasTerminalControl(line) {
			t.Errorf("削除パネルに制御シーケンスが残った: %q", line)
		}
	}
	if testenv.HasControlExceptNewline(v.deleteLogText()) {
		t.Errorf("y のコピー (pbcopy へ生で渡る) に制御シーケンスが残った: %q", v.deleteLogText())
	}
	// 🚨 削除の**記録**ではパスを落とさずに書き換える (落とすと「触ったのに一覧に出ない対象」が
	// できて記録として嘘になる。照合にはもう使わない)
	if got := v.del.result.Entries[0].Items[0].Path; !strings.HasPrefix(got, "/tmp/ev") {
		t.Errorf("触った対象が記録から消えた: %q", got)
	}
}

// live の Msg 受け口へ「実走査では作りにくいが実際に外部由来」のフィールドを直接流す。
// 🚨 実 FS の fixture (TestDoctorLiveDiskScanSanitizesRealFileNames) はパスと Contents しか
// 作れないので、Reason / Entry の各文字列 / Ref はそちらの視界に入らない。
func TestDoctorLiveDiskMsgSanitizesEveryDisplayedField(t *testing.T) {
	v := testView(t)
	v.shown, v.gen = true, 1
	v.receiveDisk(doctorDiskMsg{gen: 1, ev: doctorDiskEvent{r: &disk.Result{
		Entry: disk.Entry{
			ID: "thing", Label: "ラベル" + testenv.OSC52Seq, Risk: disk.Risk("safe" + testenv.ClearSeq),
			Recover: "戻せます" + testenv.ClearSeq, Detail: "補足" + testenv.OSC52Seq,
			DeleteVia: "rm" + testenv.ClearSeq, Unverified: "未実測" + testenv.ClearSeq,
		},
		Status: disk.StatusOK, Size: 8192,
		// 🚨 2 本目は**生の不正 UTF-8 バイト** (0x9b = 8bit CSI)。`unicode.IsPrint` は
		// U+FFFD を印字可能と答えるので、自前の走査だとこれを通していた
		Items: []disk.Item{{Path: "/tmp/ok", Size: 4096, Ref: "id" + testenv.ClearSeq},
			{Path: "/tmp/raw\x9bbyte", Size: 4096}},
		Reason:   "理由" + testenv.OSC52Seq,
		Failures: []string{"読めず" + testenv.ClearSeq},
		Contents: []string{"child" + testenv.OSC52Seq},
	}}})

	if len(v.diskResults) != 1 {
		t.Fatalf("Result が入っていない: %d", len(v.diskResults))
	}
	r := v.diskResults[0]
	if len(r.Items) != 1 || r.Items[0].Path != "/tmp/ok" {
		t.Fatalf("残った Item が想定と違う (不正 UTF-8 のパスだけ落ちること): %+v", r.Items)
	}
	if r.Size != 4096 {
		t.Errorf("落とした Item の分が合計から引かれていない: %d", r.Size)
	}
	assertNoControl(t, map[string]string{
		"Result.Reason":      r.Reason,
		"Result.Failures[0]": r.Failures[0],
		"Result.Contents[0]": r.Contents[0],
		"Entry.Label":        r.Entry.Label,
		"Entry.Risk":         string(r.Entry.Risk),
		"Entry.Recover":      r.Entry.Recover,
		"Entry.Detail":       r.Entry.Detail,
		"Entry.DeleteVia":    r.Entry.DeleteVia,
		"Entry.Unverified":   r.Entry.Unverified,
		"Item.Ref":           r.Items[0].Ref,
	})
	// 🚨 rows は lines() が組む。呼ばずに v.rows を回すと**空スライスを回すだけ**の
	// vacuous な assert になる
	for _, line := range strings.Split(testText(v, 60), "\n") {
		if testenv.HasTerminalControl(line) {
			t.Errorf("doctor の行に制御シーケンスが残った: %q", line)
		}
	}
	if len(v.rows) == 0 {
		t.Fatal("row が 1 つも組まれていない (以下の検査が空回りする)")
	}
	for _, row := range doctorAllRows(v.rows) {
		if testenv.HasTerminalControl(row.text) {
			t.Errorf("row の text に制御シーケンスが残った: %q", row.text)
		}
	}

	// 🚨 **落とす対象が 1 件も無いケース**も見る。無害化した Items の反映を
	// `if dropped > 0` の中でだけ行っていた実装は、上の fixture (1 件落ちる) では検出できない
	// — 落ちた瞬間に反映されるので緑になる (敵対レビュー 2026-09-04 の P3 を、当初のテストは
	// 素通りさせた)。
	v2 := testView(t)
	v2.shown, v2.gen = true, 1
	v2.receiveDisk(doctorDiskMsg{gen: 1, ev: doctorDiskEvent{r: &disk.Result{
		Entry: disk.Entry{ID: "thing", Label: "ラベル"}, Status: disk.StatusOK, Size: 4096,
		Items: []disk.Item{{Path: "/tmp/ok", Size: 4096, Ref: "id" + testenv.ClearSeq}},
	}}})
	if len(v2.diskResults) != 1 || len(v2.diskResults[0].Items) != 1 {
		t.Fatalf("前提が作れていない: %+v", v2.diskResults)
	}
	if got := v2.diskResults[0].Items[0].Ref; testenv.HasTerminalControl(got) {
		t.Errorf("落とす対象が無いときに Ref の無害化が捨てられている: %q", got)
	}
}

// 保存から復元する経路も同じ関門を通る。
// 🚨 **Entry は復元では JSON 由来 = 攻撃者が決められる** (live ではカタログの写しなので無害)。
// doctorRiskMark は `string(Entry.Risk)` を、行は Label / Recover をそのまま描く。
// 🚨 svc の PlistPath は `validPlistPath` が文字種を絞らないので、以前はここだけ素通りしていた。
func TestDoctorSnapshotRestoreSanitizesEntryAndPlistPath(t *testing.T) {
	v := testView(t)
	now := time.Now()
	writeTestSnapshot(t, doctorSnapshot{ScannedAt: now.Add(-time.Minute),
		Disk: disk.Report{Results: []disk.Result{{
			Entry: disk.Entry{
				ID: "thing", Label: "ラベル" + testenv.OSC52Seq, Risk: disk.Risk("safe" + testenv.ClearSeq),
				Recover: "戻せます" + testenv.ClearSeq, Detail: "補足" + testenv.OSC52Seq,
				DeleteVia: "rm" + testenv.ClearSeq, Unverified: "未実測" + testenv.ClearSeq,
			},
			Status: disk.StatusOK, Size: 4096, MeasuredAt: now.Add(-time.Minute),
			Items: []disk.Item{{Path: "/ok", Size: 4096}},
		}}},
		Svc: svc.Report{
			Findings: []svc.Finding{{
				Label: "com.ok", Domain: "gui/501",
				PlistPath: "/Library/LaunchAgents/a" + testenv.OSC52Seq + "b.plist",
			}},
			Undiagnosed: []svc.Undiagnosed{{
				PlistPath: "/Library/LaunchAgents/u" + testenv.ClearSeq + ".plist", Reason: "壊れた",
			}},
		},
	})
	// 🚨 open() は docker の Cmd を返す (docker だけ snapshot に載せていない)。
	// 主張は「disk / svc / brew を走査し直していない」なので snapshotAt で見る
	runCmds(t, v, v.open())
	if v.snapshotAt.IsZero() {
		t.Fatal("TTL 内なのに走査した (前提が作れていない)")
	}
	if len(v.diskResults) != 1 {
		t.Fatalf("復元できていない: %+v", v.diskResults)
	}
	e := v.diskResults[0].Entry
	assertNoControl(t, map[string]string{
		"Entry.Label":      e.Label,
		"Entry.Risk":       string(e.Risk),
		"Entry.Recover":    e.Recover,
		"Entry.Detail":     e.Detail,
		"Entry.DeleteVia":  e.DeleteVia,
		"Entry.Unverified": e.Unverified,
	})
	// 識別子 (提示コマンドが指す先) が細工されたものは落とす
	if v.svcRep == nil {
		t.Fatal("svc が復元されていない")
	}
	if len(v.svcRep.Findings) != 0 || len(v.svcRep.Undiagnosed) != 0 {
		t.Errorf("制御文字入りの PlistPath が復元された: %+v / %+v", v.svcRep.Findings, v.svcRep.Undiagnosed)
	}
	for _, line := range strings.Split(testText(v, 60), "\n") { // rows を組ませる (下の検査の前提)
		if testenv.HasTerminalControl(line) {
			t.Errorf("doctor の行に制御シーケンスが残った: %q", line)
		}
	}
	if len(v.rows) == 0 {
		t.Fatal("row が 1 つも組まれていない (以下の検査が空回りする)")
	}
	for _, row := range doctorAllRows(v.rows) {
		if testenv.HasTerminalControl(row.text) {
			t.Errorf("row の text に制御シーケンスが残った: %q", row.text)
		}
		if testenv.HasControlExceptNewline(row.copyPath) || testenv.HasControlExceptNewline(row.copyText) {
			t.Errorf("コピーに制御シーケンスが残った: %q / %q", row.copyPath, row.copyText)
		}
	}
}

// flattenDoctorRows は「1 row = 1 行」を出口 1 箇所で守る保険。
// 🚨 **現時点で到達経路は無い** (外部由来の値は 1 行の場所では PlainLine を通るため)。
// それでも置くのは、無害化の使い分けを間違えた 1 行が入った瞬間に固定高の契約が壊れ、
// かつ行数を数えないテストでは気づけないため (敵対レビュー 2026-09-04 が brew の
// Unavailable でその形を実測した)。到達経路が無い以上、検査はこの直接テストが持つ。
func TestFlattenDoctorRowsEnforcesSingleLine(t *testing.T) {
	rows := flattenDoctorRows([]doctorRow{{
		text: "親\n偽の行", copyText: "コピーは\n複数行が正常",
		detail: []doctorRow{{text: "子\n偽の行"}},
	}})
	if got := rows[0].text; got != "親 偽の行" {
		t.Errorf("親の改行が残った: %q", got)
	}
	if got := rows[0].detail[0].text; got != "子 偽の行" {
		t.Errorf("detail の改行が残った (畳まれた行も Enter で開くと描かれる): %q", got)
	}
	if got := rows[0].copyText; got != "コピーは\n複数行が正常" {
		t.Errorf("コピー文の改行まで落ちた: %q", got)
	}
}

// jsonEscaped は文字列を JSON 文字列の中身 (引用符なし) にする。
func jsonEscaped(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b[1 : len(b)-1])
}

// docker の走査結果はすべて外部由来 — イメージ名はレジストリから来るし、コンテナ名・
// ボリューム名・ビルドキャッシュの Description は誰かが書いた文字列。
//
// 🚨 seam は **docker.Scan** (関門をプロデューサ側に置いてある)。ここを live 経路で通す。
func TestDoctorLiveDockerScanIsSanitized(t *testing.T) {
	v := testView(t)
	// 🚨 fixture は **JSON エスケープ**して埋める。docker の `--format json` は制御文字を
	// \u001b で出すので、生の ESC を JSON 文字列に置くと「実物では起きない形」を検査してしまう
	osc, clear := jsonEscaped(t, testenv.OSC52Seq), jsonEscaped(t, testenv.ClearSeq)
	v.deps.DockerOpts = func() docker.Options {
		return doctortest.FakeDockerOptions(`{"Images":[` +
			// ① 識別子が細工されている = **落とす** (書き換えて出すと `docker rmi` が別のものを指す)
			`{"ID":"sha256:abcabcabcabc","Repository":"ev` + osc + `il","Tag":"1","Containers":"0","Size":"2GB","UniqueSize":"2GB","CreatedAt":"2020-01-01 00:00:00 +0000 UTC"}],` +
			`"Containers":[{"ID":"c1","Names":"ok-web","State":"exited",` +
			// ② 識別子は正当で、表示だけの自由文が細工されている = **残して無害化する**
			`"Status":"Exited (0)` + clear + `","Image":"app` + osc + `:old","Size":"10MB","CreatedAt":"2020-01-01 00:00:00 +0000 UTC"}],` +
			`"Volumes":[{"Name":"ok_data","Links":"0","Size":"3GB"}],` +
			`"BuildCache":[{"ID":"bc1","InUse":"false","Size":"1GB","Description":"[2/3] COPY` + clear + ` . /app","LastUsedAt":"2020-01-01 00:00:00 +0000 UTC","CreatedAt":"2020-01-01 00:00:00 +0000 UTC"}]}`)
	}
	runCmds(t, v, v.open())
	if v.docker == nil || !v.docker.Installed || v.docker.Unavailable != "" {
		t.Fatalf("前提が作れていない: %+v", v.docker)
	}
	// 🚨 細工されたイメージ名は**書き換えて出さない**。repo:tag が識別子として読めないので
	// 短い ID へ落とし、提示コマンドも ID を指す (無害化した名前で出すと、攻撃者が
	// 無害化後の名前のイメージを別に置くだけで「別のものを消せ」と案内できる)
	var img docker.Group
	for _, g := range v.docker.Groups {
		if g.Kind == docker.KindImages {
			img = g
		}
	}
	if len(img.Items) != 1 || img.Items[0].Name != "abcabcabcabc" || img.Items[0].Command != "docker rmi -- abcabcabcabc" {
		t.Errorf("細工された repo:tag が提示に載った: %+v", img.Items)
	}

	v.tab = TabDocker
	// 全群を開いて内訳まで描く (畳んだままだと候補の行が検査対象から外れる)
	v.expanded = map[rowKey]bool{}
	for _, g := range v.docker.Groups {
		v.expanded[dockerGroupRowKey(string(g.Kind))] = true
	}
	for _, line := range strings.Split(testText(v, 60), "\n") {
		if testenv.HasTerminalControl(line) {
			t.Errorf("Docker タブの行に制御シーケンスが残った: %q", line)
		}
		if strings.Contains(line, "PWNED") {
			t.Errorf("OSC の中身が残った: %q", line)
		}
	}
	var sawItem bool
	for _, row := range doctorAllRows(v.rows) {
		if testenv.HasTerminalControl(row.text) {
			t.Errorf("row の text に制御シーケンスが残った: %q", row.text)
		}
		if testenv.HasControlExceptNewline(row.copyPath) || testenv.HasControlExceptNewline(row.copyText) {
			t.Errorf("row のコピーに制御シーケンスが残った: %q / %q", row.copyPath, row.copyText)
		}
		if strings.Contains(row.text, "ok-web") {
			sawItem = true
		}
	}
	// 🚨 候補の行が**実際に描かれている**ことを確かめる (描かれていなければ、上の assert は
	// 1 つも sink を守っていない)
	if !sawItem {
		t.Fatal("候補の行が描かれていない (検査対象が空)")
	}
}
