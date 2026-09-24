package lineedit

import "testing"

type step struct{ key, text string }

func typed(s string) []step {
	out := make([]step, 0, len(s))
	for _, r := range s {
		out = append(out, step{string(r), string(r)})
	}
	return out
}

func run(steps ...[]step) *Line {
	var l Line
	for _, ss := range steps {
		for _, s := range ss {
			l.Key(s.key, s.text)
		}
	}
	return &l
}

func keys(ks ...string) []step {
	out := make([]step, 0, len(ks))
	for _, k := range ks {
		out = append(out, step{key: k})
	}
	return out
}

// 各編集キーを、カーソルが途中にある状態で当てて、文字列とカーソルの位置を見る (カーソルを末尾に置くと、
// 「前を消す」と「後ろを消す」の取り違えが見えない)。全角も 1 文字として扱う。
func TestEditKeys(t *testing.T) {
	cases := []struct {
		name   string
		steps  [][]step
		want   string
		cursor int
	}{
		{"入力", [][]step{typed("遅延ロード")}, "遅延ロード", 5},
		{"backspace", [][]step{typed("abc"), keys("backspace")}, "ab", 2},
		{"ctrl+h は backspace", [][]step{typed("abc"), keys("left", "ctrl+h")}, "ac", 1},
		{"ctrl+d はカーソルの位置", [][]step{typed("abc"), keys("ctrl+a", "ctrl+d")}, "bc", 0},
		{"ctrl+b / ctrl+f", [][]step{typed("ac"), keys("ctrl+b"), typed("b"), keys("ctrl+f"), typed("d")}, "abcd", 4},
		{"ctrl+a / ctrl+e", [][]step{typed("bc"), keys("ctrl+a"), typed("a"), keys("ctrl+e"), typed("d")}, "abcd", 4},
		{"ctrl+w は前の 1 語", [][]step{typed("foo bar baz"), keys("alt+b"), keys("ctrl+w")}, "foo baz", 4},
		{"ctrl+u はカーソルの前", [][]step{typed("foo bar"), keys("alt+b", "ctrl+u")}, "bar", 0},
		{"ctrl+k はカーソルの後ろ", [][]step{typed("foo bar"), keys("alt+b", "ctrl+k")}, "foo ", 4},
		{"alt+f", [][]step{typed("foo bar"), keys("ctrl+a", "alt+f"), typed("!")}, "foo! bar", 4},
		{"全角の backspace", [][]step{typed("遅延ロード"), keys("left", "left", "backspace")}, "遅延ード", 2},
		{"端では何もしない", [][]step{keys("backspace", "ctrl+d", "left", "ctrl+w"), typed("a"), keys("right", "ctrl+k")}, "a", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := run(tc.steps...)
			if l.String() != tc.want || l.Cursor() != tc.cursor {
				t.Fatalf("got %q (カーソル %d) want %q (カーソル %d)", l.String(), l.Cursor(), tc.want, tc.cursor)
			}
		})
	}
}

// Enter / Esc / Tab と、編集に割り当てていない ctrl 系は食べない (呼び出し側が捌く)。文字の入力は食べる。
func TestKeyReportsWhatItConsumed(t *testing.T) {
	var l Line
	for _, k := range []string{"enter", "esc", "tab", "ctrl+c", "ctrl+n"} {
		if l.Key(k, "") {
			t.Fatalf("%s を食べた", k)
		}
	}
	// 修飾キー付きの打鍵は、端末や版によって Text に文字が入って届くことがある。編集に割り当てていないものは入力にしない
	for _, k := range [][2]string{{"ctrl+z", "z"}, {"alt+x", "x"}} {
		if l.Key(k[0], k[1]) || !l.Empty() {
			t.Fatalf("%s を文字として入力した: %q", k[0], l.String())
		}
	}
	if !l.Key("a", "a") || l.String() != "a" {
		t.Fatalf("文字の入力を食べていない: %q", l.String())
	}
}

// ペーストはカーソルの位置に入り、改行とタブは空白、他の制御文字は落ちる (1 行の欄に行を増やさない)。
func TestInsertIsOneLine(t *testing.T) {
	l := run(typed("[]"), keys("left"))
	l.Insert("a\nb\tc\x1bd\x07")
	if l.String() != "[a b cd]" || l.Cursor() != 7 {
		t.Fatalf("got %q (カーソル %d)", l.String(), l.Cursor())
	}
	if v := l.View("|"); v != "[a b cd|]" {
		t.Fatalf("View が違う: %q", v)
	}
}
