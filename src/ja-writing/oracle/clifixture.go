package oracle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"ja-writing/cmdenv"
)

// TB は CLI の記録と突き合わせるテストが testing.T に求める道具 (oracle を testing に依存させないための最小の形)。
type TB interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Chdir(dir string)
	Context() context.Context
}

// CLIFixture は記録の作業フォルダの写し。files (作業フォルダからの相対パス → 中身) を 1 度だけ使い捨ての dir に置く。
// テストは作業フォルダへ chdir する (本家の入力のパスの検査が、プロセスの cwd を見るため)。プロセスの cwd は 1 つなので、これを使うテストは t.Parallel を使わない。
// 消すのはパッケージのテストが終わるとき (TestMain で Cleanup を呼ぶ)。
type CLIFixture struct {
	prefix string
	build  func() (map[string]string, error)
	once   sync.Once
	root   string
	err    error
}

// NewCLIFixture は prefix を一時 dir の名前に付け、build が返すファイルを置く fixture。
func NewCLIFixture(prefix string, build func() (map[string]string, error)) *CLIFixture {
	return &CLIFixture{prefix: prefix, build: build}
}

// Root は作業フォルダの実パス。置くのは最初の 1 回だけ。
func (f *CLIFixture) Root(t TB) string {
	t.Helper()
	f.once.Do(func() {
		var files map[string]string
		if files, f.err = f.build(); f.err != nil {
			return
		}
		var dir string
		if dir, f.err = os.MkdirTemp("", f.prefix); f.err != nil {
			return
		}
		if f.root, f.err = filepath.EvalSymlinks(dir); f.err != nil {
			return
		}
		f.err = placeFiles(f.root, files)
	})
	if f.err != nil {
		t.Fatal(f.err)
	}
	// 本家の使い捨ての dir と同じ前提: 上に .git・.yomiyasu-words・.yomiyasu があると、語の一覧の探索が上まで届いて結果が置き場所で変わる。
	for cur := f.root; ; cur = filepath.Dir(cur) {
		for _, name := range []string{".git", ".yomiyasu-words", ".yomiyasu"} {
			if _, err := os.Lstat(filepath.Join(cur, name)); err == nil {
				t.Fatalf("テストの作業フォルダ %s の上 (%s) に %s があります。TMPDIR を、上に .git・.yomiyasu-words・.yomiyasu が無い場所に向けてください", f.root, cur, name)
			}
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	return f.root
}

// Cleanup は作業フォルダを消す (TestMain から呼ぶ)。
func (f *CLIFixture) Cleanup() {
	if f.root != "" {
		_ = os.RemoveAll(f.root)
	}
}

// RunMain は main を、標準入力・作業フォルダ (root からの相対) を指定して呼び、stdout・stderr・rc を返す (root の実パスは RootToken に戻す)。
func RunMain(t TB, main func(context.Context, cmdenv.Env) int, root, cwd string, stdin []byte, args ...string) (stdout, stderr string, rc int) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(cwd))
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	env := cmdenv.Env{Args: args, Stdin: bytes.NewReader(stdin), Stdout: &out, Stderr: &errOut, Cwd: dir, Getenv: func(string) string { return "" }}
	rc = main(t.Context(), env)
	return strings.ReplaceAll(out.String(), root, RootToken), strings.ReplaceAll(errOut.String(), root, RootToken), rc
}

// CLIDigest は CLI の記録の stdout の SHA-256 の先頭 LintCLIDigestWidth 字。
func CLIDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:LintCLIDigestWidth]
}
