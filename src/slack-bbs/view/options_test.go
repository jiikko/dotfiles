package view

import (
	"context"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/termcaps"
)

// tmux の中で外側の端末が同期出力を持たない (Terminal.app) と、同期出力の答えを捨てる設定を足す。足さないと tmux は本文の 1 行の
// スクロールのたびにペインを描き直す。設定の中身 (捨てるのは同期出力の答えだけ) は tuikit/termsync のテストが見る。
func TestProgramOptionsDropsSyncWhenOuterTerminalLacksIt(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	calls := 0
	features := func(out string) termcaps.Runner {
		return func(context.Context, string, ...string) (string, error) { calls++; return out, nil }
	}
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	inTmux := map[string]string{"TMUX": "/tmp/tmux-501/default,1,0", "TERM_PROGRAM": "tmux"}
	for _, c := range []struct {
		name string
		env  map[string]string
		out  string
		want int
	}{
		{"tmux の外側が Terminal.app (24bit の色はある・同期出力は無い)", inTmux, "bpaste,ccolour,clipboard,cstyle,extkeys,focus,RGB,title\n", 2},
		{"tmux の外側が両方を持つ", inTmux, "256,RGB,sync,title\n", 1},
		{"tmux の外側が同期出力だけを持つ", inTmux, "256,sync\n", 0},
		{"tmux の外の Terminal.app", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, "", 1},
		{"tmux の外の他の端末", map[string]string{"TERM_PROGRAM": "ghostty"}, "", 0},
	} {
		calls = 0
		if got := len(ProgramOptions(env(c.env), features(c.out))); got != c.want {
			t.Errorf("%s: 設定が %d 個 (want %d)", c.name, got, c.want)
		}
		// 24bit の色と同期出力の判定は同じ tmux の問い合わせをする。固まった tmux は 1 回ごとに 1 秒待たせるので、1 回に留める
		if _, tmux := c.env["TMUX"]; tmux && calls != 1 {
			t.Errorf("%s: tmux に %d 回聞いた (1 回の答えを使い回すはず)", c.name, calls)
		}
	}
}
