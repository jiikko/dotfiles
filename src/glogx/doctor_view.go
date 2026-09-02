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
// 名前・状態・本文行で並べるだけ。今回は 3 セクション: ディスク占有 (doctor/disk) / サービス
// (doctor/svc) / Homebrew (brew doctor の転記)。性質の違う 3 つが同じ器に載ることが、将来の項目を
// 同じ枠で足せるかの検証になる。
//
// スキャンは**開いた時に始める** (起動時には走査しない。起動時は前回の結果を読んでトーストを出すだけ
// = doctor_cache.go)。終わったセクションから順に埋まる (ディスクは数十秒、サービスは数秒)。
// Esc で途中でも閉じられ、完了済みのディスク結果だけを partial としてキャッシュに書く。
//
// ⚠️ 走査は ctx で束ね、閉じる / quit (cancelAll) で必ず cancel する。glogx は popup 運用で開閉が
// 頻繁なので、残留するとディスク I/O を飽和させる (3 章「終了時の後始末」)。
// ⚠️ disk.Scan の OnResult は走査 goroutine から並行に呼ばれるので、channel に載せて Cmd で 1 件ずつ
// Msg にする (Update の外で状態を触らない)。
//
// 段階 ③ (2026-09-02): 表示だけ。削除キーは無い (④ で足す)。
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
	offset      int

	// テスト用の差し替え口 (zero value = 本番)
	diskOpts func() disk.Options
	svcOpts  func() svc.Options
	brewRun  runner.Runner
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

func (v *doctorView) open() tea.Cmd {
	v.shown = true
	v.gen++
	v.offset = 0
	v.diskResults, v.diskRep, v.svcRep, v.brew = nil, nil, nil, nil
	v.startedAt = timeNow()
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

// close は走査を止めて閉じる。ディスクが未完了なら完了済みの分を partial としてキャッシュに書く。
func (v *doctorView) close() {
	v.stop()
	v.shown = false
	if v.diskRep == nil && len(v.diskResults) > 0 {
		rep := disk.Report{Results: append([]disk.Result(nil), v.diskResults...), ScannedAt: timeNow(), Partial: true}
		sort.SliceStable(rep.Results, func(a, b int) bool { return rep.Results[a].Size > rep.Results[b].Size })
		for _, r := range rep.Results {
			if r.Status == disk.StatusOK {
				rep.Total += r.Size
			}
		}
		v.saveCache(rep)
	}
}

// stop は走査だけを止める (cancelAll から呼ぶ後始末。画面の状態は触らない)。
func (v *doctorView) stop() {
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
}

func (v *doctorView) saveCache(rep disk.Report) {
	prev, _ := loadDoctorDiskCache()
	_ = saveDoctorDiskCache(doctorCacheFromReport(rep, prev.LastNotifiedAt)) // 保存失敗は表示に影響しない
}

// receiveDisk は disk のイベントを受ける。完了なら nil、途中なら次を待つ Cmd を返す。
func (v *doctorView) receiveDisk(msg doctorDiskMsg) tea.Cmd {
	if msg.gen != v.gen || !v.shown {
		return nil
	}
	if msg.ev.rep != nil {
		v.diskRep = msg.ev.rep
		if !msg.ev.rep.Partial {
			v.saveCache(*msg.ev.rep)
		}
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
}

func (v *doctorView) receiveBrew(msg doctorBrewMsg) {
	if msg.gen != v.gen || !v.shown {
		return
	}
	res := msg.res
	v.brew = &res
}

// doctorAction は handleKey の結果 (rlDash と同じ語彙)。
type doctorAction int

const (
	doctorSwallow doctorAction = iota // 全画面なので裏の一覧へ素通りさせない
	doctorClosed
	doctorRescan // r: 閉じずに走査し直す (Cmd は browseModel が open() で起こす)
)

func (v *doctorView) handleKey(key string, page int) doctorAction {
	switch key {
	case "D", "q", "esc":
		v.close()
		return doctorClosed
	case "r":
		return doctorRescan
	case "j", "down":
		v.offset++
	case "k", "up":
		v.offset = max(0, v.offset-1)
	case "ctrl+d", "pgdown", " ":
		v.offset += max(1, page/2)
	case "ctrl+u", "pgup":
		v.offset = max(0, v.offset-max(1, page/2))
	case "g":
		v.offset = 0
	}
	return doctorSwallow
}

func (v *doctorView) hint() string {
	return "j/k: スクロール  r: 再スキャン  D/q/esc: 閉じる  (削除はまだできません。表示のみ)"
}

// doctorLabelWidth はディスク行のラベル列の表示幅 (リスク記号の列を揃えるため)。
const doctorLabelWidth = 40

// doctorRenderOpts は描画情報。
type doctorRenderOpts struct {
	width   int
	page    int
	colored bool
	spinner string
	now     time.Time
}

// lines はちょうど page 行を返す (全画面 viewer 共通の契約)。本文が page を超えれば offset で窓を切る。
func (v *doctorView) lines(o doctorRenderOpts) []string {
	body := v.bodyLines(o)
	head := []string{v.headerLine(o), ""}
	room := max(o.page-len(head), 1)
	maxOff := max(0, len(body)-room)
	if v.offset > maxOff {
		v.offset = maxOff
	}
	win := body[min(v.offset, len(body)):]
	if len(win) > room {
		win = win[:room]
	}
	out := make([]string, 0, len(head)+len(win))
	out = append(out, head...)
	out = append(out, win...)
	for i := range out {
		out[i] = truncateDisp(out[i], o.width, "…")
	}
	return padTo(out, o.page)
}

func (v *doctorView) headerLine(o doctorRenderOpts) string {
	left := " doctor"
	if v.scanning() {
		left += "  " + o.spinner + " スキャン中 " + timeNow().Sub(v.startedAt).Round(time.Second).String()
	}
	right := "[r] 再スキャン  [D/Esc] 閉じる "
	gap := o.width - dispWidth(left) - dispWidth(right)
	if gap < 1 {
		return left
	}
	return left + padSpaces(gap) + right
}

// bodyLines はセクション列 (ディスク / サービス / Homebrew)。終わっていないセクションは進捗行だけ。
func (v *doctorView) bodyLines(o doctorRenderOpts) []string {
	var out []string
	out = append(out, v.diskSection(o)...)
	out = append(out, "")
	out = append(out, v.svcSection(o)...)
	out = append(out, "")
	out = append(out, v.brewSection(o)...)
	return out
}

func doctorColor(colored bool, code, s string) string {
	if !colored {
		return s
	}
	return code + s + ansiReset
}

func (v *doctorView) diskSection(o doctorRenderOpts) []string {
	results := v.diskResults
	var total int64
	if v.diskRep != nil {
		results = v.diskRep.Results
		total = v.diskRep.Total
	} else {
		for _, r := range results {
			if r.Status == disk.StatusOK {
				total += r.Size
			}
		}
	}
	title := fmt.Sprintf(" ▸ ディスク占有   合計 %s 解放可能", disk.HumanSize(total))
	if v.diskRep == nil {
		title += fmt.Sprintf("   %s スキャン中 %d/%d", o.spinner, len(results), v.diskTotal)
	} else if v.diskRep.Partial {
		title += "   (中断: 部分結果)"
	}
	out := []string{doctorColor(o.colored, ansiBold, title)}
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
		// ラベル列は表示幅で詰める (全角混在の %-44s は列が揃わない。no-mixed-width-columns の規律)
		label := truncateDisp(r.Entry.Label, doctorLabelWidth, "…")
		out = append(out, fmt.Sprintf("   %8s  %s%s %s", size, label, padSpaces(doctorLabelWidth-dispWidth(label)), doctorColor(o.colored, color, mark)))
		switch r.Status {
		case disk.StatusFailed:
			out = append(out, doctorColor(o.colored, ansiDim, "             "+r.Reason))
		default:
			advice := r.Entry.Recover
			if newest := doctorNewest(r); !newest.IsZero() {
				advice += fmt.Sprintf("。最終更新 %s (%d日前)", newest.Format("2006-01-02"), int(o.now.Sub(newest).Hours()/24))
			}
			out = append(out, doctorColor(o.colored, ansiDim, "             "+advice))
			for _, f := range r.Failures {
				out = append(out, doctorColor(o.colored, ansiDim, "             ❓ 一部走査できず: "+f))
			}
		}
	}
	if v.diskRep != nil && shown == 0 {
		out = append(out, doctorColor(o.colored, ansiDim, "   掃除候補はありません"))
	}
	return out
}

func doctorRiskMark(r disk.Result) (string, string) {
	switch r.Status {
	case disk.StatusBlocked:
		return "⚠️ " + r.Reason, ansiYellow
	case disk.StatusFailed:
		return "❓ 走査できず", ansiDim
	case disk.StatusOK:
	}
	switch r.Entry.Risk {
	case disk.RiskSafe:
		return "✅ 安全", ansiGreen
	case disk.RiskCaution:
		return "⚠️ 注意", ansiYellow
	case disk.RiskConfirm:
		return "⛔ 要確認", ansiRed
	}
	return string(r.Entry.Risk), ""
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

func (v *doctorView) svcSection(o doctorRenderOpts) []string {
	if v.svcRep == nil {
		return []string{doctorColor(o.colored, ansiBold, " ▸ サービス       "+o.spinner+" launchd の登録を確認中")}
	}
	rep := v.svcRep
	title := fmt.Sprintf(" ▸ サービス       壊れた登録 %d 件 (%d 件を走査)", len(rep.Findings), rep.Scanned)
	out := []string{doctorColor(o.colored, ansiBold, title)}
	if rep.Interrupted {
		out = append(out, doctorColor(o.colored, ansiYellow, "   ⚠️ 途中で中断されました"))
	}
	if rep.StatusErr != "" {
		out = append(out, doctorColor(o.colored, ansiYellow, "   ⚠️ 診断できず (launchctl): "+rep.StatusErr+" — 実行ファイルの不在と Homebrew 台帳だけを見ています"))
	}
	if rep.BrewErr != "" {
		out = append(out, doctorColor(o.colored, ansiYellow, "   ⚠️ 診断できず (brew): "+rep.BrewErr))
	}
	for _, e := range rep.DirErrs {
		out = append(out, doctorColor(o.colored, ansiYellow, "   ⚠️ 走査できず: "+e))
	}
	for _, f := range rep.Findings {
		out = append(out, "   "+doctorColor(o.colored, ansiRed, "⛔ "+f.Label))
		for _, r := range f.Reasons {
			out = append(out, doctorColor(o.colored, ansiDim, "      - "+r))
		}
		if f.PenaltyBox {
			out = append(out, doctorColor(o.colored, ansiDim, "      - launchd の penalty box 入り (失敗の繰り返しで起動間隔が延ばされています)"))
		}
		if f.Domain == "system" && !f.HasLastExit {
			out = append(out, doctorColor(o.colored, ansiDim, "      - 起動状態は不明 (system ドメインは一般ユーザーの launchctl list に出ない)"))
		}
		out = append(out, doctorColor(o.colored, ansiDim, "      手動で実行してください (このツールは実行しません):"))
		for _, c := range f.Commands {
			out = append(out, "        "+c)
		}
	}
	for _, u := range rep.Undiagnosed {
		out = append(out, doctorColor(o.colored, ansiDim, "   ❔ 診断できず: "+u.PlistPath+" ("+u.Reason+")"))
	}
	if len(rep.Findings) == 0 && len(rep.Undiagnosed) == 0 && rep.StatusErr == "" {
		out = append(out, doctorColor(o.colored, ansiDim, "   壊れた登録は見つかりませんでした"))
	}
	return out
}

func (v *doctorView) brewSection(o doctorRenderOpts) []string {
	if v.brew == nil {
		return []string{doctorColor(o.colored, ansiBold, " ▸ Homebrew       "+o.spinner+" brew doctor を実行中")}
	}
	b := v.brew
	switch {
	case b.Unavailable != "":
		return []string{doctorColor(o.colored, ansiBold, " ▸ Homebrew       診断できず"), doctorColor(o.colored, ansiYellow, "   ⚠️ "+b.Unavailable)}
	case b.Clean:
		return []string{doctorColor(o.colored, ansiBold, " ▸ Homebrew       brew doctor: 警告なし"), doctorColor(o.colored, ansiDim, "   Your system is ready to brew.")}
	}
	out := []string{doctorColor(o.colored, ansiBold, fmt.Sprintf(" ▸ Homebrew       brew doctor: 警告 %d 件 (出力の転記。修復コマンドは提示のみ)", len(b.Warnings)))}
	for _, w := range b.Warnings {
		for i, line := range strings.Split(w, "\n") {
			if i == 0 {
				out = append(out, "   "+doctorColor(o.colored, ansiYellow, line))
			} else {
				out = append(out, doctorColor(o.colored, ansiDim, "   "+line)) // brew の字下げをそのまま写す
			}
		}
	}
	return out
}
