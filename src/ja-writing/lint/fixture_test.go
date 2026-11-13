package lint

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"ja-writing/oracle"
)

// 本家の記録と突き合わせるテストが共有する作業フォルダ。記録を取ったときと同じファイルを oracle.LintCLIFiles が決める。
// テストは作業フォルダへ chdir する (本家の入力のパスの検査 InputPathProblem が、プロセスの cwd を見るため)。プロセスの cwd は 1 つなので、これを使うテストは t.Parallel を使わない。

var (
	fixtureOnce   sync.Once
	fixtureRoot   string
	fixtureErr    error
	fixtureCorpus oracle.Corpus
)

// workRoot は記録の作業フォルダの写しを 1 度だけ作り、その実パスを返す (パッケージのテストが終わるときに消えるよう、t.TempDir ではなく TestMain で消す)。
func workRoot(t *testing.T) string {
	t.Helper()
	fixtureOnce.Do(func() {
		fixtureCorpus, fixtureErr = oracle.LoadCorpus(corpusDir)
		if fixtureErr != nil {
			return
		}
		var files map[string]string
		if files, fixtureErr = oracle.LintCLIFiles(fixtureCorpus, "../testdata/words"); fixtureErr != nil {
			return
		}
		var root string
		if root, fixtureErr = os.MkdirTemp("", "ja-writing-lint-"); fixtureErr != nil {
			return
		}
		if fixtureRoot, fixtureErr = filepath.EvalSymlinks(root); fixtureErr != nil {
			return
		}
		fixtureErr = placeFixture(fixtureRoot, files)
	})
	if fixtureErr != nil {
		t.Fatal(fixtureErr)
	}
	// 本家の使い捨ての dir と同じ前提: 上に .git・.yomiyasu-words・.yomiyasu があると、語の一覧の探索が上まで届いて結果が置き場所で変わる。
	for cur := fixtureRoot; ; cur = filepath.Dir(cur) {
		for _, name := range []string{".git", ".yomiyasu-words", ".yomiyasu"} {
			if _, err := os.Lstat(filepath.Join(cur, name)); err == nil {
				t.Fatalf("テストの作業フォルダ %s の上 (%s) に %s があります。TMPDIR を、上に .git・.yomiyasu-words・.yomiyasu が無い場所に向けてください", fixtureRoot, cur, name)
			}
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	return fixtureRoot
}

func placeFixture(root string, files map[string]string) error {
	for rel, body := range files {
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func TestMain(m *testing.M) {
	code := m.Run()
	if fixtureRoot != "" {
		_ = os.RemoveAll(fixtureRoot)
	}
	os.Exit(code)
}
