package store

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"pro-con/card"
)

func doneCard(id string, since time.Time) card.Card {
	return card.Card{ID: id, Title: id, State: card.Done, Ending: card.EndAnswered, Since: since, Archived: true}
}

func putCards(t *testing.T, dir string, cs ...card.Card) {
	t.Helper()
	if err := Update(dir, time.Now(), func(s *State) error { s.Cards, s.NextID = cs, len(cs)+1; return nil }); err != nil {
		t.Fatal(err)
	}
}

func ids(cs []card.Card) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

// 削除の途中のカードは移さない (dispatcher が PG を止めてから記録から外す。先に書庫へ移すと、削除したカードが書庫に残る)。
func TestArchiveKeepsDeletingCard(t *testing.T) {
	dir := t.TempDir()
	c := doneCard("C-001", t0)
	c.DeleteAt = t0
	putCards(t, dir, c)
	moved, err := Archive(dir, t0.Add(AutoClearAfter))
	if err != nil || len(moved) != 0 {
		t.Fatalf("削除の途中のカードを移した: %v %v", ids(moved), err)
	}
}

// 親は、記録に残る子が居る間は残す。子も移すなら一緒に移す (孫まで辿る)。
func TestArchiveParentFollowsChildren(t *testing.T) {
	dir := t.TempDir()
	top, mid, leaf := doneCard("C-001", t0), doneCard("C-002", t0), doneCard("C-003", t0)
	mid.ParentID, leaf.ParentID = "C-001", "C-002"
	leaf.Archived = false // 完了して間もない = 残す
	putCards(t, dir, top, mid, leaf)
	if moved, err := Archive(dir, t0); err != nil || len(moved) != 0 {
		t.Fatalf("残る孫の祖先を移した: %v %v", ids(moved), err)
	}
	moved, err := Archive(dir, t0.Add(AutoClearAfter))
	if err != nil || !slices.Equal(ids(moved), []string{"C-001", "C-002", "C-003"}) {
		t.Fatalf("子が移れるようになったのに親を一緒に移さない: %v %v", ids(moved), err)
	}
}

// 書庫に足した後、記録を書き直す前に落ちた形 (同じカードが記録と書庫の両方、書庫に 2 行): Find は記録を正とし、書庫は後の行を正とする。
func TestArchiveDuplicateLines(t *testing.T) {
	dir := t.TempDir()
	old := doneCard("C-001", t0)
	if err := appendArchive(dir, []card.Card{old}); err != nil {
		t.Fatal(err)
	}
	newer := old
	newer.Title = "後"
	if err := appendArchive(dir, []card.Card{newer}); err != nil {
		t.Fatal(err)
	}
	arch, err := LoadArchive(dir)
	if err != nil || len(arch) != 1 || arch[0].Title != "後" {
		t.Fatalf("書庫の同じ ID を後の行で上書きしていない: %v %v", arch, err)
	}
	live := old
	live.Title = "記録"
	putCards(t, dir, live)
	c, ok, err := Find(dir, "C-001")
	if err != nil || !ok || c.Title != "記録" {
		t.Fatalf("記録にあるカードを書庫から返した: %q %v %v", c.Title, ok, err)
	}
}

// 書庫の末尾が書きかけの行でも、次に足した行は読める (書きかけの行だけを飛ばし、数をエラーで返す)。
func TestArchiveSurvivesTornLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ArchiveFile), []byte(`{"ID":"C-00`), 0o600); err != nil {
		t.Fatal(err)
	}
	putCards(t, dir, doneCard("C-002", t0))
	if _, err := Archive(dir, t0); err != nil {
		t.Fatal(err)
	}
	c, ok, err := Find(dir, "C-002")
	if !ok || c.ID != "C-002" || err != nil {
		t.Fatalf("書きかけの行の後に足したカードを読めない: %v %v", ok, err)
	}
	if arch, err := LoadArchive(dir); len(arch) != 1 || err == nil {
		t.Fatalf("書きかけの行を黙って飛ばした / 読めた行まで捨てた: %v %v", ids(arch), err)
	}
}

// 移した後の記録は動いているカードだけ (採番は続きから)。
func TestArchiveKeepsNextID(t *testing.T) {
	dir := t.TempDir()
	putCards(t, dir, doneCard("C-001", t0))
	if _, err := Archive(dir, t0); err != nil {
		t.Fatal(err)
	}
	submit(t, dir, Request{Kind: "add", Title: "次"})
	applyAll(t, dir)
	st, err := Load(dir)
	if err != nil || !slices.Equal(ids(st.Cards), []string{"C-002"}) {
		t.Fatalf("移した後の採番が違う: %v %v", ids(st.Cards), err)
	}
}

// 答えていない btw があるカードは移さない (dispatcher が答えるまで記録に置く)。答えたら移す。
func TestArchiveWaitsForBtwAnswer(t *testing.T) {
	dir := t.TempDir()
	c := doneCard("C-001", t0)
	c.Btws = []card.Btw{{Question: "どうなった?", At: t0}}
	putCards(t, dir, c)
	if moved, err := Archive(dir, t0); err != nil || len(moved) != 0 {
		t.Fatalf("答えていない btw があるカードを移した: %v %v", ids(moved), err)
	}
	c.Btws[0].Answer, c.Btws[0].Answered = "終わった", t0
	putCards(t, dir, c)
	if moved, err := Archive(dir, t0); err != nil || len(moved) != 1 {
		t.Fatalf("答えた後も移さない: %v %v", ids(moved), err)
	}
}

// 書庫に足した後に記録を書き直せない (落ちたのと同じ形): カードは書庫と記録の両方に残り、どちらからも消えない
// (順序が逆なら、記録から外した後に書庫へ足せずにカードを失う)。
func TestArchiveKeepsCardWhenRecordWriteFails(t *testing.T) {
	dir := t.TempDir()
	putCards(t, dir, doneCard("C-001", t0))
	if err := os.WriteFile(filepath.Join(dir, ArchiveFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil { // 記録の一時ファイルを作れなくする (書庫は既存のファイルへの追記なので書ける)
		t.Fatal(err)
	}
	_, err := Archive(dir, t0)
	if cerr := os.Chmod(dir, 0o700); cerr != nil {
		t.Fatal(cerr)
	}
	if err == nil {
		t.Fatal("記録を書けないのに失敗を返さない")
	}
	if st, _ := Load(dir); !slices.Equal(ids(st.Cards), []string{"C-001"}) {
		t.Fatalf("記録を書けなかったのに記録からカードが消えた: %v", ids(st.Cards))
	}
	if arch, _ := LoadArchive(dir); !slices.Equal(ids(arch), []string{"C-001"}) {
		t.Fatalf("書庫に足してから記録を書き直す順になっていない: %v", ids(arch))
	}
}
