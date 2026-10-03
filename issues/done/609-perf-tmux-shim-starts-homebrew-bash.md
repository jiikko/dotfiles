# 609 (perf): bin/tmux の shim が Homebrew の bash 5 を起こし、tmux の呼び出し 1 回ごとに約 5〜7 ms 余計にかかる

起票日: 2026-10-02

## 概要

`bin/tmux` (本番サーバの kill を止める shim。`~/dotfiles/bin` が PATH 先頭なので、スクリプト内部の bare `tmux` もすべて通る) の
shebang が `#!/usr/bin/env bash` で、PATH 先頭の `/opt/homebrew/bin/bash` (5 系) を起動している。
kill を含まない呼び出しは fast path (`case` 1 つ + `exec`) で抜けるので、**コストのほぼ全部が bash の起動**。

実測 (2026-10-02、この Mac、50 回の平均。`zsh/datetime` の `EPOCHREALTIME` で計測):

| 呼び方 | 1 回 |
|---|---|
| `/opt/homebrew/bin/tmux -V` (実体を直接) | 5.5 ms |
| `bin/tmux -V` (今の shim) | 14.8 ms |
| `/opt/homebrew/bin/bash bin/tmux -V` | 13.1 ms |
| `/bin/bash bin/tmux -V` (macOS 同梱の 3.2) | 7.6 ms |

shim の冒頭コメントは「precmd / statusline が tmux を高頻度に叩くので fast path の前で fork しない」と書いているが、
fork を避けても起動する bash そのものが重い。
反証レビューの測り直しでは shim 13.1 ms / 実体 5.7 ms / `/bin/bash` 版 7.6 ms (負荷で揺れる。節約幅は約 5〜7 ms)。

## 対応方針

- shebang を `#!/bin/bash` にする (bash 3.2 で動かす)。`env` の PATH 探索と Homebrew bash の起動の両方が消える
- 🚨 shim は安全機構なので、**kill を含む経路も 3.2 で同じ判定になること**を既存の回帰テストで確かめる
  (`tests/tmux/test_tmux_shim_protects_default.sh`)。3.2 に無い構文 (連想配列・`mapfile`・`${x,,}`・`local -n`) は grep で 0 件
- shebang を変えた理由をコメントに残す (次の人が「brew の bash に揃える」で戻さないため)

## 受け入れ条件

- [x] shebang を変え、shim の回帰テストが 3.2 で緑
- [x] 変更後の `bin/tmux -V` を同じ方法で測り、下に記録する

## 関連ファイル

- `bin/tmux`
- `tests/tmux/test_tmux_shim_protects_default.sh`

## 進捗

- 2026-10-02 「perf(tmux): shim の shebang を /bin/bash にし、tmux の呼び出しごとの Homebrew bash の起動を消す (609)」
  - `/bin/bash -n bin/tmux` OK。`tests/tmux/test_tmux_shim_protects_default.sh` は検査 34 件 fail=0 (`GNU bash, version 3.2.57(1)-release`。
    テストは `$ROOT_DIR/bin/tmux` を shebang 経由で起動するので、3.2 で走っている)
  - 50 回平均: 実体 5.2 ms / 変更後の shim 7.3 ms / 変更前の shim (~/dotfiles の版) 14.6 ms
  - 敵対レビューは省略した: 判定ロジックは変えておらずインタプリタの版だけを変えた変更で、kill 経路を含む既存の回帰テスト 34 件を
    変更後の版で通した。3.2 に無い構文を入れない注意は shebang の直下のコメントに書いた
