//go:build !race

// 記録との突き合わせの重い検査。1 つの goroutine で順に呼ぶだけで、-race を付けても見つけられるデータ競争は無く、時間だけが数倍になる
// (CI の race の run で lint のパッケージが go test の時間の上限 10 分を超えた)。race の run からは外し、Makefile の test が race なしで別に回す。

package lint

import (
	"testing"

	"ja-writing/oracle"
	"ja-writing/visibility"
)

// CLI の単位の記録 (testdata/golden/lint-cli.json: 入力の集まりの全ファイルを、種類・読者・語の一覧・text と --json・終了コードの条件で通した出力) と、Main の出力を突き合わせる。
// 記録の全件を比べる (本家が候補を出す入力も、検査まで進まない呼び出し (誤りの呼び出し・--version・-h) も)。比べた件数が case の数と違えば落とす。
func TestCLIMatchesPythonRecording(t *testing.T) {
	root := workRoot(t)
	var g oracle.LintCLIGolden
	if err := oracle.ReadGolden(goldenDir, oracle.LintCLIFile, &g); err != nil {
		t.Fatal(err)
	}
	cases := oracle.LintCLICases(fixtureCorpus, visibility.DocTypeValues(), visibility.ReaderValues())
	if len(cases) != len(g.Cases) {
		t.Fatalf("case が %d 件、記録が %d 件 (make golden で作り直す)", len(cases), len(g.Cases))
	}
	compared, kept := 0, 0
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name {
			t.Fatalf("%d 番目の case の名前 %q が記録の %q と違う (make golden で作り直す)", i, c.Name, rec.Name)
		}
		stdout, stderr, rc := runLint(t, root, c.Cwd, c.Stdin, c.Args...)
		compared++
		if rc != rec.RC || stderr != rec.Err || len(stdout) != rec.Len || shortDigest(stdout) != rec.Out {
			t.Errorf("%s: 本家と出力が違う: rc=%d (本家 %d)、stderr %q (本家 %q)、stdout %d バイト・%s (本家 %d バイト・%s)\n%.600s",
				rec.Name, rc, rec.RC, stderr, rec.Err, len(stdout), shortDigest(stdout), rec.Len, rec.Out, stdout)
			continue
		}
		if rec.Stdout != "" {
			kept++
			if stdout != rec.Stdout {
				t.Errorf("%s: 全文が違う", rec.Name)
			}
		}
	}
	// 空振りで緑にしない: 全件を比べたこと、全文まで比べた記録があること。
	if compared != len(cases) || kept < 10 {
		t.Errorf("比べた記録が足りない (比べた %d・case %d・全文まで比べた %d)", compared, len(cases), kept)
	}
	t.Logf("CLI の記録 %d 件を全部比べた (全文まで %d 件)", compared, kept)
}
