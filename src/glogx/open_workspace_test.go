package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// stubEditorCapture は runEditorCmd を「起動せず *exec.Cmd を記録する」実装へ差し替える
// (stubEditor は回数しか見えず、起動コマンド・cwd の検査ができないためこちらを使う)。
func stubEditorCapture(t *testing.T) *[]*exec.Cmd {
	t.Helper()
	var cmds []*exec.Cmd
	orig := runEditorCmd
	runEditorCmd = func(c *exec.Cmd) tea.Cmd {
		cmds = append(cmds, c)
		return func() tea.Msg { return editorClosedMsg{} }
	}
	t.Cleanup(func() { runEditorCmd = orig })
	return &cmds
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
	if c.Dir == "" {
		t.Fatalf("cwd (repo root) が設定されていない")
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
	if c.Dir == "" {
		t.Fatalf("cwd (repo root) が設定されていない")
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
