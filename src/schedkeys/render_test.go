package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// 敵対的レビュー (2026-08-28) で見つかった描画の壊れ方に対する回帰テスト。
// 「幅・高さ・カーソル位置」は IME の未確定文字の位置を決めるので、崩れると入力そのものが壊れる。

// 壊しにいく入力の見本。実際に打てるもの (貼り付け含む) だけを並べる。
var nastyInputs = map[string]string{
	"ASCII 長文":   strings.Repeat("x", 300),
	"全角長文":       strings.Repeat("あ", 200),
	"NFD (か+濁点)": strings.Repeat("が", 80),
	// 入力としては弾くが、既存の job や貼り付け経路から流れ込んでも描画が壊れないことを見る
	"VS16":    strings.Repeat("❤️", 80),
	"キーキャップ":  strings.Repeat("1️⃣", 60),
	"ZWJ 絵文字": strings.Repeat("\U0001F468\u200d\U0001F469\u200d\U0001F467", 20),
	"ハングル":    strings.Repeat("한글", 80),
	"アラビア語":   strings.Repeat("العربية", 30),
	"混在":      strings.Repeat("aあ한❤️", 50),
}

// viewport は「必ず返る」こと。幅 0 の書記素があると、自前の rune 走査では 1 文字も進まず
// 無限ループになり、popup が 100% CPU で固まって Esc も効かなくなる (レビューで再現)。
func TestViewportTerminatesAndFits(t *testing.T) {
	for name, in := range nastyInputs {
		for _, w := range []int{1, 2, 4, 7, 20, 41, 70} {
			done := make(chan struct{})
			var shown string
			var col int
			go func() {
				var e editor
				e.setValue(in)
				shown, col = e.viewport(w, true)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatalf("%s w=%d: viewport が返らない (無限ループ)", name, w)
			}
			if got := ansi.StringWidth(shown); got > w {
				t.Errorf("%s w=%d: 表示幅 %d が幅を超えた", name, w, got)
			}
			if col < 0 || col > w {
				t.Errorf("%s w=%d: カーソル列 %d が範囲外", name, w, col)
			}
		}
	}
}

// truncate は書記素クラスタ単位で切ること (rune ごとに足すと VS16 で実幅の半分に見積もり、
// 切ったつもりで幅を超える)。
func TestTruncateRespectsGraphemeWidth(t *testing.T) {
	for name, in := range nastyInputs {
		for _, w := range []int{0, 1, 3, 10, 30} {
			if got := ansi.StringWidth(truncate(in, w)); got > w {
				t.Errorf("%s: truncate(_, %d) の幅が %d", name, w, got)
			}
		}
	}
}

// 描画は「どの端末サイズでも、幅を超えず・高さを超えず・カーソルが枠の中」であること。
func TestFrameFitsEverySize(t *testing.T) {
	label := "main:1 " + strings.Repeat("ク", 40) // window 名は長さの上限が無い
	// 幅 5..9 も見る: タイトル行は固定 10 桁 (「新規予約」+ 空白 2) なので、切らないと必ず溢れる
	for _, w := range []int{5, 8, 9, 12, 20, 24, 40, 70, 200} {
		for _, h := range []int{3, 5, 8, 9, 14, 40} {
			for _, tc := range []struct {
				name  string
				setup func(*model)
			}{
				{"プリセット", func(m *model) {}},
				{"時刻欄", func(m *model) {
					press(m, "tab", "")
					press(m, "left", "")
					press(m, "left", "")
					press(m, "enter", "")
					typeText(m, "25:00")
					press(m, "enter", "")
				}},
				{"長い文字列", func(m *model) { typeText(m, strings.Repeat("あ", 100)) }},
			} {
				m := newModel(label, now, nil)
				m.nowFn = func() time.Time { return now }
				m.width, m.height = w, h
				press(m, "enter", "")
				tc.setup(m)
				v := m.View()
				lines := strings.Split(v.Content, "\n")
				if len(lines) > h {
					t.Errorf("%dx%d %s: %d 行 (上限 %d)", w, h, tc.name, len(lines), h)
				}
				for i, l := range lines {
					if got := ansi.StringWidth(l); got > w {
						t.Errorf("%dx%d %s: %d 行目が %d 桁: %q", w, h, tc.name, i, got, stripSGR(l))
					}
				}
				if v.Cursor == nil {
					continue
				}
				if v.Cursor.Y < 0 || v.Cursor.Y >= len(lines) {
					t.Errorf("%dx%d %s: Cursor.Y=%d が枠 (%d 行) の外", w, h, tc.name, v.Cursor.Y, len(lines))
				}
				if v.Cursor.X < 0 || v.Cursor.X > w {
					t.Errorf("%dx%d %s: Cursor.X=%d が幅 %d の外", w, h, tc.name, v.Cursor.X, w)
				}
			}
		}
	}
}

// 画面が低いとき、カーソルのある欄が枠の外へ落ちないこと (落ちると入力が見えないのに
// IME の未確定文字だけがどこかに出る)。
func TestFocusedFieldStaysVisibleWhenShort(t *testing.T) {
	for _, h := range []int{3, 4, 5, 6, 8} {
		m := newTestModel()
		m.width, m.height = 70, h
		press(m, "enter", "")
		press(m, "tab", "")
		press(m, "left", "") // 自由入力 (欄が 1 つ増える)
		press(m, "enter", "")
		typeText(m, "1h30m")
		press(m, "enter", "")
		typeText(m, "make test")
		v := m.View()
		lines := strings.Split(v.Content, "\n")
		if v.Cursor == nil {
			t.Fatalf("h=%d: Cursor が nil", h)
		}
		if v.Cursor.Y >= len(lines) {
			t.Fatalf("h=%d: Cursor.Y=%d が枠 (%d 行) の外", h, v.Cursor.Y, len(lines))
		}
		row := stripSGR(lines[v.Cursor.Y])
		if !strings.Contains(row, "make test") {
			t.Errorf("h=%d: カーソル行に入力が無い: %q\n--- frame ---\n%s", h, row, stripSGR(v.Content))
		}
	}
}

// 一覧: 長い送り先の表示名が、他の行の「何を送るか」を画面外へ押し出さないこと
// (取り消す前に中身が読めないのは危険)。
func TestPickKeepsTextColumnVisible(t *testing.T) {
	jobs := []job{
		{id: "a", at: now.Add(4 * time.Minute), label: "main:1 " + strings.Repeat("ク", 40), text: "make test"},
		{id: "b", at: now.Add(time.Hour), label: "w:2 zsh", text: "deploy production"},
	}
	for _, w := range []int{40, 70, 120} {
		m := newModel("main:3 claude", now, jobs)
		m.nowFn = func() time.Time { return now }
		m.width, m.height = w, 14
		press(m, "j", "")
		press(m, "enter", "")
		lines := strings.Split(m.View().Content, "\n")
		for i, l := range lines {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("w=%d: %d 行目が %d 桁: %q", w, i, got, stripSGR(l))
			}
		}
		// 幅が足りなければ切り詰めてよいが、「何を送る予約か」が読める程度には残ること
		body := stripSGR(m.View().Content)
		for _, want := range []string{"make tes", "deploy p"} {
			if !strings.Contains(body, want) {
				t.Errorf("w=%d: 予約の文字列 %q が一覧から消えた:\n%s", w, want, body)
			}
		}
	}
}

// トーストは中身の行を食わない (低い画面では出さない)。
func TestToastNeverEatsContent(t *testing.T) {
	for _, h := range []int{5, 8, 9, 14, 40} {
		m := newTestModel()
		m.width, m.height = 70, h
		press(m, "enter", "")
		typeText(m, "make test")
		before := strings.Split(stripSGR(m.View().Content), "\n")
		press(m, "enter", "") // 確定 → トースト開始
		for range toastFrames {
			m.Update(toastTickMsg{})
		}
		after := strings.Split(stripSGR(m.View().Content), "\n")
		if len(after) > h {
			t.Errorf("h=%d: トーストで %d 行になった", h, len(after))
		}
		for _, b := range before {
			if strings.TrimSpace(b) == "" {
				continue
			}
			found := false
			for _, a := range after {
				if a == b {
					found = true
				}
			}
			if !found {
				t.Errorf("h=%d: トーストが中身の行を消した: %q\n--- after ---\n%s", h, b, strings.Join(after, "\n"))
				break
			}
		}
	}
}

// 見えない文字 (制御文字・書式文字) は受け付けないこと。幅 0 で描画とカーソルをずらすうえ、
// 打った本人に見えないまま pane へ送られる (表示順の反転はコマンドの偽装に使える)。
func TestInvisibleRunesRejected(t *testing.T) {
	for name, in := range map[string]string{
		"RLO":  "\u202e",
		"RLM":  "\u200f",
		"ZWSP": "\u200b",
		"ZWJ":  "\u200d",
		"NEL":  "\u0085",
		"LS":   "\u2028",
		"SHY":  "\u00ad",
		"混在":   "a\u202eb",
		"制御文字": "\x01",
	} {
		var e editor
		e.setValue("ok")
		if e.handle("x", in) {
			t.Errorf("%s: 受け付けた", name)
		}
		if e.value() != "ok" {
			t.Errorf("%s: 値が変わった (%q)", name, e.value())
		}
	}
	// 通常の文字と、異体字セレクタを伴わない絵文字は通る
	for _, in := range []string{"a", "あ", "한", "❤", "😀", "が"} {
		var e editor
		if !e.handle("x", in) {
			t.Errorf("%q を弾いた", in)
		}
	}
	// 異体字セレクタ (U+FE0F) は弾く: 描画側と幅の数え方が食い違い、本物のカーソルが右へずれる
	// (IME の未確定文字が別の場所に出る)。基底文字だけなら通る
	for _, in := range []string{"\ufe0f", "❤\ufe0f", "\U0001F468\ufe0f"} {
		var e editor
		e.setValue("ok")
		if e.handle("x", in) {
			t.Errorf("異体字セレクタつき %q を受け付けた", in)
		}
	}
}

// 描画が入力長に対して線形であること (自前の走査で二乗になり、1000 文字で固まっていた)。
func TestRenderCostStaysLinear(t *testing.T) {
	measure := func(n int) time.Duration {
		m := newTestModel()
		m.width, m.height = 70, 14
		press(m, "enter", "")
		m.form.text.setValue(strings.Repeat("x", n))
		start := time.Now()
		for range 20 {
			m.View()
		}
		return time.Since(start) / 20
	}
	small := measure(100)
	big := measure(10000)
	if big > 5*time.Millisecond {
		t.Errorf("10000 文字の 1 フレームが %v (5ms 以内であるべき)", big)
	}
	// 100 倍の入力で 30 倍を超えるなら線形ではない (二乗なら 10000 倍近くになる)
	if small > 0 && big > small*30 {
		t.Errorf("描画コストが入力長に対して非線形: %v → %v", small, big)
	}
}

// ラウンド 3 の指摘に対する回帰テスト。

// 書記素クラスタ単位で編集すること (rune 単位だと肌色や結合の一部だけが消え、見た目は
// ほぼ同じなのに別の文字列が pane へ送られる)。
func TestEditingMovesByGraphemeCluster(t *testing.T) {
	for _, tc := range []struct{ name, value, after string }{
		{"肌色つき", "\U0001F44D\U0001F3FD", ""},
		{"NFD の が", "が", ""},
		{"国旗", "\U0001F1EF\U0001F1F5", ""},
		{"ASCII", "ab", "a"},
	} {
		var e editor
		e.setValue(tc.value)
		e.handle("backspace", "")
		if got := e.value(); got != tc.after {
			t.Errorf("%s: backspace 後 = %q; want %q (見た目の 1 文字が消えるべき)", tc.name, got, tc.after)
		}
	}
	// 左右の移動もクラスタ単位 (クラスタの内側にカーソルが入らない)
	var e editor
	e.setValue("a\U0001F44D\U0001F3FDb")
	e.handle("home", "")
	e.handle("right", "")
	e.handle("right", "")
	if e.pos != 3 {
		t.Errorf("右 2 回で pos=%d (クラスタを 1 単位として跨ぐべき)", e.pos)
	}
	e.handle("left", "")
	if e.pos != 1 {
		t.Errorf("左 1 回で pos=%d", e.pos)
	}
}

// ペーストが入力欄に入ること (KeyPressMsg では来ないので、捨てると貼り付けが完全に効かない)。
func TestPasteEntersField(t *testing.T) {
	m := newTestModel()
	press(m, "enter", "")
	m.Update(tea.PasteMsg{Content: "make deploy"})
	if got := m.form.text.value(); got != "make deploy" {
		t.Errorf("文字列欄に貼れない: %q", got)
	}
	// 受け付けない文字は落として残りを入れる
	m.form.text.setValue("")
	m.Update(tea.PasteMsg{Content: "a\u202eb\nc"})
	if got := m.form.text.value(); got != "abc" {
		t.Errorf("見えない文字を落として貼れていない: %q", got)
	}
	// 入力欄 (時刻/自由) にも貼れる
	m2 := newTestModel()
	press(m2, "enter", "")
	press(m2, "tab", "")
	press(m2, "left", "")
	press(m2, "enter", "")
	m2.Update(tea.PasteMsg{Content: "1h30m"})
	if got := m2.form.spec.value(); got != "1h30m" {
		t.Errorf("入力欄に貼れない: %q", got)
	}
	// トースト中は貼れない (確定済み)
	m3 := newTestModel()
	press(m3, "enter", "")
	typeText(m3, "x")
	press(m3, "enter", "")
	m3.Update(tea.PasteMsg{Content: "zzz"})
	if got := m3.form.text.value(); got != "x" {
		t.Errorf("トースト中に貼れてしまった: %q", got)
	}
}

// 画面の「N 時に送る」が、開いた時刻ではなく今の時刻で計算されること
// (時刻指定では画面と実際の予約が丸 1 日ずれて見えていた)。
func TestDisplayClockAdvances(t *testing.T) {
	m := newTestModel()
	press(m, "enter", "")
	press(m, "tab", "")
	press(m, "left", "")
	press(m, "left", "") // 時刻
	press(m, "enter", "")
	typeText(m, "10:20") // now = 10:00 なので今日
	if body := stripSGR(m.View().Content); !strings.Contains(body, "10:20 に送る") {
		t.Fatalf("前提が崩れている:\n%s", body)
	}
	// 25 分放置した (tick が来る)
	late := now.Add(25 * time.Minute)
	m.nowFn = func() time.Time { return late }
	m.Update(tickMsg{})
	body := stripSGR(m.View().Content)
	if !strings.Contains(body, "(23h") {
		t.Errorf("画面が古い時計のまま (翌日になったことを見せていない):\n%s", body)
	}
}

// prefix に続く別のキーを飲み込まないこと (prefix が C-b の環境で左移動が効かなくなる)。
func TestPrefixDoesNotSwallowEditingKey(t *testing.T) {
	m := newTestModel()
	m.togglePrefix = "ctrl+b"
	press(m, "enter", "")
	typeText(m, "abc")
	m.Update(ctrlKey('b')) // prefix として arm される
	m.Update(ctrlKey('b')) // 続く C-b: トグルではないので、2 つとも左移動として効く
	if m.quit {
		t.Fatal("C-b 2 回で閉じた")
	}
	if m.form.text.pos != 1 {
		t.Errorf("pos=%d; want 1 (prefix 自身も左移動として処理されるべき)", m.form.text.pos)
	}
}
