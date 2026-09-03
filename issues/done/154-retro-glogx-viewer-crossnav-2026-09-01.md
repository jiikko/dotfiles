# 154 retro: glogx の全画面ナビ往復 (ratelimit ⇄ issues/status) — 2026-09-01

起票日: 2026-09-01

対象セッション: `d01c52a` (相互遷移の実装) / `81641c1` (敵対レビュー P2 への回答) /
`44c95fc`・`ed7bfa7` (issue 075 の done 送りと、その訂正)。

## 1. 「閉じたはずの画面が toggle で裏返る」穴を、設計中に自力で見つけられた

横断先を `toggle()` で開く形にしたため、**相手が既に開いていると「開く」が「閉じる」に化ける**。
実際に起動時復元 (`issuesRestoreMsg`) が非同期で着地する経路が残っており、status 用の同型
ガードが既にあった (2026-08-06 の敵対レビューで見つかったもの) ことに気づいてガードを広げた。

- 効いたのは「既存の同型ガードのコメントを読んだ」こと。あのコメントが「なぜ弾くか」を
  書いていなければ、`|| m.rlDash.visible()` を足す発想には至らなかった
- 切り出し先: **却下** (新しい規範は無い。既存の
  [`pending-issue-rationale-in-code.md`](../_claude/rules/pending-issue-rationale-in-code.md) が
  効いた実例であって、追加ルールにはならない)

## 2. exhaustive linter が「bool 2 本 → enum」の設計変更を後押しした

`handleKey` の戻り値を `(closed, refresh bool)` から `rlDashAction` へ変えたところ、
`golangci-lint` の `exhaustive` が `switch` の case 漏れ (`rlDashSwallow`) を弾いた。
握り潰しを**明示的な case** として書くことになり、意図が型に乗った。

- 🚨 `make lint` を回すまで気づかなかった (`go build` は通る)。**Go の変更は
  `go test` だけでなく `make lint` まで回してから commit する**が、既に memory
  (`make lint/test before commit`) にある
- 切り出し先: **却下** (既存 memory どおり)

## 3. done へ送った issue に、実測しないまま結論を書いた (この session 最大の誤り)

issue 075 を done へ送るとき、決着節に現在の設定値を実測せずに書いて commit した
(`44c95fc` → `ed7bfa7` で訂正)。

- 切り出し先: **`_claude/rules/move-report-conclusions-to-issues.md` へ追記済み**
  (「同型: issue を `done/` へ送るときは、本文が前提にしている『現状』を実測する」)。
  規範はそちらが正本で、経緯と実例は同名の `rules-rationale/` に置いた

## 4. hint に足したキーが画面に出ていなかった

status viewer の hint へ `R: 残量` を足したが、実測すると hint は 155 桁で、popup 実幅 84 では
`d: diff` 以降が丸ごと切れている = **足した案内は一度も表示されていない**。テストは
`strings.Contains(v.hint(), "R: 残量")` で green になる。

- issues viewer 側には幅テスト (`TestIssuesViewHintFitsPopupWidth`) があるが status には無い、
  という非対称が原因
- 切り出し先: **issue 155 として起票済み**
- 🚨 一般化して「表示されるかを assert しろ」というルールにはしない。既存の
  [`mutation-verify-new-tests.md`](../_claude/rules/mutation-verify-new-tests.md) の
  「その assert が見ている量は、壊れたときに動くのか」が既に同じことを言っている

## 5. 並行セッションとの issue 番号調整に 3 往復かかった

`dotfiles-1d` が 150〜153 を順に予約し、そのたびにこちらの採番可能範囲が動いた。実害は無い
(こちらは起票が最後だったので待つだけで済んだ) が、`issues/README.md` が既に
「番号を取る前に一声かける」を明文化しているとおりの運用コストが出た形。

- 切り出し先: **却下** (規約どおりに動いた結果であって、改善案が無い。番号を予約制にする
  仕組みを作るコストの方が高い)

## 残課題

なし (2026-09-01 に決着)。項目 3 は rules へ追記、項目 4 は issue 155 として起票、
項目 1・2・5 は理由つきで却下した。
