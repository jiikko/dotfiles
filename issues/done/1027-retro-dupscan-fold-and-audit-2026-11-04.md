# 1027 (retro): dupscan の表の行の畳みと 2 回目の監査

起票日: 2026-11-04

## 概要

treefiler で「統合できるメソッドはあるか」を dupscan で調べ、出た雑音 (sweep の節 2 の表の行) を dupscan 側で直し (1021)、続けて dupscan の 22 観点の監査 (1025) を回したセッションの振り返り。

## どこで踏んだか

1. **番号を取る push で、未検証の WIP の commit が master に出た**
   - 監査の worktree で、修正 2 本を WIP として commit した後に 1025 の骨組みを commit し、`git push origin HEAD:master` で番号を取った。
     push はブランチ単位なので、骨組みより前の WIP 2 本 (typed の鍵の修正・README) も `WIP:` の件名のまま master に出た (push の出力は `6b36a1484..30a75f220`。この範囲に WIP 2 本と骨組みが並ぶ)
   - その時点で回していたのは typed のテストと変異だけで、lint・gate・root の make test は push の後に回した (rc=0 だった)。件名は直せない
   - claim の規約 (`issue-rules.d/claim-issue-in-next-and-push.md`) には「push はブランチ単位なので、他に未 push の commit があれば一緒に飛ぶ」と書いてあるが、
     採番の骨組みの push (`issue-rules.md` の「採番したら即 commit & push」) には同じ注意が無かった

2. **検出器のテストの fixture を何度も作り直した** (1021。回数は会話の記録で、1021 には書いていない)
   - 同じ形のクロージャを置いたつもりが、ファイルをまたいで 1 つの組になる・L2 でリテラル違いが同じ組になる・囲む関数どうしが同じ形で節 1 に吸われる、の順に外れた
   - 本物のバイナリで fixture を回して (`dupscan sweep` に直接かける) 初めて、どの節・どの組に入るかが分かった

3. 監査の記録に母集合の数を読み違えて書いた (`wc -l` の合計の行と件数を取り違え、61 ファイル・13,912 行と書いた。正しくは 54・13,999)。反証レビューが見つけた

## 提案

- **A. 採番の骨組みを push する前に `git log --oneline origin/<既定のブランチ>..HEAD` の出力を読み、骨組み以外の commit があるなら、その push を止める**
  (骨組みだけを origin から切った一時の worktree で commit して push する)
  - 切り出し先: `_claude/issue-rules.md` の「採番したら即 commit & push する」の項への追記 (既存への追記。claim の規約と同じく、数えずに出力を読む形にした)
- 却下: 2 (fixture の作り直し)。「テストの fixture を本物に 1 回通して、狙った状態に入ったかを見る」は `survey-receiver-guards-before-passing-new-values.md` の「その状態に入ったことを assert する」と
  `measure-external-cli-streams-separately.md` の「本物を呼んで取った値を貼る」で既に言われている (前者の文脈は HTTP の fake だが、形は同じ)。今回は従うのが遅れただけで、新しい規範は無い
- 却下: 3 (数の読み違え)。「数は機械で数える」(CLAUDE.md「レビュー方針」) は守っていて、出力の読み違えだった。反証レビューが止めたので、規範の追加は要らない

## 反証レビュー (sonnet 1 本、読み取りのみ)

P2 2 件・P3 4 件。採った: 追記を「数える」と書き、claim の規約の「出力をそのまま見せる (自分で数えた件数はずれる)」と向きが逆 → 出力を読む形に直して短く畳んだ /
同じ注意が claim の規約・hook・worktree-per-session にあり、採番を名指ししたものは無い → claim の規約を指す形にした / 「同じ push で出た」の根拠 → push の出力の範囲を書いた /
「採番は claim より頻繁」は根拠が無い → 消した / fixture の回数は 1021 に無い → 会話の記録だと書いた。

## 局所の手直し (このセッションで直した)

- `src/dupscan/sweep_cmd.go` の `splitTableGroups` のコメントに書いた実数 (treefiler の items で 3 組 28 件) は、treefiler の表が変わると古くなるので外した

## 進捗

- [x] A: `_claude/issue-rules.md` に追記 (commit「retro 1027: 採番の骨組みの push もブランチ単位 (未 push の commit が混ざるなら push しない)」)
- [x] 局所の手直し: 同じ commit
