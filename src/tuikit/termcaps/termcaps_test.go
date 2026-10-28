package termcaps

import (
	"context"
	"errors"
	"testing"
)

func TestSyncedOutput(t *testing.T) {
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
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	inTmux := map[string]string{"TMUX": "/tmp/tmux-501/default,1,0", "TERM_PROGRAM": "tmux"}
	for _, c := range []struct {
		name string
		env  map[string]string
		run  Runner
		want bool
	}{
		// Terminal.app のクライアント (2026-10-25 に本物の tmux 3.7c から読んだ値)
		{"tmux の外側が Terminal.app", inTmux, run("bpaste,ccolour,clipboard,cstyle,extkeys,focus,RGB,title\n", nil), false},
		{"tmux の外側が sync を持つ", inTmux, run("256,RGB,sync,title\n", nil), true},
		{"名前の一部が一致するだけでは数えない", inTmux, run("RGB,syncx", nil), false},
		{"tmux に聞けない", inTmux, run("", errors.New("no server")), true},
		// attach しているクライアントが無いと rc=0 で空 (2026-10-25 に隔離した tmux 3.7c で確かめた)
		{"クライアントが無い", inTmux, run("\n", nil), true},
		{"tmux の外の Terminal.app", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, run("sync", nil), false},
		{"tmux の外の他の端末", map[string]string{"TERM_PROGRAM": "ghostty"}, run("", nil), true},
	} {
		calls = 0
		if got := SyncedOutput(env(c.env), c.run); got != c.want {
			t.Errorf("%s: SyncedOutput = %v, want %v", c.name, got, c.want)
		}
		if _, tmux := c.env["TMUX"]; !tmux && calls != 0 {
			t.Errorf("%s: tmux の外で tmux に聞いた", c.name)
		}
	}
}

func TestTmuxPrefix(t *testing.T) {
	calls := 0
	run := func(out string, err error) Runner {
		return func(_ context.Context, name string, args ...string) (string, error) {
			calls++
			if name != "tmux" || len(args) != 3 || args[0] != "show-options" || args[2] != "prefix" {
				t.Errorf("想定外のコマンド: %s %v", name, args)
			}
			return out, err
		}
	}
	for _, c := range []struct {
		name, tmux string
		run        Runner
		want       string
	}{
		{"C-t", "/tmp/tmux-501/default,1,0", run("prefix C-t\n", nil), "ctrl+t"},
		{"C-b", "/tmp/tmux-501/default,1,0", run("prefix C-b\n", nil), "ctrl+b"},
		{"M- の系は切る", "/tmp/tmux-501/default,1,0", run("prefix M-a\n", nil), ""},
		{"None", "/tmp/tmux-501/default,1,0", run("prefix None\n", nil), ""},
		{"形が違う", "/tmp/tmux-501/default,1,0", run("garbage\n", nil), ""},
		{"tmux に聞けない", "/tmp/tmux-501/default,1,0", run("prefix C-t\n", errors.New("no server")), ""},
		{"tmux の外", "", run("prefix C-t\n", nil), ""},
	} {
		calls = 0
		if got := TmuxPrefix(c.tmux, c.run); got != c.want {
			t.Errorf("%s: TmuxPrefix = %q, want %q", c.name, got, c.want)
		}
		if c.tmux == "" && calls != 0 {
			t.Errorf("%s: tmux の外で tmux に聞いた", c.name)
		}
	}
}
