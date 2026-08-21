# 083 perf: agent panel の render tick と mark-seen hook の fork 過多 (repo 自身の基準との不整合)

起票日: 2026-08-21
種別: perf
優先度: **P3** (どちらも対話レイテンシ経路の外。ただし repo が別実装を捨てた基準に自分が抵触している)

出典: 監査 [070](done/070-research-quality-audit-2026-08-20.md) の `070-render-fork` /
`070-mark-seen-forks`。**出典 issue には「反証で崩れた (却下)」の一覧がある**ので、
同型の指摘を再提案する前に読むこと。

## 確認できた事実 (2026-08-21)

`scripts/tmux_agent_panel.sh` の `draw_once()` は 2 秒 tick ごとに、表示行数に比例した
コマンド置換 (= fork) を出す:

- `state_color` / `state_rank` / `rel_time` はいずれも `echo` で返す関数 → 呼び出しごとに `$( )`
- 行ごとに `line="$(printf ...)"` + その中に `$(rel_time "$since")` が入る
- ソート用の feed でも行ごとに `$(state_rank "$state")`

監査の実測は **1 tick あたり 42〜82 fork** (エージェント計測。main agent 未再測)。

`_claude/hooks/tmux-mark-seen.sh:33-37` は window の pane 数ぶん `tmux if-shell` を fork する
(`while read` ループ内で 1 pane = 1 プロセス)。

**なぜ問題か**: この repo は tmux-continuum の status interpolation を「5〜10 fork/秒は
基準に合わない」として**捨てている**。自分の常駐 panel が同じ基準に抵触しているのは内部不整合。

## 対応方針 (着手時に再確認)

1. **まず予算化**: `tests/tmux/bench_tmux.sh` に「1 tick のプロセス数」を足す。数値がないと
   改善したかどうかを主張できない (現行の bench は時間しか見ていない → `072`/`064` と同じ形)
2. `state_color` / `state_rank` / `rel_time` を **変数代入で返す** 形へ
   (`_claude/rules/zsh-hook-return-via-reply.md` と同思想)
3. epoch の取得を tick あたり 1 回に畳む / ソートを単一 awk に寄せる
4. mark-seen は `tmux if-shell` のループを 1 回の `list-panes -F` + 条件生成へ畳めるか見る
   (hook 経路なので**無音契約**を壊さないこと: 失敗時に pane へ出力を積まない)

## trigger

`070` の記録では「worktree 同一性の修正後」が trigger で、それは `f6c5efd` で済んでいる。
= agent panel を次に触るときに 1 (予算化) から着手する。1 だけ単独で入れてもよい
(退行の可視化としてそれ自体に価値がある)。
