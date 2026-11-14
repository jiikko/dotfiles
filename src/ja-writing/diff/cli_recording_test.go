//go:build !race

// 記録との突き合わせの重い検査。1 つの goroutine で順に呼ぶだけで、-race を付けても見つけられるデータ競争は無く、時間だけが数倍になる
// (race ありの手元の計測で、これ 1 本が package の 183 秒のうち 124 秒)。race の run からは外し、Makefile の test が race なしで別に回す。

package diff

import (
	"strings"
	"testing"

	"ja-writing/oracle"
	"ja-writing/visibility"
)

// CLI の単位の記録 (testdata/golden/diff-cli.json) と、Main の出力を突き合わせる。
// 引数の誤り・--version・-h・--endings・本体まで進む呼び出し (text と --json) を全部比べる。
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
	kept := 0
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name {
			t.Fatalf("%d 番目の case の名前 %q が記録の %q と違う (make golden で作り直す)", i, c.Name, rec.Name)
		}
		stdout, stderr, rc := oracle.RunMain(t, Main, root, c.Cwd, c.Stdin, c.Args...)
		if first, _, found := strings.Cut(stderr, "\n"); c.FirstLine && found {
			stderr = first + "\n" // 本家が rc 3 で止まる呼び出しは、トレースバックを揃えず 1 行目だけを比べる
		}
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
	// 空振りで緑にしない: 全文まで比べた記録があること。
	if kept < 10 {
		t.Errorf("全文まで比べた記録が %d 件しかない", kept)
	}
	t.Logf("CLI の記録 %d 件を全部比べた (全文まで %d 件)", len(cases), kept)
}
