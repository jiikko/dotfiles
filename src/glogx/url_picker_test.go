package main

import (
	"fmt"
	"strings"
	"testing"
)

func pickerURLs() []string {
	return []string{
		"https://example.com/alpha",
		"https://example.com/beta",
		"https://other.test/gamma",
	}
}

func TestURLPickerOpenClose(t *testing.T) {
	var p urlPicker
	if p.open(nil) || p.active {
		t.Error("URL が無いのに開いた")
	}
	if !p.open(pickerURLs()) || !p.active {
		t.Fatal("開けない")
	}
	if got := p.selected(); got != pickerURLs()[0] {
		t.Errorf("初期選択 = %q", got)
	}
	p.close()
	if p.active || p.query != "" || len(p.match) != 0 {
		t.Errorf("close で状態が残る: %+v", p)
	}
	// 閉じているときはキーを飲まない (呼び出し側の他の割当へ通す)
	if open, closed := p.handleKey("enter"); open || closed {
		t.Error("閉じているのにキーを処理した")
	}
}

func TestURLPickerIncrementalSearch(t *testing.T) {
	var p urlPicker
	p.open(pickerURLs())
	type step struct {
		key   string
		match int
		sel   string
	}
	// ⚠️ 検索語は打つたびに積まれる (fzf と同じ)。1 打ごとの query を併記する
	for i, s := range []step{
		{"e", 3, "https://example.com/alpha"},         // query "e": 3 件とも e を含む
		{"t", 1, "https://example.com/beta"},          // query "et": beta の "beta" だけ
		{"backspace", 3, "https://example.com/alpha"}, // query "e": 戻すと復帰
		{"backspace", 3, "https://example.com/alpha"}, // query "": 全件
		{"g", 1, "https://other.test/gamma"},          // query "g": gamma だけ
	} {
		p.handleKey(s.key)
		if len(p.match) != s.match || p.selected() != s.sel {
			t.Errorf("%d (%q): match=%d sel=%q, want %d/%q", i, s.key, len(p.match), p.selected(), s.match, s.sel)
		}
	}
	// ctrl+u で検索語を捨てる (絞り込んだ状態から一気に戻せる)
	p.handleKey("ctrl+u")
	if p.query != "" || len(p.match) != 3 {
		t.Errorf("ctrl+u で戻らない: query=%q match=%d", p.query, len(p.match))
	}
	// 大文字小文字を無視する
	p.handleKey("A")
	if len(p.match) == 0 {
		t.Error("大文字で一致しない (小文字化していない)")
	}
}

func TestURLPickerNavigation(t *testing.T) {
	var p urlPicker
	p.open(pickerURLs())
	p.handleKey("ctrl+n")
	if p.selected() != pickerURLs()[1] {
		t.Errorf("ctrl+n で進まない: %q", p.selected())
	}
	p.handleKey("down")
	if p.selected() != pickerURLs()[2] {
		t.Errorf("down で進まない: %q", p.selected())
	}
	p.handleKey("ctrl+n") // 末尾から先頭へ巡回
	if p.selected() != pickerURLs()[0] {
		t.Errorf("末尾から巡回しない: %q", p.selected())
	}
	p.handleKey("up") // 先頭から末尾へ巡回
	if p.selected() != pickerURLs()[2] {
		t.Errorf("先頭から逆巡回しない: %q", p.selected())
	}
}

// 一致 0 件では Enter を無視する (閉じてしまうと検索語を直せない)。
func TestURLPickerNoMatch(t *testing.T) {
	var p urlPicker
	p.open(pickerURLs())
	for _, k := range []string{"z", "z", "z"} {
		p.handleKey(k)
	}
	if len(p.match) != 0 || p.selected() != "" {
		t.Fatalf("一致するはずがない: match=%d sel=%q", len(p.match), p.selected())
	}
	if open, closed := p.handleKey("enter"); open || closed {
		t.Error("一致 0 件の Enter で確定/終了した")
	}
	out := strings.Join(p.lines(issuesRenderOpts{width: 60, page: 10}), "\n")
	if !strings.Contains(out, "一致する URL がありません") {
		t.Errorf("一致なしの案内が出ない:\n%s", out)
	}
}

func TestURLPickerEscCloses(t *testing.T) {
	var p urlPicker
	p.open(pickerURLs())
	open, closed := p.handleKey("esc")
	if open || !closed || p.active {
		t.Errorf("esc で閉じない: open=%v closed=%v active=%v", open, closed, p.active)
	}
}

// 名前付きキー・修飾キーは検索語にしない (pgdown で "pgdown" が入ると絞り込みが壊れる)。
func TestURLPickerIgnoresNamedKeys(t *testing.T) {
	var p urlPicker
	p.open(pickerURLs())
	for _, k := range []string{"pgdown", "ctrl+x", "shift+tab", "home", " "} {
		p.handleKey(k)
	}
	if p.query != "" {
		t.Errorf("名前付き/修飾キーが検索語に入った: %q", p.query)
	}
}

// 描画: ヘッダーに検索語と件数、選択行に矢印が 1 本だけ出る。幅は超えない。
func TestURLPickerLines(t *testing.T) {
	var p urlPicker
	p.open(pickerURLs())
	p.handleKey("b")
	o := issuesRenderOpts{width: 50, page: 8}
	out := p.lines(o)
	joined := stripANSI(strings.Join(out, "\n"))
	if !strings.Contains(joined, "URL 検索: b") {
		t.Errorf("検索語が出ない:\n%s", joined)
	}
	if !strings.Contains(joined, "1/3 件") {
		t.Errorf("件数が出ない:\n%s", joined)
	}
	if n := strings.Count(joined, cursorGutterMark[:1]); n != 1 {
		t.Errorf("選択の矢印が %d 本 (want 1):\n%s", n, joined)
	}
	for i, ln := range out {
		if w := dispWidth(stripANSI(ln)); w > o.width {
			t.Errorf("行 %d が幅を超えた (w=%d): %q", i, w, ln)
		}
	}
}

// カーソルが窓の外にあるときも選択行が描かれる (窓はカーソルから導出する)。
func TestURLPickerWindowFollowsCursor(t *testing.T) {
	urls := make([]string, 24)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://example.com/%s%d", strings.Repeat("x", i%3), i)
	}
	var p urlPicker
	p.open(urls)
	for range 20 {
		p.handleKey("ctrl+n")
	}
	sel := p.selected()
	out := stripANSI(strings.Join(p.lines(issuesRenderOpts{width: 60, page: 10}), "\n"))
	if !strings.Contains(out, sel) {
		t.Errorf("選択行 %q が描かれていない:\n%s", sel, out)
	}
}
