package filer

import (
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
