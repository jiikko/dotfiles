package doctorview_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
	"doctor/svc"
	"glogx/internal/app"
	"glogx/internal/doctorview"
	"glogx/internal/doctorview/doctortest"
)

// fakeDelete は disk.Delete の差し替え。呼ばれた引数を記録し、進捗を流してから rep を返す。
type fakeDelete struct {
	calls   []disk.DeleteOptions
	targets [][]disk.Result
	rep     disk.DeleteReport
	err     error
	phases  []disk.DeletePhase // 流す進捗
	ctxErr  error              // ctx が切れていたときに記録する
}

// fn は engine (disk.Delete) の契約を模す:
//   - OnPhase は nil でも落ちない (本物は DeleteOptions.phase() が nil ガードする)
//   - ctx が切れていたら「触っていない」= 中断として返す (本物は破壊的操作の前に必ず見る)
//   - error を返すのは 1 件も触っていないときだけ (本物の契約。UI の「何も消えていません」の根拠)
func (f *fakeDelete) fn(ctx context.Context, targets []disk.Result, opt disk.DeleteOptions) (disk.DeleteReport, error) {
	f.calls = append(f.calls, opt)
	f.targets = append(f.targets, targets)
	if err := ctx.Err(); err != nil {
		f.ctxErr = err
		return disk.DeleteReport{Entries: []disk.EntryOutcome{
			{Label: "中断", Outcome: disk.OutcomeSkipped, Reason: "中断されました"}}}, nil
	}
	for i, p := range f.phases {
		if opt.OnPhase != nil {
			opt.OnPhase(i, len(f.phases), "テスト対象", p)
		}
	}
	return f.rep, f.err
}

// targetIDs は engine へ渡った対象の ID (この UI 層の唯一の仕事なので必ず assert する)。
func (f *fakeDelete) targetIDs(call int) []string {
	if call >= len(f.targets) {
		return nil
	}
	out := make([]string, 0, len(f.targets[call]))
	for _, r := range f.targets[call] {
		out = append(out, r.Entry.ID)
	}
	return out
}

// deleteTestView は doctor を開いて 1 件を選べる状態にし、削除だけ fake に差し替える。
func deleteTestView(t *testing.T, f *fakeDelete) *doctorview.DoctorView {
	t.Helper()
	v := doctortest.DoctorTestView(t)
	v.DeleteFn = f.fn
	v.DeleteOpts = func() disk.DeleteOptions { return disk.DeleteOptions{} }
	doctortest.RunDoctorCmds(t, v, v.Open())
	_ = v.Lines(doctortest.DoctorTestOpts(30))
	return v
}

// runDeleteCmds は削除の Cmd を回して進捗 / 完了を view へ届ける。
//
// 🚨 **Batch の中身は並行に走らせる**。本番の bubbletea はそうするし、逐次に回すと
// waitDeleteCmd が channel 待ちに入って producer が動かず、そのまま止まる。
// Msg の取り込み (receiveDelete) はこの goroutine だけが行う (Update の外で状態を触らない)。
func runDeleteCmds(t *testing.T, v *doctorview.DoctorView, cmd tea.Cmd) []string {
	t.Helper()
	var progress []string
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
			dm, ok := m.(doctorview.DoctorDeleteMsg)
			if !ok {
				t.Fatalf("知らない Msg: %T", m)
			}
			if p := dm.Ev.Prog; p != nil {
				// 表示と同じ語順で組む (このヘルパーの戻り値を検査するテストがある)
				progress = append(progress, fmt.Sprintf("%d/%d %s を%s", p.I, p.Total, p.Label, doctorview.DoctorPhaseWord(p.Phase)))
			}
			start(v.ReceiveDelete(dm))
			if !v.Del.Blocking() {
				return progress // 確認 / 結果 / エラーに落ちた (待ち続けている waiter は捨ててよい)
			}
		case <-deadline:
			t.Fatalf("削除が終わらない: %+v", v.Del)
			return nil
		}
	}
}

// Space で選び、行と hint に出る。
func TestDoctorSelectAndHint(t *testing.T) {
	v := deleteTestView(t, &fakeDelete{})
	if act := v.HandleKey(" ", 20); act != doctorview.DoctorSwallow {
		t.Fatalf("Space の結果 = %v", act)
	}
	if !v.Selected["thing"] {
		t.Fatal("選択されていない")
	}
	out := doctortest.DoctorPanelText(v, 30)
	if !strings.Contains(out, "*") {
		t.Errorf("選択マークが出ていない:\n%s", out)
	}
	if h := v.Hint(120); !strings.Contains(h, "選択 1 件") {
		t.Errorf("hint に選択の件数が無い: %q", h)
	}
	v.HandleKey(" ", 20)
	if v.Selected["thing"] {
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
			tc.mut(&v.DiskRep.Results[0])
			_ = v.Lines(doctortest.DoctorTestOpts(30))
			if act := v.HandleKey(" ", 20); act != doctorview.DoctorToast {
				t.Fatalf("結果 = %v (理由を出すこと)", act)
			}
			if !strings.Contains(v.PendingToast, tc.want) {
				t.Errorf("理由 = %q, want %q を含む", v.PendingToast, tc.want)
			}
			if v.Selected["thing"] {
				t.Error("選べてしまった")
			}
		})
	}
}

// risk: confirm の行は、中身を一度見るまで選べない。
func TestDoctorSelectRequiresInspectForConfirmRisk(t *testing.T) {
	v := deleteTestView(t, &fakeDelete{})
	v.DiskRep.Results[0].Entry.Risk = disk.RiskConfirm
	v.DiskRep.Results[0].Entry.Inspect = true
	_ = v.Lines(doctortest.DoctorTestOpts(30))
	if act := v.HandleKey(" ", 20); act != doctorview.DoctorToast || !strings.Contains(v.PendingToast, "中身を確認") {
		t.Fatalf("act=%v toast=%q", act, v.PendingToast)
	}
	v.HandleKey("enter", 20) // 中身を開く (カーソルは中の対象パスへ移る)
	_ = v.Lines(doctortest.DoctorTestOpts(30))
	if act := v.HandleKey(" ", 20); act != doctorview.DoctorSwallow || len(v.SelectedItems) == 0 {
		t.Fatalf("中身を見た後も選べない: act=%v selectedItems=%v toast=%q", act, v.SelectedItems, v.PendingToast)
	}
	if got := v.SelectedResults(); len(got) != 1 {
		t.Fatalf("選んだ対象が削除に渡らない: %d 件", len(got))
	}
}

// d は選択が無いと理由を出し、engine を呼ばない。
func TestDoctorDeleteWithoutSelection(t *testing.T) {
	f := &fakeDelete{}
	v := deleteTestView(t, f)
	if act := v.HandleKey("d", 20); act != doctorview.DoctorToast || !strings.Contains(v.PendingToast, "Space") {
		t.Fatalf("act=%v toast=%q", act, v.PendingToast)
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
				BeforeSize: 4096, Items: plannedItemOutcomes(1)}}},
	}
	v := deleteTestView(t, f)
	v.HandleKey(" ", 20)
	if act := v.HandleKey("d", 20); act != doctorview.DoctorRunDelete {
		t.Fatalf("act = %v", act)
	}
	_ = runDeleteCmds(t, v, v.TakeDeleteCmd())
	if len(f.calls) != 1 || !f.calls[0].DryRun {
		t.Fatalf("下見を DryRun で呼んでいない: %+v", f.calls)
	}
	// 🚨 この層の唯一の仕事は「選んだ行を engine に渡す」こと。集合そのものを見る
	// (見ないと、nil を渡す変異が素通りする。敵対レビュー 2026-09-03 の実測)
	if got := f.targetIDs(0); len(got) != 1 || got[0] != "thing" {
		t.Fatalf("engine へ渡した対象 = %v (選んだ 1 件のはず)", got)
	}
	if !v.Del.Confirm {
		t.Fatalf("確認プロンプトが出ていない: %+v", v.Del)
	}
	out := doctortest.DoctorPanelText(v, 30)
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
			f := &fakeDelete{rep: disk.DeleteReport{DryRun: true, Entries: []disk.EntryOutcome{
				{Label: "Thing キャッシュ", Method: "rm", Outcome: disk.OutcomePlanned,
					BeforeSize: 4096, Items: plannedItemOutcomes(1)}}}}
			v := deleteTestView(t, f)
			v.HandleKey(" ", 20)
			v.HandleKey("d", 20)
			_ = runDeleteCmds(t, v, v.TakeDeleteCmd())
			// 中止したら下見の ctx も切る (切らないと leak する)
			cancelled := false
			orig := v.Del.Cancel
			v.Del.Cancel = func() { cancelled = true; orig() }
			v.HandleKey(key, 20)
			if v.Del.Active() {
				t.Fatalf("中止したのに状態が残っている: %+v", v.Del)
			}
			if !cancelled {
				t.Error("中止で ctx を切っていない")
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
			// 🚨 ID を入れる: y の対象は plan の Planned な ID で絞る (issue 245)。
			// planDelete は必ず ID を入れるので、省くと production に無い形になる
			{ID: "thing", Label: "Thing キャッシュ", Outcome: disk.OutcomePlanned, BeforeSize: 4096, Items: plannedItemOutcomes(1)}}},
	}
	v := deleteTestView(t, f)
	v.HandleKey(" ", 20)
	v.HandleKey("d", 20)
	_ = runDeleteCmds(t, v, v.TakeDeleteCmd())

	f.rep = disk.DeleteReport{Freed: 4096, HistoryPath: "/tmp/h.json",
		Entries: []disk.EntryOutcome{
			{Label: "Thing キャッシュ", Outcome: disk.OutcomeIncomplete, Reason: "1 件が残っています"}}}
	if act := v.HandleKey("y", 20); act != doctorview.DoctorRunDelete {
		t.Fatalf("act = %v", act)
	}
	if h := v.Hint(120); !strings.Contains(h, "実行中") {
		t.Errorf("実行中の hint = %q", h)
	}
	prog := runDeleteCmds(t, v, v.TakeDeleteCmd())
	if len(f.calls) != 2 || f.calls[1].DryRun {
		t.Fatalf("本番を DryRun でない形で呼んでいない: %+v", f.calls)
	}
	if got := f.targetIDs(1); len(got) != 1 || got[0] != "thing" {
		t.Fatalf("本番へ渡した対象 = %v", got)
	}
	// 🚨 **相は「最新が届く」が契約で、全部の相が届くことは保証しない** (progCh は
	// 最新優先の 1 バッファ)。engine が速いと中間の相は捨てられるので、prog[0] が
	// 「走査中」であることを固定すると、落としてよいものを固定することになる。
	// ここで守るのは「進捗が届くこと」と「最後は最終相であること」
	if len(prog) == 0 {
		t.Error("進捗が 1 件も届いていない")
	} else if last := prog[len(prog)-1]; !strings.Contains(last, "確認中") && !strings.Contains(last, "削除中") {
		t.Errorf("最後の進捗が最終相になっていない: %q (全体 %v)", last, prog)
	}
	if v.Del.Result == nil {
		t.Fatalf("結果が出ていない: %+v", v.Del)
	}
	out := doctortest.DoctorPanelText(v, 30)
	// 🚨 最後の警告行は **合計行だけを読んで「全部消えた」と読み違えるのを止める**もの
	// (issue 316 で `DeleteReport.HasFailures` を配線した。それまで production 参照 0 件だった)。
	// 合計行「解放しました: 4.0KB」と同じ画面に未完了があることを、両方 assert して固定する。
	for _, want := range []string{"削除の結果", "🚨 未完了", "1 件が残っています", "解放しました: 4.0KB", "/tmp/h.json",
		"🚨 消せなかった / 未完了の項目があります"} {
		if !strings.Contains(out, want) {
			t.Errorf("結果に %q が無い:\n%s", want, out)
		}
	}
	// 🚨 パネルと hint が違うことを言わない。**y / Y は閉じずにコピーする**ので「何かキーを押すと
	// 閉じ」は嘘になる (エラーパネル側と同じ規律。issue 242 の P3-2)
	if h := v.Hint(120); !strings.Contains(out, "y: 出力をコピー") || !strings.Contains(h, "y: 出力をコピー") {
		t.Errorf("結果パネルと hint で案内が食い違う: hint=%q panel=\n%s", h, out)
	}
	// どのキーでも閉じ、実体に合わせて再スキャンする
	if act := v.HandleKey("j", 20); act != doctorview.DoctorRescan {
		t.Fatalf("結果を閉じたら再スキャン: act = %v", act)
	}
	if v.Del.Active() {
		t.Error("閉じたのに状態が残っている")
	}
}

// 実行中はキーを飲む (別の行へ移動できない / 閉じられない)。Ctrl-C は 2 回目で中断する。
func TestDoctorDeleteBlocksKeysWhileRunning(t *testing.T) {
	v := deleteTestView(t, &fakeDelete{})
	v.Del = doctorview.DoctorDelete{Running: true, Progress: doctortest.ProgAt(1, 1, "…", disk.PhaseScanning)}
	cursor := v.Cur.Index
	for _, key := range []string{"j", "k", "d", " ", "enter", "q", "esc", "D", "r"} {
		if act := v.HandleKey(key, 20); act != doctorview.DoctorSwallow {
			t.Errorf("%q が飲まれていない: act = %v", key, act)
		}
	}
	if v.Cur.Index != cursor {
		t.Error("実行中にカーソルが動いた")
	}
	if !v.Del.Running {
		t.Fatal("実行中の状態が壊れた")
	}
	if v.HandleKey("ctrl+c", 20); !v.Del.ArmedCC {
		t.Error("1 回目の Ctrl-C で武装していない")
	}
	// 実行中の hint は「消せる」と読める語彙を出さない
	if h := v.Hint(120); strings.Contains(h, "d: 削除") || strings.Contains(h, "Space: 選択") {
		t.Errorf("実行中の hint に削除の導線が出ている: %q", h)
	}
}

// 削除の語彙が立っているあいだは C / X (自己更新) にキーを渡さない。
func TestDoctorOwnsKeysDuringDelete(t *testing.T) {
	v := deleteTestView(t, &fakeDelete{})
	if v.OwnsKeys() {
		t.Fatal("平常時に owns している")
	}
	for _, st := range []doctorview.DoctorDelete{{Confirm: true}, {Running: true}, {Preparing: true},
		{Result: &disk.DeleteReport{}}, {Err: "x"}} {
		v.Del = st
		if !v.OwnsKeys() {
			t.Errorf("%+v で owns していない", st)
		}
	}
}

// 選択は開き直しで消える (前回の選択が残ったまま d を押せると事故になる)。
func TestDoctorSelectionClearedOnReopen(t *testing.T) {
	v := deleteTestView(t, &fakeDelete{})
	v.HandleKey(" ", 20)
	if len(v.Selected) != 1 {
		t.Fatal("前提が崩れている")
	}
	doctortest.RunDoctorCmds(t, v, v.Rescan())
	if len(v.Selected) != 0 {
		t.Errorf("開き直しても選択が残っている: %v", v.Selected)
	}
}

// 🚨 パネルが画面に入らなくても、操作の説明 (y/N) は必ず残る。
// padTo は溢れた分を切るので、素直に並べると「操作が分からない確認プロンプト」になる。
func TestDoctorDeletePanelKeepsPromptWhenCramped(t *testing.T) {
	entries := make([]disk.EntryOutcome, 0, 12)
	for i := range 12 {
		entries = append(entries, disk.EntryOutcome{Label: "エントリ", Method: "rm",
			Outcome: disk.OutcomePlanned, BeforeSize: int64(i+1) * 1024, Items: plannedItemOutcomes(1)})
	}
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &disk.DeleteReport{Entries: entries}}}
	for _, page := range []int{6, 10, 24} {
		out := strings.Join(v.Lines(doctortest.DoctorTestOpts(page)), "\n")
		if !strings.Contains(out, "y: 削除する") {
			t.Errorf("page=%d で操作の説明が消えた:\n%s", page, out)
		}
		if n := len(v.Lines(doctortest.DoctorTestOpts(page))); n != page {
			t.Errorf("page=%d なのに %d 行", page, n)
		}
	}
}

// 下見の最中に押した Ctrl-C が、本番の 1 回目で即中断に化けない。
func TestDeleteArmedCCNotCarriedIntoRun(t *testing.T) {
	f := &fakeDelete{rep: disk.DeleteReport{DryRun: true}}
	v := deleteTestView(t, f)
	v.HandleKey(" ", 20)
	v.HandleKey("d", 20)
	v.Del.ArmedCC = true // 下見の最中に 1 回押した
	_ = runDeleteCmds(t, v, v.TakeDeleteCmd())
	v.HandleKey("y", 20)
	if v.Del.ArmedCC {
		t.Error("下見の armedCC を本番へ持ち越した (1 回目の Ctrl-C で即中断になる)")
	}
	if v.Del.Confirm {
		t.Error("実行中なのに confirm が立ったまま (相が 2 つ)")
	}
}

// y は入口と同じガードをもう一度通す (入口だけの検査は非対称)。
func TestDeleteConfirmRechecksDeletable(t *testing.T) {
	// 下見に「消すもの」があること = 確認プロンプトが y を受ける状態にしておく
	f := &fakeDelete{rep: disk.DeleteReport{DryRun: true, Entries: []disk.EntryOutcome{
		{Label: "Thing キャッシュ", Method: "rm", Outcome: disk.OutcomePlanned,
			BeforeSize: 4096, Items: plannedItemOutcomes(1)}}}}
	v := deleteTestView(t, f)
	v.HandleKey(" ", 20)
	v.HandleKey("d", 20)
	_ = runDeleteCmds(t, v, v.TakeDeleteCmd())
	v.DiskRep.Results[0].FromSnapshot = true // 確認中に「走査していない」印が立った
	if act := v.HandleKey("y", 20); act != doctorview.DoctorToast {
		t.Fatalf("act = %v (理由を出して止めること)", act)
	}
	if len(f.calls) != 1 {
		t.Errorf("engine を %d 回呼んだ (下見の 1 回だけのはず)", len(f.calls))
	}
}

// Inspect の行は、中身の一覧が無くても**開けば何かが出る** (何も出ないと、
// 中身が無いのか走査が拾えなかったのかが区別できないまま選べてしまう)。
func TestDiskDetailShowsPathsWhenNoContents(t *testing.T) {
	v := &doctorview.DoctorView{}
	r := disk.Result{Status: disk.StatusOK, Entry: disk.Entry{ID: "x", Inspect: true, DeleteVia: "trash"},
		Items: []disk.Item{{Path: "/tmp/a", Size: 1024}}}
	out := strings.Join(rowTexts(v.DiskDetail(doctortest.DoctorTestOpts(30), r)), "\n")
	if !strings.Contains(out, "/tmp/a") {
		t.Errorf("中身が無いときに対象のパスも出ない:\n%s", out)
	}
}

// 🚨 狭い画面でも「合計」は残る (1 件目のサイズだけを見て y を押す形にしない)。
func TestDeleteConfirmKeepsTotalsWhenCramped(t *testing.T) {
	entries := make([]disk.EntryOutcome, 0, 12)
	for range 12 {
		entries = append(entries, disk.EntryOutcome{Label: "エントリ", Method: "rm",
			Outcome: disk.OutcomePlanned, BeforeSize: 1 << 30, Items: plannedItemOutcomes(1)})
	}
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &disk.DeleteReport{Entries: entries}}}
	// 🚨 page は**判別できる値**を入れる。6/10/24 だけだと「末尾を後ろから残す」機構を
	// 丸ごと外しても green だった (敵対レビュー 2026-09-03 の実測: 差が出るのは 4 と 5)
	for _, page := range []int{3, 4, 5, 6, 10, 24} {
		out := strings.Join(v.Lines(doctortest.DoctorTestOpts(page)), "\n")
		// 操作の説明は**どんなに狭くても**残る (page=3 でも)
		if !strings.Contains(out, "y: 削除する") {
			t.Errorf("page=%d で操作の説明が消えた:\n%s", page, out)
		}
		// 合計は 2 行ぶんの余地があれば残る (1 件目のサイズだけを見て y を押す形にしない)
		if page >= 4 && !strings.Contains(out, "解放される見込み") {
			t.Errorf("page=%d で合計が消えた:\n%s", page, out)
		}
	}
}

// d の直前に「走査していない」印が立ったら、押した時点で止める (選択時の検査だけでは非対称)。
func TestDeleteRechecksAtPressTime(t *testing.T) {
	f := &fakeDelete{}
	v := deleteTestView(t, f)
	v.HandleKey(" ", 20)
	v.DiskRep.Results[0].Reused = true // 選んだ後に印が立った
	if act := v.HandleKey("d", 20); act != doctorview.DoctorToast || !strings.Contains(v.PendingToast, "前回の計測") {
		t.Fatalf("act=%v toast=%q", act, v.PendingToast)
	}
	if len(f.calls) != 0 {
		t.Error("engine を呼んだ")
	}
}

// スキャン中は削除を始めない (走査中の結果を対象にしない)。
func TestDeleteRefusedWhileScanning(t *testing.T) {
	f := &fakeDelete{}
	v := doctortest.DoctorTestView(t)
	v.DeleteFn, v.DeleteOpts = f.fn, func() disk.DeleteOptions { return disk.DeleteOptions{} }
	doctorFirstDiskEvent(t, v) // 1 件だけ届いた = 走査中
	_ = v.Lines(doctortest.DoctorTestOpts(30))
	v.Selected = map[doctorview.EntryID]bool{"thing": true}
	if act := v.HandleKey("d", 20); act != doctorview.DoctorToast || !strings.Contains(v.PendingToast, "スキャンが終わるまで") {
		t.Fatalf("act=%v toast=%q", act, v.PendingToast)
	}
	if len(f.calls) != 0 {
		t.Error("走査中に engine を呼んだ")
	}
}

// 進捗は実行中のパネルに出る (出ないと数秒〜数十秒のあいだ無言で固まる)。
func TestDeleteShowsProgressInPanel(t *testing.T) {
	v := deleteTestView(t, &fakeDelete{})
	v.Del = doctorview.DoctorDelete{Running: true, Progress: doctortest.ProgAt(2, 3, "Xcode DerivedData", disk.PhaseDeleting)}
	out := doctortest.DoctorPanelText(v, 20)
	// 🚨 エントリ名と「相 + 経過」は**別の行**に出す。1 行に詰めると狭い端末で
	// term.TruncateDisp が末尾から削り、生存の手がかり (スピナー・経過) が最初に消える
	for _, want := range []string{"削除しています", "2/3  Xcode DerivedData", "削除中", "Ctrl-C"} {
		if !strings.Contains(out, want) {
			t.Errorf("実行中のパネルに %q が無い:\n%s", want, out)
		}
	}
	// 🚨 「含む」ではなく**行の形**で検査する。連結された文字列を Contains で探す書き方だと、
	// あいだに入る文字 (スピナー) を 1 つ書き落とすだけで一致しなくなり、1 行に詰める変異を
	// 素通りさせる (実際に変異検証で素通りした)
	var entryLine string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Xcode DerivedData") {
			entryLine = strings.TrimRight(l, " ")
			break
		}
	}
	if entryLine == "" {
		t.Fatalf("エントリ行が無い:\n%s", out)
	}
	if !strings.HasSuffix(entryLine, "Xcode DerivedData") {
		t.Errorf("エントリ名の行に相や経過が同居している: %q", entryLine)
	}
	v.Del = doctorview.DoctorDelete{Preparing: true, Progress: doctortest.ProgAt(1, 2, "", disk.PhaseScanning)}
	if out := doctortest.DoctorPanelText(v, 20); !strings.Contains(out, "確認しています") || !strings.Contains(out, "1/2") || !strings.Contains(out, "走査中") {
		t.Errorf("下見のパネル:\n%s", out)
	}
}

// engine が error を返したら、その旨と「何も消えていません」を出す
// (engine の契約: error は 1 件も触っていないときだけ)。
func TestDeleteShowsEngineError(t *testing.T) {
	f := &fakeDelete{err: errDeleteTest}
	v := deleteTestView(t, f)
	v.HandleKey(" ", 20)
	v.HandleKey("d", 20)
	_ = runDeleteCmds(t, v, v.TakeDeleteCmd())
	if v.Del.Err == "" {
		t.Fatalf("エラーの相に落ちていない: %+v", v.Del)
	}
	out := doctortest.DoctorPanelText(v, 20)
	for _, want := range []string{"削除できませんでした", "記録を残せない", "何も消えていません", "閉じてもう一度スキャン"} {
		if !strings.Contains(out, want) {
			t.Errorf("エラーのパネルに %q が無い:\n%s", want, out)
		}
	}
	// 🚨 パネルと hint が違うことを言わない。実挙動は doctorview.DoctorRescan (閉じて再スキャン) で、
	// 兄弟の結果パネルは正しく言っているのにエラー経路だけ「閉じる」だった (issue 242 P3-2)
	if h := v.Hint(120); !strings.Contains(h, "閉じてもう一度スキャン") {
		t.Errorf("hint がパネルと違うことを言っている: hint=%q panel=\n%s", h, out)
	}
	if act := v.HandleKey("j", 20); act != doctorview.DoctorRescan || v.Del.Active() {
		t.Errorf("エラーを閉じられない: act=%v del=%+v", act, v.Del)
	}
}

var errDeleteTest = deleteTestError("記録を残せないため中止しました")

type deleteTestError string

func (e deleteTestError) Error() string { return string(e) }

// 中断は ctx で engine へ伝わる (プロセスを殺さない)。
func TestDeleteCancelReachesEngine(t *testing.T) {
	f := &fakeDelete{rep: disk.DeleteReport{DryRun: true, Entries: []disk.EntryOutcome{
		{Label: "Thing キャッシュ", Method: "rm", Outcome: disk.OutcomePlanned, Items: plannedItemOutcomes(1)}}}}
	v := deleteTestView(t, f)
	v.HandleKey(" ", 20)
	v.HandleKey("d", 20)
	cmd := v.TakeDeleteCmd()
	v.Del.Cancel() // 実行の直前に中断された状況
	_ = runDeleteCmds(t, v, cmd)
	if f.ctxErr == nil {
		t.Error("engine に中断が伝わっていない (ctx を渡していない)")
	}
}

// 閉じた後に届いた古い Msg を取り込まない。
func TestDeleteIgnoresStaleMsg(t *testing.T) {
	v := deleteTestView(t, &fakeDelete{})
	v.Del = doctorview.DoctorDelete{Running: true}
	rep := disk.DeleteReport{Freed: 1}
	v.ReceiveDelete(doctorview.DoctorDeleteMsg{Gen: v.Gen + 1, Ev: doctorview.DoctorDeleteEvent{Rep: &rep}})
	if v.Del.Result != nil {
		t.Error("古い世代の Msg を取り込んだ")
	}
	v.Shown = false
	v.ReceiveDelete(doctorview.DoctorDeleteMsg{Gen: v.Gen, Ev: doctorview.DoctorDeleteEvent{Rep: &rep}})
	if v.Del.Result != nil {
		t.Error("閉じた後の Msg を取り込んだ")
	}
}

// 削除の経路ごとに確認の文言が変わる (trash はゴミ箱、cli はコマンド、対象外は理由)。
func TestDeleteConfirmPerMethodLines(t *testing.T) {
	plan := disk.DeleteReport{Entries: []disk.EntryOutcome{
		{Label: "ごみ", Method: "trash", Outcome: disk.OutcomePlanned, BeforeSize: 2048, Items: plannedItemOutcomes(3)},
		{Label: "こまんど", Method: "cli", Command: "go clean -modcache", Outcome: disk.OutcomePlanned,
			BeforeSize: 1024, Items: plannedItemOutcomes(1)},
		{Label: "ていじ", Method: "propose", Outcome: disk.OutcomeProposed},
		{Label: "たいしょうがい", Method: "rm", Outcome: disk.OutcomeSkipped, Reason: "いまは対象外です"},
	}}
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &plan}}
	out := strings.Join(v.Lines(doctortest.DoctorTestOpts(30)), "\n")
	// cli はコマンドの実体を 1 本ずつ出す (件数だけの注記と二重に出さない)
	for _, want := range []string{"3 件をゴミ箱へ移動", "$ go clean -modcache", "1 件にコマンドを実行", "実行しません", "🚫 対象外", "いまは対象外です"} {
		if !strings.Contains(out, want) {
			t.Errorf("確認に %q が無い:\n%s", want, out)
		}
	}
	// ゴミ箱ぶんは「解放される見込み」に足さない (空にするまで容量は戻らない)
	if strings.Contains(out, "解放される見込み: 3.0KB") {
		t.Errorf("ゴミ箱の分を解放量に足している:\n%s", out)
	}
}

// 入りきらないぶんは注記で伝え、**送れることも伝える** (黙って消さない。issue 241)。
func TestDeletePanelElisionNote(t *testing.T) {
	entries := make([]disk.EntryOutcome, 0, 8)
	for range 8 {
		entries = append(entries, disk.EntryOutcome{Label: "え", Method: "rm",
			Outcome: disk.OutcomePlanned, BeforeSize: 1024, Items: plannedItemOutcomes(1)})
	}
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &disk.DeleteReport{Entries: entries}}}
	out := strings.Join(v.Lines(doctortest.DoctorTestOpts(10)), "\n")
	for _, want := range []string{"下に ", " 行", "j/k で送る"} {
		if !strings.Contains(out, want) {
			t.Errorf("入り切らないぶんの案内に %q が無い:\n%s", want, out)
		}
	}
}

// 確認パネルは縦に送れる。**送るキーで確認が閉じない**のが要点 (issue 241):
// 以前は y/Y 以外がすべて中止だったので、対象を見ようとした打鍵で確認ごと無言で消えていた。
func TestDeleteConfirmScrollKeysDoNotCancel(t *testing.T) {
	entries := make([]disk.EntryOutcome, 0, 6)
	for i := range 6 {
		entries = append(entries, disk.EntryOutcome{Label: fmt.Sprintf("え%d", i), Method: "rm",
			Outcome: disk.OutcomePlanned, BeforeSize: 1024, Items: plannedItemOutcomes(2)})
	}
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &disk.DeleteReport{Entries: entries}}}
	first := strings.Join(v.Lines(doctortest.DoctorTestOpts(12)), "\n") // 高さを測らせる (窓は描画で決まる)
	// 🚨 キーごとに「**本当に動くか**」まで見る。confirm が閉じないことだけを見ていたら、
	// scroll() から j/k/ctrl+d/… の case を消す変異が緑のまま通った (敵対レビュー 2026-09-04 の実測)。
	// パネルの注記が "(j/k で送る)" と名指ししているので、動かないのは案内が嘘になる形
	down := []string{"j", "down", "ctrl+d", "pgdown", "G", "end"}
	up := []string{"k", "up", "ctrl+u", "pgup", "g", "home"}
	for _, key := range append(append([]string{}, down...), up...) {
		forward := slicesContains(down, key)
		v.Del.ConfirmScroll.Offset = 0
		if !forward {
			v.Del.ConfirmScroll.Offset = v.Del.ConfirmScroll.Total - v.Del.ConfirmScroll.View // 末尾から戻す
		}
		before := v.Del.ConfirmScroll.Offset
		act, handled := v.HandleDeleteKey(key)
		if !handled {
			t.Fatalf("%q が処理されていない", key)
		}
		if act != doctorview.DoctorSwallow {
			t.Errorf("%q の act = %v; want doctorview.DoctorSwallow (送るだけ。再スキャン等を起こさない)", key, act)
		}
		if !v.Del.Confirm {
			t.Fatalf("%q で確認が閉じた (送るキーは中止に落とさない)", key)
		}
		got := v.Del.ConfirmScroll.Offset
		if forward && got <= before {
			t.Errorf("%q で送れていない: offset %d -> %d", key, before, got)
		}
		if !forward && got >= before {
			t.Errorf("%q で戻れていない: offset %d -> %d", key, before, got)
		}
	}
	// 🚨 判定は**本文の中身**で行う。注記 ("上に N 行") だけを見ると、window が offset を
	// 無視して常に先頭を返す変異でも注記は動くので緑のまま通る (実測 2026-09-04)
	if !strings.Contains(first, "え0") || strings.Contains(first, "え5") {
		t.Fatalf("前提が違う (先頭に え0 が見え、え5 は見えないこと):\n%s", first)
	}
	v.Del.ConfirmScroll.Offset = 0
	_, _ = v.HandleDeleteKey("G")
	last := strings.Join(v.Lines(doctortest.DoctorTestOpts(12)), "\n")
	if !strings.Contains(last, "え5") {
		t.Errorf("末尾まで送ったのに最後のエントリが見えない:\n%s", last)
	}
	if strings.Contains(last, "え0") {
		t.Errorf("末尾まで送ったのに先頭のエントリが残っている (送れていない):\n%s", last)
	}
	if !strings.Contains(last, "上に ") {
		t.Errorf("末尾まで送ったのに「上に N 行」が出ない:\n%s", last)
	}
}

// 送るキー以外は**従来どおり中止**する (既定は安全側。緩めていないことを固定する)。
func TestDeleteConfirmOtherKeysStillCancel(t *testing.T) {
	entries := []disk.EntryOutcome{{Label: "え", Method: "rm", Outcome: disk.OutcomePlanned,
		BeforeSize: 1024, Items: plannedItemOutcomes(1)}}
	for _, key := range []string{"n", "esc", " ", "x"} { // enter は飲む側 (TestDeleteConfirmEnterIsSwallowed)
		v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &disk.DeleteReport{Entries: entries}}}
		if _, handled := v.HandleDeleteKey(key); !handled {
			t.Fatalf("%q が処理されていない", key)
		}
		if v.Del.Confirm {
			t.Errorf("%q で中止しなかった (既定は中止側)", key)
		}
	}
}

// Enter は実行もキャンセルもしない (issue 243)。他の破壊確認 (`y/Enter: 実行`) の手癖で押されるが、
// 削除の既定を実行側へ倒さず、無言で閉じもしない。plan の確認と job コマンドの確認の両方で同じ。
// 「消せるものがありません」の画面だけは「何かキーで戻る」の案内どおり Enter でも戻る。
func TestDeleteConfirmEnterIsSwallowed(t *testing.T) {
	entries := []disk.EntryOutcome{{Label: "え", Method: "rm", Outcome: disk.OutcomePlanned,
		BeforeSize: 1024, Items: plannedItemOutcomes(1)}}
	cases := map[string]doctorview.DoctorDelete{
		"plan":              {Confirm: true, Plan: &disk.DeleteReport{Entries: entries}},
		"doctorview.JobCmd": {Confirm: true, Kind: doctorview.JobCmd},
	}
	for name, del := range cases {
		v := &doctorview.DoctorView{Del: del}
		act, handled := v.HandleDeleteKey("enter")
		if !handled {
			t.Fatalf("%s: enter が処理されていない", name)
		}
		if act != doctorview.DoctorSwallow {
			t.Errorf("%s: enter の act = %v; want doctorview.DoctorSwallow (実行しない)", name, act)
		}
		if !v.Del.Confirm {
			t.Errorf("%s: enter で確認が閉じた (無言のキャンセルにしない)", name)
		}
		if v.PendingDeleteCmd != nil {
			t.Errorf("%s: enter で削除が走った", name)
		}
	}
	// 消せるものが無い画面は「何かキーを押すと戻ります」なので Enter でも戻る
	none := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &disk.DeleteReport{Entries: []disk.EntryOutcome{
		{Label: "え", Method: "rm", Outcome: disk.OutcomeSkipped, Reason: "対象外"}}}}}
	if _, _ = none.HandleDeleteKey("enter"); none.Del.Confirm {
		t.Errorf("消せるものが無い画面で enter が戻らない (案内は「何かキーを押すと戻ります」)")
	}
	// 案内は非対称を名指しする (hint とパネル末尾の両方。片方だけだと「hint と本文が違うことを言う」形)
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &disk.DeleteReport{Entries: entries}}}
	if h := v.Hint(120); !strings.Contains(h, "Enter は何もしない") {
		t.Errorf("hint に Enter の扱いが無い: %q", h)
	}
	if out := strings.Join(v.Lines(doctortest.DoctorTestOpts(20)), "\n"); !strings.Contains(out, "Enter は何もしない") {
		t.Errorf("確認パネルの末尾に Enter の扱いが無い:\n%s", out)
	}
}

// 下見の結果、消せるものが 1 件も無かったら「削除しますか?」と聞かない
// (y に意味が無いのに押させる形にしない)。
func TestDeleteConfirmWhenNothingToDo(t *testing.T) {
	f := &fakeDelete{rep: disk.DeleteReport{DryRun: true, Entries: []disk.EntryOutcome{
		{Label: "Thing キャッシュ", Method: "rm", Outcome: disk.OutcomeSkipped, Reason: "いまは対象外です"}}}}
	v := deleteTestView(t, f)
	v.HandleKey(" ", 20)
	v.HandleKey("d", 20)
	_ = runDeleteCmds(t, v, v.TakeDeleteCmd())
	out := doctortest.DoctorPanelText(v, 20)
	if !strings.Contains(out, "消せるものがありません") || strings.Contains(out, "y: 削除する") {
		t.Errorf("消せるものが無いのに削除を促した:\n%s", out)
	}
	if h := v.Hint(120); strings.Contains(h, "y: 削除する") {
		t.Errorf("hint = %q", h)
	}
	if act := v.HandleKey("y", 20); act != doctorview.DoctorSwallow || v.Del.Active() {
		t.Fatalf("y で戻らない: act=%v del=%+v", act, v.Del)
	}
	if len(f.calls) != 1 {
		t.Errorf("engine を %d 回呼んだ (下見の 1 回だけのはず)", len(f.calls))
	}
}

// 🚨 「前回の結果」を表示している画面で Space / d を押したら、拒否せず**取り直す**。
// 復元した画面は全行が FromSnapshot なので、拒否したままだと「サイズは見えているのに
// 何を選んでも断られる」行き止まりになる (ユーザー報告 2026-09-03)。
func TestDoctorSnapshotScreenRescansInsteadOfRefusing(t *testing.T) {
	for _, key := range []string{" ", "d"} {
		t.Run(key, func(t *testing.T) {
			v := doctortest.DoctorTestView(t)
			doctortest.RunDoctorCmds(t, v, v.Open()) // 1 回走査して snapshot を書く
			_ = v.Lines(doctortest.DoctorTestOpts(30))
			v.Close()
			doctortest.RunDoctorCmds(t, v, v.Open()) // TTL 内なので復元される
			_ = v.Lines(doctortest.DoctorTestOpts(30))
			if v.SnapshotAt.IsZero() {
				t.Fatal("前提が崩れている: snapshot から復元していない")
			}
			if act := v.HandleKey(key, 20); act != doctorview.DoctorRescan {
				t.Fatalf("%q の結果 = %v (取り直すこと)", key, act)
			}
			if got := v.TakeToast(); !strings.Contains(got, "取り直します") {
				t.Errorf("理由を伝えていない: %q", got)
			}
		})
	}
}

// 取り直した後は普通に選べる (取り直しが行き止まりの解消になっている)。
func TestDoctorSelectableAfterRescan(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	doctortest.RunDoctorCmds(t, v, v.Open())
	_ = v.Lines(doctortest.DoctorTestOpts(30))
	v.Close()
	doctortest.RunDoctorCmds(t, v, v.Open())
	_ = v.Lines(doctortest.DoctorTestOpts(30))
	v.HandleKey(" ", 20) // 取り直しへ倒れる
	doctortest.RunDoctorCmds(t, v, v.Rescan())
	_ = v.Lines(doctortest.DoctorTestOpts(30))
	if !v.SnapshotAt.IsZero() {
		t.Fatal("取り直した後も snapshot の印が残っている")
	}
	if act := v.HandleKey(" ", 20); act != doctorview.DoctorSwallow || !v.Selected["thing"] {
		t.Fatalf("取り直した後も選べない: act=%v selected=%v toast=%q", act, v.Selected, v.PendingToast)
	}
}

// rowTexts は行の本文だけを取り出す (詳細行が doctorview.DoctorRow になったため)。
func rowTexts(rows []doctorview.DoctorRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Text)
	}
	return out
}

// multiItemView は 2 つの対象パスを持つエントリ 1 件の doctor。
func multiItemView(t *testing.T, f *fakeDelete) *doctorview.DoctorView {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	for _, name := range []string{"a", "b"} {
		p := filepath.Join(home, "Library", "Caches", "multi", name, "blob")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, 4096), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v := &doctorview.DoctorView{
		DiskOpts: func() disk.Options {
			return disk.Options{
				Env: disk.Env{Home: home, TmpDir: home + "/", Getenv: func(string) string { return "" }},
				Catalog: []disk.Entry{{ID: "multi", Label: "2 つある", Tier: 1, Risk: disk.RiskSafe,
					DeleteVia: "rm", Recover: "x", Paths: []string{"~/Library/Caches/multi/*"}}},
				BootTime: func() (time.Time, error) { return time.Now(), nil },
			}
		},
		SvcOpts: func() svc.Options {
			return svc.Options{Dirs: []svc.LaunchDir{{Path: filepath.Join(home, "LaunchAgents"), Domain: "gui/501"}},
				Run: func(context.Context, string, ...string) (string, string, int, error) {
					return "PID\tStatus\tLabel\n", "", 0, nil
				}}
		},
		BrewRun:    func(context.Context, string, ...string) (string, string, int, error) { return "", "", 0, nil },
		DockerOpts: doctortest.NoDockerOptions, // 本物の docker system df を叩かない (issue 419)
		SsdOpts:    doctortest.FakeSSDOptions(true),
	}
	if f != nil {
		v.DeleteFn = f.fn
		v.DeleteOpts = func() disk.DeleteOptions { return disk.DeleteOptions{} }
	}
	doctortest.RunDoctorCmds(t, v, v.Open())
	_ = v.Lines(doctortest.DoctorTestOpts(40))
	return v
}

// Enter で開くと、カーソルが**中の対象パス**へ移る (j を何度も押さずに消したいものへ行ける)。
func TestDoctorEnterMovesCursorIntoItems(t *testing.T) {
	v := multiItemView(t, nil)
	if !strings.HasPrefix(string(v.Rows[v.Cur.Index].Key), "disk:") {
		t.Fatalf("前提: カーソルがエントリの行にない (%q)", v.Rows[v.Cur.Index].Key)
	}
	v.HandleKey("enter", 20)
	_ = v.Lines(doctortest.DoctorTestOpts(40))
	if !strings.HasPrefix(string(v.Rows[v.Cur.Index].Key), "diskitem:") {
		t.Fatalf("Enter で対象パスへ移らない: cursor=%q", v.Rows[v.Cur.Index].Key)
	}
	// Enter で入って Enter で出る (中に居たまま畳めないと戻れない)
	v.HandleKey("enter", 20)
	_ = v.Lines(doctortest.DoctorTestOpts(40))
	if !strings.HasPrefix(string(v.Rows[v.Cur.Index].Key), "disk:") || v.Expanded["disk:multi"] {
		t.Fatalf("対象パスの行の Enter で畳んで戻らない: cursor=%q expanded=%v",
			v.Rows[v.Cur.Index].Key, v.Expanded)
	}
}

// ディレクトリ単位で選ぶと、そのパスだけが削除に渡る。
func TestDoctorSelectSingleDirectory(t *testing.T) {
	v := multiItemView(t, nil)
	v.HandleKey("enter", 20)
	_ = v.Lines(doctortest.DoctorTestOpts(40))
	picked := v.Rows[v.Cur.Index].Key
	if act := v.HandleKey(" ", 20); act != doctorview.DoctorSwallow {
		t.Fatalf("act=%v toast=%q", act, v.PendingToast)
	}
	got := v.SelectedResults()
	if len(got) != 1 || len(got[0].Items) != 1 {
		t.Fatalf("渡る対象 = %d エントリ / %d 件 (1 エントリ 1 件のはず)", len(got), len(got[0].Items))
	}
	if !strings.HasSuffix(string(picked), got[0].Items[0].Path) {
		t.Errorf("選んだのと違うパスが渡る: key=%q path=%q", picked, got[0].Items[0].Path)
	}
	if got[0].Size != got[0].Items[0].Size {
		t.Errorf("合計を引き直していない: size=%d item=%d", got[0].Size, got[0].Items[0].Size)
	}
	// 行頭の印: エントリは「一部」の +、選んだパスは *
	out := strings.Join(v.Lines(doctortest.DoctorTestOpts(40)), "\n")
	if !strings.Contains(out, "+") || !strings.Contains(out, "*") {
		t.Errorf("選択の印が出ていない:\n%s", out)
	}
}

// エントリ全体を選ぶと、中の個別選択は畳まれる (二重に数えない)。
func TestDoctorEntrySelectionSupersedesItems(t *testing.T) {
	v := multiItemView(t, nil)
	v.HandleKey("enter", 20)
	_ = v.Lines(doctortest.DoctorTestOpts(40))
	v.HandleKey(" ", 20) // 1 件目のパスを選ぶ
	v.HandleKey("enter", 20)
	_ = v.Lines(doctortest.DoctorTestOpts(40)) // エントリの行へ戻る
	v.HandleKey(" ", 20)                       // エントリ全体を選ぶ
	if len(v.SelectedItems) != 0 {
		t.Errorf("個別の選択が残っている: %v", v.SelectedItems)
	}
	got := v.SelectedResults()
	if len(got) != 1 || len(got[0].Items) != 2 {
		t.Fatalf("エントリ全体が渡らない: %d エントリ / %d 件", len(got), len(got[0].Items))
	}
	// 逆向き: 個別に選ぶとエントリ全体の選択は落ちる
	v.HandleKey("enter", 20)
	_ = v.Lines(doctortest.DoctorTestOpts(40))
	v.HandleKey(" ", 20)
	if v.Selected["multi"] {
		t.Error("個別に選んだのにエントリ全体の選択が残っている")
	}
}

// 確認画面は**フルパスで列挙**する。ラベルとサイズだけでは、どのディレクトリが消えるのか
// 分からないまま y を押すことになる (ユーザー要望 2026-09-03)。
func TestDeleteConfirmListsFullPaths(t *testing.T) {
	items := make([]disk.ItemOutcome, 0, 13)
	for i := range 13 {
		items = append(items, disk.ItemOutcome{
			Path: fmt.Sprintf("/Users/koji/Library/Caches/thing/Entry-%d", i), Size: 1 << 20, Outcome: disk.OutcomePlanned})
	}
	plan := disk.DeleteReport{Entries: []disk.EntryOutcome{
		{Label: "たくさんある", Method: "rm", Outcome: disk.OutcomePlanned, BeforeSize: 13 << 20, Items: items}}}
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &plan}}
	out := strings.Join(v.Lines(doctortest.DoctorTestOpts(40)), "\n")
	for _, want := range []string{"/Users/koji/Library/Caches/thing/Entry-0", "/Users/koji/Library/Caches/thing/Entry-9"} {
		if !strings.Contains(out, want) {
			t.Errorf("確認に %q が無い:\n%s", want, out)
		}
	}
	// 1 エントリで画面を埋めない (他のエントリが丸ごと省略されるため)。打ち切りは件数で伝える
	if strings.Contains(out, "Entry-10") {
		t.Errorf("打ち切っていない (1 エントリで画面を埋める):\n%s", out)
	}
	if !strings.Contains(out, "他 3 件") {
		t.Errorf("打ち切った件数を伝えていない:\n%s", out)
	}
}

// 🚨 パスはファイル名由来なので改行が入りうる。確認画面に偽の行を差し込めてはいけない。
func TestDeleteConfirmPathsCannotForgeLines(t *testing.T) {
	plan := disk.DeleteReport{Entries: []disk.EntryOutcome{
		{Label: "細工", Method: "rm", Outcome: disk.OutcomePlanned, BeforeSize: 1024,
			Items: []disk.ItemOutcome{{Path: "/tmp/x\n 何もしません\n y: 何もしない", Size: 1024, Outcome: disk.OutcomePlanned}}}}}
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &plan}}
	lines := v.Lines(doctortest.DoctorTestOpts(40))
	for _, l := range lines {
		if strings.Contains(l, "\n") {
			t.Fatalf("1 行の中に改行が入った: %q", l)
		}
	}
	out := strings.Join(lines, "\n")
	if !strings.Contains(out, "/tmp/x") {
		t.Errorf("パスが出ていない:\n%s", out)
	}
	if strings.Contains(out, "y: 何もしない") && !strings.Contains(out, "/tmp/x 何もしません y: 何もしない") {
		t.Errorf("偽の行を差し込めた:\n%s", out)
	}
}

// 確認画面に「実際に実行するコマンド」が出る (ユーザー要望 2026-09-03)。
// 組み立ては engine が持つので、確認に出した形と実行する形は同じ。
func TestDeleteConfirmListsCommands(t *testing.T) {
	plan := disk.DeleteReport{Entries: []disk.EntryOutcome{
		{Label: "ランタイム", Method: "cli", Command: "xcrun simctl runtime delete <id>",
			Outcome: disk.OutcomePlanned, BeforeSize: 1 << 30,
			Items: []disk.ItemOutcome{
				{Path: "/L/a", Ref: "ABC-1", Outcome: disk.OutcomePlanned},
				{Path: "/L/b", Ref: "DEF-2", Outcome: disk.OutcomePlanned},
			}},
		{Label: "提示だけ", Method: "propose", Command: "sudo rm -rf /x", Outcome: disk.OutcomeProposed},
	}}
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &plan}}
	out := strings.Join(v.Lines(doctortest.DoctorTestOpts(30)), "\n")
	for _, want := range []string{
		"$ xcrun simctl runtime delete ABC-1",
		"$ xcrun simctl runtime delete DEF-2",
		"実行しません。手で叩いてください: sudo rm -rf /x",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("確認に %q が無い:\n%s", want, out)
		}
	}
}

// 実行中の画面にコマンドと stdout / stderr / 終了コードが流れる。
func TestDeleteRunningPanelStreamsCommandOutput(t *testing.T) {
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Running: true, Progress: doctortest.ProgAt(1, 1, "…", disk.PhaseScanning),
		Log: doctorview.CommandLogLines(disk.CommandRecord{Name: "faketool", Args: []string{"purge"},
			RC: 24, Stdout: "done", Stderr: "retry later"})}}
	out := strings.Join(v.Lines(doctortest.DoctorTestOpts(20)), "\n")
	for _, want := range []string{"$ faketool purge", "exit 24", "1| done", "2| retry later"} {
		if !strings.Contains(out, want) {
			t.Errorf("実行中の画面に %q が無い:\n%s", want, out)
		}
	}
}

// blocked の行に削除の案内を出さない。出しておいて Space が断るのは案内が嘘になる。
func TestBlockedRowDoesNotAdvertiseDeletion(t *testing.T) {
	v := &doctorview.DoctorView{}
	r := disk.Result{Status: disk.StatusBlocked, Reason: "Google Chrome 起動中のため対象外",
		Entry: disk.Entry{ID: "chrome-tmp", Label: "Chrome の一時ファイル", DeleteVia: "rm"}}
	if out := strings.Join(rowTexts(v.DiskDetail(doctortest.DoctorTestOpts(40), r)), "\n"); strings.Contains(out, "d で削除") {
		t.Errorf("blocked の行が削除を案内している:\n%s", out)
	}
	// 拒否文も「走査できていない」と混ぜない (blocked は走査できた上で対象外)
	ok, why := v.Deletable(r)
	if ok || !strings.Contains(why, "いまは対象外") || !strings.Contains(why, "Chrome") {
		t.Errorf("ok=%v why=%q (理由まで伝えること)", ok, why)
	}
}

// hint の「選択 N 件」は**ディレクトリ数**で数える (確認画面が Item 数で数えるため)。
func TestSelectionSummaryCountsDirectories(t *testing.T) {
	v := multiItemView(t, nil)
	v.HandleKey(" ", 20) // エントリ全体 (= 中の 2 本)
	n, total := v.SelectionSummary()
	if n != 2 {
		t.Errorf("選択 = %d 件 (ディレクトリ 2 本のはず)", n)
	}
	if total <= 0 {
		t.Errorf("合計 = %d", total)
	}
}

// ディレクトリ行の解説文は、その 1 本についての話にする (エントリ全体の使い回しにしない)。
func TestDiskItemCopyTextIsAboutOnePath(t *testing.T) {
	r := disk.Result{Status: disk.StatusOK, Size: 3 << 20,
		Entry: disk.Entry{ID: "x", Label: "ラベル", Risk: disk.RiskSafe, DeleteVia: "rm", Recover: "戻せます"},
		Items: []disk.Item{{Path: "/a", Size: 1 << 20}, {Path: "/b", Size: 2 << 20}}}
	// 🚨 関数を直接呼ぶだけでは**配線**を守れない (行が別の関数を使っていても green になる)。
	// 行を組み立てて、その行の copyText を見る
	v := &doctorview.DoctorView{}
	rows := v.DiskItemRows(doctortest.DoctorTestOpts(60), r)
	if len(rows) != 2 {
		t.Fatalf("対象パスの行が %d 本", len(rows))
	}
	got := rows[0].CopyText
	if !strings.Contains(got, "/a") || strings.Contains(got, "/b") {
		t.Errorf("他のパスが混ざっている:\n%s", got)
	}
	if !strings.Contains(got, "✅ 安全") {
		t.Errorf("判定が空:\n%s", got)
	}
	if strings.Contains(got, "3.0MB") {
		t.Errorf("エントリ全体の合計が混ざっている:\n%s", got)
	}
}

// 開き直しで世代をまたぐ状態を残さない (1 つでも残すと前の画面の文言・Cmd が混ざる)。
func TestDoctorStartClearsCarryOverState(t *testing.T) {
	v := doctortest.DoctorTestView(t)
	doctortest.RunDoctorCmds(t, v, v.Open())
	_ = v.Lines(doctortest.DoctorTestOpts(30))
	v.PendingToast, v.PendingCopy, v.EnterDetail = "残り", "残り", "disk:thing"
	v.Cur.FellBack = true
	v.PendingDeleteCmd = func() tea.Msg { return nil }
	v.Selected = map[doctorview.EntryID]bool{"thing": true}
	doctortest.RunDoctorCmds(t, v, v.Rescan())
	if v.PendingToast != "" || v.PendingCopy != "" || v.EnterDetail != "" ||
		v.Cur.FellBack || v.PendingDeleteCmd != nil || len(v.Selected) != 0 {
		t.Errorf("前の世代の状態が残っている: toast=%q copy=%q enter=%q fellBack=%v cmd=%v selected=%v",
			v.PendingToast, v.PendingCopy, v.EnterDetail, v.Cur.FellBack, v.PendingDeleteCmd != nil, v.Selected)
	}
}

// Inspect のエントリは**中身の一覧があっても**対象パスを選べる (issue 240)。
// 以前は Contents が非空だと選べる行を出さずに返しており、Inspect の 5 エントリ
// (どれも RiskConfirm) は「エントリ全体か、何もしないか」しか選べなかった。
func TestDiskDetailInspectWithContentsStillHasSelectablePaths(t *testing.T) {
	r := disk.Result{
		Entry:  disk.Entry{ID: "orphan-container", Label: "孤児コンテナ", Risk: disk.RiskConfirm, Inspect: true, DeleteVia: "trash"},
		Status: disk.StatusOK,
		Items: []disk.Item{
			{Path: "/Users/koji/Library/Containers/com.example.gone", Size: 1 << 20},
			{Path: "/Users/koji/Library/Containers/com.example.other", Size: 2 << 20},
		},
		Contents: []string{"com.example.gone/Data", "com.example.other/Data"},
	}
	v := &doctorview.DoctorView{}
	rows := v.DiskDetail(doctorview.DoctorRenderOpts{Width: 100, Page: 30}, r)
	var sel []doctorview.RowKey
	for _, row := range rows {
		if row.Selectable {
			sel = append(sel, row.Key)
		}
	}
	if len(sel) != len(r.Items) {
		t.Fatalf("選べる対象パス行が %d 件 (期待 %d):\n%v", len(sel), len(r.Items), rowTexts(rows))
	}
	for _, it := range r.Items {
		// 🚨 期待値は **リテラル**で組む。diskItemRowKey で作ると生成側と同じ関数になり、
		// prefix を変える退行を素通しする (敵対レビュー 2026-09-14 が実測で GREEN を確認)
		want := doctorview.RowKey("diskitem:" + r.Entry.ID + "\x00" + it.Path)
		found := false
		for _, k := range sel {
			if k == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q が選べる行に無い: %v", want, sel)
		}
	}
	// 中身の一覧は残す (見てから選ぶためのゲートなので消さない) が、選べないことを明示する
	joined := strings.Join(rowTexts(rows), "\n")
	if !strings.Contains(joined, "com.example.gone/Data") {
		t.Errorf("中身の一覧が消えている:\n%s", joined)
	}
	if !strings.Contains(joined, "この一覧は選べません") {
		t.Errorf("中身が選べないことを伝えていない:\n%s", joined)
	}
}

// 「消せるものがありません」の画面でも送れる (issue 241 の敵対レビュー P1)。
// パネルは入り切らないときに「(j/k で送る)」と出すので、送れないと**案内どおり押した打鍵で
// 無言で消える** = 241 が塞いだはずの形をこちらで作ることになる。消せない理由 (Reason) こそ読みたい。
func TestDeleteNoWorkPanelScrolls(t *testing.T) {
	entries := make([]disk.EntryOutcome, 0, 10)
	for i := range 10 {
		entries = append(entries, disk.EntryOutcome{Label: fmt.Sprintf("え%d", i), Method: "rm",
			Outcome: disk.OutcomeSkipped, Reason: "走査し直したら候補ではなくなっていました"})
	}
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &disk.DeleteReport{Entries: entries}}}
	first := strings.Join(v.Lines(doctortest.DoctorTestOpts(14)), "\n")
	if !strings.Contains(first, "消せるものがありません") {
		t.Fatalf("前提が違う (消せるものがありません の画面):\n%s", first)
	}
	if !strings.Contains(first, "j/k で送る") {
		t.Fatalf("前提が違う (入り切らず注記が出ること):\n%s", first)
	}
	if _, handled := v.HandleDeleteKey("j"); !handled {
		t.Fatal("j が処理されていない")
	}
	if !v.Del.Confirm {
		t.Error("j で画面が閉じた (パネルが j/k を勧めているのに消えるのは 241 と同型)")
	}
	if v.Del.ConfirmScroll.Offset == 0 {
		t.Error("j で送れていない")
	}
	// 送るキー以外は従来どおり戻る
	if _, handled := v.HandleDeleteKey("x"); !handled {
		t.Fatal("x が処理されていない")
	}
	if v.Del.Confirm {
		t.Error("送るキー以外で戻らなかった")
	}
}

// 結果パネル (窓を持たない側) は塊単位で落とし、落としたことを注記で伝える。
// 🚨 このカバレッジは元々 TestDeletePanelElisionNote が持っていたが、確認パネルが窓へ移った
// 書き換えで失われていた (敵対レビュー 2026-09-04 の P2-3: elide を false にする変異が全緑だった)。
func TestDeleteResultPanelElisionNote(t *testing.T) {
	entries := make([]disk.EntryOutcome, 0, 8)
	for i := range 8 {
		entries = append(entries, disk.EntryOutcome{Label: fmt.Sprintf("え%d", i), Method: "rm",
			Outcome: disk.OutcomeDeleted, Freed: 1024, Items: plannedItemOutcomes(1)})
	}
	rep := disk.DeleteReport{Entries: entries}
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Result: &rep}}
	out := strings.Join(v.Lines(doctortest.DoctorTestOpts(10)), "\n")
	for _, want := range []string{"他 ", "画面に入りません"} {
		if !strings.Contains(out, want) {
			t.Errorf("落とした件数の注記に %q が無い:\n%s", want, out)
		}
	}
}

func slicesContains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

// 確認の結末語は結果画面と同じ出典 (doctorview.DoctorOutcomeWord) から取る。
// 「触らなかった (Skipped)」と「実行できなかった (Failed)」を 1 語に畳まない (issue 242 P3-1)。
func TestDeleteConfirmSeparatesSkippedFromFailed(t *testing.T) {
	plan := disk.DeleteReport{Entries: []disk.EntryOutcome{
		{Label: "さわらず", Method: "rm", Outcome: disk.OutcomeSkipped, Reason: "いまは対象外です"},
		{Label: "できず", Method: "rm", Outcome: disk.OutcomeFailed, Reason: "削除の前に走査し直せませんでした"},
	}}
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &plan}}
	out := doctortest.DoctorPanelText(v, 30)
	// 🚨 下見の Skipped は「いまは対象外」なので**一覧と同じ語**であること。期待値は
	// disk.Mark から取る (リテラルで書くと、一覧側だけ語が変わっても気づけない)
	for _, want := range []string{disk.Mark(disk.Result{Status: disk.StatusBlocked}), "❌ できず"} {
		if !strings.Contains(out, want) {
			t.Errorf("確認に %q が無い:\n%s", want, out)
		}
	}
	// 結果画面の語 (実行したが触らなかった) を下見で使わない。直下に出る理由は
	// 「いまは対象外です: …」なので、「触れず」だと 1 行の中で語と理由が食い違う
	if w := doctorview.DoctorOutcomeWord(disk.OutcomeSkipped); strings.Contains(out, w) {
		t.Errorf("確認が結果画面の語 %q を使っている:\n%s", w, out)
	}
}

// 下見 (preparing) と実行 (running) を 1 語に畳まない。下見はまだ何も壊していない。
// 中断の案内は相に依らず出す (下見中も handleDeleteKey の blocking 分岐が Ctrl-C を受ける。issue 242 P3-3)。
func TestDeletePhaseWordingSeparatesPreparingFromRunning(t *testing.T) {
	for _, tc := range []struct {
		name     string
		del      doctorview.DoctorDelete
		panel    []string
		notPanel []string
		wantHint string
		notHint  string
	}{
		{name: "preparing", del: doctorview.DoctorDelete{Preparing: true, Progress: doctortest.ProgAt(1, 2, "なにか", disk.PhaseScanning)},
			panel:    []string{"削除できるか確認しています", "対象を走査し直しています", "Ctrl-C / Ctrl-G を 2 回押すと中断します"},
			notPanel: []string{"もう一度 Ctrl-C"},
			wantHint: "確認しています", notHint: "実行中"},
		{name: "running", del: doctorview.DoctorDelete{Running: true, Progress: doctortest.ProgAt(1, 2, "なにか", disk.PhaseDeleting)},
			panel:    []string{"削除しています", "Ctrl-C / Ctrl-G を 2 回押すと中断します"},
			notPanel: []string{"もう一度 Ctrl-C"},
			wantHint: "実行中です", notHint: "確認しています"},
		// 1 回目を押した後は残り 1 回。「2 回押せ」と「もう 1 回押せ」を並べるとあと何回か読めない
		{name: "preparing+armed", del: doctorview.DoctorDelete{Preparing: true, ArmedCC: true, Progress: doctortest.ProgAt(1, 2, "なにか", disk.PhaseScanning)},
			panel:    []string{"もう一度 Ctrl-C / Ctrl-G を押すと中断します"},
			notPanel: []string{"2 回押すと中断します"},
			wantHint: "確認しています", notHint: "実行中"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := &doctorview.DoctorView{Del: tc.del}
			out := doctortest.DoctorPanelText(v, 20)
			for _, want := range tc.panel {
				if !strings.Contains(out, want) {
					t.Errorf("パネルに %q が無い:\n%s", want, out)
				}
			}
			for _, ng := range tc.notPanel {
				if strings.Contains(out, ng) {
					t.Errorf("パネルに %q が出ている:\n%s", ng, out)
				}
			}
			h := v.Hint(120)
			if !strings.Contains(h, tc.wantHint) {
				t.Errorf("hint = %q (%q を含むこと)", h, tc.wantHint)
			}
			if strings.Contains(h, tc.notHint) {
				t.Errorf("hint = %q (%q を含まないこと。相を取り違えている)", h, tc.notHint)
			}
		})
	}
}

// plannedItemOutcomes は下見の結果に載る Item を n 件作る。
// 🚨 **Outcome を必ず入れる**: production の planDelete / planItem は Item ごとに
// `Outcome: OutcomePlanned` を入れる (delete.go:331 / :509) ので、zero 値の fixture は
// 「実際には起こらない状態」を検査することになる。issue 233 で確認画面が Planned だけを
// 数えるようにしたとき、zero 値の fixture が全部 0 件へ落ちて実際に踏んだ。
func plannedItemOutcomes(n int) []disk.ItemOutcome {
	out := make([]disk.ItemOutcome, 0, n)
	for i := range n {
		out = append(out, disk.ItemOutcome{
			Path: fmt.Sprintf("/tmp/planned-%d", i), Size: 1024, Outcome: disk.OutcomePlanned,
		})
	}
	return out
}

// 確認画面は**実際に触る Item だけ**を数え、並べる (issue 233)。
// 下見で Skipped / Failed になった Item を混ぜると、同じ行のサイズ (BeforeSize = 照合が取れた分)
// と件数が食い違い、「3 件を削除」と出ているのにサイズは 1 件分、という画面になる。
func TestDeleteConfirmCountsOnlyPlannedItems(t *testing.T) {
	plan := disk.DeleteReport{Entries: []disk.EntryOutcome{{
		Label: "混在", Method: "rm", Outcome: disk.OutcomePlanned, BeforeSize: 2048,
		Items: []disk.ItemOutcome{
			{Path: "/tmp/keep-1", Size: 1024, Outcome: disk.OutcomePlanned},
			{Path: "/tmp/keep-2", Size: 1024, Outcome: disk.OutcomePlanned},
			{Path: "/tmp/gone", Size: 4096, Outcome: disk.OutcomeSkipped, Reason: "既に存在しません"},
		},
	}}}
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &plan}}
	out := strings.Join(v.Lines(doctortest.DoctorTestOpts(24)), "\n")
	if !strings.Contains(out, "2 件を削除") {
		t.Errorf("触る件数 (2) を出していない:\n%s", out)
	}
	if strings.Contains(out, "3 件を削除") {
		t.Errorf("触らない Item まで数えている:\n%s", out)
	}
	if strings.Contains(out, "/tmp/gone") {
		t.Errorf("触らないパスを並べている (doc は「実際に触る対象そのもの」と主張している):\n%s", out)
	}
	for _, want := range []string{"/tmp/keep-1", "/tmp/keep-2"} {
		if !strings.Contains(out, want) {
			t.Errorf("触るパス %q が出ていない:\n%s", want, out)
		}
	}
	// 黙って省かない。🚨 **理由も出す** (固定文言に畳むと「対象パスを拒否」= 細工の兆候が
	// 「実体が変わりました」に化けて消える。敵対レビュー 2026-09-04 の P3)
	if !strings.Contains(out, "他 1 件は対象外") {
		t.Errorf("省いた件数を伝えていない:\n%s", out)
	}
	if !strings.Contains(out, "既に存在しません") {
		t.Errorf("省いた理由を伝えていない:\n%s", out)
	}
}

// cli 経路は**実行するコマンドだけ**を出す (issue 233 の敵対レビュー P2-1)。
// EntryOutcome.CommandLines は全 Item を回すが execCLI は Planned 以外を飛ばすので、
// 件数とパスだけ絞ると「1 件にコマンドを実行」の下にコマンドが 2 本並ぶ矛盾になっていた。
func TestDeleteConfirmCommandsMatchPlannedItems(t *testing.T) {
	plan := disk.DeleteReport{Entries: []disk.EntryOutcome{{
		Label: "simctl ランタイム", Method: "cli", Command: "xcrun simctl runtime delete <id>",
		Outcome: disk.OutcomePlanned, BeforeSize: 2048,
		Items: []disk.ItemOutcome{
			{Path: "/x/keep", Ref: "KEEP-RUNTIME", Size: 1024, Outcome: disk.OutcomePlanned},
			{Path: "/x/gone", Ref: "GONE-RUNTIME", Size: 1024, Outcome: disk.OutcomeSkipped, Reason: "既に存在しません"},
		},
	}}}
	v := &doctorview.DoctorView{Del: doctorview.DoctorDelete{Confirm: true, Plan: &plan}}
	out := strings.Join(v.Lines(doctortest.DoctorTestOpts(24)), "\n")
	if !strings.Contains(out, "KEEP-RUNTIME") {
		t.Errorf("実行するコマンドが出ていない:\n%s", out)
	}
	if strings.Contains(out, "GONE-RUNTIME") {
		t.Errorf("実行しないコマンドを出している (execCLI は Planned 以外を飛ばす):\n%s", out)
	}
	if !strings.Contains(out, "1 件にコマンドを実行") {
		t.Errorf("件数とコマンド本数が揃っていない:\n%s", out)
	}
}

// 進捗の 2 行: エントリ名と「相 + 経過」を別の行に置き、経過は 1 秒経ってから出す。
func TestDeleteProgressLines(t *testing.T) {
	base := time.Date(2026, 9, 4, 1, 0, 0, 0, time.Local)
	p := doctortest.ProgAt(2, 3, "Xcode DerivedData", disk.PhaseDeleting)
	p.Since = base
	o := doctortest.DoctorTestOpts(20)

	o.Now = base.Add(900 * time.Millisecond)
	got := doctorview.DeleteProgressLines(o, p)
	if len(got) != 2 || got[0] != "2/3  Xcode DerivedData" {
		t.Fatalf("1 行目 = %q (全体 %v)", got[0], got)
	}
	if got[1] != "  ⠋ 削除中" {
		t.Errorf("1 秒未満で経過が出ている: %q", got[1])
	}

	o.Now = base.Add(12500 * time.Millisecond)
	if got := doctorview.DeleteProgressLines(o, p); got[1] != "  ⠋ 削除中  12s" {
		t.Errorf("経過が出ていない: %q", got[1])
	}

	// 相がまだ届いていないあいだは「準備中」(スピナーだけは動く)
	if got := doctorview.DeleteProgressLines(o, doctorview.DoctorProgress{}); len(got) != 1 || got[0] != "⠋ 準備中" {
		t.Errorf("準備中 = %v", got)
	}
}

// 同じ相が続くあいだ経過の基準を据え置く。毎イベントで打ち直すと経過が 0 に戻り続け、
// 「どこまで進んだか」が読めなくなる (固まって見える元の症状に戻る)。
func TestDeleteProgressElapsedSurvivesSameStep(t *testing.T) {
	orig := app.TimeNow
	now := time.Date(2026, 9, 4, 1, 0, 0, 0, time.Local)
	app.TimeNow = func() time.Time { return now }
	t.Cleanup(func() { app.TimeNow = orig })

	v := deleteTestView(t, &fakeDelete{})
	v.Del = doctorview.DoctorDelete{Running: true, Ch: make(chan doctorview.DoctorDeleteEvent, 1), Done: make(chan doctorview.DoctorDeleteEvent, 1)}
	send := func(p doctorview.DoctorProgress) {
		v.ReceiveDelete(doctorview.DoctorDeleteMsg{Gen: v.Gen, Ev: doctorview.DoctorDeleteEvent{Prog: &p}})
	}
	send(doctortest.ProgAt(1, 2, "A", disk.PhaseScanning))
	first := v.Del.Progress.Since

	now = now.Add(5 * time.Second)
	send(doctortest.ProgAt(1, 2, "A", disk.PhaseScanning)) // 同じ相の続き
	if !v.Del.Progress.Since.Equal(first) {
		t.Errorf("同じ相で基準が打ち直された: %v → %v", first, v.Del.Progress.Since)
	}

	send(doctortest.ProgAt(1, 2, "A", disk.PhaseDeleting)) // 相が変わった
	if v.Del.Progress.Since.Equal(first) {
		t.Error("相が変わったのに経過の基準が据え置かれた")
	}
}

// 極小の端末でも**抜ける手段**を残す。blocking 中は全キーを飲むので、中断案内が
// 画面から消えると「出る方法がどこにも書いていない」状態になる。
func TestDeletePanelKeepsAbortHintOnTinyPage(t *testing.T) {
	for _, tc := range []struct {
		name string
		del  doctorview.DoctorDelete
		page int
	}{
		{"実行中 page=5", doctorview.DoctorDelete{Running: true, Progress: doctortest.ProgAt(2, 3, "Xcode DerivedData", disk.PhaseDeleting)}, 5},
		{"下見中 page=6", doctorview.DoctorDelete{Preparing: true, Progress: doctortest.ProgAt(1, 2, "Xcode DerivedData", disk.PhaseScanning)}, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := deleteTestView(t, &fakeDelete{})
			v.Del = tc.del
			out := doctortest.DoctorPanelText(v, tc.page)
			if !strings.Contains(out, "中断します") {
				t.Errorf("中断の案内が消えた:\n%s", out)
			}
		})
	}
}
