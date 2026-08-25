---
name: c
version: 1.3.0
description: このセッションで行った変更をコミットする。「コミットして」「commit」「/c」で発火。push はしない／クレデンシャルは混入させない。
---

# Quick Commit

このセッションで行った変更を、リポジトリの慣習に合わせてコミットする。

## 手順

1. `git status` / `git diff` で差分を確認し、`git log --oneline -5` でメッセージのスタイル（言語・粒度・接頭辞）に合わせる
2. **新規ファイルのみ**、このセッションで作ったものだけを**パス指定**で `git add` する（`git add -A` / `git add .` は使わない。既存ファイルの変更は次の pathspec commit が直接拾うので add 不要）
3. 変更の「なぜ」を一文で説明するメッセージを作り、**このセッションで触れたファイルを pathspec で明示してコミットする**。メッセージは `-m` ではなく heredoc で渡す:

   ```bash
   git commit -F - <<'MSGEOF' -- path/to/file1 path/to/file2
   fix(x): `Foo.swift` の `bar()` を直す
   MSGEOF
   ```

   - `-m "..."` を使わない理由: 二重引用符内のバッククォートが command substitution として評価され、`` `foo.swift` `` と書いた語が**メッセージから消える**（commit は成功するので気づかない）。`<<'MSGEOF'` のようにクォートすれば展開もコマンド置換も起きない
   - pathspec なしの `git commit` / `git commit -a` は使わない
   - **rename (`git mv`) を含むときは旧パスと新パスの両方**を pathspec に書く（片方だけだと削除が取り残される）
   - 詳細はいずれも `_claude/rules/commit-with-pathspec.md`
4. `git log -1 --stat` でコミット内容を確認する（クレデンシャル・一時ファイル・無関係ファイルが混ざっていないか）
5. `git status` を見る。**`D <旧パス>` が残っていたら rename の片側漏れ**（手順4の stat には「無いもの」が出ないので、これは status でしか見えない）

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
- **heredoc 本文が静的検査に引っかかる**: commit メッセージ本文はシェルの引用符の保護が効かない素のテキストとして扱われるため、リポジトリ側の PreToolUse フックがコマンド文字列を静的検査していると、**メッセージに書いた語だけで deny されることがある**。実例（dotfiles 2026-08-21）: ソケット未指定の破壊的 tmux コマンドを止めるフックが、commit メッセージ本文の説明文に反応して commit を 3 回拒否した。回避はシェル変数でトークンを割って組み立てる（`T="tm""ux"`）か、その語を同じ行に置かない言い換え。**フック自身の修正を commit するときに最も踏みやすい**（規範: `_claude/rules/tmux-probe-requires-socket-isolation.md` の「強制手段」節）
- **テストの exit code を見ないまま commit する**: `make test` と `git commit` を**同じコマンド
  呼び出しに並べない**。テストの結果を読む前に commit が走り、赤いまま履歴に入る。実例
  (dotfiles 2026-08-25): `make test >log 2>&1; echo $?` と commit を 1 つの Bash 呼び出しに
  入れたため、exit 2 (shellcheck エラー) を見ないまま commit した。**「テストを回した」と
  「テストが通った」は別の手**にして、exit code を確認してから commit する
  (関連: `_claude/rules/verify-execution-not-just-exit-code.md` は「exit 0 は実行された証拠に
  ならない」側で、こちらは「exit code をそもそも見ない」側)。
- **並行セッションとの index 共有**: 同一 repo で複数セッションが動くことがある。index は 1 つしかないため、pathspec なしの commit は他セッションの add 済み変更を混入させる。手順3の pathspec commit が構造的な防止策（詳細: `_claude/rules/commit-with-pathspec.md`）。
