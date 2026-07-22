package main

import "fmt"

// diffOverlay はコミット diff (d キー) を最前面に重ねる pager 型オーバーレイの状態と描画。
// usageOverlay と同じ方針で browseModel から diff の関心事 (状態 + open/scroll/receive/render)
// を 1 つの型へ切り出したサブコンポーネント。取得の非同期 (loadCommitDiff) とターゲット選定
// (cursor / panelSHA)・パネル閉じ・URL コピーは境界をまたぐため browseModel 側に薄く残し、
// この型は「どの SHA を、どの位置まで、どう描くか」という pager の内部状態機械だけを持つ。
type diffOverlay struct {
	sha    string              // 表示中の SHA ("" = 非表示)
	offset int                 // スクロール位置 (行)
	cache  map[string][]string // sha → 整形済み diff 行 (メモリ内キャッシュ)
	busy   map[string]bool     // 取得中の sha
}

// newDiffOverlay は map を初期化した diffOverlay を返す。
func newDiffOverlay() diffOverlay {
	return diffOverlay{cache: map[string][]string{}, busy: map[string]bool{}}
}

// visible は diff ポップアップを表示中か。
func (o *diffOverlay) visible() bool { return o.sha != "" }

// fetching は diff 取得中の SHA が 1 つでもあるか (スピナー tick を回し続ける判定用)。
func (o *diffOverlay) fetching() bool { return len(o.busy) > 0 }

// close はポップアップを閉じてスクロール位置を戻す。
func (o *diffOverlay) close() {
	o.sha = ""
	o.offset = 0
}

// reset は pull 後の全面リロードでキャッシュごと破棄する (旧 SHA の残骸を持ち越さない)。
func (o *diffOverlay) reset() {
	o.cache = map[string][]string{}
	o.busy = map[string]bool{}
	o.close()
}

// open は sha の diff を開く。同じ SHA を再度開こうとしたら閉じる (toggle)。取得が必要
// (キャッシュ未ヒットかつ未取得) なら busy を立てて true を返す。呼び出し側はその場合だけ
// loadCommitDiff の非同期コマンドを発行する。
func (o *diffOverlay) open(sha string) (needFetch bool) {
	if o.sha == sha {
		o.close()
		return false
	}
	o.sha = sha
	o.offset = 0
	if _, ok := o.cache[sha]; ok {
		return false
	}
	if o.busy[sha] {
		return false
	}
	o.busy[sha] = true
	return true
}

// receive は取得結果 (diffMsg) を反映する。取得失敗は err を返し (呼び出し側が notice を出す)、
// その SHA が今表示中なら閉じる。古い別 SHA のエラーは表示中の diff を閉じない。
func (o *diffOverlay) receive(msg diffMsg) error {
	delete(o.busy, msg.sha)
	if msg.err != nil {
		if o.sha == msg.sha {
			o.close()
		}
		return msg.err
	}
	o.cache[msg.sha] = msg.lines
	return nil
}

// scroll は pager 流儀のキー操作を反映する。rows は表示可能行数 (レイアウト依存なので
// 呼び出し側が算出して渡す)。閉じる系キー (q/esc/h/left/d) はここで閉じる。末尾に達したら
// 最終行を表示したまま止まる (自動で閉じない)。⚠️ y (URL コピー) は境界をまたぐため
// 呼び出し側が handleDiffKey で処理し、ここには渡さない。
func (o *diffOverlay) scroll(key string, rows int) {
	maxOffset := max(len(o.cache[o.sha])-rows, 0)
	switch key {
	case "q", "esc", "h", "left", "d":
		o.close()
	case "j", "down", "ctrl+n", "enter":
		o.offset = min(o.offset+1, maxOffset)
	case "k", "up", "ctrl+p":
		o.offset = max(o.offset-1, 0)
	case "ctrl+d", "pgdown", " ", "f":
		o.offset = min(o.offset+rows/2, maxOffset)
	case "ctrl+u", "pgup", "b":
		o.offset = max(o.offset-rows/2, 0)
	case "g", "home":
		o.offset = 0
	case "G", "end":
		o.offset = maxOffset
	}
}

// boxLines は diff ポップアップの描画行 (枠付き)。非表示・コミット解決不能なら nil。
// commit は呼び出し側が SHA から解決して渡す (この型はコミット列を知らない)。rows は本文の
// 表示行数。spinner / width / colored は browseModel 側の状態を受け取る (usageOverlay と同様)。
func (o *diffOverlay) boxLines(width int, colored bool, spinner string, commit *Commit, rows int) []string {
	if o.sha == "" || commit == nil {
		return nil
	}
	if width <= 0 {
		width = 80
	}
	var body []string
	title := fmt.Sprintf(" diff: %s %s ", commit.ShortSHA, commit.Subject)
	switch {
	case o.busy[o.sha]:
		body = []string{paint(spinner+" diff を取得中...", ansiDim, colored)}
	default:
		lines := o.cache[o.sha]
		if len(lines) == 0 {
			body = []string{paint("(diff はありません)", ansiDim, colored)}
			break
		}
		start := min(o.offset, max(len(lines)-1, 0))
		end := min(start+rows, len(lines))
		body = append(body, lines[start:end]...)
		title = fmt.Sprintf(" diff: %s [%d-%d/%d] %s ", commit.ShortSHA, start+1, end, len(lines), commit.Subject)
	}
	return buildPanelBox(title, body, width, colored)
}
