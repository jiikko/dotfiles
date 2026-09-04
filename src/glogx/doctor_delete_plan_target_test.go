package main

import (
	"path/filepath"
	"testing"

	"doctor/disk"
)

// 確認画面が約束した対象と、`y` が実際に消す対象が一致すること (issue 245)。
//
// 🚨 P1 の形: 下見 (DryRun) が**部分的に**中断されると、2 件目以降が OutcomeFailed になる。
// confirmLines は Failed を解放量に足さない (それ自体は正しい) のに、以前の `case "y"` は
// v.selectedResults() をそのまま engine へ渡していた。selectedResults が見るのは**元の走査結果**
// だけで下見の結末を見ないので、**確認画面に出した量より多く消える**。
// 破壊的操作の同意を、実際より小さい数字を見せて取る形だった。
func TestDeleteTargetsComeFromPlanNotSelection(t *testing.T) {
	planned := disk.EntryOutcome{ID: "thing", Label: "消せる", Outcome: disk.OutcomePlanned}
	aborted := disk.EntryOutcome{ID: "other", Label: "中断で落ちた", Outcome: disk.OutcomeFailed,
		Reason: "中断されました"}

	for _, tc := range []struct {
		name    string
		entries []disk.EntryOutcome
		sel     []disk.Result
		want    []string
	}{
		{
			name:    "中断で Failed になったエントリは y の対象から外れる",
			entries: []disk.EntryOutcome{planned, aborted},
			sel: []disk.Result{
				{Entry: disk.Entry{ID: "thing"}}, {Entry: disk.Entry{ID: "other"}},
			},
			want: []string{"thing"},
		},
		{
			name:    "Planned だけなら選択そのままが対象",
			entries: []disk.EntryOutcome{planned},
			sel:     []disk.Result{{Entry: disk.Entry{ID: "thing"}}},
			want:    []string{"thing"},
		},
		{
			name:    "Skipped も対象にしない (消えないものを消しに行かない)",
			entries: []disk.EntryOutcome{{ID: "gone", Outcome: disk.OutcomeSkipped}},
			sel:     []disk.Result{{Entry: disk.Entry{ID: "gone"}}},
			want:    nil,
		},
		{
			name:    "plan に無い ID が選択に居ても対象にしない",
			entries: []disk.EntryOutcome{planned},
			sel: []disk.Result{
				{Entry: disk.Entry{ID: "thing"}}, {Entry: disk.Entry{ID: "not-in-plan"}},
			},
			want: []string{"thing"},
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
// 以前は「消せるものが在るか」と「何を消すか」が別ソースで、確認画面が何を約束しても
// 実行が従わない構造だった。片方だけ直すとまた割れるので、両方を同じ入力で突き合わせる。
func TestPlanHasWorkAgreesWithPlannedTargets(t *testing.T) {
	sel := []disk.Result{
		{Entry: disk.Entry{ID: "a"}}, {Entry: disk.Entry{ID: "b"}},
	}
	for _, tc := range []struct {
		name    string
		entries []disk.EntryOutcome
	}{
		{"全部 Planned", []disk.EntryOutcome{
			{ID: "a", Outcome: disk.OutcomePlanned}, {ID: "b", Outcome: disk.OutcomePlanned}}},
		{"片方が Failed", []disk.EntryOutcome{
			{ID: "a", Outcome: disk.OutcomePlanned}, {ID: "b", Outcome: disk.OutcomeFailed}}},
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

// 🚨 **`case "y"` の経路で engine が受け取る対象**を見る (issue 245)。
//
// plannedTargets の単体テストだけでは足りない: `y` が plannedTargets を**呼ばない**形へ戻す
// 変異 (= この issue の症状そのもの) を素通りさせる。issue 217 で同じ穴を踏んだので、
// production の入口を通す形で固定する。
func TestDeleteKeyYPassesOnlyPlannedTargetsToEngine(t *testing.T) {
	f := &fakeDelete{
		phases: []disk.DeletePhase{disk.PhaseScanning},
		// 下見: thing は Planned、other は中断で Failed
		rep: disk.DeleteReport{DryRun: true, Entries: []disk.EntryOutcome{
			{ID: "thing", Label: "Thing キャッシュ", Outcome: disk.OutcomePlanned,
				BeforeSize: 4096, Items: make([]disk.ItemOutcome, 1)},
			{ID: "other", Label: "中断で落ちた", Outcome: disk.OutcomeFailed, Reason: "中断されました"},
		}},
	}
	v := deleteTestView(t, f)
	v.handleKey(" ", 20) // thing を選ぶ
	v.handleKey("d", 20) // 下見
	_ = runDeleteCmds(t, v, v.takeDeleteCmd())

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
		v.diskRep.Results = append(v.diskRep.Results, other)
	} else {
		v.diskResults = append(v.diskResults, other)
	}
	v.selected["other"] = true
	if got := len(v.selectedResults()); got != 2 {
		t.Fatalf("前提が作れていない: 選択 %d 件 (2 件にならないと、この test は何も守らない)", got)
	}

	if act := v.handleKey("y", 20); act != doctorRunDelete {
		t.Fatalf("y で本番が始まらない: act = %v", act)
	}
	_ = runDeleteCmds(t, v, v.takeDeleteCmd())

	// 本番 (2 回目の呼び出し) が受け取った対象を見る
	got := f.targetIDs(1)
	if len(got) != 1 || got[0] != "thing" {
		t.Errorf("engine が受け取った対象 = %v, want [thing] (確認画面に出していない対象まで消しに行っている)", got)
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
