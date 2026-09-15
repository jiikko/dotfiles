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

# 🚨 **停止要求は「届いた」ことも「死んだ」ことも保証しない** (issue 377)。
# ハングしたサーバ (floating pane が残った window を小さい client で attach / 縮小すると
# CPU 100% の無応答になる。復旧は `kill -9` のみ) では **client 側も返らない**ので:
#   ① 素の `$(tmux ...)` で聞くと後始末が一緒に固まる
#   ② 停止要求は空振りし、それでも socket を消すと**唯一の handle を捨てる**
#      = 誰も触れない CPU 100% のサーバが残る (実測 2026-09-15: 23 分間回り続けた)
#
# 🚨 **生死を client の応答から推論しない** (敵対レビュー 1 周目 P1-1 / 2 周目 P1-C)。
# 「返らない」も「黙って終わった」も**死んだ証拠にならない**:
#   - 無応答 = ハング (消してはいけない)
#   - 版ずれ (`protocol version mismatch`) / `tmux` が PATH に無い / fd 枯渇でも client は
#     **速く非 0 で終わり標準出力は空**になる。サーバは生きている
#   - 逆に、本当に死んだサーバでも client は**非 0** で終わる (「no server running」)
#   → 応答でも rc でも分離できない。**判定は「socket の持ち主が居るか」という観測**で行う。
#     持ち主が居ない = dead。ここだけが socket を消してよい唯一の根拠。
#
# 🚨 **ハングしたサーバは自分の pid を答えられない**ので、昇格すべき場面でだけ pid が無い、
# という形になる (= 昇格が実質死にコード)。持ち主は `lsof` で外から引く。
# 🚨 **lsof が無い環境では生死を確認できない**ので、その場合は**何も消さず** rc=1 を返す
# (fail-closed)。この repo は macOS 専用で `/usr/sbin/lsof` は常在するため実害は無い。

# tt__run_bounded は <コマンド> を最大 <秒> だけ走らせ、最初の 1 行を <出力変数> へ返す。
# 戻り値: 0 = コマンドが自分で終了した / 2 = **時間切れ** (無応答)。
# 🚨 **呼び出し側は必ず `|| ...` で受ける**。`set -e` の呼び出し元 (trap から呼ぶテストがある) では、
# 受けそこねた 1 箇所でシェルが即死し、**昇格も stderr の案内も丸ごと飛ぶ** (2 周目 P1-A の実測)。
# 時間切れのときは待ち続けている子を KILL する (EOF と時間切れを rc で見分けてから撃つ。
# 既に終了した pid を撃つと、pid 再利用で無関係なプロセスに当たりうる)。
# process substitution の中は `exec` で置き換える。bash は単純コマンドなら暗黙に exec するので
# 今の呼び出しでは差が出ない (実測: `exec` を外す変異は全ケース緑 = 等価変異) が、複合コマンドを
# 渡す呼び出しが増えた瞬間に `$!` がサブシェルを指し、撃っても実体が孤児として残る。
# 🚨 fd は 199 を使う。`exec 9<` は**呼び出し元の fd 9 を奪って閉じる**。
tt__run_bounded() { # tt__run_bounded <出力変数名> <秒> <コマンド...>
  local __var="$1" __secs="$2"; shift 2
  local __line="" __pid="" __rc=0 __ret=0
  exec 199< <(exec "$@" 2>/dev/null)
  __pid=$!
  IFS= read -r -t "$__secs" -u 199 __line || __rc=$?
  if [ "$__rc" -gt 128 ]; then            # 128 超 = read の時間切れ (EOF は 1)
    __line=""; __ret=2
    kill -KILL "$__pid" 2>/dev/null || :
  fi
  exec 199<&-
  printf -v "$__var" '%s' "$__line"
  return "$__ret"
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

# tt__pid_of_socket は socket ファイルの**持ち主**を外から引く (生死判定の唯一の根拠)。
# 戻り値: 0 = 引けた (持ち主が居れば stdout に pid、居なければ空) / 1 = **引けなかった** (lsof が無い)。
# 曖昧なとき (複数ヒット / 数字以外) は空を返すが rc=0 にはしない (撃たない・消さない側へ倒す)。
tt__pid_of_socket() { # tt__pid_of_socket <socket パス>
  local out="" dir=""
  command -v lsof >/dev/null 2>&1 || return 1
  # 🚨 **lsof は「プロセスが bind した文字列そのもの」と照合する** (実測 2026-09-15:
  # `/tmp/...` へ bind した socket は `/tmp/...` では当たるが `/private/tmp/...` では当たらない)。
  # tmux は bind の前に tmpdir を実パスへ解決するので、こちらも `pwd -P` で揃える必要がある。
  # 副作用として**別名 (symlink / hard link) 経由では当たらない**ことも実測済みで、
  # 「socket dir に本番への別名を置いて撃たせる」経路はここで成立しない。
  dir=$(cd -- "$(dirname -- "$1")" 2>/dev/null && pwd -P) || return 0
  out=$(lsof -t -- "$dir/$(basename -- "$1")" 2>/dev/null | head -2)
  case "$out" in ''|*[!0123456789]*) return 0 ;; esac   # 複数行は改行を含むのでここで落ちる
  printf '%s' "$out"
  return 0
}

# tt__rm_socket は socket ファイルを消す。**持ち主が居ないことを確認した経路からしか呼ばない**。
tt__rm_socket() { # tt__rm_socket <socket パス>
  case "${1##*/}" in default) return 0 ;; esac
  if [ -S "$1" ]; then rm -f -- "$1"; fi
  return 0
}

# tt_tmux_kill_socket は `-L <name>` のサーバを止め、**持ち主が居ないことを確認してから**
# socket ファイルを消す。サーバが既にいなければ、既定の socket dir から名前で組み立てて消す
# (中断で trap が走らなかった前回の残骸を、次の run が回収できるようにするため)。
tt_tmux_kill_socket() { # tt_tmux_kill_socket <-L の名前>
  local name="$1" path="" pid="" ans="" comm="" owner="" lsof_ok=0
  # shellcheck disable=SC2034 # discard は tt__run_bounded へ**名前で**渡す出力先 (間接代入)
  local discard=""
  [ -n "$name" ] || return 0
  # 🚨🚨 **本番 (default) の除外は「どの tmux コマンドより前」に置く。**
  # 2026-09-11 00:28、この関数が本番サーバ (30 セッション) を kill した。除外は `rm` の
  # 手前にしか無く、`tmux -L "$name" kill-server` はその**前**を通っていた。
  # 呼び出し側 (④) は `TMUX_TMPDIR` の差し替え 1 段で隔離していたが、その dir を作る
  # `mktemp` が失敗して空になり、**tmux は TMUX_TMPDIR が空 / 不在だと
  # /private/tmp/tmux-<uid>/ へフォールバックする** (実測: 空・不在・未設定の 3 形とも同じ)。
  # 名前で弾けば、隔離が何段崩れても本番へは届かない。
  case "$name" in default) return 0 ;; esac
  # 生きているうちに pid と実パスを 1 往復で取る (取れなくてもよい。生死の判定には使わない)
  tt__run_bounded ans 3 tmux -L "$name" display -p '#{pid} #{socket_path}' || :
  case "$ans" in
    *' '*) pid="${ans%% *}"; path="${ans#* }" ;;
  esac
  # 🚨 pid は **後で `kill` に渡る**のでここがゲート。範囲式 `[0-9]` はロケール次第で全角を通すため
  # 明示列挙で書き、桁数も抑える (`shell-numeric-gate-explicit-digits.md`)。`0` と負値は
  # プロセスグループ / 全プロセスを意味するので通さない。
  case "$pid" in ''|0|*[!0123456789]*) pid="" ;; esac
  if [ "${#pid}" -gt 9 ]; then pid=""; fi
  if [ -z "$path" ]; then
    path="${TMUX_TMPDIR:-/tmp}/tmux-$(id -u)/$name"
  fi
  # パス側でももう一度弾く (名前は default でなくても、組み立てたパスが本番を指す形を防ぐ)。
  # 🚨 **こちらは二重の保険であって主防御ではない**。主防御は関数の先頭の名前チェック。
  # 🚨 **kill より前に置く**。KILL を撃つようになったので、rm の手前だけでは遅い。
  case "${path##*/}" in default) return 0 ;; esac
  # 🚨 `~/.config/tmux-protected-sockets` (bin/tmux shim が読む一覧) は**ここでは見ない**。
  # 正規化と照合を別実装で持つと 2 つの判定が必ず食い違う (§0-B)。主防御は名前チェックで、
  # テストが渡すのは自分で作った `-L <prefix>-$$` だけ。
  # **trigger: その一覧が非空になったら** shim 側へ公開関数を作ってここから呼ぶ。
  owner=$(tt__pid_of_socket "$path") && lsof_ok=1
  if [ "$lsof_ok" = 0 ]; then
    printf 'tt_tmux_kill_socket: lsof が無く生死を確認できない (socket=%s)。何もしない\n' "$path" >&2
    return 1
  fi
  # 🚨 `set -e` の呼び出し元 (trap から呼ぶテストがある) では、**受けそこねた素のコマンドの
  # 非 0 でそこから先が丸ごと飛ぶ** (2 周目 P1-A: 停止要求が rc=2 を返して昇格も案内も消えた)。
  # ただし `[ cond ] && { ...; }` の**条件が偽**は errexit の対象外 (AND-OR の最後以外は適用
  # されない。実測 2026-09-16 で確認。`if` にしたのは読みやすさのためで、安全性の差は無い)
  if [ -z "$owner" ]; then tt__rm_socket "$path"; return 0; fi   # 持ち主が居ない = dead
  tt__run_bounded discard 3 tmux -L "$name" kill-server || :
  owner=$(tt__pid_of_socket "$path") || :
  if [ -z "$owner" ]; then tt__rm_socket "$path"; return 0; fi
  # まだ持ち主が居る。pid を確定させて KILL へ昇格する。
  # 🚨 hung のときは相手が自分の pid を答えられないので、持ち主の pid をそのまま使う。
  [ -n "$pid" ] || pid="$owner"
  if [ -n "$pid" ]; then
    # **撃つ直前に**素性を取り直す (聞いた時点の pid が既に死んで再利用されていると、
    # 無関係なプロセスを撃つ。窓は 0 にはならない = 残留リスクとして受容)。
    comm=$(ps -o comm= -p "$pid" 2>/dev/null || true)
    if [ "${comm##*/}" = tmux ]; then
      # 🚨 SIGKILL されたサーバは pane の子を道連れにしない (launchd へ里子化して残る)。
      # 呼び出し側が pane にプロセスを走らせているなら、その pid は呼び出し側で片付けること
      kill -KILL "$pid" 2>/dev/null || :
      tt__wait_gone "$pid" 40 || :
    fi
    owner=$(tt__pid_of_socket "$path") || :
    if [ -z "$owner" ]; then tt__rm_socket "$path"; return 0; fi
  fi
  printf 'tt_tmux_kill_socket: サーバを止められない (owner=%s socket=%s)。socket は残す。\n' \
    "${owner:-不明}" "$path" >&2
  printf '  回収: lsof -t -- %s で pid を引いて kill -9 する\n' "$path" >&2
  return 1
}
