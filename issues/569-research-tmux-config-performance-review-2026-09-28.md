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

### 2. 30 session のうち 22 が、名前からテストの残骸に見えた (2 つは誤り。下の「発生源の調査」)

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
- [x] 2: 22 の session を消した (2026-09-28 ユーザー承認)。消す前に 22 個とも attach 無し・中の 30 pane は全部 zsh を確認。
  Claude からの kill は PreToolUse の `deny-bare-tmux-kill.sh` が止めたので、ユーザーが実体の絶対パスと `-t "=<名前>"` (完全一致) で 1 つずつ消した。
  結果: 30 → 8 session、97 → 67 pane
  - 🚨 消した直後の自動保存は `scripts/tmux_resurrect_save.sh` の縮小の守り (`regression-blocked`。session が 1/3 以下) が弾き、
    `last` が消す前の保存 (00:21) のまま残った (再起動すると 22 個が戻る状態)。`TT_SAVE_ALLOW_REGRESSION=1 scripts/tmux_resurrect_save.sh` で
    1 回保存し、`last` = `tmux_resurrect_20260928T005511.txt` (8 session、消した名前 0 件) を確認
  - どこで作られたか: 下の「発生源の調査」
- [ ] 5: 端末を替えるか

## 発生源の調査 (2026-09-28)

- **`s1781492637` / `s1786094176` はテストの残骸ではなく、ユーザーの `t` (引数なし) が作った session だった見込みが高い**。
  `zshlib/_tmux_session.zsh` の `_t_impl` は名前が無いと `s$(date +%s)` で 5 window を作る。消した 2 つは 5 window・cwd `~/src/ubiregi-server`、
  名前の epoch は 2026-06-15 12:03 / 2026-08-07 18:16。中は待機中の zsh だけだったので作業は止めていないが、スクロールバックは消えた。
  消す前の保存 `tmux_resurrect_20260928T002117.txt` (と当時の pane_contents) が残っている間は戻せる。**「名前の形がテストっぽい」で残骸と判定したのは誤り**
- `nvimtest*` / `semitest*` の作成元は特定できなかった:
  - dotfiles・`~/src` のコード、`~/.zsh_history`、`~/.tmux_history`、`~/.claude/history.jsonl` のどれにも作成の痕跡なし
  - Claude のセッションの記録は 2026-08-04 以降しか残っていない。8/4 の記録の時点で、どれも「2026-07-30 15:54 に作成」
    (= 7/30 の本番サーバ誤殺の後の resurrect の復元時刻) と表示されていて、それより前から在った
  - 8/8 に、ある Claude のセッションが素の `tmux` (本番サーバ) で `nvimtest:1` などを実験台に使っていた (作ってはいない)
- 形として分かったこと: **本番サーバに一度載った session は、resurrect の保存に入り、再起動のたびに復元され続ける**。
  使われていない session を見つける手がかりは `session_activity` が作成 (= 復元) 時刻のまま動かないこと

## 出典

- tmux CHANGES: https://raw.githubusercontent.com/tmux/tmux/master/CHANGES
- tmux #3352 / #1249 / #5298 / #5529、Terminal.app の同期出力の非対応 (Web 検索、2026-09-28)
