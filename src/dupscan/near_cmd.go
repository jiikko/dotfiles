package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"dupscan/internal/exclude"
	"dupscan/internal/group"
	"dupscan/internal/langs"
	"dupscan/internal/norm"
	"dupscan/internal/repo"
	"dupscan/internal/report"
)

// runPoint は 1 つの単位を指す subcommand (near / explain)。最初の引数が `<file>:<関数名>` か `<file>:<行>`、残りが起点 (既定はそのファイルの repo 全体)。
func runPoint(name string, args []string, stdout, stderr io.Writer) int {
	fs, c := newFlags("dupscan "+name, stderr)
	var similar *bool
	var threshold *float64
	if name == "near" {
		similar = fs.Bool("similar", false, "L1部分木の類似候補を試作表示 (同一動作・再利用は未証明)")
		threshold = fs.Float64("similar-threshold", 0.4, "類似候補のweighted Dice下限 (0より大きく1以下)")
	}
	rest, rc, ok := parseFlags(fs, args)
	if !ok {
		return rc
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintf(stderr, "dupscan %s: <file>:<関数名> か <file>:<行> で単位を指す\n", name)
		return 2
	}
	opt, err := c.options()
	if err != nil {
		fmt.Fprintf(stderr, "dupscan %s: %v\n", name, err)
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "dupscan %s: %v\n", name, err)
		return 2
	}
	file, sel, ok := cutSelector(rest[0])
	if !ok {
		return fail(fmt.Errorf("%q は <file>:<関数名> か <file>:<行> の形ではない", rest[0]))
	}
	abs, err := absPath(file)
	if err != nil {
		return fail(err)
	}
	sc, err := repo.Resolve(append([]string{filepath.Dir(abs)}, rest[1:]...))
	if err != nil {
		return fail(err)
	}
	if len(rest) == 1 {
		sc.Prefixes = []string{""} // 起点を書かなければ repo 全体と比べる
	} else {
		sc.Prefixes = sc.Prefixes[1:]
	}
	rel, err := repoRel(sc.Root, abs)
	if err != nil {
		return fail(err)
	}
	src := repo.Worktree{Scope: sc, Exts: langs.Extensions()}
	cfg, err := loadConfig(src, *c.noExclude)
	if err != nil {
		return fail(fmt.Errorf("判定不能: %w", err))
	}
	got, err := scanSource(src, true, analysisPlan{Normalize: true})
	if err != nil {
		return fail(err)
	}
	entries, err := got.cloneEntries()
	if err != nil {
		return fail(err)
	}
	target, err := pick(entries, rel, sel)
	if err != nil {
		return fail(err)
	}
	if similar != nil && *similar {
		if *threshold <= 0 || *threshold > 1 || math.IsNaN(*threshold) || math.IsInf(*threshold, 0) || *c.limit < 1 || *c.limit > 50 {
			return fail(errors.New("similar-threshold は (0,1]、similarのlimitは1〜50"))
		}
		if target.Unit.Unreadable {
			return fail(errors.New("対象は読めない構文を含む"))
		}
		return writeSimilar(stdout, stderr, target, entries, got, *threshold, *c.limit, *c.format)
	}
	var out any
	var text func(io.Writer) error
	switch name {
	case "near":
		cfg, err = withComments(cfg, entries, *c.noExclude)
		if err != nil {
			return fail(fmt.Errorf("判定不能: %w", err))
		}
		s := report.BuildNear(target, group.Near(target, entries, opt), opt, exclude.New(cfg, entries, nil))
		out, text = s, func(w io.Writer) error { return report.NearText(w, s, *c.limit) }
	default:
		target.Result = norm.NormalizeWithForms(target.Unit) // 形の文字列は走査では作らないので、指した単位だけ作り直す
		s := report.BuildExplain(target, opt)
		out, text = s, func(w io.Writer) error { return report.ExplainText(w, s) }
	}
	switch *c.format {
	case "json":
		err = report.WriteJSON(stdout, out)
	case "text":
		err = text(stdout)
	default:
		return fail(fmt.Errorf("知らない形式 %q (text / json)", *c.format))
	}
	if err != nil {
		return fail(err)
	}
	return 0
}

// absPath は path の絶対パス (symlink を解いた実体。git の root と比べるため)。
func absPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return abs, nil
}

// repoRel は絶対パス abs の、repo の root からの相対パス (単位の File と同じ形)。
func repoRel(root, abs string) (string, error) {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// cutSelector は `<file>:<関数名|行>` を分ける (最後の `:` で切る)。
func cutSelector(s string) (file, sel string, ok bool) {
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// pick は file の単位から sel (関数名か行) で 1 つを選ぶ。行なら、その行を含む単位のうち最も後の行で始まるもの
// (同じ行で始まるものが複数なら左のもの。`{ a in f { … } }` で外側のクロージャを指す)。
// 名前で 2 つ以上に当たれば (多重定義・入れ子の型の同名) 候補を並べて止める (黙って片方を選ばない)。
func pick(entries []*group.Entry, file, sel string) (*group.Entry, error) {
	var inFile []*group.Entry
	for _, e := range entries {
		if e.Unit.File == file {
			inFile = append(inFile, e)
		}
	}
	if len(inFile) == 0 {
		return nil, fmt.Errorf("%s に単位が無い (起点の外か、対応する言語のファイルでない)", file)
	}
	var hits []*group.Entry
	if line, err := strconv.Atoi(sel); err == nil {
		var best *group.Entry
		for _, e := range inFile {
			u := e.Unit
			if u.Line <= line && line <= u.EndLine && (best == nil || u.Line > best.Unit.Line || u.Line == best.Unit.Line && u.Col < best.Unit.Col) {
				best = e
			}
		}
		if best == nil {
			return nil, fmt.Errorf("%s:%d を含む単位が無い", file, line)
		}
		return best, nil
	}
	for _, e := range inFile {
		if e.Unit.Name == sel {
			hits = append(hits, e)
		}
	}
	switch len(hits) {
	case 0:
		return nil, fmt.Errorf("%s に %s という単位が無い", file, sel)
	case 1:
		return hits[0], nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s の %s は %d つある。行で指す:", file, sel, len(hits))
	for _, e := range hits {
		fmt.Fprintf(&b, "\n  %s:%d %s", file, e.Unit.Line, e.Unit.Signature)
	}
	return nil, fmt.Errorf("%s", b.String())
}
