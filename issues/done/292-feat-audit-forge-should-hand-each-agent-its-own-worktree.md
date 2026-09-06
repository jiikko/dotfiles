# audit / forge が並行起動するエージェントへ worktree を配る

- 種別: feat（ツール / skill の配線）
- 対象: `~/.claude/skills/audit/SKILL.md`、`~/.claude/skills/forge/SKILL.md`
- 優先度: medium

## 何が問題か

[`parallel-write-agents-need-worktree-isolation.md`](../../_claude/rules/parallel-write-agents-need-worktree-isolation.md)
は「書き込み権限のエージェントを 2 体以上並行させるなら worktree を分ける」と要求しているが、
**分離を配線するのは起動側（main agent）の手作業**で、忘れても何も止めない。

実測 2026-09-06（retro [290](290-retro-glogx-audit-x2-and-21-issues-2026-09-06.md) の項目 2）:
audit skill 経由で forge のエージェント 3 体を並行起動したとき、**全員に同じ worktree
`~/wt-audit2` を渡した**。ルールは読んでいたのに配線で外している。

**検出したのは起動側ではなくサブエージェント**だった（「他のプロセスがファイルを書き換えている」
と報告してきた）。つまり現状の検出は偶然に頼っている。

## なぜ「ルールへの追記」で閉じないか

ルールは既に正しく、増やしても同じ手作業が残る。これは
**規範の不足ではなく配線の不足**なので、skill 側が配るのが構造的な解。

## 🚨 着手して分かったこと: 前提が違った（2026-09-06）

**skill は「書き込み体を並行起動」していなかった。** 実装前に両方を読んで確定させた事実:

| | 並行するもの | 書くもの |
|---|---|---|
| **forge** | `forge.js` の fan-out = investigate / review / ultra（**全部 read-only の調査・レビュー**） | Phase 2 実装・Phase 5 修正は **skill 層（main Claude）**。並行しない |
| **audit** | なし。`SKILL.md` は「各タイプを**順番に**実行し」と明記 | issue ファイル。main が書く |

出典: `~/.claude/workflows/forge.js` の冒頭が役割分担を明記している
（「skill 側: Phase 2 実装 / この workflow 側: Phase 1+1.1, 4+4.1+4.2, 4.3」）。

つまり **worktree を配る機構は要らない**。起票時の「skill の手順に組み込む」は、
書き込み体が並行する前提で書いており、その前提が成り立っていなかった。

### では何が欠けていたか

**明示**。どちらの skill も「fan-out する体は read-only」「書くのは main だけ」を書いていない。
だから読んだ側（私）が「体が書く」と思い込み、しかも audit の「順番に実行」に反して
3 体を並行起動した。**規範違反ではなく、規範が適用される場面かどうかを読み違えた**。

これは機構ではなく情報の穴なので、対処も明示にする
（[`verify-design-intent-before-refactor.md`](../../_claude/rules/verify-design-intent-before-refactor.md):
実需要が確定していない機構を先回りで作らない）。

## 提案（当初案。上記のとおり機構は作らない）

audit / forge が複数体を並行起動するとき、skill の手順に「体ごとに worktree を作って渡す」を
組み込む。最低限の形:

```sh
root="$(git rev-parse --show-toplevel)"
for i in 1 2 3; do
  git worktree add --detach "$root/../wt-<task>-$i" HEAD
done
# 各エージェントの作業根として -C / プロンプトで明示し、終了後に remove
```

- **後始末まで手順に入れる**（`git worktree remove --force`。放置 worktree を量産しない）
- read-only のレビュー体は分離不要（ルールの例外節どおり）。**分けるのは書き込み体だけ**
- 起動が 1 体なら worktree を作らない（分離コストは 2 体目から）

## 検討したが採らない案

- **hook で止める**: Agent ツールの起動は Bash を通らないので PostToolUse では見えない
  （`claim-issue-in-next-and-push.md` が glogx の `n` を hook で拾えないのと同じ構造）
- **ルールへの追記**: 上記のとおり、手作業が残るので同じ事故が再生産される

## 完了条件

- audit / forge の手順に worktree の配布と後始末が入っている
- 「1 体なら作らない / read-only は分けない」の判断基準が 1 行で書いてある
- 実際に複数体を起こす経路を 1 度通して、体ごとに別の worktree が渡ることを確認した
  （skill の文面だけ直して確認しない形にしない）

## やったこと（2026-09-06）

1. **forge `SKILL.md`**: 二層構造の説明に「fan-out する体は全部 read-only、書くのは main だけ、
   だから worktree 分離を必要としない」を明記。あわせて「将来 fan-out に書き込み体を足すなら
   その時点で配ること」「1 体なら不要 / read-only は分離不要」の判断基準も書いた
2. **audit `SKILL.md`**: 「順番に実行」を 🚨 付きに格上げし、**並行起動しないこと**と、
   どうしても並行させる場合の worktree 配布のコマンド例・判断基準を足した
3. どちらにも**実測（3 体へ同じ worktree を渡した / 検出したのはサブエージェント側だった）**を
   根拠として残した。理由を書かないと次の読み手が同じ読み違えをする
