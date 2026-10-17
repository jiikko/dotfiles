package main

import (
	"glogx/internal/app"
	"glogx/internal/term"
	"strings"
)

// 最下行の案内 (hint) を出す「面」のレジストリ。issue 289。
//
// なぜレジストリにするか: hint の不変条件は「**幅に収まる**」と「**抜ける手段が残る**」の 2 つで、
// これを面ごとに別の人が守っていたのが 279 / 281 の P1 2 本の原因だった。面を足す人が
// 予算計算を通し忘れる / 幅ゲートに登録し忘れると、**末尾 = 出口**が黙って切れる。
// 面を表に集めれば、予算は 1 箇所を通り、検査は表を回すだけで全面を覆える。
//
// 🚨 **脅威モデルと射程** (adversarial-review-own-safeguards §8。実装前に書いたもの):
//   - 止めるのは「面を足したとき ①予算計算を通し忘れる ②幅ゲートに登録し忘れる」の 2 つ
//   - 表に足すと `hintBuilders` の要素が nil のままになり `TestHintBuildersCoverEveryID` が落ちる。
//     テスト側の面の作り方 (`hintSurfaceSetup`) も同じ数だけ要るので、登録漏れは red になる
//   - **検出しない**: 語そのものの妥当性 (「q: 終了」が正しい案内か) / 全画面ビューア 4 枚の
//     hint 実装 (下記のとおり意図的に別経路) / 動的に組み立てた文字列が実行時に長くなる場合の
//     中身。**長さは幅ゲートが実測で見るが、fixture が最長形を作れている面に限る** —
//     hintSurfaceUpdating だけが実行時に伸びるので、そこは fixture を 2 target にして
//     実在しうる最長 (claude + codex = 26 桁、最小予算 58 桁) を測っている。これらは review の責務
//   - この 3 つは実装後に実物と突き合わせて確定させた (着手前に書いた版は意図であって射程ではない)
//
// 🚨 **全画面ビューア (ratelimit / doctor / status / issues) はここに入れない。**
// あちらは `hintLine` の冒頭で早期 return し、viewer 自身の `hint(width)` が popup の実幅
// ぴったりに詰める。前置 (取得中 / gh 警告) を混ぜると末尾の抜ける手段が切れるため、
// **前置を積まない**のが意図 (`hintLine` の冒頭コメントが一次情報)。幅の検査は
// `hint_width_test.go` が `fullScreenCases` 経由で別に回している。

type hintSurfaceID int

const (
	hintSurfaceBase hintSurfaceID = iota // 基底の一覧
	hintSurfacePushConfirm
	hintSurfacePullConfirm
	hintSurfacePushing
	hintSurfacePulling
	hintSurfaceRerunConfirm
	hintSurfaceRerunning
	hintSurfaceUpdating
	hintSurfaceDiff
	hintSurfacePRStatus
	hintSurfaceJobDetail
	hintSurfacePanelCursor
	hintSurfacePanelNoCursor
	hintSurfaceCount // 🚨 常に最後。面を足したらここより上へ
)

// hintBuilders は面 ID → その面の hint 項目。**唯一の出典**。
//
// 🚨 配列にしているのは、ID を足したときに要素が nil のまま残るのを検査で捕まえるため
// (map だと「まだ足していない」と「意図的に空」が区別できない)。
var hintBuilders = [hintSurfaceCount]func(*browseModel) []term.HintItem{
	// 🚨 固定文字列にしないこと (issue 279)。全部つなぐと 174 桁あり、端末 176 桁未満では
	// 末尾の `q: 終了` が切られて**抜ける手段が案内から消える**。既定の画面・一般的な幅で
	// 常時そうなっていた。優先度 1 = 出口、2 = 移動と主要操作、以降は幅が許せば出す。
	hintSurfaceBase: func(*browseModel) []term.HintItem {
		return []term.HintItem{
			{Text: "j/k: 移動", Prio: 2},
			{Text: "Enter: CI job", Prio: 3},
			{Text: "d: diff", Prio: 3},
			{Text: "o: ブラウザ", Prio: 5},
			{Text: "p: PR", Prio: 6},
			{Text: "P: PR 状態", Prio: 6},
			{Text: "y: URL コピー", Prio: 5},
			{Text: "b: push", Prio: 4},
			{Text: "u: pull", Prio: 4},
			{Text: "i: issues", Prio: 4},
			{Text: "U: usage", Prio: 6},
			{Text: "R: 残量", Prio: 6},
			{Text: "C: update", Prio: 7},
			{Text: "D: doctor", Prio: 5},
			{Text: "w: 警告コピー", Prio: 7},
			{Text: "q: 終了", Prio: 1},
		}
	},
	// 確認・進行中の面は 1 項目だけ。**優先度 1** にするのは、幅が足りないときに
	// 「何を聞かれているか / 何を待っているか」が消えると操作不能になるため
	// (term.FitHintItems は入らない項目を落とすので、1 でないと丸ごと消えうる)。
	hintSurfacePushConfirm: func(*browseModel) []term.HintItem {
		return []term.HintItem{{Text: "push しますか? [Y/n] (Enter=y)", Prio: 1}}
	},
	hintSurfacePullConfirm: func(*browseModel) []term.HintItem {
		return []term.HintItem{{Text: "pull --rebase しますか? [Y/n] (Enter=y)", Prio: 1}}
	},
	hintSurfacePushing: func(m *browseModel) []term.HintItem {
		return []term.HintItem{{Text: m.spinner() + " pushing...", Prio: 1}}
	},
	hintSurfacePulling: func(m *browseModel) []term.HintItem {
		return []term.HintItem{{Text: m.spinner() + " pulling...", Prio: 1}}
	},
	hintSurfaceRerunConfirm: func(*browseModel) []term.HintItem {
		return []term.HintItem{{Text: "job を再実行しますか? [Y/n] (Enter=y)", Prio: 1}}
	},
	hintSurfaceRerunning: func(m *browseModel) []term.HintItem {
		return []term.HintItem{{Text: m.spinner() + " rerunning...", Prio: 1}}
	},
	hintSurfaceUpdating: func(m *browseModel) []term.HintItem {
		return []term.HintItem{{Text: m.spinner() + " " + strings.Join(m.actModal.updatingTargets(), " + ") + " update...", Prio: 1}}
	},
	hintSurfaceDiff: func(*browseModel) []term.HintItem {
		return []term.HintItem{
			{Text: "j/k/Space: スクロール", Prio: 2},
			{Text: "J/K: 隣のコミット", Prio: 3},
			{Text: "g/G: 先頭/末尾", Prio: 5},
			{Text: "y: URL コピー", Prio: 4},
			{Text: "q/h: 閉じる", Prio: 1}, // 抜ける手段は最優先
		}
	},
	hintSurfacePRStatus: func(*browseModel) []term.HintItem {
		return []term.HintItem{
			{Text: "o: PR をブラウザで開く", Prio: 3},
			{Text: "y: URL コピー", Prio: 4},
			{Text: "P/q/h: 閉じる", Prio: 1},
		}
	},
	hintSurfaceJobDetail: func(*browseModel) []term.HintItem {
		return []term.HintItem{
			{Text: "j/k: スクロール", Prio: 3},
			{Text: "J/K: 隣の job", Prio: 4},
			{Text: "v: nvim で開く", Prio: 4},
			{Text: "r: 再実行", Prio: 3},
			{Text: "Enter/h/q: 戻る", Prio: 1}, // 抜ける手段は最優先
			{Text: "o: ブラウザ", Prio: 4},
			{Text: "y: URL", Prio: 5},
			{Text: "Y: 詳細コピー", Prio: 5},
		}
	},
	hintSurfacePanelCursor: func(*browseModel) []term.HintItem {
		return []term.HintItem{
			{Text: "j/k: job 移動", Prio: 3},
			{Text: "Enter: 詳細ログ", Prio: 3},
			{Text: "r: 再実行", Prio: 4},
			{Text: "o: ブラウザ", Prio: 4},
			{Text: "d: diff", Prio: 5},
			{Text: "p: PR", Prio: 5},
			{Text: "y: URL", Prio: 5},
			{Text: "Y: 詳細コピー", Prio: 6},
			{Text: "h/q: 閉じる", Prio: 1}, // 抜ける手段は最優先
		}
	},
	// カーソル無し (パネルを開いた直後の既定状態。openPanel が panelCursor = -1 にする)。
	// 🚨 固定文字列にしないこと (issue 279)。63 桁あり、最小サポート幅の帯 (w=60〜69) で
	// 出口 `Enter/h/q: 閉じる` が語中で切れて消えていた。
	hintSurfacePanelNoCursor: func(*browseModel) []term.HintItem {
		return []term.HintItem{
			{Text: "j: job を選択", Prio: 2},
			{Text: "d: diff", Prio: 3},
			{Text: "p: PR", Prio: 4},
			{Text: "y: commit URL", Prio: 4},
			{Text: "Enter/h/q: 閉じる", Prio: 1}, // 抜ける手段は最優先
		}
	},
}

// hintPrefix は hint の前に積む「今なぜ待っているか」の説明。
//
// 🚨 **前置を足すならここに足すこと。hintLine の中で prefix へ書き足さない**
// (敵対的レビュー 2 周目の P1)。hintLine 側は `prefix := m.hintPrefix()` の 1 行だけを持ち、
// その直後に予算を引く。**予算を引いた後で前置を積める形を残さない**のが目的で、
// それこそが 279 / 281 の根本原因だった (term.FitHintItems が幅ぴったりに収めた後に前置を積み、
// hintLineText の term.ClipToWidth が末尾 = 出口を落としていた)。
// 実測 2026-09-06: 予算減算の下に新しい前置を 1 ブロック足すと、字句ゲートも幅ゲートも
// 素通りしたまま幅 60〜140 の全帯で出口が消えた。迂回は TestHintLinePrefixGoesThroughHelper が止める。
//
// 順序: gh の警告が先 (取得できていない理由の方が、取得中であることより重い)。
func (m *browseModel) hintPrefix(maxWidth int) string {
	prefix := ""
	if m.fetching() {
		prefix = m.spinner() + " CI 状態を取得中...  "
	}
	if m.ghErr != nil {
		prefix = "🚨 " + app.FirstLine(m.ghErr.Warning()) + "  " + prefix
	}
	if term.DispWidth(prefix) > maxWidth {
		prefix = term.ClipToWidth(prefix, maxWidth)
	}
	return prefix
}

// activeHintSurface は今どの面が出ているかを返す。**状態 → ID の対応はここだけ**。
//
// 🚨 分岐の順序は意味を持つ: actModal (確認・進行中) が最優先で、以降は overlay の深い順。
// 並べ替えると「push 確認中なのに diff の案内が出る」形になる。
func (m *browseModel) activeHintSurface() hintSurfaceID {
	switch {
	case m.actModal.pushConfirm:
		return hintSurfacePushConfirm
	case m.actModal.pullConfirm:
		return hintSurfacePullConfirm
	case m.actModal.pushing:
		return hintSurfacePushing
	case m.actModal.pulling:
		return hintSurfacePulling
	case m.actModal.rerunConfirm:
		return hintSurfaceRerunConfirm
	case m.actModal.rerunning:
		return hintSurfaceRerunning
	case m.actModal.anyUpdating():
		return hintSurfaceUpdating
	case m.diffOv.Visible():
		return hintSurfaceDiff
	case m.prStatusOv.Visible():
		return hintSurfacePRStatus
	case m.detailOv.Visible():
		return hintSurfaceJobDetail
	case m.panelSHA != "" && m.panelCursor >= 0:
		return hintSurfacePanelCursor
	case m.panelSHA != "":
		return hintSurfacePanelNoCursor
	}
	return hintSurfaceBase
}
