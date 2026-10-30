package main

import (
	"errors"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/spinner"
	"github.com/jiikko/dotfiles/src/tuikit/toast"

	"glogx/internal/term"

	"glogx/internal/app"

	tea "charm.land/bubbletea/v2"
)

// tickMsg の seq は張ったときの番号 (tickchain)。
type tickMsg struct{ seq uint64 }

func tickEvery(d time.Duration, seq uint64) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{seq} })
}

// maybeTick は tick を 1 本のチェーンに束ねて仕込む。張ってある tick が今の周期のうちに届くなら nil を返して
// 二重チェーンを作らない。tea.Batch(cmd, maybeTick()) は Init・各 fetch 経路など多数に散らばり、
// 非同期処理が重なるたびに独立した自己増殖チェーンが恒久追加されて (push 直後ポーリングでは
// 最長 2 分間に ~48 本まで) 再描画/アニメが N 倍化していた (レビュー C1)。🚨 ciPoll の tea.Tick は別周期の
// 独立タイマーなので maybeTick を通さない (それぞれ seq/guard で管理)。
//
// 周期は動きの間だけ上げ (tickInterval)、それ以外は spinnerInterval (12.5fps) に落として CPU を節約する。
// 張ってある 80ms の tick より早く要るとき (動きが始まった) は張り直す。bool の single-flight にしない:
// 張った時点の周期に縛られ、動きの次のフレームが張ってある遅い tick まで最大 80ms 止まる。
// 追い越された古い tick は onTick が捨てる。
func (m *browseModel) maybeTick() tea.Cmd {
	d := m.tickInterval()
	seq, ok := m.ticks.Want(app.TimeNow(), d)
	if !ok {
		return nil
	}
	return tickEvery(d, seq)
}

// tickInterval は今のフレーム周期。横に動く演出 (scroll glide / toast スライド / 引き出し) の
// 最中だけ ~30fps へ上げ、それ以外は 12.5fps に落とす。アプリ全体の開閉演出と
// issues / status viewer の開閉スライドはさらに上げる (理由は zoomInterval の doc)。
//
// 🚨 高 FPS が要る演出の登録先はここだけ。spinnerActive は「tickInterval が周期を上げているか」で
// 演出の有無を導出するので、ここに足せばチェーン維持にも効く。両方へ手で足す規約にすると、
// 足し忘れで「回るが 12.5fps」になり点滅に見える再発が実際に 2 回起きた。
func (m *browseModel) tickInterval() time.Duration {
	if m.zoom.animating(app.TimeNow()) || m.issuesOv.SlideAnimating() || m.statusOv.SlideAnimating() {
		return zoomInterval // 短い演出なので周期がそのままフレーム数になる (60fps)
	}
	if m.glide.Active() || m.diffOv.Animating() || m.toast.Animating() || m.issuesOv.Animating() || m.statusOv.Animating() || m.filerV.Animating() {
		return scrollInterval // スライドを滑らかに (30fps)
	}
	return spinnerInterval
}

// showWarning は失敗/警告トーストを出しつつ lastWarning に残す (w で表示が消えた後もコピー
// できるように)。
//
// どの失敗をここへ通すかの基準 (🚨 「失敗は必ずこれを経由」ではない。
// 素の toast.show(…, false) を使う場面があり、3 分類):
//
//   - エラー詳細を含み、後で見返す価値があるもの (「pull に失敗: …」) → showWarning
//   - 操作を拒否した理由の案内 (「未 push のコミットはありません」) → 素の toast.show(…, false)。
//     コピー対象でないうえ、lastWarning を上書きすると直前のエラーがコピー不能になる
//   - クリップボード操作そのものの失敗 → 素の toast.show(…, false)。w (警告コピー) の対象を
//     潰さないため。そもそもクリップボードが壊れているなら警告のコピーも失敗する
//
// 成功トースト (toast.show(…, true)) もこれを通さない (成功文言で lastWarning が上書きされると
// 直前のエラーがコピー不能になる)。
func (m *browseModel) showWarning(text string) {
	// lastWarning は w でクリップボードへコピーされる = 端末の外へ出る値なので、
	// 表示 (toast 側で無害化) とは別にここでも無害化する。制御文字入りの文字列を
	// 貼り付け先へ持ち出さない
	m.lastWarning = term.SanitizePlainLine(text)
	m.toast.Show(text, false)
}

// showGitFailure は git の操作の失敗を出す。トーストは理由の 1 行だけ、w でコピーする lastWarning には git の出力の
// 全文を付ける (理由の行の前後 = どの ref が・どのファイルが、は全文にしか無い)。全文は行ごとに無害化して改行を残す。
func (m *browseModel) showGitFailure(prefix string, err error) {
	m.showWarning(prefix + ": " + app.FirstLine(err.Error()))
	var ge *gitOutputError
	if errors.As(err, &ge) && ge.Output != "" {
		m.lastWarning += "\n\n" + term.SanitizePlainBlock(ge.Output)
	}
}

// deliverNotice は viewer の操作結果 (takeNotice の戻り) をトーストへ流す。lastWarning に積むのは
// app.NoticeError だけ。戻り値は「流したか」で、呼び出し側がトーストを動かす tick を束ねるかの判断に使う。
// 🚨 setNotice は打鍵経路 (handleKey 直後) だけでなく Msg 経路 (issuesScanMsg → rebindOpen)
// からも置かれる。Msg 経路の呼び出し側でもこれを通すこと。通さないと「置いたが誰も取り出さず、
// 次の打鍵まで画面に出ない」黙殺になる (本文が無言で畳まれる)。
func (m *browseModel) deliverNotice(text string, kind app.NoticeKind) bool {
	if text == "" {
		return false
	}
	switch kind {
	case app.NoticeOK:
		m.toast.Show(text, true)
	case app.NoticeRefused:
		m.toast.Show(text, false)
	case app.NoticeError:
		m.showWarning(text)
	case app.NoticeInfo:
		m.toast.ShowInfo(text)
	}
	return true
}

func (m *browseModel) onTick(msg tickMsg) (tea.Model, tea.Cmd) {
	// 張り直しで追い越された古い tick は捨てる: 新しい tick がまだ張ってある。処理すると、フレームの数で進む動き
	// (pull の行・push の罫線・glide・toast のスライド) が余分に 1 歩進む
	if !m.ticks.Fired(msg.seq) {
		return m, nil
	}
	// 閉じる演出が着地したらここで初めて終了する (演出の間はまだ描き続ける)
	if m.zoom.settle(app.TimeNow()) {
		m.done = true
		return m, tea.Quit
	}
	// issues viewer の閉じる演出が着地したらここで畳む。🚨 下の spinnerActive の早期 return
	// より前に置く: animating() は closing のあいだ true を返し続け、この settleClose が
	// 下ろして初めて false になる。後ろに置くと最後の 1 拍が届かず閉じかけの姿で固まる。
	m.issuesOv.SettleClose()
	m.statusOv.SettleClose() // status viewer も同じ理由で spinnerActive の判定より前に畳む
	// treefiler の動きも spinnerActive の判定より前に進める (後ろだと、動き終わる最後の 1 拍が届かず途中の姿で止まる)
	m.filerV.Advance(app.TimeNow())
	m.filerV.DrainNotices(m.deliverNotice)
	m.lanV.Advance(app.TimeNow()) // 走査の途中の結果を取り込む (spinnerActive の判定より前: 走査が終わる最後の 1 拍を取りこぼさない)
	m.lanV.DrainNotices(m.deliverNotice)
	if !m.spinnerActive() {
		return m, nil // アニメ対象なし: 再アームせずチェーンを終わらせる
	}
	if m.pullAnimating {
		m.advancePullAnim() // pull で増えた新規行を 1 行/フレームで上から降らせる
	}
	if m.glide.Active() {
		m.glide.Advance(m.offset) // 一覧のスクロールを表示 offset で滑らせる
	}
	// pager 側 (diff / issues 一覧・本文) の glide も同じ tick で進める。共有型なので
	// 手触り (カーブ・フレーム数) は自動的に一覧と揃う (internal/term/scroll_glide.go)。
	m.diffOv.AdvanceGlide()
	m.issuesOv.AdvanceGlide()
	m.statusOv.AdvanceGlide()
	var pushRefetchCmd tea.Cmd
	if m.pushAnimating {
		pushRefetchCmd = m.advancePushAnim() // push 境界の罫線を 1 コミット/フレームで上へ
	}
	for sha, start := range m.pushSlides {
		if app.TimeNow().Sub(start) >= pushSlideDuration {
			delete(m.pushSlides, sha) // 沈み込みが終わった区画は通常表示へ戻す
		}
	}
	var toastHoldCmd tea.Cmd
	if m.toast.Animating() {
		// トーストの横スライド (右画面外との出入り) をカラム単位で 1 フレーム進める。
		// 入場完了時は holding へ移り toast.Hold 後の退場タイマーを返す。
		toastHoldCmd = toastTimers(m.toast.Advance())
	}
	m.frame++
	// list に毎フレーム変化する内容 (loading スピナー) が乗るのは fetch/awaitCI の 2 状態
	// だけ。他の spinnerActive 条件 (panelHasPendingJob/pullAnimating/detailsLoading/
	// jobDetailBusy/diffBusy) のスピナー・経過時間は panelLines/diffBoxLines 側 (lines() の
	// 外) で毎フレーム描かれるので、ここで list を無効化すると -p 巨大 patch を含む全行を
	// 80ms ごとに組み直すだけの無駄になる (レビュー C7)。offset を動かす pull アニメも
	// lines() は不変なので invalidate 不要 (View が窓を切り直す)
	//
	// 🚨 この 2 状態では「header 行のスピナー字形」を変えるために全行を組み直している。
	// perf 監査 2026-07-25 で指摘されたが、さらに絞る対応はしないと判断した:
	// 既定 (patch なし) の RenderLines は 7.8µs / 12.6KB で 80ms 周期に対し無視できる。
	// 効くのは -p 併用時 (実測 332µs / 733KB per frame) だけで、それも fetch 中の数秒に限る
	// = 80ms 予算の 0.4%。一方 header 行だけの差分更新にするには Line の契約 (Text に
	// 字形が焼き込まれている) を変えるか、verbatim / mediumLines の 2 経路へ header 生成を
	// 複製する必要があり、表示バグの危険が利得に見合わない。
	// -p を常用して fetch 中の描画が重いと体感したら再評価する (その時は Line にスピナー
	// 位置を持たせて View 側で差し替えるのが筋)。
	//
	// CI 実行中 (StatePending) のコミットも header 行がスピナーなので同じく組み直す。こちらは
	// fetch と違って CI の実行時間 (数分) 続くが、コストは上の実測どおり既定で 7.8µs、-p でも
	// 80ms 予算の 0.4% なので同じ判断で受ける。
	if m.fetching() || len(m.awaitCI) > 0 || m.listHasPending() {
		m.invalidateLines()
	}
	return m, tea.Batch(m.maybeTick(), toastHoldCmd, pushRefetchCmd)
}

// startScrollAnim は一覧のビューポートが動いたとき、表示 offset を旧位置 prev から論理 offset へ
// 数フレームで滑らせる (ユーザー要望「にゅっと」)。呼び出しは j/k の 1 コミット移動と半ページ
// 移動 (ctrl+d/u・PgDn/PgUp。ユーザー要望 2026-07-31)。g/G の端ジャンプは距離が不定なので
// 即時のまま。
//
// glide はフレーム数で終わる (距離では終わらない) ので、1 コミット移動でも半ページでも所要は
// 一定 (~200ms) になり、背高コミット (長メッセージ・stat/patch) でも間延びしない。高さで
// animate/snap が変わる違和感を避けるため行数キャップは設けない (ユーザー要望 2026-07-21)。
// 連打の積み上げ (「押した分だけ遅れて動く」最悪の体感) の抑制は glide.Start が持つ。
// 論理 offset は呼び出し側 (ensureCursorVisible / clampOffset) が既に動かしているので触らない。
func (m *browseModel) startScrollAnim(prev int) tea.Cmd {
	if m.offset == prev {
		return nil // ビューポートは動いていない (カーソルが画面内 / 端で clamp された)
	}
	if m.pullAnimating {
		m.glide.Stop() // pull アニメ中は積まず即時 (連打の抑制は glide.Start が持つ)
		return nil
	}
	if !m.glide.Start(prev, m.offset, term.ScrollAnimFrames) {
		return nil
	}
	return m.maybeTick()
}

// spinnerActive は「毎フレームの tick チェーンを回し続けるか」の単一ゲート
// (アニメ・スピナー・ライブ経過時間の出典をすべて OR で束ねる)。
//
// 🚨 ここに「フォーカスされているか」を足して非フォーカス中の tick を止める案 (bubbletea v2 の
// FocusMsg/BlurMsg。o/p で別アプリへ移った後も 80ms tick と usage の毎分リフレッシュが
// 回り続けるのを削る) は実装が小さいが、「今は不要」とのユーザー判断で見送っている (2026-07-25)。
// CPU が気になると言われたら再評価する。🚨 tmux は focus-events off なので、やるなら
// 先に on へ戻すか (pane の行き来が遅くなる代償つき) を決める必要がある。
// 経緯と他の未採用 v2 機能は docs/glogx-bubbletea-v2.md。
func (m *browseModel) spinnerActive() bool {
	// 演出 (glide / toast / 開閉スライド / zoom) は列挙しない: tickInterval が周期を上げている =
	// 何かの演出中、で導出する。演出の登録先を tickInterval の 1 箇所に保つ (同期漏れの再発防止)
	return m.tickInterval() != spinnerInterval || m.fetching() || m.actModal.running() || m.pullAnimating || m.pushAnimating || len(m.pushSlides) > 0 || len(m.awaitCI) > 0 || len(m.detailsLoading) > 0 || m.detailOv.Fetching() || m.diffOv.Fetching() || m.prStatusOv.Fetching() || m.listHasPending() || m.panelHasPendingJob() || m.usageOv.Loading() || m.rlDashLoading() || m.issuesOv.Loading() ||
		m.statusOv.Fetching() || m.doctorOv.Scanning() || m.doctorOv.Deleting() || m.filerV.Busy() || m.lanV.Busy()
}

func (m *browseModel) spinner() string {
	return spinner.Frame(m.frame)
}

// frameActive は今このフレームで最外周フレームを描くか。起動時固定の showFrame に加え、端末が
// 下限サイズ以上のときだけ true。入力 (showFrame + width/height) は WindowSizeMsg でのみ変化し、
// そこで invalidateLines 済みなので linesCache の無効化点は増やさない (issue の不変条件)。
func (m *browseModel) frameActive() bool {
	return m.showFrame && m.width >= frameMinWidth && m.height >= frameMinHeight
}

// toastTimers は toast の退場タイマー (静止に入った枚ごと) を bubbletea の Tick にする (github.com/jiikko/dotfiles/src/tuikit/toast はタイマーを張らない)。
func toastTimers(ts []toast.Timer) tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(ts))
	for _, t := range ts {
		msg := t.Msg
		cmds = append(cmds, tea.Tick(t.After, func(time.Time) tea.Msg { return msg }))
	}
	return tea.Batch(cmds...)
}

// toastDrawBudget は、通常の半ページ予算を保ちつつ最新の 1 枚と重要警告 2 枚ぶんを確保する。
// 下限がないと狭い窓で重要警告が 1 枚に減り、上限がないと 2 箱が窓を覆うため、両方が要る。
// reserved は最新 + 新しい重要警告 2 枚の実際の行数 (Stack.ReservedHeight)。🚨 下限は 1 行の箱 2 枚ぶん (BoxHeight*2) と
// これの大きい方: 折り返した警告は 1 行の箱より高く、BoxHeight で数えると 2 枚目が落ちる。最新を数えないと、最新が成功の
// とき警告の分を先に使う。一律に MaxBoxHeight*2 へ上げると、警告の無い 1 行の箱が 3 枚入って窓を覆う。
func toastDrawBudget(page, reserved int) int {
	return min(max(page/2, toast.BoxHeight*2, reserved), max(page-1, toast.BoxHeight))
}
