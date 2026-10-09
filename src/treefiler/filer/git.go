package filer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
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
// 見るのは root と開いているフォルダが属する repo のすべて (入れ子の repo・root が repo の外で配下に repo が並ぶ場合も。spec §5.3)。

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
	overall byte            // repo 全体のいちばん重い状態 (repo の根のフォルダの芽の色)
	prefix  string          // top + 区切り (取ったときに 1 度だけ作る。state は毎フレーム項目ごとに呼ばれる)
}

// gitSet は repo ごとの状態 (深い repo が先)。パスの状態は、そのパスを含むいちばん深い repo で引く。
type gitSet struct {
	repos []gitSnapshot
}

func (g gitSet) find(abs string) *gitSnapshot {
	if i := g.findFrom(abs, 0); i >= 0 {
		return &g.repos[i]
	}
	return nil
}

// findFrom は repos[from:] から abs を含むいちばん深い repo の位置を返す (無ければ -1)。
func (g gitSet) findFrom(abs string, from int) int {
	for i := from; i < len(g.repos); i++ {
		r := &g.repos[i]
		if abs == r.top || strings.HasPrefix(abs, r.sep()) {
			return i
		}
	}
	return -1
}

// sep は top + 区切り。🚨 ここで prefix を書き込まない: gitSet は git の goroutine (前の結果の引き継ぎ) と UI の goroutine が
// 同じ配列を読むので、遅延で書くと競合する。作るのは取ったとき (fetchRepo)。
func (s *gitSnapshot) sep() string {
	if s.prefix != "" {
		return s.prefix
	}
	return withSep(s.top)
}

func withSep(p string) string {
	if strings.HasSuffix(p, string(filepath.Separator)) {
		return p // root が / のとき // にしない
	}
	return p + string(filepath.Separator)
}

func (g gitSet) state(abs string, dir bool) byte {
	for from := 0; ; {
		i := g.findFrom(abs, from)
		if i < 0 {
			return gitNone
		}
		r := &g.repos[i]
		switch {
		case abs != r.top:
			return r.state(abs, dir)
		case r.overall != gitNone:
			return r.overall // 入れ子の repo の根: 中の変更の色
		}
		// 中に変更の無い入れ子の repo の根は、外側の repo から見た状態 (未追跡・無視) にする
		// (開いて repo の数に入った途端に、無視の灰色・細線が消えないように)
		from = i + 1
	}
}

// branchFor は abs を含む repo のブランチ (repo の外なら "")。
func (g gitSet) branchFor(abs string) string {
	if r := g.find(abs); r != nil {
		return r.branch
	}
	return ""
}

type gitWatch struct {
	mu      sync.Mutex
	snap    gitSet
	tops    []string // 次に取る repo の根 (start で差し替える)
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

// start は前回から gitMinInterval 以上たっていて走っていなければ、tops の repo を取り直す。force は間隔を見ない。
// 取る repo の集まりが前回と違えば force と同じ (開いたフォルダが別の repo なら待たずに印を出す)。
func (g *gitWatch) start(tops []string, now time.Time, force bool) {
	g.mu.Lock()
	if !slices.Equal(tops, g.tops) {
		g.tops, force = tops, true
	}
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
			g.mu.Lock()
			tops, prev := g.tops, g.snap
			g.mu.Unlock()
			set := gitSet{repos: make([]gitSnapshot, 0, len(tops))}
			for _, top := range tops {
				if snap, ok := fetchRepo(top); ok {
					set.repos = append(set.repos, snap)
				} else if old := prev.find(top); old != nil && old.top == top {
					set.repos = append(set.repos, *old) // 取れなかった repo は前の結果を残す (一時的な失敗で印が全部消えてちらつかないように)
				}
			}
			g.mu.Lock()
			g.snap = set
			g.version++
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

func (g *gitWatch) take(have int) (gitSet, int, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if have == g.version {
		return gitSet{}, have, false
	}
	return g.snap, g.version, true
}

// repoTops は dirs が属する repo の根を、深いものから重複なく返す (find が深い repo を先に引けるように)。
func repoTops(dirs []string, cache map[string]string) []string {
	seen := map[string]bool{}
	var tops []string
	for _, d := range dirs {
		top, ok := cache[d]
		if !ok {
			top = findRepoTop(d)
			cache[d] = top
		}
		if top != "" && !seen[top] {
			seen[top] = true
			tops = append(tops, top)
		}
	}
	sort.Slice(tops, func(i, j int) bool {
		if len(tops[i]) != len(tops[j]) {
			return len(tops[i]) > len(tops[j])
		}
		return tops[i] < tops[j]
	})
	return tops
}

// fetchRepo は repo の根 top の状態を取る。ok=false は取れなかった (前の結果を残す)。
func fetchRepo(top string) (gitSnapshot, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), subproc.GitOpTimeout)
	defer cancel()
	cmd := subproc.GitCommand(ctx, "--no-optional-locks", "-C", top,
		"status", "--porcelain=v1", "-z", "--branch", "--ignored", "--untracked-files=normal")
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		return gitSnapshot{}, false
	}
	s := parsePorcelain(out)
	s.top, s.prefix = top, withSep(top)
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
		if gitRank(st) > gitRank(s.overall) {
			s.overall = st
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
	fs := strings.Fields(name)
	if len(fs) == 0 {
		return "" // 空の名前で [0] を引かない (git の goroutine で panic するとプロセスごと落ちる)
	}
	label := fs[0]
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
func (s *gitSnapshot) state(abs string, dir bool) byte {
	if s.top == "" {
		return gitNone
	}
	rel, ok := strings.CutPrefix(abs, s.sep())
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
