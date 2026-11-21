# 共通ルール (`_claude/CLAUDE.md`) — 発動したときに読む節

> [`../CLAUDE.md`](../CLAUDE.md) のチェックリスト。
> 各節の発動点は CLAUDE.md 側の同じ見出しの下が持ち、常時読み込まれる。
> この文書は起動時には読まれない (常時読み込みの指示ファイル合計を上限 150k 字に収めるため、2026-11-21 に CLAUDE.md から切り出した。dotfiles issue 414 の続き)。

## 一時ファイルの配置

### 消す・片付けるとき

- 消す前に、結論が issue / コードへ移っているかと、issue や doc が指しているパスでないかを確かめる (`grep -rn 'tmp/' issues/ _claude/`)。dotfiles では `make clean-tmp` (何も指定しないと 30 日より古いもの。`DRY_RUN=1` で一覧のみ、`DAYS=7` で期間を変える)。`/private/tmp`・`$TMPDIR` を片付けるなら `cleanup-tmp-dir` skill (`bin/tmpclean`。直接 rm しない)
