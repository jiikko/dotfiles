package tmpclean

import (
	"context"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// AreaCount は 1 つの領域の集計 (読み取りだけ)。
type AreaCount struct {
	Root       string `json:"root"`
	Total      int    `json:"total"`      // 根の配下の全部 (ファイルも dir も。根自身は数えない)
	TopLevel   int    `json:"top_level"`  // 根の直下
	EmptyDirs  int    `json:"empty_dirs"` // 根の直下の空の dir
	Bytes      int64  `json:"bytes"`      // 見えたファイルの大きさの合計
	Unreadable int    `json:"unreadable"` // 読めなかった項目 (macOS が保護している置き場など)
	// Projects は scratchpad の根の直下の dir (project) ごとの配下の件数、Prefixes は $TMPDIR の直下の名前を
	// 末尾の乱数を落とした接頭辞でまとめた件数。どちらも多い順の上位 15
	Projects []Part `json:"projects,omitempty"`
	Prefixes []Part `json:"prefixes,omitempty"`
}

// Part は内訳の 1 行。
type Part struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Counts は一時領域の集計。glogx の doctor の「一時領域」タブも使う。
type Counts struct {
	Session      AreaCount  `json:"session"`           // scratchpad の根が無ければ 0 件 (Claude Code を使っていない機械)
	Tmpdir       *AreaCount `json:"tmpdir,omitempty"`  // nil = TMPDIR が無い・形が違うので数えない
	OtherPrivate int        `json:"other_private_tmp"` // /private/tmp の直下で claude-<uid> 以外 (消さない)
}

// Count は本物の一時領域を数える (読み取りだけ。外部コマンドは起こさない)。ctx が終わったら走査を打ち切って ctx の error を返す
// (glogx の doctor は閉じるときに数え終わるのを待つので、件数が多くても閉じられるようにする)。
func Count(ctx context.Context) (Counts, error) { return count(ctx, prodDeps()) }

// CountDir は任意の dir の配下を数える (読み取りだけ。内訳は数えない)。glogx の doctor が repo の tmp/ を数えるのに使う。
func CountDir(ctx context.Context, dir string) (AreaCount, error) {
	return countArea(ctx, dir, breakdownNone)
}

// randomSuffix は mktemp などが名前の末尾に付ける乱数の部分 (接頭辞でまとめるときに落とす)。
var randomSuffix = regexp.MustCompile(`[0-9A-Za-z]{6,}$`)

const topParts = 15

type breakdown int

const (
	breakdownNone breakdown = iota
	breakdownProject
	breakdownPrefix
)

// countArea は root の配下を 1 回の走査で数える。直下の空の dir も走査の中で数える (直下を読み直さない)。
func countArea(ctx context.Context, root string, by breakdown) (AreaCount, error) {
	c := AreaCount{Root: root}
	parts := map[string]int{}
	// hasChild は直下の dir ごとに「中に何かあったか」。読めなかった dir は空と数えない (true にする)
	hasChild := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, de fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if path == root {
			return err // 根を読めなければ数えない
		}
		first, rest, deeper := strings.Cut(strings.TrimPrefix(path, root+"/"), "/")
		if err != nil {
			c.Unreadable++
			if !deeper {
				hasChild[first] = true
			}
			if de != nil && de.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		c.Total++
		switch {
		case !deeper:
			c.TopLevel++
			if de.IsDir() {
				if _, seen := hasChild[first]; !seen {
					hasChild[first] = false
				}
			}
			if by == breakdownPrefix {
				parts[randomSuffix.ReplaceAllString(first, "")]++
			}
		case rest != "":
			hasChild[first] = true
		}
		if by == breakdownProject {
			parts[first]++
		}
		if de.Type().IsRegular() {
			if fi, err := de.Info(); err == nil {
				c.Bytes += fi.Size()
			}
		}
		return nil
	})
	if err != nil {
		return c, err
	}
	for _, has := range hasChild {
		if !has {
			c.EmptyDirs++
		}
	}
	switch by {
	case breakdownProject:
		c.Projects = topOf(parts)
	case breakdownPrefix:
		c.Prefixes = topOf(parts)
	case breakdownNone:
	}
	return c, nil
}

func topOf(m map[string]int) []Part {
	out := make([]Part, 0, len(m))
	for k, v := range m {
		out = append(out, Part{Name: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > topParts {
		out = out[:topParts]
	}
	return out
}

func count(ctx context.Context, d deps) (Counts, error) {
	r, err := resolveRoots(d, false, true)
	if err != nil {
		return Counts{}, err
	}
	rep := Counts{Session: AreaCount{Root: d.sessionRoot}}
	if r.session != "" {
		if rep.Session, err = countArea(ctx, r.session, breakdownProject); err != nil {
			return Counts{}, err
		}
	}
	if r.tmpdir != "" {
		t, err := countArea(ctx, r.tmpdir, breakdownPrefix)
		if err != nil {
			return Counts{}, err
		}
		rep.Tmpdir = &t
	}
	parent := filepath.Dir(d.sessionRoot)
	for _, name := range readDirSorted(parent) {
		if filepath.Join(parent, name) != d.sessionRoot && !strings.HasPrefix(name, ".") {
			rep.OtherPrivate++
		}
	}
	return rep, nil
}
