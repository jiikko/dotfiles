package main

// glogx の入力欄 (issues の番号の絞り込み・URL ピッカー) が tuikit/lineedit の編集キーで動き、端末のカーソル
// (IME が変換中の文字を出す位置) を検索語のキャレットに置くことを固定する (docs/glogx-ui-guide.md §7)。

import (
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"

	"glogx/issues"
)

func typeKeys(m *browseModel, keys ...string) {
	for _, k := range keys {
		m.handleKey(k)
		releaseKey(m) // 同じキーの連打をキーリピートとして飲ませない
	}
}

func caretTestModel(t *testing.T, frame bool) *browseModel {
	t.Helper()
	m := newTestBrowse(t, 1, nil, nil)
	m.showFrame, m.width, m.height = frame, 100, 30
	m.usageOv.visible = false // 起動時の残量のグランスは左上の入力欄に重なりうるので、caret は隠す (別の主張)
	m.issuesOv = *loadedView(
		fakeIssue("123", "feat", "a", issues.StatusOpen),
		fakeIssue("129", "feat", "b", issues.StatusOpen),
		fakeIssue("045", "bug", "c", issues.StatusOpen),
	)
	return m
}

// caretPrefix は描画した画面のうち、端末のカーソルより左にある文字列 (カーソルの行の頭から x 桁)。
// カーソルは View().Cursor から読む (caret() を直接呼ぶと、View への配線を消されても気づけない)。
func caretPrefix(t *testing.T, m *browseModel) string {
	t.Helper()
	c := m.View().Cursor
	if c == nil {
		t.Fatal("入力中なのに端末のカーソルが無い (IME の変換中の文字が入力欄の外に出る)")
	}
	rows := strings.Split(stripANSI(m.viewLines()), "\n")
	if c.Y < 0 || c.Y >= len(rows) {
		t.Fatalf("カーソルの行 %d が画面 (%d 行) の外", c.Y, len(rows))
	}
	return termwidth.Truncate(rows[c.Y], c.X, "")
}

func TestNumberFilterCaretFollowsLineEdit(t *testing.T) {
	for _, frame := range []bool{false, true} {
		m := caretTestModel(t, frame)
		typeKeys(m, "/", "1", "2", "left", "9") // 12 の 1 と 2 の間に 9 を入れる
		if q := m.issuesOv.numFilter.query(); q != "192" {
			t.Fatalf("frame=%v: 途中に入らない: query=%q (期待 192)", frame, q)
		}
		if got := caretPrefix(t, m); !strings.HasSuffix(got, numberFilterPrompt+"19") {
			t.Errorf("frame=%v: カーソルが 9 の直後に無い: カーソルの左 = %q", frame, got)
		}
		typeKeys(m, "ctrl+k") // カーソルの後ろ (2) を消す
		if q := m.issuesOv.numFilter.query(); q != "19" {
			t.Errorf("frame=%v: ctrl+k で後ろが消えない: query=%q", frame, q)
		}
		typeKeys(m, "a") // 数字以外は入れない
		if q := m.issuesOv.numFilter.query(); q != "19" {
			t.Errorf("frame=%v: 数字以外が入った: query=%q", frame, q)
		}
		typeKeys(m, "enter") // 確定すると入力欄ではなくなる
		if c := m.caret(); c != nil {
			t.Errorf("frame=%v: 確定した後もカーソルを置いている: %+v", frame, c)
		}
	}
}

func TestURLPickerCaretFollowsLineEdit(t *testing.T) {
	for _, frame := range []bool{false, true} {
		m := caretTestModel(t, frame)
		if !m.issuesOv.urlPick.open([]string{"https://example.com/abc", "https://github.com/x"}) {
			t.Fatal("前提が崩れた: URL ピッカーが開かない")
		}
		typeKeys(m, "down")                     // 2 本目を選んでから打つ: 絞り込み直後は先頭へ戻す (fzf と同じ)
		typeKeys(m, "e", "x", "m", "home", "g") // 先頭へ戻って g を入れる
		if q := m.issuesOv.urlPick.query(); q != "gexm" {
			t.Fatalf("frame=%v: 先頭に入らない: query=%q (期待 gexm)", frame, q)
		}
		if got := caretPrefix(t, m); !strings.HasSuffix(got, urlPickerPrompt+"g") {
			t.Errorf("frame=%v: カーソルが g の直後に無い: カーソルの左 = %q", frame, got)
		}
		typeKeys(m, "end", "ctrl+w") // 末尾の語を消す
		if q := m.issuesOv.urlPick.query(); q != "" {
			t.Errorf("frame=%v: ctrl+w で語が消えない: query=%q", frame, q)
		}
		typeKeys(m, "esc")
		if c := m.caret(); c != nil {
			t.Errorf("frame=%v: ピッカーを閉じた後もカーソルを置いている: %+v", frame, c)
		}
	}
}

// 開く演出の最中は行がずれるので置かない (置くと IME の文字が演出中の行に飛ぶ)。
func TestCaretHiddenWhileIssuesViewerAnimates(t *testing.T) {
	m := caretTestModel(t, false)
	typeKeys(m, "/", "1")
	if m.caret() == nil {
		t.Fatal("前提が崩れた: 入力中なのにカーソルが無い")
	}
	m.issuesOv.animStart = timeNow()
	if c := m.caret(); c != nil {
		t.Errorf("演出の最中にカーソルを置いている: %+v", c)
	}
}

// 検索語が欄より長くなっても、キャレットは欄の中 (打った字の直後) に居る。前を切るのは lineedit.Line.Window。
func TestCaretStaysInFieldWithLongQuery(t *testing.T) {
	const width = 40
	t.Run("URL", func(t *testing.T) {
		m := caretTestModel(t, false)
		m.width = width
		m.issuesOv.urlPick.open([]string{"https://example.com/abc"})
		q := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMN" // 同じ並びが 2 度出ない (ずれた桁で偶然一致しない)
		for _, r := range q {
			typeKeys(m, string(r))
		}
		got := caretPrefix(t, m)
		if !strings.HasPrefix(got, urlPickerPrompt) || !strings.HasSuffix(got, "LMN") {
			t.Errorf("長い検索語でカーソルが打った字の直後に無い: カーソルの左 = %q", got)
		}
	})
	t.Run("番号", func(t *testing.T) {
		m := caretTestModel(t, false)
		m.width = width
		typeLongNumber(t, m)
	})
	// 🚨 枠ありでも見る: 枠なしでは caret.At が画面の右端へ寄せるので、欄で切らない旧い式でも同じ桁になり区別できない
	// (2 周目の敵対レビューが実測)。枠ありでは旧い式だと右の罫線と影の外に出る。frameMinWidth 以上の幅にする
	t.Run("番号・枠あり", func(t *testing.T) {
		m := caretTestModel(t, true)
		m.width = 70
		if !m.frameActive() {
			t.Fatal("前提が崩れた: 枠が出ていない")
		}
		typeLongNumber(t, m)
		if c := m.View().Cursor; c.X >= frameContentLeft+m.contentWidth() {
			t.Errorf("カーソルが中身の右端より外: X=%d (中身は %d 桁目まで)", c.X, frameContentLeft+m.contentWidth()-1)
		}
	})
}

func typeLongNumber(t *testing.T, m *browseModel) {
	t.Helper()
	{
		typeKeys(m, "/")
		// 81 桁: 枠ありの幅 70 (中身 63 桁) でも欄より長くする。短いと欄で切らない旧い式でも収まり、差が出ない (変異で実測)
		for range 8 {
			typeKeys(m, "1", "2", "3", "4", "5", "6", "7", "8", "9", "0")
		}
		typeKeys(m, "5")
		got := caretPrefix(t, m)
		if !strings.Contains(got, numberFilterPrompt) || !strings.HasSuffix(got, "78905") {
			t.Errorf("長い番号でカーソルが打った字の直後に無い: カーソルの左 = %q", got)
		}
	}
}

// 入力欄が無い・入力欄の上に何かが重なっている間は、端末のカーソルを置かない (IME の変換中の文字が別の所に出る)。
// 条件を 1 つずつ作り、その条件だけでカーソルが消えることを見る。
func TestCaretHiddenWhenFieldIsCoveredOrGone(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(m *browseModel)
	}{
		{"glogx を終える", func(m *browseModel) { m.done = true }},
		{"issues viewer を閉じた", func(m *browseModel) { m.issuesOv.shown = false }},
		{"窓の zoom の演出中", func(m *browseModel) { m.zoom.off = false; m.zoom.start(timeNow()) }},
		{"action モーダル", func(m *browseModel) { m.actModal.pullConfirm = true }},
		{"再起動の確認", func(m *browseModel) { m.restartPending = true }},
		{"usage の板", func(m *browseModel) { m.usageOv.visible = true }},
		{"next の目印の確認", func(m *browseModel) { m.issuesOv.markNext.active = true }},
		{"本文を開いている", func(m *browseModel) {
			if !m.issuesOv.openIssue(realIssue(t)) {
				t.Fatal("前提が崩れた: 本文を開けない")
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := caretTestModel(t, true)
			typeKeys(m, "/", "1")
			if m.View().Cursor == nil {
				t.Fatal("前提が崩れた: 入力中なのにカーソルが無い")
			}
			c.set(m)
			if cur := m.View().Cursor; cur != nil {
				t.Errorf("%s の間もカーソルを置いている: %+v", c.name, cur)
			}
		})
	}
}

// 入力欄に打っている間は、同じ字を素早く続けて打っても飲まない (https の ss)。キーリピートの抑止は入力欄の外だけ。
func TestRepeatGuardDoesNotSwallowTypedLetters(t *testing.T) {
	m := caretTestModel(t, false)
	m.issuesOv.urlPick.open([]string{"https://example.com/abc"})
	for _, k := range []string{"s", "s", "i", "i", "a", "a"} { // releaseKey を挟まない = 素早い連打
		m.handleKey(k)
	}
	if q := m.issuesOv.urlPick.query(); q != "ssiiaa" {
		t.Errorf("連打の 2 字目が飲まれた: query=%q (期待 ssiiaa)", q)
	}
}

// 打った直後は選択を先頭へ戻す (fzf と同じ)。🚨 打った字に両方の URL が一致する入力にする: 一致が 1 件に減ると
// refilter が選択を範囲へ寄せて 0 になり、戻す処理を消しても緑のまま通る (変異で実測)。
func TestURLPickerTypingResetsSelection(t *testing.T) {
	m := caretTestModel(t, false)
	m.issuesOv.urlPick.open([]string{"https://a.example/1", "https://b.example/2"})
	typeKeys(m, "down")
	if m.issuesOv.urlPick.cursor != 1 {
		t.Fatalf("前提が崩れた: 2 本目を選べない: cursor=%d", m.issuesOv.urlPick.cursor)
	}
	typeKeys(m, "x") // どちらの URL にも x がある
	if n := len(m.issuesOv.urlPick.match); n != 2 {
		t.Fatalf("前提が崩れた: 絞り込みで一致が %d 件 (2 件のままでないと戻す処理を検査できない)", n)
	}
	if m.issuesOv.urlPick.cursor != 0 {
		t.Errorf("打った後に選択が先頭へ戻らない: cursor=%d", m.issuesOv.urlPick.cursor)
	}
	typeKeys(m, "down", "left") // カーソルを動かすだけのキーでは選択を動かさない
	if m.issuesOv.urlPick.cursor != 1 {
		t.Errorf("検索語のカーソルを動かしただけで選択が動いた: cursor=%d", m.issuesOv.urlPick.cursor)
	}
}

// 入力欄の外 (一覧) では、押しっぱなしの n のリピートを今までどおり飲む。飲まないと 1 打目で開いた目印の確認を
// 2 打目のリピートが取り消す (markNextKey は y/Enter 以外を取り消しに倒す)。入力欄の除外を広げすぎない (ownsKeys
// は確認中も真なので使わない) ことを固定する。
func TestRepeatGuardStillHoldsNConfirmOutsideInputs(t *testing.T) {
	m := caretTestModel(t, false)
	m.handleKey("n")
	if !m.issuesOv.markNext.active {
		t.Fatal("前提が崩れた: n で目印の確認が開かない")
	}
	m.handleKey("n") // releaseKey を挟まない = 押しっぱなしのリピート
	if !m.issuesOv.markNext.active {
		t.Error("押しっぱなしの n のリピートが目印の確認を取り消した")
	}
}
