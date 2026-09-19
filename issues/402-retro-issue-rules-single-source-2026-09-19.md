# 402 (retro): issue 運用規約の一本化 (issue 401) の振り返り

起票日: 2026-09-19

対象: retro 指針の追記 → dotfiles / obaket / ThumbnailThumb への個別追記 → 規約を
`_claude/issue-rules.md` + SessionStart hook へ一本化し、my-products の README 10 本を縮めた一連の作業。

## 1. コピーで配った規約は、注記があっても腐る

`project-templates` の README には「直接変更せず正本を変更してください」と書かれていたが、
6 アプリのコピーは正本より古いまま（human / risk / task / next / epic が欠落）で、誰も気づいていなかった。
注記はコピーを**書き換えない**ことは促すが、正本の更新を**コピーへ届ける**仕組みにはならない。
今回もまず「3 つの README に同じ段落を手で足す」から始め、ユーザーに言われるまで構造を疑わなかった。

- 一般形: **同じ規約を複数の場所へ配るときは、コピーを置かず単一の正本を読み込ませる経路を作る。
  コピーが避けられないなら、ずれを機械で検出する検査を同時に置く**。「直接変更しないこと」の注記は同期の仕組みではない
- 切り出し済み (2026-09-19): 既存 [`claude-md-maintenance.md`](../_claude/rules/claude-md-maintenance.md) の「書き方」節に 1 項追記
  （発動点は「同じ文面を 2 箇所目へ書こうとした瞬間」で、既存の「親 CLAUDE.md の規約を重複させない」と同族）

## 2. 同じ段落を 2 箇所目へ書いた時点で、構造の問題を言い出せた

dotfiles に追記した直後に「obaket にも」と言われ、選択肢は出したが「コピーをやめて 1 箇所から読ませる」を
提案したのは 3 箇所目の依頼の後だった。CLAUDE.md の「重複コード: 同じ変更を 2 箇所にコピペするのは禁止」は
コードにしか当てていなかった。

- 一般形: **自律改善の「同じ変更を 2 箇所にコピペしない」はドキュメント・規約にも適用する**。
  2 箇所目を頼まれた時点で、共通化の案を一文添える
- 切り出し済み (2026-09-19): 既存 `~/.claude/CLAUDE.md`「コード変更時の自律改善」の重複コード項に「文書・規約も同じ」と追記

## 3. submodule の中のファイルを親 repo から commit して空振りした

obaket の README を my-products のルートから `git commit -- apps/obaket/issues/README.md` し、
`pathspec did not match` で空振りした（直後の push は `Everything up-to-date`）。rc と出力を読んだので
その場で気づけたが、「親の checkout の中にあるファイルは親の repo のもの」と思い込んでいた。

- 一般形: **commit の前に、対象ファイルが属する repo を `git -C <ファイルの dir> rev-parse --show-toplevel` で確かめる**
  （submodule・入れ子 repo・worktree では cwd の repo と一致しない）
- 切り出し済み (2026-09-19): 既存 [`commit-with-pathspec.md`](../_claude/rules/commit-with-pathspec.md) の
  「pathspec は cwd 相対で解決される」節に追記（同じ「pathspec が外れる」系の別経路）

## 4. 「次のセッションまで確かめられない」変更は `claude -p` で今確かめられる

`worktree-per-session.md` は「`_claude/` の変更は master へ push するまで動作確認できない」としか書いておらず、
新規セッションでの注入は「次回に確認」で閉じかけた。実際は push + pull の後に `claude -p --model haiku` で
新規セッションを立て、注入の有無を A-B（issues/ あり / なし）で確認できた（数十秒・低コスト）。

- 一般形: **hook・rules・settings のように「セッション開始時に効く」変更は、headless の新規セッションを
  1 回起こして観測する**。「次のセッションで効くはず」で閉じない
- 切り出し済み (2026-09-19): 既存 [`.claude/rules/worktree-per-session.md`](../.claude/rules/worktree-per-session.md) の
  「worktree で `_claude/` を編集しても、その変更は効かない」節に確認方法として追記

## 5. 規範を CLAUDE.md から hook 注入へ移すと、拘束力が下がりうる

hook の出力は `<system-reminder>` の背景情報として届き、CLAUDE.md の「OVERRIDE」扱いを受けない。
今回は CLAUDE.md に「注入された規約は CLAUDE.md と同じ拘束力で従う」の 1 行を残して補った（advisor の指摘）。

- 一般形: **規範を常時ロードの場所から条件付き注入へ移すときは、常時ロード側に「注入された規約に従う」義務を 1 行残す**
- 切り出し済み (2026-09-19): dotfiles `CLAUDE.md`「`_claude/` を触るとき」節に 1 項追記

## 局所案（提案にしない）

- my-products のルートでも `project-templates/issues` に当たって規約が注入される → 害がないため直さない
- obaket README の「`macOS/issues/` は督促されない」が issue 276 以降事実と違っていた → 今回の置換で削除済み
- 新設 hook の敵対的レビューを省略 → 規約文を注入するだけで判定ゲートを持たないため。報告に明記済み

## 決着

5 項目すべてユーザー判断で既存ルールへ追記した（commit「docs(rules,402): retro 402 の 5 項目を既存ルールへ追記」）。残課題なし。
