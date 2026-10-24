# 889 (test): serial-run の計測 — 2 session を並べたときの所要・重さ・中断のシグナル・待ちの負荷

起票日: 2026-10-23

## 概要

[869](done/869-feat-serial-run-machine-wide-heavy-command-queue.md) の段階 5 (計測) を切り出したもの。serial-run の実装・hook・Makefile への組み込み・
列を repo ごとにする変更は 869 で済んでいる。効果と前提を、まだ測っていない。

## 測るもの (どれも未実測)

- 2 session が `make test` を同時に回す場合と、serial-run で直列にした場合の、それぞれの所要と「待ちを含めた所要」
- 重なっている間の操作の重さ (load average と、エディタ・tmux の応答)。同じ 2 本の `make test` の組で比べる
- Claude Code のツールの中断・上限での背景への移行が、serial-run と子へどのシグナルをどこへ送るか (子のグループの設計の前提。`src/serialrun/README.md` の「未実測」)
- 待機中の負荷 (待ち手 1 本の CPU 時間と RSS。待っている 10 分の間に増えた CPU 時間を `ps -o time` で読む)

## trigger

2 session を並べた最初の日。待ちと所要は列ごとの `~/.local/state/serial-run/<列>/history.log` に残る (列は `serial-run status` の先頭の `lock:`。
既定は repo ごとの `repo-<dir 名>-<hash>`、`SERIAL_RUN_SCOPE=machine` なら `default`)。重さとシグナルは history.log に残らないので、その日に測る。

## 結果 (2026-10-24 測定)

条件: 同じ repo (dotfiles) の 2 つの worktree (origin/master) で `make test` を 2 本同時に始めた。A = 包まない (`SERIAL_RUN_MAKE_DISABLE=1`) / B = 包む (Makefile の入口)。
14 コアの Mac (`hw.ncpu`=14)。5 秒ごとに load (1 分)・tmux の応答 (`tmux display -p`)・serial-run の CPU 時間を取った。手順は下の「測り方」。
4 本とも rc=2: どれも `test-shellcheck` が `bin/tf` (zsh。2026-10-24 の commit「feat(bin): treefiler の短い名前 tf を足す」) で落ちていて、計測とは無関係の赤。

| | 所要 | load1 平均 / 最大 | tmux の応答 p50 / p95 / 最大 | serial-run 以外の赤 |
|---|---|---|---|---|
| 計測前の 60 秒 (何も走らせない) | — | 11.2 / 16.5 | 28 / 35 / 35 ms | — |
| A 同時 | 2 本とも 612 秒 (両方の完了まで 612 秒) | 37.5 / 94.1 | 13 / 38 / 108 ms | `test_claude_mods.sh` (2 本とも)・`test_deny_piped_push_then_destroy.sh` (1 本)・`test-discovered-serial` |
| B 直列 | 1 本目 400.4 秒 / 2 本目は 400.4 秒待って 405.8 秒 (両方の完了まで 806 秒) | 17.7 / 39.3 | 10 / 25 / 32 ms | なし |

- 両方の完了までの時間は直列の方が 194 秒 (32%) 長い。1 本目は直列の方が 212 秒早く終わる。1 本だけの所要 (history.log の過去の実行で 354〜394 秒) と比べて、同時の 1 本は約 1.6 倍
- 同時にすると、負荷の下でだけ落ちるテストが赤になった (1 回の試行。何回に 1 回かは測っていない)。直列ではその赤は出なかった
- tmux の応答は、どちらの場合も 110 ms 以内。計測前の 60 秒の方が遅いのは、worktree を作った直後で、手元の別の作業も走っていた (load 11) ためと見ている (未確認)。
  「エディタ・tmux の反応」の差は、この測り方では出なかった
- 待ち手 1 本 (私用の列 `probe889`、持ち手が sleep の間): 5 分の待ちで CPU 時間 0.02 → 0.04 秒、RSS 7.4〜7.6 MB。history.log に `wait=319.7s`
- 中断のシグナル (私用の列 `probe889sig`。外側の bash・serial-run・子の bash で、受けたシグナルを記録):
  - 背景のタスクをツールで止める (TaskStop): 外側・子が同じ秒に TERM を受けた (serial-run も同じプロセスグループにいる)。子が抜け、serial-run は rc=1 を記録し、列は空いた。子の孫 (`sleep`) も残らなかった
  - 前景のコマンドが時間の上限に当たる: シグナルは届かず、背景へ移って走り続けた (列も持ったまま)。その後の停止は TaskStop と同じ形
  - 未実測: 人が Esc で前景のコマンドを中断したとき

### 測り方

- 所要と重さ: origin/master から 2 つの worktree を作り、60 秒何も走らせない → A (`SERIAL_RUN_MAKE_DISABLE=1 make -C <wt> test` を 2 本同時) → B (`make -C <wt> test` を 2 本同時) の順。
  5 秒ごとに `sysctl -n vm.loadavg` の 1 分値と `tmux display -p x` の往復時間を TSV へ書いた
- 待ち手: `serial-run -l probe889 --max-wait 0 -- sleep 320` が走り出したのを `serial-run status -l probe889` の「走り出してから」で待ってから、
  `serial-run -l probe889 -- true` を起こし、その pid の `ps -o time=,rss=` を 10 秒後と 310 秒後に読んだ (serial-run の実体は exec されるので、起こした pid のまま)
- シグナル: 4 つのシグナルを trap して記録する bash から `serial-run -l probe889sig -- bash -c '<同じ trap>; sleep 600 & wait'` を起こし、ツールで止めた
A を先に回したので、A の方がキャッシュは冷えている (ビルドのキャッシュは内容で引くので、新しい worktree でも大半は温まっていたはず。未確認)。

## 進捗

- [x] 所要 (並べた場合 / 直列の場合) — 上の表
- [x] 重さ — load は直列で平均 37.5 → 17.7。tmux の応答には差が出なかった
- [ ] 中断のシグナル — ツールの停止と時間の上限は測った。人の Esc が残り
- [x] 待ちの負荷 — 5 分で CPU 0.02 秒・RSS 7.5 MB
