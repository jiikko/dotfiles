#!/bin/sh
# 実行中の tmux サーバの版数が指定以上かを判定する (conf の if-shell 用ヘルパー)。
# 引数: $1 = 要求 major / $2 = 要求 minor。満たせば exit 0。
# 引数なしの場合はリポジトリルートの .tmux-version (この conf 全体の要求版数の
# 単一情報源) から要求版数を読む。機能単位のガード (3.6 scrollbars / 3.7 separator 等) は
# 引数指定、conf 冒頭の全体チェックは引数なし、と使い分ける。
#
# ⚠️ `tmux -V` を使わないこと: あれはディスク上のクライアントバイナリの版数で、
# 実行中サーバとはズレうる (brew upgrade 後もサーバは旧バイナリのまま動き続ける)。
# 実例 2026-07-05: クライアント 3.7b / サーバ 3.6a のマシンで、3.7+ 専用の
# window-status-separator #{} 展開が素通し表示される表示崩れが起きた。
# 機能の有無を決めるのはサーバ側なので、サーバに #{version} を聞いて判定する。
set -eu

if [ $# -ge 2 ]; then
  req_maj="$1"
  req_min="$2"
else
  script_dir=$(cd "$(dirname "$0")" && pwd)
  req=$(cat "$script_dir/../.tmux-version")
  # サーバ版数 (下の v) と同じ正規化でサフィックスを剥がす ("3.7a" → "3.7")。
  # これが無いと req_min="7a" が数値比較に渡り test がエラー → 常に「不足」判定になり、
  # .tmux-version を tmux の標準命名 (3.7a 等) で書いた瞬間に全体ゲートが壊れる。
  req=$(printf '%s' "$req" | tr -d '[:space:]' | sed 's/[^0-9.]//g')
  req_maj=${req%%.*}
  req_min=${req#*.}
  req_min=${req_min%%.*}
fi

v=$(tmux display-message -p '#{version}' | sed 's/[^0-9.]//g')
maj=${v%%.*}
min=${v#*.}
min=${min%%.*}

[ "$maj" -gt "$req_maj" ] || { [ "$maj" -eq "$req_maj" ] && [ "$min" -ge "$req_min" ]; }
