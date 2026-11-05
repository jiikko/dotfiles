package filer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// numbered は「prefix 0」から n 行の本文 (行ごとに中身が違うので、古い読み位置から読み足すと番号の並びが崩れて見える)。
func numbered(prefix string, n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "%s %d\n", prefix, i)
	}
	return b.String()
}

// openLongTile は root に name を body で作ってタイルで開き、1 行送る (読み位置を先頭から動かす)。
func openLongTile(t *testing.T, m *Model, name, body string) (*tile, string) {
	t.Helper()
	p := filepath.Join(m.root.path(), name)
	mustWrite(t, p, body)
	m.Refresh()
	settle(m)
	cdTo(t, m, name)
	m.HandleKey("enter")
	tl := m.frontTile()
	if tl == nil {
		t.Fatal("タイルが開かない (テストの前提の破れ)")
	}
	m.HandleKey("j")
	return tl, p
}

func wantNumbered(t *testing.T, m *Model, tl *tile, prefix string, n int) {
	t.Helper()
	m.HandleKey("G")
	m.HandleKey("G")
	vl := m.view(tl)
	if len(vl) != n {
		t.Fatalf("行の数 = %d, want %d (1 行目 %q)", len(vl), n, firstPlain(vl))
	}
	for i, l := range vl {
		if want := fmt.Sprintf("%s %d", prefix, i); l.plain != want {
			t.Fatalf("%d 行目 = %q, want %q (古い読み位置から新しいファイルを読み足した)", i, l.plain, want)
		}
	}
}

// 別の名前に書いて置き換える保存 (inode が変わる) をライブ更新が拾ったら、タイルは開き直して新しい本文を出す。
func TestLiveTileReopensReplacedFile(t *testing.T) {
	m := newTest(t)
	tl, p := openLongTile(t, m, "long.txt", numbered("old", 2000))
	replaceFile(t, p, numbered("new", 1500))
	m.applyChange(dirChange{dir: m.root.path(), changed: []string{"long.txt"}})
	if tl.scroll != 1 {
		t.Fatalf("開き直した後の scroll = %d (読んでいた位置 1 を保つはず)", tl.scroll)
	}
	wantNumbered(t, m, tl, "new", 1500)
}

// 同じファイルをその場で切り詰めて書き直した (inode は同じ・大きさが減る) ときも開き直す。
func TestLiveTileReopensTruncatedFile(t *testing.T) {
	m := newTest(t)
	tl, p := openLongTile(t, m, "long.txt", numbered("old", 2000))
	if err := os.WriteFile(p, []byte(numbered("new", 30)), 0o644); err != nil {
		t.Fatal(err)
	}
	m.applyChange(dirChange{dir: m.root.path(), changed: []string{"long.txt"}})
	wantNumbered(t, m, tl, "new", 30)
}

// その場で大きく書き直した (inode は同じ・大きさは増える) ときは、追記と取り違えずに開き直す。先頭は同じにして、
// 読んだ所の直前の比べだけで見分ける形にする。
func TestLiveTileReopensGrownRewrite(t *testing.T) {
	m := newTest(t)
	head := strings.Repeat("h", 2*sigBytes) + "\n"
	tl, p := openLongTile(t, m, "long.txt", head+numbered("old", 2000))
	if err := os.WriteFile(p, []byte(head+numbered("new", 3000)), 0o644); err != nil {
		t.Fatal(err)
	}
	m.applyChange(dirChange{dir: m.root.path(), changed: []string{"long.txt"}})
	m.HandleKey("G")
	m.HandleKey("G")
	ls := tl.src.lines // 表示の行ではなく本文の行で見る (長い先頭の行は折り返す)
	if len(ls) != 3001 || ls[1] != "new 0" || ls[3000] != "new 2999" {
		t.Fatalf("書き直した後: 行の数 %d・2 行目 %q (new 0 から 3000 行のはず)", len(ls), ls[min(1, len(ls)-1)])
	}
}

// 先頭だけを同じ長さで書き換えて末尾に足した (読んだ所の直前は変わらない) ときも、先頭の比べで見分けて開き直す。
func TestLiveTileReopensHeadRewrite(t *testing.T) {
	m := newTest(t)
	body := numbered("old", 2000)
	tl, p := openLongTile(t, m, "long.txt", "aaaa\n"+body)
	if err := os.WriteFile(p, []byte("bbbb\n"+body+"extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.applyChange(dirChange{dir: m.root.path(), changed: []string{"long.txt"}})
	if got := firstPlain(m.view(tl)); got != "bbbb" {
		t.Fatalf("先頭を書き換えた後の 1 行目 = %q (bbbb のはず)", got)
	}
}

// 末尾に足されただけ (書き込みの続くログ) なら、開き直さずに続きを読み足す。改行の無い末尾を最後の行として出していたら、
// 続きと 1 行に繋ぐ。
func TestLiveTileFollowsAppendWithoutReopening(t *testing.T) {
	m := newTest(t)
	tl, p := openLongTile(t, m, "app.log", "line 0\nline 1\nline 2\nhalf")
	src := tl.src
	if !src.eof {
		t.Fatal("前提: 小さいファイルを末尾まで読んでいない")
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(" done\nline 4\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	m.applyChange(dirChange{dir: m.root.path(), changed: []string{"app.log"}})
	if tl.src != src {
		t.Fatal("末尾に足されただけなのに開き直した (読んだ所までを毎回読み直す)")
	}
	m.HandleKey("G")
	var got []string
	for _, l := range m.view(tl) {
		got = append(got, l.plain)
	}
	if want := "line 0|line 1|line 2|half done|line 4"; strings.Join(got, "|") != want {
		t.Fatalf("追記の後の行 = %q, want %q", strings.Join(got, "|"), want)
	}
}

// ジャンプ中に書き換わったら、抜けるまで開き直さない (選んでいるリンクとジャンプを黙って捨てない)。抜けたら開き直す。
func TestLiveTileDefersReopenDuringJump(t *testing.T) {
	m := newTest(t)
	mustWrite(t, filepath.Join(m.root.abs, "first.txt"), "x\n")
	tl, p := openLongTile(t, m, "note.txt", "see first.txt\n"+numbered("old", 50))
	m.HandleKey("k")
	m.HandleKey("tab")
	if !tl.jump {
		t.Fatal("前提: first.txt のリンクでジャンプに入らない")
	}
	replaceFile(t, p, numbered("new", 40))
	m.applyChange(dirChange{dir: m.root.path(), changed: []string{"note.txt"}})
	if !tl.jump {
		t.Fatal("ジャンプ中に書き換わったら、ジャンプを抜けた (次の enter がタイルを閉じるキーに化ける)")
	}
	if got := firstPlain(m.view(tl)); got != "see first.txt" {
		t.Fatalf("ジャンプ中に本文を差し替えた (1 行目 %q)", got)
	}
	m.HandleKey("esc")
	if tl.jump {
		t.Fatal("前提: esc でジャンプを抜けない")
	}
	wantNumbered(t, m, tl, "new", 40)
}

// tileWidthAt は端末の幅 w での、1 枚目のタイルの本文の幅 (行の少ないファイル)。
func tileWidthAt(m *Model, w int) int {
	m.Resize(w, 40)
	return m.tileTextWidth(&tile{src: &textSource{lines: []string{"x"}, eof: true}})
}

// linkNote は、幅 narrow で折り返すと first.txt が行をまたいで切れ、広げると 1 つのリンクに戻る行を持つ note.txt を作る。
func linkNote(t *testing.T, m *Model, narrow int) {
	t.Helper()
	mustWrite(t, filepath.Join(m.root.abs, "first.txt"), "x\n")
	mustWrite(t, filepath.Join(m.root.abs, "later.txt"), "x\n")
	mustWrite(t, filepath.Join(m.root.abs, "note.txt"), strings.Repeat("x", narrow-3)+" first.txt later.txt\n")
	m.Refresh()
	settle(m)
	cdTo(t, m, "note.txt")
}

func selectedBase(m *Model, tl *tile) string {
	ls := m.tileLinks(tl, len(m.view(tl)))
	if len(ls) == 0 {
		return ""
	}
	return filepath.Base(ls[max(0, min(tl.sel, len(ls)-1))].path)
}

// ジャンプ中に端末の幅を変えて折り返しが変わっても、選んでいるリンクは同じパスのまま (添字のまま別のリンクへ移らない)。
func TestJumpKeepsSelectionAcrossRewrap(t *testing.T) {
	m := newTest(t)
	narrow := tileWidthAt(m, 100)
	linkNote(t, m, narrow)
	m.HandleKey("enter")
	tl := m.frontTile()
	m.HandleKey("tab")
	if !tl.jump || selectedBase(m, tl) != "later.txt" {
		t.Fatalf("前提: 狭い幅で later.txt を選んでいない (jump=%v 選択=%q)", tl.jump, selectedBase(m, tl))
	}
	m.Resize(220, 40)
	if n := len(m.tileLinks(tl, len(m.view(tl)))); n != 2 {
		t.Fatalf("前提: 広げた後のリンクの数 = %d (first.txt が 1 つのリンクに戻るはず)", n)
	}
	if got := selectedBase(m, tl); got != "later.txt" {
		t.Fatalf("幅を変えた後の選択 = %q (選んでいた later.txt のはず)", got)
	}
}

// 作り直した並びに選んでいたリンクが無くなったら、先頭へ寄せて知らせる (黙って別のリンクへ移さない)。
func TestJumpTellsWhenSelectionGoneAfterRewrap(t *testing.T) {
	m := newTest(t)
	narrow := tileWidthAt(m, 100)
	linkNote(t, m, narrow)
	m.Resize(220, 40)
	m.HandleKey("enter")
	tl := m.frontTile()
	m.HandleKey("tab")
	if !tl.jump || selectedBase(m, tl) != "first.txt" {
		t.Fatalf("前提: 広い幅で first.txt を選んでいない (jump=%v 選択=%q)", tl.jump, selectedBase(m, tl))
	}
	m.TakeNotices()
	m.Resize(100, 40)
	if got := selectedBase(m, tl); got != "later.txt" {
		t.Fatalf("選んでいたリンクが消えた後の選択 = %q (先頭の later.txt のはず)", got)
	}
	told := false
	for _, n := range m.TakeNotices() {
		told = told || strings.Contains(n.Text, "選んでいたパス")
	}
	if !told {
		t.Fatal("選んでいたリンクが作り直しで消えたのに知らせなかった")
	}
}

// replaceFile は p を、tmp に書いて名前を変える保存 (inode が変わる) で body に置き換える。
func replaceFile(t *testing.T, p, body string) {
	t.Helper()
	tmp := p + ".new"
	mustWrite(t, tmp, body)
	if err := os.Rename(tmp, p); err != nil {
		t.Fatal(err)
	}
}

// 名前を変えて置き換えた保存は、先頭と読んだ所の直前が同じでも (途中の 1 行だけ違う) 追記と取り違えない (inode で見分ける)。
func TestLiveTileReopensReplacedFileWithSameEdges(t *testing.T) {
	m := newTest(t)
	head := strings.Repeat("h", 2*sigBytes) + "\n"
	tl, p := openLongTile(t, m, "long.txt", head+numbered("old", 2000))
	replaceFile(t, p, head+strings.Replace(numbered("old", 2000), "old 5\n", "OLD 5\n", 1))
	m.applyChange(dirChange{dir: m.root.path(), changed: []string{"long.txt"}})
	if got := tl.src.lines[6]; got != "OLD 5" {
		t.Fatalf("置き換えた後の 7 行目 = %q (OLD 5 のはず)", got)
	}
}

// 追記と取り違えた書き換え (同じ inode・同じ長さで途中だけ変える) も、エディタ・シェルから戻ったときには開き直して拾う
// (追記として読み足したときに更新時刻を進めると、ExecDone が変わっていないと読んで古い本文を残す)。
func TestLiveTileMissedRewriteRecoversOnExecDone(t *testing.T) {
	clearEditorEnv(t)
	m := newTest(t)
	head := strings.Repeat("h", 2*sigBytes) + "\n"
	tl, p := openLongTile(t, m, "long.txt", head+numbered("old", 2000))
	if err := os.WriteFile(p, []byte(head+strings.Replace(numbered("old", 2000), "old 5\n", "OLD 5\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	m.applyChange(dirChange{dir: m.root.path(), changed: []string{"long.txt"}})
	if got := tl.src.lines[6]; got != "old 5" {
		t.Fatalf("前提: 同じ長さの途中の書き換えを追記と見ていない (7 行目 %q)", got)
	}
	takeEdit(t, m, "e")
	m.ExecDone(nil)
	if got := tl.src.lines[6]; got != "OLD 5" {
		t.Fatalf("エディタから戻った後の 7 行目 = %q (開き直して OLD 5 のはず)", got)
	}
}

// Markdown のタイルで、改行の無い末尾の行に足されたら、繋いだ行を出す (行の数が変わらないので、作り直させないと古い行が残る)。
func TestLiveTileMarkdownJoinsAppendedLastLine(t *testing.T) {
	m := newTest(t)
	tl, p := openLongTile(t, m, "x.md", "# a\n\nfoo")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("bar"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	m.applyChange(dirChange{dir: m.root.path(), changed: []string{"x.md"}})
	var all []string
	for _, l := range m.view(tl) {
		all = append(all, l.plain)
	}
	if !strings.Contains(strings.Join(all, "|"), "foobar") {
		t.Fatalf("追記の後の Markdown の行 = %q (foobar を含むはず)", strings.Join(all, "|"))
	}
}

// diff のタイルは、ライブ更新で書き換わっても diff を取り直さない (書き込みの続くファイルで、変わるたびに先頭へ戻して取り直さない)。
// 本文は d で本体へ戻すときに開き直す (古い本文を出さない)。
func TestLiveTileDiffKeepsDiffAndRereadsOnToggle(t *testing.T) {
	m := newTest(t)
	tl, p := openLongTile(t, m, "long.txt", numbered("old", 50))
	tl.setMode(modeDiff)
	job := tl.diff
	replaceFile(t, p, numbered("new", 50))
	m.applyChange(dirChange{dir: m.root.path(), changed: []string{"long.txt"}})
	if tl.mode != modeDiff || tl.diff != job {
		t.Fatalf("ライブ更新で diff を取り直した (mode=%v・同じ取得=%v)", tl.mode, tl.diff == job)
	}
	m.toggleDiff(tl)
	if got := firstPlain(m.view(tl)); tl.mode != modeFile || got != "new 0" {
		t.Fatalf("d で本体へ戻した後: mode=%v・1 行目 %q (開き直して new 0 のはず)", tl.mode, got)
	}
}

// 読めなくなったファイルに書き手が書き続けても、同じ理由を変わるたびに知らせ直さない。
func TestLiveTileTellsUnreadableOnce(t *testing.T) {
	m := newTest(t)
	tl, p := openLongTile(t, m, "long.txt", numbered("old", 50))
	m.TakeNotices()
	count := func() int {
		n := 0
		for _, x := range m.TakeNotices() {
			if strings.Contains(x.Text, "long.txt") {
				n++
			}
		}
		return n
	}
	for i := range 3 {
		replaceFile(t, p, numbered(fmt.Sprintf("v%d", i), 50))
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
		m.applyChange(dirChange{dir: m.root.path(), changed: []string{"long.txt"}})
		if n := count(); n != map[bool]int{true: 1, false: 0}[i == 0] {
			t.Fatalf("%d 回目の変化で知らせが %d 件 (最初の 1 回だけのはず)", i+1, n)
		}
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	m.applyChange(dirChange{dir: m.root.path(), changed: []string{"long.txt"}})
	if got := tl.src.lines[0]; got != "v2 0" {
		t.Fatalf("読めるようになった後の 1 行目 = %q (v2 0 のはず)", got)
	}
}

// 同じファイルが読めなくなり、読めるようになって追記され (読み足しで回復)、また読めなくなったら、もう一度知らせる。
func TestLiveTileTellsAgainAfterAppendRecovery(t *testing.T) {
	m := newTest(t)
	_, p := openLongTile(t, m, "app.log", numbered("line", 5))
	m.TakeNotices()
	told := func() bool {
		for _, x := range m.TakeNotices() {
			if strings.Contains(x.Text, "app.log") {
				return true
			}
		}
		return false
	}
	appendAndChange := func(mode os.FileMode, text string) {
		t.Helper()
		if err := os.Chmod(p, 0o644); err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(text); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		m.applyChange(dirChange{dir: m.root.path(), changed: []string{"app.log"}})
	}
	appendAndChange(0, "a\n")
	if !told() {
		t.Fatal("前提: 読めなくなったのを知らせない")
	}
	appendAndChange(0o644, "b\n")
	appendAndChange(0, "c\n")
	if !told() {
		t.Fatal("読み足しで回復した後に、また読めなくなったのを知らせない")
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
}

// 作り直した並びに選べるリンクが 1 つも無くなったら、ジャンプを抜けて知らせる (選べないものを選んだとは言わない)。
func TestJumpLeavesWhenAllLinksGoneAfterRewrap(t *testing.T) {
	m := newTest(t)
	narrow := tileWidthAt(m, 100)
	mustWrite(t, filepath.Join(m.root.abs, "first.txt"), "x\n")
	mustWrite(t, filepath.Join(m.root.abs, "note.txt"), strings.Repeat("x", narrow-3)+" first.txt\n")
	m.Refresh()
	settle(m)
	cdTo(t, m, "note.txt")
	m.Resize(220, 40)
	m.HandleKey("enter")
	tl := m.frontTile()
	m.HandleKey("tab")
	if !tl.jump {
		t.Fatal("前提: 広い幅で first.txt のリンクでジャンプに入らない")
	}
	m.TakeNotices()
	m.Resize(100, 40)
	m.tileLinks(tl, len(m.view(tl)))
	if tl.jump {
		t.Fatal("選べるリンクが無くなった後もジャンプのまま")
	}
	told := false
	for _, n := range m.TakeNotices() {
		told = told || strings.Contains(n.Text, "選べるパスがなくなりました")
	}
	if !told {
		t.Fatal("選べるリンクが無くなったのに知らせなかった")
	}
}

// 選んでいたリンクを見失った打鍵は捨てる: ジャンプを抜けた後の enter がタイルを閉じるキーに化けない・
// 寄せた先頭のリンクを知らせを見る前に開かない。見失うのは描画 (drawTile) が先のことも、打鍵 (jumpKey) が先のこともある
// (bubbletea は大きさの変更の後に描く)。
func TestJumpSwallowsKeyThatLostSelection(t *testing.T) {
	for _, draw := range []bool{true, false} {
		m := newTest(t)
		narrow := tileWidthAt(m, 100)
		mustWrite(t, filepath.Join(m.root.abs, "first.txt"), "x\n")
		mustWrite(t, filepath.Join(m.root.abs, "note.txt"), strings.Repeat("x", narrow-3)+" first.txt\n")
		m.Refresh()
		settle(m)
		cdTo(t, m, "note.txt")
		m.Resize(220, 40)
		m.HandleKey("enter")
		settle(m) // タイルを開き切る (開く途中は描画が本文とリンクを描かない)
		tl := m.frontTile()
		m.HandleKey("tab")
		m.Resize(100, 40)
		if draw {
			m.View()
		}
		m.HandleKey("enter")
		if tl.closing || len(m.tiles) != 1 {
			t.Fatalf("描画=%v: リンクが無くなった後の enter がタイルのキーとして効いた (閉じる=%v・タイル %d 枚)", draw, tl.closing, len(m.tiles))
		}

		m2 := newTest(t)
		linkNote(t, m2, tileWidthAt(m2, 100))
		m2.Resize(220, 40)
		m2.HandleKey("enter")
		settle(m2)
		tl2 := m2.frontTile()
		m2.HandleKey("tab")
		m2.Resize(100, 40)
		if draw {
			m2.View()
		}
		m2.HandleKey("enter")
		if len(m2.tiles) != 1 || !tl2.jump {
			t.Fatalf("描画=%v: 選んでいたリンクを見失った enter で先頭のリンクを開いた (タイル %d 枚・ジャンプ=%v)", draw, len(m2.tiles), tl2.jump)
		}
	}
}

// diff を出している間に変わって消えたファイルは、d で本体へ戻すたびに開けない理由を知らせる (利用者の操作なので、同じ理由でも黙らない)。
func TestLiveTileDiffToggleTellsEveryTime(t *testing.T) {
	m := newTest(t)
	tl, p := openLongTile(t, m, "long.txt", numbered("old", 50))
	tl.setMode(modeDiff)
	replaceFile(t, p, numbered("new", 50))
	m.applyChange(dirChange{dir: m.root.path(), changed: []string{"long.txt"}})
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	m.TakeNotices()
	for i := range 2 {
		m.toggleDiff(tl) // 本体へ
		told := false
		for _, n := range m.TakeNotices() {
			told = told || strings.Contains(n.Text, "long.txt")
		}
		if !told {
			t.Fatalf("%d 回目に本体へ戻したとき、開けない理由を知らせない", i+1)
		}
		tl.setMode(modeDiff) // diff へ (消えたファイルは diffable でないので toggleDiff では戻れない)
	}
}

// 作り直しで先頭へ寄せた選択が画面の外なら、見える位置までスクロールする (見えないリンクを次の enter で開かない)。
func TestJumpScrollsToRelocatedSelection(t *testing.T) {
	m := newTest(t)
	narrow := tileWidthAt(m, 100)
	mustWrite(t, filepath.Join(m.root.abs, "first.txt"), "x\n")
	mustWrite(t, filepath.Join(m.root.abs, "later.txt"), "x\n")
	body := "see later.txt\n" + numbered("filler", 200) + strings.Repeat("x", narrow-3) + " first.txt\n"
	mustWrite(t, filepath.Join(m.root.abs, "note.txt"), body)
	m.Refresh()
	settle(m)
	cdTo(t, m, "note.txt")
	m.Resize(220, 40)
	m.HandleKey("enter")
	settle(m)
	tl := m.frontTile()
	m.HandleKey("G")
	m.HandleKey("tab")
	if !tl.jump || selectedBase(m, tl) != "first.txt" || tl.scroll == 0 {
		t.Fatalf("前提: 末尾で first.txt を選んでいない (jump=%v 選択=%q scroll=%d)", tl.jump, selectedBase(m, tl), tl.scroll)
	}
	m.Resize(100, 40)
	m.View()
	if got := selectedBase(m, tl); got != "later.txt" {
		t.Fatalf("前提: 見失った後に先頭の later.txt へ寄せていない (%q)", got)
	}
	if tl.scroll != 0 {
		t.Fatalf("先頭へ寄せた選択 (0 行目) が画面の外のまま (scroll=%d)", tl.scroll)
	}
}
