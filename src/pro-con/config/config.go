// Package config は pro-con の設定ファイル (~/.config/pro-con/config.toml) と、そこから repo を列挙する処理。
//
//	repo_roots = ["~/src"]      # この直下の git repo を列挙する (深くは掘らない)
//	repos      = ["~/dotfiles"] # root の外にある repo を個別に足す
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	RepoRoots []string `toml:"repo_roots"`
	Repos     []string `toml:"repos"`
}

// Default はファイルが無いときの設定。dotfiles は ~/src の外にあるので個別に足している
// (無いと、今いちばん使う repo がタブに出ない)。
func Default() Config {
	return Config{RepoRoots: []string{"~/src"}, Repos: []string{"~/dotfiles"}}
}

// DefaultPath は $XDG_CONFIG_HOME/pro-con/config.toml (未設定なら ~/.config/pro-con/config.toml)。
func DefaultPath(home string) string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "pro-con", "config.toml")
}

// Load は path を読む。ファイルが無ければ Default を返す (エラーにしない)。
// 壊れた TOML と知らないキーはエラーにする (書き間違えたキーを黙って無視すると「設定したのに効かない」が無音で起きる)。
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	md, err := toml.Decode(string(data), &c)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, len(und))
		for i, k := range und {
			keys[i] = k.String()
		}
		return Config{}, fmt.Errorf("%s: 知らないキー %s (使えるのは repo_roots / repos)", path, strings.Join(keys, ", "))
	}
	return c, nil
}

// Repo は列挙された repo。Name はカードの Repo と突き合わせる名前 (ディレクトリ名)。
type Repo struct {
	Name string
	Path string
}

// Discover は設定から repo を列挙する。root の直下で .git (ディレクトリか、worktree の .git ファイル) を持つものだけ。
// 同じ名前が 2 つ見つかったら先に見つけた方を採り、後の方は warnings に入れる (カードは名前で repo を指すので区別できない)。
// 存在しない root / repo も warnings に入れて続ける (1 つの書き間違いで全体を止めない)。
func Discover(c Config, home string) (repos []Repo, warnings []string) {
	seen := map[string]string{}
	add := func(path string) {
		name := filepath.Base(path)
		if prev, ok := seen[name]; ok {
			if prev != path {
				warnings = append(warnings, fmt.Sprintf("repo 名 %s が重複している (%s を採り、%s は外した)", name, prev, path))
			}
			return
		}
		seen[name] = path
		repos = append(repos, Repo{Name: name, Path: path})
	}
	for _, root := range c.RepoRoots {
		dir := expand(root, home)
		entries, err := os.ReadDir(dir)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("repo_roots の %s を読めない: %v", root, err))
			continue
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if e.IsDir() && isRepo(p) {
				add(p)
			}
		}
	}
	for _, r := range c.Repos {
		p := expand(r, home)
		if !isRepo(p) {
			warnings = append(warnings, fmt.Sprintf("repos の %s は git repo ではない", r))
			continue
		}
		add(p)
	}
	slices.SortFunc(repos, func(a, b Repo) int { return strings.Compare(a.Name, b.Name) })
	return repos, warnings
}

func isRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// expand は先頭の ~ を home に置き換える (~user の形は扱わない)。
func expand(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
