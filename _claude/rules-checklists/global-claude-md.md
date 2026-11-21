# 共通ルール (`_claude/CLAUDE.md`) — 発動したときに読む節

> [`../CLAUDE.md`](../CLAUDE.md) のチェックリスト。
> 各節の発動点は CLAUDE.md 側の同じ見出しの下が持ち、常時読み込まれる。
> この文書は起動時には読まれない (常時読み込みの指示ファイル合計を上限 150k 字に収めるため、2026-11-21 に CLAUDE.md から切り出した。dotfiles issue 414 の続き)。

## 一時ファイルの配置

### 消す・片付けるとき

- 消す前に、結論が issue / コードへ移っているかと、issue や doc が指しているパスでないかを確かめる (`grep -rn 'tmp/' issues/ _claude/`)。dotfiles では `make clean-tmp` (既定は 30 日より古いもの。`DRY_RUN=1` で一覧のみ、`DAYS=7` で期間を変える)。`/private/tmp`・`$TMPDIR` を片付けるなら `cleanup-tmp-dir` skill (`bin/tmpclean`。直接 rm しない)

### `tmp/` 直下への書き込みを止める hook

- git の root の `tmp/` 直下へのリダイレクト・`tee`・Write は PreToolUse の hook (`_claude/hooks/deny-tmp-root-write.sh`) が止める (字面の判定。変数を含むパス・`curl -o` などは素通りする。検出しない形は hook の冒頭)

### `tmp/` の ignore の出所

- 🚨 `tmp/` の ignore が `~/.gitignore_global` 由来で repo の `.gitignore` に無い repo がある (dotfiles 等)。その場合、新品チェックアウトと CI では ignore されず、`tmp/` 自体も存在しない

### `skill-tmpdir` が返す場所

- `skill-tmpdir <skill名>` (dotfiles の `bin/skill-tmpdir`) は worktree の中から打っても本体 checkout を返す。
