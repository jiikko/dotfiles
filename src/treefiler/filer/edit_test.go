package filer

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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
	if tl.scroll != 1 || int(tl.sy.v) != 1 {
		t.Fatalf("開き直した後の scroll = %d・sy = %v (読んでいた位置 1 を保つはず)", tl.scroll, tl.sy.v)
	}
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

// 変わっていないファイルのタイルは開き直さない (! や s から戻るたびに、深く送ったタイルで読んだ所までを読み直さない)。
// 縮んだファイルでは、表示の位置 (sy) も本文の中へ合わせる (古い位置の空白と「1999/1500」を描いてから巻き戻さない)。
func TestTileRereadSkipsUnchangedAndClampsShrunkFile(t *testing.T) {
	clearEditorEnv(t)
	m := newTest(t)
	p := filepath.Join(m.root.path(), "long.txt")
	var b strings.Builder
	for i := range 2000 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	mkfile(t, p, b.String())
	m.Refresh()
	settle(m)
	cdTo(t, m, "long.txt")
	m.HandleKey("enter")
	tl := m.frontTile()
	m.HandleKey("G")
	settle(m) // 表示の位置 (sy) を末尾まで動かし切る (動かないままだと、縮んだときの sy の確かめが何も見ない)
	if int(tl.sy.v) < 1500 {
		t.Fatalf("前提: 末尾へ送った sy = %v", tl.sy.v)
	}
	src := tl.src
	m.ExecDone(nil)
	if tl.src != src {
		t.Fatal("変わっていないファイルのタイルを開き直した")
	}
	if tl.scroll < 1500 {
		t.Fatalf("前提: 末尾へ送った scroll = %d", tl.scroll)
	}
	mkfile(t, p, "short\n")
	m.ExecDone(nil)
	if tl.src == src {
		t.Fatal("縮んだファイルのタイルを開き直さない")
	}
	if tl.scroll != 0 || int(tl.sy.v) != 0 {
		t.Fatalf("1 行に縮んだファイルの scroll = %d・sy = %v (どちらも 0 のはず)", tl.scroll, tl.sy.v)
	}
}

// 差分のタイルは、本文が変わっていなくても取り直す (git add / commit は作業ツリーのファイルの大きさも更新時刻も変えない)。
// 途中で読めなくなったタイルも開き直す (chmod で権限を戻しても大きさと更新時刻は変わらない)。
func TestTileRereadRetakesDiffAndRetriesReadError(t *testing.T) {
	clearEditorEnv(t)
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	m.HandleKey("enter")
	tl := m.frontTile()
	tl.setMode(modeDiff)
	job := tl.diff
	m.ExecDone(nil)
	if tl.mode != modeDiff || tl.diff == job {
		t.Fatalf("本文の変わっていない差分のタイルを取り直さない (mode=%v・同じ取得=%v)", tl.mode, tl.diff == job)
	}
	tl.setMode(modeFile)
	tl.src.readErr = true // 途中で読めなくなった (ensure の Open が失敗した) のと同じ
	src := tl.src
	m.ExecDone(nil)
	if tl.src == src || tl.src.readErr {
		t.Fatal("途中で読めなくなったタイルを、読めるようになった後も開き直さない")
	}
}

// 消したファイルの差分のタイルも取り直す (差分は本文を要さない。開けない理由を出して古い差分を残さない)。
func TestTileRereadRetakesDiffOfRemovedFile(t *testing.T) {
	clearEditorEnv(t)
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	m.HandleKey("enter")
	tl := m.frontTile()
	tl.setMode(modeDiff)
	job := tl.diff
	if err := os.Remove(tl.n.path()); err != nil {
		t.Fatal(err)
	}
	m.TakeNotices()
	m.ExecDone(nil)
	if tl.diff == job {
		t.Fatal("消したファイルの差分のタイルを取り直さない")
	}
	if ns := m.TakeNotices(); len(ns) != 0 {
		t.Fatalf("差分のタイルで本文を開けない知らせが出た: %+v", ns)
	}
}

// 大きさを変えずに書き換えたファイル (1 文字の打ち直し) も開き直す (大きさだけで変わっていないと見なさない)。
func TestTileRereadsSameSizeEdit(t *testing.T) {
	clearEditorEnv(t)
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	m.HandleKey("enter")
	tl := m.frontTile()
	p := tl.n.path()
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	after := strings.Replace(string(before), "1", "9", 1)
	if len(after) != len(before) || after == string(before) {
		t.Fatal("前提: 同じ大きさで中身だけ変える")
	}
	mkfile(t, p, after)
	future := time.Now().Add(time.Hour) // 同じ時刻の打ち直しで更新時刻が揃わないように、はっきり進める
	if err := os.Chtimes(p, future, future); err != nil {
		t.Fatal(err)
	}
	m.ExecDone(nil)
	if got := firstPlain(m.view(tl)); got != "9" {
		t.Fatalf("同じ大きさで書き換えた後の 1 行目 = %q (9 のはず)", got)
	}
}
