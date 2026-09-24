package fake

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
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

// TUI からの依頼は、そのタブの repo のカードとして依頼の列に出て、PM に渡す指示にスコープが入る。
func TestNewRequestCreatesScopedCard(t *testing.T) {
	s := New(t0)
	if _, err := s.Apply(backend.NewRequest{Repo: backend.Repo{Name: "dotfiles", Path: "/src/dotfiles"}, Text: "検索を足して"}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	c := snap.Cards[len(snap.Cards)-1]
	if c.State != card.Requested || c.Repo != "dotfiles" || c.Request != "検索を足して" {
		t.Fatalf("依頼の列に dotfiles のカードとして出るはず: %+v", c)
	}
	if c.Prompt != backend.PMPrompt(backend.Repo{Name: "dotfiles", Path: "/src/dotfiles"}, "検索を足して") {
		t.Fatalf("PM に渡す指示にスコープの前置きが無い: %q", c.Prompt)
	}
	if len(snap.Violations) != 0 {
		t.Fatalf("不変条件が破れた: %v", snap.Violations)
	}
}

// 出力の無い長い実行は、見込みの 2 倍までは停滞にしない。越えたら停滞にする。
func TestLongExecStallsOnlyAfterTwiceExpected(t *testing.T) {
	s := New(t0)
	s.cards = append(s.cards, card.Card{ID: "X1", State: card.Running, Session: "s-x", Since: t0, LastProgress: t0,
		Exec: card.Exec{Command: "make e2e", Expected: 5 * time.Minute, Since: t0}})
	s.scripts["X1"] = &script{} // 出力しない (台本が空 = 進捗が出ない)
	for range 9 {
		s.Step()
	}
	if get(t, s, "X1").Stalled {
		t.Fatal("見込み 5 分の実行を 9 分で停滞にした (閾値は 2 倍の 10 分)")
	}
	s.Step()
	s.Step()
	if !get(t, s, "X1").Stalled {
		t.Fatal("見込みの 2 倍を越えても停滞にならない")
	}
}

// 実機 E2E はリソースを占有してから実行を始め (作業中の列のまま)、出力を出し切ったら実行を終えてレビューへ進む。
func TestExecStartsAfterResourceAndEnds(t *testing.T) {
	s := New(t0)
	sawExec := false
	for range 20 {
		s.Step()
		c := get(t, s, "C-006")
		if c.Exec.Active() {
			sawExec = true
			if c.State != card.Running || c.Wait.Kind != card.WaitNone || c.Exec.Resource != "device" {
				t.Fatalf("実行中は作業中の列で、リソースを占有しているはず: state=%s wait=%v res=%q", c.State.Label(), c.Wait.Kind, c.Exec.Resource)
			}
		}
		if c.State == card.Review {
			if !sawExec || c.Exec.Active() {
				t.Fatalf("実行してから終えてレビューへ進むはず: sawExec=%v active=%v", sawExec, c.Exec.Active())
			}
			return
		}
	}
	t.Fatal("20 刻みでレビューへ進まない")
}

// issue から出した依頼は、最初からその issue に紐づき、受付 PM は新しい番号を振らずに PG へ回す。
func TestIssueRequestStaysLinked(t *testing.T) {
	s := New(t0)
	target := &backend.IssueTarget{Number: 415, Title: "設計", Path: "/r/issues/415-design.md"}
	if _, err := s.Apply(backend.NewRequest{Repo: backend.Repo{Name: "dotfiles", Path: "/r"}, Issue: target}); err != nil {
		t.Fatalf("補足なしの issue の依頼は通るはず: %v", err)
	}
	id := s.Snapshot().Cards[len(s.Snapshot().Cards)-1].ID
	c := get(t, s, id)
	if len(c.Issues) != 1 || c.Issues[0].Number != 415 || !strings.Contains(c.Prompt, "/r/issues/415-design.md") {
		t.Fatalf("issue に紐づいていない / 指示に issue のパスが無い: %+v", c)
	}
	for range 10 {
		s.Step()
	}
	if c := get(t, s, id); len(c.Issues) != 1 || c.Issues[0].Number != 415 || c.State == card.Requested {
		t.Fatalf("受付 PM が別の番号を振った / 進まない: %+v", c)
	}
}

// 書き出して読み戻した Sim は、元の Sim と同じように進む (入れ替えても模擬が続く)。
func TestSaveRestoreContinues(t *testing.T) {
	a := New(t0)
	for range 4 {
		a.Step()
	}
	data, err := a.Save()
	if err != nil {
		t.Fatal(err)
	}
	b := New(t0.Add(time.Hour)) // 別の起点で作ってから置き換える
	if err := b.Restore(data); err != nil {
		t.Fatal(err)
	}
	for range 12 {
		a.Step()
		b.Step()
	}
	ja, _ := json.Marshal(a.Snapshot())
	jb, _ := json.Marshal(b.Snapshot())
	if string(ja) != string(jb) {
		t.Fatalf("読み戻した Sim の進み方が違う:\n%s\n%s", ja, jb)
	}
}

// Sim に欄を足したら persist.go の simState も直す (直し忘れると、その欄だけ黙って引き継がれない)。
func TestSimFieldsArePersisted(t *testing.T) {
	if n, want := reflect.TypeOf(Sim{}).NumField(), reflect.TypeOf(simState{}).NumField(); n != want {
		t.Fatalf("Sim の欄 %d 個と simState の欄 %d 個が合わない。persist.go の Save / Restore を直す", n, want)
	}
}

// ClearDone は完了のカードだけを Archived にする (消さない)。repo を指定したらその repo の分だけ。
func TestClearDoneArchivesOnlyDoneInRepo(t *testing.T) {
	s := New(t0)
	// 見本の完了は dotfiles だけなので、他の repo の完了を 1 枚足す (repo の絞り込みを見るため)
	s.cards = append(s.cards, card.Card{ID: "OB-DONE", Repo: "obaket", State: card.Done, Ending: card.EndAnswered, Since: t0})
	before := s.Snapshot().Cards
	var doneHere, doneOther, notDone int
	for _, c := range before {
		switch {
		case c.State == card.Done && c.Repo == "dotfiles":
			doneHere++
		case c.State == card.Done:
			doneOther++
		default:
			notDone++
		}
	}
	if doneHere == 0 || doneOther == 0 {
		t.Fatalf("見本に dotfiles と他の repo の完了カードが要る: dotfiles %d / 他 %d", doneHere, doneOther)
	}
	msg, err := s.Apply(backend.ClearDone{Repo: "dotfiles"})
	if err != nil || !strings.Contains(msg, "片付けた") {
		t.Fatalf("片付けの応答: %q %v", msg, err)
	}
	after := s.Snapshot().Cards
	if len(after) != len(before) {
		t.Fatalf("カードが消えた (片付けは Archived にするだけ): %d → %d", len(before), len(after))
	}
	for _, c := range after {
		want := c.State == card.Done && c.Repo == "dotfiles"
		if c.Archived != want {
			t.Fatalf("%s (%v, %s) の Archived=%v (期待 %v)", c.ID, c.State, c.Repo, c.Archived, want)
		}
	}
	if vs := s.Snapshot().Violations; len(vs) != 0 {
		t.Fatalf("片付けで不変条件が破れた: %v", vs)
	}
	if msg, _ := s.Apply(backend.ClearDone{Repo: "dotfiles"}); !strings.Contains(msg, "無い") {
		t.Fatalf("2 回目は片付けるものが無いはず: %q", msg)
	}
	// 状態ファイルにも残る (再起動で片付けたカードが戻らない / 記録が消えない)
	b, err := s.Save()
	if err != nil {
		t.Fatal(err)
	}
	r := New(t0)
	if err := r.Restore(b); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range r.Snapshot().Cards {
		if c.Archived {
			n++
		}
	}
	if n != doneHere {
		t.Fatalf("復元後の片付け済み %d 枚 (期待 %d)", n, doneHere)
	}
}
