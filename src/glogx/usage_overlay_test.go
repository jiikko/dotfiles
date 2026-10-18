package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ratelimit/usage"

	"glogx/internal/app"
	"glogx/internal/gitdata"
	"glogx/internal/ratelimitview"
	"glogx/internal/term"
	"glogx/internal/usageview"
)

// term.OverlayCenteredBox は中央モーダルを行塗り潰しでなく合成で重ね、左右の背景リストを残す
// (ユーザー要望 2026-07-22: モーダル左側テキストが消える問題の解消)。
func TestOverlayCenteredBoxKeepsBackgroundBothSides(t *testing.T) {
	const width = 60
	// 背景行: 左 8 桁 "L"・中央 44 桁 "m"・右 8 桁 "R" (計 60)
	bg := strings.Repeat("L", 8) + strings.Repeat("m", 44) + strings.Repeat("R", 8)
	window := make([]string, 10)
	for i := range window {
		window[i] = bg
	}
	box := []string{strings.Repeat("B", 44)} // 幅 44 → leftGap=(60-44)/2=8
	out := term.OverlayCenteredBox(window, box, width, len(window), false)

	// box は縦中央 ((10-1)/2=4) に載る
	got := out[4]
	if !strings.HasPrefix(got, strings.Repeat("L", 8)) {
		t.Errorf("左背景が保持されていない: %q", got)
	}
	if !strings.HasSuffix(got, strings.Repeat("R", 8)) {
		t.Errorf("右背景が保持されていない: %q", got)
	}
	if !strings.Contains(got, strings.Repeat("B", 44)) {
		t.Errorf("box 本体が載っていない: %q", got)
	}
	if strings.Contains(got, "m") {
		t.Errorf("box が占める中央列に背景 'm' が残っている (塗り潰せていない): %q", got)
	}
	// box を載せない行は無改変
	if out[0] != bg {
		t.Errorf("box 行以外が改変された: %q", out[0])
	}
}

// 右背景の色 (cut より後ろの SGR) が合成後も保たれる (term.DropToColumn の replay 経由)。
func TestOverlayCenteredBoxPreservesRightColor(t *testing.T) {
	const width = 60
	// 右 8 桁を緑に。左 8 "L" + 中央 44 "m" + 緑 "RRRRRRRR"
	bg := strings.Repeat("L", 8) + strings.Repeat("m", 44) + term.AnsiGreen + strings.Repeat("R", 8) + term.AnsiReset
	window := []string{bg, bg, bg, bg, bg, bg, bg, bg, bg, bg}
	box := []string{strings.Repeat("B", 44)}
	out := term.OverlayCenteredBox(window, box, width, len(window), true)
	got := out[4]
	if !strings.Contains(got, term.AnsiGreen) {
		t.Errorf("右背景の色コードが失われた: %q", got)
	}
	if term.StripANSI(got) != strings.Repeat("L", 8)+strings.Repeat("B", 44)+strings.Repeat("R", 8) {
		t.Errorf("合成後の可視内容がずれた: plain=%q", term.StripANSI(got))
	}
}

// 起動時は表示、任意キーで非表示、U で再表示 (ユーザー要望の「何か押したら消える」)。
func TestUsageOverlayDismiss(t *testing.T) {
	m := newTestBrowse(t, 5, nil, nil)
	if !m.usageOv.Visible() {
		t.Fatal("起動時に usageOv.visible=false")
	}
	m.handleKey("j") // 何かキー → 消える
	if m.usageOv.Visible() {
		t.Error("キー押下後も usageOv.visible=true (消えていない)")
	}
	m.handleKey("U") // U で再表示
	if !m.usageOv.Visible() {
		t.Error("U で再表示されない")
	}
	releaseKey(m)    // 指を離してから押し直す (キーリピート判定を跨ぐ)
	m.handleKey("U") // U でまた非表示 (トグル)
	if m.usageOv.Visible() {
		t.Error("U トグルで非表示にならない")
	}
}

// U は push 確認モーダルを素通りせず、通常キー = キャンセルとして扱われる (footgun 回帰)。
func TestUsageToggleDoesNotBypassConfirmModal(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
	m.actModal.pushConfirm = true
	visBefore := m.usageOv.Visible()
	m.handleKey("U")
	if m.actModal.pushConfirm {
		t.Error("U が push 確認モーダルをキャンセルしていない (残った確認へ Enter で誤 push する footgun)")
	}
	if m.usageOv.Visible() != visBefore {
		t.Error("モーダル中の U が usage をトグルした (モーダルのキャンセル語彙を優先すべき)")
	}
}

// 取得待ち = spinnerActive で tick が回る (スピナーが animate する前提)。取得完了で止まる。
func TestUsageLoadingDrivesSpinner(t *testing.T) {
	m := newTestBrowse(t, 5, nil, nil) // toFetch なし = CI fetch は動かない
	if !m.usageOv.Loading() {
		t.Fatal("起動直後は usageOv.loading()=true のはず")
	}
	if !m.spinnerActive() {
		t.Error("usage 取得中に spinnerActive=false (tick が回らずスピナーが止まる)")
	}
	// 結果到着でローディング終了 → spinner 対象から外れる。
	m.usageOv.Fixture().SetSnapshot(&usage.Snapshot{Windows: []usage.Window{
		{Label: "5h", Percent: 4, ResetAt: time.Now().Add(time.Hour)},
	}})
	if m.usageOv.Loading() {
		t.Error("snap 到着後も usageOv.loading()=true")
	}
	if m.spinnerActive() {
		t.Error("他に動くものが無いのに spinnerActive=true (tick が止まらない)")
	}
}

// 取得中の box はスピナー行を含み、成功時は枠ごとに 1 行 + 罫線で複数行になる。
func TestUsageBoxLines(t *testing.T) {
	m := newTestBrowse(t, 5, nil, nil)

	loading := m.usageOv.BoxLines(m.width, m.colored, m.spinner())
	if len(loading) < 3 { // 上罫線 + 内容 + 下罫線 (影付きは更に多い)
		t.Fatalf("取得中の box 行数が少ない: %d", len(loading))
	}
	loadingPlain := term.StripANSI(strings.Join(loading, "\n"))
	if !strings.Contains(loadingPlain, "取得中") {
		t.Errorf("取得中 box に '取得中' が無い:\n%s", loadingPlain)
	}
	// 取得中でも title は省略せず "Claude Code · usage" を出す (ユーザー要望 2026-07-23)。
	if !strings.Contains(loadingPlain, "Claude Code") {
		t.Errorf("取得中 box の title が省略されている ('Claude Code' が無い):\n%s", loadingPlain)
	}

	m.usageOv.Fixture().SetSnapshot(&usage.Snapshot{Windows: []usage.Window{
		{Label: "5h", Percent: 4, ResetAt: time.Now().Add(4 * time.Hour)},
		{Label: "7d", Percent: 29, ResetAt: time.Now().Add(50 * time.Hour)},
	}})
	box := m.usageOv.BoxLines(m.width, m.colored, m.spinner())
	plain := term.StripANSI(strings.Join(box, "\n"))
	if !strings.Contains(plain, "5h") || !strings.Contains(plain, "7d") {
		t.Errorf("成功 box に 5h/7d が無い:\n%s", plain)
	}
	// 列見出し (ヘッダー) は表示しない (ユーザー要望 2026-07-23)。header 専用ラベル "リセット" が
	// 出ていないことで検証する (データ行はリセット時刻を出すが「リセット」の語は出さない)。
	if strings.Contains(plain, "リセット") {
		t.Errorf("ヘッダー (列見出し) が消えていない ('リセット' が残る):\n%s", plain)
	}

	// 非表示なら nil。
	m.usageOv.Fixture().SetVisible(false)
	if m.usageOv.BoxLines(m.width, m.colored, m.spinner()) != nil {
		t.Error("非表示で nil を返さない")
	}
}

// --- 1 分ごとのバックグラウンド定期リフレッシュ (ユーザー要望 2026-07-22) ---

// usageRefreshMsg はバックグラウンド再取得を仕掛け、次回リフレッシュを再予約する
// (cmd 非 nil = チェーンが継続。fetchCmd 起動で cancel がセットされる)。
func TestUsageRefreshMsgReschedulesAndFetches(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
	_, cmd := m.Update(usageRefreshMsg{})
	if cmd == nil {
		t.Fatal("usageRefreshMsg が nil を返した (再取得も再予約もされない = リフレッシュ停止)")
	}
	if !m.usageOv.Fixture().Fetching() {
		t.Error("リフレッシュで fetchCmd が起動していない (cancel 未セット)")
	}
}

// 非表示中の定期リフレッシュは subprocess を起こさない (見えないもののために claude を
// 60 秒ごとに起動しない) が、tick チェーンは維持する (再表示後に周期取得が復活する)。
func TestUsageRefreshSkipsFetchWhileHidden(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
	m.usageOv.Dismiss()
	_, cmd := m.Update(usageRefreshMsg{})
	if cmd == nil {
		t.Fatal("非表示でチェーンまで切れた (再表示後に周期取得が復活しない)")
	}
	if m.usageOv.Fixture().Fetching() {
		t.Error("非表示なのに fetchCmd が起動した (cancel がセットされた)")
	}
}

// U での再表示は、表示が陳腐なら取り直す / fresh なら取り直さない。
// 非表示中にリフレッシュを止めた分の鮮度をここで回収する。
func TestUsageToggleRefetchesOnlyWhenStale(t *testing.T) {
	t.Run("stale なら取り直す", func(t *testing.T) {
		m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
		m.usageOv.Dismiss()
		m.usageOv.Fixture().SetSnapshot(&usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 1}}})
		m.usageOv.Fixture().SetFetchedAt(time.Now().Add(-2 * app.UsageRefreshInterval))
		m.handleKey("U")
		if !m.usageOv.Visible() {
			t.Fatal("U で表示になっていない")
		}
		if !m.usageOv.Fixture().Fetching() {
			t.Error("stale なのに取り直していない (cancel 未セット)")
		}
	})
	t.Run("fresh なら取り直さない", func(t *testing.T) {
		m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
		m.usageOv.Dismiss()
		m.usageOv.Fixture().SetSnapshot(&usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 1}}})
		m.usageOv.Fixture().SetFetchedAt(time.Now())
		m.handleKey("U")
		if m.usageOv.Fixture().Fetching() {
			t.Error("fresh なのに取り直した (無駄な subprocess)")
		}
	})
}

// Claude 側だけ取れなかったときは右上の箱にも注記を出す。ただし理由の長さで箱を広げない
// (全文はダッシュボードに出る)。
func TestUsageBoxShowsClaudeFetchError(t *testing.T) {
	m := newTestBrowse(t, 5, nil, nil)
	m.usageOv.Fixture().SetVisible(true)
	m.usageOv.Fixture().SetSnapshot(&usage.Snapshot{Windows: []usage.Window{
		{Label: "5h", Percent: 4, ResetAt: time.Now().Add(time.Hour)},
	}})
	base := m.usageOv.BoxLines(m.width, m.colored, m.spinner())
	if strings.Contains(term.StripANSI(strings.Join(base, "\n")), "取得に失敗") {
		t.Fatalf("取得できているのに注記がある:\n%s", term.StripANSI(strings.Join(base, "\n")))
	}
	m.usageOv.Snapshot().ClaudeErr = "claude /usage 実行失敗: exit status 127: " + strings.Repeat("x", 300)
	box := m.usageOv.BoxLines(m.width, m.colored, m.spinner())
	plain := term.StripANSI(strings.Join(box, "\n"))
	if !strings.Contains(plain, "Claude Code の取得に失敗 (前回の値を表示中)") {
		t.Errorf("箱に注記が無い:\n%s", plain)
	}
	if got, want := term.DispWidth(box[0]), term.DispWidth(base[0]); got != want {
		t.Errorf("理由の長さで箱の幅が変わった: %d → %d", want, got)
	}
}

// 全滅 (キャッシュ経路の err) のときは last-good を保持するが、古いことと理由を注記で出す。次の成功で消える。
func TestUsageLastGoodShowsStaleReason(t *testing.T) {
	// 描画キャッシュの鍵には秒が入る。時刻を止めないと、失敗時と回復後の描画が秒をまたいだときに
	// 鍵の秒の違いでキャッシュが外れ、鍵に理由を載せ忘れた退行を素通りする
	orig := app.TimeNow
	app.TimeNow = func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { app.TimeNow = orig })
	m := newTestBrowse(t, 5, nil, nil)
	m.usageOv.Fixture().SetVisible(true)
	good := &usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 4, ResetAt: time.Now().Add(time.Hour)}}}
	m.usageOv.Handle(usageview.FixtureUsageMsg(good, nil, nil))
	m.usageOv.Handle(usageview.FixtureUsageMsg(nil, errors.Join(errors.New("claude /usage 実行失敗: exit status 127"), errors.New("codex 起動失敗")), nil))
	if m.usageOv.Snapshot() != good {
		t.Fatal("全滅で last-good を捨てた")
	}
	box := term.StripANSI(strings.Join(m.usageOv.BoxLines(m.width, m.colored, m.spinner()), "\n"))
	if !strings.Contains(box, "取得に失敗 (前回の値を表示中)") {
		t.Errorf("last-good を保持したのに注記が無い:\n%s", box)
	}
	var d ratelimitview.RatelimitDash
	d.Toggle()
	if head := term.StripANSI(d.Lines(m.ratelimitOpts())[1]); !strings.Contains(head, "exit status 127 / ") {
		t.Errorf("ダッシュボードに両方の理由が区切られて出ていない: %q", head)
	}
	m.usageOv.Handle(usageview.FixtureUsageMsg(good, nil, nil)) // 回復 (同じポインタでも注記は消える = 描画キャッシュの鍵に理由が要る)
	box = term.StripANSI(strings.Join(m.usageOv.BoxLines(m.width, m.colored, m.spinner()), "\n"))
	if strings.Contains(box, "取得に失敗") {
		t.Errorf("回復後も注記が残る:\n%s", box)
	}
	if head := d.Lines(m.ratelimitOpts())[1]; head != "" {
		t.Errorf("回復後もダッシュボードに注記が残る: %q", head)
	}
}

// R のダッシュボードの `r` は、全プロセス共有のゲートの 5 分の間引きを飛ばして取り直す。定期の取得は
// 間引きの内側 (claude を起こさない)。`r` が定期と同じ経路に戻ると、数分前の値が「今取った値」として
// 入り、押しても取り直されない。
func TestRatelimitDashRefreshKeyBypassesSharedGate(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	stub := func(name, script string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stub("claude", `case "$1" in
--version) echo "9.9.9 (Claude Code)";;
*) echo x >> `+calls+`
printf '%s\n' '{"type":"result","result":"Current session: 2% used · resets Jul 22 at 3:09am (Asia/Tokyo)","is_error":false}';;
esac
`)
	stub("codex", "exit 1\n")
	t.Setenv("PATH", dir)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	count := func() int {
		b, _ := os.ReadFile(calls)
		return strings.Count(string(b), "\n")
	}

	m := newTestBrowse(t, 2, map[string]gitdata.CIState{}, nil)
	m.rlDash.Fixture().SetShown(true)
	runCmdTree(m.usageOv.FetchCmd(false))  // 共有ゲートに 5 分以内の結果を作る
	m.usageOv.Fixture().SetInFlight(false) // handle を通していないので手で下ろす
	if got := count(); got != 1 {
		t.Fatalf("前提: claude %d 回, want 1", got)
	}
	runCmdTree(m.usageOv.FetchCmd(false)) // 定期の取得は間引かれる
	m.usageOv.Fixture().SetInFlight(false)
	if got := count(); got != 1 {
		t.Fatalf("定期の取得が 5 分以内に claude を起こした: %d 回", got)
	}
	_, cmd := m.routeKeyToRatelimitDash("r")
	runCmdTree(cmd)
	if got := count(); got != 2 {
		t.Fatalf("r で取り直さない: claude %d 回, want 2", got)
	}
}
