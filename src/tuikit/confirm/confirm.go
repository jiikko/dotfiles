// Package confirm は y/N 確認ダイアログの部品: 中央に浮かべる小さな板と、実行キーの判定。
//
// 描画は layout.Panel の上に「本文 + 空行 + 案内」を並べるだけで、重ねるのは使う側
// (layout.OverlayCentered)。状態 (確認中か・何を確認しているか) も使う側が持つ。
package confirm

import (
	"tuikit/layout"
	"tuikit/sgr"
)

// MaxWidth は板の幅の上限。これより広い画面でも板は広げない (短い確認文が横に間延びしない)。
//
// 🚨 長いファイル名などは 1 行に繋がず行を分ける。上限を超えた行は `…` で切られ、切れた先が
// 確認の対象そのもの (宛先・パス) だと「実行前の唯一の確認画面」に何も出ない。
const MaxWidth = 44

// WideMaxWidth は WideDialog の板の幅の上限 (表のような揃えた行を並べる確認。pro-con の終了のダイアログ)。
const WideMaxWidth = 64

// 案内の定型。どのキーが取り消しになるかは IsYes / IsYesStrict のどちらで判定するかに合わせて選ぶ
// (どちらも y と Enter 以外はすべて取り消し。HintYesNo は取り消しの代表キーを挙げているだけ)。
const (
	HintYesNo    = "y/Enter: 実行   n/Esc: キャンセル"
	HintYesOther = "y/Enter: 実行   その他: キャンセル"
)

// Box は中央に浮かべる小さな板 (確認・通知)。幅は min(MaxWidth, width)、width が 0 以下なら 80 とみなす。
// title は枠の上辺に載る (前後の空白は呼び出し側が付ける: " git push ")。
func Box(title string, rows []string, width int, colored bool) []string {
	return box(title, rows, MaxWidth, width, colored)
}

func box(title string, rows []string, maxWidth, width int, colored bool) []string {
	if width <= 0 {
		width = 80
	}
	return layout.Panel(title, rows, min(maxWidth, width), colored,
		layout.PanelStyle{Border: layout.BorderLight, Color: sgr.Dim})
}

// Dialog は確認ダイアログの板: body の下に空行を 1 つ空けて hint を淡色で置く。
func Dialog(title string, body []string, hint string, width int, colored bool) []string {
	return dialog(title, body, hint, MaxWidth, width, colored)
}

// WideDialog は幅の上限を WideMaxWidth にした Dialog (揃えた行が MaxWidth では切れる確認)。
func WideDialog(title string, body []string, hint string, width int, colored bool) []string {
	return dialog(title, body, hint, WideMaxWidth, width, colored)
}

func dialog(title string, body []string, hint string, maxWidth, width int, colored bool) []string {
	rows := make([]string, 0, len(body)+2)
	rows = append(rows, body...)
	if colored {
		hint = sgr.Dim + hint + sgr.Reset
	}
	return box(title, append(rows, "", hint), maxWidth, width, colored)
}

// IsYes は確認の「実行」キーか: y / Y / Enter。それ以外はすべて取り消しとして扱う。
//
// キーの綴りは bubbletea の KeyPressMsg.String() (Enter は "enter")。
func IsYes(key string) bool { return key == "y" || key == "Y" || key == "enter" }

// IsYesStrict は IsYes から大文字 Y を外したもの: y / Enter だけが実行。
//
// 🚨 IsYes と使い分けている画面がある (glogx の issues viewer の next 目印は厳密、push / pull /
// 変更の破棄は Y も受ける)。揃えるかは画面ごとの判断で、この部品は両方を用意するだけ
// (経緯は glogx の status_view.go:discardKey の注記)。
func IsYesStrict(key string) bool { return key == "y" || key == "enter" }
