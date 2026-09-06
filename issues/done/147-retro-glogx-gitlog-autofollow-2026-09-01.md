# retro: glogx の git log 自動追従 (2026-09-01)

「glogx で git log を表示中は 1 分ポーリングして変更を反映して。イベント駆動って無理だよね？」
という依頼。実装は 5bd6715 / 起票は 0abfaab。

## 良かった点

1. **「無理だよね？」に対して先行実装を確認してから答えた**。`issues_watch.go` が既に
   「イベントで起こし、指紋で判定する」を持っていたので、方式ごと再利用できた。前提を
   確かめずに「できます」とも「無理です」とも言わなかったのが正解だった
2. **並行セッション (dotfiles-78) との調整が機能した**。SendMessage で担当ファイルを交換した
   結果、(a) 予約しようとした issue 番号 137〜139 が既に使用済みだと判明 (相手の認識も古かった)、
   (b) tui.go の競合を worktree 退避で構造的に回避、(c) master の lint 失敗が相手のコミット由来
   だと切り分けられた
3. **変異検証が「守っていないテスト」を 2 本捕まえた**。`TestGitLogFPDiscardsMeasurementTakenBeforeSelfReload`
   は pull の演出中の見送りに助けられて green になっていて、狙った不変条件を一切見ていなかった
   (M13/M16 が GREEN)。演出を落として判定を絞ったら RED になった

## 反省点

### 1. read-only レビュワーを走らせている間に実装を編集した

「壊す」観点のレビュワーが最初の走査で見つけた P1 3 件は、報告が届いた時点で**既に自分が
直していた**。レビュワーは再複製して再検証する手間を払っており、報告の半分が
「もう直っている」の確認に使われた。

`parallel-write-agents-need-worktree-isolation.md` は「read-only のレビュー中に同じファイルで
**変異検証**を回さない」と書いているが、今回は変異ではなく**通常の修正**で同じことが起きた。
規範を「レビュー中はそのファイルを書き換えない (修正も変異も)」へ広げるか、
「気づいた修正はレビュー結果が届くまで別の worktree でやる」を足すのが妥当。

- 切り出し先候補: [`_claude/rules/parallel-write-agents-need-worktree-isolation.md`](../../_claude/rules/parallel-write-agents-need-worktree-isolation.md) への追記

### 2. 変異判定スクリプトの 3 値判定を自分で壊した

M8 の判定が「テストが走っていない (第 3 の結果)」と出たが、実際は PASS だった。原因は
`go test` を `-v` なしで実行して `--- PASS:` 行を探していたこと。
`mutation-verify-new-tests.md` は「判定は runner の実行サマリ行で行う」と要求していて、
それ自体は守っていたが、**サマリ行が出る実行方法にしていなかった**。

判定スクリプトを書くときは「探している行が本当に出る実行方法か」を 1 回確かめる
(= `verify-execution-not-just-exit-code.md` をスクリプト自身にも適用する)。

- 切り出し先候補: ルール追記は不要 (既存 2 ルールの合成で導ける)。この retro に実例として残す

### 3. 敵対レビューが自己レビュー後に P1 を 3 件出した (実測 3 回目)

変異検証 11 本を全部 red にし、自分で fd 漏れ (cancelAll) も見つけた**後**で、
敵対レビューが再現つきの P1 を 3 件 (TOCTOU / 自己 reload との競合 / ctrl+d) 出した。
`adversarial-review-own-safeguards.md` の「変異を全部 red にした後でも敵対レビューは
想定の外側の P1 を出す (実測 2 回)」の 3 回目。回数の更新以外の学びは無い。

- 切り出し先候補: 同ルールの実測回数を 2 → 3 に更新

### 4. 「1 案だけ出して聞かない」を表示の判断以外にも使えた

`decide-layout-in-sample-renderer-first.md` は表示の話だが、今回は**方式**(イベント + ポーリング /
ポーリングのみ) と**反映のふるまい**(3 案) を選択肢として出したら即決した。表示以外の
「言葉で説明されるより並べて見た方が速い」判断にも同じ形が効く。

- 切り出し先候補: なし (既存ルールの適用範囲を広げただけ)

## 切り出し結果 (2026-09-01)

- 反省点 1 → [`_claude/rules/parallel-write-agents-need-worktree-isolation.md`](../../_claude/rules/parallel-write-agents-need-worktree-isolation.md)
  に追記。規範を「レビュー中は**そのファイルを書き換えない**(変異検証だけでなく、思いついた
  修正・リファクタも)」へ広げ、損が両方向に出ることを明記した。実例は同名の rationale へ
- 反省点 3 → [`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md)
  と [`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md) の
  「実測 2 回」を 3 回へ更新。今回の 5 件 (TOCTOU / 自己 reload との競合 / ctrl+d / 錨の選び方 /
  センチネルの衝突) と、逆に**変異検証だけが捕まえた** 2 本を rationale に対で残した
- 反省点 2・4 → **却下** (ルール追記なし)。2 は既存 2 ルール
  (`verify-execution-not-just-exit-code` / `mutation-verify-new-tests`) の合成で導けるので、
  規範を増やさずこの retro に実例として残す。4 は既存ルールの適用範囲が広いと分かっただけで、
  新しい規範ではない

デバウンス 1 秒 / ポーリング 1 分 / トースト文言の調整は issue 145 (目視確認)、反映の非同期化は
issue 146 が持つ。この retro としての残課題は無い。
