package lint

import (
	"os"
	"path/filepath"
	"testing"

	"ja-writing/oracle"
)

func runLint(t *testing.T, root, cwd string, stdin []byte, args ...string) (stdout, stderr string, rc int) {
	t.Helper()
	return oracle.RunMain(t, Main, root, cwd, stdin, args...)
}

func shortDigest(s string) string { return oracle.CLIDigest(s) }

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
