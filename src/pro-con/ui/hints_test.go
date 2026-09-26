package ui

import (
	"strings"
	"testing"

	"pro-con/backend"
	"pro-con/card"
)

// dimmed は案内の中の項目 text が暗く (押しても効かない色で) 出ているか。見つからなければ落とす。
func dimmed(t *testing.T, m *Model, text string) bool {
	t.Helper()
	for _, h := range m.hints() {
		if strings.Contains(h, text) {
			return strings.HasPrefix(h, fg(239))
		}
	}
	t.Fatalf("案内に %q が無い: %q", text, m.hints())
	return false
}

// カードへの操作は、選んでいるカードで効くかどうかで色が変わる。
func TestHintsDimUnavailableCardActions(t *testing.T) {
	be := newSpy()
	be.snap.Cards = append(be.snap.Cards, card.Card{ID: "N1", State: card.Planned, Since: be.snap.Now}) // session も issue も無い
	m := New(be, nil)

	m.selected = "W1" // 質問待ち
	if dimmed(t, m, "r 回答") || dimmed(t, m, "a attach") {
		t.Fatal("質問待ちで session のあるカードなのに r / a が暗い")
	}
	m.selected = "R1" // 作業中
	if !dimmed(t, m, "r 回答") {
		t.Fatal("作業中のカードで r が明るい (押しても回答できない)")
	}
	m.selected = "N1"
	if !dimmed(t, m, "a attach") || !dimmed(t, m, "e issue を開く") || !dimmed(t, m, "y パス") {
		t.Fatal("session も issue も無いカードで a / e / y が明るい")
	}
	if dimmed(t, m, "+ 追加オーダー") || dimmed(t, m, "Y 内容") {
		t.Fatal("カードを選んでいれば + / Y は効くのに暗い")
	}
	if !dimmed(t, m, "x 完了を片付け") {
		t.Fatal("完了のカードが無いのに x が明るい")
	}
	be.snap.Cards = append(be.snap.Cards, card.Card{ID: "D1", State: card.Done, Ending: card.EndAnswered})
	m.setSnap(be.snap)
	if dimmed(t, m, "x 完了を片付け") {
		t.Fatal("完了のカードがあるのに x が暗い")
	}
}

// 詳細を開いている間は、案内がスクロールと隣のカードへの送りに切り替わり、カンバンの移動は出さない。
func TestHintsInDrawer(t *testing.T) {
	m, _, clk := drawerModel(t)
	open(t, m, clk)
	line := strings.Join(m.hints(), "  ")
	if !strings.Contains(line, "j / k スクロール") || !strings.Contains(line, "J / K 隣のカード") || strings.Contains(line, "hjkl 選択") {
		t.Fatalf("詳細の案内になっていない: %q", line)
	}
}

// roSpy は書き込みの操作を 1 つも受けない backend (backend.Accepter)。
type roSpy struct{ *spy }

func (roSpy) Accepts(backend.Op) bool { return false }

// 読み取り専用の backend では、依頼・回答・追加オーダー・btw・片付けの案内を暗くする (押しても backend が拒否する)。
func TestHintsDimWritesOnReadOnlyBackend(t *testing.T) {
	be := roSpy{newSpy()}
	be.snap.Cards = append(be.snap.Cards, card.Card{ID: "D1", State: card.Done, Ending: card.EndAnswered})
	m := New(be, nil)
	m.selected = "W1" // 質問待ち (書き込みを受け付ける backend なら r は明るい)
	for _, h := range []string{"n 新しい依頼", "i issue から", "r 回答", "+ 追加オーダー", "w btw", "x 完了を片付け", "K / J 優先度"} {
		if !dimmed(t, m, h) {
			t.Fatalf("読み取り専用なのに %q が明るい", h)
		}
	}
	if dimmed(t, m, "Y 内容") || dimmed(t, m, "enter 詳細") {
		t.Fatal("読むだけの操作 (Y / enter) まで暗くなった")
	}
}

// 読み取り専用の backend では、書き込みの操作のキーを押した時点で断る (入力欄を開かない。書いた文が無駄にならないように)。
func TestReadOnlyRefusesWriteKeysImmediately(t *testing.T) {
	for _, k := range []string{"n", "i", "r", "+", "w", "x", "K", "J"} {
		be := roSpy{newSpy()}
		m := New(be, nil)
		m.selected = "W1" // 質問待ち (書き込みを受け付ける backend なら r で入力欄が開く)
		press(m, k)
		if m.mode != modeBoard || m.picker.open || !strings.Contains(m.toasts.Text(), "使えない操作") {
			t.Fatalf("読み取り専用なのに %q で操作が始まった: mode=%v picker=%v flash=%q", k, m.mode, m.picker.open, m.toasts.Text())
		}
		// 断った理由は赤の ✗ (シアンの … だと効かなかったことが進行中の知らせに見える。2026-09-25 のユーザーの指摘)
		if m.toasts.OK() || m.toasts.Info() {
			t.Fatalf("%q を断る通知が赤の ✗ でない: ok=%v info=%v", k, m.toasts.OK(), m.toasts.Info())
		}
	}
	m := New(newSpy(), nil) // 書き込みを受け付ける backend では、今までどおり入力欄が開く (対照)
	press(m, "n")
	if m.mode != modeInput {
		t.Fatal("書き込みを受け付ける backend で n の入力欄が開かない")
	}
}

// 見ているだけの画面の案内の行には、押すと断る操作 (attach・依頼・回答・追加オーダー・btw・片付け) を出さない (暗くもしない。issue 445)。
// 読むだけの操作 (詳細・issue を開く・コピー・設定画面) と抜ける手段は残す。viewSpy は Accepter を持たない (出さないのは ReadOnly だから)。
func TestHintsOmitActionsOnViewOnly(t *testing.T) {
	be := viewSpy{spy: newSpy()}
	be.snap.Cards = append(be.snap.Cards, card.Card{ID: "D1", State: card.Done, Ending: card.EndAnswered})
	m := New(be, nil)
	m.selected = "W1" // 質問待ち・session あり (書ける画面なら a / r が明るい)
	check := func(where string) {
		t.Helper()
		line := strings.Join(m.hints(), "  ")
		for _, h := range []string{"a attach", "r 回答", "+ 追加オーダー", "w btw", "d 削除", "n 新しい依頼", "i issue から", "x 完了を片付け"} {
			if strings.Contains(line, h) {
				t.Fatalf("%s: 見ているだけなのに %q を出した: %q", where, h, line)
			}
		}
		for _, h := range []string{"Y 内容", "e issue を開く"} {
			if !strings.Contains(line, h) {
				t.Fatalf("%s: 読むだけの操作 %q まで消した: %q", where, h, line)
			}
		}
	}
	check("ボード")
	for _, h := range []string{"enter 詳細", "s 設定", "Q 終了"} {
		if !strings.Contains(strings.Join(m.hints(), "  "), h) {
			t.Fatalf("ボードの %q まで消した: %q", h, m.hints())
		}
	}
	m.showDetail = true
	check("詳細")
}
