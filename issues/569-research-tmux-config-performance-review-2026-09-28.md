# 569 (research): tmux の設定を性能の観点で見直す (Web の知見との突き合わせ)

起票日: 2026-09-28

## 概要

ユーザー依頼「設定をもう一度見直して、ググった上で、パフォーマンスの観点で改善できることがないか調べて」。
`_tmux.conf` の設定値を全部洗い、Web の既知の落とし穴と突き合わせ、本番サーバを読むだけで観察した。
設定そのものに大きな穴は無かった。効きそうなのは設定の外の 2 つ (サーバの版と、溜まった session)。

## 見つかったこと

### 1. 動いているサーバが 3.7b のまま (入っているのは 3.7c)

- `tmux display -p '#{version}'` = 3.7b (pid 4815、2026-09-27 20:20 起動)。Homebrew の 3.7c は同日 20:36 に入った
- 3.7c の CHANGES (3.7b → 3.7c) に性能関係の修正がある: "Check time periodically in loops rather than every one"。
  3.7a の "Scrollbar options are now cached rather than being looked up for every redraw (issue 5298)" は 3.7b に入っている
- 3.7c にするにはサーバの再起動が要る (session の配置は resurrect が戻すが、中で動いているプロセスは止まる)。効果は未実測

### 2. 30 session のうち 22 が、名前からテストの残骸に見える

- `nvimtest`〜`nvimtest7` / `semitest`〜`semitest13` / `s1781492637` / `s1786094176`。どれも 2026-09-27 20:20 の復元で作られ、以後誰も使っていない
  (`session_activity` が作成時刻のまま)。最新のスナップショット (`tmux_resurrect_20260928T002117.txt`) にも 30 pane 行が載っていて、
  **再起動のたびに復元され続けている**
- dotfiles の中にこの名前を作るコードは無い (`grep -rn 'semitest\|nvimtest'` で 0 件)。どこで作られたかは未確認
- 効く先: 保存のたびの `capture-pane` (pane ごと。`@resurrect-capture-pane-contents on`)、zsh のプロセスとメモリ。
  status の毎秒の描画は attach 中の session だけなので、そこには効かない。効果は未実測

### 3. 本番サーバの負荷 (読むだけで観察)

- 30 session / 95 window / 97 pane / client 3。RSS 387MB。CPU は起動から 4 時間 27 分で累計 392 秒 (平均 2.4%)、直近 10 秒で 3.5%

### 4. Web の既知の落とし穴 — 今の設定は踏んでいない

| 落とし穴 | 今の設定 | 判定 |
|---|---|---|
| `escape-time` の既定 500ms で Esc が遅い | `escape-time 5` | 済み |
| status の `#()` と大きな scrollback で `refresh-client -S` が重くなる (tmux #3352) | status に `#()` は無い (fork ゼロ方針) | 済み |
| 大きな `history-limit` で resize の reflow が遅い (tmux #1249) | 30000 | 許容 (resize 時だけ) |
| 3.7 の全画面 TUI の入力の遅れ (tmux #5298) | 3.7a で修正済み | 済み |
| 同期出力 (DECSET 2026) で描画のちらつきを抑える (`terminal-features ...:sync`) | 未設定 | **効かない**: Terminal.app は同期出力に対応していない |
| focus-events で pane の行き来が遅れる | off (issue 568) | 済み |

### 5. 設定の外で一番大きいのは Terminal.app

- Terminal.app は同期出力に対応していない。分割した pane が多いと tmux が 1 本の出力にまとめて流すので、端末の描画の速さがそのまま効く (Web の報告)
- 端末を替える (Ghostty / iTerm2 / WezTerm など) のが一番効くはずだが、影響が大きいのでユーザーの判断

## 対応の候補 (ユーザーの判断待ち)

- [ ] 1: 都合のよいときにサーバを再起動して 3.7c にする
- [ ] 2: 22 の session を消し、スナップショットに載らないようにする (本当に要らないかをユーザーが確かめてから)。作られた場所も探す
- [ ] 5: 端末を替えるか

## 出典

- tmux CHANGES: https://raw.githubusercontent.com/tmux/tmux/master/CHANGES
- tmux #3352 / #1249 / #5298 / #5529、Terminal.app の同期出力の非対応 (Web 検索、2026-09-28)
