package doctorview

import (
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
