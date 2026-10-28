package main

import (
	"fmt"
	"io"
	"time"

	"dupscan/internal/exclude"
	"dupscan/internal/gate"
	"dupscan/internal/group"
	"dupscan/internal/langs"
	"dupscan/internal/norm"
	"dupscan/internal/repo"
	"dupscan/internal/report"
)

func runGate(args []string, stdout, stderr io.Writer) int {
	fs, c := newFlags("dupscan gate", stderr)
	baseRef := fs.String("base", "", "比べる base (その commit と head の merge-base を使う)。既定は upstream、無ければ origin/HEAD")
	headRef := fs.String("head", "", "作業ツリーの代わりに比べる commit (過去の commit の流し直し用。checkout しない)")
	failLevels := fs.String("fail-levels", "L1,L2", "違反を止める (rc=1) 段。それ以外の段は報告だけ")
	record := fs.String("record", "", "1 回ごとの結果 (違反・除外・許可) を、時刻を添えた 1 行の JSON でこのファイルに追記する")
	strictAnalysis := fs.Bool("strict-analysis", false, "base/headのどちらかに読めない構文があれば判定不能 (rc=2) にする")
	failNewEx := fs.Bool("fail-new-exclusions", false, "base から新しく足された除外か、パスの除外で通した変わった単位があれば rc=1 にする (CI で人の目に届けるため)")
	dirs, rc, ok := parseFlags(fs, args)
	if !ok {
		return rc
	}
	opt, err := c.options()
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate:", err)
		return 2
	}
	fail, err := parseLevels(*failLevels)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate:", err)
		return 2
	}
	gopt := gate.Options{Options: opt, Fail: map[norm.Level]bool{}}
	for _, l := range fail {
		gopt.Fail[l] = true
	}
	sc, err := repo.Resolve(dirs)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate:", err)
		return 2
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
			fmt.Fprintln(stderr, "dupscan gate:", err)
			return 2
		}
		head, headCommit = &repo.Commit{Scope: sc, Exts: langs.Extensions(), Hash: h}, h
	}
	baseHash, err := resolveBase(sc.Root, *baseRef, headCommit)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate: 判定不能:", err)
		return 2
	}
	headCfg, err := loadConfig(head, *c.noExclude)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate: 判定不能:", err)
		return 2
	}
	baseSrc := &repo.Commit{Scope: sc, Exts: langs.Extensions(), Hash: baseHash}
	// base の設定は読めなければ空とみなす (壊れた設定を直す commit を通すため。空とみなすと除外は全部新しく見え、安全側)。
	baseCfg, baseCfgErr := loadConfig(baseSrc, false)
	b, err := scanSource(baseSrc, false, analysisPlan{Normalize: true})
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate: 判定不能:", err)
		return 2
	}
	h, err := scanSource(head, true, analysisPlan{Normalize: true})
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate: 判定不能:", err)
		return 2
	}
	baseEntries, err := b.cloneEntries()
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate:", err)
		return 2
	}
	headEntries, err := h.cloneEntries()
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate:", err)
		return 2
	}
	headCfg, err = withComments(headCfg, headEntries, *c.noExclude)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate: 判定不能:", err)
		return 2
	}
	// base のコメントの誤りは数えない (base の許可が欠けると head の許可は全部新しく見え、安全側)。
	baseRules, _ := exclude.CommentRules(baseEntries)
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
	s := report.BuildGate(sc.Root, baseHash, head.Name(), res, ex)
	s.BaseFilesWithErrors = b.errFiles
	s.HeadFilesWithErrors = h.errFiles
	s.AnalysisIncomplete = len(b.errFiles) > 0 || len(h.errFiles) > 0
	s.StrictAnalysis = *strictAnalysis
	s.NewExclusions = append(s.NewExclusions, exclude.NewElements(baseCfg, headCfg)...)
	if baseCfgErr != nil {
		s.BaseConfigError = baseCfgErr.Error()
	}
	if *headRef == "" {
		u, err := repo.Untracked(sc, langs.Extensions())
		if err != nil {
			fmt.Fprintln(stderr, "dupscan gate: 判定不能:", err)
			return 2
		}
		s.Untracked = append(s.Untracked, u...)
	}
	// base が head の commit と同じなら、見たのは未コミットの変更だけ (CI で -base を付け忘れた疑い)。
	if hc, err := repo.CommitHash(sc.Root, headCommit); err == nil && hc == baseHash {
		s.BaseIsHead = true
	}
	s.BaseGiven = *baseRef != ""
	s.Partial, s.Prefixes = !sc.Whole(), sc.Prefixes
	if *record != "" {
		if err := report.AppendRecord(*record, s, time.Now()); err != nil {
			fmt.Fprintln(stderr, "dupscan gate: --record:", err)
			return 2
		}
	}
	switch *c.format {
	case "json":
		err = report.GateJSON(stdout, s)
	case "xcode":
		err = report.GateXcode(stdout, s)
	case "text":
		err = report.GateText(stdout, s, report.TextOptions{Limit: *c.limit, Verbose: *c.verbose})
	default:
		fmt.Fprintf(stderr, "dupscan gate: 知らない形式 %q\n", *c.format)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate:", err)
		return 2
	}
	if s.StrictAnalysis && s.AnalysisIncomplete {
		return 2
	}
	if res.Failed() || *failNewEx && (len(s.NewExclusions) > 0 || s.PathPassed > 0) {
		return 1
	}
	return 0
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
