package main

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
)

// fakeDelete は disk.Delete の差し替え。呼ばれた引数を記録し、進捗を流してから rep を返す。
type fakeDelete struct {
	calls   []disk.DeleteOptions
	targets [][]disk.Result
	rep     disk.DeleteReport
	err     error
	phases  []disk.DeletePhase // 流す進捗
}

func (f *fakeDelete) fn(_ context.Context, targets []disk.Result, opt disk.DeleteOptions) (disk.DeleteReport, error) {
	f.calls = append(f.calls, opt)
	f.targets = append(f.targets, targets)
	for i, p := range f.phases {
		opt.OnPhase(i, len(f.phases), "テスト対象", p)
	}
	return f.rep, f.err
}

// deleteTestView は doctor を開いて 1 件を選べる状態にし、削除だけ fake に差し替える。
func deleteTestView(t *testing.T, f *fakeDelete) *doctorView {
	t.Helper()
	v := doctorTestView(t)
	v.deleteFn = f.fn
	v.deleteOpts = func() disk.DeleteOptions { return disk.DeleteOptions{} }
	runDoctorCmds(t, v, v.open())
	_ = v.lines(doctorTestOpts(30))
	return v
}

// runDeleteCmds は削除の Cmd を回して進捗 / 完了を view へ届ける。
//
// ⚠️ **Batch の中身は並行に走らせる**。本番の bubbletea はそうするし、逐次に回すと
// waitDeleteCmd が channel 待ちに入って producer が動かず、そのまま止まる。
// Msg の取り込み (receiveDelete) はこの goroutine だけが行う (Update の外で状態を触らない)。
func runDeleteCmds(t *testing.T, v *doctorView, cmd tea.Cmd) {
	t.Helper()
	msgs := make(chan tea.Msg, 64)
	var start func(tea.Cmd)
	start = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			switch m := c().(type) {
			case tea.BatchMsg:
				for _, sub := range m {
					start(sub)
				}
			case nil:
			default:
				msgs <- m
			}
		}()
	}
	start(cmd)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case m := <-msgs:
			dm, ok := m.(doctorDeleteMsg)
			if !ok {
				t.Fatalf("知らない Msg: %T", m)
			}
			start(v.receiveDelete(dm))
			if !v.del.blocking() {
				return // 確認 / 結果 / エラーに落ちた (待ち続けている waiter は捨ててよい)
			}
		case <-deadline:
			t.Fatalf("削除が終わらない: %+v", v.del)
		}
	}
}

func doctorPanelText(v *doctorView, page int) string {
	return strings.Join(v.lines(doctorTestOpts(page)), "\n")
}

// Space で選び、行と hint に出る。
func TestDoctorSelectAndHint(t *testing.T) {
	v := deleteTestView(t, &fakeDelete{})
	if act := v.handleKey(" ", 20); act != doctorSwallow {
		t.Fatalf("Space の結果 = %v", act)
	}
	if !v.selected["thing"] {
		t.Fatal("選択されていない")
	}
	out := doctorPanelText(v, 30)
	if !strings.Contains(out, "*") {
		t.Errorf("選択マークが出ていない:\n%s", out)
	}
	if h := v.hint(120); !strings.Contains(h, "選択 1 件") {
		t.Errorf("hint に選択の件数が無い: %q", h)
	}
	v.handleKey(" ", 20)
	if v.selected["thing"] {
		t.Error("もう一度押しても外れない")
	}
}

// 走査していない行 / 走査できていない行は選べない (engine も拒否するが、押す前に理由を出す)。
func TestDoctorSelectRefusesUnscannedRows(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*disk.Result)
		want string
	}{
		{"FromSnapshot", func(r *disk.Result) { r.FromSnapshot = true }, "前回の結果"},
		{"Reused", func(r *disk.Result) { r.Reused = true }, "前回の計測"},
		{"failed", func(r *disk.Result) { r.Status = disk.StatusFailed }, "走査できていない"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := deleteTestView(t, &fakeDelete{})
			tc.mut(&v.diskRep.Results[0])
			_ = v.lines(doctorTestOpts(30))
			if act := v.handleKey(" ", 20); act != doctorToast {
				t.Fatalf("結果 = %v (理由を出すこと)", act)
			}
			if !strings.Contains(v.pendingToast, tc.want) {
				t.Errorf("理由 = %q, want %q を含む", v.pendingToast, tc.want)
			}
			if v.selected["thing"] {
				t.Error("選べてしまった")
			}
		})
	}
}

// 対象が 0 件の行は選べない (行そのものが一覧に出ないので、判定を単体で見る)。
func TestDoctorDeletableRejectsEmptyItems(t *testing.T) {
	v := &doctorView{}
	r := disk.Result{Status: disk.StatusOK, Entry: disk.Entry{ID: "x"}}
	if ok, why := v.deletable(r); ok || !strings.Contains(why, "削除対象はありません") {
		t.Errorf("ok=%v why=%q", ok, why)
	}
}

// risk: confirm の行は、中身を一度見るまで選べない。
func TestDoctorSelectRequiresInspectForConfirmRisk(t *testing.T) {
	v := deleteTestView(t, &fakeDelete{})
	v.diskRep.Results[0].Entry.Risk = disk.RiskConfirm
	v.diskRep.Results[0].Entry.Inspect = true
	_ = v.lines(doctorTestOpts(30))
	if act := v.handleKey(" ", 20); act != doctorToast || !strings.Contains(v.pendingToast, "中身を確認") {
		t.Fatalf("act=%v toast=%q", act, v.pendingToast)
	}
	v.handleKey("enter", 20) // 中身を開く
	_ = v.lines(doctorTestOpts(30))
	if act := v.handleKey(" ", 20); act != doctorSwallow || !v.selected["thing"] {
		t.Fatalf("中身を見た後も選べない: act=%v selected=%v", act, v.selected)
	}
}

// d は選択が無いと理由を出し、engine を呼ばない。
func TestDoctorDeleteWithoutSelection(t *testing.T) {
	f := &fakeDelete{}
	v := deleteTestView(t, f)
	if act := v.handleKey("d", 20); act != doctorToast || !strings.Contains(v.pendingToast, "Space") {
		t.Fatalf("act=%v toast=%q", act, v.pendingToast)
	}
	if len(f.calls) != 0 {
		t.Error("engine を呼んだ")
	}
}

// d は下見 (DryRun) を走らせ、その結果をそのまま確認プロンプトに出す。
func TestDoctorDeleteShowsDryRunInConfirm(t *testing.T) {
	f := &fakeDelete{
		phases: []disk.DeletePhase{disk.PhaseScanning},
		rep: disk.DeleteReport{DryRun: true, Entries: []disk.EntryOutcome{
			{ID: "thing", Label: "Thing キャッシュ", Method: "rm", Outcome: disk.OutcomePlanned,
				BeforeSize: 4096, Items: make([]disk.ItemOutcome, 1)}}},
	}
	v := deleteTestView(t, f)
	v.handleKey(" ", 20)
	if act := v.handleKey("d", 20); act != doctorRunDelete {
		t.Fatalf("act = %v", act)
	}
	runDeleteCmds(t, v, v.takeDeleteCmd())
	if len(f.calls) != 1 || !f.calls[0].DryRun {
		t.Fatalf("下見を DryRun で呼んでいない: %+v", f.calls)
	}
	if !v.del.confirm {
		t.Fatalf("確認プロンプトが出ていない: %+v", v.del)
	}
	out := doctorPanelText(v, 30)
	for _, want := range []string{"本当に削除しますか?", "Thing キャッシュ", "4.0KB", "y: 削除する"} {
		if !strings.Contains(out, want) {
			t.Errorf("確認に %q が無い:\n%s", want, out)
		}
	}
}

// 確認で n / Esc は中止し、engine を 2 度と呼ばない。
func TestDoctorDeleteConfirmCancel(t *testing.T) {
	for _, key := range []string{"n", "esc", "q"} {
		t.Run(key, func(t *testing.T) {
			f := &fakeDelete{rep: disk.DeleteReport{DryRun: true}}
			v := deleteTestView(t, f)
			v.handleKey(" ", 20)
			v.handleKey("d", 20)
			runDeleteCmds(t, v, v.takeDeleteCmd())
			v.handleKey(key, 20)
			if v.del.active() {
				t.Fatalf("中止したのに状態が残っている: %+v", v.del)
			}
			if len(f.calls) != 1 {
				t.Fatalf("engine を %d 回呼んだ (下見の 1 回だけのはず)", len(f.calls))
			}
		})
	}
}

// 確認で y は本番の削除を走らせ、結果を出す。incomplete は成功にも失敗にも畳まない。
func TestDoctorDeleteRunsAndShowsResult(t *testing.T) {
	f := &fakeDelete{
		phases: []disk.DeletePhase{disk.PhaseScanning, disk.PhaseDeleting, disk.PhaseVerifying},
		rep: disk.DeleteReport{Entries: []disk.EntryOutcome{
			{Label: "Thing キャッシュ", Outcome: disk.OutcomePlanned, BeforeSize: 4096, Items: make([]disk.ItemOutcome, 1)}}},
	}
	v := deleteTestView(t, f)
	v.handleKey(" ", 20)
	v.handleKey("d", 20)
	runDeleteCmds(t, v, v.takeDeleteCmd())

	f.rep = disk.DeleteReport{Freed: 4096, HistoryPath: "/tmp/h.json",
		Entries: []disk.EntryOutcome{
			{Label: "Thing キャッシュ", Outcome: disk.OutcomeIncomplete, Reason: "1 件が残っています"}}}
	if act := v.handleKey("y", 20); act != doctorRunDelete {
		t.Fatalf("act = %v", act)
	}
	runDeleteCmds(t, v, v.takeDeleteCmd())
	if len(f.calls) != 2 || f.calls[1].DryRun {
		t.Fatalf("本番を DryRun でない形で呼んでいない: %+v", f.calls)
	}
	if v.del.result == nil {
		t.Fatalf("結果が出ていない: %+v", v.del)
	}
	out := doctorPanelText(v, 30)
	for _, want := range []string{"削除の結果", "🚨 未完了", "1 件が残っています", "解放しました: 4.0KB", "/tmp/h.json"} {
		if !strings.Contains(out, want) {
			t.Errorf("結果に %q が無い:\n%s", want, out)
		}
	}
	// どのキーでも閉じ、実体に合わせて再スキャンする
	if act := v.handleKey("j", 20); act != doctorRescan {
		t.Fatalf("結果を閉じたら再スキャン: act = %v", act)
	}
	if v.del.active() {
		t.Error("閉じたのに状態が残っている")
	}
}

// 実行中はキーを飲む (別の行へ移動できない / 閉じられない)。Ctrl-C は 2 回目で中断する。
func TestDoctorDeleteBlocksKeysWhileRunning(t *testing.T) {
	v := deleteTestView(t, &fakeDelete{})
	v.del = doctorDelete{running: true, progress: "1/1 …"}
	cursor := v.cursor
	for _, key := range []string{"j", "k", "d", " ", "enter", "q", "esc", "D", "r"} {
		if act := v.handleKey(key, 20); act != doctorSwallow {
			t.Errorf("%q が飲まれていない: act = %v", key, act)
		}
	}
	if v.cursor != cursor {
		t.Error("実行中にカーソルが動いた")
	}
	if !v.del.running {
		t.Fatal("実行中の状態が壊れた")
	}
	if v.handleKey("ctrl+c", 20); !v.del.armedCC {
		t.Error("1 回目の Ctrl-C で武装していない")
	}
	if v.del.cancel != nil {
		t.Skip("cancel は startDelete が入れる (この経路では nil)")
	}
}

// 削除の語彙が立っているあいだは C / X (自己更新) にキーを渡さない。
func TestDoctorOwnsKeysDuringDelete(t *testing.T) {
	v := deleteTestView(t, &fakeDelete{})
	if v.ownsKeys() {
		t.Fatal("平常時に owns している")
	}
	for _, st := range []doctorDelete{{confirm: true}, {running: true}, {preparing: true},
		{result: &disk.DeleteReport{}}, {err: "x"}} {
		v.del = st
		if !v.ownsKeys() {
			t.Errorf("%+v で owns していない", st)
		}
	}
}

// 選択は開き直しで消える (前回の選択が残ったまま d を押せると事故になる)。
func TestDoctorSelectionClearedOnReopen(t *testing.T) {
	v := deleteTestView(t, &fakeDelete{})
	v.handleKey(" ", 20)
	if len(v.selected) != 1 {
		t.Fatal("前提が崩れている")
	}
	runDoctorCmds(t, v, v.rescan())
	if len(v.selected) != 0 {
		t.Errorf("開き直しても選択が残っている: %v", v.selected)
	}
}

// 🚨 パネルが画面に入らなくても、操作の説明 (y/N) は必ず残る。
// padTo は溢れた分を切るので、素直に並べると「操作が分からない確認プロンプト」になる。
func TestDoctorDeletePanelKeepsPromptWhenCramped(t *testing.T) {
	entries := make([]disk.EntryOutcome, 0, 12)
	for i := range 12 {
		entries = append(entries, disk.EntryOutcome{Label: "エントリ", Method: "rm",
			Outcome: disk.OutcomePlanned, BeforeSize: int64(i+1) * 1024, Items: make([]disk.ItemOutcome, 1)})
	}
	v := &doctorView{del: doctorDelete{confirm: true, plan: &disk.DeleteReport{Entries: entries}}}
	for _, page := range []int{6, 10, 24} {
		out := strings.Join(v.lines(doctorTestOpts(page)), "\n")
		if !strings.Contains(out, "y: 削除する") {
			t.Errorf("page=%d で操作の説明が消えた:\n%s", page, out)
		}
		if n := len(v.lines(doctorTestOpts(page))); n != page {
			t.Errorf("page=%d なのに %d 行", page, n)
		}
	}
}
