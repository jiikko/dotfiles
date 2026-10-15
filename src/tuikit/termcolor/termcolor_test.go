package termcolor

import (
	"context"
	"errors"
	"testing"
)

func TestHasRGB(t *testing.T) {
	for in, want := range map[string]bool{
		"256,RGB,bpaste,clipboard\n": true,
		"RGB":                        true,
		"256,bpaste":                 false,
		"256,RGBX":                   false, // 名前の一部が一致するだけでは数えない
		"":                           false,
	} {
		if got := HasRGB(in); got != want {
			t.Errorf("HasRGB(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestTrueColorThroughTmux(t *testing.T) {
	t.Setenv("NO_COLOR", "") // 走らせる人の環境に依らない
	calls := 0
	run := func(out string, err error) Runner {
		return func(_ context.Context, name string, args ...string) (string, error) {
			calls++
			if name != "tmux" || len(args) != 3 || args[2] != "#{client_termfeatures}" {
				t.Errorf("想定外のコマンド: %s %v", name, args)
			}
			return out, err
		}
	}
	if TrueColorThroughTmux("", run("RGB", nil)) || calls != 0 {
		t.Errorf("tmux の外で聞いた (calls=%d)", calls)
	}
	if !TrueColorThroughTmux("/tmp/tmux-501/default,1,0", run("256,RGB", nil)) {
		t.Error("RGB のあるクライアントで false")
	}
	if TrueColorThroughTmux("/tmp/tmux-501/default,1,0", run("RGB", errors.New("no server"))) {
		t.Error("聞けなかったのに true")
	}
	t.Setenv("NO_COLOR", "0") // colorprofile は偽と読んで色を出すので、引き上げも外さない
	if !TrueColorThroughTmux("/tmp/tmux-501/default,1,0", run("256,RGB", nil)) {
		t.Error("NO_COLOR=0 で 24bit への引き上げを外した (colorprofile と読み方が違う)")
	}
	t.Setenv("NO_COLOR", "1")
	if TrueColorThroughTmux("/tmp/tmux-501/default,1,0", run("256,RGB", nil)) {
		t.Error("NO_COLOR なのに 24bit を強制した")
	}
}
