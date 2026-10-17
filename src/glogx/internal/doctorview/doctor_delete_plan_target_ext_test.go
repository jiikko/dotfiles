package doctorview_test

import (
	"path/filepath"
	"testing"

	"doctor/disk"
	"glogx/internal/doctorview"
)

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
	v.HandleKey(" ", 20) // thing を選ぶ
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
	if v.DiskRep != nil {
		*v.DiskRep = v.DiskRep.WithResults(append(v.DiskRep.Results, other))
	} else {
		v.DiskResults = append(v.DiskResults, other)
	}
	v.Selected["other"] = true
	if got := len(v.SelectedResults()); got != 2 {
		t.Fatalf("前提が作れていない: 選択 %d 件 (2 件にならないと、この test は何も守らない)", got)
	}

	if act := v.HandleKey("y", 20); act != doctorview.DoctorRunDelete {
		t.Fatalf("y で本番が始まらない: act = %v", act)
	}
	_ = runDeleteCmds(t, v, v.TakeDeleteCmd())

	// 本番 (2 回目の呼び出し) が受け取った対象を見る
	got := f.targetIDs(1)
	if len(got) != 1 || got[0] != "thing" {
		t.Errorf("engine が受け取った対象 = %v, want [thing] (確認画面に出していない対象まで消しに行っている)", got)
	}
}
