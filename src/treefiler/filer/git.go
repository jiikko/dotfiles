package filer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jiikko/dotfiles/src/termsafe"
	"subproc"
)

// git.go は git の状態 (ファイルの印・フォルダの芽の色・ブランチ。spec §5.3)。
//
// 🚨 周期のタイマーは張らない (glogx の「止まっている間は tick を回さない」を崩さないため)。取り直すのは
// 開いたとき・読み直したとき・キーを押したときで、前回から gitMinInterval 以上たっていれば。treebeard は 3 秒周期。
// 見るのは root を含む repo 1 つだけ (treebeard は開いたフォルダごとの repo を見る。入れ子の repo の中は印が出ない)。

const gitMinInterval = 3 * time.Second

// 状態の重さ (重いほど強い。フォルダの芽は配下のいちばん重い色。ignored は祖先へ昇らない)。
const (
	gitNone byte = 0
	gitIgn  byte = 'i'
)

func gitRank(s byte) int {
	switch s {
	case '?':
		return 1
	case '+':
		return 2
	case 'M':
		return 3
	case '!':
		return 4
	}
	return 0
}

type gitSnapshot struct {
	top     string          // repo の根 (無ければ "")
	branch  string          // 表示用 (termsafe 済み)
	files   map[string]byte // repo の根からの相対パス → 状態
	whole   map[string]byte // 末尾 / のパス (未追跡・ignored のフォルダ一括) → 状態
	folders map[string]byte // 変更を含むフォルダ → 配下のいちばん重い状態
}

type gitWatch struct {
	mu      sync.Mutex
	snap    gitSnapshot
	version int
	running bool
	again   bool // 走っている間に force で頼まれた (終わったらもう 1 回取る。読み直しの直前の変更を落とさない)
	last    time.Time
}

// findRepoTop は dir から上へ .git (フォルダかファイル) を探す。
func findRepoTop(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// start は前回から gitMinInterval 以上たっていて走っていなければ取り直す。force は間隔を見ない。
func (g *gitWatch) start(root string, now time.Time, force bool) {
	g.mu.Lock()
	if g.running {
		g.again = g.again || force
		g.mu.Unlock()
		return
	}
	if !force && now.Sub(g.last) < gitMinInterval {
		g.mu.Unlock()
		return
	}
	g.running, g.last = true, now
	g.mu.Unlock()
	go func() {
		for {
			snap, ok := fetchGit(root)
			g.mu.Lock()
			if ok {
				g.snap = snap // 取れなかったときは前の結果を残す (一時的な失敗で印が全部消えてちらつかないように)
				g.version++
			}
			if !g.again {
				g.running = false
				g.mu.Unlock()
				return
			}
			g.again = false
			g.mu.Unlock()
		}
	}()
}

func (g *gitWatch) pending(have int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return have != g.version
}

func (g *gitWatch) busy() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.running
}

func (g *gitWatch) take(have int) (gitSnapshot, int, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if have == g.version {
		return gitSnapshot{}, have, false
	}
	return g.snap, g.version, true
}

// fetchGit は root を含む repo の状態を取る。ok=false は取れなかった (前の結果を残す)。repo の外なら空で ok。
func fetchGit(root string) (gitSnapshot, bool) {
	top := findRepoTop(root)
	if top == "" {
		return gitSnapshot{}, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), subproc.GitOpTimeout)
	defer cancel()
	cmd := subproc.CommandContext(ctx, "git", "--no-optional-locks", "-C", top,
		"status", "--porcelain=v1", "-z", "--branch", "--ignored", "--untracked-files=normal")
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		return gitSnapshot{}, false
	}
	s := parsePorcelain(out)
	s.top = top
	return s, true
}

// parsePorcelain は `status --porcelain=v1 -z --branch` の出力を読む。
func parsePorcelain(out []byte) gitSnapshot {
	s := gitSnapshot{files: map[string]byte{}, whole: map[string]byte{}, folders: map[string]byte{}}
	recs := bytes.Split(out, []byte{0})
	for i := 0; i < len(recs); i++ {
		r := string(recs[i])
		if r == "" {
			continue
		}
		if b, ok := strings.CutPrefix(r, "## "); ok {
			s.branch = termsafe.PlainLine(branchLabel(b))
			continue
		}
		if len(r) < 4 {
			continue
		}
		xy, path := r[:2], r[3:]
		if xy[0] == 'R' || xy[0] == 'C' || xy[1] == 'R' || xy[1] == 'C' {
			i++ // 次のレコードは元のパス (worktree 側の rename = `git add -N` の ` R` も同じ形)
		}
		st := classifyXY(xy)
		if dir, ok := strings.CutSuffix(path, "/"); ok {
			s.whole[dir] = st
		} else {
			s.files[path] = st
		}
		if st == gitIgn {
			continue // ignored は祖先へ昇らない
		}
		for d := filepath.Dir(strings.TrimSuffix(path, "/")); d != "." && d != "/"; d = filepath.Dir(d) {
			if gitRank(st) > gitRank(s.folders[d]) {
				s.folders[d] = st
			}
		}
	}
	return s
}

// gitWord はステータスバーの状態語 (spec §5.3 の St::describe)。
func gitWord(s byte) string {
	switch s {
	case '?':
		return "untracked"
	case '+':
		return "staged"
	case 'M':
		return "modified"
	case '!':
		return "conflict"
	case gitIgn:
		return "ignored"
	}
	return ""
}

func classifyXY(xy string) byte {
	switch {
	case xy == "??":
		return '?'
	case xy == "!!":
		return gitIgn
	case xy[0] == 'U' || xy[1] == 'U' || xy == "AA" || xy == "DD":
		return '!'
	case xy[1] != ' ':
		return 'M'
	}
	return '+'
}

// branchLabel は `main...origin/main [ahead 1, behind 2]` を `main ↑1 ↓2` にする (spec §5.3)。
func branchLabel(b string) string {
	if rest, ok := strings.CutPrefix(b, "No commits yet on "); ok {
		return rest
	}
	if strings.HasPrefix(b, "HEAD (no branch)") {
		return "detached"
	}
	name, track, _ := strings.Cut(b, "...")
	label := strings.Fields(name + " ")[0]
	if i := strings.Index(track, "["); i >= 0 {
		for part := range strings.SplitSeq(strings.Trim(track[i:], "[]"), ", ") {
			if n, ok := strings.CutPrefix(part, "ahead "); ok {
				label += " ↑" + n
			}
			if n, ok := strings.CutPrefix(part, "behind "); ok {
				label += " ↓" + n
			}
		}
	}
	return label
}

// state は abs (絶対パス) の状態。ファイルはそれ自身、フォルダは配下のいちばん重い状態。
// 未追跡・ignored のフォルダ一括 (末尾 /) の中は、その状態を引き継ぐ。
func (s gitSnapshot) state(abs string, dir bool) byte {
	if s.top == "" {
		return gitNone
	}
	prefix := s.top
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator) // root が / のとき // にしない
	}
	rel, ok := strings.CutPrefix(abs, prefix)
	if !ok {
		return gitNone
	}
	if dir {
		if st, ok := s.whole[rel]; ok {
			return st
		}
	} else if st, ok := s.files[rel]; ok {
		return st
	}
	for d := filepath.Dir(rel); d != "." && d != "/"; d = filepath.Dir(d) {
		if st, ok := s.whole[d]; ok {
			return st
		}
	}
	if dir {
		return s.folders[rel]
	}
	return gitNone
}
