package ui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"pro-con/backend"
	"pro-con/card"
)

// pickerRepo は open / next の目印 / pending / done と、epic (親 + 未完了の子 + 完了した子) を持つ repo。
func pickerRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"issues/100-bug-a.md":                   "# 100 (bug): タイトル A\n",
		"issues/pending/101-feat-b.md":          "# 101 (feat): タイトル B\n",
		"issues/done/102-feat-c.md":             "# 102 (feat): 終わったもの\n",
		"issues/epic/200/200-design-epic.md":    "# 200 (design): epic の親\n",
		"issues/epic/200/201-feat-child.md":     "# 201 (feat): 子 1\n",
		"issues/epic/200/done/202-feat-gone.md": "# 202 (feat): 終わった子\n",
	}
	for f, body := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "issues/next"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../100-bug-a.md", filepath.Join(root, "issues/next/100-bug-a.md")); err != nil {
		t.Fatal(err)
	}
	return root
}

func pickerModel(t *testing.T) (*Model, *spy, string) {
	t.Helper()
	root := pickerRepo(t)
	be := newSpy()
	be.snap.Cards = []card.Card{{ID: "C-001", State: card.Running, Repo: "dotfiles", Since: be.snap.Now}}
	m := New(be, []backend.Repo{{Name: "dotfiles", Path: root}})
	m.Update(keyTab()) // dotfiles のタブ
	return m, be, root
}

func rowLabels(m *Model) []string {
	var out []string
	for _, r := range m.picker.rows {
		switch {
		case r.head != "":
			out = append(out, "repo:"+r.head)
		case r.epic != "":
			out = append(out, "epic:"+r.epic)
		default:
			out = append(out, r.iss.Number)
		}
	}
	return out
}

// 完了していない issue だけを並べる。epic は見出し (親 issue) の下に未完了の子。done の issue と done の子は出さない。
func TestPickerListsOpenIssuesWithEpics(t *testing.T) {
	m, _, root := pickerModel(t)
	press(m, "i")
	// epic は見出しの直後に子が続く。epic の外の issue の並びは glogx の issues viewer と同じ (glogx/issues.Scan の順)
	got := rowLabels(m)
	if len(got) != 4 || got[0] != "epic:200" || got[1] != "201" || !slices.Contains(got, "100") || !slices.Contains(got, "101") {
		t.Fatalf("行が違う: %v (want epic:200, 201, と 100 / 101)", got)
	}
	head := m.picker.rows[0]
	if head.iss == nil || head.iss.Number != "200" || len(head.kids) != 1 || head.kids[0] != filepath.Join(root, "issues/epic/200/201-feat-child.md") {
		t.Fatalf("epic の親と未完了の子が違う: %+v", head)
	}
}

// issue を選んで Enter → 補足 → Enter で、その issue に紐づく依頼が届く。パスは next/ の目印ではなく実体。
func TestPickIssueSendsLinkedRequest(t *testing.T) {
	m, be, root := pickerModel(t)
	press(m, "i")
	for m.picker.rows[m.picker.cursor].iss == nil || m.picker.rows[m.picker.cursor].iss.Number != "100" {
		before := m.picker.cursor
		press(m, "j")
		if m.picker.cursor == before {
			t.Fatal("100 の行に届かない")
		}
	}
	press(m, "enter")
	if m.mode != modeInput || m.inputKind != inputIssue {
		t.Fatal("補足の入力欄が開かない")
	}
	typeText(m, "急ぎで")
	press(m, "enter", "y")
	r, ok := be.applied[len(be.applied)-1].(backend.NewRequest)
	if !ok || r.Issue == nil {
		t.Fatalf("issue 付きの依頼が届いていない: %#v", be.applied)
	}
	if r.Issue.Number != 100 || r.Issue.Path != filepath.Join(root, "issues/100-bug-a.md") || r.Text != "急ぎで" || r.Repo.Name != "dotfiles" {
		t.Fatalf("届いた依頼が違う: %+v / %+v", r, *r.Issue)
	}
}

// epic の見出しを選ぶと、未完了の子の一覧が付いた依頼になる。補足は空でよい。
func TestPickEpicSendsChildren(t *testing.T) {
	m, be, _ := pickerModel(t)
	press(m, "i", "enter")
	press(m, "enter", "y") // 補足なし
	r, ok := be.applied[len(be.applied)-1].(backend.NewRequest)
	if !ok || r.Issue == nil || r.Issue.Epic != "200" || r.Issue.Number != 200 || len(r.Issue.Children) != 1 {
		t.Fatalf("epic の依頼が違う: %#v", be.applied)
	}
}

// global のタブでは repo の見出しが出て、カーソルは見出しの行に止まらない。
func TestPickerGlobalHasRepoHeadings(t *testing.T) {
	root := pickerRepo(t)
	be := newSpy()
	m := New(be, []backend.Repo{{Name: "dotfiles", Path: root}})
	press(m, "i")
	if got := rowLabels(m); len(got) == 0 || got[0] != "repo:dotfiles" {
		t.Fatalf("global の先頭に repo の見出しが無い: %v", got)
	}
	if m.picker.cursor != 1 {
		t.Fatalf("カーソルが見出しの行に居る: %d", m.picker.cursor)
	}
	press(m, "k")
	if m.picker.cursor != 1 {
		t.Fatalf("k で見出しの行へ上がった: %d", m.picker.cursor)
	}
}

// i / q / esc で閉じる (ボードへ戻る)。
func TestPickerCloses(t *testing.T) {
	for _, k := range []string{"i", "q", "esc"} {
		m, _, _ := pickerModel(t)
		press(m, "i")
		press(m, k)
		if m.picker.open {
			t.Fatalf("%s で閉じない", k)
		}
	}
}
