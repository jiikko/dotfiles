package wtclean

// 判定に使う git の読み取り。🚨 ここには ref・作業ツリー・index を動かすコマンドを足さない (動かすのは clean.go だけ)。
// status は --no-optional-locks で走らせる (index を書き直さない)。

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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

// status は worktree の未 commit の変更 (追跡していないファイルも) と、消すと失う無視されたファイルのうち作業の残り (tmp/ の下)。
// 🚨 tmp/ は Claude のセッションの成果物の置き場 (~/.claude/CLAUDE.md「一時ファイルの配置」)。無視されているので git status の
// 変更には出ないが、結論を issue へ移す前のレポートが残っているかもしれない。ほかの無視されたファイル (ビルドの産物) は作り直せるので見ない
type status struct {
	changes   []string
	workFiles []string
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
			if hasSegment(path, "tmp") {
				files, err := filesUnder(filepath.Join(wt, path))
				if err != nil {
					return status{}, err
				}
				for _, f := range files {
					st.workFiles = append(st.workFiles, filepath.Join(path, f))
				}
			}
		default:
			st.changes = append(st.changes, path)
			if xy[0] == 'R' || xy[0] == 'C' { // 名前を変えた・写した行は、次の欄が元の名前
				i++
			}
		}
	}
	return st, nil
}

// filesUnder は p の下のファイル (ディレクトリでないもの。symlink も数える) を p からの相対で返す。p がファイルなら "." 。
// 空のディレクトリは数えない (git は無視された空の tmp/ も !! で出す。2026-09-26 の実物では 40 個中 40 個が空)。
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

func hasSegment(path, seg string) bool {
	for _, s := range strings.Split(strings.TrimSuffix(path, "/"), "/") {
		if s == seg {
			return true
		}
	}
	return false
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
