package main

import "fmt"

// prStatusOverlay は PR 状態ポップアップ (P キー, issue 021) の状態と描画。コミットに紐づく
// PR の「マージできるか」(draft / レビュー / conflict) をブラウザを開かずに確認する。
// diffOverlay と同型: 対象 sha を所有し、open は toggle、取得結果はセッション内キャッシュ。
// CI 行はコミット側の statuses が出典なので、boxLines へ整形済みの 1 行 (ciLine) を注入する
// (この型は CI 状態を知らない)。
type prStatusOverlay struct {
	sha   string               // 表示対象コミットの SHA ("" = 非表示)
	busy  bool                 // 表示対象の取得中 (スピナー)
	cache map[string]*PRStatus // sha → PR 詳細 (nil 格納 = 確認済みで PR なし)
}

func newPRStatusOverlay() prStatusOverlay {
	return prStatusOverlay{cache: map[string]*PRStatus{}}
}

// visible はポップアップ表示中か。
func (o *prStatusOverlay) visible() bool { return o.sha != "" }

// fetching は取得中か (スピナー tick を回す判定用)。
func (o *prStatusOverlay) fetching() bool { return o.busy }

// close はポップアップを閉じる。cache は保持する (開き直しで再取得しない)。
func (o *prStatusOverlay) close() {
	o.sha = ""
	o.busy = false
}

// reset は pull 後の全面リロードで cache ごと破棄する (旧 SHA の残骸を持ち越さない)。
func (o *prStatusOverlay) reset() {
	o.cache = map[string]*PRStatus{}
	o.close()
}

// open は sha のポップアップを開く。同じ sha なら閉じる (toggle)。cache 未ヒットのときだけ
// needFetch=true を返す (呼び出し側が FetchPRStatus を発行する)。
func (o *prStatusOverlay) open(sha string) (needFetch bool) {
	if o.sha == sha {
		o.close()
		return false
	}
	o.sha = sha
	if _, ok := o.cache[sha]; ok {
		o.busy = false
		return false
	}
	o.busy = true
	return true
}

// receive は取得結果を反映する。エラー時はキャッシュせず閉じる (呼び出し側が notice を出す。
// 一時エラーで「PR なし」を固定しない = prMsg と同じ方針)。表示対象が変わった後の遅延到着は
// キャッシュだけ更新する。
func (o *prStatusOverlay) receive(sha string, status *PRStatus, ghErr *GHError) {
	if sha == o.sha {
		o.busy = false
	}
	if ghErr != nil {
		if sha == o.sha {
			o.close()
		}
		return
	}
	o.cache[sha] = status
}

// current は表示中 PR の詳細 (未取得 / PR なしは nil)。o (ブラウザ) / y (コピー) の URL 解決用。
func (o *prStatusOverlay) current() *PRStatus {
	if o.sha == "" {
		return nil
	}
	return o.cache[o.sha]
}

// prStateLabel は PR の状態行 ("OPEN" / "OPEN (draft)" / "MERGED" ...) を色付きで返す。
func prStateLabel(pr *PRStatus, colored bool) string {
	color := ansiDim
	switch pr.State {
	case "OPEN":
		color = ansiGreen
	case "MERGED":
		color = ansiMagenta
	case "CLOSED":
		color = ansiRed
	}
	label := pr.State
	if pr.IsDraft {
		label += " (draft)"
	}
	return paint(label, color, colored)
}

// reviewRow は reviewDecision の表示行。ブランチ保護が無い repo では null ("") が返る。
func reviewRow(decision string, colored bool) string {
	switch decision {
	case "APPROVED":
		return paint("✓ APPROVED", ansiGreen, colored)
	case "CHANGES_REQUESTED":
		return paint("✗ CHANGES_REQUESTED", ansiRed, colored)
	case "REVIEW_REQUIRED":
		return paint("● REVIEW_REQUIRED", ansiYellow, colored)
	default:
		return paint("(レビュー必須ではない)", ansiDim, colored)
	}
}

// mergeableRow は mergeable の表示行。UNKNOWN は GitHub 側の遅延計算中 (リトライはしない)。
func mergeableRow(mergeable string, colored bool) string {
	switch mergeable {
	case "MERGEABLE":
		return paint("✓ なし (MERGEABLE)", ansiGreen, colored)
	case "CONFLICTING":
		return paint("✗ あり (CONFLICTING)", ansiRed, colored)
	default:
		return paint("? 計算中 (UNKNOWN)", ansiDim, colored)
	}
}

// boxLines はポップアップの描画行 (枠付き)。非表示なら nil。ciLine はコミットの CI 状態の
// 整形済み 1 行 ("" = 出さない)。spinner / width / colored は browseModel の状態を受け取る。
func (o *prStatusOverlay) boxLines(width int, colored bool, spinner, ciLine string) []string {
	if o.sha == "" {
		return nil
	}
	if width <= 0 {
		width = 80
	}
	if o.busy {
		return buildPanelBox(" PR ", []string{paint(spinner+" PR を取得中...", ansiDim, colored)}, width, colored)
	}
	pr := o.cache[o.sha]
	if pr == nil {
		// receive で PR なしが確定したケース (呼び出し側は notice も出すが、開いたままなら枠で示す)
		return buildPanelBox(" PR ", []string{paint("(紐づく PR はありません)", ansiDim, colored)}, width, colored)
	}
	title := fmt.Sprintf(" PR #%d: %s ", pr.Number, sanitizeDetailLine(pr.Title))
	rows := []string{
		prStateLabel(pr, colored) + "  " + paint(pr.HeadRefName+" → "+pr.BaseRefName, ansiDim, colored),
		"レビュー: " + reviewRow(pr.ReviewDecision, colored),
		"conflict: " + mergeableRow(pr.Mergeable, colored),
	}
	if ciLine != "" {
		rows = append(rows, "CI: "+ciLine)
	}
	return buildPanelBox(title, rows, width, colored)
}
