package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/card"
)

// タイトルは表示幅で titleLines 行まで折り返す。収まらない分は最後の行の末尾を … で切り、全角の途中では切らない。
func TestWrapLines(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    string
		w    int
		want []string
	}{
		{"空", "", 5, []string{"", ""}},
		{"1 行に収まる", "abc", 5, []string{"abc", ""}},
		{"2 行ちょうど", "abcdefghij", 5, []string{"abcde", "fghij"}},
		{"溢れた分は 2 行目の末尾を … で切る", "abcdefghijk", 5, []string{"abcde", "fghi…"}},
		{"折り返しの先頭の空白は落とす", "abcde fgh", 5, []string{"abcde", "fgh"}},
		{"全角は途中で切らない", "あいうえ", 5, []string{"あい", "うえ"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := wrapLines(tc.s, tc.w, 2)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("wrapLines(%q, %d) = %q (期待 %q)", tc.s, tc.w, got, tc.want)
			}
			for _, l := range got {
				if ansi.StringWidth(l) > tc.w {
					t.Fatalf("幅 %d を超えた行: %q", tc.w, l)
				}
			}
		})
	}
}

// 画面の上で、長いタイトルはカードの 2 行目に続き、バッジはその下の行に出る (タイトルを 1 行で切らない)。
func TestCardTitleWrapsOnBoard(t *testing.T) {
	for _, tc := range []struct {
		name, title, line2 string
	}{
		{"2 行目に続く", strings.Repeat("a", 24) + "ZZ", "ZZ"},
		{"2 行にも収まらなければ … で切る", strings.Repeat("a", 24) + "ZZ" + strings.Repeat("b", 60), "…"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			be := newSpy()
			be.snap.Cards = []card.Card{{ID: "R1", State: card.Running, Session: "s-r1", Since: be.snap.Now, Title: tc.title}}
			m := New(be, nil)
			m.width, m.height = 120, 30
			lines := strings.Split(ansi.Strip(m.render()), "\n")
			r := -1
			for i, l := range lines {
				if strings.Contains(l, "R1 aaa") {
					r = i
					break
				}
			}
			if r < 0 || r+2 >= len(lines) {
				t.Fatalf("前提: カードの 1 行目が見つからない:\n%s", strings.Join(lines, "\n"))
			}
			if strings.Contains(lines[r], "ZZ") {
				t.Fatalf("前提: 1 行目に収まる長さになっている (幅を見直す): %q", lines[r])
			}
			if !strings.Contains(lines[r+1], tc.line2) {
				t.Fatalf("タイトルの続きが 2 行目に無い (%q を期待):\n%s\n%s", tc.line2, lines[r], lines[r+1])
			}
			if !strings.Contains(lines[r+2], "0秒") {
				t.Fatalf("バッジがタイトルの 2 行の下に無い:\n%s\n%s\n%s", lines[r], lines[r+1], lines[r+2])
			}
		})
	}
}

// 確認のカード (issue 531) は 1 行目の番号の後に印を色つきで出す。作業のカードには出さない。
func TestQuestionCardMarkOnBoard(t *testing.T) {
	be := newSpy()
	be.snap.Cards = []card.Card{
		{ID: "Q1", State: card.Requested, Since: be.snap.Now, Title: "issue にするか", Purpose: card.ForQuestion},
		{ID: "W1", State: card.Requested, Since: be.snap.Now, Title: "直す"},
	}
	m := New(be, nil)
	m.width, m.height = 120, 30
	out := m.render()
	if !strings.Contains(ansi.Strip(out), "Q1 "+card.QuestionMark+" issue") || !strings.Contains(out, sgrPink+sgrBold+card.QuestionMark) {
		t.Fatalf("確認のカードに印が出ない: %q", ansi.Strip(out))
	}
	if strings.Contains(ansi.Strip(out), "W1 "+card.QuestionMark) {
		t.Fatal("作業のカードに確認の印を出した")
	}
}
