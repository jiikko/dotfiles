package confirm

import (
	"os"
	"strings"
	"testing"

	"tuikit/termwidth"
	"tuikit/widthenv"
)

func TestMain(m *testing.M) {
	widthenv.ExitIfUnsupported()
	os.Exit(m.Run())
}

// 実行キーの判定表。どちらも「y / Enter 以外は取り消し」で、違いは大文字 Y だけ。
// 取り消し側に n / Esc だけでなく、打ち間違えやすい隣接キーと移動キーを並べる
// (知らないキーで実行されるのが最悪の失敗)。
func TestYesKeys(t *testing.T) {
	cases := []struct {
		key         string
		yes, strict bool
	}{
		{"y", true, true},
		{"enter", true, true},
		{"Y", true, false},
		{"n", false, false},
		{"N", false, false},
		{"esc", false, false},
		{"t", false, false},
		{"u", false, false},
		{"space", false, false},
		{" ", false, false},
		{"j", false, false},
		{"ctrl+m", false, false},
		{"shift+y", false, false},
		{"", false, false},
		{"yes", false, false},
	}
	for _, c := range cases {
		if got := IsYes(c.key); got != c.yes {
			t.Errorf("IsYes(%q) = %v, want %v", c.key, got, c.yes)
		}
		if got := IsYesStrict(c.key); got != c.strict {
			t.Errorf("IsYesStrict(%q) = %v, want %v", c.key, got, c.strict)
		}
	}
}

func TestBoxWidthCappedAtMax(t *testing.T) {
	for _, w := range []int{200, MaxWidth} {
		box := Box(" t ", []string{"x"}, w, false)
		for i, l := range box {
			if got := termwidth.Of(l); got != MaxWidth {
				t.Fatalf("width=%d: 行 %d の幅 = %d, want %d: %q", w, i, got, MaxWidth, l)
			}
		}
	}
}

// 画面が上限より狭ければ画面幅に合わせる (はみ出して右端が切れない)。0 以下は 80 とみなす。
func TestBoxWidthFollowsNarrowScreen(t *testing.T) {
	for _, c := range []struct{ width, want int }{{30, 30}, {0, MaxWidth}, {-1, MaxWidth}} {
		box := Box(" t ", []string{"x"}, c.width, false)
		if got := termwidth.Of(box[0]); got != c.want {
			t.Errorf("width=%d: 幅 = %d, want %d", c.width, got, c.want)
		}
	}
}

// Dialog は本文 → 空行 → 案内の順。案内は色つきのときだけ淡色で包み、色なしでは SGR を出さない。
func TestDialogLayout(t *testing.T) {
	box := Dialog(" 確認 ", []string{"本文 1", "本文 2"}, HintYesNo, 80, false)
	// 上辺 + 本文 2 + 空行 + 案内 + 下辺 + 下影
	if len(box) != 7 {
		t.Fatalf("行数 = %d, want 7:\n%s", len(box), strings.Join(box, "\n"))
	}
	if !strings.Contains(box[0], "確認") {
		t.Errorf("題字が上辺に無い: %q", box[0])
	}
	for i, want := range []string{"本文 1", "本文 2", "", HintYesNo} {
		inner := strings.TrimSpace(strings.Trim(strings.TrimRight(box[1+i], "█▓▒░ "), "│"))
		if inner != want {
			t.Errorf("行 %d = %q, want %q", 1+i, inner, want)
		}
	}
	if strings.Contains(strings.Join(box, ""), "\x1b[") {
		t.Errorf("色なしなのに SGR が出ている")
	}
	colored := Dialog(" 確認 ", []string{"本文"}, HintYesNo, 80, true)
	if !strings.Contains(colored[3], "\x1b[2m"+HintYesNo+"\x1b[0m") {
		t.Errorf("案内が淡色で包まれていない: %q", colored[3])
	}
}
