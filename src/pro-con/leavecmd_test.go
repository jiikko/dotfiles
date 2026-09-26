package main

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// ps の行から、popup の入れ子の tmux (client と server の 2 行を 1 つに束ねる) と、pro-con の画面の子の attach だけを拾う。
// 外のシェルから開いた claude attach・PG の session (claude --bg)・利用者の tmux には触らない。
func TestFindAttaches(t *testing.T) {
	sock := "/var/folders/x/T/pro-con-attach-123/sock"
	rows := []psRow{
		{pid: 10, ppid: 1, command: "/opt/homebrew/bin/tmux -S " + sock + " -f /var/folders/x/T/pro-con-attach-123/conf new-session -- /opt/homebrew/bin/claude attach abc"},
		{pid: 11, ppid: 9, command: "/opt/homebrew/bin/tmux -S " + sock + " -f /var/folders/x/T/pro-con-attach-123/conf new-session -- /opt/homebrew/bin/claude attach abc"},
		{pid: 20, ppid: 1, command: "/Users/u/dotfiles/bin/pro-con"},
		{pid: 21, ppid: 20, command: "claude attach def"},                        // 画面の子の attach (端末を渡した)
		{pid: 22, ppid: 20, command: "/Users/u/bin/pro-con fake-attach e2e0001"}, // e2e モードの偽の attach
		{pid: 30, ppid: 1, command: "-zsh"},
		{pid: 31, ppid: 30, command: "claude attach ghi"},                // 外のシェルから開いた attach
		{pid: 40, ppid: 1, command: "claude --bg --model sonnet 作業"},     // PG の session
		{pid: 50, ppid: 1, command: "tmux -S /tmp/tmux-501/default new"}, // 利用者の tmux
	}
	socks, pids := findAttaches(rows)
	if !slices.Equal(socks, []string{sock}) {
		t.Fatalf("popup の socket が違う: %v", socks)
	}
	if !slices.Equal(pids, []int{21, 22}) {
		t.Fatalf("画面の子の attach だけを拾うはず: %v", pids)
	}
}

func TestRunAttachLeave(t *testing.T) {
	sock := "/T/pro-con-attach-9/sock"
	rows := []psRow{
		{pid: 10, ppid: 1, command: "tmux -S " + sock + " new-session -- claude attach a"},
		{pid: 20, ppid: 1, command: "pro-con"},
		{pid: 21, ppid: 20, command: "claude attach b"},
	}
	var killed []string
	var termed []int
	l := attachLeaver{
		list:       func(context.Context) ([]psRow, error) { return rows, nil },
		killServer: func(s string) error { killed = append(killed, s); return nil },
		term:       func(p int) error { termed = append(termed, p); return nil },
	}
	var out, errOut bytes.Buffer
	if rc := runAttach([]string{"--leave"}, l, &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d err=%q", rc, errOut.String())
	}
	if !slices.Equal(killed, []string{sock}) || !slices.Equal(termed, []int{21}) {
		t.Fatalf("終わらせた先が違う: socks=%v pids=%v", killed, termed)
	}
	if !strings.Contains(out.String(), "PG の session は動き続ける") {
		t.Fatalf("PG の session が残ることを書いていない: %q", out.String())
	}

	// 見つからない・終わらせられないときは rc=1、引数の誤りは rc=2
	rows = nil
	if rc := runAttach([]string{"--leave"}, l, &out, &errOut); rc != 1 || !strings.Contains(errOut.String(), "無い") {
		t.Fatalf("attach が無いのに rc=%d err=%q", rc, errOut.String())
	}
	rows = []psRow{{pid: 20, ppid: 1, command: "pro-con"}, {pid: 21, ppid: 20, command: "claude attach b"}}
	l.term = func(int) error { return errors.New("no such process") }
	if rc := runAttach([]string{"--leave"}, l, &out, &errOut); rc != 1 {
		t.Fatalf("終わらせられなかったのに rc=%d", rc)
	}
	if rc := runAttach(nil, l, &out, &errOut); rc != 2 {
		t.Fatalf("--leave が無いのに rc=%d", rc)
	}
}
