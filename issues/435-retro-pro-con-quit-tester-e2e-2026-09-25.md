# 435 (retro): pro-con の終了の扱い・テストの係・e2e モードを入れた日 (427)

起票日: 2026-09-25

## 概要

434 (段階 3 の retro) の後、同じ日に 427 の終了の扱い・段階 4 (テストの係)・自動起動・Q → quit・e2e モード・dispatcher への改名を入れた。
うまくいった話は書かない。踏んだ所と、他の作業にも効く形の改善だけ。

## どこで踏んだか

1. **make test が落ちたまま push した** (subject「テストの係の撃つ条件を偽物のプロセスで固定し」)。make test → commit → push を `;` で繋いでいて、
   make test の rc=2 で止まらなかった。commit メッセージの「make test 緑」も誤りになった (427 に訂正を書いた)。落ちたのは無関係の flaky だったが、
   関係のある失敗でも同じように push していた
2. **`git reset --soft origin/master` で、並行セッションの commit を巻き戻す commit を作りかけた**。自分の作業ツリーが origin の新しい commit を
   まだ取り込んでいない状態で soft reset すると、index が「origin の新しい変更を消す差分」になる。push の前に staged の一覧で気づいた。
   さらに zsh が `HEAD@{1}` の波括弧を展開して、元へ戻す `git reset --soft HEAD@{1}` が効かなかった
3. **read-only のレビューを走らせている間に、そのレビューが読む作業ツリーのファイルを書き換えた** (runner.go)。ルールにある形そのもの
4. 変異で「緑のまま」を 4 回踏み、どれもテスト側の欠陥だった (偽物のプロセス自身が exec されて照合の候補にならない / 撃たれた子がゾンビで
   kill(pid,0) が成功する / 別の枝が先に当たって狙いの枝に届かない / 台本を 1 行にしても exec されない = 等価)。変異を当てていなければ、どれも黙って通っていた

## 次に効きそうな改善 (切り出し先の提案。実行はユーザーの判断を待つ)

1. **「squash / 付け替えの soft reset は、`git rebase <新しい先>` を済ませてから」**。origin が動いた後に `reset --soft origin/...` すると、
   index は「origin の新しい変更を打ち消す差分」になる。commit の前に `git diff --cached --stat origin/...` の一覧に自分の触っていないパスが無いかを見る
   → 切り出し先: `_claude/rules/commit-with-pathspec.md` の「履歴操作 (reset / amend / rebase) の前に」の節へ追記
2. **検証 → commit → push を 1 行に繋ぐなら、検証の rc で止める** (`;` で繋がない)。既に `verify-execution-not-just-exit-code.md` に
   「成否で後段を走らせる `&&` のつなぎを書いた瞬間にも発動する」とあるので、新しい規範は立てず、「make test → commit → push」の例を 1 行足す
   → 切り出し先: `_claude/rules/verify-execution-not-just-exit-code.md` の該当の項へ追記
3. **zsh で `@{N}` の reflog 指定を打つときは引用符で包む** (`'HEAD@{1}'`)。波括弧展開で `HEAD@1` になり、戻したつもりで戻らない
   → 切り出し先: 同上 (commit-with-pathspec の履歴操作の節) に 1 行。局所的だが git の履歴操作の全部に効く

## 局所の件 (提案にしない)

- `tmux kill-server` の後に socket が残る件は既存のルール (tmux-probe) どおりに控えたパスだけを消した
- `ps` に他のプロセスの環境が出ない (この macOS) ので、実行の印は環境変数でなく argv に載せた (427 に記録)
