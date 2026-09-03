package main

// 「今どの全画面ビューアが出ているか」の単一の出典。
//
// 全画面ビューア = 開いている間コミット一覧の窓ごと画面を差し替え、キーを全部飲むもの
// (issues / status / 残量ダッシュボード / doctor)。**同時に開くのは高々 1 枚**で、
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
// 新しい全画面ビューアは fullScreenCount の直前に ID を足す。足した瞬間に 2 段で捕まる:
//
//  1. **lint** — `exhaustive` (`.golangci.yml`) が「default なし switch は enum を全部書く」を
//     強制するので、ID を足すと activeFullScreen を読む全 switch が `make lint` で赤くなる。
//     🚨 **これらの switch に `default:` を書かないこと**。この repo は
//     `default-signifies-exhaustive: true` なので、default を 1 つ書いた switch は
//     網羅チェックから外れ、その配線だけが黙って取り残される
//  2. **テスト** — `TestFullScreenSurfacesWireEverySite` (fullscreen_test.go) が ID ごとに
//     全サイトの**挙動**を通す (lint は「case を書いたか」しか見ない。中身が空でも通る)
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
// (TestFrameAllocBudget)、issues-40 の余裕は 4 回しかない。enum の switch は確保 0 で
// 同じ「単一の出典」を作れる。
type fullScreenID int

const (
	fullScreenNone fullScreenID = iota
	fullScreenRatelimit
	fullScreenDoctor
	fullScreenStatus
	fullScreenIssues
	// fullScreenCount は番兵 (テストが ID を全部走査するのに使う)。新しいビューアはこの上へ足す。
	fullScreenCount
)

// String は失敗メッセージ用の名前 (テストが ID を名指しできるようにする)。
func (id fullScreenID) String() string {
	switch id {
	case fullScreenNone:
		return "none"
	case fullScreenRatelimit:
		return "ratelimit"
	case fullScreenDoctor:
		return "doctor"
	case fullScreenStatus:
		return "status"
	case fullScreenIssues:
		return "issues"
	case fullScreenCount:
		return "count(番兵)"
	}
	return "unknown"
}

// activeFullScreen は今出ている全画面ビューア (無ければ fullScreenNone)。
//
// 高々 1 枚しか出ない前提なので順序は本来どれでもよいが、**ここを唯一の順序**にすることで
// 描画・hint・routing が同じ 1 枚を選ぶことが構造的に保証される (以前は場所ごとに順序が
// 違い、前提が崩れたときにどれが勝つかが場所によって変わった)。
func (m *browseModel) activeFullScreen() fullScreenID {
	switch {
	case m.rlDash.visible():
		return fullScreenRatelimit
	case m.doctorOv.visible():
		return fullScreenDoctor
	case m.statusOv.visible():
		return fullScreenStatus
	case m.issuesOv.visible():
		return fullScreenIssues
	}
	return fullScreenNone
}

// fullScreenActive は全画面ビューアが 1 枚でも出ているか。
func (m *browseModel) fullScreenActive() bool { return m.activeFullScreen() != fullScreenNone }
