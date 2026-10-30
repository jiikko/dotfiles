package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"testwait"
)

// popup の中身は外の tmux サーバの環境で起きるので、attach の作業場所・環境を渡し、入れ子の tmux は TMUX を落として
// 一時ディレクトリの socket と設定で起こす。窓の枠の見出しにカードの id と戻る操作を出す。
func TestPopupCommand(t *testing.T) {
	p := &PopupAttach{Tmux: "/bin/tmux"}
	c := exec.Command("/usr/bin/true", "attach", "abc123")
	c.Dir, c.Env = "/work", []string{"PATH=/x"}
	got := p.command(c, "C-082", "C-t", "/tmp/pro-con-attach-1").Args
	want := []string{"/bin/tmux", "display-popup", "-E", "-w", "90%", "-h", "90%", "-S", "fg=colour202",
		"-T", " C-082 の PG に attach 中 ─ C-t d / Ctrl+Z で pro-con に戻る (PG は動き続ける) ",
		"-d", "/work", "-e", "PATH=/x", "--", "/usr/bin/env", "-u", "TMUX", "-u", "TMUX_PANE",
		"/bin/tmux", "-S", "/tmp/pro-con-attach-1/sock", "-f", "/tmp/pro-con-attach-1/conf", "new-session", "--", "/usr/bin/true", "attach", "abc123"}
	if !slices.Equal(got, want) {
		t.Fatalf("popup のコマンドが違う:\n got  %q\n want %q", got, want)
	}
	if title := popupTitle("C-082", ""); !strings.Contains(title, "─ Ctrl+Z で pro-con に戻る") { // 外の prefix が無い (None)
		t.Fatalf("prefix が無いのに prefix の戻り方を出した: %q", title)
	}
}

// 入れ子の tmux のキーは戻るキー (外の prefix + d・Ctrl+Z) と、prefix を 2 回で prefix を送るものだけ。他は全部 claude attach へ渡す。
func TestPopupConfBindsOnlyLeaveKeys(t *testing.T) {
	conf := popupConf("C-t")
	for _, want := range []string{"unbind -a", "set -g prefix2 None", "bind -n C-z kill-server", "set -g prefix 'C-t'", "bind d kill-server",
		"bind 'C-t' send-prefix", "set -g destroy-unattached on"} {
		if !strings.Contains(conf, want) {
			t.Fatalf("入れ子の tmux の設定に %q が無い:\n%s", want, conf)
		}
	}
	if strings.Index(conf, "unbind -a") > strings.Index(conf, "bind -n") || strings.Index(conf, "unbind -a") > strings.Index(conf, "bind d") {
		t.Fatal("戻るキーを bind した後で全部を外している")
	}
	for prefix, want := range map[string]string{"#": "bind '#' send-prefix", ";": "bind ';' send-prefix", "'": `bind "'" send-prefix`} {
		if conf := popupConf(prefix); !strings.Contains(conf, want) { // 引用しないと # は注釈・; はコマンドの区切りに読まれる
			t.Fatalf("prefix %q を引用していない:\n%s", prefix, conf)
		}
	}
	if conf := popupConf(""); strings.Contains(conf, "bind d") || strings.Contains(conf, "send-prefix") {
		t.Fatalf("外の prefix が無いのに prefix の bind を足した:\n%s", conf)
	}
}

// tmux の外では attach を popup で開かない。
func TestTmuxPopupOnlyInTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	if p := TmuxPopup(); p != nil {
		t.Fatalf("tmux の外なのに popup を使う: %+v", p)
	}
}

// tmux の中 (popup を使う): 案内を出さず、端末も渡さず、裏の処理で popup を待つ。
func TestAttachInPopupSkipsGuideAndTerminal(t *testing.T) {
	m := New(newSpy(), nil)
	m.UsePopupAttach(&PopupAttach{Tmux: "/nonexistent/tmux"})
	handed := false
	m.execProcess = func(*exec.Cmd, tea.ExecCallback) tea.Cmd { handed = true; return nil }
	press(m, "right") // W1
	_, cmd := m.Update(press(m, "a")())
	if m.guide != nil || handed || cmd == nil {
		t.Fatalf("popup なのに案内を出した / 端末を渡した / 待つ処理が無い: guide=%v handed=%v", m.guide != nil, handed)
	}
	done, ok := cmd().(attachDoneMsg) // tmux が無いので popup は開けず、失敗の知らせで戻る
	if !ok || done.cardID != "W1" || done.session != "s-w1" || done.err == nil {
		t.Fatalf("popup の戻りの知らせが違う: %+v", done)
	}
}

// tmux の外: 案内でやめたら端末を渡さない。
func TestAttachGuideCancel(t *testing.T) {
	m := New(newSpy(), nil)
	handed := false
	m.execProcess = func(*exec.Cmd, tea.ExecCallback) tea.Cmd { handed = true; return nil }
	press(m, "right")
	m.Update(press(m, "a")())
	if !strings.Contains(m.View().Content, "pro-con に戻るには Ctrl+Z") {
		t.Fatal("案内に戻り方が出ていない")
	}
	press(m, "esc")
	if handed || m.guide != nil {
		t.Fatalf("案内でやめたのに端末を渡した (%v) / 案内が残った", handed)
	}
}

// 案内を出している間に選んでいたカードが外れたら、enter でも端末を渡さない。
func TestAttachGuideRechecksSelection(t *testing.T) {
	m := New(newSpy(), nil)
	handed := false
	m.execProcess = func(*exec.Cmd, tea.ExecCallback) tea.Cmd { handed = true; return nil }
	press(m, "right")
	m.Update(press(m, "a")())
	m.selected = "W2" // 裏の読み直しで選択が移った
	press(m, "enter")
	if handed {
		t.Fatal("案内の間に選択が変わったのに端末を渡した")
	}
}

// 外の tmux の prefix は聞いて読む。tmux が応答しなければ上限で諦め、popup も開かずに失敗で戻る (同じサーバへの display-popup も
// 戻らないので、開きに行くと裏の処理が止まったままになる)。
func TestPopupGivesUpOnHungTmux(t *testing.T) {
	fake := func(body string) string {
		p := filepath.Join(t.TempDir(), "tmux")
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if got, hung := (&PopupAttach{Tmux: fake(`echo C-t`)}).outerPrefix(); got != "C-t" || hung {
		t.Fatalf("応答する tmux の prefix を読めない: %q hung=%v", got, hung)
	}
	if got, hung := (&PopupAttach{Tmux: fake(`echo None`)}).outerPrefix(); got != "" || hung {
		t.Fatalf("prefix が None なのに %q hung=%v", got, hung)
	}
	// 応答しない tmux は、どのサブコマンドにも sleep で演じる (待ちではなく入力。上限に掛からなければ戻らない。
	// red の経路で子が残っても 60 秒で消えるよう長くしない)
	hung := &PopupAttach{Tmux: fake(`exec sleep 60`), prefixTimeout: 50 * time.Millisecond}
	got := make(chan error, 1)
	go func() { got <- hung.run(exec.Command("/usr/bin/true"), "C-1") }()
	// hang guard (合否は「戻ったか」。所要は測らない)
	err := testwait.Recv(t, "応答しない tmux で attach の裏の処理が戻らない", got, testwait.Within(30*time.Second))
	if err == nil || !strings.Contains(err.Error(), "応答しない") {
		t.Fatalf("応答しない tmux で popup を開きに行った / 失敗で戻らない: %v", err)
	}
}
