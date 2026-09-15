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
# 戻り値: 0 = サーバ不在を確認して socket も片付けた / 1 = **サーバが生きたまま残った**
# (stderr に pid を出す。`set -e` の trap から呼んでいるなら、そこで落ちるのが正しい)。
#
# 🚨 **socket のパスは kill する前に取る**。サーバが死んでからでは `display -p` が失敗し、
# 消すべきパスが分からなくなる (「判定不能だから消さない」に倒れて残骸が残る)。

# 🚨 **`kill-server` は「届いた」ことも「死んだ」ことも保証しない** (issue 377)。
# ハングしたサーバ (floating pane が残った window を小さい client で attach / 縮小すると
# CPU 100% の無応答になる。復旧は `kill -9` のみ) では **client 側も返らない**ので、
#   ① 素の `$(tmux ...)` で聞くと後始末が一緒に固まる
#   ② `kill-server` は空振りし、それでも socket を消すと**唯一の handle を捨てる**
#      = 誰も触れない CPU 100% のサーバが残る (実測 2026-09-15: 23 分間回り続けた)
# そこで「bounded に聞く → kill → **不在を確認** → socket 削除」にする。確認できなければ
# socket は消さず、pid を stderr に出して呼び出し元へ 1 を返す (沈黙で成功にしない)。

# tt__run_bounded は <コマンド> を最大 <秒> だけ走らせ、最初の 1 行を <出力変数> へ返す。
# 時間切れなら空を返し、待ち続けている子を KILL する (EOF と時間切れは rc で区別する。
# 既に終了した pid を撃つと、pid 再利用で無関係なプロセスに当たりうる)。
# process substitution の中は `exec` で置き換える。bash は**単純コマンドなら暗黙に exec する**ので
# 今の 2 つの呼び出しでは差が出ない (実測 2026-09-15: `exec` を外す変異は全ケース緑 = 等価変異)。
# 複合コマンドを渡す呼び出しが増えた瞬間に `$!` がサブシェルを指し、撃っても実体が孤児として
# 残るようになるため、明示のまま残す。
tt__run_bounded() { # tt__run_bounded <出力変数名> <秒> <コマンド...>
  local __var="$1" __secs="$2"; shift 2
  local __line="" __pid="" __rc=0
  exec 9< <(exec "$@" 2>/dev/null)
  __pid=$!
  IFS= read -r -t "$__secs" -u 9 __line || __rc=$?
  if [ "$__rc" -gt 128 ]; then            # 128 超 = read の時間切れ (EOF は 1)
    __line=""
    kill -KILL "$__pid" 2>/dev/null || :
  fi
  exec 9<&-
  printf -v "$__var" '%s' "$__line"
  return 0
}

# tt__wait_gone は pid が消えるまで最大 <回> x 0.05s 待つ。消えたら 0、残っていたら 1。
tt__wait_gone() { # tt__wait_gone <pid> <回数>
  local p="$1" n="$2" i=0
  while [ "$i" -lt "$n" ]; do
    kill -0 "$p" 2>/dev/null || return 0
    sleep 0.05
    i=$((i + 1))
  done
  ! kill -0 "$p" 2>/dev/null
}

# tt_tmux_kill_socket は `-L <name>` のサーバを止め、**死んだことを確認してから** socket ファイルを
# 消す。サーバが既にいなければ、既定の socket dir から名前で組み立てて消す (中断で trap が
# 走らなかった前回の残骸を、次の run が回収できるようにするため)。
tt_tmux_kill_socket() { # tt_tmux_kill_socket <-L の名前>
  # shellcheck disable=SC2034 # discard は tt__run_bounded へ**名前で**渡す出力先 (間接代入)
  local name="$1" path="" pid="" ans="" discard="" comm=""
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
  # 生きているうちに実パスと pid を 1 往復で取る (TMUX_TMPDIR が差し替わっていても正しく追える)
  tt__run_bounded ans 3 tmux -L "$name" display -p '#{pid} #{socket_path}'
  case "$ans" in
    *' '*) pid="${ans%% *}"; path="${ans#* }" ;;
    *)     pid=""; path="" ;;
  esac
  # 🚨 pid は **後で `kill` に渡す**ので、ここがゲート。範囲式 `[0-9]` はロケール次第で全角を
  # 通すため明示列挙で書き、桁数も抑える (`shell-numeric-gate-explicit-digits.md`)。
  case "$pid" in ''|*[!0123456789]*) pid="" ;; esac
  [ -n "$pid" ] && [ "${#pid}" -le 9 ] || pid=""
  if [ -z "$path" ]; then
    # 既に死んでいる / 聞けなかった: 既定の場所を組み立てる (TMUX_TMPDIR を尊重する)
    path="${TMUX_TMPDIR:-/tmp}/tmux-$(id -u)/$name"
  fi
  # パス側でももう一度弾く (名前は default でなくても、組み立てたパスが本番を指す形を防ぐ)。
  # 🚨 **こちらは二重の保険であって主防御ではない**。主防御は関数の先頭の名前チェック。
  # 🚨 **kill より前に置く**。KILL を撃つようになったので、rm の手前だけでは遅い。
  case "${path##*/}" in default) return 0 ;; esac
  # 🚨 `~/.config/tmux-protected-sockets` (bin/tmux shim が読む一覧) は**ここでは見ない**。
  # 正規化と照合を別実装で持つと 2 つの判定が必ず食い違う (`adversarial-review-own-safeguards.md`
  # §0-B)。主防御は名前チェックで、テストが渡すのは自分で作った `-L <prefix>-$$` だけ。
  # shim 側が protected の一覧を関数として公開したら、ここから呼ぶ形へ寄せる。
  tt__run_bounded discard 3 tmux -L "$name" kill-server
  if [ -n "$pid" ]; then
    if ! tt__wait_gone "$pid" 40; then      # 0.05s x 40 = 最大 2s
      # `kill-server` が届かなかった (377 のハング)。**撃つ直前に**素性を取り直してから KILL する
      # (聞いた時点の pid が既に死んで再利用されていると、無関係なプロセスを撃つ)。
      comm=$(ps -o comm= -p "$pid" 2>/dev/null || true)
      if [ "${comm##*/}" = tmux ]; then
        kill -KILL "$pid" 2>/dev/null || :
        tt__wait_gone "$pid" 40 || :
      fi
    fi
    if kill -0 "$pid" 2>/dev/null; then
      printf 'tt_tmux_kill_socket: サーバが死んでいない (pid=%s socket=%s)。socket は消さない\n' \
        "$pid" "$path" >&2
      return 1
    fi
  fi
  [ -S "$path" ] && rm -f -- "$path"
  return 0
}
