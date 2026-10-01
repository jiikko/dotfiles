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
func caretPrefix(t *testing.T, m *browseModel) string {
	t.Helper()
	c := m.caret()
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
