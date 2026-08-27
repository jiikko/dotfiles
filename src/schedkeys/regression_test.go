package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// 以下はユーザー報告のバグに対する回帰テスト。いずれも「実端末で見て分かった」ものなので、
// 再発を機械で捕まえられる形 (行数・カーソル位置・行幅) に落としてある。

// popup の内側 (70x14) に必ず収まること。溢れると端末が流れ、表示が二重になり、
// 本物のカーソル位置 (IME の未確定文字が出る場所) も行数分ずれる (2026-08-28 報告)。
func TestFormFitsInPopup(t *testing.T) {
	const w, h = 70, 14
	for _, tc := range []struct {
		name  string
		setup func(*model)
	}{
		{"既定 (プリセット)", func(m *model) {}},
		{"時刻欄 + エラー", func(m *model) {
			press(m, "tab", "")
			press(m, "left", "")
			press(m, "left", "")
			press(m, "enter", "")
			typeText(m, "25:00")
			press(m, "enter", "")
		}},
		{"自由入力 + 長い文字列", func(m *model) {
			press(m, "tab", "")
			press(m, "left", "")
			press(m, "enter", "")
			typeText(m, "1h30m")
			press(m, "enter", "")
			typeText(m, strings.Repeat("x", 200))
		}},
		{"長い日本語", func(m *model) { typeText(m, strings.Repeat("あ", 120)) }},
	} {
		m := newTestModel()
		m.width, m.height = w, h
		press(m, "enter", "")
		tc.setup(m)
		v := m.View()
		lines := strings.Split(v.Content, "\n")
		if len(lines) > h {
			t.Errorf("%s: %d 行 (上限 %d)", tc.name, len(lines), h)
		}
		for i, l := range lines {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("%s: %d 行目が %d 桁 (上限 %d): %q", tc.name, i, got, w, stripSGR(l))
			}
		}
		if v.Cursor != nil {
			if v.Cursor.Y >= len(lines) || v.Cursor.Y < 0 {
				t.Errorf("%s: Cursor.Y=%d が行数 %d の外", tc.name, v.Cursor.Y, len(lines))
			}
			if v.Cursor.X >= w || v.Cursor.X < 0 {
				t.Errorf("%s: Cursor.X=%d が幅 %d の外", tc.name, v.Cursor.X, w)
			}
		}
	}
}

// カーソルは「フォーカス中の欄の、入力済みの直後」に置かれること。入力欄が増減しても
// 行がずれない (自由入力で 5 行ずれた 2026-08-28 の報告)。
func TestCursorSitsOnFocusedFieldRow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(*model)
		marker string
		typed  string
	}{
		{"文字列 (入力欄なし)", func(m *model) { typeText(m, "make test") }, "文字列", "make test"},
		{"時刻欄", func(m *model) {
			press(m, "tab", "")
			press(m, "left", "")
			press(m, "left", "")
			press(m, "enter", "")
			typeText(m, "09:00")
		}, "時刻", "09:00"},
		{"自由入力欄", func(m *model) {
			press(m, "tab", "")
			press(m, "left", "")
			press(m, "enter", "")
			typeText(m, "1h30m")
		}, "あと", "1h30m"},
	} {
		m := newTestModel()
		m.width, m.height = 70, 14
		press(m, "enter", "")
		tc.setup(m)
		v := m.View()
		if v.Cursor == nil {
			t.Fatalf("%s: Cursor が nil", tc.name)
		}
		lines := strings.Split(v.Content, "\n")
		row := stripSGR(lines[v.Cursor.Y])
		if !strings.Contains(row, tc.marker) || !strings.Contains(row, tc.typed) {
			t.Errorf("%s: カーソル行 = %q (見出し %q と入力 %q がある行のはず)", tc.name, row, tc.marker, tc.typed)
		}
		// X は「見出しの幅 + 入力済みの表示幅」= 行内の入力末尾
		if want := labelCol + ansi.StringWidth(tc.typed); v.Cursor.X != want {
			t.Errorf("%s: Cursor.X = %d; want %d", tc.name, v.Cursor.X, want)
		}
	}
}

// 同じ理由 (エラーが 2 行に重複して行数が増える) の回帰。エラー表示は 1 行だけ。
func TestErrorShownOnce(t *testing.T) {
	m := newTestModel()
	press(m, "enter", "")
	press(m, "tab", "")
	press(m, "left", "")
	press(m, "left", "")
	press(m, "enter", "")
	typeText(m, "25:00")
	press(m, "enter", "")
	body := stripSGR(m.View().Content)
	if n := strings.Count(body, "時刻は HH:MM で"); n != 1 {
		t.Errorf("エラー行が %d 回出ている (1 回であるべき):\n%s", n, body)
	}
}

// 長い入力でも行が幅を超えず、カーソルが画面内に留まること (水平スクロール)。
func TestLongInputScrollsHorizontally(t *testing.T) {
	var e editor
	e.setValue(strings.Repeat("x", 200))
	shown, col := e.viewport(30, true)
	if w := ansi.StringWidth(shown); w > 30 {
		t.Errorf("表示幅 = %d (上限 30)", w)
	}
	if col > 30 || col < 0 {
		t.Errorf("カーソル列 = %d (0..30 の外)", col)
	}
	// 全角でも同じ (半端に割らない)
	e.setValue(strings.Repeat("あ", 100))
	shown, col = e.viewport(21, true)
	if w := ansi.StringWidth(shown); w > 21 {
		t.Errorf("全角の表示幅 = %d (上限 21)", w)
	}
	if col > 21 {
		t.Errorf("全角のカーソル列 = %d", col)
	}
	if strings.ContainsRune(shown, '�') {
		t.Error("全角を半端に割っている")
	}
	// 先頭付近では左端固定 (無用にスクロールしない)
	e.setValue("abc")
	shown, col = e.viewport(30, true)
	if shown != "abc" || col != 3 {
		t.Errorf("短い入力で %q col=%d (そのまま出すべき)", shown, col)
	}
}

// 一覧の行も幅を超えないこと (折り返すと選択の反転が崩れ、行数も増える)。
func TestPickRowsFitWidth(t *testing.T) {
	n := time.Now()
	jobs := []job{
		{id: "a", at: n.Add(time.Minute), label: "main:3 クロード実況中", text: strings.Repeat("y", 200)},
		{id: "b", at: n.Add(time.Hour), label: "w:1 zsh", text: "short"},
	}
	m := newModel("main:3 claude", n, jobs)
	m.width, m.height = 70, 14
	press(m, "j", "")
	press(m, "enter", "")
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) > 14 {
		t.Errorf("%d 行 (上限 14)", len(lines))
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got > 70 {
			t.Errorf("%d 行目が %d 桁: %q", i, got, stripSGR(l))
		}
	}
}

// 選択中の候補・欄が「太字だけ」でなく色と記号でも分かること (2026-08-28 の指摘)。
func TestSelectionIsVisibleWithoutBoldOnly(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 70, 14
	press(m, "enter", "")
	press(m, "tab", "") // いつ にフォーカス
	body := m.View().Content
	plain := stripSGR(body)
	if !strings.Contains(plain, "[5分後]") {
		t.Errorf("選択中の候補が [ ] で囲まれていない:\n%s", plain)
	}
	if !strings.Contains(body, "\x1b["+revAccent) {
		t.Error("選択中の候補に色 (反転) が付いていない")
	}
	whenRow := ""
	for _, l := range strings.Split(plain, "\n") {
		if strings.Contains(l, "いつ") {
			whenRow = l
		}
	}
	if !strings.HasPrefix(whenRow, "> ") {
		t.Errorf("フォーカス中の欄に > が無い: %q", whenRow)
	}
	// メニューも同じ規律
	m2 := newTestModel()
	m2.width, m2.height = 70, 14
	menu := m2.View().Content
	if !strings.Contains(menu, "\x1b["+revAccent) || !strings.Contains(stripSGR(menu), "> 新規予約") {
		t.Errorf("メニューの選択行が色と > で示されていない:\n%s", menu)
	}
}

// Emacs 風のキーでも欄と候補を移動できること (ユーザー要望 2026-08-28)。
func TestEmacsKeysMoveFocusAndChips(t *testing.T) {
	m := newTestModel()
	press(m, "enter", "")
	if m.form.focus != focusText {
		t.Fatal("初期フォーカスが文字列でない")
	}
	m.Update(ctrlKey('n'))
	if m.form.focus != focusWhen {
		t.Errorf("C-n で欄が移らない (focus=%v)", m.form.focus)
	}
	m.Update(ctrlKey('f'))
	if got := m.form.current().label; got != "10分後" {
		t.Errorf("C-f で候補が進まない (%q)", got)
	}
	m.Update(ctrlKey('b'))
	if got := m.form.current().label; got != "5分後" {
		t.Errorf("C-b で候補が戻らない (%q)", got)
	}
	m.Update(ctrlKey('p'))
	if m.form.focus != focusText {
		t.Errorf("C-p で欄が戻らない (focus=%v)", m.form.focus)
	}
	// 文字列欄では C-f / C-b は文字移動 (欄移動に横取りされない)
	typeText(m, "ab")
	m.Update(ctrlKey('b'))
	if m.form.text.pos != 1 {
		t.Errorf("文字列欄の C-b が文字移動でない (pos=%d)", m.form.text.pos)
	}
	if m.form.focus != focusText {
		t.Error("文字列欄の C-b で欄が移った")
	}
}

// メニューと一覧も C-n / C-p で動くこと。
func TestEmacsKeysMoveMenuAndPick(t *testing.T) {
	jobs := []job{
		{id: "a", at: time.Now().Add(time.Minute), label: "x", text: "1"},
		{id: "b", at: time.Now().Add(time.Hour), label: "y", text: "2"},
	}
	m := newModel("main:3 claude", time.Now(), jobs)
	m.Update(ctrlKey('n'))
	if m.menuIdx != 1 {
		t.Errorf("メニューが C-n で動かない (idx=%d)", m.menuIdx)
	}
	m.Update(ctrlKey('p'))
	if m.menuIdx != 0 {
		t.Errorf("メニューが C-p で戻らない (idx=%d)", m.menuIdx)
	}
	m.Update(ctrlKey('n'))
	press(m, "enter", "")
	if m.screen != screenPick {
		t.Fatal("一覧に入れない")
	}
	m.Update(ctrlKey('n'))
	if m.pickIdx != 1 {
		t.Errorf("一覧が C-n で動かない (idx=%d)", m.pickIdx)
	}
	m.Update(ctrlKey('p'))
	if m.pickIdx != 0 {
		t.Errorf("一覧が C-p で戻らない (idx=%d)", m.pickIdx)
	}
}

// 起動キーの再入力で閉じられること (トグル。ユーザー要望 2026-08-28)。
// popup が開いている間 tmux の prefix はキーテーブルへ届かずこの UI に素通りするので、
// prefix + m / Enter / C-m を自前で受けて中止扱いにする。
func TestTogglePrefixCloses(t *testing.T) {
	for _, second := range []string{"m", "enter", "ctrl+m"} {
		m := newTestModel()
		m.togglePrefix = "ctrl+t"
		press(m, "enter", "") // フォームへ
		typeText(m, "make test")
		m.Update(ctrlKey('t'))
		if m.quit {
			t.Fatalf("%s: prefix だけで閉じた", second)
		}
		switch second {
		case "ctrl+m":
			m.Update(ctrlKey('m'))
		default:
			press(m, second, "")
		}
		if !m.quit {
			t.Errorf("prefix + %s で閉じない", second)
		}
		if m.res.action != "" {
			t.Errorf("prefix + %s で予約が作られた (res=%+v)", second, m.res)
		}
	}
}

// prefix に続く別のキーは取りこぼさず、通常どおり処理されること。
func TestTogglePrefixDoesNotSwallowOtherKeys(t *testing.T) {
	m := newTestModel()
	m.togglePrefix = "ctrl+t"
	press(m, "enter", "")
	m.Update(ctrlKey('t'))
	typeText(m, "a")
	if got := m.form.text.value(); got != "a" {
		t.Errorf("prefix の次のキーが落ちた (text=%q)", got)
	}
	if m.quit {
		t.Error("閉じてしまった")
	}
	// 続けて m を押しても (prefix は解除済みなので) 閉じない
	typeText(m, "m")
	if m.quit {
		t.Error("prefix 解除後の m で閉じた")
	}
	if got := m.form.text.value(); got != "am" {
		t.Errorf("text=%q; want am", got)
	}
}

// prefix を渡していないときは、prefix キーが特別扱いされないこと。
func TestNoTogglePrefixMeansNoSpecialKey(t *testing.T) {
	m := newTestModel()
	press(m, "enter", "")
	m.Update(ctrlKey('t'))
	press(m, "m", "m")
	if m.quit {
		t.Error("--toggle-prefix 未指定なのに閉じた")
	}
}

func TestTeaKeyName(t *testing.T) {
	cases := map[string]string{"C-t": "ctrl+t", "C-b": "ctrl+b", "M-x": "alt+x", "": "", "Space": "space", "C-Space": "ctrl+space"}
	for in, want := range cases {
		if got := teaKeyName(in); got != want {
			t.Errorf("teaKeyName(%q) = %q; want %q", in, got, want)
		}
	}
}
