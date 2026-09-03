package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
