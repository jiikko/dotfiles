package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"treefiler/filer"
)

// q で終わるとき Close を呼ぶ (Remember place が保存される)。
func TestQuitSavesPlace(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("TREEFILER_CONFIG_DIR", cfg)
	if err := os.WriteFile(filepath.Join(cfg, "treefiler.toml"), []byte("remember = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := filer.New(t.TempDir(), filer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{f: f}
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if _, err := os.Stat(filepath.Join(cfg, "treefiler-places")); err != nil {
		t.Fatalf("q で終えても前回の場所が保存されない: %v", err)
	}
}

type exitStatusError struct{}

func (exitStatusError) Error() string { return "exit status 1" }
func (exitStatusError) ExitCode() int { return 1 }

// シェルを起動できなかったときは知らせ、終了コードでは知らせない。
func TestExecStartFailureIsShown(t *testing.T) {
	t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir())
	f, err := filer.New(t.TempDir(), filer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{f: f}
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.Update(execDoneMsg{err: exitStatusError{}})
	if a.toast.Visible() {
		t.Fatalf("終了コードを失敗と出した: %q", a.toast.Text())
	}
	a.Update(execDoneMsg{err: errors.New("chdir: no such file or directory")})
	if !a.toast.Visible() || !strings.Contains(a.toast.Text(), "起動できません") {
		t.Fatalf("起動の失敗が出ない: visible=%v %q", a.toast.Visible(), a.toast.Text())
	}
}

// 裏で終わった処理の知らせ (explode の完了) は、次のキーを待たずに tick で toast に出る。
func TestBackgroundNoticeShowsOnTick(t *testing.T) {
	t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := filer.New(root, filer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{f: f}
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.Update(tea.KeyPressMsg{Code: '>', Text: ">"}) // カーソルは sub (フォルダ) → explode
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(a.toast.Text(), "開いた") {
		if time.Now().After(deadline) {
			t.Fatalf("explode の完了の知らせが tick で出ない: %q", a.toast.Text())
		}
		a.Update(tickMsg{})
		time.Sleep(time.Millisecond) // sleep-ok: tick: 裏の explode の完了を条件で待つループの刻み
	}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func newToggleApp(t *testing.T, toggleKey, prefix string) *app {
	t.Helper()
	t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir())
	f, err := filer.New(t.TempDir(), filer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{f: f, toggleKey: toggleKey}
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.Update(prefixMsg{prefix})
	return a
}

var (
	keyPrefix = tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}
	keyF      = tea.KeyPressMsg{Code: 'f', Text: "f"}
	keyQ      = tea.KeyPressMsg{Code: '?', Text: "?"}
)

// tmux の popup で開いたとき、開いた prefix+f で閉じる (popup の中のキーは tmux の bind を通らない)。
func TestPrefixToggleKeyQuits(t *testing.T) {
	a := newToggleApp(t, "f", "ctrl+t")
	if _, cmd := a.Update(keyPrefix); isQuit(cmd) {
		t.Fatal("prefix だけで閉じた")
	}
	if _, cmd := a.Update(keyF); !isQuit(cmd) {
		t.Fatal("prefix → f で閉じない")
	}
	// prefix を続けて押してから f でも閉じる (押し直した prefix で待ちが解けない)
	b := newToggleApp(t, "f", "ctrl+t")
	b.Update(keyPrefix)
	b.Update(keyPrefix)
	if _, cmd := b.Update(keyF); !isQuit(cmd) {
		t.Fatal("prefix → prefix → f で閉じない")
	}
}

// prefix の後の別のキーは普通の操作として効き、その後の f は閉じない。
func TestPrefixThenOtherKeyPassesThrough(t *testing.T) {
	a := newToggleApp(t, "f", "ctrl+t")
	a.Update(keyPrefix)
	a.Update(keyQ) // ? はキーの一覧を開く
	if !a.f.OwnsKeys() {
		t.Fatal("prefix の後の ? が効かない (キーの一覧が開かない)")
	}
	a.Update(keyQ) // 一覧を閉じる
	if _, cmd := a.Update(keyF); isQuit(cmd) {
		t.Fatal("prefix の待ちが解けず、後の f で閉じた")
	}
}

// 閉じるキーを渡されていない (ペインで直接起動・tmux の外) なら prefix → f で閉じない。
func TestNoToggleKeyDoesNotQuit(t *testing.T) {
	for _, c := range []struct{ name, toggle, prefix string }{
		{"閉じるキーなし", "", "ctrl+t"},
		{"prefix が取れない", "f", ""},
	} {
		a := newToggleApp(t, c.toggle, c.prefix)
		a.Update(keyPrefix)
		if _, cmd := a.Update(keyF); isQuit(cmd) {
			t.Fatalf("%s: prefix → f で閉じた", c.name)
		}
	}
}
