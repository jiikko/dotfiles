package filer

import (
	"path/filepath"
	"slices"
	"testing"
)

// takeEdit は e で頼まれたエディタの起動を取り出す ($VISUAL / $EDITOR を空にして既定の nvim に固定する)。
func takeEdit(t *testing.T, m *Model, key string) ExecRequest {
	t.Helper()
	if r := m.HandleKey(key); r != Exec {
		t.Fatalf("%s で Exec を返さない (%v)", key, r)
	}
	r, ok := m.TakeExec()
	if !ok {
		t.Fatalf("%s でプロセスを頼んでいない", key)
	}
	return r
}

func clearEditorEnv(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
}

// 木の e: ファイルならエディタ、フォルダなら今までどおり explode (spec §0.1)。
func TestTreeEEditsFileAndExplodesFolder(t *testing.T) {
	clearEditorEnv(t)
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	p := m.cur.path()
	r := takeEdit(t, m, "e")
	if !slices.Equal(r.Argv, []string{"nvim", p}) || r.Dir != filepath.Dir(p) {
		t.Fatalf("ファイルの e = %+v (親のフォルダで nvim <path>)", r)
	}
	if m.exploding != nil {
		t.Fatal("ファイルの e で explode が走った")
	}

	cdTo(t, m, "b")
	if got := m.HandleKey("e"); got != None {
		t.Fatalf("フォルダの e = %v (explode は端末を明け渡さない)", got)
	}
	if _, ok := m.TakeExec(); ok {
		t.Fatal("フォルダの e でプロセスを頼んだ")
	}
	if m.exploding == nil {
		t.Fatal("フォルダの e で explode が始まらない")
	}
	m.exploding.cancel()
}

// タイルの e: 手前のタイルのファイルを開く。タイルは閉じない (戻ったら同じ所を読み続けられる)。
func TestTileEEditsShownFile(t *testing.T) {
	clearEditorEnv(t)
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	m.HandleKey("enter")
	r := takeEdit(t, m, "e")
	if want := filepath.Join(m.root.path(), "a", "one.txt"); !slices.Equal(r.Argv, []string{"nvim", want}) {
		t.Fatalf("タイルの e = %v (表示中のファイル %s)", r.Argv, want)
	}
	if m.frontTile() == nil || m.frontTile().closing {
		t.Fatal("e でタイルが閉じた")
	}
}

// ジャンプ中の e: 選んだリンクの先を開く (issues viewer のジャンプモードの e と同じ)。タイルのファイルではない。
func TestJumpEEditsSelectedLink(t *testing.T) {
	clearEditorEnv(t)
	m := newTest(t)
	cdTo(t, m, "a/three.md")
	m.HandleKey("enter")
	m.HandleKey("tab")
	m.HandleKey("j") // 2 つ目のリンク (b/deep/x.txt)
	r := takeEdit(t, m, "e")
	if want := filepath.Join(m.root.path(), "b", "deep", "x.txt"); !slices.Equal(r.Argv, []string{"nvim", want}) {
		t.Fatalf("ジャンプ中の e = %v (選んだリンク %s)", r.Argv, want)
	}
	if r.Env[len(r.Env)-1] != "f="+r.Argv[1] {
		t.Fatalf("$f が開くファイルでない: %s", r.Env[len(r.Env)-1])
	}
	if m.frontTile().jump {
		t.Fatal("e の後もジャンプモードのまま")
	}
}
