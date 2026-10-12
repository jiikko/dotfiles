# 履歴操作 (reset / amend / rebase) の前に直近コミットの所有者を確認する

> [`../rules/commit-with-pathspec.md`](../rules/commit-with-pathspec.md) のチェックリスト。
> 発動点 (reset / amend / rebase / cherry-pick / squash を打つ前) は rule 側が持ち、常時読み込まれる。
> この節は起動時には読まれない (常時読み込みの指示ファイル合計が上限 150k 字を超えたため、2026-10-12 に rule から切り出した。dotfiles issue 414 の続き)。

pathspec 規律は「混入」は防ぐが、**履歴を書き換える操作は防げない**。branch の先頭には並行セッションのコミットが積まれているかもしれない。

- **rebase / cherry-pick / merge の途中状態を自動で進めるループは、「今どの commit で止まっているか」を git の状態ファイル
  (`REBASE_HEAD` / `.git/rebase-merge/stopped-sha` 等) で判定する**。`git status` の表示の文言で判定すると、進んだ後も同じ行を読んで
  別の commit に同じ操作 (skip 等) をかける (obaket 1003: 他人の bump を skip するつもりで、次の周で自分の bump も skip した)
- **`git reset HEAD~N` / `git commit --amend` / `git rebase` の前に、必ず `git log -N --format='%h %ad %s' --date=format:'%H:%M'` で対象コミットが自分のものか確認する**（自分が数分前に作ったコミットと、メッセージ・時刻が一致するか）
- 「直近コミット = 自分の直近コミット」と思い込まない。自分のコミットの直後に並行セッションが commit していれば、reset HEAD~1 は**他人のコミット**を、自分のコミットの上に他人が積んでいれば**自分のつもりで他人の**を切り落とす
- **上の確認が効くのは「自分が書いたメッセージと一致するか」までで、他セッションへの帰属には使えない**。
  全セッションが同じ git user なので **author では区別できず**、`git pull --rebase` は他人の commit を
  自分の commit のあいだに挟むので **時刻の前後も根拠にならない**。帰属に使えるのは **commit が触ったファイル**と、
  **本人に聞くこと**だけ。誤帰属のコストは濡れ衣だけでなく、**真の当事者を探すのをやめてしまう**こと
- **推測した帰属を第三者へ伝えない。** 誤情報はそこで止まらず伝播し、受け取った側が
  無関係な相手へ確認に行く。分からないなら「分からない」と言い、
  `ListAgents` + `SendMessage` で**本人に聞く**
- 副次の注意: **`git mv` は即座に stage される**。stage された変更は共有 index 上で「他セッションの pathspec なし commit / reset に拾われ得る」状態になるため、stage から commit までの間隔を最小にする
- 🚨 **squash / 付け替えの soft reset の起点は、共有の ref (`origin/master` 等) ではなく自分の変更を積んだ起点の commit を hash で固定する**。
  共有の ref は別の worktree の fetch で黙って進み、`reset --soft` した index が「他の session の変更を打ち消す差分」になる。
  起点を変えるなら、先に新しい起点の hash へ rebase を済ませてから、その hash へ soft reset する (rebase の後に古い起点の hash を使い続けない)。
  commit の前に `git diff --cached --stat <同じ hash>` と差分を読み、自分の作業と無関係な変更が無いことを確かめる
- **zsh に渡す reflog の参照は `'HEAD@{1}'` のように引用符で包む** (`brace_ccl` 等の設定で波括弧が展開され、戻したつもりで戻らない)
- **zsh で refspec (`sha:ref`) を変数から組むときは `${SHA}:refs/heads/master` と波括弧で囲む**。`$SHA:r...` の `:r` は
  履歴修飾子 (拡張子を落とす) に化け、`<sha>efs/heads/master` という refspec になる。
  `:h` `:t` `:e` `:l` `:u` も修飾子になる (全ての英字ではないが、どれが化けるかを覚えるより常に `${VAR}` にする)
