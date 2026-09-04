package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"doctor/disk"
	"doctor/svc"
	"glogx/issues"
	"glogx/usage"
)

// 外部由来の文字列が「無害化を通らずに端末へ出る」sink が残っていないことを、sink ごとに固定する。
// 敵対的レビュー (2026-08-05) が実際に見つけた素通し経路の回帰テスト。

const (
	csi8 = "\u009b" // 8bit CSI (C1)
	osc8 = "\u009d" // 8bit OSC (C1)
	st8  = "\u009c" // 8bit ST (C1)
)

// PR の枠に載るブランチ名は外部由来。git は ref に ASCII 制御文字を許さないが C1 は許すので、
// 8bit の OSC/CSI が実際に入ってくる (直上の Title は無害化していたのにここだけ抜けていた)。
func TestPRStatusBoxSanitizesBranchNames(t *testing.T) {
	o := newPRStatusOverlay()
	o.sha = "deadbeef"
	o.cache["deadbeef"] = &PRStatus{
		PRRef: PRRef{Number: 12, State: "OPEN"}, Title: "PR タイトル",
		HeadRefName: "feat/" + osc8 + "0;PWNED" + st8 + "x",
		BaseRefName: "master" + csi8 + "2J",
	}
	for _, line := range o.boxLines(80, false, "⠋", "") {
		if hasTerminalControl(line) {
			t.Errorf("PR の枠に制御シーケンスが残った: %q", line)
		}
		if strings.Contains(line, "PWNED") {
			t.Errorf("OSC の中身が残った: %q", line)
		}
	}
}

// X の確認モーダルに載るパスは git status 由来 (POSIX ファイル名は制御文字を許す)。一覧行と
// pager タイトルは dispPath で無害化していたのに、破壊操作の確認画面だけが raw だった回帰テスト。
func TestDiscardBoxSanitizesPath(t *testing.T) {
	v := newStatusView()
	v.discarding = true
	v.discard = worktreeRow{
		section: sectionUnstaged, code: 'M', x: ' ', y: 'M',
		path: "notes" + osc8 + "0;PWNED" + st8 + ".txt",
		orig: "old" + csi8 + "2J.txt",
	}
	for _, line := range v.discardBox(statusRenderOpts{width: 80, page: 20}) {
		if hasTerminalControl(line) {
			t.Errorf("discard 確認モーダルに制御シーケンスが残った: %q", line)
		}
		if strings.Contains(line, "PWNED") {
			t.Errorf("OSC の中身が残った: %q", line)
		}
	}
}

// n の確認モーダルに載る issue ファイル名は issues/ 直下の実ファイル名 (Rel は同一性のため
// 生のまま保持される契約)。表示に出すここで無害化する回帰テスト。
func TestMarkNextBoxSanitizesFilename(t *testing.T) {
	v := &issuesView{}
	v.markNext = issuesMarkConfirm{active: true, targets: []*issues.Issue{
		{Rel: "036-bug-" + osc8 + "0;PWNED" + st8 + csi8 + "2J.md"},
	}}
	for _, line := range v.markNextBox(80, false) {
		if hasTerminalControl(line) {
			t.Errorf("markNext 確認モーダルに制御シーケンスが残った: %q", line)
		}
		if strings.Contains(line, "PWNED") {
			t.Errorf("OSC の中身が残った: %q", line)
		}
	}
}

// usage の枠タイトルに載る CLI バージョンは外部バイナリの出力。
func TestUsageBoxSanitizesVersion(t *testing.T) {
	o := usageOverlay{
		visible: true,
		snap: &usage.Snapshot{
			Version: "1.2.3\a" + osc8 + "0;PWNED" + st8,
			Windows: []usage.Window{{Label: "5h", Percent: 20}},
		},
	}
	for _, line := range o.boxLines(80, false, "⠋") {
		if hasTerminalControl(line) {
			t.Errorf("usage の枠に制御シーケンスが残った: %q", line)
		}
		if strings.Contains(line, "PWNED") {
			t.Errorf("OSC の中身が残った: %q", line)
		}
	}
}

// トーストの文言も表示 sink。gh / git のエラー出力や claude のバージョン文字列を素で
// 埋め込む呼び出しが多いので、setNotice と同じく show 自体を関門にする。
// lastWarning は w でクリップボードへ出るため、そちらも無害化する。
func TestToastAndWarningAreSanitized(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.showWarning("push に失敗: fatal" + osc8 + "0;PWNED" + st8 + " remote rejected")
	if hasTerminalControl(m.toast.text) {
		t.Errorf("トーストに制御シーケンスが残った: %q", m.toast.text)
	}
	if hasTerminalControl(m.lastWarning) {
		t.Errorf("コピー対象の警告に制御シーケンスが残った: %q", m.lastWarning)
	}
	if strings.Contains(m.lastWarning, "PWNED") {
		t.Errorf("OSC の中身が残った: %q", m.lastWarning)
	}
}

// 色なしモードでは外部由来の SGR も枠へ通さない。
//
// paint も scrollbarColumn も colored=false のとき reset を出さないので、閉じていない SGR が
// 1 行でもあると padding・枠・スクロールバー列・後続行まで属性が続く (NO_COLOR 起動時に
// 「以降が全部消える」形の画面破壊になる)。
func TestPanelBoxDropsANSIWhenNotColored(t *testing.T) {
	rows := []string{"\x1b[41;30m閉じていない SGR", "無関係な次の行"}
	for _, line := range buildShadowPanelBox(" title ", rows, 40, false, ansiDim) {
		if strings.Contains(line, "\x1b") {
			t.Errorf("色なしモードなのに ANSI が枠へ出た: %q", line)
		}
	}
	// 色ありモードでは従来どおり通す (色を出すのが仕事なので落とさない)
	got := strings.Join(buildShadowPanelBox(" title ", rows, 40, true, ansiDim), "\n")
	if !strings.Contains(got, "\x1b[41;30m") {
		t.Errorf("色ありモードで外部の SGR まで落とした: %q", got)
	}
}

// usage の **codex 枠の Label** はキャッシュ由来 (`~/.cache/glog/claude-usage.json` は一般ユーザー
// 権限で書き換えられる)。live の取得経路は安全だが、codex 枠は allowlist ではなく Source で拾うので、
// キャッシュに書かれた Label がそのまま 3 経路 (RenderLine / RenderTableGroups / RenderDashboard) へ出る。
// 🚨 fixture は **codex 枠**で作ること: Claude 枠だと defaultOrder の allowlist に阻まれて
// 退行しても最初から不可視になり、何も守らないテストになる (issue 230)。
func TestUsageCacheSanitizesCodexLabel(t *testing.T) {
	stubLookPath(t, nil)
	path := filepath.Join(t.TempDir(), usageCacheFile)
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.Local)
	snap := usageSnapFixture(t)
	snap.Windows = append(snap.Windows, usage.Window{
		Label:   "cx7d\a" + osc8 + "0;PWNED" + st8,
		Percent: 20, Source: usage.SourceCodex, ResetAt: now.Add(time.Hour),
	})
	if err := saveUsageCache(path, snap, now); err != nil {
		t.Fatal(err)
	}
	got, ok := loadUsageCache(path, now)
	if !ok {
		t.Fatal("キャッシュを読めない (前提が違う)")
	}
	line := usage.RenderLine(got, now, false)
	if hasTerminalControl(line) {
		t.Errorf("RenderLine に制御シーケンスが残った: %q", line)
	}
	if strings.Contains(line, "PWNED") {
		t.Errorf("OSC の中身が残った: %q", line)
	}
	header, rows := usage.RenderTable(got, now, false)
	for _, l := range append(rows, header) {
		if hasTerminalControl(l) {
			t.Errorf("RenderTable に制御シーケンスが残った: %q", l)
		}
	}
}

// --- doctor (issue 228) ---
//
// doctor は「走査したて (live)」と「保存から復元」の 2 経路を持つが、無害化は復元側にしか
// 無かった。この節は **live 経路**を固定する。fixture は実際に制御文字入りのディレクトリを
// 作って走査させる: 復元経路からしか制御文字を入れないテストは、今回の穴 (live) を
// **構造的に一度も踏まない**まま緑になる (既存の TestDoctorSnapshotTrustBoundaryFreeText が
// まさにその形だった)。

const (
	escSeq   = "\x1b"
	belSeq   = "\a"
	osc52Seq = escSeq + "]52;c;cHduZWQ=" + belSeq // クリップボード書き込み (貼った先で発火する)
	clearSeq = escSeq + "[2J"                     // 画面消去
)

// hasControlExceptNewline は「1 件が複数行の塊」用 (コピー文は改行が正常)。
func hasControlExceptNewline(s string) bool {
	return hasTerminalControl(strings.ReplaceAll(s, "\n", ""))
}

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
	evilDir := filepath.Join(root, "ev"+osc52Seq+"il")
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
	if err := os.WriteFile(filepath.Join(okDir, "child"+clearSeq+"name"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := &doctorView{
		diskOpts: func() disk.Options {
			return disk.Options{
				Env: disk.Env{Home: home, TmpDir: home + "/", Getenv: func(string) string { return "" }},
				Run: func(context.Context, string, ...string) (string, string, int, error) { return "", "", 0, nil },
				Catalog: []disk.Entry{{ID: "evil", Label: "細工されたキャッシュ", Tier: 1, Risk: disk.RiskSafe,
					DeleteVia: "rm", Recover: "再生成されます", Inspect: true,
					Paths: []string{"~/Library/Caches/evil/*"}}},
				BootTime: func() (time.Time, error) { return time.Now(), nil },
			}
		},
		svcOpts: func() svc.Options {
			return svc.Options{Dirs: []svc.LaunchDir{{Path: filepath.Join(home, "LaunchAgents"), Domain: "gui/501"}},
				Run: func(context.Context, string, ...string) (string, string, int, error) {
					return "PID\tStatus\tLabel\n", "", 0, nil
				}}
		},
		brewRun: func(context.Context, string, ...string) (string, string, int, error) { return "", "", 0, nil },
	}
	runDoctorCmds(t, v, v.open())

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
	for _, line := range strings.Split(doctorText(v, 60), "\n") {
		if hasTerminalControl(line) {
			t.Errorf("doctor の行に制御シーケンスが残った: %q", line)
		}
	}
	for _, row := range doctorAllRows(v.rows) {
		if hasTerminalControl(row.text) {
			t.Errorf("row の text に制御シーケンスが残った: %q", row.text)
		}
		if hasControlExceptNewline(row.copyPath) {
			t.Errorf("y のコピー (pbcopy へ生で渡る) に制御シーケンスが残った: %q", row.copyPath)
		}
		if hasControlExceptNewline(row.copyText) {
			t.Errorf("Y のコピー (pbcopy へ生で渡る) に制御シーケンスが残った: %q", row.copyText)
		}
	}
}

// svc (plist の Label) / brew (警告本文) も live 経路で無害化する。
// 🚨 seam は Msg の受け口 (走査結果がモデルへ入る場所)。ここを通らない値は画面にもコピーにも
// 出ない、という形にしてある。
func TestDoctorLiveSvcAndBrewAreSanitized(t *testing.T) {
	v := doctorTestView(t)
	v.shown, v.gen = true, 1
	v.receiveSvc(doctorSvcMsg{gen: 1, rep: svc.Report{
		Scanned: 2,
		Findings: []svc.Finding{{
			// ① 識別子が細工されている = **落とす** (書き換えて出すと、提示する
			// `launchctl bootout` / `rm` が別のファイルを指す)
			Label: "com.evil" + osc52Seq, PlistPath: "/Library/LaunchAgents/x" + clearSeq + ".plist",
			Domain: "gui/501",
		}, {
			// ② 識別子は正当で、表示だけの自由文が細工されている = **残して無害化する**
			Label: "com.ok", PlistPath: "/Library/LaunchAgents/ok.plist", Domain: "gui/501",
			Reasons:     []string{"実行ファイルが無い\n偽の行" + clearSeq},
			MissingExec: "/usr/local/bin/gone" + clearSeq,
			BrewFormula: "formula" + osc52Seq,
			RestartKeys: []string{"KeepAlive" + clearSeq},
			Commands:    []string{"launchctl bootout gui/501/com.ok" + clearSeq},
		}},
		Undiagnosed: []svc.Undiagnosed{{PlistPath: "/x" + osc52Seq + ".plist", Reason: "壊れ" + clearSeq + "た"}},
		StatusErr:   "launchctl 失敗" + clearSeq,
		BrewErr:     "brew 失敗" + osc52Seq,
		DirErrs:     []string{"読めず" + clearSeq},
	}})
	// 🚨 Warnings と Unavailable を**同時に立てない**。brewSection は Unavailable != "" で
	// 早期 return するので、両方立てると警告に触れる row が 0 個になり、Warnings 側の
	// 無害化を外しても緑のまま通る (敵対レビュー 2026-09-04 が実測した false green)。
	v.receiveBrew(doctorBrewMsg{gen: 1, res: brewDoctorResult{
		Warnings: []string{"Warning: 危険" + osc52Seq + "\n2 行目は正常" + clearSeq},
	}})

	for _, line := range strings.Split(doctorText(v, 60), "\n") {
		if hasTerminalControl(line) {
			t.Errorf("doctor の行に制御シーケンスが残った: %q", line)
		}
	}
	for _, row := range doctorAllRows(v.rows) {
		if hasTerminalControl(row.text) {
			t.Errorf("row の text に制御シーケンスが残った: %q", row.text)
		}
		if hasControlExceptNewline(row.copyPath) || hasControlExceptNewline(row.copyText) {
			t.Errorf("row のコピーに制御シーケンスが残った: %q / %q", row.copyPath, row.copyText)
		}
	}
	// brew の警告は**複数行が正常**なので改行は残す (1 行に潰すと段落が読めなくなる)
	if v.brew == nil || len(v.brew.Warnings) != 1 || !strings.Contains(v.brew.Warnings[0], "\n") {
		t.Fatalf("brew の改行まで落ちた: %+v", v.brew)
	}
	if hasControlExceptNewline(v.brew.Warnings[0]) {
		t.Errorf("brew の警告本文に制御シーケンスが残った: %q", v.brew.Warnings[0])
	}
	// 🚨 警告本文が**実際に row として描かれている**ことを確かめる (描かれていなければ
	// 上の assert は「モデルの値」しか見ておらず、表示 sink を 1 つも守っていない)
	var sawWarning bool
	for _, row := range doctorAllRows(v.rows) {
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
		if hasTerminalControl(strings.ReplaceAll(v, "\x00", "")) {
			t.Errorf("%s に制御シーケンスが残った: %q", name, v)
		}
	}
}

// brew の Unavailable は**1 行**の row として描かれるので、改行を残す PlainBlock で通しては
// いけない (固定高パネルの行数が狂う)。
func TestDoctorBrewUnavailableIsSingleLine(t *testing.T) {
	v := doctorTestView(t)
	v.shown, v.gen = true, 1
	v.receiveBrew(doctorBrewMsg{gen: 1, res: brewDoctorResult{
		Unavailable: "brew 無し" + clearSeq + "\n ⛔ 偽の行 (これは brew の出力ではない)",
	}})
	if v.brew == nil || strings.Contains(v.brew.Unavailable, "\n") {
		t.Errorf("1 行として描く値に改行が残った: %q", v.brew.Unavailable)
	}
	const page = 20
	lines := v.lines(doctorTestOpts(page))
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
	v := doctorTestView(t)
	v.shown, v.gen = true, 1
	v.del.running = true

	v.receiveDelete(doctorDeleteMsg{gen: 1, ev: doctorDeleteEvent{
		prog: &doctorProgress{i: 2, total: 3, label: "細工されたラベル" + clearSeq, known: true},
	}})
	v.receiveDelete(doctorDeleteMsg{gen: 1, ev: doctorDeleteEvent{cmd: &disk.CommandRecord{
		Name: "xcrun" + clearSeq, Args: []string{"simctl", "delete", "id" + clearSeq},
		RC: 0, Stdout: "消しました" + osc52Seq + "\n2 行目", Stderr: "警告" + clearSeq,
		Err: "起動できず" + osc52Seq,
	}}})
	v.receiveDelete(doctorDeleteMsg{gen: 1, ev: doctorDeleteEvent{rep: &disk.DeleteReport{
		HistoryPath: "/tmp/hist" + clearSeq + ".json",
		Entries: []disk.EntryOutcome{{
			ID: "evil", Label: "細工" + osc52Seq, Outcome: disk.OutcomeDeleted,
			Reason: "理由" + clearSeq,
			Items: []disk.ItemOutcome{{
				Path: "/tmp/ev" + osc52Seq + "il", Outcome: disk.OutcomeDeleted, Reason: "消した" + clearSeq,
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
		if hasTerminalControl(l) {
			t.Errorf("実行の記録に制御シーケンスが残った: %q", l)
		}
	}
	// パネル (画面) と y のコピーの両方
	for _, line := range v.lines(doctorTestOpts(40)) {
		if hasTerminalControl(line) {
			t.Errorf("削除パネルに制御シーケンスが残った: %q", line)
		}
	}
	if hasControlExceptNewline(v.deleteLogText()) {
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
	v := doctorTestView(t)
	v.shown, v.gen = true, 1
	v.receiveDisk(doctorDiskMsg{gen: 1, ev: doctorDiskEvent{r: &disk.Result{
		Entry: disk.Entry{
			ID: "thing", Label: "ラベル" + osc52Seq, Risk: disk.Risk("safe" + clearSeq),
			Recover: "戻せます" + clearSeq, Detail: "補足" + osc52Seq,
			DeleteVia: "rm" + clearSeq, Unverified: "未実測" + clearSeq,
		},
		Status: disk.StatusOK, Size: 8192,
		// 🚨 2 本目は**生の不正 UTF-8 バイト** (0x9b = 8bit CSI)。`unicode.IsPrint` は
		// U+FFFD を印字可能と答えるので、自前の走査だとこれを通していた
		Items: []disk.Item{{Path: "/tmp/ok", Size: 4096, Ref: "id" + clearSeq},
			{Path: "/tmp/raw\x9bbyte", Size: 4096}},
		Reason:   "理由" + osc52Seq,
		Failures: []string{"読めず" + clearSeq},
		Contents: []string{"child" + osc52Seq},
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
	for _, line := range strings.Split(doctorText(v, 60), "\n") {
		if hasTerminalControl(line) {
			t.Errorf("doctor の行に制御シーケンスが残った: %q", line)
		}
	}
	if len(v.rows) == 0 {
		t.Fatal("row が 1 つも組まれていない (以下の検査が空回りする)")
	}
	for _, row := range doctorAllRows(v.rows) {
		if hasTerminalControl(row.text) {
			t.Errorf("row の text に制御シーケンスが残った: %q", row.text)
		}
	}

	// 🚨 **落とす対象が 1 件も無いケース**も見る。無害化した Items の反映を
	// `if dropped > 0` の中でだけ行っていた実装は、上の fixture (1 件落ちる) では検出できない
	// — 落ちた瞬間に反映されるので緑になる (敵対レビュー 2026-09-04 の P3 を、当初のテストは
	// 素通りさせた)。
	v2 := doctorTestView(t)
	v2.shown, v2.gen = true, 1
	v2.receiveDisk(doctorDiskMsg{gen: 1, ev: doctorDiskEvent{r: &disk.Result{
		Entry: disk.Entry{ID: "thing", Label: "ラベル"}, Status: disk.StatusOK, Size: 4096,
		Items: []disk.Item{{Path: "/tmp/ok", Size: 4096, Ref: "id" + clearSeq}},
	}}})
	if len(v2.diskResults) != 1 || len(v2.diskResults[0].Items) != 1 {
		t.Fatalf("前提が作れていない: %+v", v2.diskResults)
	}
	if got := v2.diskResults[0].Items[0].Ref; hasTerminalControl(got) {
		t.Errorf("落とす対象が無いときに Ref の無害化が捨てられている: %q", got)
	}
}

// 保存から復元する経路も同じ関門を通る。
// 🚨 **Entry は復元では JSON 由来 = 攻撃者が決められる** (live ではカタログの写しなので無害)。
// doctorRiskMark は `string(Entry.Risk)` を、行は Label / Recover をそのまま描く。
// 🚨 svc の PlistPath は `validPlistPath` が文字種を絞らないので、以前はここだけ素通りしていた。
func TestDoctorSnapshotRestoreSanitizesEntryAndPlistPath(t *testing.T) {
	v := doctorTestView(t)
	now := time.Now()
	writeDoctorSnapshot(t, doctorSnapshot{ScannedAt: now.Add(-time.Minute),
		Disk: disk.Report{Results: []disk.Result{{
			Entry: disk.Entry{
				ID: "thing", Label: "ラベル" + osc52Seq, Risk: disk.Risk("safe" + clearSeq),
				Recover: "戻せます" + clearSeq, Detail: "補足" + osc52Seq,
				DeleteVia: "rm" + clearSeq, Unverified: "未実測" + clearSeq,
			},
			Status: disk.StatusOK, Size: 4096, MeasuredAt: now.Add(-time.Minute),
			Items: []disk.Item{{Path: "/ok", Size: 4096}},
		}}},
		Svc: svc.Report{
			Findings: []svc.Finding{{
				Label: "com.ok", Domain: "gui/501",
				PlistPath: "/Library/LaunchAgents/a" + osc52Seq + "b.plist",
			}},
			Undiagnosed: []svc.Undiagnosed{{
				PlistPath: "/Library/LaunchAgents/u" + clearSeq + ".plist", Reason: "壊れた",
			}},
		},
	})
	if cmd := v.open(); cmd != nil {
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
	for _, line := range strings.Split(doctorText(v, 60), "\n") { // rows を組ませる (下の検査の前提)
		if hasTerminalControl(line) {
			t.Errorf("doctor の行に制御シーケンスが残った: %q", line)
		}
	}
	if len(v.rows) == 0 {
		t.Fatal("row が 1 つも組まれていない (以下の検査が空回りする)")
	}
	for _, row := range doctorAllRows(v.rows) {
		if hasTerminalControl(row.text) {
			t.Errorf("row の text に制御シーケンスが残った: %q", row.text)
		}
		if hasControlExceptNewline(row.copyPath) || hasControlExceptNewline(row.copyText) {
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
