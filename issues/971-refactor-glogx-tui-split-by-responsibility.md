# 971 (refactor): glogx の tui.go (4241 行) を責務ごとに分割・解体する

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-29〜）

起票日: 2026-10-29

## 概要

ユーザー依頼 (2026-10-29「src/glogx/tui.go の責務ごとへの分割・解体」)。`tui.go` は 4241 行・関数 145 個で、直近 30 日の commit が 63 本と
src で最多。その中には treefiler・mac-lan-manager の機能追加の commit も混ざる (別の画面の変更も tui.go を触る)。
`browseModel.update` は 1 本の type switch で 659 行あり、43 種類のメッセージの処理を直接持つ。

## 何が複雑か (分割で良くなるもの)

`src/glogx/CLAUDE.md` は「行数や責務の見た目で割らない」とし、`structure_ratchet_test.go` も行数の上限を採っていない。
そこで、ファイルを分けるだけ (複雑性の移動) ではなく、次の 2 つが実際に良くなるかで分け方を決める
(`~/.claude/rules/verify-design-intent-before-refactor.md` の 3 問)。

1. **update の switch に処理が直書きされている**: CI のポーリングの結果・job の詳細・push / pull の結果・エディタを閉じた後など、
   責務の違う処理が 1 つの関数の中に並ぶ。ある責務を直すと、関係ない 42 種類の case と同じ関数の差分になる。
   → 2 文以上の case の本体を、責務ごとのファイルの `on<Msg>` メソッドへ切り出す。update は「どのメッセージを誰が受けるか」の
   表だけになる (1 文の case は既に 1 行の委譲なのでそのまま残す)。
2. **同じ責務の関数が tui.go の中で散っている**: たとえば CI の取得は 632〜730 行 (取得) と 2983〜3155 行 (ポーリング・ETA) に分かれ、
   その結果を受ける case は update の中にある。→ 責務ごとに、その関数・型・定数・`on<Msg>` を 1 つのファイルに集める。

browseModel の状態 (フィールド) は分けない。分けるのは「どの関数がどこにあるか」と update の中身だけで、状態の持ち主は 1 つのまま
(状態を分けると、同じ排他で守っていた状態が別々の単位へ割れる形になりうる。今回の範囲の外)。

## 分け方 (package main のまま。全部 browseModel のメソッド)

| ファイル | 責務 | 主な中身 |
|---|---|---|
| `tui.go` | 型・起動・配線の表 | `browseModel` の型、`newBrowseModel`・`Init`・起動時の復元、`Update` / `update` (メッセージ → 受け手の表)、`RunBrowse` |
| `browse_keys.go` | キーの入口と振り分け | `handleKey`・`updateKeyReachable`・`applyListMotion`・キーリピートの抑止・入力欄の編集キー、`onKeyPress`・`onPaste` |
| `browse_screens.go` | 全画面へのキーと結果の受け渡し | `routeKeyTo{Doctor,RatelimitDash,Issues,Status,Filer,Lan}`・`restore{Filer,Lan}Screen`、各画面の非同期の結果の `on<Msg>` |
| `browse_ci.go` | CI 状態の取得とポーリング | `fetchCIStatusesCmd`・`startCIFetch`・チャンク・`ciPoll*`・`ensureCIPoll`・`settleAwaitCI`、`onCIResult`・`onCIPoll`・`onCIPollResult` と型 |
| `browse_jobs.go` | CI の job パネルと詳細 | `openPanel`・`closePanel`・`handlePanelKey`・`handleDetailKey`・job 詳細・rerun・ETA・job ログ、`panelLines`・`detailBoxLines`、対応する `on<Msg>` と型 |
| `browse_overlays.go` | コミットに重ねる diff / PR / PR status と URL | `openDiff`・`handleDiffKey`・`openPR`・`openPRStatus`・URL のコピーと open、対応する `on<Msg>` と型 |
| `browse_log.go` | git log の読み直しと push / pull | `reloadLog`・`applyLogData`・push / pull とその演出 (`start/advance{Pull,Push}Anim`・`slideColumns`)・`refetchAfterPush`、`onPull`・`onPush` と型 |
| `browse_lifecycle.go` | 終了・再起動・更新の知らせ | `cancelAll`・`handleQuitKey`・`quitWith`・autobuild の再起動の確認・claude / codex の更新、対応する `on<Msg>` と型 |
| `browse_anim.go` | 時間で進む演出と知らせ | `tickEvery`・`maybeTick`・`tickInterval`・`onTick`、`showWarning`・`showGitFailure`・`deliverNotice`・`onToast` |
| `browse_usage.go` | 利用枠の表示 | `toggleUsage`・`toggleRatelimitDash`・`usageRefreshTick`、`onUsageRefresh` と型 |
| `browse_view.go` | 描画 | `View`・`viewLines`・`finishWindow`・案内の行・幅と頁・カーソル・背景・spinner・各画面へ渡す描画の options |

- 既存の `gitlog_watch.go`・`fullscreen.go`・`hint_surfaces.go`・`open_workspace.go`・`action_modal.go` はそのまま (既に責務ごとに分かれている)。
- 新しい 10 ファイルは `structure_ratchet_test.go` の `mainSourceFiles` と `browseModelMethodFiles` に理由つきで足す (検査の目的は
  「新しい画面が browseModel にメソッドを生やす」形を止めることで、今回は既存の状態機械の分割なので一覧の更新が正当な形)。
- `src/glogx/CLAUDE.md` の「ファイルの地図」を同じ変更で直す。

## 挙動を変えないことの担保

- 移すのは宣言の位置だけ。関数の本体・コメントは書き換えない (case の本体は、1 段字下げを戻し、本体の最後が return で無いときだけ
  `return m, nil` を足す)。update の switch に `break`・`fallthrough`・`goto` が無いこと、case の本体が `m` と `msg` 以外の外の変数を
  使っていないことを確かめた (switch の後は `return m, nil` だけ)。
- 手で写さず、`go/parser` で位置を取ってテキストを切り貼りする使い捨ての道具で行う (写し間違いを作らない)。
- commit を 2 つに分ける: ① update の case を `on<Msg>` へ切り出す (同じ tui.go の中) ② 宣言を責務のファイルへ移す。
  どちらもテストを変えずに `make -C src/glogx test lint` が通ることを等価の証拠にする (変えるのは構造の検査の一覧だけ)。
- glogx は Bench の対象 (`.claude/rules/bench-watch-after-push.md`)。push 後に Bench を見る。

## 関連ファイル

- `src/glogx/tui.go`・`src/glogx/structure_ratchet_test.go`・`src/glogx/CLAUDE.md`

## 進捗

- [ ] 設計のレビュー
- [ ] ① update の case の切り出し
- [ ] ② 責務ごとのファイルへの移設
- [ ] 実装のレビュー・push・Bench
