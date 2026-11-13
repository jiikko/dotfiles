package lint

import (
	"os"
	"testing"

	"ja-writing/oracle"
)

// 本家の記録と突き合わせるテストが共有する作業フォルダ。記録を取ったときと同じファイルを oracle.LintCLIFiles が決める。
// テストは作業フォルダへ chdir する (本家の入力のパスの検査 InputPathProblem が、プロセスの cwd を見るため)。プロセスの cwd は 1 つなので、これを使うテストは t.Parallel を使わない。

var (
	fixtureCorpus oracle.Corpus
	fixture       = oracle.NewCLIFixture("ja-writing-lint-", func() (map[string]string, error) {
		var err error
		if fixtureCorpus, err = oracle.LoadCorpus(corpusDir); err != nil {
			return nil, err
		}
		return oracle.LintCLIFiles(fixtureCorpus, "../testdata/words")
	})
)

// workRoot は記録の作業フォルダの写しを 1 度だけ作り、その実パスを返す。
func workRoot(t *testing.T) string {
	t.Helper()
	return fixture.Root(t)
}

func TestMain(m *testing.M) {
	code := m.Run()
	fixture.Cleanup()
	os.Exit(code)
}
