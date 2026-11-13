package lint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ja-writing/cmdenv"
	"ja-writing/oracle"
	"ja-writing/visibility"
)

// runLint は lint.Main を、標準入力・作業フォルダ (記録の root からの相対) を指定して呼び、stdout・stderr・rc を返す (root の実パスは oracle.RootToken に戻す)。
func runLint(t *testing.T, root, cwd string, stdin []byte, args ...string) (stdout, stderr string, rc int) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(cwd))
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	env := cmdenv.Env{Args: args, Stdin: bytes.NewReader(stdin), Stdout: &out, Stderr: &errOut, Cwd: dir, Getenv: func(string) string { return "" }}
	rc = Main(t.Context(), env)
	return strings.ReplaceAll(out.String(), root, oracle.RootToken), strings.ReplaceAll(errOut.String(), root, oracle.RootToken), rc
}

func shortDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:oracle.LintCLIDigestWidth]
}

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

// ヘルプの文面は、本家の記録 (-h の標準出力の全文) と同じバイト列を embed している。
func TestHelpTextIsTheRecordedOne(t *testing.T) {
	var g oracle.LintCLIGolden
	if err := oracle.ReadGolden(goldenDir, oracle.LintCLIFile, &g); err != nil {
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

// 記録の入力の集まりは、入力のファイルが作業フォルダに置かれることを前提にする (置き場の食い違いを、記録と突き合わせる前に見つける)。
func TestFixtureRootHoldsEveryCorpusFile(t *testing.T) {
	root := workRoot(t)
	for _, f := range fixtureCorpus.Files {
		if _, err := os.Stat(filepath.Join(root, "corpus", filepath.FromSlash(f.Path))); err != nil {
			t.Fatal(err)
		}
	}
	if len(fixtureCorpus.Files) < 300 {
		t.Fatalf("入力の集まりが %d 件", len(fixtureCorpus.Files))
	}
}
