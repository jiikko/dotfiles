package dispatcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/live"
	"pro-con/metrics"
	"pro-con/store"
)

func metricRows(t *testing.T, dir string) map[string]metrics.Row {
	t.Helper()
	rows, err := store.LoadMetrics(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]metrics.Row{}
	for _, r := range rows {
		out[r.Card] = r
	}
	return out
}

// カードを閉じると所要の行が書かれ、1 週間の削除 (497) の後も残る。90 日たつと消え、消した件数が出来事の記録に残る。
// 時計は Now を差し替えて進める (実時間を待たない)。
func TestClosedCardMetricsOutliveWeekPurgeAndExpireAt90Days(t *testing.T) {
	dir := t.TempDir()
	if err := store.Update(dir, func(s *store.State) error {
		c := card.Card{ID: "C-001", Title: "回答だけ", Repo: "dotfiles", State: card.Requested, Since: t0, History: []card.Event{{At: t0, Text: "依頼を受けた"}}}
		c.Mark(t0)
		s.NextID, s.Cards = 2, []card.Card{c}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Submit(dir, store.Request{Kind: "close", CardID: "C-001", Ending: card.EndAnswered}); err != nil {
		t.Fatal(err)
	}
	now := t0.Add(20 * time.Minute)
	var recorded []eventlog.Event
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	d.PMOff, d.Now, d.Record = true, func() time.Time { return now }, func(evs []eventlog.Event) { recorded = append(recorded, evs...) }
	tick := func() {
		t.Helper()
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	tick()
	r, ok := metricRows(t, dir)["C-001"]
	if !ok || r.Ending != "回答済み" || !r.ClosedAt.Equal(now) || r.Stay == nil || r.Stay.Requested != 20*60 {
		t.Fatalf("閉じたカードの行 = %+v (書いた: %v)", r, ok)
	}
	if r.Usage != nil || r.UsageMissing != "PG を起こしていない" {
		t.Fatalf("PG の無いカードの枠を 0 にした: %+v %q", r.Usage, r.UsageMissing)
	}
	tick() // 同じカードを 2 度書かない
	if data, _ := os.ReadFile(filepath.Join(dir, store.MetricsFile)); countLines(data) != 1 {
		t.Fatalf("同じカードの行を重ねて書いた:\n%s", data)
	}

	now = t0.Add(store.PurgeAfter + 2*time.Hour) // 書庫へ移り、1 週間の削除で書庫からも消える
	d.purgedAt = time.Time{}
	tick()
	d.purgedAt = time.Time{}
	tick()
	if _, found, _ := store.Find(dir, "C-001"); found {
		t.Fatal("1 週間たってもカードが残っている (前提が崩れた)")
	}
	if _, ok := metricRows(t, dir)["C-001"]; !ok {
		t.Fatal("1 週間の削除で所要の行まで消えた")
	}

	now = r.ClosedAt.Add(store.MetricsKeep)
	d.purgedAt = time.Time{}
	recorded = nil
	tick()
	if _, ok := metricRows(t, dir)["C-001"]; ok {
		t.Fatal("90 日たった行が残った")
	}
	if !hasNote(recorded, eventlog.KindArchive, "90 日たった 1 行を消す") {
		t.Fatalf("消した件数を出来事の記録に残さない: %v", recorded)
	}
}

// writeTranscript は PG の transcript を書く (応答は 1 行ずつ。形は metrics/usage_test.go と同じ)。
func writeTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	data := ""
	for _, l := range lines {
		data += l + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func response(id string, in, out int64) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"id":%q,"model":"claude-opus-5-5","usage":{"input_tokens":%d,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":%d}}}`, id, in, out)
}

func countLines(data []byte) int {
	n := 0
	for _, b := range data {
		if b == '\n' {
			n++
		}
	}
	return n
}

// PG の枠は、そのカードの session (今の分と再開で退いた分) の transcript から数える。transcript が無いカードは 0 ではなく
// 「取れなかった」(Usage が nil で理由つき) になる。
func TestMetricsUsageFromTranscriptsOrMissing(t *testing.T) {
	dir := t.TempDir()
	done := func(id string) card.Card {
		c := card.Card{ID: id, Title: id, Repo: "dotfiles", State: card.Done, Session: "short-" + id, Since: t0,
			Issues: []card.IssueRef{{Repo: "dotfiles", Number: 1}}, History: []card.Event{{At: t0.Add(-time.Hour), Text: "依頼を受けた"}}}
		return c
	}
	if err := store.Update(dir, func(s *store.State) error {
		s.NextID, s.Cards = 3, []card.Card{done("C-001"), done("C-002")}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reg := filepath.Join(dir, live.RegistryFile)
	for _, o := range []live.Owned{{ID: "a1", SessionID: "s-old", PID: 1, CardID: "C-001"}, {ID: "a2", SessionID: "s-new", PID: 2, CardID: "C-001"},
		{ID: "b1", SessionID: "s-gone", PID: 3, CardID: "C-002"}} {
		if err := live.ReplaceCard(reg, o); err != nil {
			t.Fatal(err)
		}
	}
	tr := t.TempDir()
	writeTranscript(t, filepath.Join(tr, "s-old.jsonl"), response("m1", 10, 100))
	writeTranscript(t, filepath.Join(tr, "s-new.jsonl"), response("m1", 10, 100), response("m2", 5, 50)) // m1 は写し
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	d.PMOff = true
	d.TranscriptPath = func(sid string) (string, error) {
		p := filepath.Join(tr, sid+".jsonl")
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("探した: %w", os.ErrNotExist)
		}
		return p, nil
	}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows := metricRows(t, dir)
	if u := rows["C-001"].Usage; u == nil || u.Responses != 2 || u.Input != 15 || u.Output != 150 || u.USD == nil {
		t.Fatalf("C-001 の枠 = %+v %q", u, rows["C-001"].UsageMissing)
	}
	if r := rows["C-002"]; r.Usage != nil || r.UsageMissing != "transcript が無い (session s-gone)" {
		t.Fatalf("transcript の無いカードの枠 = %+v %q", r.Usage, r.UsageMissing)
	}
	if r := rows["C-001"]; r.Ending != metrics.EndIssue || r.Stay != nil || !r.RequestedAt.Equal(t0.Add(-time.Hour)) {
		t.Fatalf("足跡の無いカードの行 = %+v", r)
	}
}

// 依頼の列ですぐ消した削除も、PG を止めてから消した削除も、終わり方「削除」の行を残す。
func TestDeletedCardsLeaveMetrics(t *testing.T) {
	r := newCrashRig(t) // C-001 が作業中・登録済み
	if _, err := store.Submit(r.dir, store.Request{Kind: "add", Title: "すぐ消す"}); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	st, _ := store.Load(r.dir)
	var fresh string
	for _, c := range st.Cards {
		if c.State == card.Requested {
			fresh = c.ID
		}
	}
	for _, id := range []string{fresh, "C-001"} {
		if _, err := store.Submit(r.dir, store.Request{Kind: "delete", CardID: id}); err != nil {
			t.Fatal(err)
		}
	}
	r.tick(t)
	rows := metricRows(t, r.dir)
	for _, id := range []string{fresh, "C-001"} {
		if rows[id].Ending != metrics.EndDeleted {
			t.Fatalf("%s の削除の行 = %+v (全部: %v)", id, rows[id], rows)
		}
	}
	if _, ok := states(t, r.dir)["C-001"]; ok {
		t.Fatal("C-001 が記録に残った (削除の前提が崩れた)")
	}
}

// 書庫に残っている、この issue より前に閉じたカードも、1 週間の削除より先に埋め戻す。
func TestArchiveBackfilledBeforePurge(t *testing.T) {
	dir := t.TempDir()
	old := card.Card{ID: "C-001", Title: "t", Repo: "dotfiles", State: card.Done, Ending: card.EndAnswered, Since: t0.Add(-store.PurgeAfter), Archived: true}
	if err := store.Update(dir, func(s *store.State) error { s.NextID, s.Cards = 2, []card.Card{old}; return nil }); err != nil {
		t.Fatal(err)
	}
	if moved, err := store.Archive(dir, t0); err != nil || len(moved) != 1 { // 所要の記録が無かった頃に書庫へ移ったカード
		t.Fatalf("書庫へ移せない: %v %v", moved, err)
	}
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	d.PMOff = true
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if arch, _ := store.LoadArchive(dir); len(arch) != 0 {
		t.Fatalf("書庫から消していない: %d 枚", len(arch))
	}
	if r, ok := metricRows(t, dir)["C-001"]; !ok || !r.ClosedAt.Equal(old.Since) || r.Ending != "回答済み" {
		t.Fatalf("消す前に埋め戻していない: %+v", r)
	}
}

// 削除の行を書いたが記録から外せずに残ったカードは、dispatcher を起動し直した後でも、完了したら完了の行で書き直す。
func TestDeleteRowReplacedByCloseAfterRestart(t *testing.T) {
	dir := t.TempDir()
	c := card.Card{ID: "C-001", Title: "t", Repo: "dotfiles", State: card.Done, Ending: card.EndAnswered, Since: t0}
	if err := store.Update(dir, func(s *store.State) error { s.NextID, s.Cards = 2, []card.Card{c}; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMetrics(dir, []metrics.Row{{Card: "C-001", Ending: metrics.EndDeleted, ClosedAt: t0.Add(-time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	d := newDispatcher(t, dir, &fakeLauncher{}, nil) // 起動し直した dispatcher (印はファイルから読む)
	d.PMOff = true
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := metricRows(t, dir)["C-001"]; r.Ending != "回答済み" || !r.ClosedAt.Equal(t0) {
		t.Fatalf("削除の行のまま: %+v", r)
	}
}

// 完了の行を書いた後に削除したカードは、削除の行で上書きしない (振り返るのは完了。削除は片付け)。
func TestDeletingClosedCardKeepsCloseRow(t *testing.T) {
	dir := t.TempDir()
	c := card.Card{ID: "C-001", Title: "t", Repo: "dotfiles", State: card.Done, Ending: card.EndAnswered, Since: t0}
	if err := store.Update(dir, func(s *store.State) error { s.NextID, s.Cards = 2, []card.Card{c}; return nil }); err != nil {
		t.Fatal(err)
	}
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	d.PMOff = true
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	deleteCard(t, dir, "C-001")
	for range 2 {
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := states(t, dir)["C-001"]; ok {
		t.Fatal("前提: 削除できていない")
	}
	if r := metricRows(t, dir)["C-001"]; r.Ending != "回答済み" {
		t.Fatalf("完了の行を削除の行で上書きした: %+v", r)
	}
}
