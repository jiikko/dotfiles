package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

// 以下はユーザー報告のバグに対する回帰テスト。いずれも「実端末で見て分かった」ものなので、
// 再発を機械で捕まえられる形 (行数・カーソル位置・行幅) に落としてある。

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
			focusSpecOfKind(t, m, kindClock)
			typeText(m, "09:00")
		}, "時刻", "09:00"},
		{"自由入力欄", func(m *model) {
			focusSpecOfKind(t, m, kindFree)
			typeText(m, "1h30m")
		}, "あと", "1h30m"},
	} {
		m := newTestModel()
		m.width, m.height = 70, 14
		openForm(t, m) // 文字列欄まで進む (入力欄のあるケースは setup が入り直す)
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
	focusSpecOfKind(t, m, kindClock)
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
	// ⚠️ col の範囲だけを見ない: viewport 自身が最後に clamp するので、スクロールを丸ごと消しても
	//    その assert は通る (監査 2026-08-28)。「末尾が見えていて、カーソルがその末尾にある」で見る
	if col != 29 {
		t.Errorf("カーソル列 = %d; want 29 (末尾が見える位置までスクロールする)", col)
	}
	if ansi.StringWidth(shown) != 29 && ansi.StringWidth(shown) != 30 {
		t.Errorf("見えている幅 = %d (末尾付近の窓であるべき)", ansi.StringWidth(shown))
	}
	// 値の末尾がその窓に含まれていること (先頭で固まっていないこと)
	e.setValue(strings.Repeat("x", 199) + "Z")
	shown, _ = e.viewport(30, true)
	if !strings.HasSuffix(shown, "Z") {
		t.Errorf("末尾が見えていない (先頭で固まっている): %q", shown)
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

// 選択中の候補・欄が「太字だけ」でなく色と記号でも分かること (2026-08-28 の指摘)。
func TestSelectionIsVisibleWithoutBoldOnly(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 70, 14
	press(m, "enter", "") // 開いた直後が「いつ」
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
	if m.form.focus != focusWhen {
		t.Fatal("開いた直後のフォーカスが「いつ」でない")
	}
	press(m, "ctrl+n", "")
	if m.form.focus != focusText {
		t.Errorf("C-n で欄が移らない (focus=%v)", m.form.focus)
	}
	press(m, "ctrl+p", "")
	if m.form.focus != focusWhen {
		t.Errorf("C-p で「いつ」へ戻らない (focus=%v)", m.form.focus)
	}
	press(m, "ctrl+f", "")
	if got := m.form.current().label; got != "10分後" {
		t.Errorf("C-f で候補が進まない (%q)", got)
	}
	press(m, "ctrl+b", "")
	if got := m.form.current().label; got != "5分後" {
		t.Errorf("C-b で候補が戻らない (%q)", got)
	}
	// 文字列欄では C-f / C-b は文字移動 (欄移動に横取りされない)
	focusTextField(t, m)
	typeText(m, "ab")
	press(m, "ctrl+b", "")
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
	m := newTestModel(jobs...)
	press(m, "ctrl+n", "")
	if m.menuIdx != 1 {
		t.Errorf("メニューが C-n で動かない (idx=%d)", m.menuIdx)
	}
	press(m, "ctrl+p", "")
	if m.menuIdx != 0 {
		t.Errorf("メニューが C-p で戻らない (idx=%d)", m.menuIdx)
	}
	press(m, "ctrl+n", "")
	press(m, "enter", "")
	if m.screen != screenPick {
		t.Fatal("一覧に入れない")
	}
	press(m, "ctrl+n", "")
	if m.pickIdx != 1 {
		t.Errorf("一覧が C-n で動かない (idx=%d)", m.pickIdx)
	}
	press(m, "ctrl+p", "")
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
		openForm(t, m)
		typeText(m, "make test")
		press(m, "ctrl+t", "")
		if m.quit {
			t.Fatalf("%s: prefix だけで閉じた", second)
		}
		switch second {
		case "ctrl+m":
			press(m, "ctrl+m", "")
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
	openForm(t, m)
	press(m, "ctrl+t", "")
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
	press(m, "ctrl+t", "")
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

// 予約が決まったらトーストを出し、それが終わってから閉じること (ユーザー要望 2026-08-28)。
// 右下から滑り込む見せ方は glogx に合わせている。
func TestToastAfterReservation(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 70, 14
	openForm(t, m)
	typeText(m, "make test")
	press(m, "enter", "")
	if !m.toast.shown {
		t.Fatal("確定してもトーストが出ない")
	}
	if m.quit {
		t.Fatal("トーストを見せる前に閉じた")
	}
	if m.res.action != "new" {
		t.Fatalf("結果が確定していない: %+v", m.res)
	}
	// 滑り込み中も画面の高さを超えず、ヘルプ行を消さないこと (最下行に出す)
	helpBefore := ""
	for _, l := range strings.Split(m.View().Content, "\n") {
		if strings.Contains(stripSGR(l), "Esc") {
			helpBefore = stripSGR(l)
		}
	}
	if helpBefore == "" {
		t.Fatal("ヘルプ行が見つからない (テストの前提が崩れている)")
	}
	widths := []int{}
	for range toastFrames {
		m.Update(toastTickMsg{})
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) > m.height {
			t.Fatalf("トーストで画面が溢れた (%d 行 > %d)", len(lines), m.height)
		}
		found := false
		for _, l := range lines {
			if stripSGR(l) == helpBefore {
				found = true
			}
		}
		if !found {
			t.Fatalf("トーストがヘルプ行を消した:\n%s", stripSGR(m.View().Content))
		}
		last := lines[len(lines)-1]
		if w := ansi.StringWidth(last); w > 70 {
			t.Fatalf("トースト行が %d 桁 (上限 70)", w)
		}
		// 行は右端まで空白で埋めるので、見えている中身 (左の空白を除いた幅) を測る
		widths = append(widths, ansi.StringWidth(strings.TrimLeft(stripSGR(last), " ")))
	}
	// 幅が単調に増えて「滑り込む」こと (最後は全幅)
	for i := 1; i < len(widths); i++ {
		if widths[i] < widths[i-1] {
			t.Errorf("滑り込みが逆行した: %v", widths)
			break
		}
	}
	if widths[len(widths)-1] <= widths[0] {
		t.Errorf("トーストが広がっていない: %v", widths)
	}
	body := stripSGR(m.View().Content)
	if !strings.Contains(body, "予約しました") {
		t.Errorf("トーストの文言が出ていない:\n%s", body)
	}
	// 静止が終わると閉じる
	m.Update(toastDoneMsg{})
	if !m.quit {
		t.Error("静止の後に閉じない")
	}
	if m.res.action != "new" {
		t.Errorf("結果が消えた: %+v", m.res)
	}
}

// トースト中のキーで状態が変わらないこと (確定済みなので取り消し・再編集させない)。
func TestToastIgnoresEditingKeys(t *testing.T) {
	m := newTestModel()
	openForm(t, m)
	typeText(m, "make test")
	press(m, "enter", "")
	before := m.res
	typeText(m, "zzz")
	press(m, "tab", "")
	press(m, "left", "")
	if m.res != before {
		t.Errorf("トースト中のキーで結果が変わった: %+v → %+v", before, m.res)
	}
	if got := m.form.text.value(); got != "make test" {
		t.Errorf("トースト中に入力が変わった: %q", got)
	}
	// Esc / Enter は閉じるのを早めるだけ (結果は保つ)
	press(m, "esc", "")
	if !m.quit || m.res.action != "new" {
		t.Errorf("Esc で閉じられない / 結果が消えた (quit=%v res=%+v)", m.quit, m.res)
	}
}

// 取消 (一覧) では確定と同時に閉じること (取消の確認と実行はシェル側なので、ここで
// 「取り消しました」と出すと嘘になりうる)。
func TestNoToastForCancel(t *testing.T) {
	jobs := []job{{id: "a", at: time.Now().Add(time.Hour), label: "x", text: "make test"}}
	m := newTestModel(jobs...)
	press(m, "j", "")
	press(m, "enter", "")
	press(m, "enter", "")
	if m.toast.shown {
		t.Error("取消でトーストを出している (実行はシェル側なので確定していない)")
	}
	if !m.quit || m.res.action != "cancel" {
		t.Errorf("取消が返らない (quit=%v res=%+v)", m.quit, m.res)
	}
}

// 発火時刻は「Enter を押した瞬間」を基準にすること。popup を開いた時刻で固定すると、
// 入力に迷った時間だけ予約が前倒しになり、放置すれば過去になって即送信される
// (敵対的レビュー 2026-08-28)。
func TestReservationUsesClockAtSubmit(t *testing.T) {
	m := newTestModel()
	openForm(t, m)
	typeText(m, "make test")
	// popup を開いてから 25 分経ってから確定した
	late := now.Add(25 * time.Minute)
	m.nowFn = func() time.Time { return late }
	press(m, "enter", "")
	finishToast(m)
	if want := late.Add(5 * time.Minute); !m.res.at.Equal(want) {
		t.Errorf("at = %v; want %v (押した瞬間から 5 分後)", m.res.at, want)
	}
	if !m.res.at.After(late) {
		t.Errorf("発火時刻が過去になった (at=%v now=%v) — 即座に送信されてしまう", m.res.at, late)
	}
	// トーストの残り時間も同じ時計で計算していること (嘘を出さない)
	if body := stripSGR(m.View().Content); !strings.Contains(body, "(5m後)") {
		t.Errorf("トーストの残り時間が確定時の時計で計算されていない:\n%s", body)
	}
}

// 時刻指定も同じ: 15:29 に開いて 15:31 に 15:30 を入れたら「明日」になること。
func TestClockSpecUsesClockAtSubmit(t *testing.T) {
	m := newTestModel()
	press(m, "enter", "")
	focusSpecOfKind(t, m, kindClock)
	typeText(m, "10:20") // now は 10:00
	late := now.Add(25 * time.Minute)
	m.nowFn = func() time.Time { return late } // 10:25 に確定
	openForm(t, m)
	typeText(m, "x")
	press(m, "enter", "")
	finishToast(m)
	if !m.res.at.After(late) {
		t.Errorf("過去の時刻で予約された (at=%v now=%v)", m.res.at, late)
	}
	if want := time.Date(2026, 8, 28, 10, 20, 0, 0, time.UTC); !m.res.at.Equal(want) {
		t.Errorf("at = %v; want %v (過ぎていれば翌日)", m.res.at, want)
	}
}

// 中止も「結果」として返すこと。⚠️ 終了コードで中止を表すと、ビルド失敗やバイナリ不在
// (どちらも rc≠0) と区別できず、呼び出し側が異常を「ユーザーが閉じた」と読んで黙る (監査 2026-08-28)。
func TestAbortIsAResultNotAnExitCode(t *testing.T) {
	if got := formatResult(result{}); got != "" {
		t.Fatalf("中止の result が空行でない: %q", got)
	}
	// main はこれを "abort" として書く。ここでは変換規則だけ固定する
	if line := resultLine(result{}); line != "abort" {
		t.Errorf("中止の出力 = %q; want abort", line)
	}
	if line := resultLine(result{action: "cancel", id: "x"}); line != "cancel\tx" {
		t.Errorf("cancel の出力 = %q", line)
	}
}

// 1 欄に入れられる長さに上限があること。⚠️ 無いと大きな貼り付け 1 回で 1 行 1MB 超の予約ができ、
// 以後その一覧を読む側が壊れて UI が二度と開けなくなる (監査 2026-08-28 で再現)。
func TestInputIsCapped(t *testing.T) {
	var e editor
	e.insert(strings.Repeat("a", maxInput+5000))
	if got := len(e.runes); got != maxInput {
		t.Errorf("入力長 = %d; want %d", got, maxInput)
	}
	e.insert("bbb")
	if got := len(e.runes); got != maxInput {
		t.Errorf("上限に達した後も増えた: %d", got)
	}
	// 貼り付けも同じ上限に従う
	m := newTestModel()
	openForm(t, m)
	m.Update(tea.PasteMsg{Content: strings.Repeat("z", maxInput*2)})
	if got := len([]rune(m.form.text.value())); got > maxInput {
		t.Errorf("貼り付けで上限を超えた: %d", got)
	}
}

// 予約できる長さに上限があること (11 年後に起きる sleeper を作らせない)。
func TestDurationIsCapped(t *testing.T) {
	if _, err := parseDuration("99999h"); err == nil {
		t.Error("99999h (約 11 年) が通った")
	}
	if _, err := parseDuration("720h"); err != nil {
		t.Errorf("30 日は通るべき: %v", err)
	}
	if _, err := parseDuration("721h"); err == nil {
		t.Error("30 日超が通った")
	}
}

// ⚠️ トーストが自分でティックを張り、静止のあと自分で閉じメッセージを出すこと。
// テストが toastTickMsg / toastDoneMsg を手で流すと、**Tick を張らない実装でも緑になる**
// (= 確定後に popup が永久に閉じない。監査 2026-08-28)。ここは戻り値の Cmd だけを見る。
func TestToastDrivesItselfToClose(t *testing.T) {
	saved := toastHold
	toastHold = time.Millisecond
	defer func() { toastHold = saved }()

	var to toast
	cmd := to.start("予約しました")
	if cmd == nil {
		t.Fatal("start がティックを張っていない (誰も動かさないので永久に閉じない)")
	}
	if _, ok := cmd().(toastTickMsg); !ok {
		t.Fatalf("start の Cmd が toastTickMsg を出さない: %T", cmd())
	}
	// 進めるたびに次の Cmd が返り、最後に閉じメッセージへ辿り着くこと
	for i := range toastFrames + 2 {
		cmd = to.advance()
		if cmd == nil {
			t.Fatalf("%d フレーム目で動きが止まった (閉じない)", i)
		}
		msg := cmd()
		if _, ok := msg.(toastDoneMsg); ok {
			return // 静止 → 閉じるところまで自力で来た
		}
		if _, ok := msg.(toastTickMsg); !ok {
			t.Fatalf("%d フレーム目の Cmd が不明: %T", i, msg)
		}
	}
	t.Fatal("静止の後に閉じメッセージが出ない (popup が閉じない)")
}

// ⚠️ 本番の構築点で「確定用の時計」が配線されていること。テストは自分で nowFn を差し替えるので、
// newModel から nowFn を落としても全テストが緑のまま通ってしまう (監査 2026-08-28)。
func TestNewModelWiresWallClock(t *testing.T) {
	m := newModel("x", now, nil) // nowFn を差し替えない
	if m.nowFn == nil {
		t.Fatal("newModel が確定用の時計を配線していない")
	}
	// 確定の時計は「実時刻」であること (起動時に渡した now ではない)
	if d := time.Until(m.submitNow()); d > time.Second || d < -time.Second {
		t.Errorf("確定の時計が実時刻でない (submitNow=%v, 実時刻との差=%v)", m.submitNow(), d)
	}
	if m.submitNow().Equal(m.now) {
		t.Error("確定の時計が起動時の now に凍っている")
	}
}

// ⚠️ 表示用の時計を「自分で」回すこと。テストが tickMsg を手で流すと、Init が Tick を張らない実装
// (= 放置しても画面が更新されない) でも緑になる (監査 2026-08-28)。
func TestClockTickIsSelfSustaining(t *testing.T) {
	m := newTestModel()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init が時計のティックを張っていない")
	}
	if _, ok := cmd().(tickMsg); !ok {
		t.Fatalf("Init の Cmd が tickMsg を出さない: %T", cmd())
	}
	_, next := m.Update(tickMsg{})
	if next == nil {
		t.Fatal("tick が次のティックを張り直していない (1 回で止まる)")
	}
	if _, ok := next().(tickMsg); !ok {
		t.Fatalf("次の Cmd が tickMsg でない: %T", next())
	}
	// 閉じたあとは張り直さない (無駄に動かし続けない)
	m.quit = true
	if _, after := m.Update(tickMsg{}); after != nil {
		t.Error("閉じた後もティックを張り直している")
	}
}

// 開いた直後は「いつ」にフォーカスし、Enter だけで予約まで辿れること (ユーザー要望 2026-08-28)。
// ⚠️ 既定を文字列欄に戻すと、時刻を選ばずに打ち始める流れになり、この画面の順序 (いつ → 何を) が崩れる。
func TestFormStartsOnWhenRow(t *testing.T) {
	m := newTestModel()
	press(m, "enter", "") // メニュー → フォーム
	if m.form.focus != focusWhen {
		t.Fatalf("開いた直後の focus = %v; want when", m.form.focus)
	}
	// カーソル (IME の位置) は入力欄が対象なので、候補行では出さない
	if m.View().Cursor != nil {
		t.Error("候補行なのにカーソルを出している")
	}
	// 候補を選ぶキーがそのまま効く (欄移動なしで)
	press(m, "right", "")
	if got := m.form.current().label; got != "10分後" {
		t.Errorf("開いた直後に候補を動かせない (%q)", got)
	}
	// Enter だけで 文字列 → 予約 まで進める
	press(m, "enter", "")
	if m.form.focus != focusText {
		t.Fatalf("Enter で文字列欄へ進まない (focus=%v)", m.form.focus)
	}
	typeText(m, "make test")
	press(m, "enter", "")
	finishToast(m)
	if !m.quit || m.res.action != "new" {
		t.Fatalf("Enter だけの流れで予約できない (res=%+v)", m.res)
	}
	if want := now.Add(10 * time.Minute); !m.res.at.Equal(want) {
		t.Errorf("at = %v; want %v (選んだ候補が効いている)", m.res.at, want)
	}
}

// 入力欄の Esc は「一つ前の欄へ戻る」。先頭の欄 (いつ) で押したときだけメニューへ降りる
// (ユーザー要望 2026-08-28)。⚠️ いきなり降りると、打ち間違いを直したいだけのときに入力ごと畳まれる。
func TestEscapeStepsBackThroughFields(t *testing.T) {
	m := newTestModel()
	press(m, "enter", "")
	focusSpecOfKind(t, m, kindFree) // いつ → (自由) 入力欄
	typeText(m, "1h30m")
	press(m, "enter", "") // → 文字列
	typeText(m, "make test")
	if m.form.focus != focusText {
		t.Fatalf("前提が崩れている (focus=%v)", m.form.focus)
	}

	press(m, "esc", "") // 文字列 → 入力欄
	if m.form.focus != focusSpec {
		t.Fatalf("Esc で一つ前の欄へ戻らない (focus=%v)", m.form.focus)
	}
	if m.screen != screenForm {
		t.Fatal("Esc で画面を降りてしまった")
	}
	press(m, "esc", "") // 入力欄 → いつ
	if m.form.focus != focusWhen {
		t.Fatalf("Esc で「いつ」へ戻らない (focus=%v)", m.form.focus)
	}
	if m.screen != screenForm {
		t.Fatal("Esc で画面を降りてしまった")
	}
	// 入力は畳まれていない (戻っただけ)
	if got := m.form.text.value(); got != "make test" {
		t.Errorf("戻る操作で入力が消えた: %q", got)
	}
	if got := m.form.spec.value(); got != "1h30m" {
		t.Errorf("戻る操作で入力欄が消えた: %q", got)
	}
	press(m, "esc", "") // いつ → メニュー
	if m.screen != screenMenu {
		t.Errorf("先頭の欄の Esc でメニューへ降りない (screen=%v)", m.screen)
	}
	// 入力欄の無い候補でも同じ (文字列 → いつ → メニュー)
	m2 := newTestModel()
	press(m2, "enter", "")
	focusTextField(t, m2)
	press(m2, "esc", "")
	if m2.form.focus != focusWhen || m2.screen != screenForm {
		t.Errorf("入力欄が無いとき Esc が「いつ」へ戻らない (focus=%v screen=%v)", m2.form.focus, m2.screen)
	}
	press(m2, "esc", "")
	if m2.screen != screenMenu {
		t.Errorf("2 回目の Esc でメニューへ降りない (screen=%v)", m2.screen)
	}
}

// --start pick で一覧から開けること (取消のあとシェルが開き直すために使う)。
// ⚠️ 予約が 0 件のときは一覧を開かない (空の一覧を見せない。menuItems の enabled と同じ判断)。
func TestStartAtPick(t *testing.T) {
	jobs := []job{{id: "a", at: now.Add(time.Hour), label: "x", text: "make test"}}
	m := newTestModel(jobs...)
	m.startAt("pick")
	if m.screen != screenPick {
		t.Errorf("--start pick で一覧から始まらない (screen=%v)", m.screen)
	}
	// 0 件なら一覧へは入らない
	m2 := newTestModel()
	m2.startAt("pick")
	if m2.screen != screenMenu {
		t.Errorf("0 件なのに一覧から始まった (screen=%v)", m2.screen)
	}
	// 指定なしはメニュー
	m3 := newTestModel(jobs...)
	m3.startAt("")
	if m3.screen != screenMenu {
		t.Errorf("指定なしでメニューから始まらない (screen=%v)", m3.screen)
	}
}
