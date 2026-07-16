# commit は自分が触ったファイルを pathspec で明示する（並行セッションの混入防止）

## ルール

- **commit は必ず `git commit -m "..." -- <path1> <path2>` の形式で、このセッションで変更したファイルを明示して行う**。pathspec なしの `git commit` / `git commit -a` は使わない
- `git add` も対象ファイルを明示する。`git add .` / `git add -A` は禁止（既存の c skill ルールと同じ）
- **新規ファイルは pathspec commit でも事前の `git add <path>` が必要**（untracked は pathspec だけでは拾えない）。add するのは自分が作ったファイルだけ
- commit 前の `git status` で、自分が触っていないファイルがステージ済み・変更済みでも**巻き込まない・リセットしない**。それは並行セッションの作業中データかもしれない

## なぜ

同一 repo・同一 working tree で複数の Claude セッションが並行することがある。git の index（staging area）は working tree に 1 つしかないため、pathspec なしの `git commit` は**他セッションが `git add` 済みの変更を自分のコミットに混入させる**。commit コマンド同士の衝突は git 自身の `index.lock` が直列化してくれるので、混入さえ防げばロックや worktree 分離なしで安全に並行できる。

`git commit -- <pathspec>` は index の状態に関係なく指定ファイルの working tree 内容だけをコミットするため、「変更範囲が被らない」前提が守られている限り混入が構造的に起きない。

## 履歴操作 (reset / amend / rebase) の前に直近コミットの所有者を確認する

pathspec 規律は「混入」は防ぐが、**履歴を書き換える操作は防げない**。branch の先頭には並行セッションのコミットが積まれているかもしれない。

- **`git reset HEAD~N` / `git commit --amend` / `git rebase` の前に、必ず `git log -N --format='%h %ad %s' --date=format:'%H:%M'` で対象コミットが自分のものか確認する**（自分が数分前に作ったコミットと、メッセージ・時刻が一致するか）
- 「直近コミット = 自分の直近コミット」と思い込まない。自分のコミットの直後に並行セッションが commit していれば、reset HEAD~1 は**他人のコミット**を、自分のコミットの上に他人が積んでいれば**自分のつもりで他人の**を切り落とす
- 実例 (2026-07-16): 並行セッションの `reset HEAD~1` が、直前に積まれていた別セッションのコミット (issue rename の参照更新 14 ファイル) を切り落とし staged に戻した。reflog と `git diff --cached <旧SHA>` の同一性検証で復旧できたが、気づかなければ次の pathspec なし commit に溶けて消えていた
- 副次の注意: **`git mv` は即座に stage される**。stage された変更は共有 index 上で「他セッションの pathspec なし commit / reset に拾われ得る」状態になるため、stage から commit までの間隔を最小にする

## やること / やらないこと

- ✓ `git commit -m "..." -- path1 path2` で自分の変更ファイルだけコミットする
- ✓ 新規ファイルは自分が作ったものだけ `git add <path>` してから pathspec commit
- ✓ 見覚えのないステージ済み変更は放置する（並行セッションの作業中かもしれない）
- ✓ reset / amend / rebase の前に `git log` で対象コミットが自分のものか確認する
- ✗ pathspec なしの `git commit` / `git commit -a` / `git add .` / `git add -A`
- ✗ 他セッションのものかもしれない変更の unstage・checkout・restore
- ✗ 直近コミットの所有者を確認せずに reset HEAD~N / commit --amend する

## 例外

- ユーザーが明示的に「全部コミットして」と指示した場合（その場合も `git status` で内容を確認し、意図外のファイルが混ざっていないか報告してから）

## 関連

- `~/.claude/skills/c/SKILL.md` — commit 手順の一次情報（本ルールの pathspec 要件を組み込み済み）
- `~/.claude/CLAUDE.md`「Git 禁止操作」— stash 禁止・push 前確認
