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

## 進捗

- [ ] 所要 (並べた場合 / 直列の場合)
- [ ] 重さ
- [ ] 中断のシグナル
- [ ] 待ちの負荷
