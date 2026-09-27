package dispatcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/store"
)

// quietRig は一覧を取った回数を数える dispatcher (時計は手で進める)。
type quietRig struct {
	d     *Dispatcher
	dir   string
	now   time.Time
	lists int
}

func newQuietRig(t *testing.T, cards ...card.Card) *quietRig {
	t.Helper()
	r := &quietRig{dir: t.TempDir(), now: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)}
	for i := range cards { // 完了した直後 (Since が zero だと、最初の Tick で書庫へ移って記録から消える)
		if cards[i].Since.IsZero() {
			cards[i].Since = r.now
		}
	}
	saveCards(t, r.dir, cards)
	r.d = &Dispatcher{Dir: r.dir, Limit: 1, Now: func() time.Time { return r.now }, Sleep: func(time.Duration) {},
		List: func(context.Context) ([]agents.Session, error) { r.lists++; return nil, nil }}
	return r
}

// tickAt は after だけ時計を進めて 1 回 Tick し、この Tick で一覧を取ったかを返す。
func (r *quietRig) tickAt(t *testing.T, after time.Duration) bool {
	t.Helper()
	r.now = r.now.Add(after)
	before := r.lists
	if _, err := r.d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r.lists > before
}

func (r *quietRig) seen(t *testing.T) store.Seen {
	t.Helper()
	s, err := store.LoadSeen(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func doneCard(id string) card.Card {
	return card.Card{ID: id, State: card.Done, Ending: card.EndAnswered, Session: "s-" + id}
}

// 暇な間 (カードが全部完了・役を回さない) は、一覧を QuietListEvery に 1 回だけ取り、画面にはその間使える一覧を渡す (issue 559)。
func TestQuietTicksThinOutList(t *testing.T) {
	r := newQuietRig(t, doneCard("C-001"))
	if !r.tickAt(t, 0) {
		t.Fatal("起動して最初の Tick で一覧を取らない")
	}
	if got := r.seen(t).Keep; got != defaultQuietListEvery {
		t.Fatalf("暇な Tick の一覧の Keep = %v (%v のはず)", got, defaultQuietListEvery)
	}
	for i := range 9 { // 3 秒ごとに 27 秒まで
		if r.tickAt(t, 3*time.Second) {
			t.Fatalf("暇な間の %d 回目の Tick で一覧を取った", i+2)
		}
	}
	if !r.tickAt(t, 3*time.Second) { // 最後に取ってから 30 秒
		t.Fatal("暇な間でも QuietListEvery 経ったら一覧を取る")
	}
	if r.lists != 2 {
		t.Fatalf("30 秒の間に一覧を %d 回取った (2 回のはず)", r.lists)
	}
}

// 忙しい Tick の直後は、暇になっても 1 度は一覧を取り直す (画面に渡した一覧は忙しい間の鮮度なので、間引いた鮮度で渡し直す)。
func TestQuietAfterBusyListsOnceMore(t *testing.T) {
	c := doneCard("C-001")
	c.State, c.Ending, c.Wait = card.Waiting, card.EndNone, card.Wait{Kind: card.WaitQuestion}
	r := newQuietRig(t, c)
	if !r.tickAt(t, 0) || r.seen(t).Keep != 0 {
		t.Fatalf("忙しい Tick: 一覧 %d 回・Keep %v (取って 0 のはず)", r.lists, r.seen(t).Keep)
	}
	if !r.tickAt(t, 3*time.Second) {
		t.Fatal("忙しい間の Tick で一覧を取らない")
	}
	done := doneCard("C-001")
	done.Since = r.now
	saveCards(t, r.dir, []card.Card{done})
	if !r.tickAt(t, 3*time.Second) {
		t.Fatal("暇になった最初の Tick で一覧を取り直さない (前の一覧は忙しい間の鮮度)")
	}
	if got := r.seen(t).Keep; got != defaultQuietListEvery {
		t.Fatalf("暇になった Tick の一覧の Keep = %v", got)
	}
	if r.tickAt(t, 3*time.Second) {
		t.Fatal("暇な Tick が続いても一覧を取る")
	}
}

// 暇で間引いている間に依頼が来たら、待たずにその Tick で一覧を取る (カードを起動・登録する)。
func TestQuietListsAtOnceOnRequest(t *testing.T) {
	r := newQuietRig(t, doneCard("C-001"))
	r.d.Launch = &fakeLauncher{}
	r.tickAt(t, 0)
	if r.tickAt(t, 3*time.Second) {
		t.Fatal("前提: 暇な Tick で一覧を取った")
	}
	if _, err := store.Submit(r.dir, store.Request{Kind: "add", Title: "新しい依頼", Repo: "dotfiles"}); err != nil {
		t.Fatal(err)
	}
	if !r.tickAt(t, 3*time.Second) {
		t.Fatal("依頼を適用した Tick で一覧を取らない")
	}
}

// quiet は、一覧と照らして扱うものが 1 つでもあれば偽。
func TestQuietConditions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		card  func(*card.Card)
		pm    *store.PMState
		quiet bool
	}{
		{"全部完了", nil, nil, true},
		{"完了していないカード", func(c *card.Card) { c.State, c.Ending = card.Review, card.EndNone }, nil, false},
		{"止める途中", func(c *card.Card) { c.StopAfterClose = true }, nil, false},
		{"削除待ち", func(c *card.Card) { c.DeleteAt = time.Now() }, nil, false},
		{"起動の結果待ち", func(c *card.Card) { c.Launching = "再開" }, nil, false},
		{"PM を止めてある", nil, &store.PMState{Session: "pm1", Stopped: true}, true},
		{"PM が居ない", nil, &store.PMState{}, true},
		{"PM が生きている (知らせる物が無くても、入力待ちと落ちたのを一覧で見張る)", nil, &store.PMState{Session: "pm1"}, false},
		{"PM の起動の結果待ち", nil, &store.PMState{Launching: "起動"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := doneCard("C-001")
			if tc.card != nil {
				tc.card(&c)
			}
			r := newQuietRig(t, c)
			if tc.pm != nil {
				r.d.PMRepo, r.d.IntegratorOff = "/repo", true
				if err := store.SaveRole(r.dir, store.PMStateFile, *tc.pm); err != nil {
					t.Fatal(err)
				}
			}
			if got := r.d.quiet(); got != tc.quiet {
				t.Fatalf("quiet = %v (%v のはず)", got, tc.quiet)
			}
		})
	}
	// 取り込みの係も同じに見る (役のループから外れていない)
	t.Run("取り込みの係の起動の結果待ち", func(t *testing.T) {
		r := newQuietRig(t, doneCard("C-001"))
		r.d.PMRepo, r.d.PMOff = "/repo", true
		if err := store.SaveRole(r.dir, store.IntegratorStateFile, store.PMState{Launching: "起動"}); err != nil {
			t.Fatal(err)
		}
		if r.d.quiet() {
			t.Fatal("取り込みの係の起動の結果待ちを暇と読んだ")
		}
	})
	// 読めない記録は暇と言わない (一覧を取る側に倒す)
	t.Run("記録を読めない", func(t *testing.T) {
		r := newQuietRig(t, doneCard("C-001"))
		if err := os.WriteFile(filepath.Join(r.dir, store.StateFile), []byte("{壊れた"), 0o600); err != nil {
			t.Fatal(err)
		}
		if r.d.quiet() {
			t.Fatal("読めない記録を暇と読んだ")
		}
	})
}

// 一覧を間引いた Tick でも、一覧を使わない経路は回す: 完了したカードへの btw に、次に一覧を取るのを待たずに答える。
// (PG の出力が無いので答えは記録から同期的に作る経路。裏で作る経路 (d.Ask) は btw_test が見る)
func TestQuietTickStillAnswersBtw(t *testing.T) {
	r := newQuietRig(t, doneCard("C-001"))
	r.tickAt(t, 0)
	if _, ok := states(t, r.dir)["C-001"]; !ok {
		t.Fatal("前提: 完了したカードが書庫へ移った")
	}
	btw(t, r.dir, "C-001", "結果は?")
	if r.tickAt(t, 3*time.Second) {
		t.Fatal("前提: 暇な Tick で一覧を取った")
	}
	c := states(t, r.dir)["C-001"]
	if len(c.Btws) != 1 || c.Btws[0].Answered.IsZero() || c.Btws[0].Answer == "" {
		t.Fatalf("一覧を間引いた Tick で btw に答えない: %+v", c.Btws)
	}
}

// 一覧を間引いた Tick でも、役の様子を「確かめ中」にしない (最後に照らした様子のまま出す)。
func TestQuietTickKeepsRoleState(t *testing.T) {
	r := newQuietRig(t, doneCard("C-001"))
	r.d.PMRepo, r.d.IntegratorOff = "/repo", true
	if err := store.SaveRole(r.dir, store.PMStateFile, store.PMState{Session: "pm1", Stopped: true}); err != nil {
		t.Fatal(err)
	}
	r.tickAt(t, 0)
	if r.tickAt(t, 3*time.Second) {
		t.Fatal("前提: 暇な Tick で一覧を取った")
	}
	if got := r.d.roleState(pmRole, r.now).Phase; got != card.RoleStopped {
		t.Fatalf("間引いた Tick の PM の様子 = %v (止めた のはず)", got)
	}
}
