// Package repo は、調べる repo のファイルを git で列挙する。調べる repo には何も書かない。
package repo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"subproc"
)

// Scope は repo の root と、走査の起点 (root からの相対の prefix)。
type Scope struct {
	Root     string
	Prefixes []string // 空の文字列は repo 全体
}

// Resolve は起点のディレクトリから repo の root と prefix を決める。
// prefix は git に正規化させる: 大文字小文字の違う起点 (`macos`) を字面のまま pathspec に渡すと 0 件になる。
func Resolve(dirs []string) (Scope, error) {
	var s Scope
	for _, d := range dirs {
		root, err := git(d, "rev-parse", "--show-toplevel")
		if err != nil {
			return Scope{}, fmt.Errorf("%s は git の repo の中ではない: %w", d, err)
		}
		root = strings.TrimSpace(root)
		if s.Root != "" && s.Root != root {
			return Scope{}, fmt.Errorf("起点が別々の repo にある: %s と %s", s.Root, root)
		}
		s.Root = root
		prefix, err := git(d, "rev-parse", "--show-prefix")
		if err != nil {
			return Scope{}, err
		}
		s.Prefixes = append(s.Prefixes, strings.TrimSpace(prefix))
	}
	return s, nil
}

// Whole は起点に repo の root が入っているか。
func (s Scope) Whole() bool {
	for _, p := range s.Prefixes {
		if p == "" {
			return true
		}
	}
	return false
}

// Contains は root からの相対パスが起点の中か (起点は `git rev-parse --show-prefix` の形で、末尾が / のディレクトリ)。
func (s Scope) Contains(file string) bool {
	for _, p := range s.Prefixes {
		if strings.HasPrefix(file, p) {
			return true
		}
	}
	return false
}

// List は起点の中の、拡張子の合うファイルを root からの相対で返す。
// 追跡しているファイルと、ignore されていない untracked のファイルを含む。symlink・消えたファイル・submodule は含めない。
func List(s Scope, exts []string) ([]string, error) {
	args := []string{"ls-files", "-z", "--cached", "--others", "--exclude-standard", "--"}
	for _, p := range s.Prefixes {
		// repo の root は `:(top)` だけにする (`:(top).` は何にも一致しない)。
		args = append(args, ":(top)"+p)
	}
	out, err := git(s.Root, args...)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f == "" || seen[f] || !hasExt(f, exts) {
			continue
		}
		seen[f] = true
		fi, err := os.Lstat(filepath.Join(s.Root, f))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // 追跡しているが作業ツリーで消えたファイル
			}
			return nil, err
		}
		// symlink は辿らない: 別ターゲットに取り込んだ同じファイルを二重に数えない。
		if !fi.Mode().IsRegular() {
			continue
		}
		files = append(files, f)
	}
	sort.Strings(files)
	return files, nil
}

func hasExt(f string, exts []string) bool {
	for _, e := range exts {
		if strings.HasSuffix(f, e) {
			return true
		}
	}
	return false
}

// git は調べる repo に書かない形で git を打つ。subproc.GitCommand が --no-optional-locks を付け、status 系が .git/index を書き直すのを止める。
// 呼び出し元 (hook・make) から継承した GIT_DIR などは外す (付いていると -C の先ではなくそちらを読む)。
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), subproc.GitOpTimeout)
	defer cancel()
	cmd := subproc.GitCommand(ctx, append([]string{"-C", dir}, args...)...)
	cmd.Env = cleanEnv(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func cleanEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !subproc.IsGitLocationEnv(k) {
			out = append(out, kv)
		}
	}
	return out
}
