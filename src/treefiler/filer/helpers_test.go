package filer

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"testwait"
)

// needGit は git の無い環境ではテストを飛ばす。CI (macos runner には git がある) で無いのは環境の壊れなので落とす
// (Skip のまま緑にすると、git を使う検査が CI で 1 本も走らないことが見えない)。
func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("CI に git が無い")
		}
		t.Skip("git が無い")
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// commitAll は dir の変更を全部 stage して commit する。
func commitAll(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "add", ".")
	gitCommit(t, dir)
}

// gitCommit は stage 済みのものを commit する (作者は固定。利用者の設定は読まない)。
func gitCommit(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x")
}

// settle は動きを終点まで進める (Advance で裏の結果を取り込み、アニメーションを飛ばす)。
func settle(m *Model) {
	m.Advance(fixedNow)
	m.snapAll()
}

// newAt は dir を root にした Model (時刻は fixedNow に固定)。大きさは呼び出し側が決める。
func newAt(t testing.TB, dir string) *Model {
	t.Helper()
	m, err := New(dir, Options{Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// mustWrite は path に body を書く (0o644)。
// dupscan:allow-group 3 intentional 0o644 で書いて落とす mustWrite は safefs・pro-con のテストも同じ数行を持つ。_test.go の helper は module の外から import できず、寄せるにはテスト専用の module を新設して、使う各 module の go.mod の replace と workflow の paths に 1 行ずつ足すことになる。数行に対して依存の方が重い
func mustWrite(t testing.TB, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readBody は p の中身を読む。
// dupscan:allow-group 2 intentional ファイルを読んで落とす readBody は safefs のテストも同じ数行を持つ。_test.go の helper は module の外から import できず、寄せるにはテスト専用の module を新設して、使う各 module の go.mod の replace と workflow の paths に 1 行ずつ足すことになる。数行に対して依存の方が重い
func readBody(t testing.TB, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// initRepo は dir に branch を初期の枝にした repo を作る。
func initRepo(t *testing.T, dir, branch string) {
	t.Helper()
	gitIn(t, dir, "init", "-q", "-b", branch)
}

// isolateConfig はこのテストの設定の置き場を自分だけの一時ディレクトリにする (TestMain の置き場は全テストで共有なので、`.` などで保存する
// テストの後に読むと結果が順序に依存する)。
func isolateConfig(t testing.TB) {
	t.Helper()
	t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir())
}

// mkfile は親のフォルダを作ってから p に body を書く。
func mkfile(t testing.TB, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p, body)
}

// waitWalked は裏の走査が dir の結果を出すまで、時計を 1 秒ずつ進めて待つ (now は呼び出し側の時計。進めた分が残る)。
func waitWalked(t *testing.T, m *Model, now *time.Time, dir string) {
	t.Helper()
	testwait.Until(t, "走査の結果: "+dir, func() bool {
		*now = now.Add(time.Second)
		m.Advance(*now)
		_, ok := m.recs[dir]
		return ok
	})

}
