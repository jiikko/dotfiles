package dispatcher

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/live"
	"pro-con/schedule"
	"pro-con/store"
)

// purgeJob は予定の表 (schedule.Jobs) の本物の 1 週間の削除の行 (表と dispatcher の配線ごと試す)。
func purgeJob(t *testing.T) schedule.Job {
	t.Helper()
	i := slices.IndexFunc(schedule.Jobs, func(j schedule.Job) bool { return j.Name == schedule.CardPurge })
	if i < 0 {
		t.Fatal("予定の表に card-purge が無い")
	}
	return schedule.Jobs[i]
}

// 完了から 1 週間たったカードは、予定 (card-purge) の枠が来た Tick が書庫から消し、片付けの印 (worktree・ブランチ・
// 起動の記録の今の分と退いた分の session) を残す。枠の前は書庫を読みに行かない。session・worktree は自動では消さない (起動の記録の行は残る)。
func TestTickPurgesWeekOldCardsWithMark(t *testing.T) {
	dir := t.TempDir()
	old := card.Card{ID: "C-001", Title: "t", Repo: "dotfiles", State: card.Done, Ending: card.EndAnswered, Since: t0.Add(-store.PurgeAfter), Archived: true}
	if err := store.Update(dir, func(s *store.State) error { s.NextID, s.Cards = 2, []card.Card{old}; return nil }); err != nil {
		t.Fatal(err)
	}
	reg := filepath.Join(dir, live.RegistryFile)
	for _, o := range []live.Owned{{ID: "aaaa0001", SessionID: "s-old", PID: 1, CardID: "C-001"}, {ID: "aaaa0002", SessionID: "s-new", PID: 2, CardID: "C-001"}} {
		if err := live.ReplaceCard(reg, o); err != nil { // 2 本目で 1 本目が退いた側へ移る
			t.Fatal(err)
		}
	}
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	d.PMOff = true
	j := purgeJob(t)
	d.Scheduled = []schedule.Job{j} // 子を起こす口 (RunScheduled) は持たない: 中で回す行はそれでも回る
	slot := j.Slot(t0)
	if err := store.SaveScheduleRun(dir, j.Name, store.ScheduleRun{Start: slot.Add(time.Minute), End: slot.Add(time.Minute), RC: 0}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if arch, _ := store.LoadArchive(dir); len(arch) != 1 {
		t.Fatalf("この枠で回し済みなのに消しに行った: 書庫 %d 枚", len(arch))
	}
	if err := store.SaveScheduleRun(dir, j.Name, store.ScheduleRun{Start: slot.Add(-time.Minute), End: slot.Add(-time.Minute), RC: 0}); err != nil {
		t.Fatal(err)
	}
	notes, err := d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if arch, _ := store.LoadArchive(dir); len(arch) != 0 {
		t.Fatalf("書庫に残った: %d 枚", len(arch))
	}
	marks, err := store.LoadPurged(dir)
	m := marks["C-001"]
	var sids []string
	for _, s := range m.Sessions {
		sids = append(sids, s.SessionID)
	}
	slices.Sort(sids)
	if err != nil || m.Worktree != "/w/dotfiles/.claude/worktrees/pc-c-001" || m.Branch != "worktree-pc-c-001" || !slices.Equal(sids, []string{"s-new", "s-old"}) {
		t.Fatalf("片付けの印 = %+v %v", m, err)
	}
	if !hasNote(notes, eventlog.KindArchive, "C-001: "+store.PurgeText) {
		t.Fatalf("消したことを出来事に出さない: %v", notes)
	}
	if rec, err := store.LoadSchedule(dir); err != nil || rec[j.Name].RC != 0 || !rec[j.Name].Start.Equal(t0) || rec[j.Name].End.IsZero() ||
		rec[j.Name].Note != schedule.ResultLine("書庫から消した 1 枚", 0) {
		t.Fatalf("予定の記録 = %+v %v", rec[j.Name], err)
	}
	now, _ := live.LoadRegistry(reg)
	retired, _ := live.LoadRetired(reg)
	if len(now)+len(retired) != 2 {
		t.Fatalf("起動の記録の行を自動で消した: %v / %v", now, retired)
	}
}

// forget の依頼は、挙げた session の行だけを起動の記録 (今の分と退いた分) から消し、片付けの印を消す。
// 挙げていない session (片付けの後に再開した) とほかのカードの行は残す。
func TestTickAppliesForget(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, live.RegistryFile)
	for _, o := range []live.Owned{
		{ID: "aaaa0001", SessionID: "s-old", PID: 1, CardID: "C-001"},
		{ID: "aaaa0002", SessionID: "s-cur", PID: 2, CardID: "C-001"},
		{ID: "aaaa0003", SessionID: "s-other", PID: 3, CardID: "C-002"},
	} {
		if err := live.ReplaceCard(reg, o); err != nil {
			t.Fatal(err)
		}
	}
	if err := live.Register(reg, live.Owned{ID: "aaaa0004", SessionID: "s-later", PID: 4, CardID: "C-001"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(dir, func(s *store.State) error { s.NextID = 3; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, store.PurgeFile), []byte(`{"cardId":"C-001"}`+"\n"+`{"cardId":"C-002"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Submit(dir, store.Request{Kind: store.KindForget, CardID: "C-001", Sessions: []string{"s-old", "s-cur", "s-other"}}); err != nil {
		t.Fatal(err)
	}
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	d.PMOff = true
	notes, err := d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, load := range []func(string) ([]live.Owned, error){live.LoadRegistry, live.LoadRetired} {
		rows, err := load(reg)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range rows {
			left = append(left, o.SessionID)
		}
	}
	slices.Sort(left)
	if !slices.Equal(left, []string{"s-later", "s-other"}) {
		t.Fatalf("残った行 = %v", left)
	}
	if marks, err := store.LoadPurged(dir); err != nil || len(marks) != 1 || marks["C-002"].CardID != "C-002" {
		t.Fatalf("片付けの印 = %v %v (C-001 の印だけを消すはず)", marks, err)
	}
	if !hasNote(notes, eventlog.KindArchive, "C-001: 片付けが済んだので、起動の記録の行 2 本") {
		t.Fatalf("消したことを出来事に出さない: %v", notes)
	}
}
