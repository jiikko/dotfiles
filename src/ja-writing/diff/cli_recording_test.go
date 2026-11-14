//go:build !race

// 記録との突き合わせの重い検査。1 つの goroutine で順に呼ぶだけで、-race を付けても見つけられるデータ競争は無く、時間だけが数倍になる
// (race ありの手元の計測で、これ 1 本が package の 183 秒のうち 124 秒)。race の run からは外し、Makefile の test が race なしで別に回す。

package diff

import (
	"strings"
	"testing"

	"ja-writing/cmdenv"
	"ja-writing/internal/pycli"
	"ja-writing/oracle"
	"ja-writing/visibility"
)

// reportImplemented は人が読む出力 (本家の report。群 AD) が入ったか。入ったら true にする。true のとき、比べていない記録が 1 件でもあれば落ちる。
// --json の出力 (本家の diff() の組み立て。群 AC) は入っていて、本体まで進む呼び出しのうち --json のものは全部比べる。
const reportImplemented = false

// unimplementedStderr は人が読む出力がまだ無いときに、引数の検査と入力の読みを済ませた --json なしの呼び出しが出す標準エラー (cmdenv.Unimplemented の文)。
const unimplementedStderr = "ja-writing diff: まだ実装していません\n"

// CLI の単位の記録 (testdata/golden/diff-cli.json) と、Main の出力を突き合わせる。
// 人が読む出力が入るまで、本家が本体まで進んだ --json なしの呼び出し (rc 0 か 1 で、--endings でないもの) は比べられない。それらは「比べていない」と数えてログに出し、
// 引数の誤り・--version・-h・--endings・--json (本体まで進む呼び出しも含む) は全部比べる。Go が「まだ実装していません」と答えるのは、本家が rc 0 か 1 の --json なしの呼び出しだけ
// (本家が断る呼び出しを Go が本体まで進めたら、ここで落ちる)。
func TestCLIMatchesPythonRecording(t *testing.T) {
	root := fixture.Root(t)
	var g oracle.LintCLIGolden
	if err := oracle.ReadGolden(goldenDir, oracle.DiffCLIFile, &g); err != nil {
		t.Fatal(err)
	}
	cases := oracle.DiffCLICases(fixtureCorpus, fixtureInputs, visibility.DocTypeValues(), visibility.ReaderValues())
	if len(cases) != len(g.Cases) {
		t.Fatalf("case が %d 件、記録が %d 件 (make golden で作り直す)", len(cases), len(g.Cases))
	}
	compared, uncompared, kept, mustCompare := 0, 0, 0, 0
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name {
			t.Fatalf("%d 番目の case の名前 %q が記録の %q と違う (make golden で作り直す)", i, c.Name, rec.Name)
		}
		reachesBody := (rec.RC == 0 || rec.RC == pycli.ExitHit) && !hasArg(c.Args, "--endings", "--version", "-h", "--help")
		needsReport := reachesBody && !hasArg(c.Args, "--json")
		if !needsReport {
			mustCompare++
		}
		stdout, stderr, rc := oracle.RunMain(t, Main, root, c.Cwd, c.Stdin, c.Args...)
		if first, _, found := strings.Cut(stderr, "\n"); c.FirstLine && found {
			stderr = first + "\n" // 本家が rc 3 で止まる呼び出しは、トレースバックを揃えず 1 行目だけを比べる
		}
		if !reportImplemented && stderr == unimplementedStderr && rc == cmdenv.ExitUsage && stdout == "" {
			if !needsReport {
				t.Errorf("%s: 本家は rc %d で人が読む出力まで進まないのに、Go は進む", rec.Name, rec.RC)
			}
			uncompared++
			continue
		}
		compared++
		if rc != rec.RC || stderr != rec.Err || len(stdout) != rec.Len || oracle.CLIDigest(stdout) != rec.Out {
			t.Errorf("%s: 本家と出力が違う: rc=%d (本家 %d)、stderr %q (本家 %q)、stdout %d バイト・%s (本家 %d バイト・%s)\n%.600s",
				rec.Name, rc, rec.RC, stderr, rec.Err, len(stdout), oracle.CLIDigest(stdout), rec.Len, rec.Out, stdout)
			continue
		}
		if rec.Stdout != "" {
			kept++
			if stdout != rec.Stdout {
				t.Errorf("%s: 全文が違う", rec.Name)
			}
		}
	}
	// 空振りで緑にしない: 本体まで進まない記録は全部比べたこと、全文まで比べた記録があること。
	if compared+uncompared != len(cases) || compared < mustCompare || kept < 10 {
		t.Errorf("比べた記録が足りない (比べた %d・比べていない %d・case %d・人が読む出力まで進まない記録 %d・全文まで比べた %d)", compared, uncompared, len(cases), mustCompare, kept)
	}
	if reportImplemented && uncompared != 0 {
		t.Errorf("人が読む出力が入ったのに、比べていない記録が %d 件ある", uncompared)
	}
	t.Logf("CLI の記録 %d 件のうち、比べた %d 件 (全文まで %d 件)、人が読む出力が無くて比べていない %d 件", len(cases), compared, kept, uncompared)
}

func hasArg(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n {
				return true
			}
		}
	}
	return false
}
