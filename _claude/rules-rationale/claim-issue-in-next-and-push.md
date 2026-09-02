# issue に着手するときは `issues/next/` へ移して claim し、即 push する — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/claim-issue-in-next-and-push.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## 2026-09-02 — 同じ retro の切り出しを 2 マシンが同時にやった

このマシンのセッションが retro 164 の残課題 (項目 1 / 2 / 5) を切り出している最中に、
**別マシンのセッションが同じ retro の項目 2 と 5 を切り出して先に push していた**。

衝突の中身:

| 項目 | このマシン | 別マシン | 結果 |
|---|---|---|---|
| 2 (pathspec は cwd 相対) | `commit-with-pathspec.md` に節を新設 | 同じファイルに同趣旨の節を新設 | **content conflict**。相手版を採用し、こちらの固有分 1 行だけ足した |
| 5 (同じ判定を 2 箇所に描く) | **却下**すると判断 | **採用**して `mutation-verify-new-tests.md` に追記 | 相手が先に入れていたので却下を取り下げ |

どちらのセッションも規律どおりに動いていた (実測して書く / 却下理由を残す)。それでも
**片方の成果は捨てるかマージし直すコストを払った**。防げた地点は 1 つだけで、
**着手を宣言していなかったこと**。

## なぜ「push まで」が条件なのか

claim がローカルにある間、他マシンから見える状態は「誰も着手していない」と**完全に同じ**。

- `git log` にも `git status` にも出ない (相手の repo には存在しない commit)
- issue ファイルの位置は相手にとって `issues/` 直下のまま
- author も同一 (全セッションが同じ git identity) なので、後から履歴を見ても誰の claim か分からない

つまり **push されていない claim は、情報として 0**。「commit した」で止めると、
claim したつもりの側だけが安心し、相手は何も知らないまま同じ issue を始める。

## なぜ claim だけを単独 commit にするのか

claim を実装と同じ commit に入れると、**push できる条件が実装の完成に縛られる**。
検証が終わるまで push しない運用だと、その間ずっと claim は見えない。

claim は「これから触る」という**宣言**なので、成果物の完成度と独立に出せる必要がある。
pathspec commit ([`commit-with-pathspec.md`](../rules/commit-with-pathspec.md)) がそのまま使える。

## hook で強制できる範囲と、できない範囲

`_claude/hooks/next-claim-push.sh` は PostToolUse(Bash) で `issues/next/` への移動を検出し、
「単独 commit して push したか」を注入する。ただし:

- **glogx の issues viewer の `n` キーは Bash を通らない** (Go が直接 rename する) ので発火しない。
  人が viewer で付けた目印を push するのは人の側の運用
- **「着手前に fetch する」は検出できない** (何もしていないことは観測できない)
- **push を強制しない**。強制すると「push できない事情があるとき」に手が止まる。hook は
  注意を出すだけにして、判断は残す

この非対称 (検出できるのは移動だけ) を承知のうえで、**取りこぼすより出す側へ倒している**。
誤発火の害は注意書きが 1 回出るだけ。

## 却下した設計案

- **`issues/claimed/` を新設する**: 状態語彙が 1 つ増える。`next/` は既に「次にやる」= 着手予定の
  意味を持っており、claim との差は「宣言したか」だけ。分けるほどの違いが無い
- **claim にマシン名・セッション名を書く**: 誰が claim したかは `git log` の commit で追える。
  ファイル本文に書くと、done へ移すときに消し忘れて嘘が残る (既読ヘッダーを使わない規律と同型)
- **hook で push まで自動実行する**: 破壊的ではないが**外向きの操作**を hook が勝手にやる形になる。
  push はユーザーの領分 ([`no-unauthorized-branch-switch.md`](../rules/no-unauthorized-branch-switch.md) と同じファミリー)
