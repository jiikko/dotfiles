package fake

import (
	"errors"
	"testing"
	"time"

	"pro-con/backend"
	"pro-con/card"
)

var t0 = time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

func get(t *testing.T, s *Sim, id string) card.Card {
	t.Helper()
	for _, c := range s.Snapshot().Cards {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("%s が無い", id)
	return card.Card{}
}

func TestSeedHasNoViolations(t *testing.T) {
	if vs := New(t0).Snapshot().Violations; len(vs) != 0 {
		t.Fatalf("見本データが不変条件を破っている: %v", vs)
	}
}

// 回答は「まだ質問待ちなら書く」の比較付き更新。2 回目 (PM と人間の同時回答の負けた側) は拒否される。
func TestAnswerIsCompareAndSet(t *testing.T) {
	s := New(t0)
	if _, err := s.Apply(backend.Answer{CardID: "C-007", Text: "遅延ロードで", From: "人間"}); err != nil {
		t.Fatalf("1 回目の回答が失敗した: %v", err)
	}
	if st := get(t, s, "C-007").State; st != card.Planned {
		t.Fatalf("回答後は再開待ち (分解済み) のはず: %s", st.Label())
	}
	_, err := s.Apply(backend.Answer{CardID: "C-007", Text: "外して", From: "PM-A"})
	if !errors.Is(err, backend.ErrNotWaiting) {
		t.Fatalf("2 回目の回答は ErrNotWaiting のはず: %v", err)
	}
}

// 回答を受けたカードは、新しい session ではなく同じ session で再開する (415 論点 8 の候補 A)。
func TestAnsweredCardResumesSameSession(t *testing.T) {
	s := New(t0)
	sess := get(t, s, "C-007").Session
	if _, err := s.Apply(backend.Answer{CardID: "C-007", Text: "遅延ロードで", From: "人間"}); err != nil {
		t.Fatal(err)
	}
	for range 30 {
		s.Step()
		if c := get(t, s, "C-007"); c.State == card.Running {
			if c.Session != sess {
				t.Fatalf("別の session で再開した: %s → %s", sess, c.Session)
			}
			return
		}
	}
	t.Fatal("30 刻み待っても再開しなかった")
}

// dispatcher は上限を超えて PG を起こさない。
func TestDispatchRespectsLimit(t *testing.T) {
	s := New(t0)
	sawFull := false
	for range 40 {
		s.Step()
		snap := s.Snapshot()
		if len(snap.Consumers) > snap.Limit {
			t.Fatalf("PG %d 体が上限 %d を超えた", len(snap.Consumers), snap.Limit)
		}
		if len(snap.Consumers) == snap.Limit {
			sawFull = true
		}
	}
	if !sawFull {
		t.Fatal("上限まで埋まる場面が一度も無かった (上限の検査が空振りしている)")
	}
}

// watchdog: 進捗の止まった作業中のカードは停滞になる。リソース待ちは正当な待ちなので、待っている間は停滞にしない。
func TestWatchdogFlagsStallButNotResourceWait(t *testing.T) {
	s := New(t0)
	// 外の占有を停滞の閾値より長く続け、C-006 のリソース待ちが閾値を越える状況を作る
	// (既定の 2 刻みでは閾値の前に待ちが終わり、除外を外す変異が緑のまま通った)
	s.externalUntil["device"] = t0.Add(3 * StallAfter)
	flagged := false
	for range 12 {
		s.Step()
		for _, c := range s.Snapshot().Cards {
			if c.Stalled && c.Wait.Kind == card.WaitResource {
				t.Fatalf("%s はリソース待ちなのに停滞にされた", c.ID)
			}
		}
		if get(t, s, "C-005").Stalled {
			flagged = true
		}
	}
	if !flagged {
		t.Fatal("進捗の止まった C-005 が停滞にならなかった")
	}
	if c := get(t, s, "C-006"); c.Wait.Kind != card.WaitResource || s.now.Sub(c.LastProgress) < StallAfter {
		t.Fatalf("前提が崩れた: C-006 が閾値を越えてリソースを待っていない (wait=%v, 進捗なし %v)", c.Wait.Kind, s.now.Sub(c.LastProgress))
	}
}

// 別件の追加オーダーは新しい依頼として受け付けられ、PM の分解 (issue への紐づけ) を通って完了まで進む。
// 途中のどの刻みでも不変条件を破らない (分解を飛ばすと issue も終わり方も無いまま完了する)。
func TestSeparateOrderCreatesChildCard(t *testing.T) {
	s := New(t0)
	before := len(s.Snapshot().Cards)
	if _, err := s.Apply(backend.AddOrder{CardID: "C-005", Kind: card.OrderSeparate, Text: "別の画面も"}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if len(snap.Cards) != before+1 {
		t.Fatalf("カードが 1 枚増えるはず: %d → %d", before, len(snap.Cards))
	}
	child := snap.Cards[len(snap.Cards)-1]
	if child.ParentID != "C-005" || child.State != card.Requested {
		t.Fatalf("子カードは C-005 を親に依頼の列へ入るはず: parent=%q state=%s", child.ParentID, child.State.Label())
	}
	for range 60 {
		s.Step()
		if vs := s.Snapshot().Violations; len(vs) != 0 {
			t.Fatalf("別件の子カードが進む途中で不変条件が破れた: %v", vs)
		}
		if c := get(t, s, child.ID); c.State == card.Done {
			if len(c.Issues) == 0 {
				t.Fatal("issue に紐づかないまま完了した")
			}
			return
		}
	}
	t.Fatal("60 刻み待っても子カードが完了しなかった (完了まで進まないと途中の検査が空振りする)")
}

// 方針変更は届いた扱いになり、停滞していたカードの停滞を解く (止めて差し替えて再開したので)。
func TestRedirectOrderClearsStall(t *testing.T) {
	s := New(t0)
	for range 12 {
		s.Step()
		if get(t, s, "C-005").Stalled {
			break
		}
	}
	if !get(t, s, "C-005").Stalled {
		t.Fatal("前提が崩れた: C-005 が停滞にならない")
	}
	if _, err := s.Apply(backend.AddOrder{CardID: "C-005", Kind: card.OrderRedirect, Text: "B 案で"}); err != nil {
		t.Fatal(err)
	}
	c := get(t, s, "C-005")
	if c.Stalled {
		t.Fatal("方針変更の後も停滞のまま")
	}
	if len(c.Orders) != 1 || !c.Orders[0].Delivered {
		t.Fatalf("方針変更は届いた扱いのはず: %+v", c.Orders)
	}
	s.Step()
	if get(t, s, "C-005").Stalled {
		t.Fatal("方針変更の直後の刻みでまた停滞にされた (進捗の時刻が更新されていない)")
	}
}

// 追記は受けた時点では未達で、PG のターンの区切り (次の刻みで作業中なら) に届く。
func TestAppendOrderIsDeliveredOnNextStep(t *testing.T) {
	s := New(t0)
	if _, err := s.Apply(backend.AddOrder{CardID: "C-005", Kind: card.OrderAppend, Text: "文言も直して"}); err != nil {
		t.Fatal(err)
	}
	if o := get(t, s, "C-005").Orders; len(o) != 1 || o[0].Delivered {
		t.Fatalf("受けた直後は未達の追加オーダーが 1 件のはず: %+v", o)
	}
	s.Step()
	if o := get(t, s, "C-005").Orders; !o[0].Delivered {
		t.Fatal("次の刻みで届いていない")
	}
}

func TestAttachNeedsSession(t *testing.T) {
	if _, err := New(t0).AttachCommand(""); !errors.Is(err, backend.ErrNoSession) {
		t.Fatalf("session の無いカードの attach は ErrNoSession のはず: %v", err)
	}
}
