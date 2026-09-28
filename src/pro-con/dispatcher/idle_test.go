package dispatcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/live"
	"pro-con/store"
)

// quietRig は一覧を取った回数を数える dispatcher (時計は手で進める)。
type quietRig struct {
	d     *Dispatcher
	dir   string
	now   time.Time
	lists int
	// lastListed は最後の Tick で一覧を取ったか
	lastListed bool
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
	r.tickNotes(t)
	return r.lastListed
}

// tickNotes は時計を 3 秒進めずに 1 回 Tick し、その Tick の出来事を返す (一覧を取ったかは lastListed)。
func (r *quietRig) tickNotes(t *testing.T) []eventlog.Event {
	t.Helper()
	before := r.lists
	notes, err := r.d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r.lastListed = r.lists > before
	return notes
}

// tickNotes30 は時計を 30 秒進めて Tick する (QuietListEvery 経った Tick)。
func (r *quietRig) tickNotes30(t *testing.T) []eventlog.Event {
	t.Helper()
	r.now = r.now.Add(30 * time.Second)
	notes := r.tickNotes(t)
	if !r.lastListed {
		t.Fatal("前提: QuietListEvery 経った Tick で一覧を取った")
	}
	return notes
}

func hasText(notes []eventlog.Event, s string) bool {
	for _, n := range notes {
		if strings.Contains(n.Reason, s) {
			return true
		}
	}
	return false
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
		{"PM が生きていて見張れない (JobsDir が無い。入力待ちと落ちたのを一覧で見張る)", nil, &store.PMState{Session: "pm1"}, false},
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

// pmWatchRig は生きている PM (pid つき) が居て、カードが全部完了している dispatcher (issue 576)。
type pmWatchRig struct {
	*quietRig
	jobs  string
	ss    []agents.Session
	alive map[int]bool
	// noRow は、settle で PM の行が記録に載ったことを求めない (止めた PM は載らない)
	noRow bool
}

func newPMWatchRig(t *testing.T) *pmWatchRig {
	t.Helper()
	r := &pmWatchRig{quietRig: newQuietRig(t, doneCard("C-001")), jobs: t.TempDir(), alive: map[int]bool{4242: true}}
	// 起動 (LaunchedAt) の後に始まった session: 記録 (registry) に PM の行が載る本番の形 (前に始まった session は別物の疑いで載せない)
	r.ss = []agents.Session{{ID: "pm1", SessionID: "sid-pm1", Kind: "background", Status: agents.StatusIdle, State: "working", PID: 4242,
		StartedAt: r.now.Add(-30 * time.Minute).UnixMilli()}}
	r.d.PMRepo, r.d.IntegratorOff, r.d.JobsDir = "/repo", true, r.jobs
	r.d.Exists = func(string) bool { return true }
	r.d.Alive = func(pid int) bool { return r.alive[pid] }
	r.d.List = func(context.Context) ([]agents.Session, error) { r.lists++; return r.ss, nil }
	if err := store.SaveRole(r.dir, store.PMStateFile, store.PMState{Session: "pm1", LaunchedAt: r.now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	r.writeJob(t, `{"state":"working"}`)
	return r
}

func (r *pmWatchRig) writeJob(t *testing.T, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(r.jobs, "pm1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.jobs, "pm1", "state.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// settle は見張りが始まるまで Tick する (照らして控え、暇と分かった Tick の後は間引く)。
func (r *pmWatchRig) settle(t *testing.T) {
	t.Helper()
	defer func() {
		if t.Failed() || r.noRow {
			return
		}
		reg, err := live.LoadRegistry(filepath.Join(r.dir, live.RegistryFile))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := roleRow(reg, pmRole.cardID); !ok {
			t.Fatal("前提: PM の session が記録 (registry) に載った (本番の形)")
		}
	}()
	for i := range 5 {
		if !r.tickAt(t, 3*time.Second) {
			return
		}
		if i == 4 {
			t.Fatal("生きている PM を見張れる形なのに、5 Tick 続けて一覧を取った")
		}
	}
}

// 生きている PM を見張っている間は一覧を間引き、様子が動いた合図 (state.json の書き換え) で取り直す。
func TestWatchedPMThinsOutListUntilJobChanges(t *testing.T) {
	r := newPMWatchRig(t)
	r.settle(t)
	for i := range 5 {
		if r.tickAt(t, 3*time.Second) {
			t.Fatalf("見張っている間の %d 回目の Tick で一覧を取った (合図は無い)", i+1)
		}
	}
	if got := r.d.roleState(pmRole, r.now).Phase; got != card.RoleIdle {
		t.Fatalf("間引いた Tick の PM の様子 = %v (待機中 のまま出すはず)", got)
	}
	r.ss[0].Status, r.ss[0].WaitingFor = agents.StatusWaiting, "permission prompt"
	r.writeJob(t, `{"state":"blocked","detail":"permission prompt"}`)
	r.now = r.now.Add(3 * time.Second)
	notes := r.tickNotes(t)
	if !r.lastListed {
		t.Fatal("state.json が変わった Tick で一覧を取らない")
	}
	if !hasText(notes, "入力待ち") {
		t.Fatalf("取り直した Tick で PM の入力待ちを知らせない: %v", notes)
	}
	if hasText(notes, "state.json が変わっていない") {
		t.Fatalf("合図で取り直した Tick を、合図が鳴らない形と読んだ: %v", notes)
	}
	if r.tickAt(t, 3*time.Second) {
		t.Fatal("取り直した後、合図が無いのに一覧を取り続ける")
	}
}

// 見張っている PM が落ちたら (pid が居ない)、次の Tick で一覧を取り、落ちた時刻を付けて毎 Tick 取る側に戻る (終了が自動の再開を待てるように)。
func TestWatchedPMDeathListsAndStopsWatching(t *testing.T) {
	r := newPMWatchRig(t)
	r.settle(t)
	r.alive[4242] = false
	r.ss[0].PID, r.ss[0].Status = 0, ""
	if !r.tickAt(t, 3*time.Second) {
		t.Fatal("PM の pid が居なくなった Tick で一覧を取らない")
	}
	pm, err := store.LoadRole(r.dir, store.PMStateFile, pmRole.name)
	if err != nil {
		t.Fatal(err)
	}
	if pm.DeadSince.IsZero() {
		t.Fatal("落ちた PM に DeadSince を付けない")
	}
	for i := range 3 {
		if !r.tickAt(t, 3*time.Second) {
			t.Fatalf("落ちた PM が居る間の %d 回目の Tick で一覧を間引いた (自動の再開は一覧でしか分からない)", i+1)
		}
	}
}

// 見張っている間でも、QuietListEvery 経ったら一覧を取る。
func TestWatchedPMStillListsEveryQuietInterval(t *testing.T) {
	r := newPMWatchRig(t)
	r.settle(t)
	n := r.lists
	for range 10 { // 30 秒
		r.tickAt(t, 3*time.Second)
	}
	if r.lists != n+1 {
		t.Fatalf("見張っている 30 秒の間に一覧を %d 回取った (1 回のはず)", r.lists-n)
	}
}

// state.json が変わらないまま status が変わったら (合図が鳴らない形)、1 度だけ suspect の出来事にする。
func TestWatchedPMSilentStatusChangeIsSuspected(t *testing.T) {
	r := newPMWatchRig(t)
	r.settle(t)
	r.ss[0].Status = agents.StatusBusy // state.json は書き換えない
	var suspects int
	for range 25 { // 75 秒 (QuietListEvery を 2 回越える)
		r.now = r.now.Add(3 * time.Second)
		for _, n := range r.tickNotes(t) {
			if n.Kind == eventlog.KindSuspect && strings.Contains(n.Reason, "state.json が変わっていない") {
				suspects++
			}
		}
		if r.lastListed { // 一覧を取るたびに違う status を見せる (Tick ごとに変えると、取る間隔によっては同じ status に戻って見える)
			r.ss[0].Status = map[string]string{agents.StatusBusy: agents.StatusIdle, agents.StatusIdle: agents.StatusBusy}[r.ss[0].Status]
		}
	}
	if suspects != 1 {
		t.Fatalf("合図が鳴らない status の変化を %d 回出来事にした (1 回のはず)", suspects)
	}
}

// 一覧を取っている間に state.json が変わったら (取る前に控えるので)、次の Tick で取り直す。
func TestWatchedPMJobChangeDuringListIsNotLost(t *testing.T) {
	r := newPMWatchRig(t)
	r.settle(t)
	r.tickAt(t, 27*time.Second) // QuietListEvery 経った Tick で取る
	if !r.lastListed {
		t.Fatal("前提: QuietListEvery 経った Tick で一覧を取った")
	}
	list := r.d.List
	r.d.List = func(ctx context.Context) ([]agents.Session, error) {
		ss, err := list(ctx)
		r.writeJob(t, `{"state":"blocked","detail":"permission prompt"}`) // 一覧を返した後に書き換わる
		r.ss[0].Status = agents.StatusWaiting                             // 次の一覧では入力待ち
		return ss, err
	}
	notes := r.tickNotes30(t)
	r.d.List = list
	if !r.lastListed {
		t.Fatal("前提: QuietListEvery 経った Tick で一覧を取った")
	}
	r.now = r.now.Add(3 * time.Second)
	notes = append(notes, r.tickNotes(t)...)
	if !r.lastListed {
		t.Fatal("一覧を取っている間に state.json が変わったのに、次の Tick で取り直さない")
	}
	if hasText(notes, "state.json が変わっていない") {
		t.Fatalf("取っている間に書き換わった state.json を、合図が鳴らない形と読んだ: %v", notes)
	}
}

// 一覧を取れなかった Tick の後は、見張りをやめて次の Tick で取り直す。
func TestWatchedPMListErrorRetriesNextTick(t *testing.T) {
	r := newPMWatchRig(t)
	r.settle(t)
	r.tickAt(t, 27*time.Second)
	list := r.d.List
	r.d.List = func(context.Context) ([]agents.Session, error) {
		r.lists++
		return nil, errors.New("claude が落ちた")
	}
	if !r.tickAt(t, 30*time.Second) {
		t.Fatal("前提: QuietListEvery 経った Tick で一覧を取りに行った")
	}
	r.d.List = list
	r.ss[0].Status = agents.StatusBusy // state.json は書き換えない (見張りを外した後の取り直しで控え直すので、合図が鳴らない形とは読まない)
	r.now = r.now.Add(3 * time.Second)
	notes := r.tickNotes(t)
	if !r.lastListed {
		t.Fatal("一覧を取れなかった Tick の次で取り直さない (前の見張りのまま間引いた)")
	}
	if hasText(notes, "state.json が変わっていない") {
		t.Fatalf("見張りを外した後の取り直しで、合図が鳴らない形と読んだ: %v", notes)
	}
}

// 役を照らせなかった Tick (記録を読めず tellRole が様子を決める前に抜けた) の後は、前の様子で見張らない (次の Tick も一覧を取る)。
func TestWatchedPMUnreadableRegistryStopsWatching(t *testing.T) {
	r := newPMWatchRig(t)
	r.settle(t)
	if err := os.WriteFile(filepath.Join(r.dir, live.RegistryFile), []byte("{壊れた"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.now = r.now.Add(30 * time.Second)
	before := r.lists
	if _, err := r.d.Tick(context.Background()); err == nil || r.lists == before {
		t.Fatalf("前提: QuietListEvery 経った Tick で一覧を取り、記録を読めずに抜けた (err = %v)", err)
	}
	if err := os.Remove(filepath.Join(r.dir, live.RegistryFile)); err != nil {
		t.Fatal(err)
	}
	if !r.tickAt(t, 3*time.Second) {
		t.Fatal("PM を照らせなかった Tick の後も、前の様子のまま見張って一覧を間引いた")
	}
}

// 止めた PM の行が一覧に pid 無しで残っていても (止め損ねた等)、見張りの合図にしない (毎 Tick 取り続けない)。
func TestStoppedPMLeftInListDoesNotKeepListing(t *testing.T) {
	r := newPMWatchRig(t)
	if err := store.SaveRole(r.dir, store.PMStateFile, store.PMState{Session: "pm1", Stopped: true, LaunchedAt: r.now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	r.ss[0].PID, r.ss[0].Status = 0, ""
	r.noRow = true
	r.settle(t)
	n := r.lists
	for range 15 { // 45 秒 (QuietListEvery の取得をまたぐ。見張りの控えはその取得で付く)
		r.tickAt(t, 3*time.Second)
	}
	if got := r.lists - n; got != 1 {
		t.Fatalf("止めた PM の行が残っている 45 秒の間に一覧を %d 回取った (QuietListEvery の 1 回のはず)", got)
	}
}

// state.json が無い役 (claude の版で置き場所が変わった等) は見張れない: pid だけで見張ると入力待ちの知らせが遅れるので、毎 Tick 取る。
func TestPMWithoutJobFileIsNotWatched(t *testing.T) {
	r := newPMWatchRig(t)
	if err := os.RemoveAll(filepath.Join(r.jobs, "pm1")); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		if !r.tickAt(t, 3*time.Second) {
			t.Fatalf("state.json の無い PM が生きている間の %d 回目の Tick で一覧を間引いた", i+1)
		}
	}
}

// 同じ長さの書き換え (中身の長さが変わらない) でも、mtime が進めば合図にする。
func TestWatchedPMSameSizeRewriteIsNoticed(t *testing.T) {
	r := newPMWatchRig(t)
	r.settle(t)
	p := filepath.Join(r.jobs, "pm1", "state.json")
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	r.writeJob(t, `{"state":"blocked"}`) // `{"state":"working"}` と同じ長さ (19 バイト)
	if err := os.Chtimes(p, fi.ModTime().Add(time.Second), fi.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if fi2, err := os.Stat(p); err != nil || fi2.Size() != fi.Size() {
		t.Fatalf("前提: 同じ長さに書き換えた (%v, %v)", fi2, err)
	}
	if !r.tickAt(t, 3*time.Second) {
		t.Fatal("同じ長さで書き換わった state.json を合図にしない")
	}
}

// pidAlive の本物の経路 (kill(pid, 0)): 自分のプロセスは居る、居ない pid と 0 は居ない。
func TestPidAliveUsesKill(t *testing.T) {
	d := &Dispatcher{}
	if !d.pidAlive(os.Getpid()) {
		t.Fatal("自分のプロセスを居ないと読んだ")
	}
	if d.pidAlive(0) || d.pidAlive(-1) {
		t.Fatal("pid 0 / 負の pid を居ると読んだ (kill(0, 0) はプロセスグループに届く)")
	}
	if d.pidAlive(99999999) { // macOS の pid の上限 (99998) より大きい
		t.Fatal("居ない pid を居ると読んだ")
	}
}
