package doctorview

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
// (Update の外で状態を触らない)。ディスク走査 (DoctorDiskEvent) と同じ形。

import (
	"context"
	"fmt"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
	"doctor/runner"
	"glogx/internal/app"
	"glogx/internal/term"
)

// DoctorDelete は削除の状態機械。confirm → running → result の 3 相。
type DoctorDelete struct {
	// preparing は d を押した直後の下見 (DryRun) 中。engine が削除の直前と同じ走査を通すので
	// 重いエントリでは数秒かかる = 無言で固まらないよう進捗を出す
	Preparing bool
	Plan      *disk.DeleteReport // 下見の結果 (確認プロンプトの材料)
	Confirm   bool               // y/N の確認中
	Running   bool               // 削除の実行中 (キーを飲む)
	Result    *disk.DeleteReport
	Kind      doctorJob         // 何の仕事か (既定 = jobDisk)
	CmdPlan   []doctorCmdAction // JobCmd: 実行するコマンド (確認画面の材料)
	CmdRep    *cmdRunReport     // JobCmd: 実行の結果
	Err       string
	Progress  DoctorProgress // 「2/3 Xcode DerivedData」+「走査中 12s」

	// log は実行したコマンドとその出力 (実行中は垂れ流し、終わった後も残す)。
	// 失敗したときに LLM へ投げられるよう、y でまるごとコピーできる
	Log []string

	Cancel context.CancelFunc
	// ch は進捗とコマンドの出力 (落としてよい)。done は**完了**専用の 1 バッファ。
	// 🚨 完了を取りこぼすと UI が「実行中」のまま永久に止まる。進捗で埋まった ch に
	// 完了を載せると詰まり、ctx で諦めると今度は届かない (2026-09-03 に実測: テストが 10 秒で
	// タイムアウトした)。**落としてよいもの**と**落としてはいけないもの**を別の口にする
	Ch chan DoctorDeleteEvent
	// progCh は**相だけ**の 1 バッファ。溢れたら古い相を捨てて新しい相を入れる。
	// 🚨 ch と同じ「落としてよい」口に相を載せると、速いエントリが 16 件溜まった直後の
	// 重いエントリの相が捨てられ、**古いエントリ番号に伸び続ける経過秒が付く** =
	// 「16 番目の走査が 97 秒続いている」という具体的な嘘になる (敵対レビュー 2026-09-04 が実測)。
	// 落としてよいのは「古い相」であって「最新の相」ではない
	progCh  chan DoctorDeleteEvent
	Done    chan DoctorDeleteEvent
	ArmedCC bool // 実行中に Ctrl-C が 1 回押された (2 回目で cancel)

	// confirmScroll は確認パネルの縦位置。**破壊的操作の確認で「対象が見えない」を作らない**ため
	// (issue 241)。以前は入り切らない塊を落として「端末を広げてください」と言うだけで、
	// 送ろうとして押したキーは下の default に落ちて確認ごと無言で閉じていた
	ConfirmScroll deleteScroll
}

// deleteScroll は確認パネルの縦位置。total / view は**最後に描いたとき**の値で、
// キー操作 (画面を持たない) 側が送り幅と上限を決めるために要る。
// 🚨 描画は状態を持たない方が望ましいが、この 2 つは「今の画面で何行見えているか」なので
// 描画側にしか無い。同 package の rowCursor も同じ形 (restore が index を描画時に寄せる)。
type deleteScroll struct {
	Offset int // 本文の先頭から何行送ったか
	Total  int // 本文の全行数
	View   int // 実際に見えている行数
}

// window は本文を今の位置で切り出し、入り切らないぶんを注記で伝える。
// 入り切るときは注記を出さない (常に出すと、送る必要が無い画面でも送れると誤解させる)。
func (s *deleteScroll) window(o DoctorRenderOpts, all []string, avail int) []string {
	s.Total = len(all)
	if avail <= 0 {
		// 本文に使える行が無い (極端に低い画面)。注記も出さない:
		// 1 行でも返すと末尾 (合計と操作の説明) が押し出される。
		// 🚨 offset は**捨てない**。捨てると、端末が一瞬縮んだだけで送った位置が失われる
		// (敵対レビュー 2026-09-04 の P3-2 が実測: page20 で G → page3 を 1 フレーム → page20 に
		// 戻しても末尾が見えない)
		s.View = 0
		return nil
	}
	if len(all) <= avail {
		s.Offset, s.View = 0, len(all)
		return all
	}
	view := max(avail-1, 0) // 位置の注記 1 行を先に確保する
	s.View = view
	s.Offset = min(max(s.Offset, 0), len(all)-view)
	above, below := s.Offset, len(all)-s.Offset-view
	var parts []string
	if above > 0 {
		parts = append(parts, fmt.Sprintf("上に %d 行", above))
	}
	if below > 0 {
		parts = append(parts, fmt.Sprintf("下に %d 行", below))
	}
	out := make([]string, 0, view+1)
	out = append(out, all[s.Offset:s.Offset+view]...)
	return append(out, doctorColor(o.Colored, term.AnsiDim,
		fmt.Sprintf(" … %s (j/k で送る)", strings.Join(parts, " / "))))
}

// scroll は最後に描いた画面の高さを基準に位置を動かす。
func (s *deleteScroll) scroll(key string) {
	maxOff := max(s.Total-s.View, 0)
	half := max(s.View/2, 1)
	switch key {
	case "j", "down":
		s.Offset++
	case "k", "up":
		s.Offset--
	case "ctrl+d", "pgdown":
		s.Offset += half
	case "ctrl+u", "pgup":
		s.Offset -= half
	case "g", "home":
		s.Offset = 0
	case "G", "end":
		s.Offset = maxOff
	}
	s.Offset = min(max(s.Offset, 0), maxOff)
}

// deleteScrollKeys は確認パネルで**中止に落とさない**キー (送るだけ)。
// 🚨 ここに無いキーは既定どおり中止側へ落ちる (安全側)。送る手段が無いまま
// 「送ろうとした打鍵で確認が消える」のを塞ぐのが目的で、既定を緩めるものではない。
// 🚨 他の画面と違って移動の語彙を tuikit の listnav.MotionOf に寄せていないのは意図的:
// 寄せると Space / f / b / ctrl+n 等が「中止に落ちないキー」へ黙って増え、破壊的操作の
// 確認で「中止のつもりの打鍵」が送りに化ける。送るキーを足すなら、ここに 1 つずつ足す
var deleteScrollKeys = map[string]bool{
	"j": true, "down": true, "k": true, "up": true,
	"ctrl+d": true, "pgdown": true, "ctrl+u": true, "pgup": true,
	"g": true, "home": true, "G": true, "end": true,
}

// active は削除の語彙がキーを持っているか (裏の一覧にも C/X にも渡さない)。
func (d *DoctorDelete) Active() bool {
	return d.Preparing || d.Confirm || d.Running || d.Result != nil || d.CmdRep != nil || d.Err != ""
}

// blocking は実行中で、抜ける以外のキーを飲む状態か。
func (d *DoctorDelete) Blocking() bool { return d.Preparing || d.Running }

func (d *DoctorDelete) reset() {
	if d.Cancel != nil {
		d.Cancel()
	}
	*d = DoctorDelete{}
}

// DoctorDeleteEvent は削除の進捗 / 完了 (channel 経由で Update へ運ぶ)。
// doctorJob は確認 → 実行 → 結果のパネルが今どの仕事を運んでいるか。
//
// 🚨 **削除エンジンの経路は触らない。** 分岐するのは「何を確認するか」「何を走らせるか」
// 「結果をどう描くか」の 3 箇所だけで、相の機械 (Ctrl-C 2 回のガード / コマンドの垂れ流し /
// 進捗 / スクロール) は共有する。2 つ目の状態機械を作らない
// (issue 071 が「全画面 viewer 2 枚が状態機械を独立に 2 コピー持つ」を同型の問題として挙げている)。
type doctorJob int

const (
	jobDisk doctorJob = iota // ディスクの削除 (既存)
	JobCmd                   // 提示したコマンドを実行する (brew の手 / docker の回収)
)

// doctorCmdAction は「画面が提示し、x で実行できる 1 コマンド」。
//
// 🚨 **brew 専用ではない** (Homebrew の手と Docker の回収コマンドが共有する)。
// 確認 → 実行 → 結果の相の機械もこの型の列だけを見ており、提示元を知らない。
// 2 つ目の状態機械を作らないための共有点なので、提示元に固有の情報をここへ足さないこと。
type doctorCmdAction struct {
	Label string // 日本語のラベル (何をする手か)
	Cmd   string // 実行するコマンド
	Note  string // 打つ前に知っておくこと ("" = 無し)
}

// cmdRunReport は提示したコマンドを実行した結果。1 コマンド 1 レコード。
type cmdRunReport struct {
	Records []disk.CommandRecord
}

// failed は 0 でない終了コード / 起動できなかったものの数。
func (r cmdRunReport) failed() int {
	n := 0
	for _, rec := range r.Records {
		if rec.RC != 0 || rec.Err != "" {
			n++
		}
	}
	return n
}

// DoctorProgress は「今どこまで進んだか」。
//
// 🚨 **文字列 1 本で持たない**。2 行に割れず (エントリ名と相を別行にできない)、経過秒を
// 後から足せない。相が変わった時刻 (since) は receiveDelete が入れる = 時計を読むのは
// Update の中だけ、という既存の規律を守る (engine の goroutine で時計を読まない)。
type DoctorProgress struct {
	I, Total int              // 何番目 / 全体 (表示は 1 始まり)
	Label    string           // エントリ名
	Phase    disk.DeletePhase // 走査中 / 削除中 / 確認中
	Since    time.Time        // この相に入った時刻 (経過の基準)
	Known    bool             // 一度でも相が届いたか (false なら「準備中」)
	// verb は相の語の上書き ("" = phase から引く)。brew の実行は disk の相語彙 (走査中 /
	// 削除中 / 確認中) に当てはまらないので、"実行中" を直接渡す
	verb string
}

// sameStep は「同じ相の続き」か。経過秒の基準を据え置くかの判定に使う
// (毎イベントで since を更新すると経過が常に 0 に戻り、止まって見える元に戻る)。
func (p DoctorProgress) sameStep(q DoctorProgress) bool {
	return p.Known && q.Known && p.I == q.I && p.Total == q.Total && p.Label == q.Label && p.Phase == q.Phase
}

type DoctorDeleteEvent struct {
	Prog   *DoctorProgress     // 相が変わった (落としてよい)
	cmdRep *cmdRunReport       // 提示したコマンドの実行が終わった
	Cmd    *disk.CommandRecord // cli: の 1 コマンドが終わった
	Rep    *disk.DeleteReport
	err    string
	dryRun bool
}

type DoctorDeleteMsg struct {
	Gen int
	Ev  DoctorDeleteEvent
}

// startDelete は下見 (dryRun) か本番の削除を走らせる。どちらも同じ経路で、
// 違うのは DryRun フラグだけ (確認プロンプトに出す内容を UI 側で組み直さないため)。
func (v *DoctorView) StartDelete(targets []disk.Result, dryRun bool) tea.Cmd {
	if v.Del.Cancel != nil {
		v.Del.Cancel() // 前の相 (下見) の ctx を残さない
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan DoctorDeleteEvent, 16)
	prog := make(chan DoctorDeleteEvent, 1)
	done := make(chan DoctorDeleteEvent, 1)
	// 相は 1 つだけ立てる。🚨 confirm を落とし忘れると confirm && running の非正規状態になり、
	// 今は switch の並び順だけで無害になっている (並べ替えた瞬間に黙って壊れる)。
	// armedCC も引き継がない: 下見の最中に押した Ctrl-C が、本番の 1 回目で即中断に化ける
	// progress はゼロ値 (known:false) から始める = 最初の相が届くまで「準備中」を出す
	v.Del = DoctorDelete{Cancel: cancel, Ch: ch, progCh: prog, Done: done,
		Preparing: dryRun, Running: !dryRun}
	gen := v.Gen
	opt := v.deleteOptions()
	opt.DryRun = dryRun
	opt.OnPhase = func(i, total int, label string, p disk.DeletePhase) {
		// 🚨 ノンブロッキング。読み手 (receiveDelete の再アーム) が止まると、engine が
		// **削除の途中で** channel 待ちに入る。進捗は落としてよい情報なので捨てる方へ倒す
		sendLatestProgress(prog, DoctorDeleteEvent{Prog: &DoctorProgress{
			I: i + 1, Total: total, Label: label, Phase: p, Known: true}})
	}
	opt.OnCommand = func(rec disk.CommandRecord) {
		select {
		case ch <- DoctorDeleteEvent{Cmd: &rec}:
		default: // 読み手が詰まっても engine を止めない (進捗と同じ扱い)
		}
	}
	run := v.deps.Delete
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
		ev := DoctorDeleteEvent{Rep: &rep, dryRun: dryRun}
		if err != nil {
			ev.err = err.Error()
		}
		done <- ev // cap 1 の専用口。進捗で詰まらないし、取りこぼしもしない
		return nil
	})
}

// beginBrewRun は選んだ brew の手を確認画面へ出す (x)。
//
// 🚨 下見 (preparing) を挟まない: 削除と違い、走らせる前に測り直すものが無い。
// 代わりに**確認画面が実行するコマンドそのものを全部出す**ので、同意の対象と実行の対象が一致する。
func (v *DoctorView) beginBrewRun() doctorAction {
	acts := v.selectedBrewActions()
	if len(acts) == 0 {
		v.PendingToast = "Space で実行する手を選んでください"
		return DoctorToast
	}
	v.Del = DoctorDelete{Kind: JobCmd, Confirm: true, CmdPlan: acts}
	return DoctorSwallow
}

// beginDockerRun は選んだ Docker の回収コマンドを確認画面へ出す (x)。
// 相の機械は brew と共有する (JobCmd)。2 つ目の状態機械を作らない。
func (v *DoctorView) beginDockerRun() doctorAction {
	acts := v.selectedDockerActions()
	if len(acts) == 0 {
		v.PendingToast = "Space で実行するコマンドを選んでください"
		return DoctorToast
	}
	v.Del = DoctorDelete{Kind: JobCmd, Confirm: true, CmdPlan: acts}
	return DoctorSwallow
}

// selectedBrewActions は選ばれた手を**画面に出ている順**で返す。
// 🚨 選択は map (コマンド文字列) なので、そのまま回すと順序が実行ごとに変わる。
// 確認画面に出した順と実行の順が違うと、途中で中断したときに「どこまで走ったか」が読めない。
func (v *DoctorView) selectedBrewActions() []doctorCmdAction {
	if len(v.SelectedActions) == 0 {
		return nil
	}
	out := make([]doctorCmdAction, 0, len(v.SelectedActions))
	seen := map[CmdKey]bool{}
	for _, r := range v.Rows {
		cmd := CmdKey(r.CopyPath)
		if !r.Key.isBrewAction() || !v.SelectedActions[cmd] || seen[cmd] {
			continue
		}
		seen[cmd] = true
		act, ok := v.actionByCmd[cmd]
		if !ok {
			act = doctorCmdAction{Label: "(不明な手)", Cmd: r.CopyPath}
		}
		out = append(out, act)
	}
	return out
}

// startCmdRun は手を 1 つずつ実行し、コマンドと出力を垂れ流す。
func (v *DoctorView) startCmdRun(acts []doctorCmdAction) tea.Cmd {
	if v.Del.Cancel != nil {
		v.Del.Cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan DoctorDeleteEvent, 16)
	prog := make(chan DoctorDeleteEvent, 1)
	done := make(chan DoctorDeleteEvent, 1)
	v.Del = DoctorDelete{Kind: JobCmd, Cancel: cancel, Ch: ch, progCh: prog, Done: done, Running: true, CmdPlan: acts}
	gen := v.Gen
	run := v.deps.Brew
	if run == nil {
		run = runner.Exec
	}
	return tea.Batch(v.waitDeleteCmd(gen), func() tea.Msg {
		var recs []disk.CommandRecord
		// 🚨 削除と同じ latch に載せる (issue 211)。brew install は数分かかるので、
		// 終了・再起動で走行中のプロセスが watchdog ごと消えないようにする
		doctorTrack(func() {
			for i, a := range acts {
				if ctx.Err() != nil {
					break // 中断: ここまでの結果を返す (何が走ったかは残す)
				}
				sendLatestProgress(prog, DoctorDeleteEvent{Prog: &DoctorProgress{
					I: i + 1, Total: len(acts), Label: a.Label, verb: "実行中", Known: true}})
				fields := strings.Fields(a.Cmd)
				if len(fields) == 0 {
					continue
				}
				stdout, stderr, rc, err := run(ctx, fields[0], fields[1:]...)
				rec := disk.CommandRecord{Name: fields[0], Args: fields[1:], RC: rc, Stdout: stdout, Stderr: stderr}
				if err != nil {
					rec.Err = err.Error()
				}
				recs = append(recs, rec)
				select {
				case ch <- DoctorDeleteEvent{Cmd: &rec}:
				default:
				}
			}
		})
		done <- DoctorDeleteEvent{cmdRep: &cmdRunReport{Records: recs}}
		return nil
	})
}

// sendLatestProgress は相を 1 バッファへ「最新優先」で入れる (古い相は捨てる)。
// 単一の生産者 (engine の goroutine) から呼ばれる前提。
func sendLatestProgress(prog chan DoctorDeleteEvent, ev DoctorDeleteEvent) {
	select {
	case prog <- ev:
		return
	default:
	}
	select {
	case <-prog: // 古い相を捨てる
	default:
	}
	select {
	case prog <- ev:
	default:
	}
}

// CommandLogLines は 1 コマンドの記録を、そのまま貼れる形の行にする。
// **stdout と stderr を混ぜない** (どちらに出たかが判定材料なので、印で分ける)。
func CommandLogLines(rec disk.CommandRecord) []string {
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

func DoctorPhaseWord(p disk.DeletePhase) string {
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

// DeleteProgressLines は進捗を 2 行で描く (1 行目: 何番目のエントリか / 2 行目: 相 + 経過)。
//
// 🚨 **スピナーと経過秒を 2 行目に置くのは幅のため。** 1 行に詰めると、エントリ名が長い +
// 端末が狭いときに term.TruncateDisp が**末尾から**削るので、「動いている」ことを示す手がかり
// (スピナー・経過) が最初に消える。固まって見える問題を直すための表示が、狭い端末でだけ
// 消えるのでは意味がない (ユーザー選定 2026-09-04: 候補 4 案からこの形)。
//
// 経過は 1 秒未満のあいだ出さない。押した直後に「0s」が見えると、止まっているのか
// 始まっていないのか区別が付かない (スピナーが先に生存を示す)。
func DeleteProgressLines(o DoctorRenderOpts, p DoctorProgress) []string {
	if !p.Known {
		return []string{o.Spinner + " 準備中"}
	}
	word := p.verb
	if word == "" {
		word = DoctorPhaseWord(p.Phase)
	}
	sub := "  " + o.Spinner + " " + word
	if !p.Since.IsZero() {
		if el := o.Now.Sub(p.Since); el >= time.Second {
			sub += "  " + strconv.Itoa(int(el.Seconds())) + "s"
		}
	}
	head := strconv.Itoa(p.I) + "/" + strconv.Itoa(p.Total)
	if p.Label != "" {
		head += "  " + p.Label // ラベルが空のときに末尾の空白を残さない
	}
	return []string{head, sub}
}

// waitDeleteCmd は channel から 1 件だけ受けて Msg にする (Update の外で状態を触らない)。
func (v *DoctorView) waitDeleteCmd(gen int) tea.Cmd {
	ch, prog, done := v.Del.Ch, v.Del.progCh, v.Del.Done
	if ch == nil || prog == nil || done == nil {
		return nil
	}
	return func() tea.Msg {
		// 溜まっている進捗を先に出す (両方 ready のときに select が完了を選ぶと、
		// 直前のコマンドの出力が画面に出ないまま結果へ飛ぶ)
		if ev, ok := pendingDeleteEvent(ch, prog); ok {
			return DoctorDeleteMsg{Gen: gen, Ev: ev}
		}
		return DoctorDeleteMsg{Gen: gen, Ev: awaitDeleteEvent(ch, prog, done)}
	}
}

// awaitDeleteEvent は次の 1 件を待つ。完了は、溜まっている出力と相を出し切ってから返す。
//
// 🚨 呼び出し側で覗いてからこの select までの間に進捗と完了の両方が届くと、select は
// 無作為に完了を選ぶ。完了を返すと待ち受けを張り直さないので、残った進捗とコマンドの出力
// (y でコピーする記録) を誰も読まない (1000 回に 8 回の実測)。残っていれば先に返し、完了は
// done へ戻して次の待ち受けで受ける。done は cap 1 で、書き手は仕事 1 本につき 1 回しか
// 書かない (今空けたので詰まらない)
func awaitDeleteEvent(ch, prog, done chan DoctorDeleteEvent) DoctorDeleteEvent {
	select {
	case ev := <-ch:
		return ev
	case ev := <-prog:
		return ev
	case ev := <-done:
		if pending, ok := pendingDeleteEvent(ch, prog); ok {
			done <- ev
			return pending
		}
		return ev
	}
}

// pendingDeleteEvent は溜まっている出力 (ch) と相 (prog) を、この順に 1 件だけ待たずに取り出す。
func pendingDeleteEvent(ch, prog chan DoctorDeleteEvent) (DoctorDeleteEvent, bool) {
	select {
	case ev := <-ch:
		return ev, true
	default:
	}
	select {
	case ev := <-prog:
		return ev, true
	default:
	}
	return DoctorDeleteEvent{}, false
}

// receiveDelete は進捗 / 完了を取り込む。次の 1 件を待つ Cmd を返す。
func (v *DoctorView) ReceiveDelete(msg DoctorDeleteMsg) tea.Cmd {
	if !v.Shown || msg.Gen != v.Gen || !v.Del.Active() {
		return nil // 閉じた後に届いた古い Msg
	}
	// 🚨 **ここも live 経路の関門** (issue 228)。削除の記録に乗るのは対象パス (ファイル名由来) と
	// OS のエラー文、そして `cli:` で起こしたコマンドの stdout / stderr = どれも自分以外が
	// 書いた文字列。パネルに描くだけでなく `y` でまるごとコピーできるので、無害化せずに
	// 持つと**貼った先の端末**で発火する。CLI と同じ関数を通す。
	ev := msg.Ev
	if ev.Cmd != nil {
		rec := disk.SanitizeCommandRecordForDisplay(*ev.Cmd)
		v.Del.Log = append(v.Del.Log, CommandLogLines(rec)...)
		return v.waitDeleteCmd(msg.Gen)
	}
	if ev.Prog != nil {
		next := *ev.Prog
		next.Label = term.SanitizePlainLine(next.Label) // エントリ名は Result 由来 = 外部の文字列
		// 同じ相の続きなら基準を据え置く (経過が 0 に戻らないように)
		if next.sameStep(v.Del.Progress) {
			next.Since = v.Del.Progress.Since
		} else {
			next.Since = app.TimeNow()
		}
		v.Del.Progress = next
		return v.waitDeleteCmd(msg.Gen)
	}
	if ev.cmdRep != nil {
		v.Del.Preparing, v.Del.Running = false, false
		v.Del.CmdRep = ev.cmdRep
		return nil
	}
	if ev.Rep == nil {
		return v.waitDeleteCmd(msg.Gen) // 何も載っていない event は捨てて待ち直す
	}
	v.Del.Preparing, v.Del.Running = false, false
	if ev.err != "" {
		v.Del.Err = term.SanitizePlainLine(ev.err)
		return nil
	}
	rep := disk.SanitizeDeleteReportForDisplay(*ev.Rep)
	if ev.dryRun {
		// 🚨 plan は表示だけでなく**実行対象の絞り込み**にも使われる (plannedTargets → Entry.ID。
		// issue 245)。無害化が触るのは表示に出る文字列 (Label / Reason / パス) だけで **ID は
		// 触らない**ので、絞り込みも実際に消す対象 (selectedResults 側の実パス) も変わらない
		v.Del.Plan, v.Del.Confirm = &rep, true
		return nil
	}
	v.Del.Result = &rep
	return nil
}

// InterruptDelete は削除の下見・実行中の Ctrl-C (browseModel が Ctrl-C を先に捌かずにここへ渡す)。
// 1 回目は武装、2 回目で cancel する (HandleDeleteKey の意味づけをそのまま使う)。
func (v *DoctorView) InterruptDelete() { v.HandleDeleteKey("ctrl+c") }

// handleDeleteKey は削除の語彙。飲んだら true。
func (v *DoctorView) HandleDeleteKey(key string) (doctorAction, bool) {
	d := &v.Del
	switch {
	case d.Blocking():
		// 実行中は抜ける手段だけ残す。1 回目の Ctrl-C はブロックし、2 回目で cancel する
		// (途中終了は「消したのに記録に残らない」を作りうるので、誤爆を 1 段止める)
		if key == "ctrl+c" {
			if !d.ArmedCC {
				d.ArmedCC = true
				return DoctorSwallow, true
			}
			if d.Cancel != nil {
				d.Cancel()
			}
		}
		return DoctorSwallow, true
	case d.Result != nil || d.CmdRep != nil || d.Err != "":
		// y / Y は出力をコピー (失敗したときに LLM へそのまま投げられる形)。閉じない
		if key == "y" || key == "Y" {
			v.PendingCopy = v.DeleteLogText() // 見出しを必ず書くので空にならない
			return DoctorCopyLog, true
		}
		// それ以外はどのキーでも閉じる。閉じたら再スキャンして表示を実体に合わせる
		d.reset()
		return DoctorRescan, true
	case d.Confirm:
		// 🚨 送るキーは**中止に落とさない** (issue 241)。窓に入り切らない対象を見ようとした
		// 打鍵で確認が消えるのを塞ぐ。ここに無いキーは既定どおり中止側 (安全側) へ落ちる。
		// 🚨 **「消せるものがありません」の画面でも送れること**: あちらもパネルに
		// 「(j/k で送る)」と出るので、送れないと「案内どおり押したら無言で消えた」形になる
		// (敵対レビュー 2026-09-04 の P1。塞いだはずの形を自分で作っていた)。
		// 消せない理由 (skipped の Reason) こそ読みたいので、読む手段を先に確保する
		if deleteScrollKeys[key] {
			d.ConfirmScroll.scroll(key)
			return DoctorSwallow, true
		}
		// 🚨 Enter は飲む (実行もキャンセルもしない。issue 243、ユーザー判断 2026-09-05)。
		// glogx の他の破壊確認 (status の discard / push / pull) は `y/Enter: 実行` なので手癖で
		// 押されるが、ディスクから実体を消す既定を実行側へ倒さない。かといって既定の中止側へ
		// 落とすと「押したら無言で閉じた」になる。どちらにも倒さず、案内に非対称を書く。
		// 「消せるものがありません」の画面は「何かキーで戻る」と案内しているので除く。
		if key == "enter" && (d.Kind == JobCmd || planHasWork(d.Plan)) {
			return DoctorSwallow, true
		}
		if d.Kind == JobCmd {
			if key == "y" || key == "Y" {
				v.PendingDeleteCmd = v.startCmdRun(d.CmdPlan)
				return DoctorRunDelete, true
			}
			d.reset() // それ以外はやめる (安全側)
			return DoctorSwallow, true
		}
		if !planHasWork(d.Plan) {
			d.reset() // 消せるものが無いので、送るキー以外はどれでも戻る
			return DoctorSwallow, true
		}
		switch key {
		case "y", "Y":
			// 🚨 **実行対象は「確認画面に出した plan」から作る** (issue 245)。
			// UI の選択 (selectedResults) をそのまま渡すと、**確認画面が約束した量と
			// 実際に消える対象がずれる**: 下見が部分的に中断されると 2 件目以降が
			// OutcomeFailed になり、confirmLines はそれを解放量に足さない (正しい) のに、
			// selectedResults は元の走査結果しか見ないので y で消えてしまう。
			// 「見せた量より多く消える」= 破壊的操作の同意を実際より小さい数字で取る形。
			// plan の Planned だけに絞れば、確認画面と実行が同じソースを見る。
			targets := plannedTargets(d.Plan, v.SelectedResults())
			if len(targets) == 0 {
				d.reset()
				v.PendingToast = "削除する対象がありません"
				return DoctorToast, true
			}
			// 入口 (beginDelete) だけで検査すると非対称になる。押す瞬間にもう一度見る
			for _, r := range targets {
				if ok, why := v.Deletable(r); !ok {
					d.reset()
					v.PendingToast = r.Entry.Label + ": " + why
					return DoctorToast, true
				}
			}
			v.PendingDeleteCmd = v.StartDelete(targets, false)
			return DoctorRunDelete, true
		default: // n / esc / その他はすべて中止 (既定は中止側)
			d.reset()
			return DoctorSwallow, true
		}
	}
	return DoctorSwallow, false
}

// deleteAbortKeys は削除の実行中 / 下見中に中断へ使えるキー。案内の文言はここから作る。
//
// 🚨 **キーの分岐の正本は tui.go:1321** で、ここはその写し。両者が一致していることは
// TestDeleteAbortGuidanceMatchesKeys が突き合わせるが、**あのテストの候補キーもハードコード**
// なので、tui.go とここの両方に載っていない 3 つ目のキーを tui.go だけに足しても誰も落ちない
// (敵対レビュー 2026-09-04 が ctrl+q を足して実測)。tui.go の分岐を触るときは、ここと
// あのテストの候補表も一緒に見ること。字句で結ぶ検査は書いていない。
var deleteAbortKeys = []string{"ctrl+c", "ctrl+g"}

// deleteAbortKeysWord は案内に出す表記。"ctrl+c" -> "Ctrl-C"。
var deleteAbortKeysWord = abortKeysWord(deleteAbortKeys)

// abortKeysWord は表記の組み立て。init で評価されるので**壊れた入力でも panic しない**
// (panic すると削除パネルを開くまでもなく glogx が起動しない)。
func abortKeysWord(keys []string) string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		parts := strings.Split(k, "+")
		for i, p := range parts {
			if p == "" {
				continue // 空の要素で p[:1] が panic しない (この var は init で評価される)
			}
			r := []rune(p)
			parts[i] = strings.ToUpper(string(r[0])) + string(r[1:]) // "ctrl" -> "Ctrl" / "c" -> "C"
		}
		out = append(out, strings.Join(parts, "-"))
	}
	return strings.Join(out, " / ")
}

// deleteLogText は「別セッションの LLM にそのまま投げられる」形の実行記録。
// 実行したコマンド・終了コード・stdout・stderr を**分けたまま**入れる。
func (v *DoctorView) DeleteLogText() string {
	d := &v.Del
	var b strings.Builder
	// 見出しは仕事ごとに変える。LLM へ投げたときに「何をした記録か」が最初の行で分かるように
	if d.Kind == JobCmd {
		b.WriteString("glogx doctor から実行したコマンドの記録 (macOS)\n")
	} else {
		b.WriteString("glogx doctor の削除の記録 (macOS)\n")
	}
	if d.Err != "" {
		b.WriteString("\n中止した理由: " + d.Err + "\n")
	}
	if rep := d.Result; rep != nil {
		for _, e := range rep.Entries {
			fmt.Fprintf(&b, "\n[%s] %s", e.Label, DoctorOutcomeWord(e.Outcome))
			if e.Reason != "" {
				b.WriteString(" — " + e.Reason)
			}
			b.WriteString("\n")
			for _, it := range e.Items {
				fmt.Fprintf(&b, "  %s  %s %s\n", DoctorOutcomeWord(it.Outcome), it.Path, it.Reason)
			}
		}
		if rep.HistoryPath != "" {
			b.WriteString("\n記録: " + rep.HistoryPath + "\n")
		}
	}
	if rep := d.CmdRep; rep != nil {
		fmt.Fprintf(&b, "\n%d 件を実行し、%d 件が失敗しました\n", len(rep.Records), rep.failed())
	}
	if len(d.Log) > 0 {
		b.WriteString("\n実行したコマンド (1| = stdout / 2| = stderr):\n")
		for _, l := range d.Log {
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
func (v *DoctorView) SelectedResults() []disk.Result {
	// 🚨 選択が空なら即返す (issue 274)。hint は毎フレーム selectionSummary 経由でここへ来るが、
	// 条件 (`n > 0 && v.tab == TabDisk`) は**呼び出しの後**にあるので、選択していない間も
	// 全エントリの全 Item を舐め、Item ごとに diskItemKey で文字列を 1 本確保していた
	// (合成 6,400 items の memprofile で確保の 88.9%)。下のループは選択が空なら必ず
	// 空を返すので、これは早期 return であって振る舞いの変更ではない。
	//
	// 呼び出し側 (hint) で `v.tab == TabDisk` を先に見る形にしないのは、次に別の場所から
	// 呼ぶ人が同じ書き忘れをするため。本体側で閉じる。
	if len(v.Selected) == 0 && len(v.SelectedItems) == 0 {
		return nil
	}
	out := make([]disk.Result, 0, len(v.Selected)+len(v.SelectedItems))
	for _, r := range v.currentDiskResults() {
		if v.Selected[resultEntryID(r)] {
			out = append(out, r)
			continue
		}
		var items []disk.Item
		for _, it := range r.Items {
			if v.SelectedItems[diskItemKey(resultEntryID(r), it.Path)] {
				items = append(items, it)
			}
		}
		if len(items) == 0 {
			continue
		}
		out = append(out, r.WithItems(items))
	}
	return out
}

func (v *DoctorView) currentDiskResults() []disk.Result {
	if v.DiskRep != nil {
		return v.DiskRep.Results
	}
	return v.DiskResults
}

// deletable は「その行を選べるか」と、選べない理由。
//
// 🚨 走査していない行 (Reused / FromSnapshot) をここで止める。engine も拒否するが、
// 押した後に失敗するより、押す前に理由を出す方が親切。
func (v *DoctorView) Deletable(r disk.Result) (bool, string) {
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
	case (r.Entry.Inspect || r.Entry.Risk == disk.RiskConfirm) && !v.inspected[resultEntryID(r)]:
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
func (v *DoctorView) toggleSelect() (string, bool) {
	if v.Cur.Index < 0 || v.Cur.Index >= len(v.Rows) {
		return "", false
	}
	key := v.Rows[v.Cur.Index].Key
	if item, ok := key.diskItemKey(); ok {
		return v.toggleItem(item)
	}
	if key.isBrewAction() {
		return v.toggleCmdAction()
	}
	if key.isDocker() {
		return v.toggleDockerAction()
	}
	id, ok := key.diskEntryID()
	if !ok {
		return "選べるのはディスクの行だけです", false
	}
	r, ok := v.findDiskResult(id)
	if !ok {
		return "この行の結果が見つかりません (r で取り直してください)", false
	}
	if ok, why := v.Deletable(r); !ok {
		return why, false
	}
	if v.Selected == nil {
		v.Selected = map[EntryID]bool{}
	}
	v.Selected[id] = !v.Selected[id]
	if !v.Selected[id] {
		delete(v.Selected, id)
	}
	// エントリ全体を選んだら、その中の個別選択は畳む (二重に数えない)
	if v.Selected[id] {
		v.clearItemsOf(id)
	}
	return "", true
}

// toggleItem はディレクトリ単位の選択。
// toggleCmdAction は brew の手の選択を切り替える。**コマンド文字列**で覚える
// (行の key は警告の並びに依存するので、再スキャンで別の手を指す)。
func (v *DoctorView) toggleCmdAction() (string, bool) {
	cmd := CmdKey(v.Rows[v.Cur.Index].CopyPath)
	if cmd == "" {
		return "この行には実行するコマンドがありません", false
	}
	if v.SelectedActions == nil {
		v.SelectedActions = map[CmdKey]bool{}
	}
	if v.SelectedActions[cmd] {
		delete(v.SelectedActions, cmd)
	} else {
		v.SelectedActions[cmd] = true
	}
	return "", true
}

func (v *DoctorView) toggleItem(item itemKey) (string, bool) {
	id, ok := item.entryID()
	if !ok {
		return "", false
	}
	r, ok := v.findDiskResult(id)
	if !ok {
		return "この行の結果が見つかりません (r で取り直してください)", false
	}
	if ok, why := v.Deletable(r); !ok {
		return why, false
	}
	if v.SelectedItems == nil {
		v.SelectedItems = map[itemKey]bool{}
	}
	v.SelectedItems[item] = !v.SelectedItems[item]
	if !v.SelectedItems[item] {
		delete(v.SelectedItems, item)
	}
	// 個別に選んだら、エントリ全体の選択は畳む (「全部」と「一部」を同時に立てない)
	if v.SelectedItems[item] {
		delete(v.Selected, id)
	}
	return "", true
}

func (v *DoctorView) findDiskResult(id EntryID) (disk.Result, bool) {
	for _, r := range v.currentDiskResults() {
		if resultEntryID(r) == id {
			return r, true
		}
	}
	return disk.Result{}, false
}

// hasSelectedItems はそのエントリの中で個別に選ばれたものがあるか (行頭の印に使う)。
func (v *DoctorView) hasSelectedItems(id EntryID) bool {
	for k := range v.SelectedItems {
		if k.belongsTo(id) {
			return true
		}
	}
	return false
}

func (v *DoctorView) clearItemsOf(id EntryID) {
	for k := range v.SelectedItems {
		if k.belongsTo(id) {
			delete(v.SelectedItems, k)
		}
	}
}

// selectionSummary は選択中の**ディレクトリ数**と合計。
// 🚨 エントリ数で数えない: 確認画面が「N 件を削除」を Item 数で出すので、hint だけ
// エントリ数だと「hint は 1 件、確認は 3 件」になる (敵対レビュー 2026-09-03)。
func (v *DoctorView) SelectionSummary() (int, int64) {
	var n int
	var total int64
	for _, r := range v.SelectedResults() {
		n += len(r.Items)
		total += r.Size
	}
	return n, total
}

// ownsKeys は削除の語彙がキーを持っているか (browseModel の updateKeyReachable が見る)。
func (v *DoctorView) OwnsKeys() bool { return v.Del.Active() }

// takeDeleteCmd は handleKey が組んだ Cmd を取り出す (1 回だけ返す)。
func (v *DoctorView) TakeDeleteCmd() tea.Cmd {
	cmd := v.PendingDeleteCmd
	v.PendingDeleteCmd = nil
	return cmd
}

// snapshotRescan は「前回の結果」を表示している画面で削除の操作をしたときに、**拒否ではなく
// 再スキャンへ倒す**。
//
// 🚨 復元した画面は**全行が FromSnapshot** なので、拒否したままだと「サイズは見えているのに
// 何を選んでも断られる」行き止まりになる (ユーザー報告 2026-09-03: Enter で開いた後 Space で
// 選べない)。ヘッダーに「N 分前の結果 (r で再スキャン)」とは出ているが、**削除しようとした
// その瞬間**に気づける形ではなかった。押した意図 (これを消したい) は再スキャンで果たせる。
func (v *DoctorView) snapshotRescan() (doctorAction, bool) {
	if v.SnapshotAt.IsZero() {
		return DoctorSwallow, false
	}
	// 🚨 削除に関係ない行 (brew の警告 / svc) の上では起こさない。押した意図
	// (この行を消したい) が存在しないので、全体の再スキャンは驚きにしかならない
	if v.Cur.Index >= 0 && v.Cur.Index < len(v.Rows) {
		k := v.Rows[v.Cur.Index].Key
		if !k.isDiskRelated() {
			return DoctorSwallow, false
		}
	}
	v.PendingToast = "前回の結果を表示していたので、取り直します (終わったら選び直してください)"
	return DoctorRescan, true
}

// beginDelete は d の入口。選択が無い / 走査中 / 走査していない行が混ざっているときは理由を出す。
func (v *DoctorView) beginDelete() doctorAction {
	if act, ok := v.snapshotRescan(); ok {
		return act
	}
	switch {
	case v.DiskRep == nil:
		// 🚨 v.scanning() ではない: あれは svc / brew も見るので、ディスクが完走していても
		// brew doctor (最大 60 秒) の間ずっと d が通らなくなる。削除に要るのはディスクの結果だけ。
		// 助言も「r で取り直す」と言わない (全部を最初からやり直すので待ち時間が増えるだけ)
		v.PendingToast = "ディスクのスキャンが終わるまで待ってください"
		return DoctorToast
	case len(v.SelectedResults()) == 0:
		v.PendingToast = "Space で削除するものを選んでください"
		return DoctorToast
	}
	// 🚨 走査していない行は engine が拒否する。押す前にここで止める
	for _, r := range v.SelectedResults() {
		if ok, why := v.Deletable(r); !ok {
			v.PendingToast = r.Entry.Label + ": " + why
			return DoctorToast
		}
	}
	v.PendingDeleteCmd = v.StartDelete(v.SelectedResults(), true) // 下見 (何も壊さない)
	return DoctorRunDelete
}

// deletePanel は確認 / 進捗 / 結果の全画面パネル。doctor 自体が全画面なので、重ねずに差し替える
// (重ねると狭い幅で下の行が透けて「どれを消すのか」が読めなくなる)。
func (v *DoctorView) deletePanel(o DoctorRenderOpts) []string {
	d := &v.Del
	switch {
	case d.Err != "":
		body := []string{d.Err, "", "何も消えていません。"}
		if len(d.Log) > 0 {
			body = append(body, "", " 実行したコマンド:")
			body = append(body, d.Log...)
		}
		return doctorPanel(o, "削除できませんでした", append(body, "", "y: 出力をコピー   他のキー: 閉じてもう一度スキャン"))
	case d.Result != nil:
		blocks, tail := doctorDeleteResultLines(o, *d.Result, d.Log)
		return assembleDeletePanel(o, "削除の結果", blocks, tail, nil)
	case d.CmdRep != nil:
		return doctorPanel(o, "実行の結果", cmdResultLines(o, *d.CmdRep, d.CmdPlan, d.Log))
	case d.Blocking():
		head := "削除しています"
		switch {
		case d.Kind == JobCmd:
			head = "実行しています"
		case d.Preparing:
			head = "削除できるか確認しています"
		}
		// 🚨 極小の端末では**抜ける手段を優先して**進捗を 1 行へ畳む。
		// blocking 中は handleDeleteKey が全キーを飲むので、中断案内が画面から消えると
		// 「このパネルから出る方法がどこにも書いていない」状態になる。
		// 実測 (敵対レビュー 2026-09-04): 2 行にしたことで案内が出る下限が
		// running は page 5 → 6、preparing は 6 → 7 へ上がっていた。7 未満で畳めば元に戻る
		prog := DeleteProgressLines(o, d.Progress)
		if o.Page < 7 && len(prog) > 1 {
			prog = []string{strings.TrimSpace(prog[0]) + "  " + strings.TrimSpace(prog[1])}
		}
		prog = append(prog, "")
		body := prog
		if d.Preparing {
			body = append(body, "対象を走査し直しています (消してよいかを測り直します)")
		}
		// 🚨 中断の案内は相に依らず出す。下見中も handleDeleteKey の blocking 分岐が
		// Ctrl-C を受けて cancel でき (実測 2026-09-04: 走査が打ち切られて確認へ戻る)、
		// 案内が無いと「このパネルから抜ける手段が無い」に見える
		// armedCC なら残り 1 回。「2 回押せ」と「もう 1 回押せ」を並べると、あと何回なのか読めない
		//
		// 🚨 **Ctrl-G も併記する** (issue 244)。tui.go は ctrl+g を ctrl+c と同じ 2 段ガードへ
		// 渡すので実際に中断できるが、案内が Ctrl-C しか書いていなかった。Ctrl-G で抜けようと
		// した人は「効かない」と思って Ctrl-C を探し (実際には 1 回目が消費されている)、
		// 逆に Ctrl-G 2 回で中断したときは案内を読んでいた人に理由が分からない。
		// glogx の他の全画面 (issues viewer / url picker) でも ctrl+g は esc と同じ「やめる」なので、
		// **挙動を正として案内を直す**向きで揃えた。
		if d.ArmedCC {
			body = append(body, "もう一度 "+deleteAbortKeysWord+" を押すと中断します")
		} else {
			body = append(body, deleteAbortKeysWord+" を 2 回押すと中断します")
		}
		// 実行したコマンドと出力を垂れ流す (何が起きているかを見せる)。入る分だけ末尾を出す
		if len(d.Log) > 0 {
			body = append(body, "")
			body = append(body, tailLines(d.Log, max(o.Page-len(body)-3, 1))...)
		}
		return doctorPanel(o, head, body)
	case d.Confirm && d.Kind == JobCmd:
		blocks, tail := cmdConfirmLines(o, d.CmdPlan)
		return assembleDeletePanel(o, "これを実行しますか?", blocks, tail, &d.ConfirmScroll)
	case d.Confirm:
		blocks, tail := v.confirmLines(o)
		title := "本当に削除しますか?"
		if !planHasWork(d.Plan) {
			// 下見の結果、消せるものが 1 件も無かった。**「削除しますか?」と聞かない**
			// (y に意味が無いのに押させる形になる)
			title = "消せるものがありません"
		}
		return assembleDeletePanel(o, title, blocks, tail, &d.ConfirmScroll)
	}
	return nil
}

// cmdConfirmLines は「これから何を実行するか」。**コマンドをそのまま全部出す**
// (同意の対象と実行の対象を一致させる。削除側が issue 245 で学んだのと同じ規律)。
func cmdConfirmLines(o DoctorRenderOpts, acts []doctorCmdAction) (blocks [][]string, tail []string) {
	for _, a := range acts {
		b := []string{"▸ " + doctorColor(o.Colored, term.AnsiBold, a.Label), "  $ " + a.Cmd}
		if a.Note != "" {
			for i, w := range term.WrapToWidth(a.Note, max(20, o.Width-8)) {
				pre := "  🚨 "
				if i > 0 {
					pre = "     "
				}
				b = append(b, doctorColor(o.Colored, term.AnsiYellow, pre+w))
			}
		}
		blocks = append(blocks, b)
	}
	tail = []string{"", "y: 実行する   n/Esc: やめる   (Enter は何もしない)"}
	return blocks, tail
}

// cmdResultLines は実行の結果。**失敗の有無を先に言い切る** (ログを読む前に分かるように)。
func cmdResultLines(o DoctorRenderOpts, rep cmdRunReport, plan []doctorCmdAction, log []string) []string {
	head := fmt.Sprintf("%d 件すべて成功しました", len(rep.Records))
	if f := rep.failed(); f > 0 {
		head = doctorColor(o.Colored, term.AnsiYellow, fmt.Sprintf("%d 件中 %d 件が失敗しました", len(rep.Records), f))
	}
	if n := len(plan) - len(rep.Records); n > 0 {
		// 中断で走らなかったぶん。0 件に畳まず「走っていない」と言う
		head += fmt.Sprintf(" (%d 件は実行していません)", n)
	}
	out := []string{head, ""}
	out = append(out, log...)
	return append(out, "", "y: 出力をコピー   他のキー: 閉じてもう一度スキャン")
}

// planHasWork は下見の結果に「実際に消すもの」があるか (全部 対象外 / 提示のみ なら false)。
func planHasWork(plan *disk.DeleteReport) bool {
	return len(plannedIDs(plan)) > 0
}

// plannedIDs は下見で「これから消す」と出たエントリの ID。
//
// 🚨 **確認画面と実行対象の唯一の出典** (issue 245)。以前は「消せるものが在るか」の判定
// (planHasWork) と「何を消すか」の判定 (case "y" の selectedResults) が別ソースで、
// **確認画面が何を約束しても実行が従わない**構造だった。述語をここに 1 つ置く。
func plannedIDs(plan *disk.DeleteReport) map[EntryID]bool {
	if plan == nil {
		return nil
	}
	ids := make(map[EntryID]bool, len(plan.Entries))
	for _, e := range plan.Entries {
		if e.Outcome == disk.OutcomePlanned {
			ids[EntryID(e.ID)] = true
		}
	}
	return ids
}

// plannedTargets は選択のうち、下見で Planned になったエントリだけを返す
// (確認画面に出した対象と実行対象を一致させる。issue 245)。
func plannedTargets(plan *disk.DeleteReport, sel []disk.Result) []disk.Result {
	planned := plannedIDs(plan)
	if len(planned) == 0 {
		return nil
	}
	out := make([]disk.Result, 0, len(sel))
	for _, r := range sel {
		if planned[resultEntryID(r)] {
			out = append(out, r)
		}
	}
	return out
}

// confirmLines は確認の本文。**下見 (DryRun) の結果をそのまま出す** (UI 側で組み直さない)。
//
// 並びは一覧の行と同じ「サイズ / ラベル / 語」。🚨 記号を先頭に置く形は採らない:
// `・` (全角) と絵文字と `—` が同じ列に来て、幅は合っていても目には揃わない
// (~/.claude/rules/no-mixed-width-columns-in-terminal-ui.md)。
// 🚨 結末の語は **DoctorOutcomeWord から取る** (結果画面と同じ語)。ここで別の語を作ると
// Skipped「触らなかった」と Failed「実行できなかった」が 1 語に畳まれ、しかも `🚫 対象外` は
// 一覧 (disk.Mark) では StatusBlocked の語なので、同じ記号が 3 つの意味を持つ。
//
// ⚠️ 記号は**単独で幅 2 のもの**だけを使う。`🗑` は実測で幅 1、`🗑️` (VS16 付き) は 2 で、
// 端末によって右端が動く。既存の DoctorRiskMark と同じ語彙 (✅ 🚨 🚫 ⛔ ❓) に `🚮` `📋` `❌` を足した。
func (v *DoctorView) confirmLines(o DoctorRenderOpts) (blocks [][]string, tail []string) {
	labelW := deleteLabelWidth(v.Del.Plan.Entries, o.Width)
	var freeing, trashing int64
	for _, e := range v.Del.Plan.Entries {
		var out []string
		size, word := disk.HumanSize(e.BeforeSize), deleteMethodWord(e.Method)
		skipped := e.Outcome == disk.OutcomeSkipped || e.Outcome == disk.OutcomeFailed
		switch {
		case skipped:
			size, word = "---", doctorPlanOutcomeWord(e.Outcome)
		case e.Method == "trash":
			trashing += e.BeforeSize
		case e.Method != "propose":
			freeing += e.BeforeSize
		}
		out = append(out, fmt.Sprintf(" %8s  %s  %s", size, padLabel(e.Label, labelW), word))
		// 🚨 件数は**実際に触る Item だけ**を数える (issue 233)
		planned, dropped := plannedItems(e)
		switch {
		case skipped:
			out = append(out, deleteNote(o, e.Reason))
		case e.Method == "trash":
			out = append(out, deleteNote(o, fmt.Sprintf("%d 件をゴミ箱へ移動 (空にするまで容量は戻りません)", len(planned))))
		case e.Method == "cli":
			// コマンドの実体はこの下に 1 本ずつ出るので、ここでは件数だけ (二重に出さない)
			out = append(out, deleteNote(o, fmt.Sprintf("%d 件にコマンドを実行", len(planned))))
		case e.Method == "propose":
			out = append(out, deleteNote(o, "コマンドを表示するだけで、実行しません"))
		default:
			out = append(out, deleteNote(o, fmt.Sprintf("%d 件を削除", len(planned))))
		}
		if !skipped {
			if len(dropped) > 0 {
				// 黙って省かない (件数が減った理由が読めないと下見の結果を確かめられない)
				out = append(out, droppedNote(o, dropped))
			}
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
	if !planHasWork(v.Del.Plan) {
		return blocks, append(tail, " 何かキーを押すと戻ります")
	}
	return blocks, append(tail, " y: 削除する      n / Esc: やめる      (Enter は何もしない)")
}

// deleteCommandLines は「このエントリで実際に実行するコマンド」を並べる (ユーザー要望 2026-09-03)。
// 組み立ては engine (EntryOutcome.CommandLines) が持つ = **確認に出した形と実行する形が同じ**。
// rm / trash は外部コマンドを起こさないので何も出さない (経路の語が既にそう言っている)。
func deleteCommandLines(o DoctorRenderOpts, e disk.EntryOutcome) []string {
	if e.Method == "propose" {
		return []string{deleteNote(o, "実行しません。手で叩いてください: "+cleanOneLine(e.Command))}
	}
	// 🚨 コマンド行も**実際に実行する Item だけ**から作る (issue 233 の敵対レビュー P2-1)。
	// EntryOutcome.CommandLines は全 Item を回すが、execCLI は `Outcome != OutcomePlanned` を
	// 飛ばす (delete.go)。件数とパスだけ絞ると「1 件にコマンドを実行」の下にコマンドが 2 本
	// 並ぶ形になり、この関数の doc が言う「確認に出した形と実行する形が同じ」が崩れる
	planned := e
	planned.Items, _ = plannedItems(e)
	cmds := planned.CommandLines()
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
// plannedItems は下見で**実際に触ると決まった** Item だけを返す (issue 233)。
//
// 🚨 エントリ単位の Outcome だけを見ると、下見で Skipped / Failed になった Item まで
// 「N 件を削除」に数え、パス一覧にも並ぶ。同じ行のサイズは BeforeSize (照合が取れた分だけ) なので
// **件数とサイズが食い違う**。発火は「走査時と実体が変わった」= キャッシュ相手では珍しくない。
func plannedItems(e disk.EntryOutcome) (planned []disk.ItemOutcome, dropped []disk.ItemOutcome) {
	for _, it := range e.Items {
		if it.Outcome == disk.OutcomePlanned {
			planned = append(planned, it)
			continue
		}
		dropped = append(dropped, it)
	}
	return planned, dropped
}

// droppedNote は省いた Item を件数**と理由**で伝える。
// 🚨 固定文言にしない (敵対レビュー 2026-09-04 の P3)。dropped には「対象パスを拒否: …」
// (validateTarget = 細工の兆候) や「実体を識別できません」も落ちてくるので、
// 「走査時と実体が変わりました」で畳むと**細工の兆候が消える**。理由は重複を畳んで並べる。
func droppedNote(o DoctorRenderOpts, dropped []disk.ItemOutcome) string {
	seen := map[string]bool{}
	reasons := make([]string, 0, 2)
	for _, it := range dropped {
		r := cleanOneLine(it.Reason)
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		reasons = append(reasons, r)
		if len(reasons) == 2 { // 3 つ目からは件数だけ (行を増やして本文を押し出さない)
			break
		}
	}
	if len(reasons) == 0 {
		return deleteNote(o, fmt.Sprintf("他 %d 件は対象外", len(dropped)))
	}
	return deleteNote(o, fmt.Sprintf("他 %d 件は対象外 (%s)", len(dropped), strings.Join(reasons, " / ")))
}

// 🚨 1 エントリあたりの表示は maxConfirmPaths 件で打ち切る。全部並べると 1 エントリで画面を
// 埋め、**他のエントリが窓の外へ押し出される** (送れば読めるが、最初のフレームに何が出るかは
// 「どれを消すか」の把握に効く)。打ち切ったことは件数で伝える。
// 🚨 理由の更新 (issue 241): 以前は「塊単位で落とすので他のエントリが**丸ごと省略される**」と
// 書いていたが、確認パネルは行単位の窓になったので丸ごと省略はもう起きない。打ち切りを残すのは
// 「1 エントリで最初の画面を埋めない」ためで、11 件目以降が窓でも読めない点は残っている
// (issue 241 の決着節に記録)。
func deletePathLines(o DoctorRenderOpts, e disk.EntryOutcome) []string {
	// 🚨 並べるのは**実際に触る Item だけ** (issue 233)。下見で対象外になったパスを混ぜると、
	// 上の doc の「実際に触る対象そのもの」が成立しない
	items, _ := plannedItems(e)
	out := make([]string, 0, min(len(items), maxConfirmPaths)+1)
	for i, it := range items {
		if i >= maxConfirmPaths {
			out = append(out, deleteNote(o, fmt.Sprintf("… 他 %d 件", len(items)-maxConfirmPaths)))
			break
		}
		// 🚨 パスは**ファイル名由来**なので改行や制御文字が入りうる (macOS のファイル名は
		// `/` と NUL 以外を許す)。1 行 = 1 件の契約を破ると、確認画面に偽の行
		// (「y: 削除する」等) を差し込めてしまう。印字可能文字だけに絞る
		// 🚨 パスは**先頭を削って末尾を残す** (issue 239)。確認画面で末尾が落ちると、
		// 同一プロジェクトの旧 DerivedData のように**世代の違いが見分けられない**まま
		// y を押すことになる。deleteNote が 11 桁字下げするので、その分を引いた予算で詰める
		out = append(out, deleteNote(o, doctorFitPath(cleanOneLine(it.Path), o.Width-deleteNoteIndent)))
	}
	return out
}

const maxConfirmPaths = 10

// doctorDeleteResultLines は結果。**incomplete を成功にも失敗にも畳まない**。
func doctorDeleteResultLines(o DoctorRenderOpts, rep disk.DeleteReport, log []string) (blocks [][]string, tail []string) {
	labelW := deleteLabelWidth(rep.Entries, o.Width)
	for _, e := range rep.Entries {
		out := []string{fmt.Sprintf(" %8s  %s  %s",
			deleteResultSize(e), padLabel(e.Label, labelW), DoctorOutcomeWord(e.Outcome))}
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
	// 🚨 **合計行だけを読んで「全部消えた」と読み違えるのを止める** (issue 316)。
	// 各行には ❌ / 🚨 が出ているが、目に入りやすいのは末尾の合計行で、そこは成功分しか語らない。
	// `HasFailures` はまさにこの読み違えを防ぐために作られた口 (delete.go の doc) なのに、
	// production から一度も呼ばれていなかった (= 配線漏れ)。
	if rep.HasFailures() {
		tail = append(tail, " 🚨 消せなかった / 未完了の項目があります (上の ❌ / 🚨 の行)")
	}
	// 🚨 hint (doctor_view.go) と同じことを言う。「何かキー」と書くと y を含んでしまうが、
	// y / Y は閉じずにコピーする (handleDeleteKey の result/err 分岐)
	return blocks, append(tail, " y: 出力をコピー   他のキー: 閉じてもう一度スキャン")
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

// DoctorOutcomeWord は結末の固定語彙 (記号 + 語)。一覧の DoctorRiskMark と同じ作り:
// NO_COLOR で色が消えても語で区別できるようにする。
func DoctorOutcomeWord(o disk.Outcome) string {
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

// doctorPlanOutcomeWord は**下見 (確認画面) の**結末語。結果画面 (DoctorOutcomeWord) と
// 分けているのは、同じ Outcome でも指している状態が違うため:
//
//   - 下見の Skipped は「この行は今回消さない」で、一覧が同じ行に付ける語と揃える必要がある。
//     結果画面の「🚫 触れず」(= 実行したが触らなかった) とは**別の状態**なので語を借りない。
//     🚨 **同じ語に 3 つの理由が載る**ので、直下の理由文は状態ごとに違う:
//     engine の StatusBlocked 分岐なら「いまは対象外です: …」、触る対象 0 件なら
//     「触れる対象がありませんでした」、下見そのものが中断されたなら
//     「中断されました (消してよいかを確認できていません)」(issue 246 で 3 つ目が増えた)。
//     語だけを見て理由文を決め打ちしないこと
//     🚨 一覧 (disk.Mark) と語が割れていないかは confirmLines のテストが突き合わせる
//   - Failed は下見でも結果でも「実行できなかった」なので、結果画面の語をそのまま借りる
func doctorPlanOutcomeWord(o disk.Outcome) string {
	if o == disk.OutcomeSkipped {
		return "🚫 対象外"
	}
	return DoctorOutcomeWord(o)
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
		w = max(w, term.DispWidth(e.Label))
	}
	return max(8, min(w, width-deleteRowFixedW))
}

// deleteWordW は行の最後の列 (語) に充てている幅。
// 🚨 語の出典は deleteMethodWord **と** doctorPlanOutcomeWord / DoctorOutcomeWord の両方。
// 今の最長は「📋 コマンド」= 2+1+10 = 13 で収まっているが、これを超える語を足すと
// ラベルの予算が破れ、行末の term.TruncateDisp が**語そのもの**を切る (語が最後の列なので)。
// 全語彙が収まっているかは TestDeleteVocabulary が見る
const deleteWordW = 13

// deleteRowFixedW は 1 行のうちラベル以外が使う幅 (先頭 1 + サイズ 8 + 区切り 2 + 区切り 2 + 語)。
const deleteRowFixedW = 1 + 8 + 2 + 2 + deleteWordW

func padLabel(label string, w int) string {
	label = term.TruncateDisp(label, w, "…")
	return label + term.PadSpaces(w-term.DispWidth(label))
}

// deleteNote はラベル列の下に付く補足 (dim)。1 行 = 1 件の契約なので改行は入れない。
func deleteNote(o DoctorRenderOpts, s string) string {
	return doctorColor(o.Colored, term.AnsiDim, term.PadSpaces(deleteNoteIndent)+s)
}

// deleteNoteIndent は deleteNote の字下げ幅 (パスの予算計算に要る)。
const deleteNoteIndent = 11

// assembleDeletePanel は「見出し + エントリの塊 + 末尾」をちょうど page 行に収める。
//
// 🚨 padTo は**溢れた分を切る** (lines[:n])。素直に並べると、選択が多いときに末尾の
// 「y: 削除する n/Esc: やめる」が消え、**操作が分からない確認プロンプト**になる。
// なので末尾を先に確保し、入る分だけエントリを載せて、省いた件数を注記する。
// assembleDeletePanel は見出し + 本文 + 末尾を組む。sc != nil の呼び出し (確認パネル) は
// 本文を**行単位の窓**で見せる: 塊単位で落とす形だと、1 つの塊が窓より大きいときに
// 本文が丸ごと消える (issue 241)。sc == nil (結果パネル) は従来どおり塊単位で落とす。
func assembleDeletePanel(o DoctorRenderOpts, title string, blocks [][]string, tail []string, sc *deleteScroll) []string {
	head := []string{" " + doctorColor(o.Colored, term.AnsiBold, title), ""}
	room := max(o.Page-len(head), 1)
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
			out[i] = term.TruncateDisp(l, o.Width, "…")
		}
		return layout.PadTo(out, o.Page)
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
		body = append(body, doctorColor(o.Colored, term.AnsiDim,
			fmt.Sprintf(" … 他 %d 件は画面に入りません (端末を広げてください)", len(blocks)-shown)))
	}
	out := make([]string, 0, len(head)+len(body)+len(kept))
	out = append(out, head...)
	out = append(out, body...)
	out = append(out, kept...)
	for i, l := range out {
		out[i] = term.TruncateDisp(l, o.Width, "…")
	}
	return layout.PadTo(out, o.Page)
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
func doctorPanel(o DoctorRenderOpts, title string, body []string) []string {
	out := []string{" " + doctorColor(o.Colored, term.AnsiBold, title), ""}
	for _, l := range body {
		out = append(out, term.TruncateDisp(" "+l, o.Width, "…"))
	}
	return out
}
