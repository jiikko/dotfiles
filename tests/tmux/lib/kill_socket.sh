# shellcheck shell=bash
# tests/tmux/lib/kill_socket.sh — 隔離 tmux サーバを、**socket ファイルごと**片付ける。
#
# 🚨 なぜ必要か (issue 305 ①): **`tmux kill-server` は socket ファイルを消さない**。
# 実測 2026-09-10:
#
#   tmux -L probe -f /dev/null new-session -d 'sleep 30'
#   tmux -L probe kill-server
#   → /private/tmp/tmux-501/probe が**残る** (SIGKILL で殺した場合も同じ)
#
# つまり中断時だけでなく**正常終了のたびに 1 個ずつ漏れる**。同日の実測で
# `/private/tmp/tmux-501/` に **536 ファイル**あり、生きているのは `default` (本番) の
# 1 個だけだった (最古 2026-07-05)。大半は `ctrlv-test-*` と `pane-state-bell-*` = この形の
# テストが `-L <name>-$$` で起こしたもの。
#
# 🚨 **掃除機構 (母集合を走査して消す) は作らない** (`adversarial-review-own-safeguards.md` §0-A)。
# 自分が作った socket のパスを**起動時に控えて、そのパスだけを消す**。走査しないので
# 「母集合の取り違えで他人のものを消す」経路が原理的に無い (本番の `default` を除外する
# ロジックすら要らない)。
#
# 使い方:
#   . "$ROOT_DIR/tests/tmux/lib/kill_socket.sh"
#   tt_tmux_kill_socket "$SOCK"      # cleanup / trap の中で呼ぶ
#
# 🚨 **socket のパスは kill する前に取る**。サーバが死んでからでは `display -p` が失敗し、
# 消すべきパスが分からなくなる (「判定不能だから消さない」に倒れて残骸が残る)。

# tt_tmux_kill_socket は `-L <name>` のサーバを止め、その socket ファイルも消す。
# サーバが既にいなければ、既定の socket dir から名前で組み立てて消す (中断で trap が
# 走らなかった前回の残骸を、次の run が回収できるようにするため)。
tt_tmux_kill_socket() { # tt_tmux_kill_socket <-L の名前>
  local name="$1" path=""
  [ -n "$name" ] || return 0
  # 🚨🚨 **本番 (default) の除外は「どの tmux コマンドより前」に置く。**
  # 2026-09-11 00:28、この関数が本番サーバ (30 セッション) を kill した。除外は `rm` の
  # 手前にしか無く、`tmux -L "$name" kill-server` はその**前**を通っていた。
  # 呼び出し側 (④) は `TMUX_TMPDIR` の差し替え 1 段で隔離していたが、その dir を作る
  # `mktemp` が失敗して空になり、**tmux は TMUX_TMPDIR が空 / 不在だと
  # /private/tmp/tmux-<uid>/ へフォールバックする** (実測: 空・不在・未設定の 3 形とも同じ)。
  # 名前で弾けば、隔離が何段崩れても本番へは届かない。
  # (`list-masked-failure-modes-before-removing-guard.md`: 「冗長」と書いた防御が
  #  実際には kill 経路を 1 mm も守っていなかった)
  case "$name" in default) return 0 ;; esac
  # 生きているうちに実パスを取る (TMUX_TMPDIR が差し替わっていても正しく追える)
  path=$(tmux -L "$name" display -p '#{socket_path}' 2>/dev/null || true)
  tmux -L "$name" kill-server 2>/dev/null || :
  if [ -z "$path" ]; then
    # 既に死んでいる: 既定の場所を組み立てる (TMUX_TMPDIR を尊重する)
    path="${TMUX_TMPDIR:-/tmp}/tmux-$(id -u)/$name"
  fi
  # パス側でももう一度弾く (名前は default でなくても、組み立てたパスが本番を指す形を防ぐ)。
  # 🚨 **こちらは二重の保険であって主防御ではない**。主防御は関数の先頭の名前チェック。
  case "${path##*/}" in default) return 0 ;; esac
  [ -S "$path" ] && rm -f -- "$path"
  return 0
}
