package wtclean

// 判定に使う git の読み取り。🚨 ここには ref・作業ツリー・index を動かすコマンドを足さない (動かすのは clean.go だけ)。
// status は --no-optional-locks で走らせる (index を書き直さない)。

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"pro-con/gitx"
)

// worktree は `git worktree list --porcelain` の 1 個。
type worktree struct {
	Path       string
	Head       string
	Branch     string // 短い名前。detached なら空
	Locked     bool
	LockReason string
	Prunable   bool
}

func listWorktrees(ctx context.Context, repo string) ([]worktree, error) {
	out, _, err := gitx.Run(ctx, repo, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	var wts []worktree
	var cur *worktree
	for _, f := range strings.Split(out, "\x00") {
		k, val, _ := strings.Cut(f, " ")
		switch k {
		case "worktree":
			wts = append(wts, worktree{Path: val})
			cur = &wts[len(wts)-1]
		case "HEAD":
			if cur != nil {
				cur.Head = val
			}
		case "branch":
			if cur != nil {
				cur.Branch = strings.TrimPrefix(val, "refs/heads/")
			}
		case "locked":
			if cur != nil {
				cur.Locked, cur.LockReason = true, val
			}
		case "prunable":
			if cur != nil {
				cur.Prunable = true
			}
		}
	}
	return wts, nil
}

// status は worktree を消すと失うもの: 未 commit の変更 (追跡していないファイルも)・git status に出ない変更の印・無視されたファイル。
type status struct {
	changes []string
	hidden  []string // skip-worktree / assume-unchanged の印 (sparse-checkout も付ける)。変更しても git status に出ない
	ignored []string // 無視されたファイルのうち、作り直せないもの (rebuildable でないもの)
}

func readStatus(ctx context.Context, wt string) (status, error) {
	out, _, err := gitx.Run(ctx, wt, "--no-optional-locks", "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored=matching")
	if err != nil {
		return status{}, err
	}
	var st status
	ents := strings.Split(out, "\x00")
	for i := 0; i < len(ents); i++ {
		e := ents[i]
		if len(e) < 4 {
			continue
		}
		xy, path := e[:2], e[3:]
		switch {
		case xy == "!!":
			files, err := filesUnder(filepath.Join(wt, path))
			if err != nil {
				return status{}, err
			}
			for _, f := range files {
				if rel := filepath.Join(path, f); !rebuildable(wt, rel) {
					st.ignored = append(st.ignored, rel)
				}
			}
		default:
			st.changes = append(st.changes, path)
			if xy[0] == 'R' || xy[0] == 'C' { // 名前を変えた・写した行は、次の欄が元の名前
				i++
			}
		}
	}
	ls, _, err := gitx.Run(ctx, wt, "--no-optional-locks", "ls-files", "-v", "-z")
	if err != nil {
		return status{}, err
	}
	for _, e := range strings.Split(ls, "\x00") {
		if len(e) < 3 {
			continue
		}
		if tag := e[0]; tag == 'S' || (tag >= 'a' && tag <= 'z') {
			st.hidden = append(st.hidden, e[2:])
		}
	}
	return st, nil
}

// rebuildable は、消しても作り直せる無視されたファイルか: bin/lib/go_autobuild.zsh の作業ファイル (.autobuild.*) と、
// .autobuild.built の隣に置いた実行ファイル (autobuild が作ったバイナリ)。それ以外 (tmp/ のレポート・.env・settings.local.json・
// 入れ子の repo の中身) は作り直せないとみなす。
func rebuildable(wt, rel string) bool {
	base := filepath.Base(rel)
	if strings.HasPrefix(base, ".autobuild.") {
		return true
	}
	full := filepath.Join(wt, rel)
	fi, err := os.Lstat(full)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return false
	}
	_, err = os.Lstat(filepath.Join(filepath.Dir(full), ".autobuild.built"))
	return err == nil
}

// filesUnder は p の下のファイル (ディレクトリでないもの。symlink も数える) を p からの相対で返す。p がファイルなら "." 。
// 空のディレクトリは数えない (git は無視された空の tmp/ も !! で出す。2026-09-26 の実物では 40 個中 34 個が空)。
func filesUnder(p string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(p, path)
			out = append(out, rel)
		}
		return nil
	})
	return out, err
}

func countMerges(ctx context.Context, repo, base, head string) (int, error) {
	out, _, err := gitx.Run(ctx, repo, "rev-list", "--count", "--merges", base+".."+head)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("git rev-list --count の出力を読めない: %q", out)
	}
	return n, nil
}

// verbatimMissing は base..head の commit のうち、空白まで同じ patch (git patch-id --verbatim) が head..base に無いものの本数。
// git cherry と同じ組を比べるが、🚨 git cherry の patch-id は空白の違いを落とすので、インデントだけ違う変更 (Python・YAML・Makefile では
// 中身の違い) を「同じ」と読む。こちらで空白まで比べ直す。中身の無い commit (空の commit) は比べる patch が無いので数えない
func verbatimMissing(ctx context.Context, repo, base, head string) (int, error) {
	have, err := patchIDs(ctx, repo, head+".."+base)
	if err != nil {
		return 0, err
	}
	want, err := patchIDs(ctx, repo, base+".."+head)
	if err != nil {
		return 0, err
	}
	n := 0
	for id := range want {
		if !have[id] {
			n++
		}
	}
	return n, nil
}

// patchIDs は範囲の merge でない commit の patch-id (--verbatim) の集合。diff の形は設定に左右されないよう固定する。
func patchIDs(ctx context.Context, repo, rng string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, gitx.Timeout)
	defer cancel()
	log := exec.CommandContext(ctx, "git", "-C", repo, "-c", "diff.noprefix=false", "-c", "diff.mnemonicPrefix=false", "log", "--no-merges", "-p",
		"--no-renames", "--no-color", "--no-ext-diff", "--no-textconv", "--full-index", "--format=commit %H", rng, "--")
	pid := exec.CommandContext(ctx, "git", "-C", repo, "patch-id", "--verbatim")
	log.Env, pid.Env = gitx.Env(), gitx.Env()
	pipe, err := log.StdoutPipe()
	if err != nil {
		return nil, err
	}
	pid.Stdin = pipe
	var out bytes.Buffer
	pid.Stdout = &out
	if err := log.Start(); err != nil {
		return nil, err
	}
	if err := pid.Run(); err != nil {
		_ = log.Wait()
		return nil, fmt.Errorf("git patch-id: %w", err)
	}
	if err := log.Wait(); err != nil {
		return nil, fmt.Errorf("git log -p %s: %w", rng, err)
	}
	ids := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if id, _, ok := strings.Cut(l, " "); ok {
			ids[id] = true
		}
	}
	return ids, nil
}

// cherry は `git cherry base head` の + (base に無い) と - (同じ patch が base にある) の本数。
func cherry(ctx context.Context, repo, base, head string) (plus, minus int, err error) {
	out, _, err := gitx.Run(ctx, repo, "cherry", base, head)
	if err != nil {
		return 0, 0, err
	}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		switch {
		case l == "":
		case strings.HasPrefix(l, "+ "):
			plus++
		case strings.HasPrefix(l, "- "):
			minus++
		default:
			return 0, 0, fmt.Errorf("git cherry の出力を読めない: %q", l)
		}
	}
	return plus, minus, nil
}

// claudeLockRe は Claude Code が `claude -w <name>` の session に掛ける lock の理由 (2.1.282 で実測 2026-09-26:
// "claude session pc-c-025 (pid 5442 start Fri Sep 25 14:15:38 2026)")。形が変わったら人が掛けた lock として残す側に倒れる。
var claudeLockRe = regexp.MustCompile(`^claude session (\S+) \(pid ([0-9]+) start [^)]*\)$`)

// claudeLock は、その worktree の名前で Claude Code が掛けた lock か。
func claudeLock(reason, name string) bool {
	m := claudeLockRe.FindStringSubmatch(reason)
	return m != nil && m[1] == name
}

// lockHolderAlive は lock の pid に claude のプロセスが居るか (居れば、lock を掛けた session がまだ居るかもしれないので残す)。
// pid は使い回されるので、居てもコマンドが claude でなければ別のプロセス。ps を読めなければ居る側に倒す。
func lockHolderAlive(reason string) bool {
	m := claudeLockRe.FindStringSubmatch(reason)
	if m == nil {
		return true
	}
	out, err := exec.Command("ps", "-p", m[2], "-o", "command=").Output()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return strings.Contains(string(out), "claude")
	case errors.As(err, &exit) && exit.ExitCode() == 1 && strings.TrimSpace(string(out)) == "": // その pid のプロセスが無い
		return false
	}
	return true
}
