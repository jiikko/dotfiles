package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("ファイルが無いのはエラーではない: %v", err)
	}
	if !slices.Equal(c.RepoRoots, Default().RepoRoots) || !slices.Equal(c.Repos, Default().Repos) {
		t.Fatalf("既定値にならない: %+v", c)
	}
}

func TestLoadReadsKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	write(t, p, "repo_roots = [\"~/src\", \"/w\"]\nrepos = [\"~/dotfiles\"]\n")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.RepoRoots, []string{"~/src", "/w"}) || !slices.Equal(c.Repos, []string{"~/dotfiles"}) {
		t.Fatalf("読んだ値が違う: %+v", c)
	}
}

// 書き間違えたキーを黙って無視しない (「設定したのに効かない」が無音になる)。
func TestLoadRejectsUnknownKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	write(t, p, "repo_root = [\"~/src\"]\n")
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "repo_root") {
		t.Fatalf("知らないキーをエラーにして名前を出すはず: %v", err)
	}
}

func TestLoadRejectsBrokenTOML(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	write(t, p, "repo_roots = [\n")
	if _, err := Load(p); err == nil {
		t.Fatal("壊れた TOML がエラーにならない")
	}
}

// root の直下の git repo だけを拾い、~ を展開し、個別の repos も足す。
// .git がファイルの worktree も repo として拾う。孫ディレクトリの repo は拾わない。
func TestDiscover(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"src/a/.git", "src/b/.git", "src/notrepo", "src/nest/c/.git", "dotfiles/.git"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(home, "src/wt/.git"), "gitdir: /elsewhere\n")
	repos, warns := Discover(Config{RepoRoots: []string{"~/src"}, Repos: []string{"~/dotfiles"}}, home)
	var names []string
	for _, r := range repos {
		names = append(names, r.Name)
	}
	if want := []string{"a", "b", "dotfiles", "wt"}; !slices.Equal(names, want) {
		t.Fatalf("列挙が違う: got %v want %v", names, want)
	}
	if len(warns) != 0 {
		t.Fatalf("警告は無いはず: %v", warns)
	}
	if repos[2].Path != filepath.Join(home, "dotfiles") {
		t.Fatalf("~ が展開されていない: %s", repos[2].Path)
	}
}

// 1 つの書き間違いで全体を止めない。読めない root / repo でない repos / 名前の重複は警告にして続ける。
func TestDiscoverWarnsAndContinues(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"src/a/.git", "other/a/.git", "plain"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	repos, warns := Discover(Config{RepoRoots: []string{"~/src", "~/missing"}, Repos: []string{"~/other/a", "~/plain"}}, home)
	if len(repos) != 1 || repos[0].Path != filepath.Join(home, "src/a") {
		t.Fatalf("先に見つけた a だけが残るはず: %+v", repos)
	}
	joined := strings.Join(warns, "\n")
	for _, want := range []string{"~/missing", "~/plain", "重複"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("警告に %q が無い: %v", want, warns)
		}
	}
}
