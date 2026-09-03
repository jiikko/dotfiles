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

	// log は実行したコマンドとその出力 (実行中は垂れ流し、終わった後も残す)。
	// 失敗したときに LLM へ投げられるよう、y でまるごとコピーできる
	log []string

	cancel context.CancelFunc
	// ch は進捗とコマンドの出力 (落としてよい)。done は**完了**専用の 1 バッファ。
	// 🚨 完了を取りこぼすと UI が「実行中」のまま永久に止まる。進捗で埋まった ch に
	// 完了を載せると詰まり、ctx で諦めると今度は届かない (2026-09-03 に実測: テストが 10 秒で
	// タイムアウトした)。**落としてよいもの**と**落としてはいけないもの**を別の口にする
	ch      chan doctorDeleteEvent
	done    chan doctorDeleteEvent
	armedCC bool // 実行中に Ctrl-C が 1 回押された (2 回目で cancel)

	// confirmScroll は確認パネルの縦位置。**破壊的操作の確認で「対象が見えない」を作らない**ため
	// (issue 241)。以前は入り切らない塊を落として「端末を広げてください」と言うだけで、
	// 送ろうとして押したキーは下の default に落ちて確認ごと無言で閉じていた
	confirmScroll deleteScroll
}

// deleteScroll は確認パネルの縦位置。total / view は**最後に描いたとき**の値で、
// キー操作 (画面を持たない) 側が送り幅と上限を決めるために要る。
// 🚨 描画は状態を持たない方が望ましいが、この 2 つは「今の画面で何行見えているか」なので
// 描画側にしか無い。同 package の rowCursor も同じ形 (restore が index を描画時に寄せる)。
type deleteScroll struct {
	offset int // 本文の先頭から何行送ったか
	total  int // 本文の全行数
	view   int // 実際に見えている行数
}

// window は本文を今の位置で切り出し、入り切らないぶんを注記で伝える。
// 入り切るときは注記を出さない (常に出すと、送る必要が無い画面でも送れると誤解させる)。
func (s *deleteScroll) window(o doctorRenderOpts, all []string, avail int) []string {
	s.total = len(all)
	if avail <= 0 {
		// 本文に使える行が無い (極端に低い画面)。注記も出さない:
		// 1 行でも返すと末尾 (合計と操作の説明) が押し出される
		s.offset, s.view = 0, 0
		return nil
	}
	if len(all) <= avail {
		s.offset, s.view = 0, len(all)
		return all
	}
	view := max(avail-1, 0) // 位置の注記 1 行を先に確保する
	s.view = view
	s.offset = min(max(s.offset, 0), len(all)-view)
	above, below := s.offset, len(all)-s.offset-view
	var parts []string
	if above > 0 {
		parts = append(parts, fmt.Sprintf("上に %d 行", above))
	}
	if below > 0 {
		parts = append(parts, fmt.Sprintf("下に %d 行", below))
	}
	out := make([]string, 0, view+1)
	out = append(out, all[s.offset:s.offset+view]...)
	return append(out, doctorColor(o.colored, ansiDim,
		fmt.Sprintf(" … %s (j/k で送る)", strings.Join(parts, " / "))))
}

// scroll は最後に描いた画面の高さを基準に位置を動かす。
func (s *deleteScroll) scroll(key string) {
	maxOff := max(s.total-s.view, 0)
	half := max(s.view/2, 1)
	switch key {
	case "j", "down":
		s.offset++
	case "k", "up":
		s.offset--
	case "ctrl+d", "pgdown":
		s.offset += half
	case "ctrl+u", "pgup":
		s.offset -= half
	case "g", "home":
		s.offset = 0
	case "G", "end":
		s.offset = maxOff
	}
	s.offset = min(max(s.offset, 0), maxOff)
}

// deleteScrollKeys は確認パネルで**中止に落とさない**キー (送るだけ)。
// 🚨 ここに無いキーは既定どおり中止側へ落ちる (安全側)。送る手段が無いまま
// 「送ろうとした打鍵で確認が消える」のを塞ぐのが目的で、既定を緩めるものではない
var deleteScrollKeys = map[string]bool{
	"j": true, "down": true, "k": true, "up": true,
	"ctrl+d": true, "pgdown": true, "ctrl+u": true, "pgup": true,
	"g": true, "home": true, "G": true, "end": true,
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
	cmd      *disk.CommandRecord // cli: の 1 コマンドが終わった
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
	done := make(chan doctorDeleteEvent, 1)
	// 相は 1 つだけ立てる。🚨 confirm を落とし忘れると confirm && running の非正規状態になり、
	// 今は switch の並び順だけで無害になっている (並べ替えた瞬間に黙って壊れる)。
	// armedCC も引き継がない: 下見の最中に押した Ctrl-C が、本番の 1 回目で即中断に化ける
	v.del = doctorDelete{cancel: cancel, ch: ch, done: done, progress: "準備中",
		preparing: dryRun, running: !dryRun}
	gen := v.gen
	opt := v.deleteOptions()
	opt.DryRun = dryRun
	opt.OnPhase = func(i, total int, label string, p disk.DeletePhase) {
		// 🚨 ノンブロッキング。読み手 (receiveDelete の再アーム) が止まると、engine が
		// **削除の途中で** channel 待ちに入る。進捗は落としてよい情報なので捨てる方へ倒す
		select {
		case ch <- doctorDeleteEvent{progress: fmt.Sprintf("%d/%d %s を%s", i+1, total, label, doctorPhaseWord(p))}:
		default:
		}
	}
	opt.OnCommand = func(rec disk.CommandRecord) {
		select {
		case ch <- doctorDeleteEvent{cmd: &rec}:
		default: // 読み手が詰まっても engine を止めない (進捗と同じ扱い)
		}
	}
	run := v.deleteFn
	if run == nil {
		run = disk.Delete
	}
	return tea.Batch(v.waitDeleteCmd(gen), func() tea.Msg {
		// 🚨 **削除も latch に載せる** (issue 211 の敵対的レビュー P1)。走査 3 本だけ看取っても、
		// いちばん危ない経路 (rm / trash / brew cleanup / simctl delete と、その後のインベントリ
		// 記録) が終了・再起動で watchdog ごと消えて走り続ける。CmdTimeout は 5 分ある
		var rep disk.DeleteReport
		var err error
		doctorTrack(func() { rep, err = run(ctx, targets, opt) })
		ev := doctorDeleteEvent{rep: &rep, dryRun: dryRun}
		if err != nil {
			ev.err = err.Error()
		}
		done <- ev // cap 1 の専用口。進捗で詰まらないし、取りこぼしもしない
		return nil
	})
}

// commandLogLines は 1 コマンドの記録を、そのまま貼れる形の行にする。
// **stdout と stderr を混ぜない** (どちらに出たかが判定材料なので、印で分ける)。
func commandLogLines(rec disk.CommandRecord) []string {
	out := []string{"$ " + strings.TrimSpace(rec.Name+" "+strings.Join(rec.Args, " "))}
	switch {
	case rec.Err != "":
		out = append(out, "  ! "+rec.Err)
	default:
		out = append(out, fmt.Sprintf("  exit %d", rec.RC))
	}
	for _, l := range strings.Split(rec.Stdout, "\n") {
		if l != "" {
			out = append(out, "  1| "+l) // stdout
		}
	}
	for _, l := range strings.Split(rec.Stderr, "\n") {
		if l != "" {
			out = append(out, "  2| "+l) // stderr
		}
	}
	return out
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
	ch, done := v.del.ch, v.del.done
	if ch == nil || done == nil {
		return nil
	}
	return func() tea.Msg {
		// 溜まっている進捗を先に出す (両方 ready のときに select が完了を選ぶと、
		// 直前のコマンドの出力が画面に出ないまま結果へ飛ぶ)
		select {
		case ev := <-ch:
			return doctorDeleteMsg{gen: gen, ev: ev}
		default:
		}
		select {
		case ev := <-ch:
			return doctorDeleteMsg{gen: gen, ev: ev}
		case ev := <-done:
			return doctorDeleteMsg{gen: gen, ev: ev}
		}
	}
}

// receiveDelete は進捗 / 完了を取り込む。次の 1 件を待つ Cmd を返す。
func (v *doctorView) receiveDelete(msg doctorDeleteMsg) tea.Cmd {
	if !v.shown || msg.gen != v.gen || !v.del.active() {
		return nil // 閉じた後に届いた古い Msg
	}
	ev := msg.ev
	if ev.cmd != nil {
		v.del.log = append(v.del.log, commandLogLines(*ev.cmd)...)
		return v.waitDeleteCmd(msg.gen)
	}
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
		v.del.confirmScroll = deleteScroll{} // 開き直しは必ず先頭から見せる
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
		// y / Y は出力をコピー (失敗したときに LLM へそのまま投げられる形)。閉じない
		if key == "y" || key == "Y" {
			v.pendingCopy = v.deleteLogText() // 見出しを必ず書くので空にならない
			return doctorCopyLog, true
		}
		// それ以外はどのキーでも閉じる。閉じたら再スキャンして表示を実体に合わせる
		d.reset()
		return doctorRescan, true
	case d.confirm && !planHasWork(d.plan):
		d.reset() // 消せるものが無いので、どのキーでも戻る
		return doctorSwallow, true
	case d.confirm:
		// 🚨 送るキーは**中止に落とさない** (issue 241)。窓に入り切らない対象を見ようとした
		// 打鍵で確認が消えるのを塞ぐ。ここに無いキーは既定どおり中止側 (安全側) へ落ちる
		if deleteScrollKeys[key] {
			d.confirmScroll.scroll(key)
			return doctorSwallow, true
		}
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

// deleteLogText は「別セッションの LLM にそのまま投げられる」形の実行記録。
// 実行したコマンド・終了コード・stdout・stderr を**分けたまま**入れる。
func (v *doctorView) deleteLogText() string {
	d := &v.del
	var b strings.Builder
	b.WriteString("glogx doctor の削除の記録 (macOS)\n")
	if d.err != "" {
		b.WriteString("\n中止した理由: " + d.err + "\n")
	}
	if rep := d.result; rep != nil {
		for _, e := range rep.Entries {
			fmt.Fprintf(&b, "\n[%s] %s", e.Label, doctorOutcomeWord(e.Outcome))
			if e.Reason != "" {
				b.WriteString(" — " + e.Reason)
			}
			b.WriteString("\n")
			for _, it := range e.Items {
				fmt.Fprintf(&b, "  %s  %s %s\n", doctorOutcomeWord(it.Outcome), it.Path, it.Reason)
			}
		}
		if rep.HistoryPath != "" {
			b.WriteString("\n記録: " + rep.HistoryPath + "\n")
		}
	}
	if len(d.log) > 0 {
		b.WriteString("\n実行したコマンド (1| = stdout / 2| = stderr):\n")
		for _, l := range d.log {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// selectedResults は削除に渡す Result。**エントリ全体を選んだものは丸ごと、ディレクトリ単位で
// 選んだものは Items をその部分集合にした Result** を返す。
//
// engine (disk.Delete) は渡された Items を「今回の走査でも候補だったか」で照合するので、
// **部分集合をそのまま渡してよい** (UI 側で消す対象を組み直す必要はない)。
func (v *doctorView) selectedResults() []disk.Result {
	out := make([]disk.Result, 0, len(v.selected)+len(v.selectedItems))
	for _, r := range v.currentDiskResults() {
		if v.selected[r.Entry.ID] {
			out = append(out, r)
			continue
		}
		var items []disk.Item
		var size int64
		for _, it := range r.Items {
			if v.selectedItems[diskItemKey(r.Entry.ID, it.Path)] {
				items = append(items, it)
				size += it.Size
			}
		}
		if len(items) == 0 {
			continue
		}
		part := r
		part.Items, part.Size = items, size
		out = append(out, part)
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
	case r.Status == disk.StatusBlocked:
		// blocked は「走査**できた上で**今は対象外」。走査できなかったのと混ぜない (issue 182 の語彙)
		return false, "いまは対象外の行です: " + r.Reason
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
		// 🚨 Inspect だけを見ると、カタログが `RiskConfirm` に `Inspect` を付け忘れた瞬間に
		// ゲートが消える (今の 5 件はたまたま両方立っている)。危険度そのものも条件にする
		return false, "中身を確認してから選んでください (Enter で開く)"
	}
	return true, ""
}

// toggleSelect は現在行の選択を切り替える。選べない行なら理由を返す。
//
// 2 つの粒度がある: **エントリ全体** (`disk:` の行) と **ディレクトリ単位** (`diskitem:` の行)。
// 後者は Enter で開いた中の対象パス。どちらも同じ `deletable` のゲートを通す
// (ゲートはエントリの性質 = 走査の新しさ・危険度で決まるので、粒度で緩めない)。
func (v *doctorView) toggleSelect() (string, bool) {
	if v.cur.index < 0 || v.cur.index >= len(v.rows) {
		return "", false
	}
	key := v.rows[v.cur.index].key
	if itemKey, ok := strings.CutPrefix(key, "diskitem:"); ok {
		return v.toggleItem(itemKey)
	}
	id, ok := strings.CutPrefix(key, "disk:")
	if !ok {
		return "選べるのはディスクの行だけです", false
	}
	r, ok := v.findDiskResult(id)
	if !ok {
		return "この行の結果が見つかりません (r で取り直してください)", false
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
	// エントリ全体を選んだら、その中の個別選択は畳む (二重に数えない)
	if v.selected[id] {
		v.clearItemsOf(id)
	}
	return "", true
}

// toggleItem はディレクトリ単位の選択。
func (v *doctorView) toggleItem(itemKey string) (string, bool) {
	id, _, ok := strings.Cut(itemKey, "\x00")
	if !ok {
		return "", false
	}
	r, ok := v.findDiskResult(id)
	if !ok {
		return "この行の結果が見つかりません (r で取り直してください)", false
	}
	if ok, why := v.deletable(r); !ok {
		return why, false
	}
	if v.selectedItems == nil {
		v.selectedItems = map[string]bool{}
	}
	v.selectedItems[itemKey] = !v.selectedItems[itemKey]
	if !v.selectedItems[itemKey] {
		delete(v.selectedItems, itemKey)
	}
	// 個別に選んだら、エントリ全体の選択は畳む (「全部」と「一部」を同時に立てない)
	if v.selectedItems[itemKey] {
		delete(v.selected, id)
	}
	return "", true
}

func (v *doctorView) findDiskResult(id string) (disk.Result, bool) {
	for _, r := range v.currentDiskResults() {
		if r.Entry.ID == id {
			return r, true
		}
	}
	return disk.Result{}, false
}

// hasSelectedItems はそのエントリの中で個別に選ばれたものがあるか (行頭の印に使う)。
func (v *doctorView) hasSelectedItems(id string) bool {
	for k := range v.selectedItems {
		if strings.HasPrefix(k, id+"\x00") {
			return true
		}
	}
	return false
}

func (v *doctorView) clearItemsOf(id string) {
	for k := range v.selectedItems {
		if strings.HasPrefix(k, id+"\x00") {
			delete(v.selectedItems, k)
		}
	}
}

// selectionSummary は選択中の**ディレクトリ数**と合計。
// 🚨 エントリ数で数えない: 確認画面が「N 件を削除」を Item 数で出すので、hint だけ
// エントリ数だと「hint は 1 件、確認は 3 件」になる (敵対レビュー 2026-09-03)。
func (v *doctorView) selectionSummary() (int, int64) {
	var n int
	var total int64
	for _, r := range v.selectedResults() {
		n += len(r.Items)
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

// snapshotRescan は「前回の結果」を表示している画面で削除の操作をしたときに、**拒否ではなく
// 再スキャンへ倒す**。
//
// 🚨 復元した画面は**全行が FromSnapshot** なので、拒否したままだと「サイズは見えているのに
// 何を選んでも断られる」行き止まりになる (ユーザー報告 2026-09-03: Enter で開いた後 Space で
// 選べない)。ヘッダーに「N 分前の結果 (r で再スキャン)」とは出ているが、**削除しようとした
// その瞬間**に気づける形ではなかった。押した意図 (これを消したい) は再スキャンで果たせる。
func (v *doctorView) snapshotRescan() (doctorAction, bool) {
	if v.snapshotAt.IsZero() {
		return doctorSwallow, false
	}
	// 🚨 削除に関係ない行 (brew の警告 / svc) の上では起こさない。押した意図
	// (この行を消したい) が存在しないので、全体の再スキャンは驚きにしかならない
	if v.cur.index >= 0 && v.cur.index < len(v.rows) {
		k := v.rows[v.cur.index].key
		if !strings.HasPrefix(k, "disk:") && !strings.HasPrefix(k, "diskitem:") {
			return doctorSwallow, false
		}
	}
	v.pendingToast = "前回の結果を表示していたので、取り直します (終わったら選び直してください)"
	return doctorRescan, true
}

// beginDelete は d の入口。選択が無い / 走査中 / 走査していない行が混ざっているときは理由を出す。
func (v *doctorView) beginDelete() doctorAction {
	if act, ok := v.snapshotRescan(); ok {
		return act
	}
	switch {
	case v.diskRep == nil:
		// 🚨 v.scanning() ではない: あれは svc / brew も見るので、ディスクが完走していても
		// brew doctor (最大 60 秒) の間ずっと d が通らなくなる。削除に要るのはディスクの結果だけ。
		// 助言も「r で取り直す」と言わない (全部を最初からやり直すので待ち時間が増えるだけ)
		v.pendingToast = "ディスクのスキャンが終わるまで待ってください"
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
		body := []string{d.err, "", "何も消えていません。"}
		if len(d.log) > 0 {
			body = append(body, "", " 実行したコマンド:")
			body = append(body, d.log...)
		}
		return doctorPanel(o, "削除できませんでした", append(body, "", "y: 出力をコピー   他のキー: 閉じる"))
	case d.result != nil:
		blocks, tail := doctorDeleteResultLines(o, *d.result, d.log)
		return assembleDeletePanel(o, "削除の結果", blocks, tail, nil)
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
				body = append(body, "もう一度 Ctrl-C を押すと中断します")
			}
		}
		// 実行したコマンドと出力を垂れ流す (何が起きているかを見せる)。入る分だけ末尾を出す
		if len(d.log) > 0 {
			body = append(body, "")
			body = append(body, tailLines(d.log, max(o.page-len(body)-3, 1))...)
		}
		return doctorPanel(o, head, body)
	case d.confirm:
		blocks, tail := v.confirmLines(o)
		title := "本当に削除しますか?"
		if !planHasWork(d.plan) {
			// 下見の結果、消せるものが 1 件も無かった。**「削除しますか?」と聞かない**
			// (y に意味が無いのに押させる形になる)
			title = "消せるものがありません"
		}
		return assembleDeletePanel(o, title, blocks, tail, &d.confirmScroll)
	}
	return nil
}

// planHasWork は下見の結果に「実際に消すもの」があるか (全部 対象外 / 提示のみ なら false)。
func planHasWork(plan *disk.DeleteReport) bool {
	if plan == nil {
		return false
	}
	for _, e := range plan.Entries {
		if e.Outcome == disk.OutcomePlanned {
			return true
		}
	}
	return false
}

// confirmLines は確認の本文。**下見 (DryRun) の結果をそのまま出す** (UI 側で組み直さない)。
//
// 並びは一覧の行と同じ「サイズ / ラベル / 語」。🚨 記号を先頭に置く形は採らない:
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
			// コマンドの実体はこの下に 1 本ずつ出るので、ここでは件数だけ (二重に出さない)
			out = append(out, deleteNote(o, fmt.Sprintf("%d 件にコマンドを実行", len(e.Items))))
		case e.Method == "propose":
			out = append(out, deleteNote(o, "コマンドを表示するだけで、実行しません"))
		default:
			out = append(out, deleteNote(o, fmt.Sprintf("%d 件を削除", len(e.Items))))
		}
		if !skipped {
			out = append(out, deleteCommandLines(o, e)...)
			out = append(out, deletePathLines(o, e)...)
		}
		blocks = append(blocks, out)
	}
	// 🚨 合計は**1 行にまとめる**。2 行に割ると、狭い画面で先に落ちて「1 件目のサイズだけが
	// 見えている状態で y を受ける」形になる (敵対レビュー 2026-09-03: 78GB の削除で 1.0GB しか
	// 見えなかった)。assembleDeletePanel は末尾を後ろから残すので、1 行なら生き残りやすい
	// 🚨 tail は「**捨ててよい順**」に並べる (assembleDeletePanel は前から削る)。
	// 空行 → 合計 → 操作の説明。最後の行は必ず残る
	tail = append(tail, "")
	if sum := deleteTotalsLine("解放される見込み", freeing, trashing); sum != "" {
		tail = append(tail, sum)
	}
	if !planHasWork(v.del.plan) {
		return blocks, append(tail, " 何かキーを押すと戻ります")
	}
	return blocks, append(tail, " y: 削除する      n / Esc: やめる")
}

// deleteCommandLines は「このエントリで実際に実行するコマンド」を並べる (ユーザー要望 2026-09-03)。
// 組み立ては engine (EntryOutcome.CommandLines) が持つ = **確認に出した形と実行する形が同じ**。
// rm / trash は外部コマンドを起こさないので何も出さない (経路の語が既にそう言っている)。
func deleteCommandLines(o doctorRenderOpts, e disk.EntryOutcome) []string {
	if e.Method == "propose" {
		return []string{deleteNote(o, "実行しません。手で叩いてください: "+cleanOneLine(e.Command))}
	}
	cmds := e.CommandLines()
	out := make([]string, 0, min(len(cmds), maxConfirmPaths)+1)
	for i, c := range cmds {
		if i >= maxConfirmPaths {
			out = append(out, deleteNote(o, fmt.Sprintf("… 他 %d 本", len(cmds)-maxConfirmPaths)))
			break
		}
		out = append(out, deleteNote(o, "$ "+cleanOneLine(c)))
	}
	return out
}

// deletePathLines は「これから触るもの」をフルパスで並べる。
//
// 🚨 確認の本体はここ。ラベルとサイズだけでは「どのディレクトリが消えるのか」が分からず、
// **中身を確かめずに y を押す**ことになる (ユーザー要望 2026-09-03)。パスは engine が
// 走査し直して正規化したもの = 実際に触る対象そのもの。
//
// 🚨 1 エントリあたりの表示は maxConfirmPaths 件で打ち切る。全部並べると 1 エントリで画面を
// 埋め、**他のエントリが丸ごと省略される** (assembleDeletePanel は塊単位で落とすため)。
// 打ち切ったことは件数で伝える。
func deletePathLines(o doctorRenderOpts, e disk.EntryOutcome) []string {
	out := make([]string, 0, min(len(e.Items), maxConfirmPaths)+1)
	for i, it := range e.Items {
		if i >= maxConfirmPaths {
			out = append(out, deleteNote(o, fmt.Sprintf("… 他 %d 件", len(e.Items)-maxConfirmPaths)))
			break
		}
		// 🚨 パスは**ファイル名由来**なので改行や制御文字が入りうる (macOS のファイル名は
		// `/` と NUL 以外を許す)。1 行 = 1 件の契約を破ると、確認画面に偽の行
		// (「y: 削除する」等) を差し込めてしまう。印字可能文字だけに絞る
		out = append(out, deleteNote(o, cleanOneLine(it.Path)))
	}
	return out
}

const maxConfirmPaths = 10

// doctorDeleteResultLines は結果。**incomplete を成功にも失敗にも畳まない**。
func doctorDeleteResultLines(o doctorRenderOpts, rep disk.DeleteReport, log []string) (blocks [][]string, tail []string) {
	labelW := deleteLabelWidth(rep.Entries, o.width)
	for _, e := range rep.Entries {
		out := []string{fmt.Sprintf(" %8s  %s  %s",
			deleteResultSize(e), padLabel(e.Label, labelW), doctorOutcomeWord(e.Outcome))}
		if e.Reason != "" {
			out = append(out, deleteNote(o, e.Reason))
		}
		blocks = append(blocks, out)
	}
	// 実行したコマンドの記録は**塊として**出す (エントリの後、合計の前)。失敗したときに
	// ここを読んで原因が分かる形にする。y でまるごとコピーできる
	if len(log) > 0 {
		blocks = append(blocks, append([]string{" 実行したコマンド:"}, log...))
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
// assembleDeletePanel は見出し + 本文 + 末尾を組む。sc != nil の呼び出し (確認パネル) は
// 本文を**行単位の窓**で見せる: 塊単位で落とす形だと、1 つの塊が窓より大きいときに
// 本文が丸ごと消える (issue 241)。sc == nil (結果パネル) は従来どおり塊単位で落とす。
func assembleDeletePanel(o doctorRenderOpts, title string, blocks [][]string, tail []string, sc *deleteScroll) []string {
	head := []string{" " + doctorColor(o.colored, ansiBold, title), ""}
	room := max(o.page-len(head), 1)
	// 末尾は**後ろから**優先して残す。tail の並びは「捨ててよい順」= 空行 → 補足 → 合計 → 操作の説明
	kept := tail
	for len(kept) > 1 && len(kept) > room {
		kept = kept[1:]
	}
	avail := max(room-len(kept), 0)
	if sc != nil {
		var all []string
		for _, b := range blocks {
			all = append(all, b...)
		}
		out := make([]string, 0, len(head)+avail+len(kept))
		out = append(out, head...)
		out = append(out, sc.window(o, all, avail)...)
		out = append(out, kept...)
		for i, l := range out {
			out[i] = truncateDisp(l, o.width, "…")
		}
		return padTo(out, o.page)
	}
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

// tailLines は末尾 n 行 (実行中は「今どこか」が見たいので先頭ではなく末尾を残す)。
func tailLines(lines []string, n int) []string {
	if n <= 0 || len(lines) <= n {
		return lines
	}
	out := make([]string, 0, n+1)
	out = append(out, fmt.Sprintf("… 先頭 %d 行は省略", len(lines)-n))
	return append(out, lines[len(lines)-n:]...)
}

// doctorPanel は見出し + 本文を全画面に敷く (行数は呼び出し側の lines が padTo で揃える)。
func doctorPanel(o doctorRenderOpts, title string, body []string) []string {
	out := []string{" " + doctorColor(o.colored, ansiBold, title), ""}
	for _, l := range body {
		out = append(out, truncateDisp(" "+l, o.width, "…"))
	}
	return out
}
