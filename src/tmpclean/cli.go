package tmpclean

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// 終了コード (tmpclean 独自。doctor の exitcode とは意味が違う)。
const (
	exitOK           = 0 // 走り切った。移せなかったものも無い (残したものは理由つきで出る)
	exitFailed       = 1 // 走り切ったが、移せなかったものがある (権限・別ボリューム)
	exitStopped      = 2 // 前提の検査で止めた。1 件も移していない (引数の誤り・判定不能・承認の不一致・古い plan)
	exitEnv          = 3 // 実行環境の失敗 (書き出し)
	exitStoppedMoved = 4 // 途中で止めた。一部は移した
)

const usage = `使い方:
  tmpclean count [-json]
      一時領域 (Claude Code の scratchpad の根・$TMPDIR) を数える。何も動かさない
  tmpclean prefs-shapes [-days 7] [-json]
      ~/Library/Preferences の直下の、名前に UUID を含む plist (com.apple. を除く) を、名前の形 (UUID を <UUID> に置き換えた名前) ごとに数える。
      形ごとに件数・N 日より古い件数・空 / 中身あり / 判定できない の件数を出す。何も動かさない
  tmpclean plan -kinds session,tmpdir,derived-data,prefs -out <dir> [-session-days 7] [-tmpdir-days 3]
                [-prefs-shape <形> ...] [-prefs-days 7] [-prefs-nonempty] [-json]
      移す候補を選び、<dir>/plan-<ID>.json に書く。何も動かさない。
      derived-data は Xcode の DerivedData のうち、持ち主 (info.plist の WorkspacePath) が消えたもの (古さでは選ばない)。
      prefs は prefs-shapes の形を -prefs-shape で 1 つ以上選び、その形の N 日より古い空の plist を選ぶ (-prefs-nonempty で中身のあるものも)
  tmpclean apply -plan <dir>/plan-<ID>.json -approve <ID> [-json]
      plan に載っていて今も候補であるものだけを、1 件ずつ判定し直してからゴミ箱 (~/.Trash/tmpclean-…) へ移す。
      移した記録は plan と同じ dir の moved-<ID>.jsonl
  tmpclean sweep [-days 3] [-json]
      $TMPDIR の直下の空の TemporaryDirectory.* (Swift のツールチェーンが残す) を承認なしで消す (ゴミ箱を経由しない)。
      消すのは空か空の .keep-directory だけのもので、N 日より古く、開かれておらず、動いている Swift のツールの起動より前に作られたもの

終了コード: 0 = 走り切った / 1 = 移せなかったものがある / 2 = 前提の検査で止めた (1 件も移していない) /
            3 = 実行環境の失敗 / 4 = 途中で止めた (一部は移した)
`

// Run は CLI の本体。cmd/tmpclean の main はこれを呼んで終了コードで終わるだけ。
func Run(args []string, stdout, stderr io.Writer) int {
	return run(context.Background(), prodDeps(), args, stdout, stderr)
}

func run(ctx context.Context, d deps, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitStopped
	}
	switch args[0] {
	case "count":
		return runCount(d, args[1:], stdout, stderr)
	case "plan":
		return runPlan(ctx, d, args[1:], stdout, stderr)
	case "prefs-shapes":
		return runPrefsShapes(d, args[1:], stdout, stderr)
	case "apply":
		return runApply(ctx, d, args[1:], stdout, stderr)
	case "sweep":
		return runSweep(ctx, d, args[1:], stdout, stderr)
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "知らないサブコマンド: %s\n%s", args[0], usage)
		return exitStopped
	}
}

func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	return fs
}

// stopped は止めた理由を出し、終了コードを返す。stopError でないもの (想定外の失敗) は実行環境の失敗として扱う。
func stopped(stderr io.Writer, err error, movedAny bool) int {
	fmt.Fprintf(stderr, "止めた: %v\n", err)
	switch {
	case movedAny:
		return exitStoppedMoved
	case isStop(err):
		return exitStopped
	default:
		return exitEnv
	}
}

func writeJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(stderr, "JSON を書けない: %v\n", err)
		return exitEnv
	}
	return exitOK
}

func runCount(d deps, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("count", stderr)
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return exitStopped
	}
	rep, err := count(context.Background(), d)
	if err != nil {
		return stopped(stderr, err, false)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, rep)
	}
	printArea := func(label string, c AreaCount) {
		fmt.Fprintf(stdout, "%s: %s\n  total: %d / top_level: %d / empty_dirs: %d / bytes: %d / unreadable: %d\n",
			label, c.Root, c.Total, c.TopLevel, c.EmptyDirs, c.Bytes, c.Unreadable)
		for _, p := range append(c.Projects, c.Prefixes...) {
			fmt.Fprintf(stdout, "  %7d %s\n", p.Count, p.Name)
		}
	}
	printArea("session", rep.Session)
	if rep.Tmpdir != nil {
		printArea("tmpdir", *rep.Tmpdir)
	} else {
		fmt.Fprintln(stdout, "tmpdir: (TMPDIR が無い・形が違うので数えない)")
	}
	fmt.Fprintf(stdout, "other_private_tmp (消さない): %d\n", rep.OtherPrivate)
	return exitOK
}

type planOutput struct {
	ID       string         `json:"id"`
	Path     string         `json:"path"`
	Counts   map[string]int `json:"counts"`
	Kept     map[string]int `json:"kept"`
	Entries  []entry        `json:"entries"`
	Settings selection      `json:"selection"`
}

func runPlan(ctx context.Context, d deps, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("plan", stderr)
	kinds := fs.String("kinds", "", "session,tmpdir,derived-data,prefs のどれか (カンマ区切り)")
	out := fs.String("out", "", "plan を書く dir")
	sessionDays := fs.Int("session-days", 7, "")
	tmpdirDays := fs.Int("tmpdir-days", 3, "")
	var prefsShapesArg []string
	fs.Func("prefs-shape", "prefs の形 (繰り返せる)", func(v string) error {
		prefsShapesArg = append(prefsShapesArg, v)
		return nil
	})
	prefsDays := fs.Int("prefs-days", 7, "")
	prefsNonEmpty := fs.Bool("prefs-nonempty", false, "")
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return exitStopped
	}
	if *out == "" {
		fmt.Fprintln(stderr, "-out が要る")
		return exitStopped
	}
	sel := selection{Kinds: splitKinds(*kinds), SessionDays: *sessionDays, TmpdirDays: *tmpdirDays}
	prefsDaysSet := false
	fs.Visit(func(f *flag.Flag) { prefsDaysSet = prefsDaysSet || f.Name == "prefs-days" })
	if prefsDaysSet && !sel.has(kindPrefs) {
		fmt.Fprintln(stderr, "-prefs-days は -kinds に prefs があるときだけ使える")
		return exitStopped
	}
	if sel.has(kindPrefs) || len(prefsShapesArg) > 0 || *prefsNonEmpty {
		// prefs を選ばない plan の selection (と ID) には、prefs の条件を何も書かない
		sel.PrefsShapes, sel.PrefsDays, sel.PrefsNonEmpty = prefsShapesArg, *prefsDays, *prefsNonEmpty
	}
	if err := sel.validate(); err != nil {
		return stopped(stderr, err, false)
	}
	w, err := gatherWorld(ctx, d, sel)
	if err != nil {
		return stopped(stderr, err, false)
	}
	entries, ks := candidates(w, sel)
	p, err := newPlan(w.now, sel, entries)
	if err != nil {
		return stopped(stderr, err, false)
	}
	path, err := writePlan(*out, p)
	if err != nil {
		return stopped(stderr, err, false)
	}
	o := planOutput{ID: p.ID, Path: path, Counts: map[string]int{}, Kept: map[string]int{}, Entries: p.Entries, Settings: sel}
	for _, e := range p.Entries {
		o.Counts[e.Kind]++
	}
	for _, k := range ks {
		o.Kept[k.reason]++
	}
	if *asJSON {
		return writeJSON(stdout, stderr, o)
	}
	fmt.Fprintf(stdout, "plan: %s\nid: %s\n", path, p.ID)
	for _, k := range sel.Kinds {
		fmt.Fprintf(stdout, "candidates.%s: %d\n", k, o.Counts[k])
	}
	for _, line := range sortedCounts(o.Kept) {
		fmt.Fprintf(stdout, "kept (%s): %d\n", line.key, line.n)
	}
	for _, e := range p.Entries {
		fmt.Fprintf(stdout, "candidate (%s): %s\n", e.Kind, e.Path)
	}
	return exitOK
}

type applyOutput struct {
	ID      string         `json:"id"`
	Moved   []moved        `json:"moved"`
	Kept    map[string]int `json:"kept"`
	Failed  []failure      `json:"failed"`
	Stopped string         `json:"stopped,omitempty"`
	Exit    int            `json:"exit"`
}

type failure struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func runApply(ctx context.Context, d deps, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("apply", stderr)
	planPath := fs.String("plan", "", "plan ファイル")
	approve := fs.String("approve", "", "承認した plan の ID")
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return exitStopped
	}
	if *planPath == "" || *approve == "" {
		fmt.Fprintln(stderr, "-plan と -approve が要る")
		return exitStopped
	}
	p, err := readPlan(*planPath, *approve, d.now())
	if err != nil {
		return stopped(stderr, err, false)
	}
	res := apply(ctx, d, p, filepath.Dir(*planPath))
	code := exitOK
	switch {
	case res.stopped != nil && len(res.moved) > 0:
		code = exitStoppedMoved
	case res.stopped != nil && isStop(res.stopped):
		code = exitStopped
	case res.stopped != nil:
		code = exitEnv
	case len(res.failed) > 0:
		code = exitFailed
	}
	o := applyOutput{ID: p.ID, Moved: res.moved, Kept: map[string]int{}, Failed: []failure{}, Exit: code}
	if o.Moved == nil {
		o.Moved = []moved{}
	}
	for _, k := range res.kept {
		o.Kept[k.reason]++
	}
	for _, f := range res.failed {
		o.Failed = append(o.Failed, failure{Path: f.path, Reason: f.reason})
	}
	if res.stopped != nil {
		o.Stopped = res.stopped.Error()
	}
	if *asJSON {
		if rc := writeJSON(stdout, stderr, o); rc != exitOK {
			return rc
		}
		return code
	}
	for _, m := range res.moved {
		fmt.Fprintf(stdout, "moved: %s -> %s\n", m.From, m.To)
	}
	for _, k := range res.kept {
		fmt.Fprintf(stdout, "kept (%s): %s\n", k.reason, k.path)
	}
	for _, f := range res.failed {
		fmt.Fprintf(stdout, "failed (%s): %s\n", f.reason, f.path)
	}
	fmt.Fprintf(stdout, "moved: %d / kept: %d / failed: %d\n", len(res.moved), len(res.kept), len(res.failed))
	if res.stopped != nil {
		fmt.Fprintf(stderr, "止めた: %v\n", res.stopped)
	}
	return code
}

func runPrefsShapes(d deps, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("prefs-shapes", stderr)
	days := fs.Int("days", 7, "")
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return exitStopped
	}
	rep, err := prefsShapes(d, *days)
	if err != nil {
		return stopped(stderr, err, false)
	}
	if *asJSON {
		return writeJSON(stdout, stderr, rep)
	}
	if rep.Root == "" {
		fmt.Fprintln(stdout, "prefs: (Preferences の dir が無い)")
		return exitOK
	}
	fmt.Fprintf(stdout, "prefs: %s\n  total: %d / shapes: %d / old は %d 日より古い件数\n", rep.Root, rep.Total, len(rep.Shapes), rep.Days)
	fmt.Fprintf(stdout, "  %7s %7s %7s %9s %7s %10s  %-10s  %s\n", "count", "old", "empty", "non_empty", "unknown", "bytes", "newest", "shape")
	for _, s := range rep.Shapes {
		fmt.Fprintf(stdout, "  %7d %7d %7d %9d %7d %10d  %-10s  %s\n", s.Count, s.Old, s.Empty, s.NonEmpty, s.Unknown, s.Bytes,
			s.Newest.Format("2006-01-02"), s.Shape)
	}
	return exitOK
}

func splitKinds(s string) []string {
	var out []string
	for _, k := range strings.Split(s, ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

type keyCount struct {
	key string
	n   int
}

func sortedCounts(m map[string]int) []keyCount {
	out := make([]keyCount, 0, len(m))
	for k, n := range m {
		out = append(out, keyCount{k, n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

type sweepOutput struct {
	Removed []string       `json:"removed"`
	Kept    map[string]int `json:"kept"`
	Failed  []failure      `json:"failed"`
	Limit   string         `json:"limit,omitempty"` // 消してよい範囲を決めた一番古い Swift のツール (pid と起動時刻と command)
	Stopped string         `json:"stopped,omitempty"`
	Exit    int            `json:"exit"`
}

func runSweep(ctx context.Context, d deps, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("sweep", stderr)
	days := fs.Int("days", 3, "")
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return exitStopped
	}
	res, err := sweep(ctx, d, *days)
	code := exitOK
	switch {
	case err != nil && len(res.removed) > 0:
		code = exitStoppedMoved
	case err != nil && isStop(err):
		code = exitStopped
	case err != nil:
		code = exitEnv
	case len(res.failed) > 0:
		code = exitFailed
	}
	o := sweepOutput{Removed: res.removed, Kept: map[string]int{}, Failed: []failure{}, Exit: code}
	if o.Removed == nil {
		o.Removed = []string{}
	}
	for _, k := range res.kept {
		o.Kept[k.reason]++
	}
	for _, f := range res.failed {
		o.Failed = append(o.Failed, failure{Path: f.path, Reason: f.reason})
	}
	if res.limit != nil {
		o.Limit = fmt.Sprintf("pid %d (%s から): %s", res.limit.pid, res.limit.started.Format("2006-01-02 15:04:05"), res.limit.command)
	}
	if err != nil {
		o.Stopped = err.Error()
	}
	if *asJSON {
		if rc := writeJSON(stdout, stderr, o); rc != exitOK {
			return rc
		}
		return code
	}
	for _, p := range res.removed {
		fmt.Fprintf(stdout, "removed: %s\n", p)
	}
	for _, f := range res.failed {
		fmt.Fprintf(stdout, "failed (%s): %s\n", f.reason, f.path)
	}
	for _, line := range sortedCounts(o.Kept) {
		fmt.Fprintf(stdout, "kept (%s): %d\n", line.key, line.n)
	}
	if o.Limit != "" {
		fmt.Fprintf(stdout, "limit: %s\n", o.Limit)
	}
	fmt.Fprintf(stdout, "removed: %d / failed: %d\n", len(res.removed), len(res.failed))
	if err != nil {
		fmt.Fprintf(stderr, "止めた: %v\n", err)
	}
	return code
}
