# 書き込み権限のエージェントを 2 体以上並行させるなら、必ず worktree を分ける

## ルール

- **`-s workspace-write` の codex（や書き込み可能なサブエージェント）を 2 体以上同時に走らせるとき、
  各体を別の git worktree に置く**。同一 working tree で並行させない
- 「担当ディレクトリが重ならないから大丈夫」で同一 tree に置かない。**重ならない保証は指示側の想定でしかなく、
  エージェントは想定外のファイルを触りうる**（未使用 import の掃除、テストヘルパーへの追記、
  生成物の再生成など、指示に無くても正当な理由で共有ファイルへ手が伸びる）
- 1 体だけなら共有 working tree でよい。分離コストを払うのは 2 体目から
- **「並行」は同時起動に限らない。前の write 実行の検証・commit が終わる前に次の write 実行を同じ tree で
  始めるのも 2 体並行と同じ**。実例 (obaket 611/612, 2026-08-27): 598 の codex 実装が終わって検証待ちの間に
  597 の codex 実装を同じ tree で起動した。ファイル集合は重ならなかったが、598 の `make lint` / `make build` を
  回す tree に 597 の中途状態が混ざり、598 単体の検証にならなかった (597 完了まで 598 の検証を遅らせて凌いだ)。
  前の成果物を commit するか、後の実行を worktree に出してから始める
- **read-only のレビュー / 検証エージェントを走らせている間は、そのファイルを書き換えない**
  (変異検証だけでなく、**自分で気づいた修正・リファクタも含む**)。どちらかを worktree へ分ける。
  損は両方向に出る:
  - レビュワーは「外部プロセスに書き換えられ旧バグが再導入された状態」を **P1 として報告して
    くる**が、実体は自分の変異中のスナップショットで、裏取りコストが丸ごと無駄になる
    (2026-08-21 実例)
  - 逆に**先に直してしまうと**、届いた報告の一部が「もう直っている」の確認に費やされ、
    レビュワーは再走査を強いられる (2026-09-01 実例)
  気づいた修正はメモしておき、**レビュー結果が届いてからまとめて当てる**
- **レビュー / 検証エージェントが終わったら `git status` で残骸を確認する**。一時ファイルの
  置き場と後始末を毎回明示していても残る (2026-08-21: red team が検証用の `*_test.go` を
  3 本 working tree に残し、ビルドが壊れた状態だった)

## なぜ

起源: ThumbnailThumb #435, 2026-08-11。根拠・起源・実例は `~/dotfiles/_claude/rules-rationale/parallel-write-agents-need-worktree-isolation.md` に置く（起動時には読まれない。ルールを疑う・改訂するときに読む）。

## やること

```bash
# repo root からの絶対パスで作る（cwd がサブディレクトリだと repo 内部に worktree ができる）
root="$(git rev-parse --show-toplevel)"
git worktree add --detach "$root/../wt-taskA" <base-commit>
git worktree add --detach "$root/../wt-taskB" <base-commit>
```

- 各エージェントに `-C <worktree>` で作業根を渡す
- 統合は cherry-pick / patch で行い、**衝突は人間（main agent）が解決する**
- 終わったら `git worktree remove --force` + `git branch -D`（CLAUDE.md「worktree を残さない」）

共有ファイル（テストヘルパー、生成物、設定 YAML）が衝突しうるなら、**先に 1 体で共有部分を
確定させて commit し、その上で並行させる**と衝突が構造的に減る。

## やらないこと

- ✗ `-s workspace-write` を 2 体以上、同一 working tree で並行起動する
- ✗ 「担当ディレクトリが重ならない」を理由に分離を省く
- ✗ 並行中に main agent が同じ tree で build / test を走らせる（何を検証したか不明になる）
- ✗ read-only のレビュー中に、同じファイルを書き換える（変異検証も、思いついた修正も）

## 例外

- **read-only（`-s read-only`）の並行は分離不要**。レビュー・調査・トリアージを何体並べてもよい
  （本ルールは書き込みだけを対象にする）。**ただし自分 (main agent) が同じファイルを書き換えて
  いる間は例外にならない** — 変異検証・実装の途中状態・レビュー中に入れた修正はレビュワーには
  「他者の書き込み」に見えるので、上のルールどおり分ける
- 1 体だけの書き込み + main agent の待機

## 関連

- [`commit-with-pathspec.md`](commit-with-pathspec.md) — 同一 index を複数主体が触る問題。
  本ルールは「同一 working tree を複数主体が書く」版
- [`mutation-verify-new-tests.md`](mutation-verify-new-tests.md) — 変異検証の手順そのもの。
  レビュー中に回さない（本ルール）と併せて読む。**本ルールは「自分が並行させるとき」を
  対象にしているが、同じ checkout を使うのは自分が起こした並行だけではない** —
  別セッション (別の人・別の Claude) が同じ repo を触っていて自分からは見えない場合の
  規律は、あちらの「復元の作法」に置いた (既定は worktree)
- `~/.claude/skills/codex-drive/SKILL.md` の `[2p]` 競作 — 競作では worktree 分離を必須にしている。
  本ルールはそれを**競作以外の並行実装にも広げる**もの
