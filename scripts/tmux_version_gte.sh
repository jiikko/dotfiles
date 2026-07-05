#!/bin/sh
# 実行中の tmux サーバの版数が指定以上かを判定する (conf の if-shell 用ヘルパー)。
# 引数: $1 = 要求 major / $2 = 要求 minor。満たせば exit 0。
#
# ⚠️ `tmux -V` を使わないこと: あれはディスク上のクライアントバイナリの版数で、
# 実行中サーバとはズレうる (brew upgrade 後もサーバは旧バイナリのまま動き続ける)。
# 実例 2026-07-05: クライアント 3.7b / サーバ 3.6a のマシンで、3.7+ 専用の
# window-status-separator #{} 展開が素通し表示される表示崩れが起きた。
# 機能の有無を決めるのはサーバ側なので、サーバに #{version} を聞いて判定する。
set -eu

req_maj="$1"
req_min="$2"

v=$(tmux display-message -p '#{version}' | sed 's/[^0-9.]//g')
maj=${v%%.*}
min=${v#*.}
min=${min%%.*}

[ "$maj" -gt "$req_maj" ] || { [ "$maj" -eq "$req_maj" ] && [ "$min" -ge "$req_min" ]; }
