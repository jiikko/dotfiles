package main

import (
	"bytes"
	"fmt"
	"io"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"dupscan/internal/exclude"
	"dupscan/internal/gate"
	"dupscan/internal/group"
	"dupscan/internal/langs"
	"dupscan/internal/norm"
	"dupscan/internal/repo"
	"dupscan/internal/report"
)

// runGate は gate を回し、-record があれば最終の終了結果まで決めてから 1 行を書く。記録を判定・出力の前に書くと、
// その後の rc (出力の失敗・strict の判定不能) が記録と食い違い、早期の判定不能は記録に届かない。
func runGate(args []string, stdout, stderr io.Writer) int {
	o := gateOnce(args, stdout, stderr)
	if o.record == "" {
		return o.rc
	}
	if err := report.AppendRecord(o.record, o.entry(), time.Now()); err != nil {
		// 記録が無い run を成功に読ませない。元の判定は stderr に残す
		fmt.Fprintf(stderr, "dupscan gate: --record: %v (判定は rc=%d。記録を書けないので rc=2)\n", err, o.rc)
		return 2
	}
	return o.rc
}

func gateOnce(args []string, stdout, stderr io.Writer) gateOutcome {
	fs, c := newFlags("dupscan gate", stderr)
	baseRef := fs.String("base", "", "比べる base (その commit と head の merge-base を使う)。既定は upstream、無ければ origin/HEAD")
	headRef := fs.String("head", "", "作業ツリーの代わりに比べる commit (過去の commit の流し直し用。checkout しない)")
	failLevels := fs.String("fail-levels", "L1,L2", "違反を止める (rc=1) 段。それ以外の段は報告だけ")
	record := fs.String("record", "", "1 回ごとの結果 (違反・除外・許可) を、時刻を添えた 1 行の JSON でこのファイルに追記する")
	strictAnalysis := fs.Bool("strict-analysis", false, "base/headのどちらかに読めない構文があれば判定不能 (rc=2) にする")
	failNewEx := fs.Bool("fail-new-exclusions", false, "base から新しく足された除外か、パスの除外で通した変わった単位があれば rc=1 にする (CI で人の目に届けるため)")
	dirs, rc, ok := parseFlags(fs, args)
	if !ok {
		return gateOutcome{rc: rc} // -record の先も読めていない
	}
	o := gateOutcome{record: *record, cond: report.GateConditions{Levels: *c.levels, FailLevels: *failLevels, MinNodes: *c.minNodes,
		MinClosureNodes: *c.minClosure, FailNewExclusions: *failNewEx, StrictAnalysis: *strictAnalysis, NoExclude: *c.noExclude,
		Base: *baseRef, Head: *headRef}}
	stop := func(phase, prefix string, err error) gateOutcome {
		fmt.Fprintln(stderr, prefix, err)
		o.rc, o.phase, o.err, o.reasons = 2, phase, err.Error(), []string{"error"}
		return o
	}
	if *record != "" {
		// 版は起動時に取る (書く時点で読むと、gate の間に go_autobuild が rename で差し替えた別の実行ファイルを読む)
		o.tool = report.ToolRevision()
		// 閉じたパイプへの出力で SIGPIPE で落ちると、出力の後に書く記録が残らない。無視して EPIPE を output_error として記録する
		signal.Ignore(syscall.SIGPIPE)
	}
	switch *c.format {
	case "text", "json", "xcode":
	default:
		return stop("args", "dupscan gate:", fmt.Errorf("知らない形式 %q", *c.format))
	}
	opt, err := c.options()
	if err != nil {
		return stop("args", "dupscan gate:", err)
	}
	fail, err := parseLevels(*failLevels)
	if err != nil {
		return stop("args", "dupscan gate:", err)
	}
	gopt := gate.Options{Options: opt, Fail: map[norm.Level]bool{}}
	for _, l := range fail {
		gopt.Fail[l] = true
	}
	sc, err := repo.Resolve(dirs)
	if err != nil {
		return stop("scope", "dupscan gate:", err)
	}
	if fs.NArg() == 0 {
		// 起点を書かなければ repo 全体を見る。cwd が target の dir だと、別の dir にある同じ形の相手が見えず、止めるべき写しを通す。
		// 一部だけ見たいときは dir を明示する (その場合は 1 行目に「一部だけ」と出る)。
		sc.Prefixes = []string{""}
	}

	var head repo.Source = repo.Worktree{Scope: sc, Exts: langs.Extensions()}
	headCommit := "HEAD"
	if *headRef != "" {
		h, err := repo.CommitHash(sc.Root, *headRef)
		if err != nil {
			return stop("head", "dupscan gate:", err)
		}
		head, headCommit = &repo.Commit{Scope: sc, Exts: langs.Extensions(), Hash: h}, h
	}
	if hc, err := repo.CommitHash(sc.Root, headCommit); err == nil {
		o.headCommit = hc // 判定の前に止まった run にも残す
	}
	baseHash, err := resolveBase(sc.Root, *baseRef, headCommit)
	if err != nil {
		return stop("base", "dupscan gate: 判定不能:", err)
	}
	headCfg, err := loadConfig(head, *c.noExclude)
	if err != nil {
		return stop("config", "dupscan gate: 判定不能:", err)
	}
	baseSrc := &repo.Commit{Scope: sc, Exts: langs.Extensions(), Hash: baseHash}
	// base の設定は読めなければ空とみなす (壊れた設定を直す commit を通すため。空とみなすと除外は全部新しく見え、安全側)。
	baseCfg, baseCfgErr := loadConfig(baseSrc, false)
	b, err := scanSource(baseSrc, false, analysisPlan{Normalize: true})
	if err != nil {
		return stop("scan", "dupscan gate: 判定不能:", err)
	}
	h, err := scanSource(head, true, analysisPlan{Normalize: true})
	if err != nil {
		return stop("scan", "dupscan gate: 判定不能:", err)
	}
	baseEntries, err := b.cloneEntries()
	if err != nil {
		return stop("scan", "dupscan gate:", err)
	}
	headEntries, err := h.cloneEntries()
	if err != nil {
		return stop("scan", "dupscan gate:", err)
	}
	// base と head で同じ判定を使う (印の効き方が base と head で食い違うと、変わっていない印が新しく見える)
	complete, err := scopeComplete(sc)
	if err != nil {
		return stop("scope", "dupscan gate:", err)
	}
	headCfg, err = withComments(headCfg, headEntries, *c.noExclude, complete)
	if err != nil {
		return stop("config", "dupscan gate: 判定不能:", err)
	}
	// base のコメントの誤りは数えない (base の許可が欠けると head の許可は全部新しく見え、安全側)。
	baseRules, _ := exclude.CommentRules(baseEntries, complete)
	baseCfg = exclude.WithComments(baseCfg, baseRules)
	ex := exclude.New(headCfg, headEntries, scopeFilter(sc))
	ex.LimitToBase(baseEntries, baseCfg)
	if c.narrowed() {
		ex.Narrowed()
	}
	ex.Scanned(gopt.Levels)
	gopt.Exclude = ex
	// 効いていない除外を数えるため、head 全体の組にも除外を掛ける (gate が見るのは変わった単位だけなので、それだけでは足りない)。
	for _, g := range ex.ApplyGroups(group.Build(headEntries, gopt.Options)) {
		for _, m := range g.Members {
			ex.Allowed(m)
		}
	}
	res := gate.Judge(baseEntries, headEntries, gopt)
	compareEntries := headEntries
	if *headRef != "" {
		compareEntries = nil
	}
	s := report.BuildGate(sc.Root, baseHash, head.Name(), res, ex, compareEntries)
	s.BaseFilesWithErrors = b.errFiles
	s.HeadFilesWithErrors = h.errFiles
	s.NewGeneratedFiles = append(s.NewGeneratedFiles, changedGenerated(b, h)...)
	s.AnalysisIncomplete = len(b.errFiles) > 0 || len(h.errFiles) > 0
	s.StrictAnalysis = *strictAnalysis
	s.NewExclusions = append(s.NewExclusions, exclude.NewElements(baseCfg, headCfg)...)
	if baseCfgErr != nil {
		s.BaseConfigError = baseCfgErr.Error()
	}
	if *headRef == "" {
		u, err := repo.Untracked(sc, langs.Extensions())
		if err != nil {
			return stop("untracked", "dupscan gate: 判定不能:", err)
		}
		s.Untracked = append(s.Untracked, u...)
	}
	// base が head の commit と同じなら、見たのは未コミットの変更だけ (CI で -base を付け忘れた疑い)。
	// head の commit は記録と同じ値で比べる (走査の間に commit が入っても、記録の head_commit と食い違わない)
	if o.headCommit != "" && o.headCommit == baseHash {
		s.BaseIsHead = true
	}
	s.BaseGiven = *baseRef != ""
	s.Partial, s.Prefixes = !sc.Whole(), sc.Prefixes
	o.summary = &s
	// text と xcode の出力は Fprintf の誤りを捨てるので、最初の書き込みの失敗を out で覚えて読む (閉じた stdout で rc=0 にしない)。
	out := &lineWriter{w: stdout}
	switch *c.format {
	case "json":
		err = report.GateJSON(out, s)
	case "xcode":
		err = report.GateXcode(out, s)
	case "text":
		err = report.GateText(out, s, report.TextOptions{Limit: *c.limit, Verbose: *c.verbose})
	default:
		// 先頭の形式の検査に足して出力を書き忘れた形。黙って text にしない
		return stop("args", "dupscan gate:", fmt.Errorf("出力の無い形式 %q", *c.format))
	}
	if err == nil {
		err = out.err
	}
	// 理由は併存するので全部残す (rc はそのうち最も重いもの)。
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate:", err)
		o.reasons = append(o.reasons, "output_error")
		o.err = err.Error()
	}
	if s.StrictAnalysis && s.AnalysisIncomplete {
		o.reasons = append(o.reasons, "strict_incomplete")
	}
	if res.Failed() {
		o.reasons = append(o.reasons, "duplicates")
	}
	if *failNewEx && len(s.NewExclusions) > 0 {
		o.reasons = append(o.reasons, "new_exclusions")
	}
	if *failNewEx && s.PathPassed > 0 {
		o.reasons = append(o.reasons, "path_excluded_passed")
	}
	switch {
	case slices.Contains(o.reasons, "output_error") || slices.Contains(o.reasons, "strict_incomplete"):
		o.rc = 2
	case len(o.reasons) > 0:
		o.rc = 1
	}
	return o
}

// gateOutcome は gate 1 回の最終の結果 (記録の材料)。summary は判定まで届いたときだけ。
type gateOutcome struct {
	rc         int
	record     string
	phase, err string
	reasons    []string
	summary    *report.GateSummary
	headCommit string
	cond       report.GateConditions
	tool       *report.Tool
}

func (o gateOutcome) entry() report.GateRecord {
	r := report.GateRecord{ExitCode: o.rc, Reasons: o.reasons, Phase: o.phase, Error: o.err, HeadCommit: o.headCommit,
		Conditions: o.cond, Tool: o.tool, GateSummary: o.summary}
	if r.Reasons == nil {
		r.Reasons = []string{}
	}
	return r
}

// resolveBase は base の commit を決める。渡された ref (CI の push の前の commit・PR の対象) でも既定 (upstream) でも、
// head との merge-base を使う (先端をそのまま使うと、分かれた後に相手で変わった分が head の変更に見える。force push で ref が祖先でなくても取れる)。
func resolveBase(root, baseRef, headCommit string) (string, error) {
	var ref string
	if baseRef != "" {
		h, err := repo.CommitHash(root, baseRef)
		if err != nil {
			return "", err
		}
		ref = h
	} else {
		h, err := repo.DefaultBase(root)
		if err != nil {
			return "", err
		}
		ref = h
	}
	return repo.MergeBase(root, headCommit, ref)
}

// changedGenerated は head の生成ファイルのうち、base で生成ファイルでなかったもの (base に無いファイルを含む) と、中身が base と違うもの。
// 中身を比べないと、見出しを付けた commit で 1 度だけ出た後は、同じファイルに足した写しがずっと黙って通る。
// 移しただけの生成ファイルも新しいパスとして出る (雑音だけで rc は変えない)。
func changedGenerated(base, head scanned) []string {
	var out []string
	for _, f := range head.generated {
		if !slices.Contains(base.generated, f) || !bytes.Equal(base.contents[f], head.contents[f]) {
			out = append(out, f)
		}
	}
	return out
}
