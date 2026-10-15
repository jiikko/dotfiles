// issue-claim は issue に着手の claim を付ける: next/ に目印 (symlink) を置き、本文冒頭に担当者バナーを書く。
// 中身は glogx の issues viewer の `n` と同じ issues.MoveToSubdir(iss, NextDirName) で、書式と失敗時の戻しもそちらが正本。
// commit と push は呼び出し側 (scripts/issue_claim.sh) が一時 worktree で行う。
//
//	issue-claim <issues ディレクトリ> <NNN>
//
// 終了コード: 0 = claim した / 1 = 拒否 (既に claim 済み・直下に無い・実体が 0 件 / 複数) / 2 = 引数の誤り・書けなかった
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"glogx/issues"
)

var numberRe = regexp.MustCompile(`^[0-9]{3,}$`)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintln(stdout, "usage: issue-claim <issues ディレクトリ> <NNN>")
		return 0
	}
	if len(args) != 2 || !numberRe.MatchString(args[1]) {
		fmt.Fprintln(stderr, "usage: issue-claim <issues ディレクトリ> <NNN>")
		return 2
	}
	dir, num := args[0], args[1]
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		fmt.Fprintf(stderr, "issue-claim: issues ディレクトリが無い: %s\n", dir)
		return 2
	}
	list, warnings := issues.Scan([]string{dir})
	var hits []*issues.Issue
	for _, iss := range list {
		// 接頭辞つきの ID (`PRO-105`) は Number が "105" になるので、接頭辞の無いものだけを番号で拾う
		if iss.Number == num && iss.Prefix == "" && iss.Status != issues.StatusDone {
			hits = append(hits, iss)
		}
	}
	// 壊れた目印などの警告は、拒否や失敗の理由になりうるので出しておく
	for _, w := range warnings {
		fmt.Fprintf(stderr, "issue-claim: 警告: %s\n", w)
	}
	switch len(hits) {
	case 0:
		fmt.Fprintf(stderr, "issue-claim: issue %s の実体が無い (done/ に在るか、番号が違う)\n", num)
		return 1
	case 1:
	default:
		fmt.Fprintf(stderr, "issue-claim: issue %s の実体が %d 件ある (番号の衝突)\n", num, len(hits))
		for _, iss := range hits {
			fmt.Fprintf(stderr, "  %s\n", iss.Rel)
		}
		return 1
	}
	iss := hits[0]
	// 🚨 MoveToSubdir は claim 済みの issue に対して何もせず成功を返し、直下に無い issue (pending/ waiting/) は
	// ファイルを next/ へ動かす (旧運用)。どちらも claim の道具としては拒否する (目印は symlink、ファイルは動かさない)
	if iss.Status == issues.StatusNext || iss.NextLink != "" {
		fmt.Fprintf(stderr, "issue-claim: issue %s は既に claim されている: %s\n", num, iss.Rel)
		return 1
	}
	if iss.Status != issues.StatusOpen {
		fmt.Fprintf(stderr, "issue-claim: issue %s は直下 (open) に無い: %s。pending / waiting から出してから claim する\n", num, iss.Rel)
		return 1
	}
	if _, err := issues.MoveToSubdir(iss, issues.NextDirName); err != nil {
		fmt.Fprintf(stderr, "issue-claim: issue %s を claim できない: %v\n", num, err)
		return 2
	}
	fmt.Fprintf(stdout, "%s\n%s\n", iss.Rel, filepath.ToSlash(mustRel(dir, issues.NextLinkPath(iss))))
	return 0
}

// mustRel は path を dir からの相対で返す (取れなければ path のまま)。
func mustRel(dir, path string) string {
	if r, err := filepath.Rel(dir, path); err == nil {
		return r
	}
	return path
}
