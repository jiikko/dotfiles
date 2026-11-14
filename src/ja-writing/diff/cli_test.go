package diff

import (
	"strings"
	"testing"

	"ja-writing/oracle"
)

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
