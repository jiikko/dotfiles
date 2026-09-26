package store

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"pro-con/card"
)

func markOf(c card.Card) (Purged, error) {
	return Purged{Worktree: "/r/.claude/worktrees/" + card.SessionName(c), Sessions: []PurgedSession{{ID: "abcd1234", SessionID: "s-" + c.ID}}}, nil
}

// 書庫へ移して間もないカードは残し、完了から 1 週間たったものだけを消して、消す前に片付けの印を残す。
func TestPurgeRemovesWeekOldCardsAndLeavesMark(t *testing.T) {
	dir := t.TempDir()
	old, young := doneCard("C-001", t0), doneCard("C-002", t0.Add(PurgeAfter/2))
	putCards(t, dir, old, young)
	now := t0.Add(PurgeAfter)
	if _, err := Archive(dir, now); err != nil {
		t.Fatal(err)
	}
	gone, err := Purge(dir, now, markOf)
	if err != nil || !slices.Equal(ids(gone), []string{"C-001"}) {
		t.Fatalf("消したカード = %v %v", ids(gone), err)
	}
	arch, err := LoadArchive(dir)
	if err != nil || !slices.Equal(ids(arch), []string{"C-002"}) {
		t.Fatalf("書庫 = %v %v", ids(arch), err)
	}
	marks, err := LoadPurged(dir)
	m, ok := marks["C-001"]
	if err != nil || !ok || !m.DoneAt.Equal(t0) || !m.PurgedAt.Equal(now) || m.Worktree != "/r/.claude/worktrees/pc-c-001" || len(m.Sessions) != 1 {
		t.Fatalf("片付けの印 = %+v (%v, %v)", m, ok, err)
	}
	if _, found, _ := Find(dir, "C-001"); found {
		t.Error("消したカードを Find が返す")
	}
	st, _ := Load(dir)
	if st.NextID != 3 {
		t.Errorf("nextId = %d (消したカードの ID を使い回す)", st.NextID)
	}
}

// 消さない: 記録にもあるカード・PG を止め終えていない・削除の途中・完了していない・残るカードの親。
func TestPurgeKeeps(t *testing.T) {
	now := t0.Add(PurgeAfter)
	stopping := doneCard("C-002", t0)
	stopping.StopAfterClose = true
	deleting := doneCard("C-003", t0)
	deleting.DeleteAt = t0
	running := doneCard("C-004", t0)
	running.State = card.Running
	parent := doneCard("C-005", t0)
	child := doneCard("C-006", t0.Add(PurgeAfter/2))
	child.ParentID = "C-005"
	arch := []card.Card{doneCard("C-001", t0), stopping, deleting, running, parent, child}
	got := purgeable([]card.Card{doneCard("C-001", t0)}, arch, now)
	if len(got) != 0 {
		t.Fatalf("消してはいけないカードを消す: %v", got)
	}
	// 子も消すなら親も消す
	child.Since = t0
	got = purgeable(nil, []card.Card{parent, child}, now)
	if !got["C-005"] || !got["C-006"] {
		t.Fatalf("子と一緒に親を消さない: %v", got)
	}
}

// 印を作れなければ何も消さない (印の無いまま消すと、片付けがそのカードの worktree と session を二度と消せない)。
func TestPurgeKeepsAllWhenMarkFails(t *testing.T) {
	dir := t.TempDir()
	putCards(t, dir, doneCard("C-001", t0))
	now := t0.Add(PurgeAfter)
	if _, err := Archive(dir, now); err != nil {
		t.Fatal(err)
	}
	if _, err := Purge(dir, now, func(card.Card) (Purged, error) { return Purged{}, errors.New("起動の記録を読めない") }); err == nil {
		t.Fatal("失敗を返さない")
	}
	if arch, _ := LoadArchive(dir); len(arch) != 1 {
		t.Fatalf("印を作れないのに消した: %v", ids(arch))
	}
	if marks, _ := LoadPurged(dir); len(marks) != 0 {
		t.Fatalf("印を残した: %v", marks)
	}
}

// 書庫の読めない行は消さずにそのまま残す (消してよいか示せない)。書きかけの末尾の行の後ろに次の行を繋げない。
func TestPurgeKeepsBrokenLines(t *testing.T) {
	dir := t.TempDir()
	putCards(t, dir, doneCard("C-001", t0), doneCard("C-002", t0.Add(PurgeAfter/2)))
	now := t0.Add(PurgeAfter)
	if _, err := Archive(dir, now); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ArchiveFile)
	data, _ := os.ReadFile(path)
	data = append([]byte("{壊れた行\n"), data...)
	data = append(data, []byte(`{"id":"C-00`)...) // 書きかけで落ちた末尾
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if gone, err := Purge(dir, now, markOf); err != nil || !slices.Equal(ids(gone), []string{"C-001"}) {
		t.Fatalf("消したカード = %v %v", ids(gone), err)
	}
	after, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSuffix(string(after), "\n"), "\n")
	if len(lines) != 3 || lines[0] != "{壊れた行" || lines[2] != `{"id":"C-00` || !strings.Contains(lines[1], `"C-002"`) {
		t.Fatalf("書き直した書庫 = %q", after)
	}
	if _, err := Archive(dir, now); err != nil { // 書きかけの行の後ろに足しても次の行を壊さない
		t.Fatal(err)
	}
}

// 印は片付けが済んだカードだけ消す (ほかのカードの印は残す)。同じカードの印が 2 行あれば (落ちてから足し直した) 両方消す。
func TestDropPurged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, PurgeFile)
	if err := appendLines(path, []Purged{{CardID: "C-001"}, {CardID: "C-002"}, {CardID: "C-001"}}); err != nil {
		t.Fatal(err)
	}
	if err := DropPurged(dir, "C-001"); err != nil {
		t.Fatal(err)
	}
	marks, err := LoadPurged(dir)
	if _, ok := marks["C-002"]; err != nil || len(marks) != 1 || !ok {
		t.Fatalf("印 = %v %v", marks, err)
	}
	if err := DropPurged(t.TempDir(), "C-001"); err != nil { // 印のファイルが無くても失敗にしない
		t.Fatal(err)
	}
}

// forget は PG のカードの ID だけを受け、完了していないカードは除ける (動いている PG の起動の記録を消させない)。記録は変えない。
func TestApplyForget(t *testing.T) {
	dir := t.TempDir()
	running := doneCard("C-002", t0)
	running.State, running.Archived = card.Running, false
	putCards(t, dir, doneCard("C-001", t0), running)
	ok := submit(t, dir, Request{Kind: KindForget, CardID: "C-001", Sessions: []string{"s1"}})
	gone := submit(t, dir, Request{Kind: KindForget, CardID: "C-009", Sessions: []string{"s9"}}) // 記録から消したカード
	notDone := submit(t, dir, Request{Kind: KindForget, CardID: "C-002", Sessions: []string{"s2"}})
	role := submit(t, dir, Request{Kind: KindForget, CardID: "PM", Sessions: []string{"s3"}})
	before, _ := Load(dir)
	res, err := Apply(dir, t0, nil)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Result{}
	for _, r := range res {
		byID[r.ID] = r
	}
	if r := byID[ok]; r.Err != "" || !slices.Equal(r.Sessions, []string{"s1"}) {
		t.Errorf("forget C-001 = %+v", r)
	}
	if r := byID[gone]; r.Err != "" {
		t.Errorf("記録に無いカードの forget を除けた: %s", r.Err)
	}
	if byID[notDone].Err == "" || byID[role].Err == "" {
		t.Errorf("完了していない / 役の forget を受けた: %+v / %+v", byID[notDone], byID[role])
	}
	after, _ := Load(dir)
	if len(after.Cards) != len(before.Cards) || after.Cards[0].State != card.Done || len(after.Cards[0].History) != len(before.Cards[0].History) {
		t.Error("forget で記録を変えた")
	}
}
