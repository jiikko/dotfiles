# 1042 (feat): 未コミットの変更がある worktree の削除を PreToolUse の hook で止める

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-06〜）

起票日: 2026-11-06

## 概要

`git worktree remove --force` は未コミットの変更ごと消し、object にも残らない。retro 1040 の提案 D。
起点: retro 1040 の項目 6。rebase の失敗で commit が走らず、`Everything up-to-date` の push の後の `git worktree remove --force` が、未コミットの issue の本文を消した。

## 対応方針の変更 (提案 D との違い)

提案は「未コミットの変更がある worktree の削除を hook で判定して止める」だった。実装の前に、git 自身が `--force` 無しの `git worktree remove` で
modified / untracked のある worktree の削除を拒否すること (ignore されたファイルだけなら消せる。dotfiles の worktree で実測) を確かめたので、
hook は `--force` だけを止め、汚れているかの判定は実行時の git に任せた (adversarial-review-own-safeguards の §0-B)。
同じ Bash の中で commit してから消す形でも、commit が走らなければ git が止める。パスの解決 (変数・`cd`) も要らない。
失うもの: lock された worktree・submodule を初期化した worktree・捨てると確かめた worktree は `--force` 無しでは消せない (人が `!` で打つか、中身を commit してから消す)。

## 対応

- `_claude/hooks/deny-force-worktree-remove.sh` (PreToolUse(Bash)、settings.json に配線): `git … worktree … remove|rm` に `--force` / `-f` / 省略形 (`--forc` 等) がある区切りを止める。
  untracked を見えなくする設定 (`status.showUntrackedFiles`・`core.excludesFile`・`include.path` / `includeIf`) を同じ区切りで変える指定と、設定ファイルを差し替える前置き
  (`GIT_CONFIG*=`・`HOME=`・`XDG_CONFIG_HOME=`) も止める (`status.showUntrackedFiles=no` と全部を ignore する `core.excludesFile` では、git が --force 無しでも untracked ごと消すことを実測)
- 手順書の `git worktree remove --force` を外した: `.claude/rules/worktree-per-session.md`・`_claude/rules/parallel-write-agents-need-worktree-isolation.md`・`_claude/skills/audit/SKILL.md`・
  `_claude/skills/codex-drive/phases-1-2.md` (codex-drive は patch を移した後に、本体以外の登録済みの worktree だと実パスで確かめてから `reset --hard` + `clean -fd` で捨て、`--force` 無しで消す)
- テスト `tests/claude/test_deny_force_worktree_remove.sh` 45 件 (前提: --force 無しなら git が untracked / modified のある worktree を拒否し、ignore だけなら消せる・_gitconfig が status.showUntrackedFiles を立てない、を本物の git で固定)

## 検出しないもの・受ける偽陽性 (hook の冒頭が正本)

検出しない: 変数・関数・eval・スクリプトの中の削除・シェルに食わせる heredoc の本文・別の区切りで export した設定・repo / global の設定ファイルの中身・ignore されたファイル・`rm -rf` + `prune`・字句を崩した書き方。
受ける偽陽性: lock / submodule の worktree・1 行の引用符の中の文言と `git grep` / `rg` の検索語 (語を崩して `worktree.remove` で検索する)・同じ区切りに上の設定の語がある remove・前置きで設定を差し替えた remove。

## 進捗

- [x] 実装・配線・テスト・手順書 4 本
- [x] 敵対的レビュー 4 周 (opus)。採否:
  - 1 周目: `$(git …)`・バッククォート・`bash -c '…'`・`"--force"`・省略形で素通り → 語の頭と末尾の記号を剥がし、省略形を拾う / `-c status.showUntrackedFiles=no` だと git が untracked ごと消す (実測) → 止める / codex-drive の新しい手順でパスを誤ると `reset --hard` が本体に走る (P1) → 本体以外の登録済みの worktree だと実パスで確かめる段を足した (macOS の /var → /private/var で一致しない誤りも直した)
  - 2 周目: `git grep "worktree remove --force"` が止まる → 最初のサブコマンドで絞った / `--config-env`・`GIT_CONFIG_KEY` で素通り → 止める / codex-drive の本体の取り方 → 一覧の先頭から
  - 3 周目: 2 周目の絞り込みが退行 (P1: `git -C "$(git rev-parse …)" worktree remove --force`・値を空白で区切る大域オプションの後ろで素通り) → 絞り込みを戻した / 引用符付きの設定・`GIT_CONFIG_PARAMETERS`・設定ファイルの差し替えで素通り → 語を広く拾う
  - 4 周目: 3 周目の「引用符で始まる語をキーワードにしない」が退行 (`git "worktree" remove --force`) → 戻し、検索語の偽陽性を受ける側に倒した / `core.excludesFile`・`include.path`・`HOME=` で素通り (core.excludesFile は実測) → 止める
  - **4 周目の修正には 5 周目を回していない** (戻す変更と語の追加で、変異で赤を確かめた)。字句を崩した書き方 (`remove{,}` 等) は記録のみ
- [x] 変異で red を確認 (mutate-verify): --force を拾わない・省略形を拾わない・引用符を剥がさない・コマンド置換の git を拾わない・showUntrackedFiles / config-env / GIT_CONFIG_KEY / GIT_CONFIG_PARAMETERS / 設定ファイルの差し替え / core.excludesFile を拾わない・heredoc を落とさない・区切らない・サブコマンドで絞る (退行の形)・引用符で始まる語をキーワードにしない (退行の形)
- [x] skill-eval: audit・codex-drive とも PASS (1/1)。skill の整合チェックを 3 観点 (skill の中・外の実物・使う人の手順) で回した
- [x] root の `make test` rc=0 (最終の木で)
