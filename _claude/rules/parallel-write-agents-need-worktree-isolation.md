# 書き込み権限のエージェントを 2 体以上並行させるなら、必ず worktree を分ける

## ルール

- **`-s workspace-write` の codex（や書き込み可能なサブエージェント）を 2 体以上同時に走らせるとき、
  各体を別の git worktree に置く**。同一 working tree で並行させない
- 「担当ディレクトリが重ならないから大丈夫」で同一 tree に置かない。**重ならない保証は指示側の想定でしかなく、
  エージェントは想定外のファイルを触りうる**（未使用 import の掃除、テストヘルパーへの追記、
  生成物の再生成など、指示に無くても正当な理由で共有ファイルへ手が伸びる）
- 1 体だけなら共有 working tree でよい。分離コストを払うのは 2 体目から

## なぜ（起源: ThumbnailThumb #435, 2026-08-11）

構造化ログ移行の最終盤で、コード修正とドキュメント修正の codex を**同じ working tree で並行実行**した。
担当は「`.swift` / `.swiftlint.yml`」と「`.md`」で重ならない想定だった。

結果的に事故は起きなかったが、それは**運が良かっただけ**:

- 両者とも `issues/done/435-*.md` に手が届く位置にいた（ドキュメント側の担当、コード側も
  「lint ルールの説明を issue に書く」余地があった）
- ドキュメント側が終了報告で「作業中に別の `.swift` / `.swiftlint.yml` 変更が並行して出現した。
  これらには触れず保持している」と**明示的に述べていた**ため、後から上書きが無かったと確認できた。
  この報告が無ければ、片方の編集が消えたかどうかを知る手段がなかった

同一 tree の並行書き込みで起きうること:

- 後から書いた側が、先に書いた側の編集を含むファイルを読まずに上書きする（**サイレントに消える**）
- 片方が `xcodegen generate` などで生成物を再生成し、もう片方の未反映の変更を巻き込む
- 一方が build/test を走らせている最中に他方がソースを書き換え、**どちらの状態を検証したのか
  分からない green** が出る（最も危険。検証の意味が消える）

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

## 例外

- **read-only（`-s read-only`）の並行は分離不要**。レビュー・調査・トリアージを何体並べてもよい
  （本ルールは書き込みだけを対象にする）
- 1 体だけの書き込み + main agent の待機

## 関連

- [`commit-with-pathspec.md`](commit-with-pathspec.md) — 同一 index を複数主体が触る問題。
  本ルールは「同一 working tree を複数主体が書く」版
- `~/.claude/skills/codex-drive/SKILL.md` の `[2p]` 競作 — 競作では worktree 分離を必須にしている。
  本ルールはそれを**競作以外の並行実装にも広げる**もの
