package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// status viewer (s キー) — 未コミットの変更を一覧し、stage / unstage / 変更を捨てる を行う全画面ビュー。
// 読み書きの規約 (何を読むか・何を絶対にしないか) の一次情報は docs/status-viewer-spec.md。
//
// この型は「一覧 + プレビュー + 確認」の状態機械と描画だけを持ち、browseModel の状態は知らない
// (issuesView と同じ契約)。操作結果は notice に置いて browseModel がトーストにする。
//
// ⚠️ 一覧は毎回 git status から作り直す派生ビューであり、stage の結果をローカルの配列へ書いて
// 済ませない。glogx は「別プロセス (Claude Code / $EDITOR / git pull) が同じツリーを同時に編集する」
// 環境で使うため、書き換え型は外部編集が入った瞬間に画面が嘘になる (spec 5 節)。
type statusView struct {
	shown   bool
	loaded  bool // 一度読み込みを完了したか (取り直し中に前回の結果を出してよいかの判定)
	loading bool // git status 実行中 (スピナー + 二重発行の防止)

	st   worktreeStatus
	rows []worktreeRow // st.ordered() (表示順)。カーソルはこの index を指す
	err  string        // 直近の読み取り失敗 ("" = 成功)

	cursor int
	offset int // 窓の先頭 (表示行 index。セクション見出しを含む数え方)

	// preview はプレビュー / 全画面 diff が共有する取得結果 (line_cache.go)。キーは previewKey
	// (パス + XY) なので、外部編集で内容が変わると自然にキャッシュミスになる (古い diff を
	// 出し続けない)。上限超過分の evict と取得の単発化は lineCache が持つ。
	preview lineCache
	// previewSeq はカーソル移動のデバウンス世代。キーリピート中に 1 行ごとに git を起動しない
	// ため、止まってから statusPreviewDebounce 後の tick だけが取得を発行する。
	previewSeq int

	// pager は d で開く全画面 diff ("" = 閉じている)。行は preview と同じキャッシュを読む。
	pagerKey    string
	pagerTitle  string
	pagerOffset int
	pagerGlide  scrollGlide

	// discard は X の確認 (zero value = 確認なし)。確認に出した時点の行を丸ごと持つ:
	// 実行時に git status を取り直して一致を検証するため (spec 4 節の不変条件 1)。
	discard    worktreeRow
	discarding bool

	notice   string
	noticeOK bool

	// wantIssues は「i で issues viewer へ切り替えたい」の一度きりの信号 (browseModel が
	// takeWantIssues で取り出す。閉じ→開きの連携は viewer 単体では完結しないため)。
	wantIssues bool
	// wantQuit は「q/esc で glogx ごと終了したい」の一度きりの信号 (同上。quit は browseModel の
	// 仕事で、viewer は bubbletea を知らない)。
	wantQuit bool

	// 開閉の演出 (下からせり上がる / 下へ沈む)。issuesView と同じく closing 中も shown を
	// 立てたままにして、片付けは finishClose に一本化する。
	animStart    time.Time
	closing      bool
	closeAnimOff bool // テスト用 (zero value = 演出あり。本番の既定を「演出する」に置く)

	// pollArmed / gen は自動更新チェーンの single-flight と世代 (閉じ → 開き直しで古い tick を弾く)。
	pollArmed bool
	gen       int
}

const (
	// statusOpenDuration / statusCloseDuration は開閉の所要。issues viewer の引き出し
	// (issuesDrawerDuration) と同じ値: 「板が 1 枚成長する / 縮む」演出は同じ速さに揃える
	// (行ごとに流し込む issues の 700ms とは質感が違うので合わせない)。
	statusOpenDuration  = 450 * time.Millisecond
	statusCloseDuration = 450 * time.Millisecond
	// statusPollInterval は自動更新の周期。fsnotify を張らない理由は spec 5 節
	// (作業ツリー全体の再帰 watch は対象数が読めない一方、git status はシェルの prompt が
	// 毎コマンド叩いている程度のコストしかない)。
	statusPollInterval = 1500 * time.Millisecond
	// statusPreviewDebounce はプレビュー取得のデバウンス (カーソルが止まってから取る)。
	statusPreviewDebounce = 120 * time.Millisecond
	// statusPreviewMinWidth は 2 カラムにする下限幅。これ未満は一覧だけの 1 カラムに落ちる
	// (tmux popup / 小さい pane。diff は d で見る)。
	statusPreviewMinWidth = 100
	// statusListMinWidth / statusListMaxWidth は 2 カラム時の一覧側の幅の範囲。
	statusListMinWidth = 34
	statusListMaxWidth = 60
	// statusSepWidth はカラム間の区切り ("│ ") の幅。
	statusSepWidth = 2
	// statusPreviewMaxBytes は untracked ファイルをプレビューのために読む上限バイト数。
	// 行数上限 (statusPreviewMaxLines) だけでは「改行が 1 つも無い巨大ファイル」を全部読んでしまう。
	statusPreviewMaxBytes = 64 * 1024
	// statusPreviewMaxLines はプレビュー欄に流す行数の上限 (全文は d)。窓の高さで切るので
	// 実際にはそれ以下だが、幅整形のコストを高い端末で払わないための上限。
	statusPreviewMaxLines = 200
)

// statusLoadMsg は git status 1 回分の読み取り結果。
type statusLoadMsg struct {
	st  worktreeStatus
	err error
	gen int
}

// statusPollMsg は自動更新の目覚まし (spec 5 節)。
type statusPollMsg struct{ gen int }

// statusPreviewMsg はプレビュー / 全画面 diff の取得結果。
type statusPreviewMsg struct {
	key   string
	lines []string
	err   error
}

// statusPreviewTickMsg はカーソル移動のデバウンス満了。seq が現在の世代と一致するときだけ取得する。
type statusPreviewTickMsg struct {
	seq int
	gen int
}

// statusRenderOpts は描画に必要な外側の情報 (issuesRenderOpts と同じ役割)。
//
// ⚠️ cursorPaint を受け取らない: browseModel の cursorEmphasis は行を contentWidth まで
// 塗るので、2 カラムではプレビュー側まで背景色が伸びる。カーソル強調は一覧カラムの幅で
// 完結させる必要があるため、この型が自分で塗る (statusCursorPaint)。
type statusRenderOpts struct {
	width   int
	page    int
	colored bool
	spinner string
}

// statusViewport は「今この窓は何桁 × 何行か」+ 色 (キー処理が取得を発行するのに必要)。
//
// ⚠️ colored を含むのは issuesViewport と違う点。diff の色付けは取得時に 1 回だけ行い
// (毎フレーム chroma を回さない)、取得を発行するのはキー処理なのでここに必要になる。
type statusViewport struct {
	width   int
	page    int
	colored bool
}

func newStatusView() statusView {
	return statusView{preview: newLineCache()}
}

// visible は viewer を表示中か (閉じる演出のあいだも true)。
func (v *statusView) visible() bool { return v.shown }

// loading は git status / diff の取得中か (スピナー tick を回し続ける判定用)。
func (v *statusView) fetching() bool { return v.loading || v.preview.fetching() }

// slideAnimating は開閉のスライド演出中か。tickInterval が 60fps へ上げる判定に使う
// (pager glide は含めない: あちらは他の glide と同じ 30fps で足りる)。
func (v *statusView) slideAnimating() bool {
	if v.closing {
		return true
	}
	return v.shown && !v.animStart.IsZero() && timeNow().Sub(v.animStart) < statusOpenDuration
}

// animating は開閉の演出中か。spinnerActive (tick チェーンを回すか) がこれを見る
// (issuesView.animating と同じ契約)。
func (v *statusView) animating() bool {
	if v.pagerGlide.active {
		return true // 本文 pager の glide は tick で進むので「アニメ中」に含める
	}
	return v.slideAnimating()
}

// toggle は viewer の開閉。開くときは status を読み直し、自動更新チェーンを張る。
func (v *statusView) toggle() tea.Cmd {
	if v.closing {
		v.finishClose() // 閉じる演出の途中の s は「閉じ切ってから開き直す」
	} else if v.shown {
		v.close()
		return nil
	}
	v.shown = true
	if !v.closeAnimOff {
		v.animStart = timeNow()
	}
	return tea.Batch(v.loadCmd(), v.pollCmd(), v.previewTickCmd())
}

// close は閉じる演出へ入る (演出を切っているときは即座に畳む)。
func (v *statusView) close() {
	v.closing = true
	v.animStart = timeNow()
	if v.closeAnimOff {
		v.finishClose()
	}
}

// settleClose は閉じる演出が終わっていたら畳む (描画・tick のどちらが先でも進むように、
// 判定は 1 箇所へ寄せる。issuesView.settleClose と同じ作法)。
func (v *statusView) settleClose() {
	if v.closing && timeNow().Sub(v.animStart) >= statusCloseDuration {
		v.finishClose()
	}
}

// finishClose は片付けの一本化点 (演出の着地とキーによる即着地の両方がここを通る)。閉じる演出中
// でなければ何もしない — ⚠️ この guard は必須: handleKey は毎打鍵でこれを呼ぶ (閉じ演出中のキーを
// viewer に届かせないため) ので、guard が無いと最初のキーで開いている viewer が畳まれる。
// ⚠️ gen を進めるのは、閉じる前に張った自動更新チェーンが開き直した後の状態へ効かないため。
func (v *statusView) finishClose() {
	if !v.closing {
		return
	}
	v.shown, v.closing = false, false
	v.animStart = time.Time{}
	v.pagerKey, v.pagerTitle, v.pagerOffset = "", "", 0
	v.pagerGlide.stop()
	v.discarding, v.discard = false, worktreeRow{}
	v.pollArmed = false
	// ⚠️ 走行中の取得の札を降ろす。降ろさないと閉じた瞬間に飛んでいた git status の結果は
	// 世代違いで捨てられる (receive) 一方 loading が立ったまま残り、次に開いたとき loadCmd が
	// 「取得中」と判断して二度と読み直さない = 古い一覧が永久に居座る。busy も同様に、走行中の
	// diff 取得が返らない限り fetching() が true のままフレーム tick を回し続ける。
	v.loading = false
	v.preview.clearBusy()
	v.gen++
}

// settleAnim は開く演出が終わっていたら時計を捨てる (animating の判定を軽くする)。
func (v *statusView) settleAnim() {
	if !v.closing && !v.animStart.IsZero() && timeNow().Sub(v.animStart) >= statusOpenDuration {
		v.animStart = time.Time{}
	}
}

// advanceGlide は全画面 diff のスクロール glide を 1 フレーム進める。
func (v *statusView) advanceGlide() {
	if v.pagerGlide.active {
		v.pagerGlide.advance(v.pagerOffset)
	}
}

// setNotice / takeNotice は操作結果の受け渡し (browseModel がトーストにする)。
// ⚠️ ここで無害化する: 通知文はパス・git のエラー出力を素で埋め込む呼び出しが多く、
// 呼び出しごとに包むと必ずどこかが漏れる (自前の静的文だけの通知は無害化しても変わらない)。
func (v *statusView) setNotice(text string, ok bool) {
	v.notice, v.noticeOK = sanitizePlainLine(text), ok
}

func (v *statusView) takeNotice() (string, bool) {
	text, ok := v.notice, v.noticeOK
	v.notice = ""
	return text, ok
}

// loadCmd は git status を読み直す。取得中なら nil (二重発行の防止)。
func (v *statusView) loadCmd() tea.Cmd {
	if !v.shown || v.loading {
		return nil
	}
	v.loading = true
	gen := v.gen
	return func() tea.Msg {
		st, err := loadWorktreeStatus()
		return statusLoadMsg{st: st, err: err, gen: gen}
	}
}

// pollCmd は次の自動更新を予約する (自己再アームの独立チェーン)。⚠️ フレーム tick に混ぜない:
// 混ぜると viewer を開いている間ずっと 12.5fps で起きることになり、「動くものがある間だけ tick を
// 回す」glogx の設計を崩す (issuesWatch と同じ理由)。
func (v *statusView) pollCmd() tea.Cmd {
	if !v.shown || v.pollArmed {
		return nil
	}
	v.pollArmed = true
	gen := v.gen
	return tea.Tick(statusPollInterval, func(time.Time) tea.Msg { return statusPollMsg{gen: gen} })
}

// receivePoll は目覚ましを受けて「読み直し + 次の予約」を返す。世代違い (閉じた後に届いた古い
// チェーン) は何もしない。
func (v *statusView) receivePoll(msg statusPollMsg) tea.Cmd {
	if msg.gen != v.gen || !v.shown {
		return nil
	}
	v.pollArmed = false
	return tea.Batch(v.loadCmd(), v.pollCmd())
}

// receive は git status の結果を反映する。返り値は「プレビューを取り直す予約」(内容が変わった
// ときだけ non-nil)。カーソルは「パスで復元、失われていたらセクション内の
// 同じ位置」へ落とす (spec 4 節の不変条件 2 / 5 節のカーソル規約)。
func (v *statusView) receive(msg statusLoadMsg) tea.Cmd {
	if msg.gen != v.gen {
		return nil // 閉じた後に届いた古い結果 (開き直した画面を上書きしない)
	}
	v.loading = false
	if msg.err != nil {
		v.err = firstLine(msg.err.Error())
		if !v.loaded {
			v.rows = nil
		}
		return nil
	}
	v.err = ""
	anchorSec, anchorPath, anchorIdx := v.anchor()
	first := !v.loaded
	next := msg.st.ordered()
	changed := !sameRows(v.rows, next)
	v.st = msg.st
	v.rows = next
	v.loaded = true
	if first {
		// 初回だけ Unstaged の先頭から始める (spec 2 節)。開いた直後に触りたいのは「まだ
		// stage していない変更」で、Staged の先頭に置くと毎回 Tab か j を押させることになる
		v.cursor = firstRowOfSection(v.rows, sectionUnstaged)
	} else {
		v.restoreCursor(anchorSec, anchorPath, anchorIdx)
	}
	if !changed {
		return nil
	}
	// 内容が変わったら古い diff は捨てる (キーに XY を含めているので大半は当たらないが、
	// 同じ XY のまま中身だけ変わる編集 = 保存し直しでは当たってしまう)。
	// ⚠️ 走行中の札は残す (clearEntries): 下で取り直しを予約するので、札まで降ろすと
	// 走行中の取得と予約が同じキーを二重に取りに行く。
	v.preview.clearEntries()
	// ⚠️ 捨てたら取り直しも予約する。捨てるだけだと、外部編集のたびにプレビュー欄が空になり
	// 「カーソルを動かすまで戻らない」= 別プロセスに作業させながら眺める用途で画面が死ぬ。
	return v.previewTickCmd()
}

// anchor は現在のカーソルの (セクション, パス, セクション内の位置) を返す。
func (v *statusView) anchor() (worktreeSection, string, int) {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return sectionStaged, "", 0
	}
	cur := v.rows[v.cursor]
	idx := 0
	for _, r := range v.rows[:v.cursor] {
		if r.section == cur.section {
			idx++
		}
	}
	return cur.section, cur.path, idx
}

// restoreCursor は anchor の位置へカーソルを戻す。パスが残っていればそこへ、消えていれば
// 同じセクションの同じ位置 (= stage した次のファイル) へ。セクション自体が空になったら
// index を丸めるだけにする。
func (v *statusView) restoreCursor(sec worktreeSection, path string, idx int) {
	if len(v.rows) == 0 {
		v.cursor = 0
		return
	}
	if path != "" {
		for i, r := range v.rows {
			if r.section == sec && r.path == path {
				v.cursor = i
				return
			}
		}
	}
	// セクション内の同じ位置 (無ければ末尾)
	var inSec []int
	for i, r := range v.rows {
		if r.section == sec {
			inSec = append(inSec, i)
		}
	}
	if len(inSec) > 0 {
		v.cursor = inSec[min(idx, len(inSec)-1)]
		return
	}
	v.cursor = min(v.cursor, len(v.rows)-1)
}

// firstRowOfSection は sec の先頭行の index (無ければ 0 = 全体の先頭)。
func firstRowOfSection(rows []worktreeRow, sec worktreeSection) int {
	for i, r := range rows {
		if r.section == sec {
			return i
		}
	}
	return 0
}

// sameRows は 2 つの行列が (セクション, パス, XY) まで同じか。自動更新で「何も変わっていない」
// ときにキャッシュを捨てないための判定。
func sameRows(a, b []worktreeRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].section != b[i].section || a[i].path != b[i].path || a[i].x != b[i].x || a[i].y != b[i].y {
			return false
		}
	}
	return true
}

// current はカーソル行 (行が無ければ false)。
func (v *statusView) current() (worktreeRow, bool) {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return worktreeRow{}, false
	}
	return v.rows[v.cursor], true
}

// previewKey は取得結果のキャッシュキー。XY を含めるので、内容が変わって XY が動いたら
// 自然にミスする。
func previewKey(r worktreeRow) string {
	return strconv.Itoa(int(r.section)) + "\x00" + string([]byte{r.x, r.y}) + "\x00" + r.path
}

// previewTickCmd はカーソル移動のデバウンスを張る (満了時に statusPreviewTickMsg)。
func (v *statusView) previewTickCmd() tea.Cmd {
	if !v.shown {
		return nil
	}
	v.previewSeq++
	seq, gen := v.previewSeq, v.gen
	return tea.Tick(statusPreviewDebounce, func(time.Time) tea.Msg {
		return statusPreviewTickMsg{seq: seq, gen: gen}
	})
}

// receivePreviewTick はデバウンス満了を受けて、必要ならカーソル行の diff 取得を発行する。
func (v *statusView) receivePreviewTick(msg statusPreviewTickMsg, colored bool) tea.Cmd {
	if msg.gen != v.gen || msg.seq != v.previewSeq || !v.shown {
		return nil
	}
	row, ok := v.current()
	if !ok {
		return nil
	}
	return v.fetchDiff(row, colored)
}

// fetchDiff はカーソル行の diff を取る (キャッシュヒット / 取得中なら nil)。
func (v *statusView) fetchDiff(row worktreeRow, colored bool) tea.Cmd {
	key := previewKey(row)
	if !v.preview.begin(key) {
		return nil // キャッシュ済み / 取得中
	}
	paths := row.pathspecs()
	staged := row.section == sectionStaged
	untracked := row.section == sectionUntracked
	isDir := row.isDir()
	// ⚠️ ファイルを直接読む経路だけは repo root と結合する (rows のパスは root 相対で、
	// glogx はサブディレクトリから起動されうる。git 側は pathspec の :(top) で解決している)
	filePath := row.path
	if v.st.root != "" {
		filePath = filepath.Join(v.st.root, row.path)
	}
	return func() tea.Msg {
		if untracked {
			lines, err := untrackedPreview(filePath, isDir)
			return statusPreviewMsg{key: key, lines: lines, err: err}
		}
		lines, err := loadWorktreeDiff(paths, staged, colored)
		return statusPreviewMsg{key: key, lines: lines, err: err}
	}
}

// untrackedPreview は untracked 行のプレビュー。git diff の対象外なのでファイルの中身を出す。
// ディレクトリ行 ("dir/" に畳まれたエントリ) は中身を列挙しない (畳まれている情報量のまま出す)。
func untrackedPreview(path string, isDir bool) ([]string, error) {
	if isDir {
		return []string{"(未追跡のディレクトリ)"}, nil
	}
	// ⚠️ symlink は中身を出さない: untracked のリンクを辿ると、カーソルを合わせただけで
	// リンク先 (~/.ssh/id_rsa 等) の中身が画面に出る。第三者ブランチに 1 本仕込むだけで成立する
	// ので、リンクであること自体を表示して読まない (issues の isIssueFile と同じ判断)。
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return []string{"(シンボリックリンク。リンク先の中身は表示しません)"}, nil
	}
	// ⚠️ ファイル全体を読まない: untracked には巨大な生成物 (動画・アーカイブ) が混ざりうるので、
	// カーソルを合わせただけで数百 MB を掴むことになる。プレビューに必要な先頭だけ読む。
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, statusPreviewMaxBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	body := strings.TrimRight(string(buf[:n]), "\n")
	if body == "" {
		return []string{"(空のファイル)"}, nil
	}
	lines := make([]string, 0, min(strings.Count(body, "\n")+1, statusPreviewMaxLines+1))
	for line := range strings.SplitSeq(body, "\n") {
		if len(lines) >= statusPreviewMaxLines {
			lines = append(lines, "... (これ以降は省略)")
			break
		}
		lines = append(lines, sanitizeDetailLine(line))
	}
	return lines, nil
}

// receivePreview は diff 取得の結果を反映する。失敗は「取れなかった」ことをプレビュー欄に
// 出すだけで、トーストにはしない (カーソルを動かすたびに出ると騒がしい)。
func (v *statusView) receivePreview(msg statusPreviewMsg) {
	if msg.err != nil {
		v.storePreview(msg.key, []string{"(diff を取得できませんでした: " + firstLine(msg.err.Error()) + ")"})
		return
	}
	if len(msg.lines) == 0 {
		v.storePreview(msg.key, []string{"(差分はありません)"})
		return
	}
	v.storePreview(msg.key, msg.lines)
}

// storePreview はキャッシュへ入れて上限を超えたぶんを落とす (表示中のキーは残す)。
func (v *statusView) storePreview(key string, lines []string) {
	v.preview.store(key, lines, v.visibleKey())
}

// pagerLines は全画面 diff に出す行 (未取得なら nil)。
func (v *statusView) pagerLines() []string {
	lines, _ := v.preview.get(v.pagerKey)
	return lines
}

// visibleKey は今画面に出ているプレビューのキー (evict から守る対象)。全画面 diff を開いて
// いればそちら、無ければカーソル行。
func (v *statusView) visibleKey() string {
	if v.pagerKey != "" {
		return v.pagerKey
	}
	if row, ok := v.current(); ok {
		return previewKey(row)
	}
	return ""
}

// handleKey は viewer 内のキーを捌く。返り値の tea.Cmd は取得・自動更新の予約
// (browseModel が maybeTick と束ねる)。
//
// ⚠️ 判定順を変えないこと: 確認モーダル (X) が最優先で、次に全画面 diff (pager)、最後に一覧。
// 逆にすると確認中の j が一覧を動かして「確認に出した行」と「カーソル行」が食い違う。
func (v *statusView) handleKey(key string, vp statusViewport) tea.Cmd {
	// 開く演出中のキーは即着地させる (issues viewer の finishAnim と同じ契約。spec 7 節)。
	// 閉じ演出中のキーはここへ届かない (browseModel が finishClose してから routing する)
	if !v.closing {
		v.animStart = time.Time{}
	}
	if v.discarding {
		return v.discardKey(key)
	}
	if v.pagerKey != "" {
		return v.pagerKeyPress(key, vp)
	}
	return v.listKey(key, vp)
}

// discardKey は X の確認中のキー。y/Enter で実行、それ以外はキャンセル (push/pull 確認と同じ語彙)。
func (v *statusView) discardKey(key string) tea.Cmd {
	row := v.discard
	v.discarding, v.discard = false, worktreeRow{}
	if strings.ToLower(key) != "y" && key != "enter" {
		return nil
	}
	return v.runDiscard(row)
}

// runDiscard は「確認に出した行」を捨てる。⚠️ 実行前に git status を取り直して
// (パス, XY) の一致を検証する (spec 4 節の不変条件 1): 確認中に別プロセスがそのファイルを
// 変えていたら、ユーザーが見て判断した対象はもう存在しないので捨てない。
func (v *statusView) runDiscard(row worktreeRow) tea.Cmd {
	fresh, err := loadWorktreeStatus()
	if err != nil {
		v.setNotice("git status に失敗しました: "+firstLine(err.Error()), false)
		return v.loadCmd()
	}
	now, ok := fresh.find(row.section, row.path)
	if !ok || now.x != row.x || now.y != row.y {
		v.setNotice("確認中に "+row.path+" が変わったため中止しました", false)
		v.applyFresh(fresh)
		return nil
	}
	if row.section == sectionUntracked {
		err = runGitCleanUntracked([]string{worktreePathspec(row.path)})
	} else {
		err = runGitRestoreWorktree(row.pathspecs())
	}
	if err != nil {
		v.setNotice("捨てられませんでした: "+firstLine(err.Error()), false)
		return v.loadCmd()
	}
	v.setNotice(row.path+" の変更を捨てました", true)
	return v.loadCmd()
}

// applyFresh は同期で取り直した status を画面へ反映する (loadCmd を通さない経路。
// 実行時検証で「変わっていた」と分かった直後に、古い一覧を見せ続けないため)。
func (v *statusView) applyFresh(st worktreeStatus) {
	sec, path, idx := v.anchor()
	v.st, v.rows, v.loaded, v.err = st, st.ordered(), true, ""
	v.restoreCursor(sec, path, idx)
	v.preview.clearEntries() // 走行中の取得は走行中のまま (clearEntries の doc)
}

// pagerKeyPress は全画面 diff のキー (スクロールは共有ロジック、閉じるはここ)。
func (v *statusView) pagerKeyPress(key string, vp statusViewport) tea.Cmd {
	switch key {
	case "q", "esc", "h", "left", "d", "enter":
		v.pagerKey, v.pagerTitle, v.pagerOffset = "", "", 0
		v.pagerGlide.stop()
		return nil
	}
	total := len(v.pagerLines())
	v.pagerOffset = pagerScrollKey(key, v.pagerOffset, v.pagerRows(vp.page), total, &v.pagerGlide)
	return nil
}

// listKey は一覧のキー。
func (v *statusView) listKey(key string, vp statusViewport) tea.Cmd {
	rows := max(vp.page-1, 1)
	switch key {
	case "j", "down", "ctrl+n":
		return v.moveCursor(1)
	case "k", "up", "ctrl+p":
		return v.moveCursor(-1)
	case "ctrl+d", "pgdown":
		return v.moveCursor(rows / 2)
	case "ctrl+u", "pgup":
		return v.moveCursor(-rows / 2)
	case "g", "home":
		return v.setCursor(0)
	case "G", "end":
		return v.setCursor(len(v.rows) - 1)
	case "tab":
		return v.jumpSection()
	case " ":
		return v.toggleStage()
	case "a":
		return v.stageAll()
	case "X":
		v.askDiscard()
		return nil
	case "d", "enter", "l", "right":
		return v.openPager(vp)
	case "r":
		return v.loadCmd()
	case "q", "esc":
		// viewer からの q/esc は glogx ごと終了する (ユーザー要望 2026-08-06: git log 一覧へは
		// 戻らない)。実際の quit は browseModel が行う (takeWantQuit)。一覧へ戻りたいときは
		// s (toggle) を使う
		v.wantQuit = true
		return nil
	case "s":
		// ⚠️ 閉じるキーはこの型が持つ (browseModel 側で拾わない): 全画面ビューのキーは
		// すべて handleKey を通る契約なので、ここに無いと「押しても閉じない」になる。
		// s が閉じるのは toggle の語彙 (i = issues viewer と同じ)。
		v.close()
		return nil
	case "i":
		// issues viewer への横断 (ユーザー要望 2026-08-06)。閉じてから開くのは browseModel の
		// 仕事 (takeWantIssues)。確認中 (X) や pager 中はこの switch まで届かないので誤爆しない
		v.close()
		v.wantIssues = true
		return nil
	}
	return nil
}

// takeWantIssues は「i で issues viewer へ切り替えたい」を一度だけ取り出す (takeNotice と同じ語彙)。
func (v *statusView) takeWantIssues() bool {
	want := v.wantIssues
	v.wantIssues = false
	return want
}

// takeWantQuit は「q/esc で glogx ごと終了したい」を一度だけ取り出す (takeNotice と同じ語彙)。
func (v *statusView) takeWantQuit() bool {
	want := v.wantQuit
	v.wantQuit = false
	return want
}

// moveCursor / setCursor はカーソル移動 (移動後はプレビューのデバウンスを張り直す)。
func (v *statusView) moveCursor(delta int) tea.Cmd {
	return v.setCursor(v.cursor + delta)
}

func (v *statusView) setCursor(i int) tea.Cmd {
	if len(v.rows) == 0 {
		return nil
	}
	next := max(min(i, len(v.rows)-1), 0)
	if next == v.cursor {
		return nil
	}
	v.cursor = next
	return v.previewTickCmd()
}

// jumpSection は次の (行のある) セクションの先頭へ飛ぶ。末尾のセクションからは先頭へ回る。
func (v *statusView) jumpSection() tea.Cmd {
	cur, ok := v.current()
	if !ok {
		return nil
	}
	for i := 1; i <= len(v.rows); i++ {
		idx := (v.cursor + i) % len(v.rows)
		if v.rows[idx].section != cur.section {
			return v.setCursor(idx)
		}
	}
	return nil
}

// toggleStage はカーソル行を「セクションを移す」(spec 3 節)。向きはセクションが決めるので
// キーは 1 つで足りる。
func (v *statusView) toggleStage() tea.Cmd {
	row, ok := v.current()
	if !ok {
		return nil
	}
	if !row.section.mutable() {
		v.setNotice("conflict の解決はシェルで行ってください", false)
		return nil
	}
	var err error
	if row.section == sectionStaged {
		err = runGitRestoreStaged(row.pathspecs())
	} else {
		err = runGitAdd(row.pathspecs())
	}
	if err != nil {
		v.setNotice("失敗しました: "+firstLine(err.Error()), false)
	}
	return v.loadCmd()
}

// stageAll は Unstaged + Untracked をまとめて stage する。
func (v *statusView) stageAll() tea.Cmd {
	var paths []string
	seen := map[string]bool{}
	for _, r := range v.rows {
		if r.section != sectionUnstaged && r.section != sectionUntracked {
			continue
		}
		for _, p := range r.pathspecs() {
			if !seen[p] {
				seen[p], paths = true, append(paths, p)
			}
		}
	}
	if len(paths) == 0 {
		v.setNotice("stage するものがありません", true)
		return nil
	}
	if err := runGitAdd(paths); err != nil {
		v.setNotice("stage できませんでした: "+firstLine(err.Error()), false)
		return v.loadCmd()
	}
	v.setNotice(strconv.Itoa(len(paths))+" 件を stage しました", true)
	return v.loadCmd()
}

// askDiscard は X の確認へ入る。
//
// ⚠️ Staged 行では受けない: 「staged の変更を捨てる」は unstage + 作業ツリーの破棄という
// 二段の破壊で、同じキー・同じ文言では意味が変わる。先に Space で降ろさせて、X の意味を
// 「作業ツリーの変更を捨てる」1 つに保つ (spec 3/4 節)。
func (v *statusView) askDiscard() {
	row, ok := v.current()
	if !ok {
		return
	}
	switch {
	case !row.section.mutable():
		v.setNotice("conflict の解決はシェルで行ってください", false)
	case row.section == sectionStaged:
		v.setNotice("staged の変更は Space で unstage してから X してください", false)
	default:
		v.discarding, v.discard = true, row
	}
}

// openPager は全画面 diff を開く (未取得なら取得を発行してスピナーを出す)。
func (v *statusView) openPager(vp statusViewport) tea.Cmd {
	row, ok := v.current()
	if !ok {
		return nil
	}
	v.pagerKey = previewKey(row)
	v.pagerTitle = row.dispPath() // 画面に出す文字列なので表示用 (dispPath の doc)
	v.pagerOffset = 0
	v.pagerGlide.stop()
	return v.fetchDiff(row, vp.colored)
}

// hint は hint 行 (browseModel が枠の下に出す)。
func (v *statusView) hint() string {
	switch {
	case v.discarding:
		return "y/Enter: 捨てる  n/Esc: キャンセル"
	case v.pagerKey != "":
		return "j/k: スクロール  Space/C-d: 半ページ  g/G: 先頭/末尾  d/q: 閉じる"
	default:
		return "j/k: 移動  Tab: セクション  Space: stage/unstage  a: 全 stage  X: 変更を捨てる  d: diff  r: 再読込  U: usage  q: 閉じる"
	}
}

// ---- 描画 ----

// lines は全画面ビューの page 行を返す (issuesView.lines と同じ契約: 常にちょうど page 行)。
func (v *statusView) lines(o statusRenderOpts) []string {
	v.settleClose()
	v.settleAnim()
	// ヘッダー (ブランチ + 件数) は 2 カラムに割らず**全幅**で描く。一覧カラムの中に入れると
	// 幅 48 桁ほどで件数が切り落とされる (実測 2026-08-03: "unstaged 15 / stage…")。
	head := []string{v.headerLine(o, o.width)}
	rows := max(o.page-len(head), 1)
	inner := o
	inner.page = rows
	listW := statusListWidth(o.width)
	var body []string
	if listW >= o.width {
		body = padTo(v.listLines(inner, o.width), rows)
	} else {
		left := padTo(v.listLines(inner, listW), rows)
		right := padTo(v.previewPane(inner, o.width-listW-statusSepWidth), rows)
		body = make([]string, 0, rows)
		sep := paint("│ ", ansiDim, o.colored)
		for i := range rows {
			body = append(body, fillRight(left[i], listW)+sep+right[i])
		}
	}
	body = append(head, body...)
	// ⚠️ 契約 (ちょうど page 行) をここで必ず満たす。ヘッダー 1 行 + 本文の合成は page が
	// 極端に小さいとき (0 / 1 行の窓) に page を超える
	body = padTo(body, o.page)
	if box := v.pagerBox(o); len(box) > 0 {
		body = overlayCenteredBox(body, box, o.width, o.page, o.colored)
	}
	if box := v.discardBox(o); len(box) > 0 {
		body = overlayCenteredBox(body, box, o.width, o.page, o.colored)
	}
	if p := v.animProgress(); p < 1 {
		body = slideLeftWindow(body, p, o.width, v.closing)
	}
	return body
}

// statusListWidth は一覧カラムの幅 (>= o.width なら 1 カラム表示)。
func statusListWidth(total int) int {
	if total < statusPreviewMinWidth {
		return total
	}
	return max(min(total*4/10, statusListMaxWidth), statusListMinWidth)
}

// animProgress は演出の進み (0..1。演出していないときは 1 = 変形しない)。
func (v *statusView) animProgress() float64 {
	if v.closing {
		return max(1-float64(timeNow().Sub(v.animStart))/float64(statusCloseDuration), 0)
	}
	if v.animStart.IsZero() {
		return 1
	}
	return min(float64(timeNow().Sub(v.animStart))/float64(statusOpenDuration), 1)
}

// slideLeftWindow は窓を「板が左端から生えてくる」途中の姿にする (closing なら左へ縮んで消える
// 途中)。全行同時に幅が伸び、方向でどちらの画面が出たか判別できる (issues = 右から、status = 左から)。
// 行ごとの stagger を入れない: 斜めのウェーブに見えて板の成長に見えない (ユーザー選定 2026-08-06。
// 開きは easeOutCubic で着地を減速、閉じは等速で最後まで縮み切る)。
//
// 当初の「下からせり上がる」縦スライドをやめたのは解像度の問題: 縦は行数 (~40 ステップ) しか
// なく 60fps でも 1 コマの移動が粗い。横は桁数 (数百ステップ) で滑らか (ユーザー要望 2026-08-06)。
// 行が左から滑り込む真の平行移動 (truncateDispLeft) にしないのも意図的: 頭が画面外に出て
// 尻尾の桁から現れる見た目になる。左端アンカーの成長なら行頭から読める形で現れる。
func slideLeftWindow(window []string, progress float64, width int, closing bool) []string {
	ratio := 1 - easeOutCubicFloat(progress)
	if closing {
		ratio = 1 - progress
	}
	if ratio <= 0 {
		return window // 全桁出た
	}
	cols := width - int(math.Round(ratio*float64(width)))
	out := make([]string, 0, len(window))
	for _, ln := range window {
		if cols <= 0 || ln == "" {
			out = append(out, "") // まだ 1 桁も出ていない (または元から空行)
			continue
		}
		// clipToWidth でなく tail 無しの truncateDisp: 動く右端に「…」を走らせない
		out = append(out, truncateDisp(ln, cols, ""))
	}
	return out
}

// listLines は一覧カラム (セクション見出し + 行) を組む。ヘッダーは lines が全幅で描くので
// ここには含めない。
func (v *statusView) listLines(o statusRenderOpts, width int) []string {
	if msg := v.emptyMessage(o); msg != "" {
		return []string{paint(clipToWidth(msg, width), ansiDim, o.colored)}
	}
	rows := max(o.page, 1)
	// ⚠️ 整形するのは窓の中だけ (displayIndex の doc)。全行を整形してから切ると、画面に出る
	// 行数と無関係に変更ファイル数へ比例したコストになる。
	index, cursorAt := v.displayIndex()
	v.offset = windowOffsetFor(v.offset, cursorAt, len(index), rows)
	end := min(v.offset+rows, len(index))
	out := make([]string, 0, rows)
	for _, dl := range index[v.offset:end] {
		out = append(out, v.displayLine(dl, o, width-scrollbarColumnWidth))
	}
	return scrollbarColumn(out, width, len(index), v.offset, o.colored)
}

// headerLine は最上段 (ブランチ + 件数)。
func (v *statusView) headerLine(o statusRenderOpts, width int) string {
	left := "status"
	if v.st.branch != "" {
		left += " ── " + v.st.branch
	}
	if v.st.track != "" {
		left += " (" + v.st.track + ")"
	}
	staged, unstaged, untracked, conflicted := v.st.counts()
	right := fmt.Sprintf("unstaged %d / staged %d", unstaged+untracked, staged)
	if conflicted > 0 {
		right = fmt.Sprintf("conflict %d / ", conflicted) + right
	}
	if v.err != "" {
		// last-good の一覧を出しているあいだ、「今の表示は古いかもしれない」ことを示す
		// (emptyMessage は loaded 後は失敗を出さないので、ここが唯一の手がかりになる)
		right = "⚠ status 取得失敗 / " + right
	}
	if v.st.skipped > 0 {
		// 一部だけ解釈できなかったケース (行はあるが取りこぼしている = 一覧が不完全)
		right = fmt.Sprintf("⚠ %d 件解釈不能 / ", v.st.skipped) + right
	}
	pad := max(width-dispWidth(left)-dispWidth(right), 1)
	return paint(clipToWidth(left+padSpaces(pad)+right, width), ansiBold, o.colored)
}

// emptyMessage は「一覧に出すものが無い」状態の案内 ("" = 行を描く)。
func (v *statusView) emptyMessage(o statusRenderOpts) string {
	switch {
	case v.err != "" && !v.loaded:
		// ⚠️ 読めた履歴があるときは一覧を消さない (last-good を維持する。usage overlay /
		// issues viewer と同じ規律)。自動更新は 1.5 秒ごとに走るので、一時的な失敗で一覧が
		// 消えると「操作しようとした瞬間だけ画面が空になる」ことになる。失敗はヘッダーに出す
		return "git status に失敗しました: " + v.err
	case v.loading && !v.loaded:
		return o.spinner + " git status を読んでいます..."
	case v.st.clean() && v.st.skipped > 0:
		// ⚠️ 「読めなかった」を「クリーン」と同じ絵にしない (沈黙を成功にしない)。git の出力形式が
		// 想定と違うとき、変更を見せるための画面が「変更なし」と嘘をつくことになる
		return fmt.Sprintf("git status の出力を解釈できませんでした (%d レコード)", v.st.skipped)
	case v.st.clean():
		// クリーンでも画面は閉じない (自動更新があるのでライブモニタとして置ける。spec 6 節)
		return "作業ツリーはクリーンです (別プロセスの編集を検知したら自動で表示します)"
	default:
		return ""
	}
}

// displayLines はセクション見出しを挟んだ表示行と、カーソル行の表示 index を返す。
// statusDisplayLine は一覧の 1 行が「何を描くか」だけを持つ (文字列にはしない)。
// row < 0 = セクション見出し (件数は n)。
type statusDisplayLine struct {
	sec worktreeSection
	row int
	n   int // 見出しの件数 (row < 0 のときだけ意味を持つ)
}

// displayIndex は一覧の行構成 (見出し + 行) と、カーソルが何行目かを返す。
//
// ⚠️ ここで文字列を作らない。整形 (rowLine → パスの切り詰め → 幅計算) は可視の窓の分だけに
// 掛ける (listLines)。以前は全行を整形してから窓で切っていたため、画面に 40 行しか出ないのに
// 変更ファイル数に比例して働いていた: 実測で 40 件 103µs / 2000 件 1.65ms (16 倍・627KB/frame)。
// 大きな merge や大量の untracked を抱えた repo で status viewer を開くと、見えない行のために
// 毎フレーム捨てる文字列を作り続けることになる。
//
// 行の数え上げ自体は件数に比例したままだが、これは int の比較だけで文字列も幅計算も伴わない
// (窓の位置を決めるには全体の行数とカーソルの行番号が要るので、ここは削れない)。
func (v *statusView) displayIndex() (index []statusDisplayLine, cursorAt int) {
	index = make([]statusDisplayLine, 0, len(v.rows)+4)
	for _, sec := range []worktreeSection{sectionStaged, sectionUnstaged, sectionUntracked, sectionConflicted} {
		n := 0
		for _, r := range v.rows {
			if r.section == sec {
				n++
			}
		}
		if n == 0 {
			continue
		}
		index = append(index, statusDisplayLine{sec: sec, row: -1, n: n})
		for i, r := range v.rows {
			if r.section != sec {
				continue
			}
			if i == v.cursor {
				cursorAt = len(index)
			}
			index = append(index, statusDisplayLine{sec: sec, row: i})
		}
	}
	return index, cursorAt
}

// displayLine は index の 1 行を実際の文字列へ整形する (可視の窓の分だけ呼ぶ)。
func (v *statusView) displayLine(dl statusDisplayLine, o statusRenderOpts, width int) string {
	if dl.row < 0 {
		label := fmt.Sprintf("%s (%d)", dl.sec.label(), dl.n)
		return paint(clipToWidth(label, width), sectionColor(dl.sec)+ansiBold, o.colored)
	}
	return v.rowLine(dl.row, o, width)
}

// sectionColor はセクション (と行) の色。git 標準の語彙に寄せる: staged = 緑 / unstaged = 赤 /
// untracked = dim / conflict = 黄。
func sectionColor(sec worktreeSection) string {
	switch sec {
	case sectionStaged:
		return ansiGreen
	case sectionUnstaged:
		return ansiRed
	case sectionUntracked:
		return ansiDim
	default:
		return ansiYellow
	}
}

// rowLine は 1 行 (溝 + コード + パス + ◐)。
func (v *statusView) rowLine(i int, o statusRenderOpts, width int) string {
	r := v.rows[i]
	code := paint(string(r.code), sectionColor(r.section), o.colored)
	badge := ""
	if r.partial {
		badge = " ◐" // index 側と作業ツリー側の両方に変更がある = 一部だけ staged
	}
	pathW := max(width-cursorGutterWidth-2-dispWidth(badge), 4)
	text := code + " " + statusPathText(r, pathW, o.colored) + badge
	if i != v.cursor {
		return clipToWidth(cursorGutterBlank+text, width)
	}
	return statusCursorPaint(clipToWidth(cursorGutterMark+text, width), width, o.colored)
}

// statusPathText はパスを「ディレクトリ部分を dim、basename を明るく」描く (幅を超える場合は
// 先頭を削って basename を残す: 末尾から切ると「どのファイルか」が分からなくなる)。
func statusPathText(r worktreeRow, width int, colored bool) string {
	path := r.dispPath()
	if dispWidth(path) > width {
		// ⚠️ 先頭を削る (末尾を残す)。末尾から切ると basename が消えて「どのファイルか」が
		// 分からなくなる = 一覧として役に立たなくなる
		return paint(truncateDispLeft(path, width, "…"), ansiDim, colored)
	}
	dir, base := "", path
	if i := strings.LastIndex(strings.TrimSuffix(path, "/"), "/"); i >= 0 {
		dir, base = path[:i+1], path[i+1:]
	}
	if dir == "" {
		return base
	}
	return paint(dir, ansiDim, colored) + base
}

// statusCursorPaint はカーソル行の強調。⚠️ 塗る幅は「一覧カラムの幅」で、画面幅ではない
// (browseModel の cursorEmphasis は contentWidth まで塗るのでプレビュー側まで背景が伸びる)。
func statusCursorPaint(text string, width int, colored bool) string {
	if !colored {
		return text
	}
	pad := max(width-dispWidth(text), 0)
	return ansiCursorBg + ansiResetRe.ReplaceAllString(text, "$0"+ansiCursorBg) +
		padSpaces(pad) + ansiReset
}

// previewPane はプレビューカラム (カーソル行の diff の先頭部分)。スクロールは持たない
// (全文は d。spec 6 節)。
func (v *statusView) previewPane(o statusRenderOpts, width int) []string {
	row, ok := v.current()
	if !ok || width <= 0 {
		return nil
	}
	kind := map[worktreeSection]string{
		sectionStaged: "staged", sectionUnstaged: "unstaged",
		sectionUntracked: "untracked", sectionConflicted: "conflict",
	}[row.section]
	head := clipToWidth(row.dispPath()+"  ("+kind+")", width)
	out := []string{paint(head, ansiBold, o.colored), ""}
	key := previewKey(row)
	body, ok := v.preview.get(key)
	switch {
	case !ok && v.preview.loading(key):
		out = append(out, paint(o.spinner+" diff を取得中...", ansiDim, o.colored))
	case !ok:
		out = append(out, paint("(d で全文)", ansiDim, o.colored))
	default:
		rows := max(o.page-len(out), 1)
		body = body[previewSkipDiffHeader(body):]
		for i, line := range body {
			if i >= rows {
				out = append(out, paint("… 続きは d", ansiDim, o.colored))
				break
			}
			out = append(out, clipToWidth(line, width))
		}
	}
	return out
}

// previewSkipDiffHeader は diff の**ファイルヘッダー** (diff --git / index / --- / +++ や
// "new file mode" 等の拡張ヘッダー) を飛ばして、最初の hunk (@@) の位置を返す。
//
// プレビュー欄は 10 行前後しかないので、ヘッダー 4〜6 行に埋まると「変更が 1 行も見えない」
// (実測 2026-08-03: 13 行の枠でヘッダーが 4 行)。全画面 diff (d) では飛ばさない — あちらは
// 全文を読む場なので、ファイル名や mode 変更も情報として要る。
//
// @@ が無い diff (mode 変更だけ・binary files differ) は飛ばさない (飛ばすと空になる)。
func previewSkipDiffHeader(lines []string) int {
	const scan = 12 // 拡張ヘッダーを含めてもこの範囲に収まる (超えるものは飛ばさない)
	for i, line := range lines {
		if i >= scan {
			break
		}
		if strings.HasPrefix(stripANSI(line), "@@") {
			return i
		}
	}
	return 0
}

// pagerBox は全画面 diff (d) の枠付き本文。閉じているときは nil。
func (v *statusView) pagerBox(o statusRenderOpts) []string {
	if v.pagerKey == "" {
		return nil
	}
	// ⚠️ diffOverlay.boxLines のような width<=0 の下限ガードは置かない: 幅 0 の窓へ重ねる
	// overlayCenteredBox 自体が何もせず返すため観測できず、テストで壊せない防御になる
	// (ミューテーション検証 2026-08-03: ガードを外しても TestStatusLinesSurvivesExtremeSizes は green)。
	width := o.width
	rows := v.pagerRows(o.page)
	lines, ok := v.preview.get(v.pagerKey)
	var body []string
	title := " diff: " + v.pagerTitle + " "
	switch {
	case !ok && v.preview.loading(v.pagerKey):
		body = []string{paint(o.spinner+" diff を取得中...", ansiDim, o.colored)}
	case !ok:
		body = []string{paint("(diff はありません)", ansiDim, o.colored)}
	default:
		v.pagerOffset = clampScrollOffset(v.pagerOffset, len(lines), rows)
		start := clampScrollOffset(v.pagerGlide.offset(v.pagerOffset), len(lines), rows)
		end := min(start+rows, len(lines))
		body = append(body, lines[start:end]...)
		title = fmt.Sprintf(" diff: %s [%d-%d/%d] ", v.pagerTitle, start+1, end, len(lines))
		body = withScrollbar(body, width, len(lines), start, o.colored)
	}
	return buildShadowPanelBox(title, body, width, o.colored, ansiDim)
}

// pagerRows は全画面 diff の本文行数 (visibleDiffRows と同じ内訳: 枠 2 + 影 1 + 余白 1 + hint 1)。
func (v *statusView) pagerRows(page int) int { return max(page-5, 3) }

// discardBox は X の確認モーダル。⚠️ untracked は restore ではなく削除なので文言を変える
// (同じ見た目のキーで意味が変わるものを同じ文言で確認しない。spec 4 節)。
func (v *statusView) discardBox(o statusRenderOpts) []string {
	if !v.discarding {
		return nil
	}
	head := "次のファイルの変更を捨てます (復元できません)"
	switch {
	case v.discard.isDir():
		head = "次のディレクトリを中身ごと削除します (復元できません)"
	case v.discard.section == sectionUntracked:
		head = "次のファイルを削除します (復元できません)"
	}
	rows := []string{
		head,
		"  " + string(v.discard.code) + " " + v.discard.path,
		"",
		paint("y/Enter: 実行   n/Esc: キャンセル", ansiDim, o.colored),
	}
	return centerBox(" 変更を捨てる ", rows, o.width, o.colored)
}
