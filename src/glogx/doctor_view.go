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

	// テスト用の差し替え口 (zero value = 本番)
	diskOpts func() disk.Options
	svcOpts  func() svc.Options
	brewRun  runner.Runner
}

// doctorRow は 1 行。selectable な行だけにカーソルが止まる。detail は Enter で展開する本文。
type doctorRow struct {
	text       string
	selectable bool
	key        string
	detail     []string
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

// rescan は snapshot を無視して走査し直す (r)。
func (v *doctorView) rescan() tea.Cmd { return v.start(true) }

func (v *doctorView) start(force bool) tea.Cmd {
	v.shown = true
	v.gen++
	v.cursor, v.offset = 0, 0
	v.expanded = map[string]bool{}
	v.diskResults, v.diskRep, v.svcRep, v.brew = nil, nil, nil, nil
	v.startedAt = timeNow()
	v.snapshotAt = time.Time{}
	if !force {
		if sn, ok := loadDoctorSnapshot(timeNow()); ok {
			rep := sn.Disk
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
	catalogN := len(dOpt.Catalog)
	if catalogN == 0 {
		catalogN = disk.CatalogSize()
	}
	v.diskTotal = catalogN
	// 容量 = OnResult の回数 (カタログ数) + 完了 1 件。閉じた後に誰も読まなくても走査 goroutine は詰まらない
	ch := make(chan doctorDiskEvent, catalogN+1)
	v.diskCh = ch
	dOpt.OnResult = func(r disk.Result) { ch <- doctorDiskEvent{r: &r} }
	go func() {
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
	return tea.Batch(
		v.waitDiskCmd(gen),
		func() tea.Msg { return doctorSvcMsg{gen: gen, rep: svc.Scan(ctx, sOpt)} },
		func() tea.Msg { return doctorBrewMsg{gen: gen, res: runBrewDoctor(ctx, bRun)} },
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
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
}

// saveCache は結果をキャッシュへ。🚨 partial で**完全な結果を潰さない**: Esc や r の直後の数件だけの
// partial で 45GB の結果を 200MB に置き換えると、起動トーストが閾値未満で無期限に沈黙する
// (敵対レビュー 2026-09-02 P2)。partial は「完全な結果が無いとき」か「完全な結果より合計が大きいとき」だけ書く。
func (v *doctorView) saveCache(rep disk.Report) {
	prev, hadPrev := loadDoctorDiskCache()
	if rep.Partial && hadPrev && !prev.Partial && prev.Total >= rep.Total {
		return
	}
	_ = saveDoctorDiskCache(doctorCacheFromReport(rep, prev.LastNotifiedAt)) // 保存失敗は表示に影響しない
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
	doctorRescan // r: 走査し直す (browseModel が open() で起こす。close は経由しない = partial を書かない)
)

func (v *doctorView) handleKey(key string, page int) doctorAction {
	switch key {
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
	case "ctrl+d", "pgdown", " ":
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
		}
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

func (v *doctorView) hint() string {
	return "j/k: 移動  Enter: 詳細を開く/閉じる  r: 再スキャン  D/q/esc: 閉じる  (削除はまだできません。表示のみ)"
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
		label := truncateDisp(r.Entry.Label, doctorLabelWidth, "…")
		row := doctorRow{
			text:       fmt.Sprintf(" %8s  %s%s %s", size, label, padSpaces(doctorLabelWidth-dispWidth(label)), doctorColor(o.colored, color, mark)),
			selectable: true,
			key:        "disk:" + r.Entry.ID,
			detail:     v.diskDetail(o, r),
		}
		rows = append(rows, row)
		if r.Status == disk.StatusFailed {
			rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiDim, "           "+r.Reason)})
			continue
		}
		advice := r.Entry.Recover
		if newest := doctorNewest(r); !newest.IsZero() {
			advice += fmt.Sprintf("。最終更新 %s (%d日前)", newest.Format("2006-01-02"), int(o.now.Sub(newest).Hours()/24))
		}
		rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiDim, "           "+advice)})
		for _, f := range r.Failures {
			rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiYellow, "           ❓ 一部走査できず (合計に含めていません): "+f)})
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
	out = append(out, doctorColor(o.colored, ansiDim, "             削除経路: "+r.Entry.DeleteVia+" (このツールはまだ削除しません)"))
	if r.Entry.Detail != "" {
		out = append(out, doctorColor(o.colored, ansiDim, "             "+r.Entry.Detail))
	}
	if r.Entry.Inspect || len(r.Contents) > 0 {
		for _, c := range r.Contents {
			out = append(out, "               - "+c)
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
		return "🚨 " + r.Reason, ansiYellow
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
const doctorLabelWidth = 40

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
		rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiRed, " ⛔ "+f.Label), selectable: true, key: "svc:" + f.PlistPath, detail: detail})
		for _, r := range f.Reasons {
			rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiDim, "      - "+r)})
		}
		if f.PenaltyBox {
			rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiDim, "      - launchd の penalty box 入り (失敗の繰り返しで起動間隔が延ばされています)")})
		}
		if f.Domain == "system" && !f.HasLastExit {
			rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiDim, "      - 起動状態は不明 (system ドメインは一般ユーザーの launchctl list に出ない)")})
		}
	}
	for _, u := range rep.Undiagnosed {
		rows = append(rows, doctorRow{text: doctorColor(o.colored, ansiYellow, " ❔ 診断できず: "+u.PlistPath+" ("+u.Reason+")")})
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
		count := fmt.Sprintf("(%d 行)", len(lines))
		inner := o.width - 2
		sumW := dispWidth(summary)
		gap := inner - 3 - sumW - dispWidth(count) - 1
		text := "   " + doctorColor(o.colored, ansiYellow, summary)
		if gap >= 1 {
			text += padSpaces(gap) + doctorColor(o.colored, ansiDim, count)
		}
		rows = append(rows, doctorRow{text: text, selectable: true, key: fmt.Sprintf("brew:%d:%s", i, summary), detail: detail})
	}
	return rows
}
