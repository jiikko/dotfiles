package main

import "strings"

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
var hintBuilders = [hintSurfaceCount]func(*browseModel) []hintItem{
	// 🚨 固定文字列にしないこと (issue 279)。全部つなぐと 174 桁あり、端末 176 桁未満では
	// 末尾の `q: 終了` が切られて**抜ける手段が案内から消える**。既定の画面・一般的な幅で
	// 常時そうなっていた。優先度 1 = 出口、2 = 移動と主要操作、以降は幅が許せば出す。
	hintSurfaceBase: func(*browseModel) []hintItem {
		return []hintItem{
			{"j/k: 移動", 2},
			{"Enter: CI job", 3},
			{"d: diff", 3},
			{"o: ブラウザ", 5},
			{"p: PR", 6},
			{"P: PR 状態", 6},
			{"y: URL コピー", 5},
			{"b: push", 4},
			{"u: pull", 4},
			{"i: issues", 4},
			{"U: usage", 6},
			{"R: 残量", 6},
			{"C: update", 7},
			{"D: doctor", 5},
			{"w: 警告コピー", 7},
			{"q: 終了", 1},
		}
	},
	// 確認・進行中の面は 1 項目だけ。**優先度 1** にするのは、幅が足りないときに
	// 「何を聞かれているか / 何を待っているか」が消えると操作不能になるため
	// (fitHintItems は入らない項目を落とすので、1 でないと丸ごと消えうる)。
	hintSurfacePushConfirm: func(*browseModel) []hintItem {
		return []hintItem{{"push しますか? [Y/n] (Enter=y)", 1}}
	},
	hintSurfacePullConfirm: func(*browseModel) []hintItem {
		return []hintItem{{"pull --rebase しますか? [Y/n] (Enter=y)", 1}}
	},
	hintSurfacePushing: func(m *browseModel) []hintItem {
		return []hintItem{{m.spinner() + " pushing...", 1}}
	},
	hintSurfacePulling: func(m *browseModel) []hintItem {
		return []hintItem{{m.spinner() + " pulling...", 1}}
	},
	hintSurfaceRerunConfirm: func(*browseModel) []hintItem {
		return []hintItem{{"job を再実行しますか? [Y/n] (Enter=y)", 1}}
	},
	hintSurfaceRerunning: func(m *browseModel) []hintItem {
		return []hintItem{{m.spinner() + " rerunning...", 1}}
	},
	hintSurfaceUpdating: func(m *browseModel) []hintItem {
		return []hintItem{{m.spinner() + " " + strings.Join(m.actModal.updatingTargets(), " + ") + " update...", 1}}
	},
	hintSurfaceDiff: func(*browseModel) []hintItem {
		return []hintItem{
			{"j/k/Space: スクロール", 2},
			{"J/K: 隣のコミット", 3},
			{"g/G: 先頭/末尾", 5},
			{"y: URL コピー", 4},
			{"q/h: 閉じる", 1}, // 抜ける手段は最優先
		}
	},
	hintSurfacePRStatus: func(*browseModel) []hintItem {
		return []hintItem{
			{"o: PR をブラウザで開く", 3},
			{"y: URL コピー", 4},
			{"P/q/h: 閉じる", 1},
		}
	},
	hintSurfaceJobDetail: func(*browseModel) []hintItem {
		return []hintItem{
			{"j/k: スクロール", 3},
			{"J/K: 隣の job", 4},
			{"v: nvim で開く", 4},
			{"r: 再実行", 3},
			{"Enter/h/q: 戻る", 1}, // 抜ける手段は最優先
			{"o: ブラウザ", 4},
			{"y: URL", 5},
			{"Y: 詳細コピー", 5},
		}
	},
	hintSurfacePanelCursor: func(*browseModel) []hintItem {
		return []hintItem{
			{"j/k: job 移動", 3},
			{"Enter: 詳細ログ", 3},
			{"r: 再実行", 4},
			{"o: ブラウザ", 4},
			{"d: diff", 5},
			{"p: PR", 5},
			{"y: URL", 5},
			{"Y: 詳細コピー", 6},
			{"h/q: 閉じる", 1}, // 抜ける手段は最優先
		}
	},
	// カーソル無し (パネルを開いた直後の既定状態。openPanel が panelCursor = -1 にする)。
	// 🚨 固定文字列にしないこと (issue 279)。63 桁あり、最小サポート幅の帯 (w=60〜69) で
	// 出口 `Enter/h/q: 閉じる` が語中で切れて消えていた。
	hintSurfacePanelNoCursor: func(*browseModel) []hintItem {
		return []hintItem{
			{"j: job を選択", 2},
			{"d: diff", 3},
			{"p: PR", 4},
			{"y: commit URL", 4},
			{"Enter/h/q: 閉じる", 1}, // 抜ける手段は最優先
		}
	},
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
	case m.diffOv.visible():
		return hintSurfaceDiff
	case m.prStatusOv.visible():
		return hintSurfacePRStatus
	case m.detailOv.visible():
		return hintSurfaceJobDetail
	case m.panelSHA != "" && m.panelCursor >= 0:
		return hintSurfacePanelCursor
	case m.panelSHA != "":
		return hintSurfacePanelNoCursor
	}
	return hintSurfaceBase
}
