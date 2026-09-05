# この repo ではセッションごとに worktree を作って作業する (共有 working tree を既定にしない)

## ルール

- **dotfiles で実作業 (コード・テスト・ドキュメントの編集) を始めるときは、まず自分用の worktree を作る**。
  `~/dotfiles` の working tree は**常に他セッションの WIP で dirty** だと想定する
- **`~/dotfiles` に留まってよいのは、ファイルを書かない作業だけ** (調査・grep・レビュー・ログ確認)。
  1 ファイルでも編集するなら worktree へ出る
- **統合は `git push origin HEAD:master`**。worktree で commit してから push する
  (`cp` でファイルを戻さない。`git pull --rebase` は他セッションの未コミット変更で止まる)
- **worktree は commit が master へ載った時点で消す** (`git worktree remove --force`)。残さない
- **issue 番号は採番したら即 push する**。番号は複数セッションが同じ列から取るので、
  ローカルに置いている間は衝突が見えない (実測 2026-09-03 に 3 回衝突: 214 / 221 / 223)

## 🚨 push は「反映」ではない — 実体パスから動くものは `~/dotfiles` へ pull するまで古いまま

- **`~/dotfiles` の実体パスから起動されるもの**は、worktree の commit を push しても
  **`~/dotfiles` に pull するまで古い版が動き続ける**。対象は `_claude/` (下節) だけでなく
  **`scripts/` と `bin/`** も: 稼働中の tmux サーバが読むのは `_tmux.conf` の bind が指す
  `${DOTFILES_DIR:-$HOME/dotfiles}/scripts/...` / `.../bin/...` であって、worktree のコピーではない
- **統合は「push して終わり」にしない**。`git push origin HEAD:master` →
  **`git -C ~/dotfiles pull --rebase`** → (conf を変えたなら reload) までで 1 セット
- 🚨 **「動作確認した」と書く前に、確認した対象が本番の実体かを見る**。隔離サーバ (`-L`) や
  worktree 内で動かした結果は、`~/dotfiles` が古いままなら**本番の証拠にならない**。
  `git -C ~/dotfiles status -sb` の `behind N` が出ていないかを、報告の前に確認する
- 実測 2026-09-05 (issue 265): C-v のペースト通知を 3 commit 入れたが、`~/dotfiles` へ
  pull していなかったため**ユーザー環境では 2 回とも古い版が動いていた**。隔離サーバでは
  出ていたので「出るはず」と報告し、「出ない」の往復を 2 回作った
- 🚨 **push が成功したことを確認するまで worktree を消さない**。`push; pull; worktree remove` を `;` で
  繋ぐと、push が non-fast-forward で弾かれても後続が走り、**未 push の commit を持つ worktree を消す**
  (実測 2026-09-05 retro 266: 4 commit が一時的に参照なしになり、hash から復元した)。`&&` で繋ぐ
- 🚨 **本体への pull / worktree remove は `git -C ~/dotfiles` で対象を明示する**。worktree の cwd で
  `git pull` を打つと detached HEAD で必ず失敗する (同日 3 回)。`commit-with-pathspec.md` の
  「worktree からの merge / push も cwd 依存」と同じ罠の pull 版

## 🚨 worktree で `_claude/` を編集しても、その変更は効かない

- **hook は `~/dotfiles/_claude/hooks/...` の実体パスで起動する** (`_claude/settings.json` の
  `command`)。worktree 側のコピーは**どこからも読まれない**
- **`~/.claude/rules/` の link も `~/dotfiles/_claude/rules/` を指す**。worktree で足したルールは
  その worktree からは見えず、master へ載って初めて次のセッションから読まれる
- つまり **`_claude/` の変更は「master へ push するまで動作確認できない」**。
  worktree で編集 → push → 次のセッションで効く、の順になる。
  `scripts/claude_links.sh apply` を worktree で叩いても link 先は `~/dotfiles` のまま

## なぜ

**実測 2026-09-03 (1 セッション)**: 共有 working tree が並行セッションの WIP
(`src/glogx/cleanup_latch.go` 等) で常に dirty だったため、

- `git pull --rebase` が `cannot pull with rebase: You have unstaged changes` で 3 回止まった
- 自分の commit を載せるために **worktree で origin/master を出して cherry-pick → push** を 4 回やった
  (共有ツリーを使っているのに、結局 worktree を経由していた)
- 他セッションの未 push commit が自分の `git push` に巻き込まれる形になり、
  そのたびに本人へ確認を取る往復が発生した
- pathspec commit の規律は「混入」は防ぐが、**pull / rebase / push がブランチ単位**である
  ことは防げない

worktree を既定にすると、これらは**構造的に**起きない (index も working tree も分かれ、
push は `HEAD:master` で自分の commit だけを載せられる)。

## やらないこと

- ✗ 「今は誰も触っていないようだから」で共有ツリーで編集を始める
  (`ListAgents` で idle に見えても、未コミットの WIP は残っている)
- ✗ worktree の成果を `cp` で `~/dotfiles` へ戻す (他セッションの変更を黙って消す)
- ✗ 作った worktree を残したままセッションを終える

## 関連

- `~/.claude/rules/parallel-write-agents-need-worktree-isolation.md` — **発動点が違う**。
  あちらは「自分が書き込みエージェントを 2 体以上並行させるとき」で、こちらは
  **「この repo で作業を始めるとき」**(相手が自分の起こした並行かどうかに依存しない)
- `~/.claude/rules/commit-with-pathspec.md` — 共有ツリーに留まる場合の規律 (cwd 相対の
  pathspec / worktree からの `merge --ff-only` が本体を動かさない罠)
