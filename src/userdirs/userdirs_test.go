package userdirs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x/cfg")
	if d, err := ConfigDir("tool"); err != nil || d != "/x/cfg/tool" {
		t.Fatalf("XDG あり: %q %v", d, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/h")
	if d, err := ConfigDir("tool"); err != nil || d != "/h/.config/tool" {
		t.Fatalf("XDG なし: %q %v", d, err)
	}
}

func TestMoveIfMissing(t *testing.T) {
	d := t.TempDir()
	oldP, newP := filepath.Join(d, "old", "a.toml"), filepath.Join(d, "new", "sub", "a.toml")
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }

	// 古い方だけ → 移る
	write(oldP, "old")
	if err := MoveIfMissing(oldP, newP); err != nil || read(newP) != "old" {
		t.Fatalf("移っていない: %q %v", read(newP), err)
	}
	if _, err := os.Stat(oldP); !os.IsNotExist(err) {
		t.Fatal("古い方が残っている")
	}
	// 両方ある → 新しい方を上書きしない
	write(oldP, "old2")
	if err := MoveIfMissing(oldP, newP); err != nil || read(newP) != "old" || read(oldP) != "old2" {
		t.Fatalf("上書きした: new=%q old=%q %v", read(newP), read(oldP), err)
	}
	// どちらも無い → 何もしない
	if err := MoveIfMissing(filepath.Join(d, "none"), filepath.Join(d, "none2")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "none2")); !os.IsNotExist(err) {
		t.Fatal("無いのに作った")
	}
}

// ConfigFile は前の置き場 (~ からの相対) にだけあるファイルを、新しい置き場 ($XDG_CONFIG_HOME/<app>/<name>) へ移して返す。
func TestConfigFileMovesLegacy(t *testing.T) {
	home, xdg := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	legacy := filepath.Join(".config", "glogx", "tool", "a.toml")
	if err := os.MkdirAll(filepath.Join(home, filepath.Dir(legacy)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, legacy), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := ConfigFile("tool", "a.toml", legacy)
	if err != nil || p != filepath.Join(xdg, "tool", "a.toml") {
		t.Fatalf("置き場: %q %v", p, err)
	}
	if b, err := os.ReadFile(p); err != nil || string(b) != "x" {
		t.Fatalf("移っていない: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(home, legacy)); !os.IsNotExist(err) {
		t.Fatal("前の置き場に残っている")
	}
}
