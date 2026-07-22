package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"glogx/usage"

	"github.com/mattn/go-runewidth"
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
		if w := runewidth.StringWidth(stripANSI(got[i])); w != width {
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
	if w := runewidth.StringWidth(stripANSI(got[0])); w != 40 {
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
	if w := runewidth.StringWidth(stripANSI(got[0])); w != 20 {
		t.Errorf("表示幅 = %d, want 20", w)
	}
}

func TestOverlayBoxTopRightEmpty(t *testing.T) {
	if got := overlayBoxTopRight(nil, []string{"x"}, 10, false); got != nil {
		t.Errorf("空ウィンドウで nil を返さない: %v", got)
	}
	window := []string{"a"}
	_ = overlayBoxTopRight(window, nil, 10, false)          // 空 box: 何もしない
	_ = overlayBoxTopRight(window, []string{"x"}, 0, false) // width0: panic しなければ OK
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

// U は tmux prefix pending を素通りせず、通常キーとして pending を消費する (次キー誤飲み込み回帰)。
func TestUsageToggleDoesNotBypassPrefixPending(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.tmuxPrefix = "ctrl+t"
	m.prefixPending = true
	m.handleKey("U")
	if m.prefixPending {
		t.Error("U が prefixPending を消費していない (次キーが誤って飲み込まれる残留)")
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
	joined := strings.Join(loading, "\n")
	if !strings.Contains(stripANSI(joined), "取得中") {
		t.Errorf("取得中 box に '取得中' が無い:\n%s", stripANSI(joined))
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
	if plain := stripANSI(strings.Join(box, "\n")); !strings.Contains(plain, "取得失敗") {
		t.Errorf("エラー box に '取得失敗' が無い:\n%s", plain)
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

// 成功時の usage box は自動更新を明示するフッター「1分ごとに更新」を末尾に出す
// (ユーザー要望 2026-07-22: バックグラウンドで 1 分ごとに更新している旨の明記)。
func TestUsageBoxLinesShowsAutoRefreshFooter(t *testing.T) {
	ov := usageOverlay{
		visible: true,
		snap:    &usage.Snapshot{Windows: []usage.Window{{Label: "5h", Percent: 20}}},
	}
	box := ov.boxLines(80, false, "|")
	plain := stripANSI(strings.Join(box, "\n"))
	if !strings.Contains(plain, "1分ごとに更新") {
		t.Errorf("usage box に自動更新フッターが無い:\n%s", plain)
	}
	// フッターは末尾付近 (データ行の後) に出る: 5h 行より後であること
	if strings.Index(plain, "1分ごとに更新") < strings.Index(plain, "5h") {
		t.Errorf("フッターがデータ行より前に出ている:\n%s", plain)
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
