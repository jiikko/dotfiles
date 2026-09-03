package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

var now = time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)

// press はキー入力を 1 つ流す。text は IME 確定文字列を含む「そのキーが生む文字」。
// キー名は本番と同じ表記 ("ctrl+n" / "shift+tab" / "a") で書ける。
// 🚨 修飾キー専用の別経路を作らないこと: 以前は ctrlKey() が press を迂回していたため、
//
//	press(m, "ctrl+n", "") と書くと **c の打鍵**になり、名前と無関係な理由で通っていた。
func press(m *model, key, text string) {
	code, mod := parseKey(key)
	m.Update(tea.KeyPressMsg{Code: code, Text: text, Mod: mod})
}

// parseKey は "ctrl+n" のような表記を bubbletea の Code / Mod に直す
// (この 2 つが揃って初めて String() が元の表記に戻る)。
func parseKey(key string) (rune, tea.KeyMod) {
	var mod tea.KeyMod
	for {
		switch {
		case strings.HasPrefix(key, "ctrl+"):
			mod |= tea.ModCtrl
			key = key[len("ctrl+"):]
		case strings.HasPrefix(key, "alt+"):
			mod |= tea.ModAlt
			key = key[len("alt+"):]
		case strings.HasPrefix(key, "shift+"):
			mod |= tea.ModShift
			key = key[len("shift+"):]
		default:
			return keyCode(key), mod
		}
	}
}

func keyCode(key string) rune {
	switch key {
	case "enter":
		return tea.KeyEnter
	case "esc":
		return tea.KeyEsc
	case "tab":
		return tea.KeyTab
	case "left":
		return tea.KeyLeft
	case "right":
		return tea.KeyRight
	case "up":
		return tea.KeyUp
	case "down":
		return tea.KeyDown
	case "backspace":
		return tea.KeyBackspace
	case "space":
		return tea.KeySpace
	default:
		return []rune(key)[0]
	}
}

// finishToast は確定後のトーストを最後まで進める (実時間を待たない)。
// 予約が決まってから閉じるまでの間にトーストを見せる作りなので、確定を確かめるテストは
// これを通してから quit を見る。
func finishToast(m *model) {
	for range toastFrames + 2 {
		if !m.toast.shown || m.toast.done {
			break
		}
		m.Update(toastTickMsg{})
	}
	m.Update(toastDoneMsg{})
}

// typeText は文字列を 1 文字ずつ流す (IME の確定は「複数文字がまとめて来る」ので別途)
func typeText(m *model, s string) {
	for _, r := range s {
		press(m, string(r), string(r))
	}
}

// newTestModel は既定の見た目 (popup の内側 70x14) でモデルを作る。
// 🚨 テストで newModel を直に呼ばないこと: 時計の固定を書き忘れた 3 本が実時刻で走っていた
//
//	(監査 2026-08-28)。ここを唯一の入口にする。
func newTestModel(jobs ...job) *model { return newTestModelAt("main:3 claude", 70, 14, jobs...) }

// newTestModelAt はラベルと画面サイズを指定してモデルを作る (時計は必ず固定する)。
func newTestModelAt(label string, w, h int, jobs ...job) *model {
	m := newModel(label, now, jobs)
	m.nowFn = func() time.Time { return now }
	m.width, m.height = w, h
	return m
}

// focusWhenRow は「いつ」の行へ戻す (どの欄に居ても)。
func focusWhenRow(t *testing.T, m *model) {
	t.Helper()
	for range len(presets) + 3 {
		if m.form.focus == focusWhen {
			return
		}
		press(m, "tab", "")
	}
	t.Fatal("いつ の行へ移れない")
}

// focusTextField は文字列欄へフォーカスする (開いた直後は「いつ」なので、打つ前に呼ぶ)。
func focusTextField(t *testing.T, m *model) {
	t.Helper()
	for range len(presets) + 3 {
		if m.form.focus == focusText {
			return
		}
		press(m, "tab", "")
	}
	t.Fatal("文字列欄へ移れない")
}

// openForm はメニューからフォームを開き、文字列欄へフォーカスする (打ち始められる状態)。
func openForm(t *testing.T, m *model) {
	t.Helper()
	press(m, "enter", "")
	if m.screen != screenForm {
		t.Fatalf("フォームに入れていない (screen=%v)", m.screen)
	}
	focusTextField(t, m)
}

// focusSpecOfKind は「いつ」の候補を目的の種類まで進め、その入力欄へフォーカスする。
// 🚨 press(tab); press(left); press(left) と位置で書かないこと: 候補を 1 つ足すだけで
//
//	無関係なテストが 11 本落ちる (監査 2026-08-28)。並びでなく種類で探す。
func focusSpecOfKind(t *testing.T, m *model, k presetKind) {
	t.Helper()
	focusWhenRow(t, m)
	for range presets {
		if m.form.current().kind == k {
			press(m, "enter", "") // 入力欄へ
			if m.form.focus != focusSpec {
				t.Fatalf("入力欄に入れていない (focus=%v)", m.form.focus)
			}
			return
		}
		press(m, "right", "")
	}
	t.Fatalf("候補に kind=%v が無い", k)
}

func TestMenuToFormAndPresetReservation(t *testing.T) {
	m := newTestModel()
	press(m, "enter", "") // 新規予約
	if m.screen != screenForm {
		t.Fatalf("screen = %v; want form", m.screen)
	}
	// 開いた直後は「いつ」。Enter で次の欄へ進み、文字列欄で確定する
	if m.form.focus != focusWhen {
		t.Fatalf("開いた直後の focus = %v; want when", m.form.focus)
	}
	press(m, "enter", "") // いつ → 文字列
	typeText(m, "make test")
	press(m, "enter", "")
	if m.res.action != "new" {
		t.Fatalf("res = %+v", m.res)
	}
	if !m.toast.shown {
		t.Error("確定後にトーストが出ていない")
	}
	finishToast(m)
	if !m.quit {
		t.Fatal("トーストの後に閉じない")
	}
	if want := now.Add(5 * time.Minute); !m.res.at.Equal(want) {
		t.Errorf("at = %v; want %v (既定は先頭の候補 = 5分後)", m.res.at, want)
	}
	if m.res.text != "make test" {
		t.Errorf("text = %q", m.res.text)
	}
}

func TestPresetSelectionMovesFireTime(t *testing.T) {
	m := newTestModel()
	press(m, "enter", "")
	if m.form.focus != focusWhen {
		t.Fatalf("開いた直後の focus = %v; want when", m.form.focus)
	}
	press(m, "right", "")
	press(m, "right", "")
	press(m, "right", "")
	press(m, "right", "") // 5分 → 1時間後
	if got := m.form.current().label; got != "1時間後" {
		t.Fatalf("候補 = %q; want 1時間後", got)
	}
	press(m, "tab", "")
	typeText(m, "x")
	press(m, "enter", "")
	finishToast(m)
	if want := now.Add(time.Hour); !m.res.at.Equal(want) {
		t.Errorf("at = %v; want %v", m.res.at, want)
	}
}

func TestClockAndFreeSpec(t *testing.T) {
	// 時刻: 過ぎていれば翌日
	m := newTestModel()
	press(m, "enter", "")
	focusSpecOfKind(t, m, kindClock)
	typeText(m, "09:00")
	press(m, "tab", "") // → text
	typeText(m, "y")
	press(m, "enter", "")
	finishToast(m)
	want := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)
	if !m.res.at.Equal(want) {
		t.Errorf("時刻指定 at = %v; want %v", m.res.at, want)
	}

	// 自由入力
	m = newTestModel()
	press(m, "enter", "")
	focusSpecOfKind(t, m, kindFree)
	typeText(m, "1h30m")
	press(m, "tab", "")
	typeText(m, "y")
	press(m, "enter", "")
	finishToast(m)
	if want := now.Add(90 * time.Minute); !m.res.at.Equal(want) {
		t.Errorf("自由入力 at = %v; want %v", m.res.at, want)
	}
}

func TestInvalidInputKeepsForm(t *testing.T) {
	for _, tc := range []struct{ name, spec, text string }{
		{"空文字", "", ""},
		{"不正な時刻", "25:00", "make test"},
	} {
		m := newTestModel()
		press(m, "enter", "")
		if tc.spec != "" {
			focusSpecOfKind(t, m, kindClock)
			typeText(m, tc.spec)
			press(m, "tab", "")
		}
		focusTextField(t, m)
		typeText(m, tc.text)
		press(m, "enter", "")
		if m.quit || m.res.action != "" {
			t.Errorf("%s: 確定してしまった (res=%+v)", tc.name, m.res)
		}
		if m.form.err == "" {
			t.Errorf("%s: エラーが出ていない", tc.name)
		}
	}
}

func TestEscAndCtrlCDoNotReserve(t *testing.T) {
	m := newTestModel()
	openForm(t, m)
	typeText(m, "make test")
	// Esc は欄を 1 つずつ戻り、先頭 (いつ) まで来てからメニューへ降りる
	// (欄ごとの挙動は TestEscapeStepsBackThroughFields)
	press(m, "esc", "")
	press(m, "esc", "")
	if m.screen != screenMenu || m.res.action != "" {
		t.Errorf("Esc でフォームを抜けない (screen=%v res=%+v)", m.screen, m.res)
	}
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.quit || m.res.action != "" {
		t.Errorf("Ctrl-C が中止にならない (res=%+v)", m.res)
	}
}

func TestPickReturnsSelectedID(t *testing.T) {
	jobs := []job{
		{id: "a", at: now.Add(10 * time.Minute), label: "main:3 claude", text: "make test"},
		{id: "b", at: now.Add(10 * time.Minute), label: "main:3 claude", text: "make test"}, // 表示が同じ 2 件
	}
	m := newTestModel(jobs...)
	press(m, "j", "") // 予約一覧へ
	press(m, "enter", "")
	if m.screen != screenPick {
		t.Fatalf("screen = %v; want pick", m.screen)
	}
	press(m, "j", "") // 2 件目
	press(m, "enter", "")
	if m.res.action != "cancel" || m.res.id != "b" {
		t.Errorf("res = %+v; want cancel b (表示が同じでも選んだ行の id)", m.res)
	}
}

// IME の未確定文字は「本物のカーソルが居る場所」に出る。フォーカス中の入力欄にカーソルを
// 置いていることを View から確かめる (これが無いと gum と同じズレが再発する)。
func TestViewPlacesRealCursorAtFocusedField(t *testing.T) {
	m := newTestModel()
	openForm(t, m)
	typeText(m, "ab")
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("フォーム表示で Cursor が nil (IME の未確定文字が別の場所に出る)")
	}
	lines := strings.Split(v.Content, "\n")
	if v.Cursor.Y < 0 || v.Cursor.Y >= len(lines) {
		t.Fatalf("Cursor.Y = %d (行数 %d)", v.Cursor.Y, len(lines))
	}
	if got := lines[v.Cursor.Y]; !strings.Contains(got, "ab") {
		t.Errorf("カーソル行 = %q; 入力中の行でない", got)
	}
	if want := labelCol + 2; v.Cursor.X != want {
		t.Errorf("Cursor.X = %d; want %d (見出しの幅 + 入力の表示幅)", v.Cursor.X, want)
	}
	// 全角を打つと 1 文字 2 セル進む
	press(m, "あ", "あ")
	v = m.View()
	if want := labelCol + 2 + 2; v.Cursor.X != want {
		t.Errorf("全角入力後の Cursor.X = %d; want %d", v.Cursor.X, want)
	}
	// メニュー画面ではカーソルを出さない
	press(m, "esc", "")
	if m.View().Cursor != nil {
		t.Error("メニューで Cursor が出ている")
	}
}

func TestIMECommitArrivesAsMultiRuneText(t *testing.T) {
	// IME 確定は「複数 rune が 1 つの入力として来る」。1 文字ずつでない経路も受けること
	m := newTestModel()
	openForm(t, m)
	m.Update(tea.KeyPressMsg{Code: 'あ', Text: "こんにちは"})
	if got := m.form.text.value(); got != "こんにちは" {
		t.Errorf("IME 確定文字列 = %q; want こんにちは", got)
	}
}

func TestFormatResultEscapesTabAndNewline(t *testing.T) {
	r := result{action: "new", at: now, text: "echo\ta\nb"}
	line := formatResult(r)
	if strings.Count(line, "\t") != 2 {
		t.Errorf("結果行のタブが 2 つでない: %q", line)
	}
	if strings.Contains(line, "\n") {
		t.Errorf("結果行に改行が残っている: %q", line)
	}
	if got := formatResult(result{}); got != "" {
		t.Errorf("中止の結果 = %q; want 空", got)
	}
	if got := formatResult(result{action: "cancel", id: "x-1"}); got != "cancel\tx-1" {
		t.Errorf("cancel 行 = %q", got)
	}
}

// Enter は「いつ」と入力欄では次の欄へ進み、予約は最後の欄 (文字列) でだけ確定する。
// 候補を選んだ流れのまま Enter を押して意図せず予約されるのを避ける (ユーザー要望 2026-08-28)。
func TestEnterAdvancesInsteadOfSubmitting(t *testing.T) {
	m := newTestModel()
	press(m, "enter", "") // メニュー → フォーム
	press(m, "tab", "")   // 文字列 → いつ
	press(m, "enter", "")
	if m.quit || m.res.action != "" {
		t.Fatalf("候補行の Enter で予約された (res=%+v)", m.res)
	}
	if m.form.focus != focusText {
		t.Fatalf("focus = %v; want text (入力欄が無ければ文字列へ)", m.form.focus)
	}
	typeText(m, "make test")
	press(m, "enter", "")
	finishToast(m)
	if !m.quit || m.res.action != "new" {
		t.Fatalf("文字列欄の Enter で予約されない (res=%+v)", m.res)
	}

	// 時刻: いつ → 時刻欄 → 文字列 と Enter だけで進める
	m = newTestModel()
	press(m, "enter", "")
	focusSpecOfKind(t, m, kindClock)
	if m.form.focus != focusSpec {
		t.Fatalf("focus = %v; want spec (入力欄があればそこへ)", m.form.focus)
	}
	if m.quit {
		t.Fatal("時刻の候補で予約が確定した")
	}
	typeText(m, "09:00")
	press(m, "enter", "")
	if m.form.focus != focusText || m.quit {
		t.Fatalf("時刻欄の Enter で文字列へ進まない (focus=%v quit=%v)", m.form.focus, m.quit)
	}
	typeText(m, "y")
	press(m, "enter", "")
	finishToast(m)
	want := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)
	if !m.quit || !m.res.at.Equal(want) {
		t.Errorf("Enter だけの流れで予約できない (res=%+v want at=%v)", m.res, want)
	}
}

func TestEnterDoesNotLeaveInvalidSpec(t *testing.T) {
	m := newTestModel()
	press(m, "enter", "")
	focusSpecOfKind(t, m, kindClock)
	typeText(m, "25:00")
	press(m, "enter", "")
	if m.form.focus != focusSpec {
		t.Errorf("focus = %v; want spec (不正な入力のまま次へ行かせない)", m.form.focus)
	}
	if m.form.err == "" {
		t.Error("理由が出ていない")
	}
	if m.quit {
		t.Error("不正な入力で予約が確定した")
	}
	for range 5 {
		press(m, "backspace", "")
	}
	typeText(m, "09:00")
	press(m, "enter", "")
	if m.form.focus != focusText {
		t.Errorf("直した後も進めない (focus=%v)", m.form.focus)
	}
}

func TestHelpLineShowsWhatEnterDoes(t *testing.T) {
	m := newTestModel()
	press(m, "enter", "")
	// 開いた直後は「いつ」= Enter は次へ
	if got := stripSGR(m.View().Content); !strings.Contains(got, "Enter 次へ") {
		t.Error("候補行で 'Enter 次へ' が出ていない")
	}
	focusTextField(t, m)
	if got := stripSGR(m.View().Content); !strings.Contains(got, "Enter 予約") {
		t.Error("文字列欄で 'Enter 予約' が出ていない")
	}
}
