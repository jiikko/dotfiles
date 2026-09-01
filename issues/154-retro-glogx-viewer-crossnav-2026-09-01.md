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

- ⚠️ `make lint` を回すまで気づかなかった (`go build` は通る)。**Go の変更は
  `go test` だけでなく `make lint` まで回してから commit する**が、既に memory
  (`make lint/test before commit`) にある
- 切り出し先: **却下** (既存 memory どおり)

## 3. done へ送った issue に、実測しないまま結論を書いた (この session 最大の誤り)

issue 075 を done へ送るとき、決着節に「現状 `status-interval 0`」「再現には 1 へ戻す必要が
ある」と書いて commit した (`44c95fc`)。**実測すると `_tmux.conf:73` も稼働サーバも 1** で、
前提が逆だった (`ed7bfa7` で訂正)。

- 何が起きたか: issue 本文の調査メモ (「`status-interval 0` でブレが消えたのなら〜」という
  **仮定法の記述**) を、現在の設定値だと読み違えた。`tmux show -g status-interval` は 1 コマンド
  で済むのに、閉じる作業を「文章を書くだけ」と見なして測らなかった
- 実害: 訂正しなければ、**再開の trigger が実行不能な手順**として残った (「1 に戻して再現を見る」
  = 既に 1 なので何もできない)。しかも done 配下なので次に読まれるのは再発したときだけ
- 切り出し先: **`_claude/rules/` へ**。既存ルールは「性能の主張」
  ([`perf-claims-need-measurement.md`](../_claude/rules/perf-claims-need-measurement.md)) と
  「外部 CLI の観測」
  ([`measure-external-cli-streams-separately.md`](../_claude/rules/measure-external-cli-streams-separately.md))
  を扱うが、**「issue を閉じるときに、その issue が前提にしている環境値を実測する」**は
  どちらにも無い。案: 既存の
  [`move-report-conclusions-to-issues.md`](../_claude/rules/move-report-conclusions-to-issues.md)
  に一節を足す (新規ルールを増やさない) — 「done へ送る前に、本文中の『現状は〜』を実測で
  確認する。測れない値なら『未実測』と明記する」
- ⚠️ ユーザーの判断待ち (このセッションでは切り出さない)

## 4. hint に足したキーが画面に出ていなかった

status viewer の hint へ `R: 残量` を足したが、実測すると hint は 155 桁で、popup 実幅 84 では
`d: diff` 以降が丸ごと切れている = **足した案内は一度も表示されていない**。テストは
`strings.Contains(v.hint(), "R: 残量")` で green になる。

- issues viewer 側には幅テスト (`TestIssuesViewHintFitsPopupWidth`) があるが status には無い、
  という非対称が原因
- 切り出し先: **issue 155 として起票済み**
- ⚠️ 一般化して「表示されるかを assert しろ」というルールにはしない。既存の
  [`mutation-verify-new-tests.md`](../_claude/rules/mutation-verify-new-tests.md) の
  「その assert が見ている量は、壊れたときに動くのか」が既に同じことを言っている

## 5. 並行セッションとの issue 番号調整に 3 往復かかった

`dotfiles-1d` が 150〜153 を順に予約し、そのたびにこちらの採番可能範囲が動いた。実害は無い
(こちらは起票が最後だったので待つだけで済んだ) が、`issues/README.md` が既に
「番号を取る前に一声かける」を明文化しているとおりの運用コストが出た形。

- 切り出し先: **却下** (規約どおりに動いた結果であって、改善案が無い。番号を予約制にする
  仕組みを作るコストの方が高い)

## 残課題

- [ ] 項目 3 の切り出し (`move-report-conclusions-to-issues.md` への追記) をやるか決める
