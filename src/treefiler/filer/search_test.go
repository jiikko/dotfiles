package filer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFuzzyRanks(t *testing.T) {
	for _, c := range []struct {
		name, q string
		want    int
	}{
		{"flow-rendering", "flow", rankPrefix},
		{"my-flow", "flow", rankWordStart},
		{"myFlow", "flow", rankWordStart}, // camelCase の小文字の直後の大文字は語の先頭 (spec §6.2)
		{"overflow", "flow", rankSubstring},
		{"flow-rendering", "frend", rankLoose},
		{"abc", "zz", rankNone},
		{"README.md", "RE", rankPrefix},
		{"readme.md", "RE", rankNone}, // 大文字を含む query は区別する
	} {
		got, _ := fuzzy(c.name, c.q)
		if got != c.want {
			t.Errorf("fuzzy(%q, %q) = %d, want %d", c.name, c.q, got, c.want)
		}
	}
}

func typeText(m *Model, s string) {
	for _, r := range s {
		m.HandleInput(string(r), string(r))
	}
}

func TestSearchJumpsCancelsAndRepeats(t *testing.T) {
	m := newTest(t)
	origin := m.cur
	m.HandleKey("/")
	if !m.OwnsKeys() {
		t.Fatal("検索中なのに OwnsKeys が false (glogx の横断キーに取られる)")
	}
	typeText(m, "file")
	if m.cur.raw != "file2.txt" {
		t.Fatalf("打つとカーソルが最良の一致へ飛ばない: %q", m.cur.raw)
	}
	if _, _, ok := m.CaretPos(); !ok {
		t.Fatal("検索中なのに入力欄のキャレットが無い")
	}
	m.HandleKey("esc")
	if m.cur != origin || m.OwnsKeys() {
		t.Fatal("Esc で元のカーソルへ戻らない / 入力が閉じない")
	}
	m.HandleKey("/")
	typeText(m, "file")
	m.HandleKey("enter")
	if m.cur.raw != "file2.txt" || m.OwnsKeys() {
		t.Fatal("Enter で一致に留まらない")
	}
	m.HandleKey("n")
	if m.cur.raw != "file10.txt" {
		t.Fatalf("n で次の一致へ行かない: %q", m.cur.raw)
	}
	m.HandleKey("N")
	if m.cur.raw != "file2.txt" {
		t.Fatalf("N で前の一致へ戻らない: %q", m.cur.raw)
	}
	m.HandleKey("/")
	if m.HandleKey("ctrl+c") == Quit {
		t.Fatal("検索中の ctrl+c で終了した (検索の取り消しのはず)")
	}
	if m.OwnsKeys() {
		t.Fatal("検索中の ctrl+c で検索が閉じない")
	}
}

func TestHelpClosesOnAnyKeyWithoutActing(t *testing.T) {
	m := newTest(t)
	m.HandleKey("?")
	before := m.cur
	m.HandleKey("j")
	if m.help || m.cur != before {
		t.Fatal("キー一覧が閉じない / 閉じたキーで動いた")
	}
}

func TestExplodeOpensDescendantsButNotHidden(t *testing.T) {
	m := newTest(t)
	b := m.kids(m.root)[1]
	m.setCur(b)
	m.HandleKey("e")
	waitExplode(t, m)
	deep := m.findNode(b.path() + "/deep")
	if deep == nil || !deep.expanded || !b.expanded {
		t.Fatal("explode で配下のフォルダが開かない")
	}
	ns := m.TakeNotices()
	if len(ns) == 0 || !ns[len(ns)-1].OK {
		t.Fatalf("explode の結果の知らせが無い: %+v", ns)
	}
}

// 小文字にすると長さが変わる文字を含む名前でも落ちず、一致の範囲は元の名前のバイトを指す (レビューで panic を再現 2026-10-09)。
func TestFuzzyKeepsOriginalOffsets(t *testing.T) {
	r, spans := fuzzy("Ⱥab", "ab")
	if r == rankNone || len(spans) != 1 || "Ⱥab"[spans[0][0]:spans[0][1]] != "ab" {
		t.Fatalf("fuzzy(Ⱥab, ab) = %d %v", r, spans)
	}
	m := newTest(t)
	p := m.root.path() + "/Ⱥab.txt"
	if err := writeFile(p); err != nil {
		t.Fatal(err)
	}
	m.root.reload()
	m.HandleKey("/")
	typeText(m, "ab")
	m.draw() // 一致を色付けする描画で落ちない
}

// 検索中に元の項目が消えても、Esc でカーソルが木の外へ出ない。
func TestSearchOriginRemovedStaysInTree(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "c.txt")
	m.HandleKey("/")
	typeText(m, "file")
	if err := removeFile(m.root.path() + "/c.txt"); err != nil {
		t.Fatal(err)
	}
	m.Refresh()
	m.HandleKey("esc")
	if !m.inTree(m.cur) {
		t.Fatal("消えた元の項目へ戻り、カーソルが木の外へ出た")
	}
}

func TestBlankCommandDoesNotRun(t *testing.T) {
	m := newTest(t)
	m.HandleKey("!")
	typeText(m, "   ")
	if m.HandleKey("enter") == Exec {
		t.Fatal("空白だけのコマンドを走らせた")
	}
}

// searchFromRemoved は rel にカーソルを置いて検索を始め (before があれば打ってから)、rel を含む gone (dir からの相対) を
// ディスクから消してライブ更新を当てる (検索中に origin が消える)。
func searchFromRemoved(t *testing.T, rel, dir, gone string, before func(m *Model)) *Model {
	t.Helper()
	m := newTest(t)
	origin := m.loadPath(filepath.Join(m.root.path(), rel))
	if origin == nil {
		t.Fatalf("前提: %s を引けない", rel)
	}
	m.setCur(origin)
	m.HandleKey("/")
	if before != nil {
		before(m)
	}
	abs := filepath.Join(m.root.path(), dir)
	if err := os.RemoveAll(filepath.Join(abs, gone)); err != nil {
		t.Fatal(err)
	}
	m.applyChange(dirChange{dir: abs, listing: true, removed: []string{gone}})
	if m.inTree(m.search.origin) {
		t.Fatal("前提: origin が木から外れていない")
	}
	return m
}

// 検索中に origin が消えたら、戻る経路 (一致なし・空欄の backspace・Esc) はどれも生きているいちばん近い祖先へ戻す
// (消えた origin へ戻すと、カーソルが木の外を指し、カメラが原点へ寄る)
func TestSearchRestoresToLiveAncestorWhenOriginRemoved(t *testing.T) {
	for _, c := range []struct {
		name string
		keys func(m *Model)
	}{
		{"一致なし", func(m *Model) { typeText(m, "zzzq") }},
		{"一致なしの後に Esc", func(m *Model) { typeText(m, "zzzq"); m.HandleInput("esc", "") }},
		{"空欄の backspace", func(m *Model) { m.HandleInput("backspace", "") }},
		{"Esc", func(m *Model) { m.HandleInput("esc", "") }},
	} {
		t.Run(c.name+"/ファイルが消える", func(t *testing.T) {
			m := searchFromRemoved(t, "c.txt", ".", "c.txt", nil)
			c.keys(m)
			if m.cur != m.root {
				t.Errorf("カーソルが %q (木の中=%v)。root へ戻るはず", m.cur.raw, m.inTree(m.cur))
			}
		})
		t.Run(c.name+"/親のフォルダごと消える", func(t *testing.T) {
			m := searchFromRemoved(t, "b/deep/x.txt", "b", "deep", nil)
			c.keys(m)
			if want := m.findNode(filepath.Join(m.root.path(), "b")); m.cur != want {
				t.Errorf("カーソルが %q (木の中=%v)。生きている祖先の b へ戻るはず", m.cur.raw, m.inTree(m.cur))
			}
		})
	}
}

// 一致へ移った後に origin が消えても、一致が無くなれば「始めた位置」の側 (生きている祖先) へ戻る。一致に留まらない
// (「消えていれば今の位置に留まる」と書くと、この場面で一致していた項目に残る)
func TestSearchNoMatchAfterJumpGoesToOriginAncestor(t *testing.T) {
	m := searchFromRemoved(t, "c.txt", ".", "c.txt", func(m *Model) {
		typeText(m, "file")
		if m.cur.raw != "file2.txt" {
			t.Fatalf("前提: 一致へ移っていない (%q)", m.cur.raw)
		}
	})
	typeText(m, "zzq")
	if len(m.search.matches) != 0 {
		t.Fatalf("前提: まだ一致がある (%d 件)", len(m.search.matches))
	}
	if m.cur != m.root {
		t.Errorf("カーソルが %q。消えた origin の生きている祖先 (root) へ戻るはず", m.cur.raw)
	}
}

// 打った字を消して空欄に戻したら、始めた位置へ戻る (検索は開いたまま)。空欄は列を作らずに返す。
func TestSearchBackToEmptyReturnsToOrigin(t *testing.T) {
	m := newTest(t)
	origin := m.cur
	m.HandleKey("/")
	typeText(m, "file")
	if m.cur == origin {
		t.Fatal("前提: 打つと一致へ飛ぶ")
	}
	for range 4 {
		m.HandleKey("backspace")
	}
	if !m.search.active || m.cur != origin || len(m.search.matches) != 0 {
		t.Fatalf("空欄に戻したのに 起点へ戻らない / 一致が残る (active=%v cur=%s)", m.search.active, m.cur.raw)
	}
}

// 空欄の ctrl+h は backspace と同じく検索を閉じる (文字があるうちは 1 文字消す)。
func TestSearchClosesOnEmptyCtrlH(t *testing.T) {
	m := newTest(t)
	m.HandleKey("/")
	m.HandleInput("x", "x")
	m.HandleInput("ctrl+h", "")
	if !m.search.active || !m.search.line.Empty() {
		t.Fatalf("文字があるうちの ctrl+h で閉じた / 消えない (active=%v)", m.search.active)
	}
	m.HandleInput("ctrl+h", "")
	if m.search.active {
		t.Fatal("空欄の ctrl+h で検索が閉じない")
	}
}
