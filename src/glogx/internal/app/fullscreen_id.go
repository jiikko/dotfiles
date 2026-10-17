package app

// 「今どの全画面ビューアが出ているか」の単一の出典。
//
// 全画面ビューア = 開いている間コミット一覧の窓ごと画面を差し替え、キーを全部飲むもの
// (issues / status / 残量ダッシュボード / doctor / treefiler / mac-lan-manager)。**同時に開くのは高々 1 枚**で、
// 横断 (viewer の R / ダッシュボードの i・s) は「閉じてから開く」、起動時の復元は
// 既に 1 枚出ていれば捨てる (issuesRestoreMsg の注記)。
//
// 🚨 かつてこの membership は各所の手書き列挙に散っており、doctor を issue 148 で後から
// 足したとき `gitLogReloadDeferred` が漏れた (issue 227)。漏れると doctor を開いている間の
// 外部変更で裏の全面リロード (git 5〜6 fork) + カーソルのリセット + CI の再取得が走るが、
// build もテストも通るので silent に壊れる。
//
// そこで **「開いているか」を問う側は全部 activeFullScreen() から導出する**:
//
//	gitLogReloadDeferred (見送り) / viewLines (描画) / hintLine (最下行) /
//	issuesRestoreMsg のガード (復元の破棄) / handleKey の routing
//
// 新しい全画面ビューアは FullScreenCount の直前に ID を足す。足した瞬間に 2 段で捕まる:
//
//  1. **lint** — `exhaustive` (`.golangci.yml`) が「default なし switch は enum を全部書く」を
//     強制するので、ID を足すと activeFullScreen を読む全 switch が `make lint` で赤くなる。
//     🚨 **これらの switch に `default:` を書かないこと**。この repo は
//     `default-signifies-exhaustive: true` なので、default を 1 つ書いた switch は
//     網羅チェックから外れ、その配線だけが黙って取り残される
//  2. **テスト** — `TestFullScreenSurfacesWireEverySite` (fullscreen_test.go) が ID ごとに
//     全サイトの**挙動**を通す (lint は「case を書いたか」しか見ない。中身が空でも通る)
//
// 🚨 **この 2 段は「ID を足した後」にしか発火しない**。脅威モデルと射程をここに書いておく
// (`adversarial-review-own-safeguards.md` の節 8):
//
//   - **止める形**: ID を足して配線を 1 つ忘れる (issue 148 の doctor が実際に辿った形)
//   - **止める形**: ID を足さずに描画だけ旧来の `if …visible()` で配線する →
//     `TestFullScreenDrawingGoesThroughTheRegistry` が red にする。全画面ビューアは
//     `finishWithGlobalChrome` を通らないとトースト・usage・モーダルが載らないので、
//     描画をレジストリ経由に縛れば ID を足さざるを得ない = 上の 2 段へ合流する
//   - **止めない形**: `finishWithGlobalChrome` を通さずに自前で全画面を描く / 描画以外
//     (routing だけ・見送りだけ) を `if` で足す。**これはレビューの責務**とする
//     (構文で全部を塞ぎにいくと迂回が無限に出て収束しない)
//
// 🚨 ここから導出しないもの (概念が別。混ぜると「全画面か」で答えられない問いを持ち込む):
//
//   - `updateKeyReachable` / ctrl+c / `restartPromptVisible` — 問いは「今キーの語彙を
//     持っているか (ownsKeys)」で、全画面かどうかではない。正本は issue 213 の
//     overlayOwnershipTable (overlay_ownership_test.go)
//   - `spinnerActive` — 問いは「読み込み中か」。見えていないビューアの読み込み
//     (issues の見張り等) も回すので、可視性から導出すると tick が止まる
//
// 🚨 interface + 順序つきスライスのレジストリ (issue 227 の「もう一段」案) は採らない。
// 4 つの lines/hint/routing はシグネチャも戻り値の語彙も別々 (doctorAction / rlDashAction /
// tea.Cmd) なので、共通化には opts の統一とアダプタが要り、複雑性は下がらないまま
// **毎フレームの確保が増える** (m を捕まえた closure はスライスへ逃げるので必ずヒープに乗る。
// viewLines と hintLine は 1 フレームに 1 回ずつ通る)。1 フレームの確保には上限があり
// (TestFrameAllocBudget)、issues-40 は実測 211 回に対し上限 213 = **余裕 2 回**しかない
// (敵対レビュー 2026-09-04 の実測。設定当時の 209 を引いて「4 回」と書いていたのは古い)。
// enum の switch は確保 0 (同レビューが 8 ケースすべてで実測) で同じ「単一の出典」を作れる。
type FullScreenID int

const (
	FullScreenNone FullScreenID = iota
	FullScreenRatelimit
	FullScreenDoctor
	FullScreenStatus
	FullScreenIssues
	FullScreenFiler
	FullScreenLan
	// FullScreenCount は番兵 (テストが ID を全部走査するのに使う)。新しいビューアはこの上へ足す。
	FullScreenCount
)

// String は失敗メッセージ用の名前 (テストが ID を名指しできるようにする)。
func (id FullScreenID) String() string {
	switch id {
	case FullScreenNone:
		return "none"
	case FullScreenRatelimit:
		return "ratelimit"
	case FullScreenDoctor:
		return "doctor"
	case FullScreenStatus:
		return "status"
	case FullScreenIssues:
		return "issues"
	case FullScreenFiler:
		return "filer"
	case FullScreenLan:
		return "lan"
	case FullScreenCount:
		return "count(番兵)"
	}
	return "unknown"
}

// CrossTarget は横断キー (どの全画面からも同じ板へ飛ぶキー。docs/glogx-ui-guide.md §2) の表。
// 🚨 横断キーの割当はここだけに書く。以前は画面・モードごとに手で列挙していて、列挙から漏れた
// 場所 (全 viewer の D / doctor の i s R) では押しても無言で捨てられた (issue 664)。
// 各画面は「自分でキーを解釈し切るモード (ownsKeys / 確認 / 入力)」を抜けた後にこれを引き、
// 行き先が自分なら従来どおり自分の閉じる語彙 (toggle) に任せる。
func CrossTarget(key string) (FullScreenID, bool) {
	switch key {
	case "i":
		return FullScreenIssues, true
	case "s":
		return FullScreenStatus, true
	case "R":
		return FullScreenRatelimit, true
	case "D":
		return FullScreenDoctor, true
	case "F":
		return FullScreenFiler, true // treefiler (filer_view.go。docs/treefiler-spec.md §0.3)
	case "L":
		return FullScreenLan, true // mac-lan-manager (lan_view.go。issue 747)
	}
	return FullScreenNone, false
}
