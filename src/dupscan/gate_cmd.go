package main

import (
	"fmt"
	"io"

	"dupscan/internal/gate"
	"dupscan/internal/norm"
	"dupscan/internal/repo"
	"dupscan/internal/report"
	"dupscan/internal/swift"
)

func runGate(args []string, stdout, stderr io.Writer) int {
	fs, c := newFlags("dupscan gate", stderr)
	baseRef := fs.String("base", "", "比べる base (その commit と head の merge-base を使う)。既定は upstream、無ければ origin/HEAD")
	headRef := fs.String("head", "", "作業ツリーの代わりに比べる commit (過去の commit の流し直し用。checkout しない)")
	failLevels := fs.String("fail-levels", "L1,L2", "違反を止める (rc=1) 段。それ以外の段は報告だけ")
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

	var head repo.Source = repo.Worktree{Scope: sc, Exts: swift.Extensions}
	headCommit := "HEAD"
	if *headRef != "" {
		h, err := repo.CommitHash(sc.Root, *headRef)
		if err != nil {
			fmt.Fprintln(stderr, "dupscan gate:", err)
			return 2
		}
		head, headCommit = &repo.Commit{Scope: sc, Exts: swift.Extensions, Hash: h}, h
	}
	baseHash, err := resolveBase(sc.Root, *baseRef, headCommit)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate: 判定不能:", err)
		return 2
	}
	b, err := scanSource(&repo.Commit{Scope: sc, Exts: swift.Extensions, Hash: baseHash}, false)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate: 判定不能:", err)
		return 2
	}
	h, err := scanSource(head, true)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan gate: 判定不能:", err)
		return 2
	}
	res := gate.Judge(b.entries, h.entries, gopt)
	s := report.BuildGate(sc.Root, baseHash, head.Name(), res)
	// base が head の commit と同じなら、見たのは未コミットの変更だけ (CI で -base を付け忘れた疑い)。
	if hc, err := repo.CommitHash(sc.Root, headCommit); err == nil && hc == baseHash {
		s.BaseIsHead = true
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
	if res.Failed() {
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
