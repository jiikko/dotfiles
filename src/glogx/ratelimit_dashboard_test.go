package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"glogx/internal/app"
	"glogx/internal/gitdata"
	"glogx/internal/term"
	"glogx/internal/testenv"
	"glogx/internal/usageview"
	"ratelimit/usage"
)

// ダッシュボード ⇄ viewer の往復 (ダッシュボードの i/s と viewer の R。ユーザー要望 2026-09-01)。
// 全画面は同時に 1 枚なので、横断のたびに「相手が開く」と「自分が閉じる」の両方を見る。
func TestRatelimitDashCrossSwitching(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
	m.handleKey("R")
	if !m.rlDash.Visible() {
		t.Fatal("前提が崩れた: R でダッシュボードが開かない")
	}
	releaseKey(m)
	m.handleKey("i")
	if m.rlDash.Visible() || !m.issuesOv.Visible() {
		t.Fatalf("ダッシュボードの i で issues viewer へ移らない (dash=%v issues=%v)", m.rlDash.Visible(), m.issuesOv.Visible())
	}
	releaseKey(m)
	m.handleKey("R")
	if !m.rlDash.Visible() || m.issuesOv.Visible() {
		t.Fatalf("issues viewer の R でダッシュボードへ移らない (dash=%v issues=%v)", m.rlDash.Visible(), m.issuesOv.Visible())
	}
	releaseKey(m)
	m.handleKey("s")
	if m.rlDash.Visible() || !m.statusOv.Visible() {
		t.Fatalf("ダッシュボードの s で status viewer へ移らない (dash=%v status=%v)", m.rlDash.Visible(), m.statusOv.Visible())
	}
	releaseKey(m)
	m.handleKey("R")
	if !m.rlDash.Visible() || m.statusOv.Visible() {
		t.Fatalf("status viewer の R でダッシュボードへ移らない (dash=%v status=%v)", m.rlDash.Visible(), m.statusOv.Visible())
	}
}

// 起動時 restore が返る前に R が押されていたら復元を捨てる (status viewer と同じ理由。
// 捨てないと裏に issues viewer が開き、ダッシュボードの i が toggle で「閉じる」に化ける)。
func TestIssuesRestoreDroppedWhenRatelimitDashOpen(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
	m.handleKey("R")
	m.Update(issuesRestoreMsg{})
	if m.issuesOv.Visible() {
		t.Fatal("ダッシュボード表示中の遅延 restore で issues viewer が開いた")
	}
	if !m.rlDash.Visible() {
		t.Fatal("前提が崩れた: ダッシュボードが開いていない")
	}
	// 横断が toggle で裏返らないこと (この restore ガードが守っている本体)
	releaseKey(m)
	m.handleKey("i")
	if !m.issuesOv.Visible() {
		t.Fatal("復元を捨てた後の i で issues viewer が開かない")
	}
}

// 1 分ごとの再取得は「右上オーバーレイ」か「全画面ダッシュボード」のどちらかが見えていれば回る。
// ダッシュボードを外すと、開いている間だけ値が固まる (毎分更新の契約が静かに壊れる)。
func TestWantsUsageRefresh(t *testing.T) {
	var m browseModel
	if m.wantsUsageRefresh() {
		t.Error("どちらも非表示なのに再取得する")
	}
	m.usageOv.Fixture().SetVisible(true)
	if !m.wantsUsageRefresh() {
		t.Error("usage オーバーレイ表示中に再取得しない")
	}
	m.usageOv.Fixture().SetVisible(false)
	m.rlDash.Toggle()
	if !m.wantsUsageRefresh() {
		t.Error("ダッシュボード表示中に再取得しない")
	}
}

// ダッシュボードが取得待ちのあいだはスピナーの tick を回す (右上オーバーレイの loading()
// は自分の表示状態しか見ないので、ダッシュボード単独では止まってしまう)。
func TestRLDashLoading(t *testing.T) {
	var m browseModel
	if m.rlDashLoading() {
		t.Error("非表示なのに loading")
	}
	m.rlDash.Toggle()
	if !m.rlDashLoading() {
		t.Error("取得前なのに loading でない")
	}
	m.usageOv.Fixture().SetSnapshot(testenv.RatelimitTestSnap())
	if m.rlDashLoading() {
		t.Error("取得済みなのに loading")
	}
}

// 片方だけ届いた最初の取得では、届いた分の盤を描き、まだの出所を見出しの下に「取得中」と出す。
// 盤の描画キャッシュは「取得中」の有無で外れる (同じ snap のまま待ちが終わっても行が残らない)。
func TestRatelimitDashShowsWaitingSource(t *testing.T) {
	var m browseModel
	m.rlDash.Toggle()
	m.usageOv.FetchCmd(false)
	defer m.usageOv.Stop()
	m.usageOv.Handle(usageview.FixtureUsageMsg(nil, nil, &usage.Part{Source: usage.SourceCodex,
		Windows: []usage.Window{{Label: "cx7d", Source: usage.SourceCodex, Percent: 7, WindowMins: 10080, ResetAt: app.TimeNow().Add(time.Hour)}}}))
	if !m.rlDashLoading() {
		t.Error("Claude を待っている間にスピナーが止まる")
	}
	o := m.ratelimitOpts()
	if o.Waiting == "" {
		t.Fatal("ratelimitOpts が待っている出所を渡さない")
	}
	o.Width, o.Page = 100, 30 // ゼロ値の browseModel は窓の寸法を持たない
	if got := term.StripANSI(m.rlDash.Lines(o)[1]); !strings.Contains(got, "Claude Code 取得中...") {
		t.Errorf("見出しの下に待っている出所が無い: %q", got)
	}
	// 盤の本体は Claude の 2 枚を、スピナー付きの場所取りのカードで取る
	if all := term.StripANSI(strings.Join(m.rlDash.Lines(o), "\n")); strings.Count(all, m.spinner()+" 取得中...") != 2 {
		t.Errorf("盤が場所取りのカードで Claude の段を取っていない:\n%s", all)
	}
	o.Waiting = ""
	if got := term.StripANSI(m.rlDash.Lines(o)[1]); strings.Contains(got, "取得中") {
		t.Errorf("待ちが終わっても描画キャッシュの「取得中」が残った: %q", got)
	}
}

// ratelimitOpts は usage の取得の失敗の注記を組んでダッシュボードへ渡す (ratelimitview は usageview を import しないので、
// main が渡し忘れると注記が黙って消える)。
func TestRatelimitOptsPassesFetchNote(t *testing.T) {
	var m browseModel
	m.rlDash.Toggle()
	snap := testenv.RatelimitTestSnap()
	snap.ClaudeErr = "claude /usage 実行失敗: exit status 127"
	m.usageOv.Fixture().SetSnapshot(snap)
	o := m.ratelimitOpts()
	if !strings.Contains(o.Note, "Claude Code の取得に失敗") {
		t.Fatalf("ratelimitOpts が取得の失敗の注記を渡さない: %q", o.Note)
	}
	o.Width, o.Page = 100, 30 // ゼロ値の browseModel は窓の寸法を持たない
	if got := term.StripANSI(m.rlDash.Lines(o)[1]); !strings.Contains(got, "exit status 127") {
		t.Errorf("見出しの下に取得の失敗の注記が無い: %q", got)
	}
}

// 何も届いていない最初の取得でも、盤は場所取りのカードで届いた後と同じ段を取る (中央の 1 行の「取得中...」にしない)。
// 初回の全滅の後の取り直しは「取得失敗」のまま (場所取りで隠さない)。
func TestRatelimitDashPlaceholdersBeforeAnyArrives(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PATH", "")
	testenv.StubLookPath(t, map[string]string{"codex": "/fake/codex"}) // codex あり・前回のキャッシュ無し = codex の場所取りは既定の 1 枠
	var m browseModel
	m.rlDash.Toggle()
	m.usageOv.FetchCmd(false)
	defer m.usageOv.Stop()
	o := m.ratelimitOpts()
	o.Width, o.Page = 100, 30
	all := term.StripANSI(strings.Join(m.rlDash.Lines(o), "\n"))
	if want := len(usage.PendingWindows("", nil)) + len(usage.PendingWindows(usage.SourceCodex, nil)); strings.Count(all, m.spinner()+" 取得中...") != want {
		t.Errorf("何も届いていない盤が場所取りのカード %d 枚になっていない:\n%s", want, all)
	}
	m.usageOv.Fixture().EndRound()
	m.usageOv.Fixture().SetErr(errors.New("初回の全滅"))
	m.usageOv.FetchCmd(false)
	o = m.ratelimitOpts()
	o.Width, o.Page = 100, 30
	if all := term.StripANSI(strings.Join(m.rlDash.Lines(o), "\n")); !strings.Contains(all, "取得失敗") || strings.Contains(all, "取得中...") {
		t.Errorf("初回の全滅の後の取り直しで「取得失敗」が場所取りに隠れた:\n%s", all)
	}
}

// R でダッシュボードが開き、開いている間は移動キーが裏の一覧へ届かない (全画面の契約)。
// 届くと、閉じたときにカーソルが知らない場所へ動いている。
func TestRatelimitDashKeyRouting(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
	m.handleKey("j")
	if m.cursor != 1 {
		t.Fatalf("前提: j でカーソルが動くこと (cursor=%d)", m.cursor)
	}
	m.handleKey("R")
	if !m.rlDash.Visible() {
		t.Fatal("R でダッシュボードが開かない")
	}
	m.handleKey("j")
	m.handleKey("G")
	if m.cursor != 1 {
		t.Errorf("ダッシュボード表示中に一覧が動いた: cursor=%d", m.cursor)
	}
	m.handleKey("q")
	if m.rlDash.Visible() {
		t.Error("q で閉じない")
	}
	m.handleKey("j")
	if m.cursor != 2 {
		t.Errorf("閉じた後に一覧が動かない: cursor=%d", m.cursor)
	}
}

// R を開くと右上の usage オーバーレイは引っ込む (同じ値を 2 か所に出さない)。
func TestRatelimitDashDismissesUsageOverlay(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
	m.usageOv.Fixture().SetVisible(true)
	m.usageOv.Fixture().SetSnapshot(testenv.RatelimitTestSnap())
	m.handleKey("R")
	if m.usageOv.Visible() {
		t.Error("ダッシュボードを開いても右上オーバーレイが残っている")
	}
}

// 全画面ダッシュボードは枠の内側 (中身の行と下辺) にだけ地色を敷き、枠の外 (上の余白・影・hint 行) は塗らない
// (影は近黒なので、黒地の上に落とすと見えない。ユーザー決定 2026-10-22)。
//
// 🚨 「端末の既定へ戻す」では足りない場面のための固定色。scratch popup は display-popup 自身が
// 濃紺を敷いており、その popup では既定の背景が濃紺になる (ユーザー要望 2026-09-01)。
func TestRatelimitDashPaintsScreenBackground(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
	m.colored = true
	m.showFrame, m.width, m.height = true, 100, 30
	m.handleKey("R")
	if !m.frameActive() {
		t.Fatal("前提: 枠を描く大きさ")
	}
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) < 10 {
		t.Fatalf("行が少なすぎる: %d", len(lines))
	}
	// 並び: 上の余白 / 上辺 / 中身 … / 下辺 / 影 / hint
	n := len(lines)
	for _, i := range []int{0, 1, n - 2, n - 1} {
		if strings.Contains(lines[i], term.AnsiScreenBg) {
			t.Errorf("%d 行目 (枠の外か上辺) に地色を敷いた: %q", i, lines[i])
		}
	}
	if !strings.Contains(lines[n-3], term.AnsiScreenBg) {
		t.Errorf("下辺に地色が無い (中身と下辺の間に端末の地色の筋が出る): %q", lines[n-3])
	}
	for i := 2; i < n-3; i++ {
		ln := lines[i]
		start, end := strings.Index(ln, term.AnsiScreenBg), strings.LastIndex(ln, "║")
		if start < 0 || end < start {
			t.Errorf("%d 行目の枠の内側に地色が無い: %q", i, ln)
			continue
		}
		// 枠の内側は板の幅ぶん塗り切る (途中で切れると右側が端末の地色のまま残る)
		inner := ln[start:end]
		if want := m.width - 2 - 1 - 2; term.DispWidth(inner) != want { // 左右の余白 2・右の影 1・罫線 2 本
			t.Errorf("%d 行目の塗り幅 %d != %d: %q", i, term.DispWidth(inner), want, ln)
		}
		// 行内の SGR リセットの後は地色を張り直す (リセットで地が切れる)。最後のリセットは右の罫線の手前で地を閉じるもの
		body := strings.TrimSuffix(strings.TrimSuffix(inner, term.AnsiFrameBorder), term.AnsiReset)
		for rest := body; ; {
			idx := strings.Index(rest, term.AnsiReset)
			if idx < 0 {
				break
			}
			rest = rest[idx+len(term.AnsiReset):]
			if !strings.HasPrefix(rest, term.AnsiScreenBg) {
				t.Errorf("%d 行目: リセット後に地色を張り直していない: %q", i, ln)
				break
			}
		}
	}
	// 枠を描かないときは窓の行を端末幅まで塗り、hint 行は塗らない
	m.showFrame = false
	lines = strings.Split(m.View().Content, "\n")
	for i, ln := range lines[:len(lines)-1] {
		if !strings.HasPrefix(ln, term.AnsiScreenBg) || term.DispWidth(ln) != m.width {
			t.Errorf("枠なしの %d 行目を端末幅まで塗っていない: %q", i, ln)
		}
	}
	if hint := lines[len(lines)-1]; strings.Contains(hint, term.AnsiScreenBg) {
		t.Errorf("枠なしの hint 行に地色を敷いた: %q", hint)
	}
}

// 面塗りはダッシュボードだけ。一覧や色なし (NO_COLOR) では敷かない — 面塗りは環境の配色
// 次第で視認性を落とすため、既定では増やさないというのが repo の判断 (bgLine の doc)。
func TestScreenBackgroundOnlyForDashboard(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
	m.colored = true
	m.width, m.height = 100, 30
	if got := m.screenBg(); got != "" {
		t.Errorf("一覧なのに地色を敷いた: %q", got)
	}
	if strings.Contains(m.View().Content, term.AnsiScreenBg) {
		t.Error("一覧の描画に地色が混ざっている")
	}
	m.handleKey("R")
	if m.screenBg() != term.AnsiScreenBg {
		t.Error("ダッシュボードで地色を敷いていない")
	}
	m.colored = false
	if got := m.screenBg(); got != "" {
		t.Errorf("色なしなのに地色を敷いた: %q", got)
	}
}
