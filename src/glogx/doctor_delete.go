package main

// doctor の削除導線 (issue 148 段階 ④ S2)。**破壊的なコードはここに無い**:
// 判断はすべて `doctor/disk` の Delete が持っていて、この層は「どれを選んだか」を渡し、
// 進捗と結果を描くだけ。engine が守る不変条件 (issues/148 の ④ 節) のうち、
// UI が壊しうるのは次の 3 つなので、それをここで担保する:
//
//   - 走査していない行 (Reused / FromSnapshot) を渡さない。engine も拒否するが、
//     押してから「再スキャンしてください」と言われるより、押す前に止める方が親切
//   - 中断は **ctx の cancel** で伝える (プロセスを殺すと記録が executing のまま残る)
//   - 結果は 3 値ではない。`incomplete` (実行したが残っている) を成功にも失敗にも畳まない
//
// engine の契約 (ここが崩れると UI が嘘をつく): **error を返すのは 1 件も触っていないときだけ**
// (記録を残せず中止した場合)。中断・部分失敗は error ではなく Outcome で返る。だから
// エラーのパネルは「何も消えていません」と断言してよい。
//
// 進捗の Msg は走査 goroutine から並行に届くので、channel に載せて Cmd で 1 件ずつ Msg にする
// (Update の外で状態を触らない)。ディスク走査 (doctorDiskEvent) と同じ形。

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
)

// doctorDelete は削除の状態機械。confirm → running → result の 3 相。
type doctorDelete struct {
	// preparing は d を押した直後の下見 (DryRun) 中。engine が削除の直前と同じ走査を通すので
	// 重いエントリでは数秒かかる = 無言で固まらないよう進捗を出す
	preparing bool
	plan      *disk.DeleteReport // 下見の結果 (確認プロンプトの材料)
	confirm   bool               // y/N の確認中
	running   bool               // 削除の実行中 (キーを飲む)
	result    *disk.DeleteReport
	err       string
	progress  string // 「2/3 Xcode DerivedData を走査中」

	cancel  context.CancelFunc
	ch      chan doctorDeleteEvent
	armedCC bool // 実行中に Ctrl-C が 1 回押された (2 回目で cancel)
}

// active は削除の語彙がキーを持っているか (裏の一覧にも C/X にも渡さない)。
func (d *doctorDelete) active() bool {
	return d.preparing || d.confirm || d.running || d.result != nil || d.err != ""
}

// blocking は実行中で、抜ける以外のキーを飲む状態か。
func (d *doctorDelete) blocking() bool { return d.preparing || d.running }

func (d *doctorDelete) reset() {
	if d.cancel != nil {
		d.cancel()
	}
	*d = doctorDelete{}
}

// doctorDeleteEvent は削除の進捗 / 完了 (channel 経由で Update へ運ぶ)。
type doctorDeleteEvent struct {
	progress string
	rep      *disk.DeleteReport
	err      string
	dryRun   bool
}

type doctorDeleteMsg struct {
	gen int
	ev  doctorDeleteEvent
}

// startDelete は下見 (dryRun) か本番の削除を走らせる。どちらも同じ経路で、
// 違うのは DryRun フラグだけ (確認プロンプトに出す内容を UI 側で組み直さないため)。
func (v *doctorView) startDelete(targets []disk.Result, dryRun bool) tea.Cmd {
	if v.del.cancel != nil {
		v.del.cancel() // 前の相 (下見) の ctx を残さない
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan doctorDeleteEvent, 16)
	// 相は 1 つだけ立てる。⚠️ confirm を落とし忘れると confirm && running の非正規状態になり、
	// 今は switch の並び順だけで無害になっている (並べ替えた瞬間に黙って壊れる)。
	// armedCC も引き継がない: 下見の最中に押した Ctrl-C が、本番の 1 回目で即中断に化ける
	v.del = doctorDelete{cancel: cancel, ch: ch, progress: "準備中",
		preparing: dryRun, running: !dryRun}
	gen := v.gen
	opt := v.deleteOptions()
	opt.DryRun = dryRun
	opt.OnPhase = func(i, total int, label string, p disk.DeletePhase) {
		// ⚠️ ノンブロッキング。読み手 (receiveDelete の再アーム) が止まると、engine が
		// **削除の途中で** channel 待ちに入る。進捗は落としてよい情報なので捨てる方へ倒す
		select {
		case ch <- doctorDeleteEvent{progress: fmt.Sprintf("%d/%d %s を%s", i+1, total, label, doctorPhaseWord(p))}:
		default:
		}
	}
	run := v.deleteFn
	if run == nil {
		run = disk.Delete
	}
	return tea.Batch(v.waitDeleteCmd(gen), func() tea.Msg {
		rep, err := run(ctx, targets, opt)
		ev := doctorDeleteEvent{rep: &rep, dryRun: dryRun}
		if err != nil {
			ev.err = err.Error()
		}
		ch <- ev
		return nil
	})
}

func doctorPhaseWord(p disk.DeletePhase) string {
	switch p {
	case disk.PhaseScanning:
		return "走査中"
	case disk.PhaseDeleting:
		return "削除中"
	case disk.PhaseVerifying:
		return "確認中"
	}
	return string(p)
}

// waitDeleteCmd は channel から 1 件だけ受けて Msg にする (Update の外で状態を触らない)。
func (v *doctorView) waitDeleteCmd(gen int) tea.Cmd {
	ch := v.del.ch
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return doctorDeleteMsg{gen: gen, ev: ev}
	}
}

// receiveDelete は進捗 / 完了を取り込む。次の 1 件を待つ Cmd を返す。
func (v *doctorView) receiveDelete(msg doctorDeleteMsg) tea.Cmd {
	if !v.shown || msg.gen != v.gen || !v.del.active() {
		return nil // 閉じた後に届いた古い Msg
	}
	ev := msg.ev
	if ev.rep == nil {
		v.del.progress = ev.progress
		return v.waitDeleteCmd(msg.gen)
	}
	v.del.preparing, v.del.running = false, false
	if ev.err != "" {
		v.del.err = ev.err
		return nil
	}
	if ev.dryRun {
		v.del.plan, v.del.confirm = ev.rep, true
		return nil
	}
	v.del.result = ev.rep
	return nil
}

// handleDeleteKey は削除の語彙。飲んだら true。
func (v *doctorView) handleDeleteKey(key string) (doctorAction, bool) {
	d := &v.del
	switch {
	case d.blocking():
		// 実行中は抜ける手段だけ残す。1 回目の Ctrl-C はブロックし、2 回目で cancel する
		// (途中終了は「消したのに記録に残らない」を作りうるので、誤爆を 1 段止める)
		if key == "ctrl+c" {
			if !d.armedCC {
				d.armedCC = true
				return doctorSwallow, true
			}
			if d.cancel != nil {
				d.cancel()
			}
		}
		return doctorSwallow, true
	case d.result != nil || d.err != "":
		// 結果はどのキーでも閉じる。閉じたら再スキャンして表示を実体に合わせる
		d.reset()
		return doctorRescan, true
	case d.confirm:
		switch key {
		case "y", "Y":
			targets := v.selectedResults()
			if len(targets) == 0 {
				d.reset()
				v.pendingToast = "削除する対象がありません"
				return doctorToast, true
			}
			// 入口 (beginDelete) だけで検査すると非対称になる。押す瞬間にもう一度見る
			for _, r := range targets {
				if ok, why := v.deletable(r); !ok {
					d.reset()
					v.pendingToast = r.Entry.Label + ": " + why
					return doctorToast, true
				}
			}
			v.pendingDeleteCmd = v.startDelete(targets, false)
			return doctorRunDelete, true
		default: // n / esc / その他はすべて中止 (既定は中止側)
			d.reset()
			return doctorSwallow, true
		}
	}
	return doctorSwallow, false
}

// selectedResults は選択中のエントリの、いま画面に出ている Result。
func (v *doctorView) selectedResults() []disk.Result {
	var out []disk.Result
	for _, r := range v.currentDiskResults() {
		if v.selected[r.Entry.ID] {
			out = append(out, r)
		}
	}
	return out
}

func (v *doctorView) currentDiskResults() []disk.Result {
	if v.diskRep != nil {
		return v.diskRep.Results
	}
	return v.diskResults
}

// deletable は「その行を選べるか」と、選べない理由。
//
// 🚨 走査していない行 (Reused / FromSnapshot) をここで止める。engine も拒否するが、
// 押した後に失敗するより、押す前に理由を出す方が親切。
func (v *doctorView) deletable(r disk.Result) (bool, string) {
	switch {
	case r.Status != disk.StatusOK:
		return false, "走査できていない行は削除できません"
	case len(r.Items) == 0:
		return false, "この行に削除対象はありません"
	case r.FromSnapshot:
		return false, "前回の結果を表示しています (r で再スキャンしてから削除してください)"
	case r.Reused:
		return false, "前回の計測を再利用した行です (r で再スキャンしてから削除してください)"
	case (r.Entry.Inspect || r.Entry.Risk == disk.RiskConfirm) && !v.inspected[r.Entry.ID]:
		// ユーザーのファイルでありうる行は、中身を一度見るまで選べない (issue 148 の 3 章)。
		// ⚠️ Inspect だけを見ると、カタログが `RiskConfirm` に `Inspect` を付け忘れた瞬間に
		// ゲートが消える (今の 5 件はたまたま両方立っている)。危険度そのものも条件にする
		return false, "中身を確認してから選んでください (Enter で開く)"
	}
	return true, ""
}

// toggleSelect は現在行の選択を切り替える。選べない行なら理由を返す。
func (v *doctorView) toggleSelect() (string, bool) {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return "", false
	}
	id, ok := strings.CutPrefix(v.rows[v.cursor].key, "disk:")
	if !ok {
		return "選べるのはディスクの行だけです", false
	}
	for _, r := range v.currentDiskResults() {
		if r.Entry.ID != id {
			continue
		}
		if ok, why := v.deletable(r); !ok {
			return why, false
		}
		if v.selected == nil {
			v.selected = map[string]bool{}
		}
		v.selected[id] = !v.selected[id]
		if !v.selected[id] {
			delete(v.selected, id)
		}
		return "", true
	}
	return "", false
}

// selectionSummary は選択中の件数と合計。
func (v *doctorView) selectionSummary() (int, int64) {
	var n int
	var total int64
	for _, r := range v.selectedResults() {
		n++
		total += r.Size
	}
	return n, total
}

// ownsKeys は削除の語彙がキーを持っているか (browseModel の updateKeyReachable が見る)。
func (v *doctorView) ownsKeys() bool { return v.del.active() }

// takeDeleteCmd は handleKey が組んだ Cmd を取り出す (1 回だけ返す)。
func (v *doctorView) takeDeleteCmd() tea.Cmd {
	cmd := v.pendingDeleteCmd
	v.pendingDeleteCmd = nil
	return cmd
}

// beginDelete は d の入口。選択が無い / 走査中 / 走査していない行が混ざっているときは理由を出す。
func (v *doctorView) beginDelete() doctorAction {
	switch {
	case v.scanning():
		v.pendingToast = "スキャン中は削除できません (終わるまで待つか r で取り直してください)"
		return doctorToast
	case len(v.selectedResults()) == 0:
		v.pendingToast = "Space で削除するものを選んでください"
		return doctorToast
	}
	// 🚨 走査していない行は engine が拒否する。押す前にここで止める
	for _, r := range v.selectedResults() {
		if ok, why := v.deletable(r); !ok {
			v.pendingToast = r.Entry.Label + ": " + why
			return doctorToast
		}
	}
	v.pendingDeleteCmd = v.startDelete(v.selectedResults(), true) // 下見 (何も壊さない)
	return doctorRunDelete
}

// deletePanel は確認 / 進捗 / 結果の全画面パネル。doctor 自体が全画面なので、重ねずに差し替える
// (重ねると狭い幅で下の行が透けて「どれを消すのか」が読めなくなる)。
func (v *doctorView) deletePanel(o doctorRenderOpts) []string {
	d := &v.del
	switch {
	case d.err != "":
		return doctorPanel(o, "削除できませんでした", []string{
			d.err, "", "何も消えていません。", "", "何かキーを押すと閉じます"})
	case d.result != nil:
		blocks, tail := doctorDeleteResultLines(o, *d.result)
		return assembleDeletePanel(o, "削除の結果", blocks, tail)
	case d.blocking():
		head := "削除しています"
		if d.preparing {
			head = "削除できるか確認しています"
		}
		body := []string{d.progress, ""}
		if d.preparing {
			body = append(body, "対象を走査し直しています (消してよいかを測り直します)")
		} else {
			body = append(body, "Ctrl-C を 2 回押すと中断します")
			if d.armedCC {
				body = append(body, "", "もう一度 Ctrl-C を押すと中断します")
			}
		}
		return doctorPanel(o, head, body)
	case d.confirm:
		blocks, tail := v.confirmLines(o)
		return assembleDeletePanel(o, "本当に削除しますか?", blocks, tail)
	}
	return nil
}

// confirmLines は確認の本文。**下見 (DryRun) の結果をそのまま出す** (UI 側で組み直さない)。
//
// 並びは一覧の行と同じ「サイズ / ラベル / 語」。⚠️ 記号を先頭に置く形は採らない:
// `・` (全角) と絵文字と `—` が同じ列に来て、幅は合っていても目には揃わない
// (~/.claude/rules/no-mixed-width-columns-in-terminal-ui.md)。語彙は固定にする。
//
// ⚠️ 記号は**単独で幅 2 のもの**だけを使う。`🗑` は実測で幅 1、`🗑️` (VS16 付き) は 2 で、
// 端末によって右端が動く。既存の doctorRiskMark と同じ語彙 (✅ 🚨 🚫 ⛔ ❓) に `🚮` `📋` `❌` を足した。
func (v *doctorView) confirmLines(o doctorRenderOpts) (blocks [][]string, tail []string) {
	labelW := deleteLabelWidth(v.del.plan.Entries, o.width)
	var freeing, trashing int64
	for _, e := range v.del.plan.Entries {
		var out []string
		size, word := disk.HumanSize(e.BeforeSize), deleteMethodWord(e.Method)
		skipped := e.Outcome == disk.OutcomeSkipped || e.Outcome == disk.OutcomeFailed
		switch {
		case skipped:
			size, word = "---", "🚫 対象外"
		case e.Method == "trash":
			trashing += e.BeforeSize
		case e.Method != "propose":
			freeing += e.BeforeSize
		}
		out = append(out, fmt.Sprintf(" %8s  %s  %s", size, padLabel(e.Label, labelW), word))
		switch {
		case skipped:
			out = append(out, deleteNote(o, e.Reason))
		case e.Method == "trash":
			out = append(out, deleteNote(o, fmt.Sprintf("%d 件をゴミ箱へ移動 (空にするまで容量は戻りません)", len(e.Items))))
		case e.Method == "cli":
			out = append(out, deleteNote(o, fmt.Sprintf("%s を実行 (%d 件)", e.Command, len(e.Items))))
		case e.Method == "propose":
			out = append(out, deleteNote(o, "コマンドを表示するだけで、実行しません"))
		default:
			out = append(out, deleteNote(o, fmt.Sprintf("%d 件を削除", len(e.Items))))
		}
		blocks = append(blocks, out)
	}
	// ⚠️ 合計は**1 行にまとめる**。2 行に割ると、狭い画面で先に落ちて「1 件目のサイズだけが
	// 見えている状態で y を受ける」形になる (敵対レビュー 2026-09-03: 78GB の削除で 1.0GB しか
	// 見えなかった)。assembleDeletePanel は末尾を後ろから残すので、1 行なら生き残りやすい
	// ⚠️ tail は「**捨ててよい順**」に並べる (assembleDeletePanel は前から削る)。
	// 空行 → 合計 → 操作の説明。最後の行は必ず残る
	tail = append(tail, "")
	if sum := deleteTotalsLine("解放される見込み", freeing, trashing); sum != "" {
		tail = append(tail, sum)
	}
	return blocks, append(tail, " y: 削除する      n / Esc: やめる")
}

// doctorDeleteResultLines は結果。**incomplete を成功にも失敗にも畳まない**。
func doctorDeleteResultLines(o doctorRenderOpts, rep disk.DeleteReport) (blocks [][]string, tail []string) {
	labelW := deleteLabelWidth(rep.Entries, o.width)
	for _, e := range rep.Entries {
		out := []string{fmt.Sprintf(" %8s  %s  %s",
			deleteResultSize(e), padLabel(e.Label, labelW), doctorOutcomeWord(e.Outcome))}
		if e.Reason != "" {
			out = append(out, deleteNote(o, e.Reason))
		}
		blocks = append(blocks, out)
	}
	// 捨ててよい順: 空行 → 記録のパス → 記録のエラー → 合計 → 操作の説明
	tail = append(tail, "")
	if rep.HistoryPath != "" {
		tail = append(tail, " 記録: "+rep.HistoryPath)
	}
	if rep.HistoryError != "" {
		tail = append(tail, " 🚨 記録を書けませんでした: "+rep.HistoryError)
	}
	if sum := deleteTotalsLine("解放しました", rep.Freed, rep.Trashed); sum != "" {
		tail = append(tail, sum)
	} else {
		tail = append(tail, " 解放された容量はありません")
	}
	return blocks, append(tail, " 何かキーを押すと閉じ、もう一度スキャンします")
}

// deleteTotalsLine は合計を 1 行にまとめる (狭い画面で落ちにくくするため)。0 なら空。
func deleteTotalsLine(label string, freed, trashed int64) string {
	switch {
	case freed > 0 && trashed > 0:
		return fmt.Sprintf(" %s: %s / ゴミ箱へ %s (空にするまで容量は戻りません)",
			label, disk.HumanSize(freed), disk.HumanSize(trashed))
	case freed > 0:
		return fmt.Sprintf(" %s: %s", label, disk.HumanSize(freed))
	case trashed > 0:
		return fmt.Sprintf(" ゴミ箱へ %s (空にするまで容量は戻りません)", disk.HumanSize(trashed))
	}
	return ""
}

func deleteResultSize(e disk.EntryOutcome) string {
	switch e.Outcome {
	case disk.OutcomeDeleted:
		return disk.HumanSize(e.Freed)
	case disk.OutcomeTrashed:
		return disk.HumanSize(e.Trashed)
	case disk.OutcomePlanned, disk.OutcomeProposed, disk.OutcomeIncomplete,
		disk.OutcomeSkipped, disk.OutcomeFailed:
		// 消えていない / 消えたか分からないものに数字を出さない (「---」で区別する)
	}
	return "---"
}

// doctorOutcomeWord は結末の固定語彙 (記号 + 語)。一覧の doctorRiskMark と同じ作り:
// NO_COLOR で色が消えても語で区別できるようにする。
func doctorOutcomeWord(o disk.Outcome) string {
	switch o {
	case disk.OutcomeDeleted:
		return "✅ 削除した"
	case disk.OutcomeTrashed:
		return "🚮 ゴミ箱へ"
	case disk.OutcomeIncomplete:
		return "🚨 未完了"
	case disk.OutcomeSkipped:
		return "🚫 触れず"
	case disk.OutcomeProposed:
		return "📋 表示のみ"
	case disk.OutcomeFailed:
		return "❌ できず"
	case disk.OutcomePlanned:
		return "・これから"
	}
	return string(o)
}

func deleteMethodWord(method string) string {
	switch method {
	case "trash":
		return "🚮 ゴミ箱へ"
	case "cli":
		return "📋 コマンド"
	case "propose":
		return "📋 表示のみ"
	}
	return "✅ 削除"
}

// deleteLabelWidth はラベル列の幅 (画面が狭ければ縮める)。
func deleteLabelWidth(entries []disk.EntryOutcome, width int) int {
	w := 0
	for _, e := range entries {
		w = max(w, dispWidth(e.Label))
	}
	return max(8, min(w, width-deleteRowFixedW))
}

// deleteRowFixedW は 1 行のうちラベル以外が使う幅 (先頭 1 + サイズ 8 + 区切り 2 + 区切り 2 + 語 13)。
// 語は固定語彙で最長が「🚮 ゴミ箱へ」= 2+1+8 と「📋 コマンド」= 2+1+10 なので 13。
const deleteRowFixedW = 26

func padLabel(label string, w int) string {
	label = truncateDisp(label, w, "…")
	return label + padSpaces(w-dispWidth(label))
}

// deleteNote はラベル列の下に付く補足 (dim)。1 行 = 1 件の契約なので改行は入れない。
func deleteNote(o doctorRenderOpts, s string) string {
	return doctorColor(o.colored, ansiDim, "           "+s)
}

// assembleDeletePanel は「見出し + エントリの塊 + 末尾」をちょうど page 行に収める。
//
// 🚨 padTo は**溢れた分を切る** (lines[:n])。素直に並べると、選択が多いときに末尾の
// 「y: 削除する n/Esc: やめる」が消え、**操作が分からない確認プロンプト**になる。
// なので末尾を先に確保し、入る分だけエントリを載せて、省いた件数を注記する。
func assembleDeletePanel(o doctorRenderOpts, title string, blocks [][]string, tail []string) []string {
	head := []string{" " + doctorColor(o.colored, ansiBold, title), ""}
	room := max(o.page-len(head), 1)
	// 末尾は**後ろから**優先して残す。tail の並びは「捨ててよい順」= 空行 → 補足 → 合計 → 操作の説明
	kept := tail
	for len(kept) > 1 && len(kept) > room {
		kept = kept[1:]
	}
	avail := max(room-len(kept), 0)
	var need int
	for _, b := range blocks {
		need += len(b)
	}
	elide := need > avail
	if elide {
		avail = max(avail-1, 0) // 「他 N 件」の注記のぶんを先に確保する
	}
	var body []string
	shown := 0
	for _, b := range blocks {
		if len(body)+len(b) > avail {
			break
		}
		body = append(body, b...)
		shown++
	}
	if elide && avail > 0 {
		body = append(body, doctorColor(o.colored, ansiDim,
			fmt.Sprintf(" … 他 %d 件は画面に入りません (端末を広げてください)", len(blocks)-shown)))
	}
	out := make([]string, 0, len(head)+len(body)+len(kept))
	out = append(out, head...)
	out = append(out, body...)
	out = append(out, kept...)
	for i, l := range out {
		out[i] = truncateDisp(l, o.width, "…")
	}
	return padTo(out, o.page)
}

// doctorPanel は見出し + 本文を全画面に敷く (行数は呼び出し側の lines が padTo で揃える)。
func doctorPanel(o doctorRenderOpts, title string, body []string) []string {
	out := []string{" " + doctorColor(o.colored, ansiBold, title), ""}
	for _, l := range body {
		out = append(out, truncateDisp(" "+l, o.width, "…"))
	}
	return out
}
