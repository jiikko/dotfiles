package doctorview

import (
	"doctor/disk"
	"path/filepath"
	"testing"
)

// 確認画面が約束した対象と、`y` が実際に消す対象が一致すること。
//
// 🚨 P1 の形: 下見 (DryRun) が**部分的に**中断されると、2 件目以降が OutcomeFailed になる。
// confirmLines は Failed を解放量に足さない (それ自体は正しい) のに、以前の `case "y"` は
// v.selectedResults() をそのまま engine へ渡していた。selectedResults が見るのは**元の走査結果**
// だけで下見の結末を見ないので、**確認画面に出した量より多く消える**。
// 破壊的操作の同意を、実際より小さい数字を見せて取る形だった。
func TestDeleteTargetsComeFromPlanNotSelection(t *testing.T) {
	planned := disk.EntryOutcome{ID: "thing", Label: "消せる", Method: "rm", Outcome: disk.OutcomePlanned,
		Items: []disk.ItemOutcome{{Path: "/x/thing/a", Outcome: disk.OutcomePlanned}}}
	aborted := disk.EntryOutcome{ID: "other", Label: "中断で落ちた", Outcome: disk.OutcomeFailed,
		Reason: "中断されました"}
	thing := disk.Result{Entry: disk.Entry{ID: "thing"}, Items: []disk.Item{{Path: "/x/thing/a"}}}
	withItem := func(id string) disk.Result {
		return disk.Result{Entry: disk.Entry{ID: id}, Items: []disk.Item{{Path: "/x/" + id + "/a"}}}
	}

	for _, tc := range []struct {
		name    string
		entries []disk.EntryOutcome
		sel     []disk.Result
		want    []string
	}{
		{
			name:    "中断で Failed になったエントリは y の対象から外れる",
			entries: []disk.EntryOutcome{planned, aborted},
			sel:     []disk.Result{thing, withItem("other")},
			want:    []string{"thing"},
		},
		{
			name:    "Planned だけなら選択そのままが対象",
			entries: []disk.EntryOutcome{planned},
			sel:     []disk.Result{thing},
			want:    []string{"thing"},
		},
		{
			name:    "Skipped も対象にしない (消えないものを消しに行かない)",
			entries: []disk.EntryOutcome{{ID: "gone", Outcome: disk.OutcomeSkipped}},
			sel:     []disk.Result{withItem("gone")},
			want:    nil,
		},
		{
			name:    "plan に無い ID が選択に居ても対象にしない",
			entries: []disk.EntryOutcome{planned},
			sel:     []disk.Result{thing, withItem("not-in-plan")},
			want:    []string{"thing"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := &disk.DeleteReport{DryRun: true, Entries: tc.entries}
			got := plannedTargets(plan, tc.sel)
			ids := make([]string, 0, len(got))
			for _, r := range got {
				ids = append(ids, r.Entry.ID)
			}
			if len(ids) != len(tc.want) {
				t.Fatalf("対象 = %v, want %v", ids, tc.want)
			}
			for i := range ids {
				if ids[i] != tc.want[i] {
					t.Errorf("対象[%d] = %q, want %q (全体 %v)", i, ids[i], tc.want[i], ids)
				}
			}
		})
	}
}

// plan が nil / 空でも y が対象を作らない (「消せるものがありません」へ落ちる)。
func TestPlannedTargetsIsEmptyWithoutPlan(t *testing.T) {
	sel := []disk.Result{{Entry: disk.Entry{ID: "thing"}}}
	if got := plannedTargets(nil, sel); len(got) != 0 {
		t.Errorf("plan が nil なのに対象を作った: %d 件", len(got))
	}
	if got := plannedTargets(&disk.DeleteReport{}, sel); len(got) != 0 {
		t.Errorf("plan が空なのに対象を作った: %d 件", len(got))
	}
}

// 🚨 **planHasWork と plannedTargets が同じ述語を見ていること**。
// 「消せるものが在るか」と「何を消すか」を別ソースにしない。確認画面が何を約束しても
// 実行が従わない構造になる。片方だけ直すとまた割れるので、両方を同じ入力で突き合わせる。
func TestPlanHasWorkAgreesWithPlannedTargets(t *testing.T) {
	sel := []disk.Result{
		{Entry: disk.Entry{ID: "a"}, Items: []disk.Item{{Path: "/x/a"}}}, {Entry: disk.Entry{ID: "b"}, Items: []disk.Item{{Path: "/x/b"}}},
	}
	planned := func(id string) disk.EntryOutcome {
		return disk.EntryOutcome{ID: id, Method: "rm", Outcome: disk.OutcomePlanned,
			Items: []disk.ItemOutcome{{Path: "/x/" + id, Outcome: disk.OutcomePlanned}}}
	}
	for _, tc := range []struct {
		name    string
		entries []disk.EntryOutcome
	}{
		{"全部 Planned", []disk.EntryOutcome{planned("a"), planned("b")}},
		{"片方が Failed", []disk.EntryOutcome{
			planned("a"), {ID: "b", Outcome: disk.OutcomeFailed}}},
		{"全部 Failed", []disk.EntryOutcome{
			{ID: "a", Outcome: disk.OutcomeFailed}, {ID: "b", Outcome: disk.OutcomeFailed}}},
		{"全部 Skipped", []disk.EntryOutcome{
			{ID: "a", Outcome: disk.OutcomeSkipped}, {ID: "b", Outcome: disk.OutcomeSkipped}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := &disk.DeleteReport{Entries: tc.entries}
			hasWork := planHasWork(plan)
			n := len(plannedTargets(plan, sel))
			if hasWork != (n > 0) {
				t.Errorf("planHasWork=%v なのに対象が %d 件 (確認と実行が別ソースになっている)", hasWork, n)
			}
		})
	}
}

// plannedTargets が依存している前提: **plan のエントリは必ず ID を持つ**。
// planDelete / intent が ID を落とすと、フィルタが全部落として削除が一切走らなくなる
// (倒れる向きは安全側だが、無言で何も起きない形になる)。前提を pin しておく。
func TestPlanEntriesAlwaysCarryID(t *testing.T) {
	res := disk.Result{
		Entry:  disk.Entry{ID: "some-id", Label: "ラベル", Risk: disk.RiskSafe, DeleteVia: "rm"},
		Status: disk.StatusOK, Items: []disk.Item{{Path: t.TempDir(), Size: 1}},
	}
	rep, _ := disk.Delete(t.Context(), []disk.Result{res}, disk.DeleteOptions{DryRun: true})
	if len(rep.Entries) != 1 {
		t.Fatalf("下見のエントリが %d 件 (1 件のはず)", len(rep.Entries))
	}
	if rep.Entries[0].ID != "some-id" {
		t.Errorf("下見のエントリに ID が無い: %+v。plannedTargets が全対象を落とし、削除が無言で走らなくなる", rep.Entries[0])
	}
}

// 🚨 **`case "y"` の経路で engine が受け取る対象**を見る。
//
// plannedTargets の単体テストだけでは足りない: `y` が plannedTargets を**呼ばない**形へ戻す
// 変異を素通りさせる。同じ穴を踏みやすいので、
// production の入口を通す形で固定する。
func TestDeleteKeyYPassesOnlyPlannedTargetsToEngine(t *testing.T) {
	f := &fakeDelete{phases: []disk.DeletePhase{disk.PhaseScanning}}
	v := deleteTestView(t, f)
	v.HandleKey(" ", 20) // thing を選ぶ
	// 下見: thing は Planned、other は中断で Failed
	f.rep = disk.DeleteReport{DryRun: true, Entries: []disk.EntryOutcome{
		{ID: "thing", Label: "Thing キャッシュ", Method: "rm", Outcome: disk.OutcomePlanned,
			BeforeSize: 4096, Items: plannedItemsOf(t, v, "thing")},
		{ID: "other", Label: "中断で落ちた", Outcome: disk.OutcomeFailed, Reason: "中断されました"},
	}}
	v.HandleKey("d", 20) // 下見
	_ = runDeleteCmds(t, v, v.TakeDeleteCmd())

	// 選択には plan で Failed だったエントリも混ざっている状態を作る
	// (実際は下見の中断でこうなる。UI の選択は元の走査結果しか見ない)。
	// 🚨 currentDiskResults は diskRep があればそちらを見るので **そちらへ足す**
	// (diskResults へ足しても効かない。最初それで前提が作れず、下の assert に止められた)
	other := disk.Result{
		Entry:  disk.Entry{ID: "other", Label: "中断で落ちた", Risk: disk.RiskSafe, DeleteVia: "rm"},
		Status: disk.StatusOK, Size: 1 << 30,
		Items: []disk.Item{{Path: filepath.Join(t.TempDir(), "other"), Size: 1 << 30}},
	}
	if v.diskRep != nil {
		*v.diskRep = v.diskRep.WithResults(append(v.diskRep.Results, other))
	} else {
		v.diskResults = append(v.diskResults, other)
	}
	v.selected["other"] = true
	if got := len(v.selectedResults()); got != 2 {
		t.Fatalf("前提が作れていない: 選択 %d 件 (2 件にならないと、この test は何も守らない)", got)
	}

	if act := v.HandleKey("y", 20); act != DoctorRunDelete {
		t.Fatalf("y で本番が始まらない: act = %v", act)
	}
	_ = runDeleteCmds(t, v, v.TakeDeleteCmd())

	// 本番 (2 回目の呼び出し) が受け取った対象を見る
	got := f.targetIDs(1)
	if len(got) != 1 || got[0] != "thing" {
		t.Errorf("engine が受け取った対象 = %v, want [thing] (確認画面に出していない対象まで消しに行っている)", got)
	}
}

// 下見で外れた Item は、エントリが Planned でも y で本番へ渡さない。本番の Delete は渡された Items から計画を
// 作り直すので、渡すと確認画面で「省く」と出した Item が、本番までに候補へ戻ったときに消える。
func TestDeleteKeyYPassesOnlyItemsTheConfirmShowed(t *testing.T) {
	f := &fakeDelete{phases: []disk.DeletePhase{disk.PhaseScanning}}
	v := deleteTestView(t, f)
	v.HandleKey(" ", 20) // thing を選ぶ
	dropped := disk.Item{Path: filepath.Join(t.TempDir(), "dropped"), Size: 1 << 20}
	if v.diskRep == nil {
		t.Fatal("前提が崩れている: 走査の結果が無い")
	}
	results := append([]disk.Result(nil), v.diskRep.Results...)
	for i, r := range results {
		if r.Entry.ID == "thing" {
			results[i] = r.WithItems(append(append([]disk.Item(nil), r.Items...), dropped))
		}
	}
	*v.diskRep = v.diskRep.WithResults(results)
	kept := plannedItemsOf(t, v, "thing")
	if len(kept) < 2 {
		t.Fatalf("前提が崩れている: thing の Item が %d 件 (2 件以上にならないと、この test は何も守らない)", len(kept))
	}
	kept[len(kept)-1].Outcome, kept[len(kept)-1].Reason = disk.OutcomeFailed, "いまは削除の候補ではありません"
	f.rep = disk.DeleteReport{DryRun: true, Entries: []disk.EntryOutcome{
		{ID: "thing", Label: "Thing キャッシュ", Method: "rm", Outcome: disk.OutcomePlanned, BeforeSize: 4096, Items: kept}}}
	v.HandleKey("d", 20)
	_ = runDeleteCmds(t, v, v.TakeDeleteCmd())

	if act := v.HandleKey("y", 20); act != DoctorRunDelete {
		t.Fatalf("y で本番が始まらない: act = %v", act)
	}
	_ = runDeleteCmds(t, v, v.TakeDeleteCmd())
	if len(f.targets) < 2 || len(f.targets[1]) != 1 {
		t.Fatalf("本番へ渡した対象 = %+v", f.targets)
	}
	for _, it := range f.targets[1][0].Items {
		if it.Path == dropped.Path {
			t.Errorf("確認画面で外れた Item %s を本番へ渡した (渡した Items: %+v)", dropped.Path, f.targets[1][0].Items)
		}
	}
	if got := len(f.targets[1][0].Items); got != len(kept)-1 {
		t.Errorf("本番へ渡した Item が %d 件 (確認画面に出した %d 件のはず)", got, len(kept)-1)
	}
}
