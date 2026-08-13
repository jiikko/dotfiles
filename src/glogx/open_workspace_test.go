package main

import (
	"errors"
	"strings"
	"testing"
)

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
	// ⚠️ `nvim .` は引数でなく **cwd が開く対象**なので、非空チェックでは対象の取り違えを
	// 通してしまう (実測: cmd.Dir を "/" にしても全テストが green だった)。実際の repo root と
	// 一致することを見る。⚠️ 同じ repoRoot() を呼ぶので repoRoot() 自身の誤りは捕まえない
	// (それは repoRoot の単体テストの担当)。
	if want := repoRoot(); c.Dir != want {
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
	if want := repoRoot(); c.Dir != want { // 対象は cwd (上の e と同じ理由)
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
