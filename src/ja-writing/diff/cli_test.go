package diff

import (
	"strings"
	"testing"

	"ja-writing/cmdenv"
	"ja-writing/internal/pycli"
	"ja-writing/oracle"
	"ja-writing/visibility"
)

// bodyImplemented は検査の本体 (本家の diff() の組み立てと出力) が入ったか。入ったら true にする。true のとき、比べていない記録が 1 件でもあれば落ちる。
const bodyImplemented = false

// unimplementedStderr は検査の本体がまだ無いときに、引数の検査と入力の読みを済ませた呼び出しが出す標準エラー (cmdenv.Unimplemented の文)。
const unimplementedStderr = "ja-writing diff: まだ実装していません\n"

// CLI の単位の記録 (testdata/golden/diff-cli.json) と、Main の出力を突き合わせる。
// 検査の本体が入るまで、本家が本体まで進んだ呼び出し (rc 0 か 1 で、--endings でないもの) は比べられない。それらは「比べていない」と数えてログに出し、
// 引数の誤り・--version・-h・--endings (本家が本体まで進まないもの) は全部比べる。Go が「まだ実装していません」と答えるのは、本家が rc 0 か 1 の呼び出しだけ
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
		if !reachesBody {
			mustCompare++
		}
		stdout, stderr, rc := oracle.RunMain(t, Main, root, c.Cwd, c.Stdin, c.Args...)
		if !bodyImplemented && stderr == unimplementedStderr && rc == cmdenv.ExitUsage && stdout == "" {
			if !reachesBody {
				t.Errorf("%s: 本家は rc %d で本体まで進まないのに、Go は本体まで進む", rec.Name, rec.RC)
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
		t.Errorf("比べた記録が足りない (比べた %d・比べていない %d・case %d・本体まで進まない記録 %d・全文まで比べた %d)", compared, uncompared, len(cases), mustCompare, kept)
	}
	if bodyImplemented && uncompared != 0 {
		t.Errorf("検査の本体が入ったのに、比べていない記録が %d 件ある", uncompared)
	}
	t.Logf("CLI の記録 %d 件のうち、比べた %d 件 (全文まで %d 件)、検査の本体が無くて比べていない %d 件", len(cases), compared, kept, uncompared)
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

// ヘルプの文面は、本家の記録 (-h の標準出力の全文) と同じバイト列を embed している。
func TestHelpTextIsTheRecordedOne(t *testing.T) {
	var g oracle.LintCLIGolden
	if err := oracle.ReadGolden(goldenDir, oracle.DiffCLIFile, &g); err != nil {
		t.Fatal(err)
	}
	for _, rec := range g.Cases {
		if rec.Name == "scenario/help" {
			if rec.Stdout != helpText {
				t.Fatalf("help.txt が本家の -h の出力と違う (%d バイトと %d バイト)", len(helpText), len(rec.Stdout))
			}
			return
		}
	}
	t.Fatal("記録に scenario/help がない")
}

// 記録の入力は、入力のファイルが作業フォルダに置かれることを前提にする (置き場の食い違いを、記録と突き合わせる前に見つける)。
func TestFixtureRootHoldsEveryInput(t *testing.T) {
	root := fixture.Root(t)
	for _, in := range fixtureInputs {
		oldPath, newPath := oracle.DiffInputPaths(in.Name)
		for _, p := range []string{oldPath, newPath} {
			if _, _, rc := oracle.RunMain(t, Main, root, "", nil, "--endings", p); rc != 0 {
				t.Errorf("%s を --endings で読めない (rc %d)", p, rc)
			}
		}
	}
	if len(fixtureInputs) < 100 || len(fixtureCorpus.Files) < 300 {
		t.Fatalf("入力が足りない (手で作った組 %d・入力の集まり %d)", len(fixtureInputs), len(fixtureCorpus.Files))
	}
	if strings.Contains(root, " ") {
		t.Fatalf("作業フォルダの実パスに空白がある: %q", root)
	}
}
