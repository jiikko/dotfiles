package filer

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// 木の e: ファイルでもフォルダでもエディタで開く (spec §0.1)。explode は > にあり、e では走らない。
func TestTreeEEditsFileAndFolder(t *testing.T) {
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
	p = m.cur.path()
	r = takeEdit(t, m, "e")
	if !slices.Equal(r.Argv, []string{"nvim", p}) {
		t.Fatalf("フォルダの e = %+v (nvim <フォルダ>)", r)
	}
	if m.exploding != nil {
		t.Fatal("フォルダの e で explode が走った")
	}
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

// タイルの e から戻ったら、タイルも読み直す (spec §0.1 の e「戻ったら読み直す」)。読み直さないと、書き換えた後も古い本文が
// 残り、先へ送ると新しいファイルを古い読み位置から読み足して、行の重複・欠け・行の途中からの表示が混ざる。
func TestTileRereadsFileAfterEditor(t *testing.T) {
	clearEditorEnv(t)
	m := newTest(t)
	p := filepath.Join(m.root.path(), "long.txt")
	body := func(prefix string, n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "%s %d\n", prefix, i)
		}
		return b.String()
	}
	mkfile(t, p, body("old", 2000))
	m.Refresh()
	settle(m)
	cdTo(t, m, "long.txt")
	m.HandleKey("enter")
	tl := m.frontTile()
	if tl == nil {
		t.Fatal("タイルが開かない (テストの前提の破れ)")
	}
	m.HandleKey("j")
	takeEdit(t, m, "e")
	mkfile(t, p, body("new", 1500))
	m.ExecDone(nil)
	m.HandleKey("G")
	vl := m.view(tl)
	if len(vl) == 0 || vl[0].plain != "new 0" {
		t.Fatalf("エディタから戻った後のタイルの 1 行目 = %q (書き換えた本文 new 0 のはず)", firstPlain(vl))
	}
	for i, l := range vl {
		if want := fmt.Sprintf("new %d", i); l.plain != want {
			t.Fatalf("%d 行目 = %q, want %q (古い読み位置から読み足した)", i, l.plain, want)
		}
	}
}

func firstPlain(vl []viewLine) string {
	if len(vl) == 0 {
		return ""
	}
	return vl[0].plain
}

// エディタの中で消したファイルのタイルは、古い本文を残して理由を知らせる (黙って古いまま見せない・タイルを勝手に閉じない)。
func TestTileKeepsOldTextAndTellsWhenFileGoneAfterEditor(t *testing.T) {
	clearEditorEnv(t)
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	m.HandleKey("enter")
	tl := m.frontTile()
	before := firstPlain(m.view(tl))
	takeEdit(t, m, "e")
	if err := os.Remove(filepath.Join(m.root.path(), "a", "one.txt")); err != nil {
		t.Fatal(err)
	}
	m.TakeNotices()
	m.ExecDone(nil)
	if got := firstPlain(m.view(tl)); got != before {
		t.Fatalf("消えたファイルのタイルの 1 行目 = %q, want 古い本文 %q", got, before)
	}
	ns := m.TakeNotices()
	if len(ns) == 0 || !strings.Contains(ns[0].Text, "one.txt") {
		t.Fatalf("消えたファイルの知らせ = %+v (one.txt を開けない理由を出すはず)", ns)
	}
}
