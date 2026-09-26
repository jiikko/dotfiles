# 533 (feat): `pro-con worktree clean` で remote の取り込み済みのブランチも片付ける

起票日: 2026-09-27

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

外から動かす Claude の確認 (カード C-083) に、ユーザーが issue にすると答えた。`pro-con worktree clean` (492) はローカルの worktree とブランチしか消さない。
remote の `worktree-pc-c-*` は 522 で一度だけ手で片付けた。PG は push しない (521) ので今は増えにくいが、521 より前の PG や、例外で push したブランチは残る。

## 期待する動作

- `pro-con worktree clean` に remote のブランチも対象にする口を足す (例: `--remote`)。既定は一覧を出すだけ、`--yes` で消す (492 と同じ)
- 🚨 remote のブランチを消すのは外に出る破壊的な操作。取り込み済みの判定は 492 の `wtclean/judge.go` の `inBase` をそのまま使う (同じ判定を 2 つ作らない)。
  消す直前に `git fetch --prune` して取り直し、1 本ずつ消す。完了していないカードのブランチは消さない
- テストは偽の remote (`git init --bare` を `mktemp -d` に) で行い、本物の remote には触らない。敵対的レビューを最終ゲートにする

## 関連

- 492 (worktree clean) / 521 (PG は push しない) / 522 (remote の手での片付け)

## 進捗 (2026-09-27, C-086)

- [x] `pro-con worktree clean --remote [--yes]` (`src/pro-con/wtclean/remote.go`)。`--remote` なしの挙動は変えていない
  - 判定は `cardOf` / `trunk` / `inBase` をそのまま使う。`worktree-pc-<カード>-<後ろ>` (-r1・-rebased など作り直した版) は元のカードで見る (後ろの `-<語>` を 1 つずつ外す)
  - 一覧の前にも `git fetch --prune origin`。`--yes` では 1 本ずつ、材料と origin を取り直して判定し直し、判定した先端を lease にして
    `git push --force-with-lease=refs/heads/<b>:<sha> origin :refs/heads/<b>`。消えたかは送り先 (pushurl) の `ls-remote` で確かめる
  - 取り込む先の祖先でない先端 (cherry-pick で入った) は、消す前に `refs/pro-con/removed/origin/<ブランチ>/<sha>` に残す (ローカルの keepReflog と同じ置き場)
  - origin の無い repo は飛ばす (失敗にしない)
- [x] テスト: 偽の remote (sandbox の下の `git init --bare`)。テストの二進では origin の取得先・送り先が sandbox の外なら fetch の前にも push の前にも拒否する。変異 17 本 + CLI 3 本を 1 本ずつ当てて全部 red
  - 🚨 開発機の `~/.gitconfig` は `fetch.prune=true`。fixture の repo で `fetch.prune=false` にしないと、コードの `--prune` を外しても緑のままだった
- [x] 敵対的レビュー (最終ゲート。使い捨ての repo と bare で再現)。取り込んでいない commit を消す経路は見つからなかった (P0/P1 なし)。直したもの:
  - [P3] remote を消すと、祖先でない先端を手元で辿れなくなる → refs/pro-con/removed/origin/ に残す
  - [P3] pushurl が取得先と違うと、消したのに「残っている」と失敗にする → 送り先で確かめる
  - [P3] origin の無い repo が設定にあると毎回 rc=1 → 飛ばす
  - [P3] テストの守りが push にしか掛かっていない → fetch の前にも掛ける
  - 記録だけ: fetch の refspec が狭い repo (single-branch clone) では、refspec の外の追跡 ref が古いまま残り、消そうとして lease で断られて毎回「失敗」になる (消す事故にはならない。dotfiles は既定の refspec)
- [ ] 取り込み後、本物で `pro-con worktree clean --remote` の一覧を人が見てから `--yes` (522 の 52 本 / 9 本と照らす。-r1 等は 522 と同じく元のカードで見る)

## 決着 (2026-09-27、カード C-094)

- 外から動かす Claude の確認に、ユーザーが「そもそも remote に push しないって話になっているので push しないで」と答えた。PG は 521 から push しないので、remote に片付けるものが出ない
- `--remote` の口は外す (540)。本物で `--remote` を回す確かめはしない。remote に残っている 7 本も、消すのが push になるので消さない
