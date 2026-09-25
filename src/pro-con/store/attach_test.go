package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/card"
)

// attachRig は作業中のカード C-001 を 1 枚持つ置き場と、付けるファイルの置き場 (PG の worktree の代わり) を作る。
func attachRig(t *testing.T) (dir, work string) {
	t.Helper()
	dir, work = t.TempDir(), t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "t"})
	if _, err := Apply(dir, t0); err != nil {
		t.Fatal(err)
	}
	if err := Update(dir, func(st *State) error { st.Cards[0].State = card.Running; return nil }); err != nil {
		t.Fatal(err)
	}
	return dir, work
}

func writeFile(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func applyOne(t *testing.T, dir string) Result {
	t.Helper()
	res, err := Apply(dir, t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("結果が 1 件ではない: %+v", res)
	}
	return res[0]
}

func loadCard(t *testing.T, dir, id string) card.Card {
	t.Helper()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range st.Cards {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("%s が無い", id)
	return card.Card{}
}

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func stagedFiles(t *testing.T, dir string) []string {
	t.Helper()
	names, _ := filepath.Glob(filepath.Join(dir, InboxDir, StageDir, "*"))
	return names
}

// 付けたファイルは dispatcher (Apply) が attachments/<カード>/ へ 0700 / 0600 で移し、記録に種類・一言・大きさ・元の名前を載せる。箱には残さない。
func TestAttachmentMovesIntoCardDir(t *testing.T) {
	dir, work := attachRig(t)
	src := writeFile(t, filepath.Join(work, "shot.PNG"), "png-bytes")
	if _, err := SubmitAttachment(dir, "C-001", src, "詳細の見た目"); err != nil {
		t.Fatal(err)
	}
	if r := applyOne(t, dir); r.Err != "" {
		t.Fatalf("除けられた: %s", r.Err)
	}
	c := loadCard(t, dir, "C-001")
	if len(c.Attachments) != 1 {
		t.Fatalf("添付が載っていない: %+v", c.Attachments)
	}
	a := c.Attachments[0]
	cardDir := filepath.Join(dir, AttachDir, "C-001")
	if abs, _ := filepath.Abs(cardDir); filepath.Dir(a.Path) != abs || !strings.HasSuffix(a.Path, ".png") {
		t.Fatalf("置き場が違う: %s (want %s/<依頼>.png)", a.Path, abs)
	}
	if a.Kind != card.AttachImage || a.Note != "詳細の見た目" || a.Name != "shot.PNG" || a.Size != int64(len("png-bytes")) {
		t.Fatalf("記録の中身が違う: %+v", a)
	}
	if got, _ := os.ReadFile(a.Path); string(got) != "png-bytes" {
		t.Fatalf("中身が違う: %q", got)
	}
	if m := mode(t, cardDir); m != 0o700 {
		t.Fatalf("カードの置き場の権限 %o (want 700)", m)
	}
	if m := mode(t, a.Path); m != 0o600 {
		t.Fatalf("添付の権限 %o (want 600)", m)
	}
	if s := stagedFiles(t, dir); len(s) != 0 {
		t.Fatalf("受付の箱に残っている: %v", s)
	}
	if h := c.History[len(c.History)-1].Text; !strings.Contains(h, "添付") || !strings.Contains(h, "詳細の見た目") {
		t.Fatalf("履歴に残らない: %q", h)
	}
}

// 移した後、記録を書く前に落ちた形: 次の Apply は移し先のファイルを見て、同じ依頼を当て直す (除けない・二重に置かない)。
func TestAttachmentReappliedAfterCrash(t *testing.T) {
	dir, work := attachRig(t)
	id, err := SubmitAttachment(dir, "C-001", writeFile(t, filepath.Join(work, "a.ans"), "\x1b[31mred\x1b[0m"), "")
	if err != nil {
		t.Fatal(err)
	}
	r := Request{ID: id, Kind: KindAttachment, CardID: "C-001", Name: "a.ans"}
	staged, err := stageAttachment(dir, &r)
	if err != nil || staged == "" {
		t.Fatalf("箱のファイルを見つけられない: %q %v", staged, err)
	}
	if err := adoptAttachment(staged, r.File); err != nil { // 移しただけで落ちた
		t.Fatal(err)
	}
	if res := applyOne(t, dir); res.Err != "" {
		t.Fatalf("移し済みの依頼を除けた: %s", res.Err)
	}
	c := loadCard(t, dir, "C-001")
	if len(c.Attachments) != 1 || c.Attachments[0].Path != r.File || c.Attachments[0].Kind != card.AttachText {
		t.Fatalf("当て直した記録が違う: %+v", c.Attachments)
	}
}

// 当てられない依頼 (無いカード・完了のカード・上限) は除け、箱のファイルも消す。無いカードの置き場は作らない。
func TestAttachmentRejectedRemovesStaged(t *testing.T) {
	for _, tc := range []struct {
		name, card, why string
		setup           func(*card.Card)
	}{
		{"無いカード", "C-009", "カード", nil},
		{"完了", "C-001", "完了", func(c *card.Card) { c.State, c.Ending = card.Done, card.EndAnswered }},
		{"上限", "C-001", "まで", func(c *card.Card) {
			for range MaxAttachPerCard {
				c.Attachments = append(c.Attachments, card.Attachment{Path: "/x", Kind: card.AttachFile})
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, work := attachRig(t)
			if tc.setup != nil {
				if err := Update(dir, func(st *State) error { tc.setup(&st.Cards[0]); return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := SubmitAttachment(dir, tc.card, writeFile(t, filepath.Join(work, "a.png"), "x"), ""); err != nil {
				t.Fatal(err)
			}
			r := applyOne(t, dir)
			if !strings.Contains(r.Err, tc.why) {
				t.Fatalf("除けた理由が違う: %q (want %q を含む)", r.Err, tc.why)
			}
			if s := stagedFiles(t, dir); len(s) != 0 {
				t.Fatalf("除けた添付のファイルが箱に残る: %v", s)
			}
			if _, err := os.Stat(filepath.Join(dir, AttachDir, "C-009")); err == nil {
				t.Fatal("無いカードの置き場を作った")
			}
		})
	}
}

// 依頼に書かれた名前は信じない: 元の名前に .. が入っても、置き場の外に出ない (名前は依頼の ID と拡張子だけで決める)。
func TestAttachmentNameCannotEscape(t *testing.T) {
	dir, work := attachRig(t)
	id, err := SubmitAttachment(dir, "C-001", writeFile(t, filepath.Join(work, "a.png"), "x"), "")
	if err != nil {
		t.Fatal(err)
	}
	// 箱の依頼を書き換えた形 (元の名前に .. を入れる)
	p := filepath.Join(dir, InboxDir, id+".json")
	data, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(data), `"name":"a.png"`, `"name":"../../../evil.png"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := applyOne(t, dir); r.Err != "" {
		t.Fatalf("除けられた: %s", r.Err)
	}
	a := loadCard(t, dir, "C-001").Attachments[0]
	abs, _ := filepath.Abs(filepath.Join(dir, AttachDir, "C-001"))
	if filepath.Dir(a.Path) != abs || filepath.Base(a.Path) != id+".png" {
		t.Fatalf("置き場の外に出た / 名前が依頼の ID で決まらない: %s", a.Path)
	}
}

// 付ける側は上限を超えるファイル・空のファイル・ディレクトリを箱に置く前に断る。
func TestSubmitAttachmentRefuses(t *testing.T) {
	dir, work := attachRig(t)
	big := filepath.Join(work, "big.png")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxAttachBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	for _, src := range []string{big, writeFile(t, filepath.Join(work, "empty.png"), ""), work, filepath.Join(work, "none.png")} {
		if _, err := SubmitAttachment(dir, "C-001", src, ""); err == nil {
			t.Errorf("%s を断らない", src)
		}
	}
	if n := Pending(dir); n != 0 {
		t.Fatalf("断ったのに箱に依頼がある: %d", n)
	}
	if s := stagedFiles(t, dir); len(s) != 0 {
		t.Fatalf("断ったのに箱にファイルがある: %v", s)
	}
}

// 片付け: 記録に無いカード (削除した・書庫へ移した) の添付を消し、依頼の来ない箱のファイルは stageTTL を過ぎたら消す。
func TestSweepAttachments(t *testing.T) {
	dir, work := attachRig(t)
	if _, err := SubmitAttachment(dir, "C-001", writeFile(t, filepath.Join(work, "a.png"), "x"), ""); err != nil {
		t.Fatal(err)
	}
	applyOne(t, dir)
	kept := loadCard(t, dir, "C-001").Attachments[0].Path
	gone := filepath.Join(dir, AttachDir, "C-002", "x.png") // 記録に無いカード
	if err := os.MkdirAll(filepath.Dir(gone), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, gone, "x")
	stage := filepath.Join(dir, InboxDir, StageDir)
	oldOrphan := writeFile(t, filepath.Join(stage, "00000000000000000001-aaaaaaaa.png"), "x")
	newOrphan := writeFile(t, filepath.Join(stage, "00000000000000000002-bbbbbbbb.png"), "x")
	waiting := writeFile(t, filepath.Join(stage, "00000000000000000003-cccccccc.png"), "x") // 依頼がまだ箱にある
	writeFile(t, filepath.Join(dir, InboxDir, "00000000000000000003-cccccccc.json"), "{}")
	now := time.Now()
	for _, p := range []string{oldOrphan, waiting} {
		if err := os.Chtimes(p, now.Add(-2*stageTTL), now.Add(-2*stageTTL)); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := SweepAttachments(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "C-002" {
		t.Fatalf("消したカード %v (want [C-002])", ids)
	}
	for p, want := range map[string]bool{kept: true, gone: false, oldOrphan: false, newOrphan: true, waiting: true} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("%s: 残っている=%v (want %v)", p, err == nil, want)
		}
	}
}
