package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"

	"treefiler/filer"
)

// cdApp は root の下に a/ (フォルダ) と b.txt を置き、cwdFile を渡した app を返す。カーソルは最初の子の a。
func cdApp(t *testing.T, cwdFile string) (*app, string) {
	t.Helper()
	t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := filer.New(root, filer.Options{CdOnQuit: cwdFile != ""})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{f: f, cwdFile: cwdFile}
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return a, root
}

var (
	keyLowQ = tea.KeyPressMsg{Code: 'q', Text: "q"}
	keyJ    = tea.KeyPressMsg{Code: 'j', Text: "j"}
	keyL    = tea.KeyPressMsg{Code: 'l', Text: "l"}
)

// q で終えると、カーソルのフォルダ (ファイルの上ならその親) を行き先に控える。
func TestQuitWithCwdFileRecordsWorkDir(t *testing.T) {
	a, root := cdApp(t, "x")
	if _, cmd := a.Update(keyLowQ); !isQuit(cmd) {
		t.Fatal("q で終わらない")
	}
	if want := filepath.Join(root, "a"); a.cdTo != want {
		t.Fatalf("フォルダの上の q の行き先 = %q、want %q", a.cdTo, want)
	}
	b, root := cdApp(t, "x")
	b.Update(keyJ) // b.txt
	b.Update(keyLowQ)
	if b.cdTo != root {
		t.Fatalf("ファイルの上の q の行き先 = %q、want 親の %q", b.cdTo, root)
	}
}

// esc・Ctrl-C では cd しない (cd せずに抜ける道)。--cwd-file が無ければ q でも控えない。
func TestQuitWithoutCd(t *testing.T) {
	for _, c := range []struct {
		name    string
		cwdFile string
		key     tea.KeyPressMsg
	}{
		{"esc", "x", tea.KeyPressMsg{Code: tea.KeyEscape}},
		{"ctrl+c", "x", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}},
		{"--cwd-file なしの q", "", keyLowQ},
	} {
		a, _ := cdApp(t, c.cwdFile)
		if _, cmd := a.Update(c.key); !isQuit(cmd) {
			t.Fatalf("%s: 終わらない", c.name)
		}
		if a.cdTo != "" {
			t.Fatalf("%s: 行き先を控えた %q", c.name, a.cdTo)
		}
	}
}

// タイルの上の q はタイルを閉じるだけで、終わらず行き先も控えない。
func TestQuitOnTileClosesTileOnly(t *testing.T) {
	a, _ := cdApp(t, "x")
	a.Update(keyJ) // b.txt
	a.Update(keyL) // タイルを開く
	if _, cmd := a.Update(keyLowQ); isQuit(cmd) {
		t.Fatal("タイルの上の q で終わった")
	}
	if a.cdTo != "" {
		t.Fatalf("タイルの上の q で行き先を控えた %q", a.cdTo)
	}
}

func TestParseArgs(t *testing.T) {
	for _, c := range []struct {
		args          []string
		dir, cwdFile  string
		help, wantErr bool
	}{
		{nil, ".", "", false, false},
		{[]string{"d"}, "d", "", false, false},
		{[]string{"--cwd-file", "p", "d"}, "d", "p", false, false},
		{[]string{"d", "--cwd-file=p"}, "d", "p", false, false},
		{[]string{"-h"}, "", "", true, false},
		{[]string{"--cwd-file"}, "", "", false, true},
		{[]string{"--cwd-file="}, "", "", false, true},
		{[]string{"--cwd-file", ""}, "", "", false, true},
		{[]string{"-x"}, "", "", false, true},
		{[]string{"d", "e"}, "", "", false, true},
	} {
		dir, cwdFile, help, err := parseArgs(c.args)
		if (err != nil) != c.wantErr || help != c.help || (err == nil && !help && (dir != c.dir || cwdFile != c.cwdFile)) {
			t.Errorf("parseArgs(%q) = (%q, %q, %v, %v)", c.args, dir, cwdFile, help, err)
		}
	}
}

// cd して終える起動では、キー案内の q を cd と書き、? の一覧で esc は cd しないと書く。
func TestCdOnQuitHints(t *testing.T) {
	for _, c := range []struct {
		cwdFile   string
		hint      string
		helpLines []string
	}{
		{"x", "q cd ", []string{"そのフォルダへ cd して終了", "cd せずに終了"}},
		{"", "q quit ", []string{"q esc"}},
	} {
		a, _ := cdApp(t, c.cwdFile)
		if v := termwidth.StripSGR(strings.Join(a.f.View(), "\n")); !strings.Contains(v, c.hint) {
			t.Fatalf("cwdFile=%q: キー案内に %q が無い", c.cwdFile, c.hint)
		}
		a.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
		v := termwidth.StripSGR(strings.Join(a.f.View(), "\n"))
		for _, l := range c.helpLines {
			if !strings.Contains(v, l) {
				t.Fatalf("cwdFile=%q: ? の一覧に %q が無い", c.cwdFile, l)
			}
		}
	}
}

// 本物の bubbletea を回して、q では --cwd-file に行き先を改行なしで書き、Ctrl-C では書かない (呼び出したシェルが読む形)。
func TestRunWritesCwdFileOnQ(t *testing.T) {
	for _, c := range []struct {
		name, input string
		want        func(root string) string // "" = 書かない
	}{
		{"q", "q", func(root string) string { return filepath.Join(root, "a") }},
		{"ctrl+c", "\x03", func(string) string { return "" }},
	} {
		t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir())
		t.Setenv(perfLogEnv, "") // 開発者の環境の値で本物のログへ書かない
		t.Setenv(debugEnv, "")
		t.Setenv("TMUX", "") // tmux の中で回しても、prefix を本番のサーバへ問い合わせない
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(root, "a"), 0o755); err != nil {
			t.Fatal(err)
		}
		cwdFile := filepath.Join(t.TempDir(), "cd")
		rc := run([]string{"--cwd-file", cwdFile, root}, []tea.ProgramOption{tea.WithInput(strings.NewReader(c.input)), tea.WithOutput(io.Discard)})
		if rc != 0 {
			t.Fatalf("%s: rc=%d", c.name, rc)
		}
		got, err := os.ReadFile(cwdFile)
		switch want := c.want(root); {
		case want == "" && !errors.Is(err, os.ErrNotExist):
			t.Fatalf("%s: 書かないはずが %q (err=%v)", c.name, got, err)
		case want != "" && string(got) != want:
			t.Fatalf("%s: 書いた行き先 = %q (err=%v)、want %q (改行なし)", c.name, got, err, want)
		}
	}
}
