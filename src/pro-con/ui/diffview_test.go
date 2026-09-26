package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/card"
)

const sampleDiff = `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,2 +1,3 @@
 package a
+var x = 1
-var y = 2
diff --git a/b.md b/b.md
index 3..4 100644
--- a/b.md
+++ b/b.md
@@ -1 +1 @@
-古い
+新しい`

// diffModel は差分の本文を置いたカードの詳細を開いた画面。
func diffModel(t *testing.T) *Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "C-011.diff")
	if err := os.WriteFile(path, []byte(sampleDiff), 0o600); err != nil {
		t.Fatal(err)
	}
	be := newSpy()
	be.snap.Cards = []card.Card{{ID: "C-011", State: card.Running, Since: be.snap.Now, Progress: &card.Progress{Worktree: "/r/.claude/worktrees/pc-c-011",
		Base: "origin/master", Diff: &card.DiffSummary{Path: path, Files: 2, Add: 2, Del: 2}}},
		{ID: "C-012", State: card.Running, Since: be.snap.Now}}
	m := New(be, nil)
	m.width, m.height = 100, 30
	m.selected = "C-011"
	m.openDrawer()
	return m
}

// screenText は画面の文字 (色を落とす)。
func screenText(m *Model) string { return ansi.Strip(m.render()) }

// 詳細の D で差分の板を開く: 本文は置き場のファイルを裏で読み、ファイルごとの見出し (名前・足した / 消した行) の下に hunk を出す
// (diff --git / index / --- / +++ の行は見出しに畳む)。enter で上端のファイルを畳み、J で次のファイルへ、z で全部を畳む / 開く、D で閉じる。
func TestDiffBoardOpensFoldsAndCloses(t *testing.T) {
	m := diffModel(t)
	cmd := press(m, "D")
	if !m.diff.open || cmd == nil {
		t.Fatal("D で差分の板を開かない")
	}
	if !strings.Contains(screenText(m), "読んでいる…") {
		t.Fatalf("読み終える前に読んでいると出さない:\n%s", screenText(m))
	}
	m.Update(cmd())
	s := screenText(m)
	for _, want := range []string{"C-011 の差分", "2 ファイル +2 -2", "▾ a.go  +1 -1", "@@ -1,2 +1,3 @@", "+var x = 1", "-var y = 2", "▾ b.md  +1 -1", "+新しい"} {
		if !strings.Contains(s, want) {
			t.Fatalf("板に %q が無い:\n%s", want, s)
		}
	}
	if strings.Contains(s, "index 1..2") || strings.Contains(s, "+++ b/a.go") {
		t.Fatalf("ファイルの見出しの行を畳まない:\n%s", s)
	}
	press(m, "enter") // 選んでいるのは最初の a.go
	if s := screenText(m); !strings.Contains(s, "▸ a.go") || strings.Contains(s, "+var x = 1") || !strings.Contains(s, "+新しい") {
		t.Fatalf("enter で選んでいるファイルだけを畳まない:\n%s", s)
	}
	press(m, "J", "J", "enter") // 端で止まる (b.md のまま)
	if s := screenText(m); !strings.Contains(s, "▸ b.md") || strings.Contains(s, "+新しい") {
		t.Fatalf("J で次のファイルを選ばない:\n%s", s)
	}
	press(m, "K", "enter", "J", "enter")
	press(m, "z") // 開いている b.md があるので全部畳む
	if s := screenText(m); !strings.Contains(s, "▸ a.go") || !strings.Contains(s, "▸ b.md") || strings.Contains(s, "+新しい") {
		t.Fatalf("z で全部を畳まない:\n%s", s)
	}
	press(m, "z")
	if s := screenText(m); !strings.Contains(s, "+var x = 1") || !strings.Contains(s, "+新しい") {
		t.Fatalf("全部畳んだあとの z で全部を開かない:\n%s", s)
	}
	press(m, "D")
	if m.diff.open || !m.showDetail {
		t.Fatal("D で板だけを閉じて詳細へ戻らない")
	}
}

// 差分の無いカードでは開かずに断る。読んでいる間に閉じたら、届いた本文を捨てる。
func TestDiffBoardRefusesWithoutDiffAndDropsLateLoad(t *testing.T) {
	m := diffModel(t)
	cmd := press(m, "D")
	press(m, "q")
	m.Update(cmd())
	if m.diff.open || m.diff.lines != nil {
		t.Fatal("閉じた後に届いた本文で板を開き直した")
	}
	m.selected = "C-012"
	m.openDrawer()
	if press(m, "D"); m.diff.open {
		t.Fatal("差分の無いカードで板を開いた")
	}
}
