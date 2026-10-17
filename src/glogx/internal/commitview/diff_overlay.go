package commitview

import (
	"fmt"
	"glogx/internal/gitdata"
	"glogx/internal/linecache"
	"glogx/internal/term"
	"strings"

	"github.com/jiikko/dotfiles/src/tuikit/listnav"
)

// diffOverlay はコミット diff (d キー) を最前面に重ねる pager 型オーバーレイの状態と描画。
// usageOverlay と同じ方針で browseModel から diff の関心事 (状態 + open/scroll/receive/render)
// を 1 つの型へ切り出したサブコンポーネント。取得の非同期 (loadCommitDiff) とターゲット選定
// (cursor / panelSHA)・パネル閉じ・URL コピーは境界をまたぐため browseModel 側に薄く残し、
// この型は「どの SHA を、どの位置まで、どう描くか」という pager の内部状態機械だけを持つ。
type DiffOverlay struct {
	sha string // 表示中の SHA ("" = 非表示)
	// pager はスクロール位置 (論理 = 着地点) と、表示位置をそこへ滑らせる glide (internal/term/scroll_glide.go の
	// term.PagerScrollKey を通す。一覧と同じ手触りにする。ユーザー要望 2026-07-31)。
	pager listnav.Pager
	// lines は sha → 整形済み diff 行のキャッシュと取得の単発化 (line_cache.go)。
	cache linecache.Cache
}

// newDiffOverlay は map を初期化した diffOverlay を返す。
func NewDiffOverlay() DiffOverlay {
	return DiffOverlay{cache: linecache.New()}
}

// visible は diff ポップアップを表示中か。
func (o *DiffOverlay) Visible() bool { return o.sha != "" }

// fetching は diff 取得中の SHA が 1 つでもあるか (スピナー tick を回し続ける判定用)。
func (o *DiffOverlay) Fetching() bool { return o.cache.Fetching() }

// close はポップアップを閉じてスクロール位置を戻す。
func (o *DiffOverlay) Close() {
	o.sha = ""
	o.pager.Reset() // 閉じるときに glide を残すと、次に開いた瞬間だけ古い位置から滑る
}

// reset は pull 後の全面リロードでキャッシュごと破棄する (旧 SHA の残骸を持ち越さない)。
func (o *DiffOverlay) Reset() {
	o.cache.Reset()
	o.Close()
}

// open は sha の diff を開く。同じ SHA を再度開こうとしたら閉じる (toggle)。取得が必要
// (キャッシュ未ヒットかつ未取得) なら busy を立てて true を返す。呼び出し側はその場合だけ
// loadCommitDiff の非同期コマンドを発行する。
func (o *DiffOverlay) Open(sha string) (needFetch bool) {
	if o.sha == sha {
		o.Close()
		return false
	}
	o.sha = sha
	o.pager.Reset() // 開いたまま別 SHA へ差し替える経路 (J/K) で、前の半ページ送りの滑りを持ち越さない
	return o.cache.Begin(sha)
}

// receive は取得結果 (diffMsg) を反映する。取得失敗は err を返し (呼び出し側が notice を出す)、
// その SHA が今表示中なら閉じる。古い別 SHA のエラーは表示中の diff を閉じない。
func (o *DiffOverlay) Receive(msg DiffMsg) error {
	if msg.err != nil {
		o.cache.Abort(msg.sha)
		if o.sha == msg.sha {
			o.Close()
		}
		return msg.err
	}
	o.cache.Store(msg.sha, msg.lines, o.sha)
	return nil
}

// visibleLines は表示中 SHA のキャッシュ行 (未取得なら nil)。
func (o *DiffOverlay) visibleLines() []string {
	lines, _ := o.cache.Get(o.sha)
	return lines
}

// scroll は pager 流儀のキー操作を反映する。rows は表示可能行数 (レイアウト依存なので
// 呼び出し側が算出して渡す)。閉じる系キー (q/esc/h/left/d) はここで閉じる。末尾に達したら
// 最終行を表示したまま止まる (自動で閉じない)。🚨 y (URL コピー) は境界をまたぐため
// 呼び出し側が handleDiffKey で処理し、ここには渡さない。
func (o *DiffOverlay) Scroll(key string, rows int) {
	switch key {
	case "q", "esc", "h", "left", "d":
		o.Close()
		return
	}
	// スクロールの語彙 (1 行 / 半ページ + glide / 端ジャンプ) は status viewer の全画面 diff と
	// 共有する (internal/term/scroll_glide.go の term.PagerScrollKey)。手触りを 1 箇所に集約するため。
	term.PagerScrollKey(key, &o.pager, rows, len(o.visibleLines()))
}

// boxLines は diff ポップアップの描画行 (枠付き)。非表示・コミット解決不能なら nil。
// commit は呼び出し側が SHA から解決して渡す (この型はコミット列を知らない)。rows は本文の
// 表示行数。spinner / width / colored は browseModel 側の状態を受け取る (usageOverlay と同様)。
func (o *DiffOverlay) BoxLines(width int, colored bool, spinner string, commit *gitdata.Commit, rows int) []string {
	if o.sha == "" || commit == nil {
		return nil
	}
	if width <= 0 {
		width = 80
	}
	var body []string
	title := fmt.Sprintf(" diff: %s %s ", commit.ShortSHA, commit.Subject)
	switch {
	case o.cache.Loading(o.sha):
		body = []string{term.Paint(spinner+" diff を取得中...", term.AnsiDim, colored)}
	default:
		lines := o.visibleLines()
		if len(lines) == 0 {
			body = []string{term.Paint("(diff はありません)", term.AnsiDim, colored)}
			break
		}
		// 描画で確定した行数・窓で論理 offset を収束させる (理由は listnav.Pager.Clamp の doc)
		o.pager.Clamp(len(lines), rows)
		start := o.pager.DrawOffset(len(lines), rows)
		end := min(start+rows, len(lines))
		body = append(body, lines[start:end]...)
		title = fmt.Sprintf(" diff: %s [%d-%d/%d] %s ", commit.ShortSHA, start+1, end, len(lines), commit.Subject)
		// j/k スクロール中の現在位置を視覚化する (term.WithScrollbar が影付き枠の本文幅を補正する)
		body = term.WithScrollbar(body, width, len(lines), start, colored)
	}
	return term.BuildShadowPanelBox(title, body, width, colored)
}

// animating は演出の途中か (tick チェーンを回すか の判定に使う。issuesView.animating /
// statusView.animating と同じ契約)。diff は本文 pager の glide だけがアニメ源。
func (o *DiffOverlay) Animating() bool { return o.pager.Animating() }

// advanceGlide はスクロール glide を 1 フレーム進める (browseModel の tick から呼ばれる)。
func (o *DiffOverlay) AdvanceGlide() {
	o.pager.Advance()
}

// stopGlides は進行中の滑走を全部止める (advanceGlide と対。resize で着地点が古くなったとき)。
func (o *DiffOverlay) StopGlides() {
	o.pager.Stop()
}

// jobDetailMsg は job 詳細 (annotations / ログ tail) のオンデマンド取得の結果。
type JobDetailMsg struct {
	key   string // sha/jobIdx (取得中表示とキャッシュのキー)
	lines []string
	ghErr *gitdata.GHError
}

// diffMsg はコミット diff のオンデマンド取得の結果 (d キー)。
type DiffMsg struct {
	sha   string
	lines []string
	err   error
}

// prStateColor は PR 状態の表示色 (GitHub の慣例: OPEN=緑 / MERGED=マゼンタ / CLOSED=赤)。
// コミット行のバッジ (prBadge) と PR 状態ポップアップ (prStateLabel) が共有する — 独立に
// 2 箇所へ書くと状態語彙の変更で片方だけ直して食い違う (issue 030)。
func PrStateColor(state string) string {
	switch state {
	case "OPEN":
		return term.AnsiGreen
	case "MERGED":
		return term.AnsiMagenta
	case "CLOSED":
		return term.AnsiRed
	}
	return term.AnsiDim
}

// decorateDetailLine は job 詳細の GitHub Actions マーカー行 (##[error] 等) と
// annotations のレベル見出し ([failure] 等) を Web UI 風に着色する。raw ログに
// これらの色情報は無い (Web の配色は UI 側の後付け) ため glog 側で再現する。
// キャッシュには焼き込まず描画時に付ける (colored 判定と将来の色変更を尊重)。
func DecorateDetailLine(line string, colored bool) string {
	if !colored {
		return line
	}
	switch {
	case strings.HasPrefix(line, "##[error]"), strings.HasPrefix(line, "[failure]"):
		return term.AnsiRed + line + term.AnsiReset
	case strings.HasPrefix(line, "##[warning]"), strings.HasPrefix(line, "[warning]"):
		return term.AnsiYellow + line + term.AnsiReset
	case strings.HasPrefix(line, "##[notice]"), strings.HasPrefix(line, "[notice]"):
		return term.AnsiCyan + line + term.AnsiReset
	case strings.HasPrefix(line, "##[group]"), strings.HasPrefix(line, "##[endgroup]"),
		strings.HasPrefix(line, "##[command]"), strings.HasPrefix(line, "##[debug]"):
		return term.Paint(line, term.AnsiDim, true)
	}
	return line
}

// SHA は表示中の SHA ("" = 非表示)。
func (o *DiffOverlay) SHA() string { return o.sha }

// NewDiffMsg は差分の取得の結果の Msg (tui.go の取得の Cmd が組む)。
func NewDiffMsg(sha string, lines []string, err error) DiffMsg {
	return DiffMsg{sha: sha, lines: lines, err: err}
}

// SHA は取得した差分の SHA。
func (m DiffMsg) SHA() string { return m.sha }

// NewJobDetailMsg はジョブのログの取得の結果の Msg (tui.go の取得の Cmd が組む)。
func NewJobDetailMsg(key string, lines []string, ghErr *gitdata.GHError) JobDetailMsg {
	return JobDetailMsg{key: key, lines: lines, ghErr: ghErr}
}

// Key / Lines / Err は取得したジョブのログのキー (sha/jobIdx)・行・取得の失敗。
func (m JobDetailMsg) Key() string           { return m.key }
func (m JobDetailMsg) Lines() []string       { return m.lines }
func (m JobDetailMsg) Err() *gitdata.GHError { return m.ghErr }
