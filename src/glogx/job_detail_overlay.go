package main

import "fmt"

// jobDetailOverlay は job 詳細 (annotations / ログ tail) の第 2 ポップアップの状態と描画。
// diffOverlay と同型の pager だが、diffOverlay が自前の識別子 (sha) を所有するのに対し、こちらは
// キャッシュキー (detailKey = panelSHA/panelCursor) を「パネルのカーソル座標から借りる」= identity
// 非所有。そのため open/scroll/receive/boxLines は毎回 key を引数で受け取る (暗黙の reach-in を
// 境界の明示パラメータへ昇格させる)。panel-frame (panelSHA/panelCursor/poll/refresh) と ETA・CI
// 取得は details/statuses/commits と構造的に結合するため browseModel に残し、この型は「job ログを
// どうスクロール/キャッシュ/スピン/描画するか」だけを持つ。
//
// ⚠️ diffOverlay との差 (素朴コピー禁止): (1) scroll の閉じキーは enter/space/esc/h/left
// (diff は q/esc/h/left/d)。job 詳細では enter/space も「閉じる」= tig 流の詳細→job 一覧。
// (2) startOpen は cache ヒット時 offset をログ末尾へ (直近出力を表示)。diffOv.open の offset=0 clone
// ではない。(3) toggle しない (open 中は handlePanelKey が handleDetailKey へ委譲するため常に閉状態
// から呼ばれる)。(4) ghErr (共有 sticky 警告) は触らない — browseModel の jobDetailMsg ハンドラが
// 無条件代入して C4 契約 (成功時 nil クリア) を維持する。
type jobDetailOverlay struct {
	open   bool                // 詳細ポップアップ表示中か
	offset int                 // スクロール位置 (行)
	cache  map[string][]string // key (detailKey) → ログ行 (メモリ内キャッシュ)
	busy   map[string]bool     // 取得中の key
}

// newJobDetailOverlay は map を初期化した jobDetailOverlay を返す。
func newJobDetailOverlay() jobDetailOverlay {
	return jobDetailOverlay{cache: map[string][]string{}, busy: map[string]bool{}}
}

// visible は詳細ポップアップを表示中か。
func (o *jobDetailOverlay) visible() bool { return o.open }

// fetching は詳細取得中の key が 1 つでもあるか (スピナー tick を回し続ける判定用)。
func (o *jobDetailOverlay) fetching() bool { return len(o.busy) > 0 }

// close は詳細ポップアップを閉じてスクロール位置を戻す。cache は保持する (閉じ直しで再取得
// しないため)。全パネル退出経路 (handleKey q / closePanel) と handleDetailKey の閉じキーが呼ぶ。
func (o *jobDetailOverlay) close() {
	o.open = false
	o.offset = 0
}

// reset は pull 後の全面リロードで cache ごと破棄する (旧 SHA のログ残骸を持ち越さない)。
func (o *jobDetailOverlay) reset() {
	o.cache = map[string][]string{}
	o.busy = map[string]bool{}
	o.close()
}

// startOpen はフォーカス job のポップアップを開く。cache ヒットなら offset をログ末尾へ
// (rows = 表示可能行数)、未ヒットかつ未 busy なら busy を立てて needFetch=true を返す。
// 呼び出し側は needFetch のときだけ FetchJobDetail を発行する (openDiff と対称)。
func (o *jobDetailOverlay) startOpen(key string, rows int) (needFetch bool) {
	o.open = true
	o.offset = 0
	if lines, ok := o.cache[key]; ok {
		o.offset = max(len(lines)-rows, 0) // ログ末尾 (直近出力) を表示
		return false
	}
	if o.busy[key] {
		return false
	}
	o.busy[key] = true
	return true
}

// receive は取得結果 (jobDetailMsg) を反映する。busy を落とし lines を cache へ格納し、今まさに
// 開いている詳細 (open かつ currentKey == msg.key) なら offset をログ末尾へ合わせる。currentKey は
// 呼び出し時に detailKey() から取り直した live な値を渡すこと (snapshot 禁止: リフレッシュで
// job 数が縮み panelCursor がクランプされ key が変わる経路に追従するため)。
func (o *jobDetailOverlay) receive(msg jobDetailMsg, currentKey string, rows int) {
	delete(o.busy, msg.key)
	if msg.lines != nil {
		o.cache[msg.key] = msg.lines
		if o.open && currentKey == msg.key {
			o.offset = max(len(msg.lines)-rows, 0)
		}
	}
}

// scroll は詳細 pager のスクロール/閉じキーを反映する。contentKey は maxOffset 算出用の現在の
// cache キー、rows は表示可能行数 (どちらもレイアウト/パネル状態依存なので呼び出し側が渡す)。
// ⚠️ 閉じキーは enter/space/esc/h/left (diffOverlay と異なる)。o/v/y の越境キーは呼び出し側
// (handleDetailKey) が処理し、ここには渡らない。
func (o *jobDetailOverlay) scroll(key, contentKey string, rows int) {
	maxOffset := max(len(o.cache[contentKey])-rows, 0)
	switch key {
	case "enter", " ", "esc", "h", "left":
		o.close()
	case "j", "down", "ctrl+n":
		o.offset = min(o.offset+1, maxOffset)
	case "k", "up", "ctrl+p":
		o.offset = max(o.offset-1, 0)
	case "ctrl+d", "pgdown":
		o.offset = min(o.offset+rows/2, maxOffset)
	case "ctrl+u", "pgup":
		o.offset = max(o.offset-rows/2, 0)
	case "g", "home":
		o.offset = 0
	case "G", "end":
		o.offset = maxOffset
	}
}

// lines は key の cache 済みログ行を返す (nvim で開く v キー用の getter)。
func (o *jobDetailOverlay) lines(key string) []string { return o.cache[key] }

// boxLines は詳細ポップアップの描画行 (枠付き)。name は job 名 (title 用)、key は cache キー、
// rows は本文の表示行数。spinner / width / colored は browseModel の状態を受け取る。
func (o *jobDetailOverlay) boxLines(width int, colored bool, spinner, name, key string, rows int) []string {
	var body []string
	title := " " + name + " "
	switch {
	case o.busy[key]:
		body = []string{paint(spinner+" 詳細を取得中...", ansiDim, colored)}
	default:
		lines := o.cache[key]
		if len(lines) == 0 {
			body = []string{paint("(詳細なし)", ansiDim, colored)}
			break
		}
		start := min(o.offset, max(len(lines)-1, 0))
		end := min(start+rows, len(lines))
		body = make([]string, 0, end-start)
		for _, l := range lines[start:end] {
			body = append(body, decorateDetailLine(l, colored))
		}
		title = fmt.Sprintf(" %s [%d-%d/%d] ", name, start+1, end, len(lines))
	}
	return buildPanelBox(title, body, width, colored)
}
