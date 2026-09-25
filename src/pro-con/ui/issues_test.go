package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
	"pro-con/card"
)

// issueRepo は issues/ の下に、状態ごとのディレクトリと next/ の目印 (symlink) を持つ repo を作る。
func issueRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := []string{"issues/415-design.md", "issues/done/012-old.md", "issues/epic/pc/done/020-epic.md",
		"issues/030-dup.md", "issues/pending/030-dup-again.md", "issues/README.md"}
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "issues/next"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../415-design.md", filepath.Join(root, "issues/next/415-design.md")); err != nil {
		t.Fatal(err)
	}
	return root
}

// 状態ごとのディレクトリや epic の下に居ても番号で見つける。next/ の目印 (symlink) は飛ばして実体を返す。
func TestFindIssue(t *testing.T) {
	root := issueRepo(t)
	for n, want := range map[int]string{415: "issues/415-design.md", 12: "issues/done/012-old.md", 20: "issues/epic/pc/done/020-epic.md"} {
		got, err := findIssue(root, n)
		if err != nil || got != filepath.Join(root, want) {
			t.Fatalf("issue %03d: got %q (%v) want %q", n, got, err, want)
		}
	}
	if _, err := findIssue(root, 30); err == nil || !strings.Contains(err.Error(), "衝突") {
		t.Fatalf("番号の衝突は黙って選ばずエラーのはず: %v", err)
	}
	if _, err := findIssue(root, 999); err == nil || !strings.Contains(err.Error(), "見つからない") {
		t.Fatalf("無い番号はエラーのはず: %v", err)
	}
}

func issueModel(t *testing.T, issues []card.IssueRef) (*Model, *[]string, *[]string) {
	t.Helper()
	root := issueRepo(t)
	be := newSpy()
	be.snap.Cards = []card.Card{{ID: "C-001", Title: "設計", State: card.Running, Repo: "dotfiles", Issues: issues}}
	m := New(be, []backend.Repo{{Name: "dotfiles", Path: root}})
	var copied, opened []string
	m.copy = func(s string) error { copied = append(copied, s); return nil }
	m.openEditor = func(p string) *exec.Cmd { opened = append(opened, p); return exec.Command("true") }
	return m, &copied, &opened
}

// y は issue のファイルのパス (素の値) をコピーし、e はそのファイルをエディタで開く。
func TestYankPathAndOpenIssue(t *testing.T) {
	m, copied, opened := issueModel(t, []card.IssueRef{{Repo: "dotfiles", Number: 415}})
	press(m, "y")
	if len(*copied) != 1 || !strings.HasSuffix((*copied)[0], "/issues/415-design.md") {
		t.Fatalf("issue のパスをコピーするはず: %v", *copied)
	}
	if cmd := press(m, "e"); cmd == nil {
		t.Fatal("e でエディタを開くコマンドが返らない")
	}
	if len(*opened) != 1 || (*opened)[0] != (*copied)[0] {
		t.Fatalf("同じファイルを開くはず: opened=%v copied=%v", *opened, *copied)
	}
}

// issue に紐づかないカードでは y も e も何もせず、理由を出す (Y でタイトルと内容をコピーできる、も案内する)。
func TestIssueKeysOnCardWithoutIssue(t *testing.T) {
	m, copied, opened := issueModel(t, nil)
	press(m, "y")
	if len(*copied) != 0 || !strings.Contains(m.toasts.Text(), "紐づいていない") || !strings.Contains(m.toasts.Text(), "Y") {
		t.Fatalf("コピーせず理由と Y を案内するはず: copied=%v flash=%q", *copied, m.toasts.Text())
	}
	if cmd := press(m, "e"); cmd != nil || len(*opened) != 0 {
		t.Fatal("issue の無いカードでエディタを開いた")
	}
}

// issue に紐づくカードは 1 行目に番号を出す (複数なら残りの数も)。
func TestCardShowsIssueNumber(t *testing.T) {
	m, _, _ := issueModel(t, []card.IssueRef{{Repo: "dotfiles", Number: 415}, {Repo: "dotfiles", Number: 12}})
	if out := ansi.Strip(m.render()); !strings.Contains(out, "C-001 #415+1") {
		t.Fatalf("カードの 1 行目に issue 番号が無い:\n%s", out)
	}
}
