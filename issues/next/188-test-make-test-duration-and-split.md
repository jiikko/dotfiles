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
