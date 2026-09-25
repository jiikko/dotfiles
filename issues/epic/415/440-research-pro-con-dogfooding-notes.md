# 440 (research): pro-con を Claude が使って pro-con を開発する (dogfooding) — 手触りと改善案

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

Claude (対話の session) が PM の役をして、pro-con で PG を起動し、pro-con 自体の開発を回す。使ってみた手触りと改善案をここに書く
(2026-09-25 にユーザーが依頼: 「pro-con を Claude 自身が立ち上げて使った場合、手触りとか改善案とかを別の issue に書いて」)。

## やり方

- PM の役は Claude が CLI (`pro-con card add / plan / answer / close`) で行う (PM を pro-con が起こす仕組みはまだ無い = 437)
- 1 枚目のカードは 428 (attach 中の指示をカードに残す) の予定。`--limit 1`。PG は `claude --bg -w pc-c-NNN` の worktree で作業し、
  Claude がレビューして cherry-pick → make test → push する
- PG が pro-con のコードを書いている間、Claude は pro-con のコードを触らない (PM に徹する)

## 手触り

(使ったら書く。うまくいった話より、詰まった所・回りくどかった所・画面で分からなかった所を書く)

## 改善案

(使ったら書く。issue に切り出したら番号を書く)
