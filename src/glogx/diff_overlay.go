package main

import "fmt"

// diffOverlay はコミット diff (d キー) を最前面に重ねる pager 型オーバーレイの状態と描画。
// usageOverlay と同じ方針で browseModel から diff の関心事 (状態 + open/scroll/receive/render)
// を 1 つの型へ切り出したサブコンポーネント。取得の非同期 (loadCommitDiff) とターゲット選定
// (cursor / panelSHA)・パネル閉じ・URL コピーは境界をまたぐため browseModel 側に薄く残し、
// この型は「どの SHA を、どの位置まで、どう描くか」という pager の内部状態機械だけを持つ。
type diffOverlay struct {
	sha    string // 表示中の SHA ("" = 非表示)
	offset int    // スクロール位置 (行。論理 = 着地点)
	// glide は表示位置を offset へ滑らせるスクロールアニメ (scroll_glide.go の共有型。
	// 一覧と同じ手触りにする。ユーザー要望 2026-07-31)。
	glide scrollGlide
	// lines は sha → 整形済み diff 行のキャッシュと取得の単発化 (line_cache.go)。
	cache lineCache
}

// newDiffOverlay は map を初期化した diffOverlay を返す。
func newDiffOverlay() diffOverlay {
	return diffOverlay{cache: newLineCache()}
}

// visible は diff ポップアップを表示中か。
func (o *diffOverlay) visible() bool { return o.sha != "" }

// fetching は diff 取得中の SHA が 1 つでもあるか (スピナー tick を回し続ける判定用)。
func (o *diffOverlay) fetching() bool { return o.cache.fetching() }

// close はポップアップを閉じてスクロール位置を戻す。
func (o *diffOverlay) close() {
	o.sha = ""
	o.offset = 0
	o.glide.stop() // 閉じるときに glide を残すと、次に開いた瞬間だけ古い位置から滑る
}

// reset は pull 後の全面リロードでキャッシュごと破棄する (旧 SHA の残骸を持ち越さない)。
func (o *diffOverlay) reset() {
	o.cache.reset()
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
	o.glide.stop() // 開いたまま別 SHA へ差し替える経路 (J/K) で、前の半ページ送りの滑りを持ち越さない
	return o.cache.begin(sha)
}

// receive は取得結果 (diffMsg) を反映する。取得失敗は err を返し (呼び出し側が notice を出す)、
// その SHA が今表示中なら閉じる。古い別 SHA のエラーは表示中の diff を閉じない。
func (o *diffOverlay) receive(msg diffMsg) error {
	if msg.err != nil {
		o.cache.abort(msg.sha)
		if o.sha == msg.sha {
			o.close()
		}
		return msg.err
	}
	o.cache.store(msg.sha, msg.lines, o.sha)
	return nil
}

// visibleLines は表示中 SHA のキャッシュ行 (未取得なら nil)。
func (o *diffOverlay) visibleLines() []string {
	lines, _ := o.cache.get(o.sha)
	return lines
}

// scroll は pager 流儀のキー操作を反映する。rows は表示可能行数 (レイアウト依存なので
// 呼び出し側が算出して渡す)。閉じる系キー (q/esc/h/left/d) はここで閉じる。末尾に達したら
// 最終行を表示したまま止まる (自動で閉じない)。🚨 y (URL コピー) は境界をまたぐため
// 呼び出し側が handleDiffKey で処理し、ここには渡さない。
func (o *diffOverlay) scroll(key string, rows int) {
	switch key {
	case "q", "esc", "h", "left", "d":
		o.close()
		return
	}
	// スクロールの語彙 (1 行 / 半ページ + glide / 端ジャンプ) は status viewer の全画面 diff と
	// 共有する (scroll_glide.go の pagerScrollKey)。手触りを 1 箇所に集約するため。
	o.offset = pagerScrollKey(key, o.offset, rows, len(o.visibleLines()), &o.glide)
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
	case o.cache.loading(o.sha):
		body = []string{paint(spinner+" diff を取得中...", ansiDim, colored)}
	default:
		lines := o.visibleLines()
		if len(lines) == 0 {
			body = []string{paint("(diff はありません)", ansiDim, colored)}
			break
		}
		// 行数と窓 (rows) は端末サイズで変わる。rows が増えると maxOffset が下がるのに論理
		// offset は据え置かれ、k / up / ctrl+p は max(offset-n, 0) しか見ないので
		// **(rows_new - rows_old) 打鍵だけ上スクロールが死ぬ** (実測 2026-08-21: diff 33 打鍵 /
		// job 詳細 11 打鍵)。描画で確定した行数・窓で論理 offset を収束させて防ぐ
		// (issues_view.go の bodyOff が同じ規律。🚨 pagerScrollKey の k 腕に clamp を足す形は
		// 不可: job 詳細のスクロールは pagerScrollKey を通らない手書きなので片面しか直らない)。
		o.offset = clampScrollOffset(o.offset, len(lines), rows)
		start := clampScrollOffset(o.glide.offset(o.offset), len(lines), rows)
		end := min(start+rows, len(lines))
		body = append(body, lines[start:end]...)
		title = fmt.Sprintf(" diff: %s [%d-%d/%d] %s ", commit.ShortSHA, start+1, end, len(lines), commit.Subject)
		// j/k スクロール中の現在位置を視覚化する (withScrollbar が影付き枠の本文幅を補正する)
		body = withScrollbar(body, width, len(lines), start, colored)
	}
	return buildShadowPanelBox(title, body, width, colored, ansiDim)
}

// animating は演出の途中か (tick チェーンを回すか の判定に使う。issuesView.animating /
// statusView.animating と同じ契約)。diff は本文 pager の glide だけがアニメ源。
func (o *diffOverlay) animating() bool { return o.glide.active }

// advanceGlide はスクロール glide を 1 フレーム進める (browseModel の tick から呼ばれる)。
func (o *diffOverlay) advanceGlide() {
	if o.glide.active {
		o.glide.advance(o.offset)
	}
}
