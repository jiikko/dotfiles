package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
	"doctor/svc"
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
// 🚨 **Batch の中身は並行に走らせる**。本番の bubbletea はそうするし、逐次に回すと
// waitDeleteCmd が channel 待ちに入って producer が動かず、そのまま止まる。
// Msg の取り込み (receiveDelete) はこの goroutine だけが行う (Update の外で状態を触らない)。
func runDeleteCmds(t *testing.T, v *doctorView, cmd tea.Cmd) []string {
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
			dm, ok := m.(doctorDeleteMsg)
			if !ok {
				t.Fatalf("知らない Msg: %T", m)
			}
			if dm.ev.rep == nil {
				progress = append(progress, dm.ev.progress)
			}
			start(v.receiveDelete(dm))
			if !v.del.blocking() {
				return progress // 確認 / 結果 / エラーに落ちた (待ち続けている waiter は捨ててよい)
			}
		case <-deadline:
			t.Fatalf("削除が終わらない: %+v", v.del)
			return nil
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
	v.handleKey("enter", 20) // 中身を開く (カーソルは中の対象パスへ移る)
	_ = v.lines(doctorTestOpts(30))
	if act := v.handleKey(" ", 20); act != doctorSwallow || len(v.selectedItems) == 0 {
		t.Fatalf("中身を見た後も選べない: act=%v selectedItems=%v toast=%q", act, v.selectedItems, v.pendingToast)
	}
	if got := v.selectedResults(); len(got) != 1 {
		t.Fatalf("選んだ対象が削除に渡らない: %d 件", len(got))
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
	_ = runDeleteCmds(t, v, v.takeDeleteCmd())
	if len(f.calls) != 1 || !f.calls[0].DryRun {
		t.Fatalf("下見を DryRun で呼んでいない: %+v", f.calls)
	}
	// 🚨 この層の唯一の仕事は「選んだ行を engine に渡す」こと。集合そのものを見る
	// (見ないと、nil を渡す変異が素通りする。敵対レビュー 2026-09-03 の実測)
	if got := f.targetIDs(0); len(got) != 1 || got[0] != "thing" {
		t.Fatalf("engine へ渡した対象 = %v (選んだ 1 件のはず)", got)
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
			f := &fakeDelete{rep: disk.DeleteReport{DryRun: true, Entries: []disk.EntryOutcome{
				{Label: "Thing キャッシュ", Method: "rm", Outcome: disk.OutcomePlanned,
					BeforeSize: 4096, Items: make([]disk.ItemOutcome, 1)}}}}
			v := deleteTestView(t, f)
			v.handleKey(" ", 20)
			v.handleKey("d", 20)
			_ = runDeleteCmds(t, v, v.takeDeleteCmd())
			// 中止したら下見の ctx も切る (切らないと leak する)
			cancelled := false
			orig := v.del.cancel
			v.del.cancel = func() { cancelled = true; orig() }
			v.handleKey(key, 20)
			if v.del.active() {
				t.Fatalf("中止したのに状態が残っている: %+v", v.del)
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
			{Label: "Thing キャッシュ", Outcome: disk.OutcomePlanned, BeforeSize: 4096, Items: make([]disk.ItemOutcome, 1)}}},
	}
	v := deleteTestView(t, f)
	v.handleKey(" ", 20)
	v.handleKey("d", 20)
	_ = runDeleteCmds(t, v, v.takeDeleteCmd())

	f.rep = disk.DeleteReport{Freed: 4096, HistoryPath: "/tmp/h.json",
		Entries: []disk.EntryOutcome{
			{Label: "Thing キャッシュ", Outcome: disk.OutcomeIncomplete, Reason: "1 件が残っています"}}}
	if act := v.handleKey("y", 20); act != doctorRunDelete {
		t.Fatalf("act = %v", act)
	}
	if h := v.hint(120); !strings.Contains(h, "実行中") {
		t.Errorf("実行中の hint = %q", h)
	}
	prog := runDeleteCmds(t, v, v.takeDeleteCmd())
	if len(f.calls) != 2 || f.calls[1].DryRun {
		t.Fatalf("本番を DryRun でない形で呼んでいない: %+v", f.calls)
	}
	if got := f.targetIDs(1); len(got) != 1 || got[0] != "thing" {
		t.Fatalf("本番へ渡した対象 = %v", got)
	}
	if len(prog) == 0 || !strings.Contains(prog[0], "走査中") {
		t.Errorf("進捗が届いていない: %v", prog)
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
	cursor := v.cur.index
	for _, key := range []string{"j", "k", "d", " ", "enter", "q", "esc", "D", "r"} {
		if act := v.handleKey(key, 20); act != doctorSwallow {
			t.Errorf("%q が飲まれていない: act = %v", key, act)
		}
	}
	if v.cur.index != cursor {
		t.Error("実行中にカーソルが動いた")
	}
	if !v.del.running {
		t.Fatal("実行中の状態が壊れた")
	}
	if v.handleKey("ctrl+c", 20); !v.del.armedCC {
		t.Error("1 回目の Ctrl-C で武装していない")
	}
	// 実行中の hint は「消せる」と読める語彙を出さない
	if h := v.hint(120); strings.Contains(h, "d: 削除") || strings.Contains(h, "Space: 選択") {
		t.Errorf("実行中の hint に削除の導線が出ている: %q", h)
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

// ---- 敵対的レビュー (2026-09-03) が開けた穴を塞ぐテスト ----

// 🚨 削除の実行中に Ctrl-C / Ctrl-G が **browseModel の側で** 先に捌かれ、1 回目でアプリごと
// 落ちていた。中断は ctx で伝える契約 (プロセスを殺すと記録が executing のまま残り、
// cli: の子が孤児になる) が、UI の配線で破れていた形。
//
// 🚨 doctorView.handleKey を直叩きするテストではこの穴を 1 mm も守れない (それが実際に
// 起きたこと)。**browseModel.handleKey 経由**で見る。
func TestBrowseCtrlCDuringDeleteDoesNotQuit(t *testing.T) {
	for _, key := range []string{"ctrl+c", "ctrl+g"} {
		t.Run(key, func(t *testing.T) {
			m := newTestBrowse(t, 1, map[string]CIState{}, nil)
			m.doctorOv.shown = true
			cancelled := false
			m.doctorOv.del = doctorDelete{running: true, progress: "1/1 …",
				cancel: func() { cancelled = true }}
			m.handleKey(key)
			if m.done || m.restartRequested {
				t.Fatalf("1 回目の %s でアプリを終了した (中断は ctx で伝えること)", key)
			}
			if !m.doctorOv.del.armedCC {
				t.Errorf("1 回目で武装していない")
			}
			if cancelled {
				t.Errorf("1 回目で中断した (誤爆を 1 段止める設計)")
			}
			m.handleKey(key)
			if !cancelled {
				t.Errorf("2 回目でも中断しない")
			}
			if m.done {
				t.Errorf("2 回目でアプリごと落ちた (ctx の cancel だけでよい)")
			}
		})
	}
}

// 削除の実行中は再起動ダイアログを出さない (出るとどのキーも吸われ、r は走行中の削除を殺す)。
func TestRestartPromptDefersWhileDeleting(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.doctorOv.shown = true
	m.doctorOv.del = doctorDelete{running: true}
	m.restartPending = true
	if m.restartPromptVisible() {
		t.Fatal("削除中に再起動ダイアログを出した")
	}
	m.handleKey("r")
	if m.restartRequested {
		t.Error("削除中の r で再起動した (走行中の削除を殺す)")
	}
}

// doctor を止める (popup を閉じる / 終了) と、削除も ctx で中断される。
func TestDoctorStopCancelsDelete(t *testing.T) {
	v := &doctorView{}
	cancelled := false
	v.del = doctorDelete{running: true, cancel: func() { cancelled = true }}
	v.stop()
	if !cancelled {
		t.Error("stop() が削除の ctx を切っていない (cli: の子が孤児になる)")
	}
}

// 下見の最中に押した Ctrl-C が、本番の 1 回目で即中断に化けない。
func TestDeleteArmedCCNotCarriedIntoRun(t *testing.T) {
	f := &fakeDelete{rep: disk.DeleteReport{DryRun: true}}
	v := deleteTestView(t, f)
	v.handleKey(" ", 20)
	v.handleKey("d", 20)
	v.del.armedCC = true // 下見の最中に 1 回押した
	_ = runDeleteCmds(t, v, v.takeDeleteCmd())
	v.handleKey("y", 20)
	if v.del.armedCC {
		t.Error("下見の armedCC を本番へ持ち越した (1 回目の Ctrl-C で即中断になる)")
	}
	if v.del.confirm {
		t.Error("実行中なのに confirm が立ったまま (相が 2 つ)")
	}
}

// y は入口と同じガードをもう一度通す (入口だけの検査は非対称)。
func TestDeleteConfirmRechecksDeletable(t *testing.T) {
	// 下見に「消すもの」があること = 確認プロンプトが y を受ける状態にしておく
	f := &fakeDelete{rep: disk.DeleteReport{DryRun: true, Entries: []disk.EntryOutcome{
		{Label: "Thing キャッシュ", Method: "rm", Outcome: disk.OutcomePlanned,
			BeforeSize: 4096, Items: make([]disk.ItemOutcome, 1)}}}}
	v := deleteTestView(t, f)
	v.handleKey(" ", 20)
	v.handleKey("d", 20)
	_ = runDeleteCmds(t, v, v.takeDeleteCmd())
	v.diskRep.Results[0].FromSnapshot = true // 確認中に「走査していない」印が立った
	if act := v.handleKey("y", 20); act != doctorToast {
		t.Fatalf("act = %v (理由を出して止めること)", act)
	}
	if len(f.calls) != 1 {
		t.Errorf("engine を %d 回呼んだ (下見の 1 回だけのはず)", len(f.calls))
	}
}

// risk: confirm は Inspect が付いていなくてもゲートに掛かる
// (カタログが Inspect を付け忘れた瞬間にゲートが消える形を防ぐ)。
func TestDeletableGatesRiskConfirmWithoutInspect(t *testing.T) {
	v := &doctorView{}
	r := disk.Result{Status: disk.StatusOK, Items: []disk.Item{{Path: "/x"}},
		Entry: disk.Entry{ID: "x", Risk: disk.RiskConfirm, Inspect: false}}
	if ok, why := v.deletable(r); ok || !strings.Contains(why, "中身を確認") {
		t.Errorf("ok=%v why=%q", ok, why)
	}
}

// Inspect の行は、中身の一覧が無くても**開けば何かが出る** (何も出ないと、
// 中身が無いのか走査が拾えなかったのかが区別できないまま選べてしまう)。
func TestDiskDetailShowsPathsWhenNoContents(t *testing.T) {
	v := &doctorView{}
	r := disk.Result{Status: disk.StatusOK, Entry: disk.Entry{ID: "x", Inspect: true, DeleteVia: "trash"},
		Items: []disk.Item{{Path: "/tmp/a", Size: 1024}}}
	out := strings.Join(rowTexts(v.diskDetail(doctorTestOpts(30), r)), "\n")
	if !strings.Contains(out, "/tmp/a") {
		t.Errorf("中身が無いときに対象のパスも出ない:\n%s", out)
	}
}

// 🚨 狭い画面でも「合計」は残る (1 件目のサイズだけを見て y を押す形にしない)。
func TestDeleteConfirmKeepsTotalsWhenCramped(t *testing.T) {
	entries := make([]disk.EntryOutcome, 0, 12)
	for range 12 {
		entries = append(entries, disk.EntryOutcome{Label: "エントリ", Method: "rm",
			Outcome: disk.OutcomePlanned, BeforeSize: 1 << 30, Items: make([]disk.ItemOutcome, 1)})
	}
	v := &doctorView{del: doctorDelete{confirm: true, plan: &disk.DeleteReport{Entries: entries}}}
	// 🚨 page は**判別できる値**を入れる。6/10/24 だけだと「末尾を後ろから残す」機構を
	// 丸ごと外しても green だった (敵対レビュー 2026-09-03 の実測: 差が出るのは 4 と 5)
	for _, page := range []int{3, 4, 5, 6, 10, 24} {
		out := strings.Join(v.lines(doctorTestOpts(page)), "\n")
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
	v.handleKey(" ", 20)
	v.diskRep.Results[0].Reused = true // 選んだ後に印が立った
	if act := v.handleKey("d", 20); act != doctorToast || !strings.Contains(v.pendingToast, "前回の計測") {
		t.Fatalf("act=%v toast=%q", act, v.pendingToast)
	}
	if len(f.calls) != 0 {
		t.Error("engine を呼んだ")
	}
}

// スキャン中は削除を始めない (走査中の結果を対象にしない)。
func TestDeleteRefusedWhileScanning(t *testing.T) {
	f := &fakeDelete{}
	v := doctorTestView(t)
	v.deleteFn, v.deleteOpts = f.fn, func() disk.DeleteOptions { return disk.DeleteOptions{} }
	doctorFirstDiskEvent(t, v) // 1 件だけ届いた = 走査中
	_ = v.lines(doctorTestOpts(30))
	v.selected = map[string]bool{"thing": true}
	if act := v.handleKey("d", 20); act != doctorToast || !strings.Contains(v.pendingToast, "スキャンが終わるまで") {
		t.Fatalf("act=%v toast=%q", act, v.pendingToast)
	}
	if len(f.calls) != 0 {
		t.Error("走査中に engine を呼んだ")
	}
}

// 進捗は実行中のパネルに出る (出ないと数秒〜数十秒のあいだ無言で固まる)。
func TestDeleteShowsProgressInPanel(t *testing.T) {
	v := deleteTestView(t, &fakeDelete{})
	v.del = doctorDelete{running: true, progress: "2/3 Xcode DerivedData を削除中"}
	out := doctorPanelText(v, 20)
	for _, want := range []string{"削除しています", "2/3 Xcode DerivedData を削除中", "Ctrl-C"} {
		if !strings.Contains(out, want) {
			t.Errorf("実行中のパネルに %q が無い:\n%s", want, out)
		}
	}
	v.del = doctorDelete{preparing: true, progress: "1/2 走査中"}
	if out := doctorPanelText(v, 20); !strings.Contains(out, "確認しています") || !strings.Contains(out, "1/2 走査中") {
		t.Errorf("下見のパネル:\n%s", out)
	}
}

// engine が error を返したら、その旨と「何も消えていません」を出す
// (engine の契約: error は 1 件も触っていないときだけ)。
func TestDeleteShowsEngineError(t *testing.T) {
	f := &fakeDelete{err: errDeleteTest}
	v := deleteTestView(t, f)
	v.handleKey(" ", 20)
	v.handleKey("d", 20)
	_ = runDeleteCmds(t, v, v.takeDeleteCmd())
	if v.del.err == "" {
		t.Fatalf("エラーの相に落ちていない: %+v", v.del)
	}
	out := doctorPanelText(v, 20)
	for _, want := range []string{"削除できませんでした", "記録を残せない", "何も消えていません", "閉じてもう一度スキャン"} {
		if !strings.Contains(out, want) {
			t.Errorf("エラーのパネルに %q が無い:\n%s", want, out)
		}
	}
	// 🚨 パネルと hint が違うことを言わない。実挙動は doctorRescan (閉じて再スキャン) で、
	// 兄弟の結果パネルは正しく言っているのにエラー経路だけ「閉じる」だった (issue 242 P3-2)
	if h := v.hint(120); !strings.Contains(h, "閉じてもう一度スキャン") {
		t.Errorf("hint がパネルと違うことを言っている: hint=%q panel=\n%s", h, out)
	}
	if act := v.handleKey("j", 20); act != doctorRescan || v.del.active() {
		t.Errorf("エラーを閉じられない: act=%v del=%+v", act, v.del)
	}
}

var errDeleteTest = deleteTestError("記録を残せないため中止しました")

type deleteTestError string

func (e deleteTestError) Error() string { return string(e) }

// 中断は ctx で engine へ伝わる (プロセスを殺さない)。
func TestDeleteCancelReachesEngine(t *testing.T) {
	f := &fakeDelete{rep: disk.DeleteReport{DryRun: true, Entries: []disk.EntryOutcome{
		{Label: "Thing キャッシュ", Method: "rm", Outcome: disk.OutcomePlanned, Items: make([]disk.ItemOutcome, 1)}}}}
	v := deleteTestView(t, f)
	v.handleKey(" ", 20)
	v.handleKey("d", 20)
	cmd := v.takeDeleteCmd()
	v.del.cancel() // 実行の直前に中断された状況
	_ = runDeleteCmds(t, v, cmd)
	if f.ctxErr == nil {
		t.Error("engine に中断が伝わっていない (ctx を渡していない)")
	}
}

// 閉じた後に届いた古い Msg を取り込まない。
func TestDeleteIgnoresStaleMsg(t *testing.T) {
	v := deleteTestView(t, &fakeDelete{})
	v.del = doctorDelete{running: true}
	rep := disk.DeleteReport{Freed: 1}
	v.receiveDelete(doctorDeleteMsg{gen: v.gen + 1, ev: doctorDeleteEvent{rep: &rep}})
	if v.del.result != nil {
		t.Error("古い世代の Msg を取り込んだ")
	}
	v.shown = false
	v.receiveDelete(doctorDeleteMsg{gen: v.gen, ev: doctorDeleteEvent{rep: &rep}})
	if v.del.result != nil {
		t.Error("閉じた後の Msg を取り込んだ")
	}
}

// 語彙と桁の対応 (取り違えると「消えていないのに数字が出る」)。
func TestDeleteVocabulary(t *testing.T) {
	for _, tc := range []struct {
		o        disk.Outcome
		wantWord string
		e        disk.EntryOutcome
		wantSize string
	}{
		{disk.OutcomeDeleted, "✅ 削除した", disk.EntryOutcome{Outcome: disk.OutcomeDeleted, Freed: 1024}, "1.0KB"},
		{disk.OutcomeTrashed, "🚮 ゴミ箱へ", disk.EntryOutcome{Outcome: disk.OutcomeTrashed, Trashed: 2048}, "2.0KB"},
		{disk.OutcomeIncomplete, "🚨 未完了", disk.EntryOutcome{Outcome: disk.OutcomeIncomplete, Freed: 4096}, "---"},
		{disk.OutcomeSkipped, "🚫 触れず", disk.EntryOutcome{Outcome: disk.OutcomeSkipped, Freed: 4096}, "---"},
		{disk.OutcomeFailed, "❌ できず", disk.EntryOutcome{Outcome: disk.OutcomeFailed, Freed: 4096}, "---"},
		{disk.OutcomeProposed, "📋 表示のみ", disk.EntryOutcome{Outcome: disk.OutcomeProposed}, "---"},
	} {
		if got := doctorOutcomeWord(tc.o); got != tc.wantWord {
			t.Errorf("%s の語 = %q, want %q", tc.o, got, tc.wantWord)
		}
		// 🚨 消えていない / 消えたか分からないものに数字を出さない
		if got := deleteResultSize(tc.e); got != tc.wantSize {
			t.Errorf("%s の桁 = %q, want %q", tc.o, got, tc.wantSize)
		}
	}
	for method, want := range map[string]string{"rm": "✅ 削除", "trash": "🚮 ゴミ箱へ", "cli": "📋 コマンド", "propose": "📋 表示のみ"} {
		if got := deleteMethodWord(method); got != want {
			t.Errorf("%s の語 = %q, want %q", method, got, want)
		}
	}
	// 記号は単独で幅 2 のものだけ (端末で右端が動かないように)
	for _, s := range []string{"✅", "🚮", "🚨", "🚫", "❌", "📋"} {
		if w := dispWidth(s); w != 2 {
			t.Errorf("%q の幅 = %d (2 でない記号は使わない)", s, w)
		}
	}
}

// 削除の経路ごとに確認の文言が変わる (trash はゴミ箱、cli はコマンド、対象外は理由)。
func TestDeleteConfirmPerMethodLines(t *testing.T) {
	plan := disk.DeleteReport{Entries: []disk.EntryOutcome{
		{Label: "ごみ", Method: "trash", Outcome: disk.OutcomePlanned, BeforeSize: 2048, Items: make([]disk.ItemOutcome, 3)},
		{Label: "こまんど", Method: "cli", Command: "go clean -modcache", Outcome: disk.OutcomePlanned,
			BeforeSize: 1024, Items: make([]disk.ItemOutcome, 1)},
		{Label: "ていじ", Method: "propose", Outcome: disk.OutcomeProposed},
		{Label: "たいしょうがい", Method: "rm", Outcome: disk.OutcomeSkipped, Reason: "いまは対象外です"},
	}}
	v := &doctorView{del: doctorDelete{confirm: true, plan: &plan}}
	out := strings.Join(v.lines(doctorTestOpts(30)), "\n")
	// cli はコマンドの実体を 1 本ずつ出す (件数だけの注記と二重に出さない)
	for _, want := range []string{"3 件をゴミ箱へ移動", "$ go clean -modcache", "1 件にコマンドを実行", "実行しません", "🚫 触れず", "いまは対象外です"} {
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
			Outcome: disk.OutcomePlanned, BeforeSize: 1024, Items: make([]disk.ItemOutcome, 1)})
	}
	v := &doctorView{del: doctorDelete{confirm: true, plan: &disk.DeleteReport{Entries: entries}}}
	out := strings.Join(v.lines(doctorTestOpts(10)), "\n")
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
			Outcome: disk.OutcomePlanned, BeforeSize: 1024, Items: make([]disk.ItemOutcome, 2)})
	}
	v := &doctorView{del: doctorDelete{confirm: true, plan: &disk.DeleteReport{Entries: entries}}}
	first := strings.Join(v.lines(doctorTestOpts(12)), "\n") // 高さを測らせる (窓は描画で決まる)
	// 🚨 キーごとに「**本当に動くか**」まで見る。confirm が閉じないことだけを見ていたら、
	// scroll() から j/k/ctrl+d/… の case を消す変異が緑のまま通った (敵対レビュー 2026-09-04 の実測)。
	// パネルの注記が "(j/k で送る)" と名指ししているので、動かないのは案内が嘘になる形
	down := []string{"j", "down", "ctrl+d", "pgdown", "G", "end"}
	up := []string{"k", "up", "ctrl+u", "pgup", "g", "home"}
	for _, key := range append(append([]string{}, down...), up...) {
		forward := slicesContains(down, key)
		v.del.confirmScroll.offset = 0
		if !forward {
			v.del.confirmScroll.offset = v.del.confirmScroll.total - v.del.confirmScroll.view // 末尾から戻す
		}
		before := v.del.confirmScroll.offset
		act, handled := v.handleDeleteKey(key)
		if !handled {
			t.Fatalf("%q が処理されていない", key)
		}
		if act != doctorSwallow {
			t.Errorf("%q の act = %v; want doctorSwallow (送るだけ。再スキャン等を起こさない)", key, act)
		}
		if !v.del.confirm {
			t.Fatalf("%q で確認が閉じた (送るキーは中止に落とさない)", key)
		}
		got := v.del.confirmScroll.offset
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
	v.del.confirmScroll.offset = 0
	_, _ = v.handleDeleteKey("G")
	last := strings.Join(v.lines(doctorTestOpts(12)), "\n")
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
		BeforeSize: 1024, Items: make([]disk.ItemOutcome, 1)}}
	for _, key := range []string{"n", "esc", " ", "enter", "x"} {
		v := &doctorView{del: doctorDelete{confirm: true, plan: &disk.DeleteReport{Entries: entries}}}
		if _, handled := v.handleDeleteKey(key); !handled {
			t.Fatalf("%q が処理されていない", key)
		}
		if v.del.confirm {
			t.Errorf("%q で中止しなかった (既定は中止側)", key)
		}
	}
}

// 下見の結果、消せるものが 1 件も無かったら「削除しますか?」と聞かない
// (y に意味が無いのに押させる形にしない)。
func TestDeleteConfirmWhenNothingToDo(t *testing.T) {
	f := &fakeDelete{rep: disk.DeleteReport{DryRun: true, Entries: []disk.EntryOutcome{
		{Label: "Thing キャッシュ", Method: "rm", Outcome: disk.OutcomeSkipped, Reason: "いまは対象外です"}}}}
	v := deleteTestView(t, f)
	v.handleKey(" ", 20)
	v.handleKey("d", 20)
	_ = runDeleteCmds(t, v, v.takeDeleteCmd())
	out := doctorPanelText(v, 20)
	if !strings.Contains(out, "消せるものがありません") || strings.Contains(out, "y: 削除する") {
		t.Errorf("消せるものが無いのに削除を促した:\n%s", out)
	}
	if h := v.hint(120); strings.Contains(h, "y: 削除する") {
		t.Errorf("hint = %q", h)
	}
	if act := v.handleKey("y", 20); act != doctorSwallow || v.del.active() {
		t.Fatalf("y で戻らない: act=%v del=%+v", act, v.del)
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
			v := doctorTestView(t)
			runDoctorCmds(t, v, v.open()) // 1 回走査して snapshot を書く
			_ = v.lines(doctorTestOpts(30))
			v.close()
			runDoctorCmds(t, v, v.open()) // TTL 内なので復元される
			_ = v.lines(doctorTestOpts(30))
			if v.snapshotAt.IsZero() {
				t.Fatal("前提が崩れている: snapshot から復元していない")
			}
			if act := v.handleKey(key, 20); act != doctorRescan {
				t.Fatalf("%q の結果 = %v (取り直すこと)", key, act)
			}
			if got := v.takeToast(); !strings.Contains(got, "取り直します") {
				t.Errorf("理由を伝えていない: %q", got)
			}
		})
	}
}

// 取り直した後は普通に選べる (取り直しが行き止まりの解消になっている)。
func TestDoctorSelectableAfterRescan(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	_ = v.lines(doctorTestOpts(30))
	v.close()
	runDoctorCmds(t, v, v.open())
	_ = v.lines(doctorTestOpts(30))
	v.handleKey(" ", 20) // 取り直しへ倒れる
	runDoctorCmds(t, v, v.rescan())
	_ = v.lines(doctorTestOpts(30))
	if !v.snapshotAt.IsZero() {
		t.Fatal("取り直した後も snapshot の印が残っている")
	}
	if act := v.handleKey(" ", 20); act != doctorSwallow || !v.selected["thing"] {
		t.Fatalf("取り直した後も選べない: act=%v selected=%v toast=%q", act, v.selected, v.pendingToast)
	}
}

// rowTexts は行の本文だけを取り出す (詳細行が doctorRow になったため)。
func rowTexts(rows []doctorRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.text)
	}
	return out
}

// ---- ディレクトリ単位の選択 (ユーザー要望 2026-09-03) ----

// multiItemView は 2 つの対象パスを持つエントリ 1 件の doctor。
func multiItemView(t *testing.T, f *fakeDelete) *doctorView {
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
	v := &doctorView{
		diskOpts: func() disk.Options {
			return disk.Options{
				Env: disk.Env{Home: home, TmpDir: home + "/", Getenv: func(string) string { return "" }},
				Catalog: []disk.Entry{{ID: "multi", Label: "2 つある", Tier: 1, Risk: disk.RiskSafe,
					DeleteVia: "rm", Recover: "x", Paths: []string{"~/Library/Caches/multi/*"}}},
				BootTime: func() (time.Time, error) { return time.Now(), nil },
			}
		},
		svcOpts: func() svc.Options {
			return svc.Options{Dirs: []svc.LaunchDir{{Path: filepath.Join(home, "LaunchAgents"), Domain: "gui/501"}},
				Run: func(context.Context, string, ...string) (string, string, int, error) {
					return "PID\tStatus\tLabel\n", "", 0, nil
				}}
		},
		brewRun: func(context.Context, string, ...string) (string, string, int, error) { return "", "", 0, nil },
	}
	if f != nil {
		v.deleteFn = f.fn
		v.deleteOpts = func() disk.DeleteOptions { return disk.DeleteOptions{} }
	}
	runDoctorCmds(t, v, v.open())
	_ = v.lines(doctorTestOpts(40))
	return v
}

// Enter で開くと、カーソルが**中の対象パス**へ移る (j を何度も押さずに消したいものへ行ける)。
func TestDoctorEnterMovesCursorIntoItems(t *testing.T) {
	v := multiItemView(t, nil)
	if !strings.HasPrefix(v.rows[v.cur.index].key, "disk:") {
		t.Fatalf("前提: カーソルがエントリの行にない (%q)", v.rows[v.cur.index].key)
	}
	v.handleKey("enter", 20)
	_ = v.lines(doctorTestOpts(40))
	if !strings.HasPrefix(v.rows[v.cur.index].key, "diskitem:") {
		t.Fatalf("Enter で対象パスへ移らない: cursor=%q", v.rows[v.cur.index].key)
	}
	// Enter で入って Enter で出る (中に居たまま畳めないと戻れない)
	v.handleKey("enter", 20)
	_ = v.lines(doctorTestOpts(40))
	if !strings.HasPrefix(v.rows[v.cur.index].key, "disk:") || v.expanded["disk:multi"] {
		t.Fatalf("対象パスの行の Enter で畳んで戻らない: cursor=%q expanded=%v",
			v.rows[v.cur.index].key, v.expanded)
	}
}

// ディレクトリ単位で選ぶと、そのパスだけが削除に渡る。
func TestDoctorSelectSingleDirectory(t *testing.T) {
	v := multiItemView(t, nil)
	v.handleKey("enter", 20)
	_ = v.lines(doctorTestOpts(40))
	picked := v.rows[v.cur.index].key
	if act := v.handleKey(" ", 20); act != doctorSwallow {
		t.Fatalf("act=%v toast=%q", act, v.pendingToast)
	}
	got := v.selectedResults()
	if len(got) != 1 || len(got[0].Items) != 1 {
		t.Fatalf("渡る対象 = %d エントリ / %d 件 (1 エントリ 1 件のはず)", len(got), len(got[0].Items))
	}
	if !strings.HasSuffix(picked, got[0].Items[0].Path) {
		t.Errorf("選んだのと違うパスが渡る: key=%q path=%q", picked, got[0].Items[0].Path)
	}
	if got[0].Size != got[0].Items[0].Size {
		t.Errorf("合計を引き直していない: size=%d item=%d", got[0].Size, got[0].Items[0].Size)
	}
	// 行頭の印: エントリは「一部」の +、選んだパスは *
	out := strings.Join(v.lines(doctorTestOpts(40)), "\n")
	if !strings.Contains(out, "+") || !strings.Contains(out, "*") {
		t.Errorf("選択の印が出ていない:\n%s", out)
	}
}

// エントリ全体を選ぶと、中の個別選択は畳まれる (二重に数えない)。
func TestDoctorEntrySelectionSupersedesItems(t *testing.T) {
	v := multiItemView(t, nil)
	v.handleKey("enter", 20)
	_ = v.lines(doctorTestOpts(40))
	v.handleKey(" ", 20) // 1 件目のパスを選ぶ
	v.handleKey("enter", 20)
	_ = v.lines(doctorTestOpts(40)) // エントリの行へ戻る
	v.handleKey(" ", 20)            // エントリ全体を選ぶ
	if len(v.selectedItems) != 0 {
		t.Errorf("個別の選択が残っている: %v", v.selectedItems)
	}
	got := v.selectedResults()
	if len(got) != 1 || len(got[0].Items) != 2 {
		t.Fatalf("エントリ全体が渡らない: %d エントリ / %d 件", len(got), len(got[0].Items))
	}
	// 逆向き: 個別に選ぶとエントリ全体の選択は落ちる
	v.handleKey("enter", 20)
	_ = v.lines(doctorTestOpts(40))
	v.handleKey(" ", 20)
	if v.selected["multi"] {
		t.Error("個別に選んだのにエントリ全体の選択が残っている")
	}
}

// 確認画面は**フルパスで列挙**する。ラベルとサイズだけでは、どのディレクトリが消えるのか
// 分からないまま y を押すことになる (ユーザー要望 2026-09-03)。
func TestDeleteConfirmListsFullPaths(t *testing.T) {
	items := make([]disk.ItemOutcome, 0, 13)
	for i := range 13 {
		items = append(items, disk.ItemOutcome{
			Path: fmt.Sprintf("/Users/koji/Library/Caches/thing/Entry-%d", i), Size: 1 << 20})
	}
	plan := disk.DeleteReport{Entries: []disk.EntryOutcome{
		{Label: "たくさんある", Method: "rm", Outcome: disk.OutcomePlanned, BeforeSize: 13 << 20, Items: items}}}
	v := &doctorView{del: doctorDelete{confirm: true, plan: &plan}}
	out := strings.Join(v.lines(doctorTestOpts(40)), "\n")
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
			Items: []disk.ItemOutcome{{Path: "/tmp/x\n 何もしません\n y: 何もしない", Size: 1024}}}}}
	v := &doctorView{del: doctorDelete{confirm: true, plan: &plan}}
	lines := v.lines(doctorTestOpts(40))
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
			Items: []disk.ItemOutcome{{Path: "/L/a", Ref: "ABC-1"}, {Path: "/L/b", Ref: "DEF-2"}}},
		{Label: "提示だけ", Method: "propose", Command: "sudo rm -rf /x", Outcome: disk.OutcomeProposed},
	}}
	v := &doctorView{del: doctorDelete{confirm: true, plan: &plan}}
	out := strings.Join(v.lines(doctorTestOpts(30)), "\n")
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
	v := &doctorView{del: doctorDelete{running: true, progress: "1/1 …",
		log: commandLogLines(disk.CommandRecord{Name: "faketool", Args: []string{"purge"},
			RC: 24, Stdout: "done", Stderr: "retry later"})}}
	out := strings.Join(v.lines(doctorTestOpts(20)), "\n")
	for _, want := range []string{"$ faketool purge", "exit 24", "1| done", "2| retry later"} {
		if !strings.Contains(out, want) {
			t.Errorf("実行中の画面に %q が無い:\n%s", want, out)
		}
	}
}

// 結果 / エラーの画面で y は出力をコピーする (閉じない)。失敗を LLM へ投げるため。
func TestDeleteResultCopiesCommandOutput(t *testing.T) {
	rep := disk.DeleteReport{HistoryPath: "/tmp/h.json", Entries: []disk.EntryOutcome{
		{Label: "ランタイム", Outcome: disk.OutcomeFailed, Reason: "exit 24",
			Items: []disk.ItemOutcome{{Path: "/L/a", Outcome: disk.OutcomeFailed, Reason: "exit 24"}}}}}
	v := &doctorView{del: doctorDelete{result: &rep,
		log: commandLogLines(disk.CommandRecord{Name: "xcrun", Args: []string{"simctl"}, RC: 24, Stderr: "retry"})}}
	act, taken := v.handleDeleteKey("y")
	if !taken || act != doctorCopyLog {
		t.Fatalf("act=%v taken=%v (y は出力をコピー)", act, taken)
	}
	if v.del.result == nil {
		t.Fatal("y で閉じてしまった (コピーは閉じない)")
	}
	got := v.copyPayload()
	for _, want := range []string{"$ xcrun simctl", "exit 24", "2| retry", "/L/a", "/tmp/h.json"} {
		if !strings.Contains(got, want) {
			t.Errorf("コピー文に %q が無い:\n%s", want, got)
		}
	}
	// y 以外は閉じて再スキャン
	if act, _ := v.handleDeleteKey("j"); act != doctorRescan || v.del.active() {
		t.Errorf("他のキーで閉じない: act=%v", act)
	}
}

// ---- doctor_view.go の通しレビュー (opus, 2026-09-03) で出た食い違いの回帰 ----

// blocked の行に削除の案内を出さない。出しておいて Space が断るのは案内が嘘になる。
func TestBlockedRowDoesNotAdvertiseDeletion(t *testing.T) {
	v := &doctorView{}
	r := disk.Result{Status: disk.StatusBlocked, Reason: "Google Chrome 起動中のため対象外",
		Entry: disk.Entry{ID: "chrome-tmp", Label: "Chrome の一時ファイル", DeleteVia: "rm"}}
	if out := strings.Join(rowTexts(v.diskDetail(doctorTestOpts(40), r)), "\n"); strings.Contains(out, "d で削除") {
		t.Errorf("blocked の行が削除を案内している:\n%s", out)
	}
	// 拒否文も「走査できていない」と混ぜない (blocked は走査できた上で対象外)
	ok, why := v.deletable(r)
	if ok || !strings.Contains(why, "いまは対象外") || !strings.Contains(why, "Chrome") {
		t.Errorf("ok=%v why=%q (理由まで伝えること)", ok, why)
	}
}

// 🚨 重いエントリの計測値を再利用しても「画面ごと復元した」印は立てない。
// 立つと、今回走査した画面なのに削除が「前回の結果を表示しています」と断り、
// snapshotAt は zero なので再スキャンにも倒れず、その行だけ行き止まりになる。
func TestReuseDoesNotCarryFromSnapshot(t *testing.T) {
	now := time.Now()
	sn := doctorSnapshot{ScannedAt: now.Add(-10 * time.Minute), Disk: disk.Report{Results: []disk.Result{{
		Entry: disk.Entry{ID: "heavy"}, Status: disk.StatusOK, MeasuredAt: now.Add(-10 * time.Minute),
		Elapsed: 5 * time.Second, FromSnapshot: true, Items: []disk.Item{{Path: "/x", Size: 1}}, Size: 1,
	}}}}
	reuse := doctorReuseFrom(sn, true, now)
	if reuse == nil {
		t.Fatal("再利用が効いていない (前提が崩れている)")
	}
	got := reuse(disk.Entry{ID: "heavy"})
	if got == nil {
		t.Fatal("再利用されない")
	}
	if got.FromSnapshot {
		t.Error("再利用に「画面ごと復元した」印が残っている")
	}
}

// 削除の**確認中**も再起動ダイアログを出さない (出るとどのキーも吸われ、y が食われる)。
func TestRestartPromptDefersWhileDeleteConfirm(t *testing.T) {
	for _, tc := range []struct {
		name string
		del  doctorDelete
	}{{"確認中", doctorDelete{confirm: true}}, {"結果の表示中", doctorDelete{result: &disk.DeleteReport{}}}} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestBrowse(t, 1, map[string]CIState{}, nil)
			m.doctorOv.shown = true
			m.doctorOv.del = tc.del
			m.restartPending = true
			if m.restartPromptVisible() {
				t.Fatalf("%s に再起動ダイアログを出した", tc.name)
			}
		})
	}
}

// トーストは一度出したら消える (残ると、次の再スキャンの理由として使い回される)。
func TestDoctorToastIsConsumedOnce(t *testing.T) {
	v := &doctorView{pendingToast: "一度だけ"}
	if got := v.takeToast(); got != "一度だけ" {
		t.Fatalf("takeToast = %q", got)
	}
	if got := v.takeToast(); got != "" {
		t.Errorf("2 回目でも文言が残っている: %q", got)
	}
}

// hint の「選択 N 件」は**ディレクトリ数**で数える (確認画面が Item 数で数えるため)。
func TestSelectionSummaryCountsDirectories(t *testing.T) {
	v := multiItemView(t, nil)
	v.handleKey(" ", 20) // エントリ全体 (= 中の 2 本)
	n, total := v.selectionSummary()
	if n != 2 {
		t.Errorf("選択 = %d 件 (ディレクトリ 2 本のはず)", n)
	}
	if total <= 0 {
		t.Errorf("合計 = %d", total)
	}
}

// 前回の結果の画面でも、削除に関係ない行では再スキャンを起こさない。
func TestSnapshotRescanOnlyOnDiskRows(t *testing.T) {
	v := &doctorView{snapshotAt: time.Now().Add(-time.Minute),
		rows: []doctorRow{{key: "brew:0:x", selectable: true}}, cur: rowCursor{index: 0}}
	if _, ok := v.snapshotRescan(); ok {
		t.Error("brew の行で再スキャンを起こした")
	}
	v.rows = []doctorRow{{key: "disk:thing", selectable: true}}
	if _, ok := v.snapshotRescan(); !ok {
		t.Error("ディスクの行で再スキャンを起こさない")
	}
}

// ディレクトリ行の解説文は、その 1 本についての話にする (エントリ全体の使い回しにしない)。
func TestDiskItemCopyTextIsAboutOnePath(t *testing.T) {
	r := disk.Result{Status: disk.StatusOK, Size: 3 << 20,
		Entry: disk.Entry{ID: "x", Label: "ラベル", Risk: disk.RiskSafe, DeleteVia: "rm", Recover: "戻せます"},
		Items: []disk.Item{{Path: "/a", Size: 1 << 20}, {Path: "/b", Size: 2 << 20}}}
	// 🚨 関数を直接呼ぶだけでは**配線**を守れない (行が別の関数を使っていても green になる)。
	// 行を組み立てて、その行の copyText を見る
	v := &doctorView{}
	rows := v.diskItemRows(doctorTestOpts(60), r)
	if len(rows) != 2 {
		t.Fatalf("対象パスの行が %d 本", len(rows))
	}
	got := rows[0].copyText
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

// 裏取りコマンドの switch が見ているカタログ ID は、実在するものだけ
// (リネームすると裏取りが黙って消えるだけで build も test も赤くならない)。
func TestDiskVerifyCommandsIDsExistInCatalog(t *testing.T) {
	src, err := os.ReadFile("doctor_view.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "func diskVerifyCommands(")
	if start < 0 {
		t.Fatal("diskVerifyCommands が見つからない (走査が壊れている)")
	}
	end := strings.Index(body[start:], "\n}\n")
	if end < 0 {
		t.Fatal("関数の終わりが見つからない")
	}
	// 🚨 `case "([a-z0-9-]+)"` で拾うと **case 直後の 1 個目のリテラルにしか当たらない**。
	// `case "a", "b":` の 2 個目以降が突合を素通りし、11 ID 中 7 件しか見ていなかった
	// (実測 2026-09-03)。case 行に出る全リテラルを拾うこと。
	// この switch の case 行は ID しか持たない前提。別の文字列を並べたら
	// 「カタログに無い ID」で落ちる (fail-closed 側なので、落ちたら走査を直す)
	lit := regexp.MustCompile(`"([a-z0-9-]+)"`)
	var ids []string
	for _, line := range strings.Split(body[start:start+end], "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "case ") {
			continue
		}
		for _, m := range lit.FindAllStringSubmatch(line, -1) {
			ids = append(ids, m[1])
		}
	}
	if len(ids) < 10 {
		t.Fatalf("case を %d 件しか拾えていない (走査が壊れている)", len(ids))
	}
	for _, id := range ids {
		if !disk.CatalogHasID(id) {
			t.Errorf("カタログに無い ID を見ている: %q (リネームで裏取りが黙って消える)", id)
		}
	}
}

// 開き直しで世代をまたぐ状態を残さない (1 つでも残すと前の画面の文言・Cmd が混ざる)。
func TestDoctorStartClearsCarryOverState(t *testing.T) {
	v := doctorTestView(t)
	runDoctorCmds(t, v, v.open())
	_ = v.lines(doctorTestOpts(30))
	v.pendingToast, v.pendingCopy, v.enterDetail = "残り", "残り", "disk:thing"
	v.cur.fellBack = true
	v.pendingDeleteCmd = func() tea.Msg { return nil }
	v.selected = map[string]bool{"thing": true}
	runDoctorCmds(t, v, v.rescan())
	if v.pendingToast != "" || v.pendingCopy != "" || v.enterDetail != "" ||
		v.cur.fellBack || v.pendingDeleteCmd != nil || len(v.selected) != 0 {
		t.Errorf("前の世代の状態が残っている: toast=%q copy=%q enter=%q fellBack=%v cmd=%v selected=%v",
			v.pendingToast, v.pendingCopy, v.enterDetail, v.cur.fellBack, v.pendingDeleteCmd != nil, v.selected)
	}
}

// パスは**末尾を残して**詰める (issue 239)。末尾から切ると DerivedData のハッシュ部が落ち、
// 同一プロジェクトの旧世代を確認画面で見分けられないまま y を押すことになる。
func TestDeleteConfirmKeepsPathTail(t *testing.T) {
	long := "/Users/koji/Library/Developer/Xcode/DerivedData/ThumbnailThumb-cxxbmelbwqqahjagpvzoszkxfvfz"
	plan := disk.DeleteReport{Entries: []disk.EntryOutcome{{
		Label: "DerivedData", Method: "rm", Outcome: disk.OutcomePlanned, BeforeSize: 1 << 30,
		Items: []disk.ItemOutcome{{Path: long, Size: 1 << 30}},
	}}}
	v := &doctorView{del: doctorDelete{confirm: true, plan: &plan}}
	out := strings.Join(v.lines(doctorRenderOpts{width: 77, page: 24}), "\n")
	if !strings.Contains(out, "ThumbnailThumb-cxxbmelbwqqahjagpvzoszkxfvfz") {
		t.Errorf("末尾 (世代を見分ける識別子) が落ちている:\n%s", out)
	}
	if strings.Contains(out, "/Users/koji/Library/Developer/Xcode/DerivedData/Thumb") {
		t.Errorf("先頭から出ている = 末尾を切っている:\n%s", out)
	}
}

// 一覧の対象パス行も同じ規律 (Space で選ぶ対象そのものなので、どれか分からないと選べない)。
func TestDiskItemRowKeepsPathTail(t *testing.T) {
	long := "/Users/koji/Library/Developer/Xcode/DerivedData/ThumbnailThumb-cxxbmelbwqqahjagpvzoszkxfvfz"
	r := disk.Result{Entry: disk.Entry{ID: "dd", Label: "DerivedData", Risk: disk.RiskSafe},
		Status: disk.StatusOK, Items: []disk.Item{{Path: long, Size: 1 << 30}}}
	v := &doctorView{}
	rows := v.diskItemRows(doctorRenderOpts{width: 77, page: 24}, r)
	if len(rows) != 1 {
		t.Fatalf("行数 = %d", len(rows))
	}
	if !strings.Contains(rows[0].text, "ThumbnailThumb-cxxbmelbwqqahjagpvzoszkxfvfz") {
		t.Errorf("末尾が落ちている: %q", rows[0].text)
	}
	// 🚨 **この層で予算に収めていること**まで見る。行が予算を超えたまま返ると、後段の lines() が
	// 行末を切るので末尾が落ちる (この assert が無いと「切らずに返す」変異が緑のまま通った。実測)
	if w := dispWidth(rows[0].text); w > 77-cursorGutterWidth {
		t.Errorf("行が予算を超えている: 幅 %d > %d: %q", w, 77-cursorGutterWidth, rows[0].text)
	}
	// 🚨 コピーは**切っていない実パス**であること (表示の都合で壊さない)
	if rows[0].copyPath != long {
		t.Errorf("copyPath が切られている: %q", rows[0].copyPath)
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
	v := &doctorView{}
	rows := v.diskDetail(doctorRenderOpts{width: 100, page: 30}, r)
	var sel []string
	for _, row := range rows {
		if row.selectable {
			sel = append(sel, row.key)
		}
	}
	if len(sel) != len(r.Items) {
		t.Fatalf("選べる対象パス行が %d 件 (期待 %d):\n%v", len(sel), len(r.Items), rowTexts(rows))
	}
	for _, it := range r.Items {
		want := "diskitem:" + diskItemKey(r.Entry.ID, it.Path)
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
	v := &doctorView{del: doctorDelete{confirm: true, plan: &disk.DeleteReport{Entries: entries}}}
	first := strings.Join(v.lines(doctorTestOpts(14)), "\n")
	if !strings.Contains(first, "消せるものがありません") {
		t.Fatalf("前提が違う (消せるものがありません の画面):\n%s", first)
	}
	if !strings.Contains(first, "j/k で送る") {
		t.Fatalf("前提が違う (入り切らず注記が出ること):\n%s", first)
	}
	if _, handled := v.handleDeleteKey("j"); !handled {
		t.Fatal("j が処理されていない")
	}
	if !v.del.confirm {
		t.Error("j で画面が閉じた (パネルが j/k を勧めているのに消えるのは 241 と同型)")
	}
	if v.del.confirmScroll.offset == 0 {
		t.Error("j で送れていない")
	}
	// 送るキー以外は従来どおり戻る
	if _, handled := v.handleDeleteKey("x"); !handled {
		t.Fatal("x が処理されていない")
	}
	if v.del.confirm {
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
			Outcome: disk.OutcomeDeleted, Freed: 1024, Items: make([]disk.ItemOutcome, 1)})
	}
	rep := disk.DeleteReport{Entries: entries}
	v := &doctorView{del: doctorDelete{result: &rep}}
	out := strings.Join(v.lines(doctorTestOpts(10)), "\n")
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

// 確認の結末語は結果画面と同じ出典 (doctorOutcomeWord) から取る。
// 「触らなかった (Skipped)」と「実行できなかった (Failed)」を 1 語に畳まない (issue 242 P3-1)。
func TestDeleteConfirmSeparatesSkippedFromFailed(t *testing.T) {
	plan := disk.DeleteReport{Entries: []disk.EntryOutcome{
		{Label: "さわらず", Method: "rm", Outcome: disk.OutcomeSkipped, Reason: "いまは対象外です"},
		{Label: "できず", Method: "rm", Outcome: disk.OutcomeFailed, Reason: "削除の前に走査し直せませんでした"},
	}}
	v := &doctorView{del: doctorDelete{confirm: true, plan: &plan}}
	out := doctorPanelText(v, 30)
	for _, want := range []string{"🚫 触れず", "❌ できず"} {
		if !strings.Contains(out, want) {
			t.Errorf("確認に %q が無い:\n%s", want, out)
		}
	}
	// 🚫 対象外 は一覧 (disk.Mark) では StatusBlocked の語。確認画面で使い回すと
	// 同じ記号が「対象外」「触れず」の 2 つの意味を持つ
	if strings.Contains(out, "🚫 対象外") {
		t.Errorf("確認が一覧 (disk.Mark) の語を使っている:\n%s", out)
	}
}

// 下見 (preparing) と実行 (running) を 1 語に畳まない。下見はまだ何も壊していない。
// 中断の案内は相に依らず出す (下見中も handleDeleteKey の blocking 分岐が Ctrl-C を受ける。issue 242 P3-3)。
func TestDeletePhaseWordingSeparatesPreparingFromRunning(t *testing.T) {
	for _, tc := range []struct {
		name     string
		del      doctorDelete
		panel    []string
		wantHint string
		notHint  string
	}{
		{"preparing", doctorDelete{preparing: true, progress: "1/2 なにか を走査中"},
			[]string{"削除できるか確認しています", "対象を走査し直しています", "Ctrl-C を 2 回押すと中断します"},
			"確認しています", "実行中"},
		{"running", doctorDelete{running: true, progress: "1/2 なにか を削除中"},
			[]string{"削除しています", "Ctrl-C を 2 回押すと中断します"},
			"実行中です", "確認しています"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := &doctorView{del: tc.del}
			out := doctorPanelText(v, 20)
			for _, want := range tc.panel {
				if !strings.Contains(out, want) {
					t.Errorf("パネルに %q が無い:\n%s", want, out)
				}
			}
			h := v.hint(120)
			if !strings.Contains(h, tc.wantHint) {
				t.Errorf("hint = %q (%q を含むこと)", h, tc.wantHint)
			}
			if strings.Contains(h, tc.notHint) {
				t.Errorf("hint = %q (%q を含まないこと。相を取り違えている)", h, tc.notHint)
			}
		})
	}
}
