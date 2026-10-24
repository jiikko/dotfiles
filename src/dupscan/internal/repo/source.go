package repo

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"subproc"
)

// Source は読むファイルの一覧と中身を返す。作業ツリーとある commit の 2 つの読み方を同じ形にそろえる。
type Source interface {
	// Name は出力に出す読み方の名前 (作業ツリーか commit の短い hash)。
	Name() string
	Files() ([]string, error)
	// ReadAll は files の中身を返す。commit の側は 1 回の git cat-file --batch でまとめて読む。
	ReadAll(files []string) (map[string][]byte, error)
	// ReadRoot は repo の root のファイル (設定ファイル) を読む。無ければ ok=false。
	ReadRoot(name string) (b []byte, ok bool, err error)
}

// Worktree は今の作業ツリー (未コミットの変更と、ignore されていない untracked のファイルを含む) を読む。
type Worktree struct {
	Scope Scope
	Exts  []string
}

// Name は読み方の名前。
func (w Worktree) Name() string { return "作業ツリー" }

// Files はファイルの一覧。
func (w Worktree) Files() ([]string, error) { return List(w.Scope, w.Exts) }

// ReadAll はファイルを読む。
func (w Worktree) ReadAll(files []string) (map[string][]byte, error) {
	out := make(map[string][]byte, len(files))
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(w.Scope.Root, f))
		if err != nil {
			return nil, err
		}
		out[f] = b
	}
	return out, nil
}

// ReadRoot は repo の root のファイルを読む。
func (w Worktree) ReadRoot(name string) ([]byte, bool, error) {
	b, err := os.ReadFile(filepath.Join(w.Scope.Root, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

// Commit はある commit の木を、checkout せずに git のオブジェクトから読む (調べる repo に書かない)。
type Commit struct {
	Scope Scope
	Exts  []string
	Hash  string            // 完全な commit の hash
	blobs map[string]string // Files が控えるパス → blob の hash (ReadAll はパスでなく hash で読む。パスの改行で読み違えない)
}

// Name は読み方の名前。
func (c *Commit) Name() string { return c.Hash[:min(len(c.Hash), 12)] }

// Files は commit の木のファイルの一覧。symlink (mode 120000) と submodule (160000) は読まない。
func (c *Commit) Files() ([]string, error) {
	args := []string{"ls-tree", "-r", "-z", "--full-tree", c.Hash, "--"}
	whole := false
	for _, p := range c.Scope.Prefixes {
		if p == "" {
			whole = true // 起点に repo の root があれば全体を読む (作業ツリーの側と範囲をそろえる)
		}
	}
	if !whole {
		args = append(args, c.Scope.Prefixes...)
	}
	out, err := git(c.Scope.Root, args...)
	if err != nil {
		return nil, err
	}
	var files []string
	c.blobs = map[string]string{}
	for _, rec := range strings.Split(out, "\x00") {
		meta, path, ok := strings.Cut(rec, "\t")
		if !ok || !hasExt(path, c.Exts) {
			continue
		}
		f := strings.Fields(meta)
		if len(f) < 3 || f[0] != "100644" && f[0] != "100755" || f[1] != "blob" {
			continue
		}
		c.blobs[path] = f[2]
		files = append(files, path)
	}
	sort.Strings(files)
	return files, nil
}

// ReadAll は commit の中のファイルを git cat-file --batch でまとめて読む。
func (c *Commit) ReadAll(files []string) (map[string][]byte, error) {
	if len(files) == 0 {
		return map[string][]byte{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), subproc.GitOpTimeout)
	defer cancel()
	cmd := subproc.GitCommand(ctx, "-C", c.Scope.Root, "cat-file", "--batch")
	cmd.Env = cleanEnv(os.Environ())
	var in bytes.Buffer
	for _, f := range files {
		b, ok := c.blobs[f]
		if !ok {
			return nil, fmt.Errorf("%s は Files で列挙していない", f)
		}
		fmt.Fprintf(&in, "%s\n", b)
	}
	cmd.Stdin = &in
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(files))
	r := bufio.NewReader(stdout)
	for _, f := range files {
		header, err := r.ReadString('\n')
		if err != nil {
			_ = cmd.Wait()
			return nil, fmt.Errorf("git cat-file の出力を読めない (%s): %w: %s", f, err, strings.TrimSpace(stderr.String()))
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[1] != "blob" {
			_ = cmd.Wait()
			return nil, fmt.Errorf("git cat-file が %s:%s を blob として返さない: %q", c.Name(), f, strings.TrimSpace(header))
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			_ = cmd.Wait()
			return nil, err
		}
		body := make([]byte, size+1) // 末尾の改行まで
		if _, err := io.ReadFull(r, body); err != nil {
			_ = cmd.Wait()
			return nil, err
		}
		out[f] = body[:size]
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("git cat-file: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// ReadRoot は commit の木の root のファイルを読む (checkout しない)。無ければ ok=false。
func (c *Commit) ReadRoot(name string) ([]byte, bool, error) {
	out, err := git(c.Scope.Root, "ls-tree", "-z", "--full-tree", c.Hash, "--", name)
	if err != nil {
		return nil, false, err
	}
	meta, _, found := strings.Cut(strings.TrimSuffix(out, "\x00"), "\t")
	if !found {
		return nil, false, nil
	}
	f := strings.Fields(meta)
	if len(f) < 3 || f[1] != "blob" {
		return nil, false, fmt.Errorf("%s:%s はファイルではない", c.Name(), name)
	}
	b, err := git(c.Scope.Root, "cat-file", "blob", f[2])
	return []byte(b), err == nil, err
}

// ErrNoBase は base を決められないこと (判定不能)。
var ErrNoBase = errors.New("base を決められない")

// CommitHash は ref を完全な commit の hash にする。手元に無い (浅い clone など) ときは ErrNoBase。
func CommitHash(root, ref string) (string, error) {
	out, err := git(root, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%w: %s が手元に無い (浅い clone なら base まで fetch する): %w", ErrNoBase, ref, err)
	}
	return strings.TrimSpace(out), nil
}

// MergeBase は a と b の merge-base。
func MergeBase(root, a, b string) (string, error) {
	out, err := git(root, "merge-base", a, b)
	if err != nil {
		return "", fmt.Errorf("%w: %s と %s の merge-base が無い: %w", ErrNoBase, a, b, err)
	}
	return strings.TrimSpace(out), nil
}

// DefaultBase は base の既定: 今のブランチの upstream、無ければ origin/HEAD。どちらも無ければ ErrNoBase
// (detached HEAD の worktree では upstream が無い)。
func DefaultBase(root string) (string, error) {
	for _, ref := range []string{"@{upstream}", "refs/remotes/origin/HEAD"} {
		if h, err := CommitHash(root, ref); err == nil {
			return h, nil
		}
	}
	return "", fmt.Errorf("%w: upstream も origin/HEAD も無い。-base で base を渡す", ErrNoBase)
}
