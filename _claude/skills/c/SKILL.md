---
name: c
version: 1.2.0
description: このセッションで行った変更をコミットする。「コミットして」「commit」「/c」で発火。push はしない／クレデンシャルは混入させない。
---

# Quick Commit

このセッションで行った変更を、リポジトリの慣習に合わせてコミットする。

## 手順

1. `git status` / `git diff` で差分を確認し、`git log --oneline -5` でメッセージのスタイル（言語・粒度・接頭辞）に合わせる
2. **新規ファイルのみ**、このセッションで作ったものだけを**パス指定**で `git add` する（`git add -A` / `git add .` は使わない。既存ファイルの変更は次の pathspec commit が直接拾うので add 不要）
3. 変更の「なぜ」を一文で説明するメッセージを作り、**このセッションで触れたファイルを pathspec で明示してコミットする**:

   ```bash
   git commit -m "..." -- path/to/file1 path/to/file2
   ```

   pathspec なしの `git commit` / `git commit -a` は使わない（理由は `_claude/rules/commit-with-pathspec.md`）
4. `git log -1 --stat` でコミット内容を確認する（クレデンシャル・一時ファイル・無関係ファイルが混ざっていないか）

## ルール

- コミットメッセージは変更の「なぜ」を簡潔に説明する（「何を」変えたかは diff で分かる）
- Co-Authored-By は付けない
- push はしない
- .env やクレデンシャルファイルはコミットしない

## 落とし穴 (Gotchas)

- **`git add -A` / `git add .` の巻き込み**: 意図しない一時ファイル・ビルド生成物・`./tmp/` 配下を一緒にステージしがち。手順2の通り必ずパス指定で add する。
- **クレデンシャルの混入**: `.env`、`*.pem`、`id_rsa`、APIキーを含む設定ファイルは、たとえ差分に出ていてもコミットしない。`.gitignore` 漏れを見つけたらコミット前に指摘する。
- **dirty なサブモジュール**: サブモジュールに未コミットの変更があるまま親の参照だけ進めると CI が壊れる。`git status` でサブモジュールの dirty を確認し、ある場合はユーザーに確認する（このスキルは push しないため、サブモジュール側の push 要否も伝える）。
- **`git stash` 禁止**: 退避が必要になっても stash は使わない（共通ルール）。別ブランチにコミットするかユーザーに確認する。
- **既存のステージ済み変更**: 自分が意図していないファイルが既に `git add` 済みのことがある — **並行して動いている別の Claude セッションの作業中データかもしれない**。pathspec commit なら混入しないので、unstage / reset せずそのまま放置する（勝手に片付けない）。
- **並行セッションとの index 共有**: 同一 repo で複数セッションが動くことがある。index は 1 つしかないため、pathspec なしの commit は他セッションの add 済み変更を混入させる。手順3の pathspec commit が構造的な防止策（詳細: `_claude/rules/commit-with-pathspec.md`）。
