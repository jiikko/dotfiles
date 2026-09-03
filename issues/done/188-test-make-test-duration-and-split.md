# 188 test: `make test` の所要時間を測り、docs 変更で回せる分割 target を用意する

起票日: 2026-09-02
出典: [issue 185](185-retro-prompt-audit-apply-2026-09-02.md) 項目 4 (retro からの切り出し)
重要度: P3

## 何が起きたか

2026-09-02 のセッションで `make test` が **2 分でタイムアウトして一度も完走しなかった**。
変更が `_claude/` 配下の散文と rules-rationale の新設だけだったため、以降は
`tests/claude/test_claude_links_complete.sh` 単体だけを回して commit した。

**「全体テストを回せないので回さない」が習慣になるのが本題**。この日は影響が薄い変更
だったので実害は無かったが、次に同じことが起きたとき「今回も docs だけだから」と判断する
根拠が無い (前回そうやって省いた、という前例だけが残る)。

## やること

- [ ] `make test` の所要時間を実測する (何が支配的かも。Go の `-race` か / tmux 系か / zsh 系か)
- [ ] 長いなら、変更の種類で回せる分割 target を用意する。少なくとも
      **`_claude/` 配下だけを触ったときに回すべき検査**が 1 コマンドで走る形にする
      (現状 `tests/claude/` に何本あるかを数えるところから)
- [ ] 分割 target を足したら **入口のドキュメント**を同じ変更で更新する
      ([`new-tool-requires-entrypoint-docs.md`](../_claude/rules/new-tool-requires-entrypoint-docs.md))。
      入口は root `CLAUDE.md` か `tests/CLAUDE.md`

## 注意

- **分割 target は「全体を回さない言い訳」になりうる**。足すなら「どの変更でどれを回すか」の
  判断基準を 1 行で書く。書けないなら分割しない方がよい
- 所要時間の主張には実測値を添える ([`perf-claims-need-measurement.md`](../_claude/rules/perf-claims-need-measurement.md))。
  「速くなった」を数字なしで書かない

## 適用ログ (2026-09-03)

commit `dd8ed07` (計測 + tests/CLAUDE.md) / 後続 (敵対的レビューの訂正)。

### 起票時の前提が既に古かった

「`_claude/` 配下だけを触ったときに回すべき検査が 1 コマンドで走る形」は **2026-08-14 の
`11a36ed` (`make test-changed` / `scripts/test_changed.sh`) で既に入っていた** (起票は 9-02)。
`_claude/CLAUDE.md` と `_claude/(agents|rules|skills|...)/` は `make test-dir DIR=tests/claude` に
写像されている。**新しい分割 target は作っていない**。

### 実測 (tmp/ は gitignore で消えるので数字をここに残す)

- **通しの `make test` = 433 秒** (rc=0)。1 サンプル
- 個別実測 (別 run): `test-lint` 20 / `test-syntax` 1 / `test-discovered` 326 / `test-bats` 14 /
  `test-src` 15〜71 (3 サンプル: 15 / 53 / 71)。合計 414 は**通しの 433 と一致しない**
  (make の起動と集約 + 負荷差)
- `test-discovered` (直列) の内訳: `tests/zshrc` 187 / `tests/tmux` 56 / `tests/bin` 45 /
  `tests/nvim` 12 / `tests/claude` 12 / `tests/scripts` 3 / `tests/setup` 2 / `tests/glogx` 2 /
  `tests/theme` 1 / `tests/issues` 1 (合計 321。親の 326 とは別 run)
- `tests/zshrc` の内訳 (ランナー経由): `av1ify` 124 / `concat` 56 / `tmux-session` 16 /
  `tmux-window-name` 2 / `lazy-loading` 2 / `validate-mp4` 1 / `repair_mp4` 1 /
  `codex-wrapper` 1 / `ai-commands` 0
- **並列版は 35 秒** (`make test-discovered-heavy`。直列の av1ify + concat = 180 秒に対して)

### 敵対的レビュー (opus) で訂正した主張

- 「414 秒」は**個別実測の合計**で通しではなかった → 通しを測って 433 秒に直した
- 「実 ffmpeg を回すので分割しても総量は減らない」は**二重に誤り**。テストは ffmpeg / ffprobe を
  **shell モック**に差し替えており、時間はモック内の `grep` 連打による fork で積まれる。
  そして**並列版で 180 秒 → 35 秒 (−145 秒 / 全体の 34%)** なので「減らない」は否定された
- 「`check_*.sh` は shell / workflow を触ったときだけ入る」は誤りで、**`test-changed` からは
  一度も入らない**。判断基準を「shell / テスト / workflow / Makefile / CI を触ったら
  `make test-lint` も回す」に広げた
- 「他 10 ディレクトリ」→ 7 ディレクトリ / `test-src` を範囲表記へ / 「既定は `make test`」と
  「判断基準は `test-changed`」の食い違いを「代替してよいのは時間が取れないときだけ」に整理

### 見送り

- **並列版を `make test` の既定にする**のは採らなかった。`test-discovered-heavy` は CI の
  heavy/rest 分割に沿った別の集合で、ローカルの既定を差し替えると「何を検証したか」が
  Makefile の 2 変数に依存して読みにくくなる。速い経路として tests/CLAUDE.md に併記した
- 写像の穴 2 件 (`_claude/settings.json` が `tests/claude` へ落ちない / `rules-rationale` が
  対象なし) は tests/CLAUDE.md に**既知の穴として明記**した。写像の修正は
  `scripts/test_changed.sh` の設計判断なので別 issue が要る
