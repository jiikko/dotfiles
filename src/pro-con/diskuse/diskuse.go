// Package diskuse は pro-con が作った物のディスクの使用量と内訳を測る (issue 456。読むだけ)。
//
// 数えるのは pro-con が作った物だけ: 起動の記録 (sessions.json と sessions-retired.json) にある session の worktree
// (<repo>/.claude/worktrees/pc-*) と transcript の置き場、状態の置き場、pro-con のバイナリ。repo の本体や pro-con の外の
// session の transcript は数えない。
//
// 🚨 測るのは重い (worktree 数十個を歩くので数秒)。画面は描くたびに呼ばず、開いたときと測り直しのキーで裏で 1 回呼ぶ。
// 🚨 何も書かない・消さない (片付けはこの package の範囲外)。
package diskuse

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// WorktreeMarker は pro-con の worktree のパスに入る部分 (claude --bg -w pc-<名前> が作る。dispatcher の worktreeMarker と同じ)。
const WorktreeMarker = "/.claude/worktrees/pc-"

// Session は起動の記録の 1 行のうち、測るのに要る欄 (live.Owned から作る。この package は live を知らない)。
type Session struct {
	SessionID string
	CardID    string
	Cwd       string
}

// Input は測る対象。呼ぶ側が状態の置き場と起動の記録から組む。
type Input struct {
	StateDir string           // 状態の置き場 (~/.local/state/pro-con)
	Projects string           // transcript の置き場 (~/.claude/projects)
	Binary   string           // pro-con のバイナリ (空なら数えない)
	Sessions []Session        // 起動の記録 (今の分と退いた分)
	Done     map[string]bool  // カード ID → 完了のレーンに居るか (worktree の内訳の印)
	Now      func() time.Time // 測った時刻 (nil なら time.Now)
}

// 置き場の名前 (Group.Name)。
const (
	GroupWorktrees   = "worktree"
	GroupTranscripts = "transcript"
	GroupState       = "状態の置き場"
	GroupBinary      = "バイナリ"
)

// Usage は 1 回の測定の結果。
type Usage struct {
	MeasuredAt time.Time     `json:"measuredAt"`
	Took       time.Duration `json:"took"`
	Total      int64         `json:"total"`
	Groups     []Group       `json:"groups"` // 大きい順
	Warnings   []string      `json:"warnings,omitempty"`
}

// Group は置き場 1 つ (worktree / transcript / 状態の置き場 / バイナリ)。
type Group struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Count int    `json:"count"` // 中身の数 (worktree の数・transcript の置き場の数・状態の置き場の中の項目の数)
	Items []Item `json:"items,omitempty"`
}

// Item は置き場の中身 1 つ (大きい順)。
type Item struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Files int    `json:"files"`
	Card  string `json:"card,omitempty"` // worktree・transcript を使ったカード (役なら PM / INT)
	Done  bool   `json:"done,omitempty"` // そのカードが完了している (片付けの判断の材料。ここでは消さない)
}

// Measure は in の対象を測る。読めなかった物は Warnings に出し、測れた分だけ返す (0 と区別する)。
func Measure(in Input) Usage {
	now := in.Now
	if now == nil {
		now = time.Now
	}
	start := now()
	var warns []string
	warn := func(err error) { warns = append(warns, err.Error()) }

	groups := []Group{
		worktrees(in, warn),
		transcripts(in, warn),
		stateDir(in.StateDir, warn),
	}
	if in.Binary != "" {
		if b, n, err := size(in.Binary); err == nil {
			groups = append(groups, Group{Name: GroupBinary, Bytes: b, Count: n, Items: []Item{{Name: filepath.Base(in.Binary), Path: in.Binary, Bytes: b, Files: n}}})
		} else {
			warn(err)
		}
	}
	u := Usage{Warnings: warns}
	for _, g := range groups {
		sortItems(g.Items)
		u.Total += g.Bytes
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Bytes > groups[j].Bytes })
	u.Groups = groups
	end := now()
	u.MeasuredAt, u.Took = end, end.Sub(start)
	return u
}

// worktrees は記録の cwd にある pc-* の worktree と、その親 (<repo>/.claude/worktrees) にある pc-* (記録から落ちた物) を測る。
func worktrees(in Input, warn func(error)) Group {
	card := map[string]string{} // worktree のパス → カード
	parents := map[string]bool{}
	for _, s := range in.Sessions {
		i := strings.Index(s.Cwd, WorktreeMarker)
		if i < 0 {
			continue
		}
		wt := filepath.Clean(s.Cwd)
		if rest := wt[i+len(WorktreeMarker):]; strings.Contains(rest, "/") { // worktree の中の下のディレクトリで動いた session
			wt = wt[:i+len(WorktreeMarker)+strings.Index(rest, "/")]
		}
		if s.CardID != "" {
			card[wt] = s.CardID
		}
		parents[wt[:i+len(WorktreeMarker)-len("/pc-")]] = true
	}
	paths := map[string]bool{}
	for wt := range card {
		paths[wt] = true
	}
	for p := range parents {
		ms, _ := filepath.Glob(filepath.Join(p, "pc-*"))
		for _, m := range ms {
			paths[m] = true
		}
	}
	g := Group{Name: GroupWorktrees}
	for p := range paths {
		b, n, err := size(p)
		if errors.Is(err, fs.ErrNotExist) { // 記録にあるが消えた worktree (人が消した等) は数えない
			continue
		}
		if err != nil {
			warn(err)
		}
		it := Item{Name: filepath.Base(p), Path: p, Bytes: b, Files: n, Card: card[p]}
		it.Done = it.Card != "" && in.Done[it.Card]
		g.Items = append(g.Items, it)
		g.Bytes += b
	}
	g.Count = len(g.Items)
	return g
}

// transcripts は記録の session id の transcript がある置き場 (~/.claude/projects/<cwd から組んだ名前>) を丸ごと測る。
// 置き場の名前の組み方は真似ず、session id で探す (live.FindTranscript と同じ考え)。
func transcripts(in Input, warn func(error)) Group {
	g := Group{Name: GroupTranscripts}
	if in.Projects == "" {
		return g
	}
	card := map[string]string{}
	for _, s := range in.Sessions {
		if s.SessionID == "" {
			continue
		}
		ms, _ := filepath.Glob(filepath.Join(in.Projects, "*", s.SessionID+".jsonl"))
		for _, m := range ms {
			d := filepath.Dir(m)
			if _, ok := card[d]; !ok || s.CardID != "" {
				card[d] = s.CardID
			}
		}
	}
	for d, c := range card {
		b, n, err := size(d)
		if err != nil {
			warn(err)
		}
		g.Items = append(g.Items, Item{Name: filepath.Base(d), Path: d, Bytes: b, Files: n, Card: c, Done: c != "" && in.Done[c]})
		g.Bytes += b
	}
	g.Count = len(g.Items)
	return g
}

// stateDir は状態の置き場を、直下の項目 (live/ の下はさらにその直下) ごとに測る。
// 内訳は「テストの係のログ (runs/)」「cards.json」のように、置き場の中の名前で出す。
func stateDir(dir string, warn func(error)) Group {
	g := Group{Name: GroupState}
	if dir == "" {
		return g
	}
	var walk func(d, prefix string)
	walk = func(d, prefix string) {
		es, err := os.ReadDir(d)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				warn(err)
			}
			return
		}
		for _, e := range es {
			p := filepath.Join(d, e.Name())
			if e.IsDir() && e.Name() == "live" && prefix == "" {
				walk(p, "live/")
				continue
			}
			b, n, err := size(p)
			if err != nil {
				warn(err)
			}
			name := prefix + e.Name()
			if e.IsDir() {
				name += "/"
			}
			g.Items = append(g.Items, Item{Name: name, Path: p, Bytes: b, Files: n})
			g.Bytes += b
		}
	}
	walk(dir, "")
	g.Count = len(g.Items)
	return g
}

// size は p の下のファイルが使っているディスクの量 (du と同じくブロックで数える) とファイルの数。symlink は辿らない。
// 途中で読めない所があっても、読めた分は返す (エラーは最初の 1 つ)。
func size(p string) (int64, int, error) {
	var total int64
	files := 0
	var first error
	err := filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == p {
				return err
			}
			if first == nil {
				first = err
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil // 歩いている間に消えた (git が書き換えた等)
		}
		total += blocks(info)
		if !d.IsDir() {
			files++
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return total, files, first
}

// blocks はファイルがディスクで使う量 (du と同じ。取れなければ見かけの大きさ)。
func blocks(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return info.Size()
}

// Human は大きさを du -h のように短く出す (1.5G / 408M / 236K)。
func Human(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	v := float64(b) / unit
	for _, s := range []string{"K", "M", "G"} {
		if v < unit {
			if v < 10 {
				return fmt.Sprintf("%.1f%s", v, s)
			}
			return fmt.Sprintf("%.0f%s", v, s)
		}
		v /= unit
	}
	return fmt.Sprintf("%.1fT", v)
}

func sortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Bytes != items[j].Bytes {
			return items[i].Bytes > items[j].Bytes
		}
		return items[i].Name < items[j].Name
	})
}
