package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"glogx/issues"
)

// repoRootOracle は repoRoot() と**別の手段**で repo root を出す独立オラクル
// (テストファイルの位置から .git を持つ親を探す)。
//
// 🚨 期待値を production の repoRoot() から作らないこと: 同じ関数を両側で呼ぶ自己言及になり、
// repoRoot() が壊れても緑のままになる (issue 082)。git に問い合わせる repoRoot() とは
// 実装経路が違うので、片方の誤りをもう片方が捕まえられる。
func repoRootOracle(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("テストファイルの位置が取れない")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return realPath(t, dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf(".git を持つ親が見つからない (起点 %s)", file)
		}
		dir = parent
	}
}

// realPath は symlink を解決した絶対パス (macOS の /var → /private/var 等を吸収する)。
func realPath(t *testing.T, path string) string {
	t.Helper()
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func stubLookPath(t *testing.T, available map[string]string) {
	t.Helper()
	orig := lookPathFn
	lookPathFn = func(name string) (string, error) {
		if p, ok := available[name]; ok {
			return p, nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPathFn = orig })
}

// e: nvim が repo root (取れなければ ".") を cwd に `nvim .` で起動される
func TestOpenEditorAtRoot(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	cmds := stubEditorCapture(t)

	if _, cmd := m.handleKey("e"); cmd == nil {
		t.Fatal("e が tea.Cmd を返さない")
	}
	if len(*cmds) != 1 {
		t.Fatalf("エディタ起動回数 = %d, want 1", len(*cmds))
	}
	c := (*cmds)[0]
	if len(c.Args) < 2 || !strings.HasSuffix(c.Args[0], "nvim") || c.Args[1] != "." {
		t.Fatalf("起動コマンドが nvim . でない: %v", c.Args)
	}
	// 🚨 `nvim .` は引数でなく **cwd が開く対象**なので、非空チェックでは対象の取り違えを
	// 通してしまう (実測: cmd.Dir を "/" にしても全テストが green だった)。期待値は
	// repoRoot() ではなく独立オラクルから作る (repoRootOracle の doc 参照)。
	if want := repoRootOracle(t); realPath(t, c.Dir) != want {
		t.Fatalf("cwd が repo root でない: %q want %q", c.Dir, want)
	}
}

// E: 探索順の先頭で見つかったファイラーが repo root を cwd に起動される
func TestOpenFilerAtRootPicksFirstCandidate(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	cmds := stubEditorCapture(t)
	// yazi は無く ranger と lf がある → 探索順どおり ranger が選ばれる
	stubLookPath(t, map[string]string{"ranger": "/opt/bin/ranger", "lf": "/opt/bin/lf"})

	if _, cmd := m.handleKey("E"); cmd == nil {
		t.Fatal("E が tea.Cmd を返さない")
	}
	if len(*cmds) != 1 {
		t.Fatalf("ファイラー起動回数 = %d, want 1", len(*cmds))
	}
	c := (*cmds)[0]
	if c.Args[0] != "/opt/bin/ranger" {
		t.Fatalf("探索順の先頭 (ranger) でない: %v", c.Args)
	}
	if want := repoRootOracle(t); realPath(t, c.Dir) != want { // 対象は cwd (上の e と同じ理由)
		t.Fatalf("ファイラーの cwd が repo root でない: %q want %q", c.Dir, want)
	}
}

// E: ファイラーが 1 つも無ければ起動せず理由をトーストで案内する
func TestOpenFilerAtRootNoneFound(t *testing.T) {
	m := newTestBrowse(t, 1, nil, nil)
	cmds := stubEditorCapture(t)
	stubLookPath(t, nil)

	m.handleKey("E")
	if len(*cmds) != 0 {
		t.Fatalf("ファイラー不在なのに起動している: %v", (*cmds)[0].Args)
	}
	if !strings.Contains(m.toast.text, "ファイラーが見つかりません") {
		t.Fatalf("不在理由のトーストが出ていない: %q", m.toast.text)
	}
}

// repoRoot 自体の単体テスト (これまで 0 本だった。上の 2 本が「repoRoot の単体テストの担当」と
// 書いていた対象が実在しなかったので足す)。
func TestRepoRootReturnsGitToplevelAndFallsBack(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い環境")
	}
	// 1. repo の中 (サブディレクトリ) では toplevel を返す
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init 失敗: %v: %s", err, out)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	if got, want := realPath(t, repoRoot()), realPath(t, root); got != want {
		t.Fatalf("repo 内で toplevel を返さない: got %q want %q", got, want)
	}

	// 2. repo 外では "." に落ちる (nvim/ファイラーを起動できる形を保つ)
	outside := t.TempDir()
	t.Chdir(outside)
	if got := repoRoot(); got != "." {
		t.Fatalf("repo 外のフォールバックが \".\" でない: %q", got)
	}
}

// TestRepoRootFailureSemanticsAreCallerSpecific は「解決できなかったとき何を使うか」が
// 呼び出し側ごとに違うことを固定する (issue 320)。
//
// 🚨 これは**わざと違う 3 値**で、揃えてはいけない:
//
//	repoRoot()                    → "."  (nvim を開く先。cwd だと意図がぼやける)
//	loadWorktreeStatus の st.root → ""   (untracked プレビューが cwd 相対に落ちるだけ)
//	issues.RepoRoot()             → cwd  (git 管理外でも issues/ を探せるように)
//
// 解決そのものは issues.ResolveRepoRoot の 1 実装に寄せた。分岐だけが呼び出し側に残る。
// このテストが無いと、1 実装化のときに 3 値を「揃える」方向の変異が緑で通る。
func TestRepoRootFailureSemanticsAreCallerSpecific(t *testing.T) {
	outside := realPath(t, t.TempDir()) // git 管理外 (/var/folders 配下に .git は無い)
	// 🚨 **ここを Skip にしないこと**。最初 `t.Skipf` で書いたところ、
	// 「ResolveRepoRoot が失敗時に (cwd, true) を返す」変異でテスト全体が skip され、
	// **緑のまま通った** (`_claude/rules/mutation-verify-new-tests.md`「前提が早期 return で
	// 素通りしていないか」そのもの)。前提は Fatal で固定する。
	if root, ok := issues.ResolveRepoRoot(outside); ok {
		t.Fatalf("git 管理外の %s で ResolveRepoRoot が ok=true (root=%q) を返した。"+
			"失敗を成功に化けさせている (または TempDir が git repo の中にある)", outside, root)
	}

	t.Run("issues.RepoRoot は cwd", func(t *testing.T) {
		if got := issues.RepoRoot(outside); got != outside {
			t.Errorf("issues.RepoRoot(%q) = %q, want %q (git 管理外でも issues/ を探せるように cwd)", outside, got, outside)
		}
	})

	t.Run("repoRoot() は \".\"", func(t *testing.T) {
		t.Chdir(outside)
		if got := repoRoot(); got != "." {
			t.Errorf("repoRoot() = %q, want %q (nvim を「今いる場所」で開く)", got, ".")
		}
	})

	// 🚨 **`loadWorktreeStatus` の st.root はここでは検査していない**。理由を残す:
	// 最初「解決部分だけを同じ形で呼ぶ」テストを書いたが、それは production を 1 行も通らない
	// 自己言及だった (`st.root = issues.RepoRoot(currentDir())` に変える変異が緑で通った)。
	// production を通す形にするには「`git status` は成功するが `rev-parse` だけ失敗する」状態が
	// 要り、それは seam (runGitTimeout の差し替え) を新設しないと作れない。
	// 発火条件が timeout / 一時障害に限られる差なので、seam を足す価値より
	// **意図を worktree_status.go のコメントで固定する**方を採った
	// (`_claude/rules/refuse-low-value-coverage.md` の「テスト困難 × 低価値」)。
}
