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
# 🚨 **「答えが無い」を「死んだ」と読まない**。ここが最初の実装の誤りだった (敵対レビュー P1-1):
# 応答が無い理由は **死んでいる (消してよい)** と **ハングしている (絶対に消してはいけない)** の
# 2 つあり、区別せずに socket を消すと**塞いだはずの穴を後始末自身が開ける**。
# そこで判定を **alive / dead / hung の三値**にし、`dead` を確認したときだけ socket を消す。
#
# 🚨 **ハングしたサーバは自分の pid を答えられない**ので、`display -p '#{pid}'` に頼ると
# 昇格すべき場面でだけ pid が無い、という形になる (= 昇格が実質死にコード)。
# hung のときは **socket ファイルの持ち主を `lsof` で外から引く** (実測: `lsof -t -- <socket>` が
# サーバの pid を返す)。撃つ前に `ps -o comm=` で素性を確かめる。

# tt__run_bounded は <コマンド> を最大 <秒> だけ走らせ、最初の 1 行を <出力変数> へ返す。
# 戻り値: 0 = コマンドが自分で終了した (出力が空なら「答えなし」) / 2 = **時間切れ** (無応答)。
# 🚨 この 2 つを呼び出し側へ**区別して**返すのが本ヘルパーの役目 (丸めると上記 P1-1 になる)。
# 時間切れのときは待ち続けている子を KILL する (EOF と時間切れを rc で見分けてから撃つ。
# 既に終了した pid を撃つと、pid 再利用で無関係なプロセスに当たりうる)。
# process substitution の中は `exec` で置き換える。bash は**単純コマンドなら暗黙に exec する**ので
# 今の呼び出しでは差が出ない (実測 2026-09-15: `exec` を外す変異は全ケース緑 = 等価変異)。
# 複合コマンドを渡す呼び出しが増えた瞬間に `$!` がサブシェルを指し、撃っても実体が孤児として
# 残るようになるため、明示のまま残す。
# 🚨 fd は 199 を使う。`exec 9<` は**呼び出し元の fd 9 を奪って閉じる**ので、将来 fd 9 で
# lock を持つコードが入ると無言で解放される (repo 全体で fd 9 の利用は現在 0 件だが、
# 番号を譲っておく方が安い)。
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

# tt__probe は `-L <name>` のサーバの状態を alive / dead / hung の**三値**で返す。
# TT_PROBE_STATE / TT_PROBE_PID / TT_PROBE_PATH に入れる (pid と path は 1 往復で取るので、
# 「pid は本物・path は捏造」のような分離した嘘は作れない)。
tt__probe() { # tt__probe <-L の名前>
  local name="$1" ans="" rc=0
  TT_PROBE_PID=""; TT_PROBE_PATH=""
  tt__run_bounded ans 3 tmux -L "$name" display -p '#{pid} #{socket_path}' || rc=$?
  if [ "$rc" -eq 2 ]; then TT_PROBE_STATE=hung; return 0; fi
  case "$ans" in
    *' '*) TT_PROBE_STATE=alive; TT_PROBE_PID="${ans%% *}"; TT_PROBE_PATH="${ans#* }" ;;
    *)     TT_PROBE_STATE=dead ;;
  esac
  # 🚨 pid は **後で `kill` に渡る**のでここがゲート。範囲式 `[0-9]` はロケール次第で全角を通すため
  # 明示列挙で書き、桁数も抑える (`shell-numeric-gate-explicit-digits.md`)。`0` と負値は
  # プロセスグループ / 全プロセスを意味するので通さない。
  case "$TT_PROBE_PID" in ''|0|*[!0123456789]*) TT_PROBE_PID="" ;; esac
  [ "${#TT_PROBE_PID}" -le 9 ] || TT_PROBE_PID=""
  return 0
}

# tt__pid_of_socket は socket ファイルの持ち主を外から引く (ハング中のサーバは自分では答えられない)。
# 曖昧なとき (複数ヒット / lsof が無い / 数字以外) は**何も返さない** = 撃たない側へ倒す。
tt__pid_of_socket() { # tt__pid_of_socket <socket パス>
  local out="" dir=""
  command -v lsof >/dev/null 2>&1 || return 0
  # 🚨 **実パスへ解決してから引く**。lsof は symlink を辿らないので、macOS の
  # `/tmp` → `/private/tmp` を経由したパスでは**常に空を返す** (= 回収できないのに
  # 「持ち主が居ない」と読める。実測 2026-09-15)。組み立てたパスは必ずこの形になる。
  dir=$(cd -- "$(dirname -- "$1")" 2>/dev/null && pwd -P) || return 0
  out=$(lsof -t -- "$dir/$(basename -- "$1")" 2>/dev/null | head -2)
  # 🚨 **basename が symlink でも実体へは届かない** (実測 2026-09-15: `lsof -t -- <symlink>` は
  # 空を返す。実体のパスなら pid を返す)。`pwd -P` が解決するのは dir 側だけなので、
  # 「socket dir に本番 socket への別名を置いて撃たせる」経路はここで成立しない。
  # 追従するようになったら `default` の除外 (呼び出し側) は basename しか見ていないので素通りする
  case "$out" in ''|*[!0123456789]*) return 0 ;; esac   # 複数行は改行を含むのでここで落ちる
  printf '%s' "$out"
}

# tt__rm_socket は socket ファイルを消す。**死亡を確認した経路からしか呼ばない**。
tt__rm_socket() { # tt__rm_socket <socket パス>
  case "${1##*/}" in default) return 0 ;; esac
  [ -S "$1" ] && rm -f -- "$1"
  return 0
}

# tt_tmux_kill_socket は `-L <name>` のサーバを止め、**死んだことを確認してから** socket ファイルを
# 消す。サーバが既にいなければ、既定の socket dir から名前で組み立てて消す (中断で trap が
# 走らなかった前回の残骸を、次の run が回収できるようにするため)。
tt_tmux_kill_socket() { # tt_tmux_kill_socket <-L の名前>
  local name="$1" path="" pid="" comm=""
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
  # (`list-masked-failure-modes-before-removing-guard.md`: 「冗長」と書いた防御が
  #  実際には kill 経路を 1 mm も守っていなかった)
  case "$name" in default) return 0 ;; esac
  tt__probe "$name"
  pid="$TT_PROBE_PID"; path="$TT_PROBE_PATH"
  if [ -z "$path" ]; then
    # 答えが無かった (死んでいる / 返らない): 既定の場所を組み立てる (TMUX_TMPDIR を尊重する)
    path="${TMUX_TMPDIR:-/tmp}/tmux-$(id -u)/$name"
  fi
  # パス側でももう一度弾く (名前は default でなくても、組み立てたパスが本番を指す形を防ぐ)。
  # 🚨 **こちらは二重の保険であって主防御ではない**。主防御は関数の先頭の名前チェック。
  # 🚨 **kill より前に置く**。KILL を撃つようになったので、rm の手前だけでは遅い
  # (敵対レビューが decoy サーバで「ここが load-bearing」だと実証した)。
  case "${path##*/}" in default) return 0 ;; esac
  # 🚨 `~/.config/tmux-protected-sockets` (bin/tmux shim が読む一覧) は**ここでは見ない**。
  # 正規化と照合を別実装で持つと 2 つの判定が必ず食い違う (`adversarial-review-own-safeguards.md`
  # §0-B)。主防御は名前チェックで、テストが渡すのは自分で作った `-L <prefix>-$$` だけ。
  # **trigger: その一覧が非空になったら** shim 側へ公開関数を作ってここから呼ぶ (今は空なので実害なし)。
  [ "$TT_PROBE_STATE" = dead ] && { tt__rm_socket "$path"; return 0; }
  tt__run_bounded discard 3 tmux -L "$name" kill-server
  tt__probe "$name"
  [ -n "$TT_PROBE_PID" ] && pid="$TT_PROBE_PID"
  [ "$TT_PROBE_STATE" = dead ] && { tt__rm_socket "$path"; return 0; }
  # まだ生きている / 返らない。pid を確定させて KILL へ昇格する。
  # 🚨 hung のときは相手が自分の pid を答えられないので、socket の持ち主を外から引く。
  [ -n "$pid" ] || pid=$(tt__pid_of_socket "$path")
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
    if ! kill -0 "$pid" 2>/dev/null; then tt__rm_socket "$path"; return 0; fi
  fi
  printf 'tt_tmux_kill_socket: サーバを止められない (state=%s pid=%s socket=%s)。socket は残す。\n' \
    "$TT_PROBE_STATE" "${pid:-不明}" "$path" >&2
  printf '  回収: lsof -t -- %s で pid を引いて kill -9 する\n' "$path" >&2
  return 1
}
