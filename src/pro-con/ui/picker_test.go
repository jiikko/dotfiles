package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

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

// linkedPicker は issue に紐づいたカードを持つ model (100 は作業中とレビュー、101 は片付けた完了だけ、epic の子 201 は着手待ち)。
func linkedPicker(t *testing.T) (*Model, *spy) {
	t.Helper()
	root := pickerRepo(t)
	be := newSpy()
	ref := func(n int) []card.IssueRef { return []card.IssueRef{{Repo: "dotfiles", Number: n, Status: "open"}} }
	be.snap.Cards = []card.Card{
		{ID: "C-001", State: card.Running, Repo: "dotfiles", Since: be.snap.Now}, // issue に紐づかない
		{ID: "C-002", State: card.Review, Repo: "dotfiles", Since: be.snap.Now, Issues: ref(100)},
		{ID: "C-003", State: card.Running, Repo: "dotfiles", Since: be.snap.Now, Issues: ref(100)},
		{ID: "C-004", State: card.Done, Repo: "dotfiles", Since: be.snap.Now, Issues: ref(101), Archived: true},
		{ID: "C-005", State: card.Planned, Repo: "dotfiles", Since: be.snap.Now, Issues: ref(201)},
		{ID: "C-006", State: card.Running, Repo: "other", Since: be.snap.Now, Issues: []card.IssueRef{{Repo: "other", Number: 101}}}, // 別 repo の同じ番号
	}
	m := New(be, []backend.Repo{{Name: "dotfiles", Path: root}})
	m.Update(keyTab())
	press(m, "i")
	return m, be
}

// pickerLine は一覧の板の中で needle を含む行 (装飾なし)。
func pickerLine(t *testing.T, m *Model, needle string) string {
	t.Helper()
	for _, l := range m.pickerBlock() {
		if s := ansi.Strip(l); strings.Contains(s, needle) {
			return s
		}
	}
	t.Fatalf("%q の行が無い:\n%s", needle, ansi.Strip(strings.Join(m.pickerBlock(), "\n")))
	return ""
}

// 紐づいたカードの ID と列が番号と題名の間に出る (記録の順)。完了 (片付けたものも記録に在る間) は [完了 C-xxx]。
// 別 repo の同じ番号のカードは出さない。epic の見出しは、完了でないカードがある子の数を出す。
func TestPickerShowsLinkedCards(t *testing.T) {
	m, _ := linkedPicker(t)
	if l := pickerLine(t, m, "#100"); !strings.Contains(l, "#100  C-002 レビュー   C-003 作業中  (bug): タイトル A") {
		t.Errorf("100 の札が違う: %q", l)
	}
	if l := pickerLine(t, m, "#101"); !strings.Contains(l, "#101 [完了 C-004] (feat): タイトル B") || strings.Contains(l, "C-006") {
		t.Errorf("101 の札が違う (片付けた完了は出す・別 repo は出さない): %q", l)
	}
	if l := pickerLine(t, m, "#201"); !strings.Contains(l, "C-005 着手待ち") {
		t.Errorf("201 の札が無い: %q", l)
	}
	if l := pickerLine(t, m, "epic 200"); !strings.Contains(l, "(未完了の子 1 · カードあり 1)") {
		t.Errorf("epic の見出しに子のカードの数が無い: %q", l)
	}
	for _, l := range m.pickerBlock() {
		if s := ansi.Strip(l); strings.Contains(s, "C-001") {
			t.Errorf("issue に紐づかないカードが出た: %q", s)
		}
	}
}

// pickerTo は一覧のカーソルを番号 num の行へ動かす。
func pickerTo(t *testing.T, m *Model, num string) {
	t.Helper()
	press(m, "g")
	for r := m.picker.rows[m.picker.cursor]; r.iss == nil || r.iss.Number != num || r.epic != ""; r = m.picker.rows[m.picker.cursor] {
		before := m.picker.cursor
		press(m, "j")
		if m.picker.cursor == before {
			t.Fatalf("%s の行に届かない", num)
		}
	}
}

// カードがある issue を Enter で選ぶと、足す前に一覧の下で y/N を確かめる。y だけが足す (Enter を重ねても通らない)。
func TestPickerAsksBeforeAddingLinkedIssue(t *testing.T) {
	m, be := linkedPicker(t)
	pickerTo(t, m, "100")
	press(m, "enter")
	if m.mode == modeInput || !m.picker.open {
		t.Fatal("確かめずに補足の入力へ進んだ")
	}
	if l := pickerLine(t, m, "それでも足す?"); !strings.Contains(l, "#100 には既にカード C-002 (レビュー)・C-003 (作業中) がある") {
		t.Errorf("確かめの文が違う: %q", l)
	}
	press(m, "enter") // 2 度押しは取り消し
	if m.mode == modeInput || m.picker.dupe != "" || !m.picker.open {
		t.Fatal("Enter の 2 度押しで確かめを越えた / 一覧が閉じた")
	}
	press(m, "enter", "y")
	if m.mode != modeInput || m.inputKind != inputIssue || m.picker.target == nil || m.picker.target.Number != 100 {
		t.Fatal("y で補足の入力へ進まない")
	}
	press(m, "enter", "y")
	if r, ok := be.applied[len(be.applied)-1].(backend.NewRequest); !ok || r.Issue == nil || r.Issue.Number != 100 {
		t.Fatalf("y の後に依頼が届かない: %#v", be.applied)
	}
}

// epic の見出しは、子にカードがあれば確かめる。カードの無い issue は確かめずに補足の入力へ進む。
func TestPickerAsksForEpicWithLinkedChildren(t *testing.T) {
	m, _ := linkedPicker(t)
	press(m, "g", "enter")
	if l := pickerLine(t, m, "それでも足す?"); !strings.Contains(l, "epic 200 の子 1 件にカードがある") {
		t.Errorf("epic の確かめの文が違う: %q", l)
	}
	press(m, "n")
	if m.picker.dupe != "" || m.mode == modeInput {
		t.Fatal("n で取り消せない")
	}

	m2, _, _ := pickerModel(t) // どの issue にも紐づかない
	press(m2, "i")
	pickerTo(t, m2, "100")
	press(m2, "enter")
	if m2.mode != modeInput || m2.picker.dupe != "" {
		t.Fatal("カードの無い issue で確かめた")
	}
}
