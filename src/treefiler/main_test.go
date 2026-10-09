package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"treefiler/filer"
)

// q で終わるとき Close を呼ぶ (Remember place が保存される。issue 691)。
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
