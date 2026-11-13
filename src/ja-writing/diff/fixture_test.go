package diff

import (
	"os"
	"testing"

	"ja-writing/oracle"
)

const (
	goldenDir = "../testdata/golden"
	corpusDir = "../testdata/corpus"
	diffDir   = "../testdata/diff"
)

// 本家の記録と突き合わせるテストが共有する作業フォルダ。記録を取ったときと同じファイルを oracle.DiffCLIFiles が決める。
// テストは作業フォルダへ chdir する (本家の入力のパスの検査が、プロセスの cwd を見るため)。プロセスの cwd は 1 つなので、これを使うテストは t.Parallel を使わない。
var (
	fixtureCorpus oracle.Corpus
	fixtureInputs []oracle.DiffInput
	fixture       = oracle.NewCLIFixture("ja-writing-diff-", func() (map[string]string, error) {
		var err error
		if fixtureCorpus, err = oracle.LoadCorpus(corpusDir); err != nil {
			return nil, err
		}
		if fixtureInputs, err = oracle.LoadDiffInputs(diffDir); err != nil {
			return nil, err
		}
		return oracle.DiffCLIFiles(fixtureCorpus, "../testdata/words", diffDir)
	})
)

func TestMain(m *testing.M) {
	code := m.Run()
	fixture.Cleanup()
	os.Exit(code)
}
