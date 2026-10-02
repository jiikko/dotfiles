package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"ratelimit/usage"

	tea "charm.land/bubbletea/v2"
)

// overlayBoxTopRight は box を右上へ右揃えで重ね、覆った各行の表示幅が width ちょうどに
// 揃うこと。box 行より下のウィンドウ行は変わらないこと。
func TestOverlayBoxTopRightAligns(t *testing.T) {
	window := []string{"commit line one", "author line two", "date line three", "keep me"}
	box := []string{"┌ usage ─┐", "│ 5h ok │", "└────────┘"}
	width := 40
	got := overlayBoxTopRight(window, box, width, false)

	for i, b := range box {
		if !strings.HasSuffix(got[i], b) {
			t.Errorf("行 %d が box 行で終わっていない: %q", i, got[i])
		}
		if w := dispWidth(got[i]); w != width {
			t.Errorf("行 %d の表示幅 = %d, want %d", i, w, width)
		}
	}
	if got[3] != "keep me" {
		t.Errorf("box より下の行を壊した: %q", got[3])
	}
}

// 覆う行の左側 (見えている部分) の色は保持される (取得中に上部行の色が抜けない回帰)。
func TestOverlayBoxTopRightKeepsLeftColor(t *testing.T) {
	colored := ansiGreen + "green subject text here" + ansiReset
	window := []string{colored}
	box := []string{"┌ usage ┐"}
	got := overlayBoxTopRight(window, box, 40, true)
	if !strings.Contains(got[0], ansiGreen) {
		t.Errorf("左側の色 (%q) が保持されていない: %q", ansiGreen, got[0])
	}
	// 幅は width ちょうど、右端は box。
	if w := dispWidth(got[0]); w != 40 {
		t.Errorf("表示幅 = %d, want 40", w)
	}
	if !strings.HasSuffix(got[0], box[0]) {
		t.Errorf("右端が box で終わっていない: %q", got[0])
	}
}

// box が window より高くても (行数超過) パニックせず、収まる分だけ重ねる。
func TestOverlayBoxTopRightTallBox(t *testing.T) {
	window := []string{"only one row"}
	box := []string{"row0", "row1", "row2"}
	got := overlayBoxTopRight(window, box, 20, false)
	if len(got) != 1 {
		t.Fatalf("行数が変わった: %d", len(got))
	}
	if w := dispWidth(got[0]); w != 20 {
		t.Errorf("表示幅 = %d, want 20", w)
	}
}

func TestOverlayBoxTopRightEmpty(t *testing.T) {
	if got := overlayBoxTopRight(nil, []string{"x"}, 10, false); got != nil {
		t.Errorf("空ウィンドウで nil を返さない: %v", got)
	}
	// 空 box / 幅 0 は**ウィンドウをそのまま返す**。戻り値を捨てると「背景を丸ごと落とす」
	// 退行 (overlayBoxRight の早期 return を nil にする) が素通りする (issue 198 発見 1)。
	window := []string{"a", "b"}
	if got := overlayBoxTopRight(window, nil, 10, false); !slices.Equal(got, window) {
		t.Errorf("空 box でウィンドウが変わった: got %v want %v", got, window)
	}
	if got := overlayBoxTopRight(window, []string{"x"}, 0, false); !slices.Equal(got, window) {
		t.Errorf("幅 0 でウィンドウが変わった: got %v want %v", got, window)
	}
}

// overlayCenteredBox は中央モーダルを行塗り潰しでなく合成で重ね、左右の背景リストを残す
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
	out := overlayCenteredBox(window, box, width, len(window), false)

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

// 右背景の色 (cut より後ろの SGR) が合成後も保たれる (dropToColumn の replay 経由)。
func TestOverlayCenteredBoxPreservesRightColor(t *testing.T) {
	const width = 60
	// 右 8 桁を緑に。左 8 "L" + 中央 44 "m" + 緑 "RRRRRRRR"
	bg := strings.Repeat("L", 8) + strings.Repeat("m", 44) + ansiGreen + strings.Repeat("R", 8) + ansiReset
	window := []string{bg, bg, bg, bg, bg, bg, bg, bg, bg, bg}
	box := []string{strings.Repeat("B", 44)}
	out := overlayCenteredBox(window, box, width, len(window), true)
	got := out[4]
	if !strings.Contains(got, ansiGreen) {
		t.Errorf("右背景の色コードが失われた: %q", got)
	}
	if stripANSI(got) != strings.Repeat("L", 8)+strings.Repeat("B", 44)+strings.Repeat("R", 8) {
		t.Errorf("合成後の可視内容がずれた: plain=%q", stripANSI(got))
	}
}

// 起動時は表示、任意キーで非表示、U で再表示 (ユーザー要望の「何か押したら消える」)。
func TestUsageOverlayDismiss(t *testing.T) {
	m := newTestBrowse(t, 5, nil, nil)
	if !m.usageOv.visible {
		t.Fatal("起動時に usageOv.visible=false")
	}
	m.handleKey("j") // 何かキー → 消える
	if m.usageOv.visible {
		t.Error("キー押下後も usageOv.visible=true (消えていない)")
	}
	m.handleKey("U") // U で再表示
	if !m.usageOv.visible {
		t.Error("U で再表示されない")
	}
	releaseKey(m)    // 指を離してから押し直す (キーリピート判定を跨ぐ)
	m.handleKey("U") // U でまた非表示 (トグル)
	if m.usageOv.visible {
		t.Error("U トグルで非表示にならない")
	}
}

// U は push 確認モーダルを素通りせず、通常キー = キャンセルとして扱われる (footgun 回帰)。
func TestUsageToggleDoesNotBypassConfirmModal(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.actModal.pushConfirm = true
	visBefore := m.usageOv.visible
	m.handleKey("U")
	if m.actModal.pushConfirm {
		t.Error("U が push 確認モーダルをキャンセルしていない (残った確認へ Enter で誤 push する footgun)")
	}
	if m.usageOv.visible != visBefore {
		t.Error("モーダル中の U が usage をトグルした (モーダルのキャンセル語彙を優先すべき)")
	}
}

// 取得待ち = spinnerActive で tick が回る (スピナーが animate する前提)。取得完了で止まる。
func TestUsageLoadingDrivesSpinner(t *testing.T) {
	m := newTestBrowse(t, 5, nil, nil) // toFetch なし = CI fetch は動かない
	if !m.usageOv.loading() {
		t.Fatal("起動直後は usageOv.loading()=true のはず")
	}
	if !m.spinnerActive() {
		t.Error("usage 取得中に spinnerActive=false (tick が回らずスピナーが止まる)")
	}
	// 結果到着でローディング終了 → spinner 対象から外れる。
	m.usageOv.snap = &usage.Snapshot{Windows: []usage.Window{
		{Label: "5h", Percent: 4, ResetAt: time.Now().Add(time.Hour)},
	}}
	if m.usageOv.loading() {
		t.Error("snap 到着後も usageOv.loading()=true")
	}
	if m.spinnerActive() {
		t.Error("他に動くものが無いのに spinnerActive=true (tick が止まらない)")
	}
}

// 取得中の box はスピナー行を含み、成功時は枠ごとに 1 行 + 罫線で複数行になる。
func TestUsageBoxLines(t *testing.T) {
	m := newTestBrowse(t, 5, nil, nil)

	loading := m.usageOv.boxLines(m.width, m.colored, m.spinner())
	if len(loading) < 3 { // 上罫線 + 内容 + 下罫線 (影付きは更に多い)
		t.Fatalf("取得中の box 行数が少ない: %d", len(loading))
	}
	loadingPlain := stripANSI(strings.Join(loading, "\n"))
	if !strings.Contains(loadingPlain, "取得中") {
		t.Errorf("取得中 box に '取得中' が無い:\n%s", loadingPlain)
	}
	// 取得中でも title は省略せず "Claude Code · usage" を出す (ユーザー要望 2026-07-23)。
	if !strings.Contains(loadingPlain, "Claude Code") {
		t.Errorf("取得中 box の title が省略されている ('Claude Code' が無い):\n%s", loadingPlain)
	}

	m.usageOv.snap = &usage.Snapshot{Windows: []usage.Window{
		{Label: "5h", Percent: 4, ResetAt: time.Now().Add(4 * time.Hour)},
		{Label: "7d", Percent: 29, ResetAt: time.Now().Add(50 * time.Hour)},
	}}
	box := m.usageOv.boxLines(m.width, m.colored, m.spinner())
	plain := stripANSI(strings.Join(box, "\n"))
	if !strings.Contains(plain, "5h") || !strings.Contains(plain, "7d") {
		t.Errorf("成功 box に 5h/7d が無い:\n%s", plain)
	}
	// 列見出し (ヘッダー) は表示しない (ユーザー要望 2026-07-23)。header 専用ラベル "リセット" が
	// 出ていないことで検証する (データ行はリセット時刻を出すが「リセット」の語は出さない)。
	if strings.Contains(plain, "リセット") {
		t.Errorf("ヘッダー (列見出し) が消えていない ('リセット' が残る):\n%s", plain)
	}

	// 非表示なら nil。
	m.usageOv.visible = false
	if m.usageOv.boxLines(m.width, m.colored, m.spinner()) != nil {
		t.Error("非表示で nil を返さない")
	}
}

// loading は「表示中 かつ 結果未着」のときだけ true。err 到着後も false になり (tick が
// 止まりスピナーが無限に回らない)、非表示中も false。browseModel を作らず型単体で検証する。
func TestUsageOverlayLoadingStates(t *testing.T) {
	cases := []struct {
		name string
		ov   usageOverlay
		want bool
	}{
		{"表示中・結果未着", usageOverlay{visible: true}, true},
		{"表示中・snap 到着", usageOverlay{visible: true, snap: &usage.Snapshot{}}, false},
		{"表示中・err 到着", usageOverlay{visible: true, err: errors.New("boom")}, false},
		{"非表示・結果未着", usageOverlay{visible: false}, false},
	}
	for _, c := range cases {
		if got := c.ov.loading(); got != c.want {
			t.Errorf("%s: loading()=%v, want %v", c.name, got, c.want)
		}
	}
}

// 取得失敗時の box は "取得失敗" を表示する (エラー描画パスの回帰ガード)。型単体で検証。
func TestUsageOverlayBoxLinesError(t *testing.T) {
	ov := usageOverlay{visible: true, err: errors.New("boom")}
	box := ov.boxLines(80, false, "|")
	if len(box) == 0 {
		t.Fatal("エラー時に box が空")
	}
	plain := stripANSI(strings.Join(box, "\n"))
	if !strings.Contains(plain, "取得失敗") {
		t.Errorf("エラー box に '取得失敗' が無い:\n%s", plain)
	}
	// エラー時も title は省略しない (ユーザー要望 2026-07-23)。
	if !strings.Contains(plain, "Claude Code") {
		t.Errorf("エラー box の title が省略されている ('Claude Code' が無い):\n%s", plain)
	}
}

// --- 1 分ごとのバックグラウンド定期リフレッシュ (ユーザー要望 2026-07-22) ---

// 不変条件: 一度取れた usage は定期リフレッシュの一時失敗で失わない。既に snap がある状態で
// 失敗結果が来たら last-good を保持し "取得失敗" へ落とさない (毎分の再取得が瞬断で転けても
// 右上表示がチラつかない)。
func TestUsageHandlePreservesLastGoodOnRefreshError(t *testing.T) {
	o := &usageOverlay{visible: true}
	good := &usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 10}}}
	o.handle(usageMsg{snap: good})
	// 定期リフレッシュが失敗
	o.handle(usageMsg{err: errors.New("boom")})
	if o.snap != good {
		t.Error("リフレッシュ失敗で last-good スナップショットが消えた")
	}
	if o.err != nil {
		t.Errorf("last-good があるのにエラーが表面化した: %v", o.err)
	}
}

// 初回取得の失敗 (snap 未取得) はそのままエラー表示する。
func TestUsageHandleInitialErrorSurfaces(t *testing.T) {
	o := &usageOverlay{visible: true}
	o.handle(usageMsg{err: errors.New("boom")})
	if o.err == nil {
		t.Error("初回取得失敗はエラー表示すべき")
	}
	if o.snap != nil {
		t.Error("初回失敗で snap が nil でない")
	}
}

// リフレッシュ成功は last-good を新値へ置き換える (err はクリア)。
func TestUsageHandleRefreshSuccessReplaces(t *testing.T) {
	o := &usageOverlay{visible: true}
	v1 := &usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 10}}}
	v2 := &usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 42}}}
	o.handle(usageMsg{snap: v1})
	o.handle(usageMsg{snap: v2})
	if o.snap != v2 {
		t.Error("リフレッシュ成功で新値へ置き換わっていない")
	}
	if o.err != nil {
		t.Errorf("成功なのに err が残った: %v", o.err)
	}
}

// fetch は single-flight: 走行中にもう 1 本発行しない。cancel が単一スロットのため、overlap
// させると先行 fetch の cancel を上書きで取りこぼし、quit 後も subprocess が fetchTimeout まで
// 残る (シナリオ: 起動時 fetch の完了前に U を 2 回 = 非表示 → stale 再表示で 2 本目)。
func TestUsageFetchCmdIsSingleFlight(t *testing.T) {
	o := &usageOverlay{visible: true}
	if o.fetchCmd(false) == nil {
		t.Fatal("最初の fetchCmd が nil (fetch が始まらない)")
	}
	// closure は実行しない (実 subprocess を起こさない) ので defer cancel() が走らない。
	// ctx タイマーを残さないよう、上書きされる前の cancel をテスト側で退避して呼ぶ
	firstCancel := o.cancel
	defer firstCancel()
	if o.fetchCmd(false) != nil {
		t.Fatal("走行中なのに 2 本目の fetchCmd が発行された (先行分の cancel を取りこぼす)")
	}
	// 結果が届けば (成否によらず) 次の fetch を発行できる
	o.handle(usageMsg{err: errors.New("boom")})
	if o.fetchCmd(false) == nil {
		t.Fatal("結果を受け取った後も fetch が発行できない (inFlight が降りていない)")
	}
	o.stop() // 3 本目の ctx の後始末
}

// 初回失敗 → リフレッシュ成功 で回復する (err クリア + snap セット)。
func TestUsageHandleRecoversFromInitialError(t *testing.T) {
	o := &usageOverlay{visible: true}
	o.handle(usageMsg{err: errors.New("boom")}) // 初回失敗
	good := &usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 10}}}
	o.handle(usageMsg{snap: good}) // リフレッシュで回復
	if o.snap != good || o.err != nil {
		t.Errorf("初回失敗から回復していない: snap=%v err=%v", o.snap, o.err)
	}
}

// 成功時の usage box は自動更新を明示するフッター (usageFooter) を末尾に出す
// (ユーザー要望 2026-07-22。間隔は全プロセス共有のゲートの間隔。issue 627)。
func TestUsageBoxLinesShowsAutoRefreshFooter(t *testing.T) {
	ov := usageOverlay{
		visible: true,
		snap: &usage.Snapshot{Windows: []usage.Window{
			{Label: "5h", Percent: 20, ResetAt: time.Now().Add(4 * time.Hour)},
		}},
	}
	box := ov.boxLines(80, false, "|")
	plain := stripANSI(strings.Join(box, "\n"))
	if !strings.Contains(plain, usageFooter) {
		t.Errorf("usage box に自動更新フッターが無い:\n%s", plain)
	}
	// フッターは末尾付近 (データ行の後) に出る: 5h 行より後であること
	if strings.Index(plain, usageFooter) < strings.Index(plain, "5h") {
		t.Errorf("フッターがデータ行より前に出ている:\n%s", plain)
	}
	// 右寄せ: フッター文言は行の右端 (右罫線 │ の直前) に来る。左端 "│ " 直後には出さない。
	var footerLine string
	for _, l := range box {
		if strings.Contains(stripANSI(l), usageFooter) {
			footerLine = stripANSI(l)
			break
		}
	}
	if footerLine == "" {
		t.Fatal("フッター行が見つからない")
	}
	if !strings.Contains(footerLine, usageFooter+" │") {
		t.Errorf("フッターが右寄せされていない (右罫線に接していない):\n%q", footerLine)
	}
	if strings.Contains(footerLine, "│ "+usageFooter) {
		t.Errorf("フッターが左寄せのまま (左罫線直後に出ている):\n%q", footerLine)
	}
}

// usageRefreshMsg はバックグラウンド再取得を仕掛け、次回リフレッシュを再予約する
// (cmd 非 nil = チェーンが継続。fetchCmd 起動で cancel がセットされる)。
func TestUsageRefreshMsgReschedulesAndFetches(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	_, cmd := m.Update(usageRefreshMsg{})
	if cmd == nil {
		t.Fatal("usageRefreshMsg が nil を返した (再取得も再予約もされない = リフレッシュ停止)")
	}
	if m.usageOv.cancel == nil {
		t.Error("リフレッシュで fetchCmd が起動していない (cancel 未セット)")
	}
}

// 非表示中の定期リフレッシュは subprocess を起こさない (見えないもののために claude を
// 60 秒ごとに起動しない) が、tick チェーンは維持する (再表示後に周期取得が復活する)。
func TestUsageRefreshSkipsFetchWhileHidden(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.usageOv.dismiss()
	_, cmd := m.Update(usageRefreshMsg{})
	if cmd == nil {
		t.Fatal("非表示でチェーンまで切れた (再表示後に周期取得が復活しない)")
	}
	if m.usageOv.cancel != nil {
		t.Error("非表示なのに fetchCmd が起動した (cancel がセットされた)")
	}
}

// U での再表示は、表示が陳腐なら取り直す / fresh なら取り直さない。
// 非表示中にリフレッシュを止めた分の鮮度をここで回収する。
func TestUsageToggleRefetchesOnlyWhenStale(t *testing.T) {
	t.Run("stale なら取り直す", func(t *testing.T) {
		m := newTestBrowse(t, 1, map[string]CIState{}, nil)
		m.usageOv.dismiss()
		m.usageOv.snap = &usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 1}}}
		m.usageOv.fetchedAt = time.Now().Add(-2 * usageRefreshInterval)
		m.handleKey("U")
		if !m.usageOv.visible {
			t.Fatal("U で表示になっていない")
		}
		if m.usageOv.cancel == nil {
			t.Error("stale なのに取り直していない (cancel 未セット)")
		}
	})
	t.Run("fresh なら取り直さない", func(t *testing.T) {
		m := newTestBrowse(t, 1, map[string]CIState{}, nil)
		m.usageOv.dismiss()
		m.usageOv.snap = &usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 1}}}
		m.usageOv.fetchedAt = time.Now()
		m.handleKey("U")
		if m.usageOv.cancel != nil {
			t.Error("fresh なのに取り直した (無駄な subprocess)")
		}
	})
}

// Claude と codex の枠の間に区切り罫線が入る (ユーザー要望 2026-07-31)。単一出所なら罫線なし。
func TestUsageBoxLinesCodexDivider(t *testing.T) {
	o := usageOverlay{visible: true, snap: &usage.Snapshot{Windows: []usage.Window{
		{Label: "5h", Percent: 4, ResetAt: time.Now().Add(4 * time.Hour)},
		{Label: "7d", Percent: 29, ResetAt: time.Now().Add(50 * time.Hour)},
		{Label: "cx7d", Source: usage.SourceCodex, Percent: 69, ResetAt: time.Now().Add(120 * time.Hour)},
	}}}
	box := o.boxLines(120, false, "")
	// 区切り行 = 側罫線の内側が ─ の連続だけの行。位置は 7d 行と cx7d 行の間。
	divider, idx7d, idxCx := -1, -1, -1
	for i, line := range box {
		content := strings.Trim(stripANSI(line), "│░▒▓█ ")
		switch {
		case content != "" && strings.Trim(content, "─") == "" && i > 0: // 上辺 (┌...┐) は除外
			divider = i
		case strings.Contains(line, "cx7d"):
			idxCx = i
		case strings.Contains(line, "7d"):
			idx7d = i
		}
	}
	if divider < 0 {
		t.Fatalf("区切り罫線が無い:\n%s", strings.Join(box, "\n"))
	}
	if idx7d >= divider || divider >= idxCx {
		t.Errorf("区切り罫線の位置が 7d と cx7d の間でない (7d=%d divider=%d cx=%d)", idx7d, divider, idxCx)
	}

	// codex 枠が無ければ罫線は出ない。
	o.snap = &usage.Snapshot{Windows: []usage.Window{
		{Label: "5h", Percent: 4, ResetAt: time.Now().Add(4 * time.Hour)},
	}}
	for i, line := range o.boxLines(120, false, "") {
		content := strings.Trim(stripANSI(line), "│░▒▓█ ")
		if i > 0 && content != "" && strings.Trim(content, "─") == "" {
			t.Errorf("単一出所なのに区切り罫線がある (行 %d):\n%s", i, line)
		}
	}
}

// drainUsageFetch は fetchCmd の Cmd を走らせ、届く usageMsg を順に handle へ入れる (保存の Cmd も走らせる)。
func drainUsageFetch(t *testing.T, o *usageOverlay, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("fetchCmd が nil")
	}
	msgs := []tea.Msg{cmd()}
	if b, ok := msgs[0].(tea.BatchMsg); ok {
		msgs = msgs[:0]
		for _, c := range b {
			msgs = append(msgs, c())
		}
	}
	for _, msg := range msgs {
		um, ok := msg.(usageMsg)
		if !ok {
			t.Fatalf("usageMsg でない: %T", msg)
		}
		if c := o.handle(um); c != nil {
			c()
		}
	}
}

// 片側 (claude) の一時失敗で取れていた枠を失わない (出所単位の last-good)。stub CLI で claude 失敗 + codex 成功を作る。
func TestUsageFetchCmdKeepsOtherSourceOnPartialFailure(t *testing.T) {
	dir := t.TempDir()
	stub := func(name, script string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stub("claude", "exit 1\n")
	stub("codex", `read _l1
printf '%s\n' '{"id":1,"result":{}}'
read _l2
read _l3
printf '%s\n' '{"id":2,"result":{"rateLimits":{"primary":{"usedPercent":69,"windowDurationMins":10080,"resetsAt":1785903020},"secondary":null}}}'
`)
	t.Setenv("PATH", dir)
	t.Setenv("XDG_CACHE_HOME", t.TempDir()) // キャッシュ書き込みを隔離
	cachePath, err := usageCachePath()
	if err != nil {
		t.Fatal(err)
	}
	prev := &usage.Snapshot{Version: "9.9.9", Windows: []usage.Window{
		{Label: "5h", Percent: 4},
		{Label: "7d", Percent: 29},
		{Label: "cx7d", Source: usage.SourceCodex, Percent: 1},
	}}
	o := usageOverlay{visible: true, snap: prev}
	drainUsageFetch(t, &o, o.fetchCmd(false))
	if o.err != nil || o.staleErr != nil || o.inFlight {
		t.Fatalf("err=%v staleErr=%v inFlight=%v", o.err, o.staleErr, o.inFlight)
	}
	if _, found := o.snap.Find("5h"); !found {
		t.Errorf("claude 一時失敗で 5h 枠が消えた: %+v", o.snap.Windows)
	}
	if w, _ := o.snap.Find("cx7d"); w.Percent != 69 {
		t.Errorf("codex 枠が新値でない: %d, want 69", w.Percent)
	}
	if o.snap.Version != "9.9.9" || o.snap.ClaudeErr == "" {
		t.Errorf("Version の last-good / Claude の失敗理由: %q %q", o.snap.Version, o.snap.ClaudeErr)
	}
	// 表示用 snap は last-good で Claude 枠を補完するが、今回取得できたのは codex だけ。
	// キャッシュ契約を満たさないため、補完済み snap を保存してはならない。
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Errorf("claude 失敗 + codex 成功でキャッシュが作られた: err=%v", err)
	}
}

func usagePart(src string, pct int) *usage.Part {
	if src == usage.SourceCodex {
		return &usage.Part{Source: src, Version: "0.1", Windows: []usage.Window{{Label: "cx7d", Source: src, Percent: pct}}}
	}
	return &usage.Part{Version: "2.1", Windows: []usage.Window{{Label: "5h", Percent: pct}, {Label: "7d", Percent: pct}}}
}

func usageLabels(s *usage.Snapshot) string {
	if s == nil {
		return "<nil>"
	}
	ls := make([]string, 0, len(s.Windows))
	for _, w := range s.Windows {
		ls = append(ls, w.Label)
	}
	return strings.Join(ls, ",")
}

// 先に届いた方 (codex) をその場で出し、遅い方 (Claude) は「取得中」と出して待つ (issue 626)。
// 周の終わりに、取れた両方をキャッシュへ保存する。
func TestUsageShowsEachSourceAsItArrives(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	o := usageOverlay{visible: true}
	o.fetchCmd(false) // closure は走らせない (届く順をテストが決める)
	o.handle(usageMsg{part: usagePart(usage.SourceCodex, 7)})
	if got := usageLabels(o.snap); got != "cx7d" {
		t.Fatalf("先に届いた codex が表示へ入らない: %s", got)
	}
	if !o.loading() {
		t.Error("Claude を待っている間にスピナーが止まる")
	}
	box := strings.Join(o.boxLines(80, false, "*"), "\n")
	if !strings.Contains(box, "* Claude Code 取得中...") || strings.Contains(box, "codex 取得中") {
		t.Errorf("待っている出所だけを「取得中」に出していない:\n%s", box)
	}
	if c := o.handle(usageMsg{part: usagePart("", 3)}); c != nil {
		c() // 周の終わりのキャッシュの保存
	}
	if got := usageLabels(o.snap); got != "5h,7d,cx7d" {
		t.Errorf("Claude が後から届いても先に並ばない: %s", got)
	}
	if o.inFlight || o.loading() || len(o.waiting()) != 0 || o.fetchedAt.IsZero() {
		t.Errorf("周が閉じていない: inFlight=%v loading=%v waiting=%v", o.inFlight, o.loading(), o.waiting())
	}
	if box := strings.Join(o.boxLines(80, false, "*"), "\n"); strings.Contains(box, "取得中") {
		t.Errorf("両方そろった後も「取得中」が残った:\n%s", box)
	}
	path, _ := usageCachePath()
	if snap, ok := loadUsageCache(path, time.Now()); !ok || usageLabels(snap) != "5h,7d,cx7d" {
		t.Errorf("周の終わりに両方を保存していない: ok=%v %s", ok, usageLabels(snap))
	}
}

// 何も取れていないうちの Claude の失敗は、空の表示を出さずに codex を待つ。codex が取れたら Claude の失敗の注記つきで出す。
func TestUsageClaudeFailureFirstWaitsForCodex(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	o := usageOverlay{visible: true}
	o.fetchCmd(false)
	o.handle(usageMsg{part: &usage.Part{Err: errors.New("claude /usage 実行失敗: exit status 127")}})
	if o.snap != nil || o.err != nil || !o.loading() {
		t.Fatalf("Claude の失敗だけで表示が変わった: snap=%v err=%v", usageLabels(o.snap), o.err)
	}
	o.handle(usageMsg{part: usagePart(usage.SourceCodex, 7)})
	if usageLabels(o.snap) != "cx7d" || !strings.Contains(fetchNote(o.snap, o.staleErr), "Claude Code の取得に失敗") {
		t.Errorf("codex だけの表示 / Claude の注記: %s %q", usageLabels(o.snap), fetchNote(o.snap, o.staleErr))
	}
	path, _ := usageCachePath()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Claude 枠の無い周でキャッシュを書いた: %v", err)
	}
}

// 両方失敗した周は、途中で入れた失敗の注記ごと周の始まりの表示へ戻し、理由を Claude → codex の順で staleErr に置く。
// 何も無いところからの全滅は「取得失敗」。
func TestUsageBothFailRound(t *testing.T) {
	base := &usage.Snapshot{Windows: []usage.Window{{Label: "5h"}, {Label: "cx7d", Source: usage.SourceCodex}}}
	o := usageOverlay{visible: true, snap: base, fetchedAt: timeNow()}
	o.fetchCmd(false)
	o.handle(usageMsg{part: &usage.Part{Source: usage.SourceCodex, Err: errors.New("codex 起動失敗")}})
	o.handle(usageMsg{part: &usage.Part{Err: errors.New("claude 失敗")}})
	if o.snap != base || o.err != nil || o.inFlight {
		t.Errorf("全滅で周の始まりの表示へ戻らない: snap=%p base=%p err=%v", o.snap, base, o.err)
	}
	if o.staleErr == nil || o.staleErr.Error() != "claude 失敗\ncodex 起動失敗" {
		t.Errorf("staleErr = %v", o.staleErr)
	}

	o = usageOverlay{visible: true}
	o.fetchCmd(false)
	o.handle(usageMsg{part: &usage.Part{Err: errors.New("a")}})
	o.handle(usageMsg{part: &usage.Part{Source: usage.SourceCodex, Err: errors.New("b")}})
	if o.snap != nil || o.err == nil || o.loading() {
		t.Errorf("初回の全滅が「取得失敗」にならない: snap=%v err=%v", usageLabels(o.snap), o.err)
	}
}

// 初回の全滅の後、次の周で 1 本でも取れたらその場で「取得失敗」から回復する。
func TestUsageRecoversFromInitialErrorViaParts(t *testing.T) {
	o := usageOverlay{visible: true}
	o.fetchCmd(false)
	o.handle(usageMsg{part: &usage.Part{Err: errors.New("a")}})
	o.handle(usageMsg{part: &usage.Part{Source: usage.SourceCodex, Err: errors.New("b")}})
	o.fetchCmd(false)
	defer o.stop()
	o.handle(usageMsg{part: usagePart(usage.SourceCodex, 7)})
	if o.err != nil {
		t.Fatalf("codex が取れたのに「取得失敗」のまま: %v", o.err)
	}
	if box := strings.Join(o.boxLines(80, false, "*"), "\n"); strings.Contains(box, "取得失敗") {
		t.Errorf("箱が「取得失敗」のまま:\n%s", box)
	}
}

// 全滅した周の注記 (前回の値を表示中) は、次の周で先に届いた方では消さず、周の終わりに消す
// (まだ届いていない出所の枠は前回の値のまま)。
func TestUsageStaleNoteKeptUntilRoundEnds(t *testing.T) {
	base := (*usage.Snapshot)(nil).With(*usagePart("", 1)).With(*usagePart(usage.SourceCodex, 1))
	o := usageOverlay{visible: true, snap: base, fetchedAt: timeNow(), staleErr: errors.New("前の周の全滅")}
	o.fetchCmd(false)
	o.handle(usageMsg{part: usagePart(usage.SourceCodex, 7)})
	if o.staleErr == nil {
		t.Error("Claude の前回の値を出したまま注記を消した")
	}
	o.handle(usageMsg{part: usagePart("", 7)})
	if o.staleErr != nil {
		t.Errorf("両方取れた周の終わりに注記が消えない: %v", o.staleErr)
	}
}

// キャッシュへは今回の周で取れた分だけを書く: codex が失敗した周に、前回の codex 枠を混ぜて TTL を延ばさない。
func TestUsageCacheExcludesLastGoodOfFailedSource(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PATH", "") // codex が無い環境として読む (codex 欠損のキャッシュを miss にしない。usage_cache.go)
	base := (*usage.Snapshot)(nil).With(*usagePart("", 1)).With(*usagePart(usage.SourceCodex, 1))
	o := usageOverlay{visible: true, snap: base, fetchedAt: timeNow()}
	o.fetchCmd(false)
	o.handle(usageMsg{part: usagePart("", 5)})
	c := o.handle(usageMsg{part: &usage.Part{Source: usage.SourceCodex, Err: errors.New("codex 起動失敗")}})
	if c == nil {
		t.Fatal("Claude が取れた周なのに保存の Cmd が無い")
	}
	c()
	path, _ := usageCachePath()
	snap, ok := loadUsageCache(path, time.Now())
	if !ok || usageLabels(snap) != "5h,7d" {
		t.Errorf("キャッシュ = ok:%v %s, want 今回の Claude 枠だけ", ok, usageLabels(snap))
	}
	if w, _ := o.snap.Find("cx7d"); w.Percent != 1 {
		t.Errorf("表示からは前回の codex 枠を消さない: %+v", o.snap.Windows)
	}
}

// 定期リフレッシュは静かに差し替える: 片方を待つ間も「取得中」を出さない。表示に codex の無い環境
// (codex 未導入) でも、毎分「codex 取得中」を点滅させない。
func TestUsageRefreshDoesNotShowWaiting(t *testing.T) {
	base := (*usage.Snapshot)(nil).With(*usagePart("", 1))
	o := usageOverlay{visible: true, snap: base, fetchedAt: timeNow()}
	o.fetchCmd(false)
	defer o.stop()
	o.handle(usageMsg{part: usagePart("", 9)})
	if o.loading() || len(o.waiting()) != 0 {
		t.Errorf("リフレッシュで「取得中」を出した: %v", o.waiting())
	}
	if w, _ := o.snap.Find("5h"); w.Percent != 9 {
		t.Errorf("届いた Claude が表示へ入らない: %d", w.Percent)
	}
}

// 箱のタイトルは codex 側もバージョンを添える (Claude 側と対。ユーザー要望 2026-08-16)。
// 取得できていないとき (未導入 / --version 失敗) は名前だけの従来表記へ落ちる。
func TestUsageBoxLinesTitleShowsCodexVersion(t *testing.T) {
	snap := &usage.Snapshot{
		Version:      "2.1.216",
		CodexVersion: "0.144.6",
		Windows: []usage.Window{
			{Label: "5h", Percent: 20, ResetAt: time.Now().Add(4 * time.Hour)},
			{Label: "cx7d", Source: usage.SourceCodex, Percent: 69, ResetAt: time.Now().Add(48 * time.Hour)},
		},
	}
	ov := usageOverlay{visible: true, snap: snap}
	plain := stripANSI(strings.Join(ov.boxLines(120, false, ""), "\n"))
	if !strings.Contains(plain, "Claude Code v2.1.216 + codex v0.144.6 · usage") {
		t.Errorf("タイトルに codex バージョンが無い:\n%s", plain)
	}
	snap.CodexVersion = ""
	plain = stripANSI(strings.Join(ov.boxLines(120, false, ""), "\n"))
	if !strings.Contains(plain, "Claude Code v2.1.216 + codex · usage") {
		t.Errorf("codex バージョン欠損時に名前だけの表記へ落ちない:\n%s", plain)
	}
}

// Claude 側だけ取れなかったときは右上の箱にも注記を出す。ただし理由の長さで箱を広げない
// (全文はダッシュボードに出る)。
func TestUsageBoxShowsClaudeFetchError(t *testing.T) {
	m := newTestBrowse(t, 5, nil, nil)
	m.usageOv.visible = true
	m.usageOv.snap = &usage.Snapshot{Windows: []usage.Window{
		{Label: "5h", Percent: 4, ResetAt: time.Now().Add(time.Hour)},
	}}
	base := m.usageOv.boxLines(m.width, m.colored, m.spinner())
	if strings.Contains(stripANSI(strings.Join(base, "\n")), "取得に失敗") {
		t.Fatalf("取得できているのに注記がある:\n%s", stripANSI(strings.Join(base, "\n")))
	}
	m.usageOv.snap.ClaudeErr = "claude /usage 実行失敗: exit status 127: " + strings.Repeat("x", 300)
	box := m.usageOv.boxLines(m.width, m.colored, m.spinner())
	plain := stripANSI(strings.Join(box, "\n"))
	if !strings.Contains(plain, "Claude Code の取得に失敗 (前回の値を表示中)") {
		t.Errorf("箱に注記が無い:\n%s", plain)
	}
	if got, want := dispWidth(box[0]), dispWidth(base[0]); got != want {
		t.Errorf("理由の長さで箱の幅が変わった: %d → %d", want, got)
	}
}

// 全滅 (キャッシュ経路の err) のときは last-good を保持するが、古いことと理由を注記で出す。次の成功で消える。
func TestUsageLastGoodShowsStaleReason(t *testing.T) {
	// 描画キャッシュの鍵には秒が入る。時刻を止めないと、失敗時と回復後の描画が秒をまたいだときに
	// 鍵の秒の違いでキャッシュが外れ、鍵に理由を載せ忘れた退行を素通りする
	orig := timeNow
	timeNow = func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { timeNow = orig })
	m := newTestBrowse(t, 5, nil, nil)
	m.usageOv.visible = true
	good := &usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 4, ResetAt: time.Now().Add(time.Hour)}}}
	m.usageOv.handle(usageMsg{snap: good})
	m.usageOv.handle(usageMsg{err: errors.Join(errors.New("claude /usage 実行失敗: exit status 127"), errors.New("codex 起動失敗"))})
	if m.usageOv.snap != good {
		t.Fatal("全滅で last-good を捨てた")
	}
	box := stripANSI(strings.Join(m.usageOv.boxLines(m.width, m.colored, m.spinner()), "\n"))
	if !strings.Contains(box, "取得に失敗 (前回の値を表示中)") {
		t.Errorf("last-good を保持したのに注記が無い:\n%s", box)
	}
	var d ratelimitDash
	d.toggle()
	if head := stripANSI(d.lines(m.ratelimitOpts())[1]); !strings.Contains(head, "exit status 127 / ") {
		t.Errorf("ダッシュボードに両方の理由が区切られて出ていない: %q", head)
	}
	m.usageOv.handle(usageMsg{snap: good}) // 回復 (同じポインタでも注記は消える = 描画キャッシュの鍵に理由が要る)
	box = stripANSI(strings.Join(m.usageOv.boxLines(m.width, m.colored, m.spinner()), "\n"))
	if strings.Contains(box, "取得に失敗") {
		t.Errorf("回復後も注記が残る:\n%s", box)
	}
	if head := d.lines(m.ratelimitOpts())[1]; head != "" {
		t.Errorf("回復後もダッシュボードに注記が残る: %q", head)
	}
}

// R のダッシュボードの `r` は、全プロセス共有のゲートの 5 分の間引きを飛ばして取り直す。定期の取得は
// 間引きの内側 (claude を起こさない)。`r` が定期と同じ経路に戻ると、数分前の値が「今取った値」として
// 入り、押しても取り直されない (issue 627 の敵対的レビュー)。
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

	m := newTestBrowse(t, 2, map[string]CIState{}, nil)
	m.rlDash.shown = true
	runCmdTree(m.usageOv.fetchCmd(false)) // 共有ゲートに 5 分以内の結果を作る
	m.usageOv.inFlight = false  // handle を通していないので手で下ろす
	if got := count(); got != 1 {
		t.Fatalf("前提: claude %d 回, want 1", got)
	}
	runCmdTree(m.usageOv.fetchCmd(false)) // 定期の取得は間引かれる
	m.usageOv.inFlight = false
	if got := count(); got != 1 {
		t.Fatalf("定期の取得が 5 分以内に claude を起こした: %d 回", got)
	}
	_, cmd := m.routeKeyToRatelimitDash("r")
	runCmdTree(cmd)
	if got := count(); got != 2 {
		t.Fatalf("r で取り直さない: claude %d 回, want 2", got)
	}
}
