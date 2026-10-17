package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"glogx/internal/term"
	"glogx/issues"
	"ratelimit/usage"
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
	assertSanitizedAndVisible(t, "PR の枠", o.boxLines(80, false, "⠋", ""), "feat/", "master")
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
	assertSanitizedAndVisible(t, "discard 確認モーダル",
		v.discardBox(statusRenderOpts{width: 80, page: 20}), "notes", ".txt")
}

// n の確認モーダルに載る issue ファイル名は issues/ 直下の実ファイル名 (Rel は同一性のため
// 生のまま保持される契約)。表示に出すここで無害化する回帰テスト。
func TestMarkNextBoxSanitizesFilename(t *testing.T) {
	v := &issuesView{}
	v.markNext = issuesMarkConfirm{active: true, targets: []*issues.Issue{
		{Rel: "036-bug-" + osc8 + "0;PWNED" + st8 + csi8 + "2J.md"},
	}}
	assertSanitizedAndVisible(t, "markNext 確認モーダル", v.markNextBox(80, false), "036-bug-", ".md")
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
	assertSanitizedAndVisible(t, "usage の枠", o.boxLines(80, false, "⠋"), "1.2.3")
}

// トーストの文言も表示 sink。gh / git のエラー出力や claude のバージョン文字列を素で
// 埋め込む呼び出しが多いので、setNotice と同じく show 自体を関門にする。
// lastWarning は w でクリップボードへ出るため、そちらも無害化する。
func TestToastAndWarningAreSanitized(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	m.showWarning("push に失敗: fatal" + osc8 + "0;PWNED" + st8 + " remote rejected")
	if hasTerminalControl(m.toast.Text()) {
		t.Errorf("トーストに制御シーケンスが残った: %q", m.toast.Text())
	}
	if hasTerminalControl(m.lastWarning) {
		t.Errorf("コピー対象の警告に制御シーケンスが残った: %q", m.lastWarning)
	}
	if strings.Contains(m.lastWarning, "PWNED") {
		t.Errorf("OSC の中身が残った: %q", m.lastWarning)
	}
	// 陽性対照 (issue 284): 細工した文言が実際に保持されている。無害化が「丸ごと捨てる」形へ
	// 退行すると上の 3 つの assert は全部素通りする。
	for _, want := range []string{"push に失敗", "remote rejected"} {
		if !strings.Contains(m.lastWarning, want) {
			t.Fatalf("警告に %q が残っていない: %q (検査対象が空)", want, m.lastWarning)
		}
		if !strings.Contains(m.toast.Text(), want) {
			t.Fatalf("トーストに %q が残っていない: %q (検査対象が空)", want, m.toast.Text())
		}
	}
}

// 色なしモードでは外部由来の SGR も枠へ通さない。
//
// term.Paint も scrollbarColumn も colored=false のとき reset を出さないので、閉じていない SGR が
// 1 行でもあると padding・枠・スクロールバー列・後続行まで属性が続く (NO_COLOR 起動時に
// 「以降が全部消える」形の画面破壊になる)。
func TestPanelBoxDropsANSIWhenNotColored(t *testing.T) {
	rows := []string{"\x1b[41;30m閉じていない SGR", "無関係な次の行"}
	for _, line := range term.BuildShadowPanelBox(" title ", rows, 40, false) {
		if strings.Contains(line, "\x1b") {
			t.Errorf("色なしモードなのに ANSI が枠へ出た: %q", line)
		}
	}
	// 色ありモードでは従来どおり通す (色を出すのが仕事なので落とさない)
	got := strings.Join(term.BuildShadowPanelBox(" title ", rows, 40, true), "\n")
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
	// 🚨 **観測面は production 到達可能な 2 経路にする** (issue 317)。
	// 以前は `RenderLine` / `RenderTable` を見ていたが、実測 (2026-09-06 / 2026-09-08 に再確認)
	// で **どちらも production 呼び出し 0 件**だった。入口 (loadUsageCache) のサニタイズを
	// 外す変異はそれでも red になる (どの出口から見ても汚染が見えるため。2026-09-08 実測) が、
	// **live 経路そのものが Label を別の形で出すようになった退行**は到達不能な面からは見えない。
	// 到達可能な面で観測しておけば、その両方が同じテストで閉じる。
	//
	//	RenderTableGroups … usage_overlay.go / usage/render.go 内部 (2 件)
	//	RenderDashboard   … ratelimit_dashboard.go / tools/dial-preview (2 件)
	assertNoTerminalControl := func(where string, lines ...string) {
		t.Helper()
		for _, l := range lines {
			if hasTerminalControl(l) {
				t.Errorf("%s に制御シーケンスが残った: %q", where, l)
			}
			if strings.Contains(l, "PWNED") {
				t.Errorf("%s に OSC の中身が残った: %q", where, l)
			}
		}
	}
	header, groups := usage.RenderTableGroups(got, now, false)
	rows := []string{header}
	for _, g := range groups {
		rows = append(rows, g...)
	}
	assertNoTerminalControl("RenderTableGroups", rows...)
	assertNoTerminalControl("RenderDashboard", usage.RenderDashboard(got, now, 120, 40, false)...)

	// 🚨 **観測面が汚染ラベルを実際に描いていることを固定する** (positive control)。
	// これが無いと「Label をどこにも出さない描画」に変えた瞬間、無害化が外れても緑になる
	// (issue 284 と同型)。サニタイズ後に残るのは制御文字を落とした "cx7d" の部分。
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "cx7d") {
		t.Fatalf("RenderTableGroups が codex のラベルを描いていない。観測面が的を外している:\n%s", joined)
	}
}

// assertSanitizedAndVisible は無害化テストの 2 つの主張を対で確かめる (issue 284):
//
//  1. 制御シーケンス / OSC の中身が残っていない (本来の主張)
//  2. 🚨 **細工した値が実際に描かれている** (陽性対照)
//
// 2 が無いと、箱が空を返した瞬間に 1 の assert が 1 つも実行されず vacuous に緑を返す
// (実測: markNextBox の先頭に `return nil` を入れても PASS した)。
// 同じ罠は TestDoctorLiveDockerScanIsSanitized が sawItem で 1 本だけ塞いでいたもの。
func assertSanitizedAndVisible(t *testing.T, where string, lines []string, wantVisible ...string) {
	t.Helper()
	for _, line := range lines {
		if hasTerminalControl(line) {
			t.Errorf("%s に制御シーケンスが残った: %q", where, line)
		}
		if strings.Contains(line, "PWNED") {
			t.Errorf("%s に OSC の中身が残った: %q", where, line)
		}
	}
	joined := strings.Join(lines, "\n")
	for _, w := range wantVisible {
		if !strings.Contains(joined, w) {
			t.Fatalf("%s に %q が描かれていない (検査対象が空 = 上の assert は 1 つも sink を守っていない)",
				where, w)
		}
	}
}

// issue 417: 外部由来の文字列が termsafe を通らずに画面 / stderr へ届いていた 4 経路。
// どれも表示にしか使わない値なので、受け取った時点で平文にしている。
const evil417 = "\x1b]52;c;cHduZWQ=\a\u009b2J\u202eevil"

func assertNoTermControl(t *testing.T, what, got string) {
	t.Helper()
	if strings.ContainsAny(got, "\x1b\a\u009b\u202e") {
		t.Errorf("%s に制御シーケンスが残っている: %q", what, got)
	}
	if !strings.Contains(got, "evil") {
		t.Errorf("%s の本文まで落ちている: %q", what, got)
	}
}

func TestGHWarningSanitizesStderr(t *testing.T) {
	for _, k := range []GHErrorKind{GHRateLimited, GHOther} {
		assertNoTermControl(t, "GHError.Warning", (&GHError{Kind: k, Detail: "gh: " + evil417}).Warning())
	}
}

func TestStatusBranchHeaderIsSanitized(t *testing.T) {
	branch, track := parseBranchHeader("## main" + evil417 + "...origin/main [ahead 1" + evil417 + "]")
	assertNoTermControl(t, "branch", branch)
	assertNoTermControl(t, "track", track)
}

func TestStatusErrorsAreSanitized(t *testing.T) {
	v := newTestStatusView(t, "")
	v.receive(statusLoadMsg{err: errors.New("fatal: " + evil417), gen: v.gen})
	assertNoTermControl(t, "git status のエラー", v.err)

	v.preview.begin("k")
	v.receivePreview(statusPreviewMsg{gen: v.gen, key: "k", err: errors.New("open " + evil417 + ".txt: permission denied")})
	lines, ok := v.preview.get("k")
	if !ok || len(lines) != 1 {
		t.Fatalf("プレビューのエラーが保存されていない: ok=%v lines=%q", ok, lines)
	}
	assertNoTermControl(t, "プレビューのエラー", lines[0])
}
