#!/usr/bin/env bash
# 隔離 tmux サーバの後始末が **socket ファイルまで** 消すことを固定する (issue 305 ①)。
#
# 🚨 なぜ: **`tmux kill-server` は socket ファイルを消さない** (実測 2026-09-10。SIGKILL でも同じ)。
# つまり中断時だけでなく**正常終了のたびに 1 個ずつ漏れる**。同日の実測で
# `/private/tmp/tmux-501/` に 536 ファイルあり、生きているのは `default` (本番) の 1 個だけだった
# (最古 2026-07-05)。大半は `ctrlv-test-*` / `pane-state-bell-*` = `-L <name>-$$` を使うテスト。
#
# 🚨 **脅威モデル**: 止めるのは「テストが自分の socket / 一時 dir を置き去りにする」形だけ。
# 掃除機構 (母集合を走査して消す) は作らないので、他人のものを消す経路は原理的に無い。
#
# 🚨 **検出しないと決めた形** (敵対レビュー 3 周目で射程を実装と突き合わせた):
#   - **中断 (SIGKILL) で trap が走らない場合**。名前に `$$` が入るので次の run では回収されない。
#     `/private/tmp` は**起動時に一掃される**ので放置しても溜まり続けはしない (実測 2026-09-10:
#     uptime 67 日 / 起動より古いエントリ 0 件 / `/etc/periodic` の掃除は無し)
#   - **行内コメントの中の `mktemp`** は**偽陽性**になる (`X=$(id -u)  # 以前は mktemp -d だった`)。
#     落としているのは**行頭コメントだけ**。正規表現をこれ以上広げない (§8: 迂回を潰し続けない)
#   - `install -d` / `mkdir -p` で dir を作る形、heredoc で書き出す子スクリプトの中の `mktemp`
#   - 🚨 **「変数経由」は除外していない**。`MK=mktemp` は `=` が語境界なので**定義行が検出される**
#     (実測)。除外されているのは変数経由の *削除* であって、変数経由の mktemp ではない
#
# ⑥ は socket ではなく **一時 dir の作り方**を見る。`reap_mktemp_d` が lib に在るので、
# 検査は「テスト本体に素の `mktemp` が 1 つも無いか」で済み、**行範囲を切り出すアンカーが要らない**
# (3 周目 P1-2: `}` に行末コメントを足すだけで信頼窓が 6 行 → 51 行へ無警告で広がっていた)。
set -uo pipefail
unset CDPATH
unset TMUX TMUX_PANE   # 🚨 $TMUX は TMUX_TMPDIR より優先される。残すと本番を向く

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=tests/tmux/lib/kill_socket.sh
. "$ROOT_DIR/tests/tmux/lib/kill_socket.sh"

fails=0
checks=0
ok()  { checks=$((checks + 1)); printf '✓ %s\n' "$1"; }
bad() { checks=$((checks + 1)); printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }

# --- 後始末: **ファイル全体を 1 本の trap で覆う** -------------------------------------------
#
# 🚨 旧版は canary ファイルだけを消す trap を張り、その直後に**解除**していた。以降に作る
# `guard_dir` と ①②③ の socket 3 個は誰も消さず、**変異検証 (わざと落とす実行) のたびに
# 3 個ずつ漏れた** (実測 2026-09-10: `/private/tmp/tmux-501/` に `tt-cleanup-*` が 97 件、
# すべて当日のこのテスト由来)。**305 の failure mode を、その修正を検証するテストが
# 再生産していた**形。しかも commit の A-B は `/tmp/reap*` の dir しか数えておらず、
# この漏れを**構造的に測れなかった** (§6「その計測が構造的に見落とすもの」)。
#
# 🚨 **後始末に `tt_tmux_kill_socket` (検査対象そのもの) を使わない**。使うと、それを壊す
# 変異を当てたときに後始末も一緒に壊れて漏れる。ここは独立に kill + rm する。
TT_SOCKETS=()
tt_new_socket() {  # tt_new_socket <変数名> <prefix> — 名前を決めた瞬間に登録する
  local name="$2-$$"
  TT_SOCKETS+=("$name")
  printf -v "$1" '%s' "$name"
}
canary_src=""
guard_dir=""
shim_dir=""
hang_dir=""
# 🚨 shellcheck は `printf -v "$1"` の間接代入を追えない (SC2154) ので、ここで宣言しておく。
# 消すと lint_test_scripts.sh が「referenced but not assigned」で落ちる
raw=""; s1=""; s2=""; s7=""; s11=""
TT_FAKE_PIDS=()
tt_cleanup_all() {
  local s p uid; uid=$(id -u)
  for p in ${TT_FAKE_PIDS+"${TT_FAKE_PIDS[@]}"}; do kill -KILL "$p" 2>/dev/null || :; done
  [ -z "$hang_dir" ] || rm -rf -- "$hang_dir"
  [ -z "$canary_src" ] || rm -f -- "$canary_src"
  [ -z "$guard_dir" ] || rm -rf -- "$guard_dir"
  [ -z "$shim_dir" ] || rm -rf -- "$shim_dir"
  for s in ${TT_SOCKETS+"${TT_SOCKETS[@]}"}; do
    [ "$s" = default ] && continue          # 🚨 本番は絶対に触らない
    tmux -L "$s" kill-server 2>/dev/null || :
    p="${TMUX_TMPDIR:-/tmp}/tmux-$uid/$s"
    [ -e "$p" ] && rm -f -- "$p"
  done
  return 0
}
trap tt_cleanup_all EXIT INT TERM HUP


# --- ⓪ 🚨 default には tmux コマンドを 1 つも撃たない (本番 kill の回帰テスト) --------------
#
# 2026-09-11 00:28、このテストが**本番サーバ (30 セッション) を kill した**。
#   tmux -L default kill-server  <-  bash tests/tmux/test_socket_cleanup.sh
# 経路: ④ が `TMUX_TMPDIR="$guard_dir" tt_tmux_kill_socket default` を呼ぶ →
# `guard_dir` を作る mktemp が失敗して**空**になる (存在しない TMPDIR を渡された) →
# **tmux は TMUX_TMPDIR が空 / 不在だと /private/tmp/tmux-<uid>/ へフォールバックする** →
# 本番へ kill-server。当時 `default` の除外は `rm` の手前にしか無く、`kill` は素通りだった。
#
# 🚨 この検査は **tmux を 1 度も起動しない**。PATH 先頭に「記録するだけの shim」を置き、
# `tt_tmux_kill_socket default` が **1 つもコマンドを発行しない**ことを見る。
# (shim は実体を exec しないので `path-shim-must-resolve-real-binary.md` の無限再帰は起きない)
shim_dir=$(mktemp -d "${TMPDIR:-/tmp}/tt-shim.XXXXXX" 2>/dev/null) || shim_dir=""
if [ -z "$shim_dir" ] || [ ! -d "$shim_dir" ]; then
  bad "⓪ の shim dir を作れない (TMPDIR=${TMPDIR:-/tmp})。本番 kill の回帰検査が走らない"
else
  cat > "$shim_dir/tmux" <<SHIM
#!/bin/sh
printf '%s\n' "\$*" >> "$shim_dir/calls"
exit 0
SHIM
  chmod +x "$shim_dir/tmux"
  : > "$shim_dir/calls"
  # 🚨 **TMUX_TMPDIR を空にした最悪条件**で呼ぶ (事故当時と同じ状態)
  ( PATH="$shim_dir:$PATH"; TMUX_TMPDIR=""; tt_tmux_kill_socket default ) >/dev/null 2>&1 || :
  shim_calls=$(grep -c . "$shim_dir/calls" 2>/dev/null || true)
  [ -n "$shim_calls" ] || shim_calls=0
  if [ "$shim_calls" -eq 0 ]; then
    ok "🚨 default には tmux コマンドを 1 つも撃たない (TMUX_TMPDIR が空でも)"
  else
    bad "🚨 default に tmux コマンドを $shim_calls 回撃った (本番サーバを kill しうる): $(tr '\n' ';' < "$shim_dir/calls")"
  fi
  # 対照: default 以外なら撃つ (⓪ が「常に 0 件」を見ているだけの vacuous な検査でないこと)
  : > "$shim_dir/calls"
  ( PATH="$shim_dir:$PATH"; TMUX_TMPDIR="$shim_dir"; tt_tmux_kill_socket tt-shim-probe ) >/dev/null 2>&1 || :
  other_calls=$(grep -c . "$shim_dir/calls" 2>/dev/null || true)
  [ -n "$other_calls" ] || other_calls=0
  if [ "$other_calls" -gt 0 ]; then
    ok "対照: default 以外の名前なら tmux を呼ぶ (⓪ が vacuous でない)"
  else
    bad "対照が壊れている: 通常の名前でも tmux を 1 度も呼んでいない (⓪ は何も守っていない)"
  fi
  rm -rf -- "$shim_dir"; shim_dir=""
fi

# --- ⑥ 一時 dir が「作成と同時に登録される」形を保っていること -----------------------------
#
# 🚨 tmux を起こす ① より前に置く (①〜④ は tmux が無いと exit 77 = skip で終わるため)。
# 🚨 軸を構文から外した経緯と「検出しないと決めた形」はファイル冒頭に書いてある。
REAP="$ROOT_DIR/tests/tmux/test_reap_orphan_servers.sh"
LIB="$ROOT_DIR/tests/tmux/lib/reap_mktemp.sh"

# 非コメント行の mktemp の行番号。
# 🚨 **語境界で照合する**。素の `grep mktemp` は `reap_mktemp_d …` という**呼び出し側の識別子**
# にも当たり、全呼び出しを違反として報告する (最初そう書いて 6 件の偽陽性が出た)。
mktemp_lines() { # mktemp_lines <file>
  grep -nE '(^|[^A-Za-z0-9_])mktemp([^A-Za-z0-9_]|$)' "$1" |
    grep -v '^[0-9]*:[[:space:]]*#' | cut -d: -f1
}

# 🚨 **入力の不在を緑にしない** (3 周目 P3)。ファイルが読めないと抽出が空になり、
# 「違反 0 件」で通る。canary は**壊れた抽出器**は守るが**入力の不在**は守らない。
for f in "$REAP" "$LIB"; do
  [ -r "$f" ] || bad "検査対象が読めない: $f (rename/削除された? この状態の ⑥ は何も守らない)"
done

# canary — 本走査と**同じ関数**に既知の入力を通す。
# 🚨 期待値を**行番号で持たない** (3 周目 P3)。fixture に 1 行足すだけで壊れ、しかも
# 「抽出が壊れている」というメッセージが本物の退行と区別できなくなる。行に置いた sentinel で見る。
canary_src=$(mktemp "${TMPDIR:-/tmp}/socket_cleanup_canary.XXXXXX")
cat > "$canary_src" <<'CANARY'
reap_mktemp_d A_DIR /tmp/a.XXXXXX
  typeset B_DIR="$(mktemp -q -d /tmp/b.XXXXXX)"   # SENTINEL_B
export C_DIR=$(mktemp -d /tmp/c.XXXXXX); D_DIR=$(mktemp -d /tmp/d.XXXXXX)   # SENTINEL_C
# コメント行の mktemp -d は拾わない (SENTINEL_NEVER)
MK=mktemp   # SENTINEL_VAR — 変数経由も定義行で捕まる (除外していない)
CANARY
canary_got=$(while IFS= read -r n; do
    [ -n "$n" ] || continue
    sed -n "${n}p" "$canary_src" | grep -oE 'SENTINEL_[A-Z]+' || printf 'NO_SENTINEL_AT_%s\n' "$n"
  done < <(mktemp_lines "$canary_src") | sort | tr '\n' ' ')
if [ "$canary_got" = "SENTINEL_B SENTINEL_C SENTINEL_VAR " ]; then
  ok "canary: 綴りに依存せず拾う (typeset / export / -q -d / 1 行 2 つ / 変数経由) & コメント行は拾わない"
else
  bad "canary: 抽出が壊れている (拾った sentinel = [$canary_got])。この状態の ⑥ は何も守らない"
fi
rm -f -- "$canary_src"; canary_src=""

# 本走査 — ヘルパーが lib に在るので、テスト本体に素の mktemp は **1 つも無い**のが正
stray=$(mktemp_lines "$REAP" | tr '\n' ' ')
if [ -z "$stray" ]; then
  ok "test_reap_orphan_servers.sh: 素の mktemp が 1 つも無い (すべて reap_mktemp_d 経由)"
else
  bad "test_reap_orphan_servers.sh: ヘルパーを通らない mktemp が行 $stray に在る (登録されないので cleanup が消せない)"
fi
# 🚨 **呼び出し側も pin する** (3 周目 P2)。`X=$(reap_mktemp_d …)` はコマンド置換 = サブシェル
# なので、値は親へ届くのに**登録は届かず**、正常終了のたびに 1 個ずつ漏れる (最小再現で実証済み)
if grep -qE '\$\(.*reap_mktemp_d' "$REAP"; then
  bad "test_reap_orphan_servers.sh: reap_mktemp_d を \$( ) で受けている (登録がサブシェルに閉じる)"
else
  ok "test_reap_orphan_servers.sh: reap_mktemp_d を \$( ) で受けていない"
fi
# 🚨 **実際の source 文を見る**。素の `grep 'reap_mktemp.sh'` は直前の
# `# shellcheck source=tests/tmux/lib/reap_mktemp.sh` という**コメント**に当たり、
# source を消す変異が緑で通った (自分で変異を当てて見つけた)
if grep -qE '^[[:space:]]*\.[[:space:]].*reap_mktemp\.sh' "$REAP" && grep -q 'reap_cleanup_tmpdirs' "$REAP"; then
  ok "test_reap_orphan_servers.sh: lib を source し、cleanup で登録ぶんを消している"
else
  bad "test_reap_orphan_servers.sh が lib を使っていない (source 文 / reap_cleanup_tmpdirs が無い)"
fi
# 🚨 **この検査自身の後始末も pin する**。3 周目 P1-1 は「trap を途中で解除して以降が無防備」
# だった。自分が満たしていない基準を相手に課さない (一時ファイルの後始末を守る検査が自分で漏らす)
SELF="${BASH_SOURCE[0]}"
# 🚨 **行頭アンカーで見る**。素の部分一致だと**この検査行そのもの**が pin を満たしてしまい、
# 本物の trap を消す変異が緑で通った (自分で変異を当てて見つけた。pin の自己参照)
if grep -qE '^trap tt_cleanup_all EXIT INT TERM HUP$' "$SELF" && ! grep -qE '^[[:space:]]*trap[[:space:]]+-[[:space:]]' "$SELF"; then
  ok "この検査自身: 後始末の trap がファイル全体を覆い、途中で解除していない"
else
  bad "この検査自身の trap が弱い (tt_cleanup_all がファイル全体を覆っていない / trap - で解除している)"
fi
if grep -q 'REAP_TMPDIRS+=(' "$LIB" && grep -q 'rm -rf "\${REAP_TMPDIRS\[@\]}"' "$LIB"; then
  ok "lib: 作った dir を登録し、まとめて消す関数を持つ"
else
  bad "lib が登録 / 一括削除を持っていない (reap_mktemp.sh)"
fi
# 🚨 ヘルパーがパスを stdout で返す形へ戻っていないか (戻すと呼び出し側が \$( ) を使いたくなる)
if grep -qE '^[[:space:]]*(printf|echo)[^#]*\$d' "$LIB"; then
  bad "reap_mktemp_d がパスを stdout で返している (呼び出し側が \$( ) で受けると登録がサブシェルに閉じる)"
else
  ok "reap_mktemp_d は stdout で返さず typeset -g で呼び出し元へ入れる"
fi


# --- ⑦〜⑩ 🚨 「kill したつもり」で socket だけ消す形を塞ぐ (issue 377) --------------------
#
# 377 の実測ハーネスは後始末で `kill -9 <pid>` を撃つだけで**死んだことを確認していなかった**。
# ハングしたサーバ (CPU 100% / `kill-server` も `display-message` も返らない) が生き残り、
# **23 分間回り続けた** (2026-09-15)。しかも socket は消されているので、tmux 越しには
# もう誰も触れない。ここは「kill → 不在を確認 → socket 削除」の順と、確認できなかったときに
# **socket を残して失敗を返す**ことを固定する。
#
# 🚨 実 tmux は使わない。ハングするサーバを本物で作ると、このテスト自身が 377 を踏む。
# PATH stub で「kill-server に応答しないサーバ」を演じさせ、実体は使い捨てのプロセスにする。
hang_dir=$(mktemp -d "${TMPDIR:-/tmp}/tt-hang.XXXXXX" 2>/dev/null) || hang_dir=""
if [ -z "$hang_dir" ] || [ ! -d "$hang_dir" ]; then
  bad "⑦〜⑩ の隔離 dir を作れない (TMPDIR=${TMPDIR:-/tmp})"
else
  tt_spawn() {  # tt_spawn <実行ファイル> -> REPLY_PID
    # 🚨 `( trap - EXIT; exec ... ) &` で起こす (lib/stub_env.sh と同じ理由: fork 直後に
    #    kill されると子が EXIT trap を継承して cleanup を走らせる)
    ( trap - EXIT; exec "$1" 300 ) &
    REPLY_PID=$!
    TT_FAKE_PIDS+=("$REPLY_PID")
  }
  # 🚨 `&` の直後は exec 前なので、`kill -0` が真でも「起動した」とは言えない。
  # 実体が走り出すのを条件で待つ (壁時計の `sleep` にしない)。
  tt_wait_alive() {  # tt_wait_alive <pid>
    local p="$1" i=0
    while [ "$i" -lt 100 ]; do
      [ "$(ps -o comm= -p "$p" 2>/dev/null | sed 's|.*/||')" = sleep ] && return 0
      sleep 0.05; i=$((i + 1))
    done
    return 1
  }
  # `tmux` の stub。display は仕込んだ答えを返し、kill-server は**何もしない** (= 届かない server)
  tt_stub() {  # tt_stub <display が返す文字列>
    cat > "$hang_dir/tmux" <<STUB
#!/bin/sh
case "\$*" in
  *display*) printf '%s\n' '$1' ;;
  *) : ;;
esac
exit 0
STUB
    chmod +x "$hang_dir/tmux"
  }
  tt_mksock() {  # tt_mksock <パス>
    python3 -c 'import socket,sys; s=socket.socket(socket.AF_UNIX); s.bind(sys.argv[1])' "$1" 2>/dev/null
  }

  # --- ⑧ 🚨 死を確認できなければ socket を消さず、pid を出して失敗を返す ----------------------
  #
  # 素性 (`ps -o comm=`) が tmux でない pid は撃たない = 生き残る。そのとき socket を消すと
  # 「誰も触れない生きたサーバ」を作るので、**残す**のが正しい。
  sock8="$hang_dir/sock8"; tt_mksock "$sock8"
  if [ ! -S "$sock8" ]; then
    bad "⑧ の前提: unix socket を作れない ($sock8)"
  else
    tt_spawn /bin/sleep; pid8=$REPLY_PID          # comm=sleep → 素性チェックに落ちる
    # 🚨 前提: 実体が生きていること。死んでいると「撃たなかった」と見分けが付かない
    tt_wait_alive "$pid8" || bad "⑧ の前提: 実体 (pid=$pid8) が起動していない"
    tt_stub "$pid8 $sock8"
    ( PATH="$hang_dir:$PATH"; tt_tmux_kill_socket tt-hang-8 ) >/dev/null 2>"$hang_dir/err8"; rc8=$?
    if ! kill -0 "$pid8" 2>/dev/null; then
      bad "⑧ 素性が tmux でない pid を KILL した (pid=$pid8 が消えた)"
    elif [ ! -S "$sock8" ]; then
      bad "⑧ サーバが生きているのに socket を消した (tmux 越しに触れないサーバが残る)"
    elif [ "$rc8" -eq 0 ]; then
      bad "⑧ サーバが生き残ったのに rc=0 (沈黙で成功にしている)"
    elif ! grep -q "$pid8" "$hang_dir/err8" 2>/dev/null; then
      bad "⑧ stderr に pid が出ていない (残骸を手で片付ける手がかりが無い): $(cat "$hang_dir/err8")"
    else
      ok "⑧ 死を確認できなければ socket を残し、pid を出して rc≠0 を返す"
    fi
    kill -KILL "$pid8" 2>/dev/null || :
    rm -f -- "$sock8"
  fi

  # --- ⑨ pid のゲート: `kill` に渡る前に数字以外を落とす ---------------------------------------
  #
  # 🚨 判定は **`kill` が何を受け取ったか**を記録して見る。結果 (rc / socket) では判別できない:
  # ゲートを外して pid=-1 が通っても、`ps -o comm= -p -1` が空を返して KILL は撃たれず、
  # 「生きている」扱いで rc=1 + socket 残りになり、**ゲートがある場合と同じ結果**になる
  # (敵対レビュー P2-2 の実測)。`kill` はシェル関数でビルトインより先に解決されるので、
  # 引数を記録してから `builtin kill` へ委譲する。
  sock9="$hang_dir/sock9"; tt_mksock "$sock9"
  if [ ! -S "$sock9" ]; then
    bad "⑨ の前提: unix socket を作れない ($sock9)"
  else
    tt_stub "-1 $sock9"
    : > "$hang_dir/kill9"
    (
      PATH="$hang_dir:$PATH"
      kill() { printf '%s\n' "$*" >> "$hang_dir/kill9"; builtin kill "$@"; }
      tt_tmux_kill_socket tt-hang-9
    ) >/dev/null 2>&1; rc9=$?
    # 🚨 前提: 関数が最後まで走ったこと。早期 return だと「kill へ渡らなかった」が
    # 「そもそも判定へ到達しなかった」と見分けが付かない (ゲートを外すと -1 が記録される側)
    if [ "$rc9" -eq 0 ]; then
      bad "⑨ の前提が崩れた: 止められていないのに rc=0 で戻った"
    elif grep -qE '(^| )-1( |$)' "$hang_dir/kill9"; then
      bad "⑨ pid=-1 (シグナルを撃てる全プロセス) が kill へ渡った: $(tr '\n' ';' < "$hang_dir/kill9")"
    else
      ok "⑨ 数字でない pid はゲートで捨てる (kill へ 1 度も渡らない)"
    fi
    rm -f -- "$sock9"
  fi

  # --- ⑩ 🚨 ハングしたサーバの socket は**消さない** (敵対レビュー P1-1 の回帰) ----------------
  #
  # 377 の実症状は「`display -p` すら返らない」。このとき pid は取れないので、
  # 「答えが無い = 死んだ」と読むと**生きているサーバの唯一の handle を消す**ことになる。
  # ここでは `lsof` も答えない (socket の持ち主が居ない fixture) ので、確定できない側 =
  # socket を残して rc≠0 を返すのが正解。あわせて、後始末自身が固まらないことも見る。
  # 🚨 socket は**関数が組み立てるパス**に作る。別の場所に置くと「消されなかった」が
  # 自明に成り立ち、何も守らない (`TMUX_TMPDIR` を hang_dir に差し替えて閉じ込める)
  mkdir -p "$hang_dir/tmux-$(id -u)"; chmod 700 "$hang_dir/tmux-$(id -u)"
  sock10="$hang_dir/tmux-$(id -u)/tt-hang-10"; tt_mksock "$sock10"
  cat > "$hang_dir/tmux" <<STUB
#!/bin/sh
echo \$\$ > "$hang_dir/hangpid"
exec sleep 300                 # 🚨 display も停止要求も返らない (= 377 のハング)
STUB
  chmod +x "$hang_dir/tmux"
  rm -f -- "$hang_dir/hangpid" "$hang_dir/done10"
  if [ ! -S "$sock10" ]; then
    bad "⑩ の前提: unix socket を作れない ($sock10)"
  else
    (
      PATH="$hang_dir:$PATH"
      TMUX_TMPDIR="$hang_dir"      # 再構成されるパスをこの dir 配下に閉じ込める
      tt_tmux_kill_socket tt-hang-10 >/dev/null 2>"$hang_dir/err10"
      echo $? > "$hang_dir/done10"
    ) &
    wait10=$!
    i=0
    while [ "$i" -lt 600 ]; do          # 上限 30s (bound は 3s x 2。上限は「無限に待たない」安全網)
      [ -s "$hang_dir/done10" ] && break
      sleep 0.05; i=$((i + 1))
    done
    rc10=$(cat "$hang_dir/done10" 2>/dev/null || true)
    hp=$(cat "$hang_dir/hangpid" 2>/dev/null || true)
    if [ -z "$rc10" ]; then
      bad "⑩ ハングしたサーバへの問い合わせで後始末自身が固まった (30s 経っても戻らない)"
      kill -KILL "$wait10" 2>/dev/null || :
      [ -n "$hp" ] && kill -KILL "$hp" 2>/dev/null || :
    elif [ ! -S "$sock10" ]; then
      bad "🚨 ⑩ ハングしたサーバの socket を消した (誰も触れない CPU 100% のサーバが残る)"
    elif [ "$rc10" = 0 ]; then
      bad "⑩ 止められていないのに rc=0 を返した (沈黙で成功にしている)"
    elif ! grep -q "$sock10" "$hang_dir/err10" 2>/dev/null; then
      bad "⑩ stderr に socket パスが出ていない (回収の手がかりが無い): $(cat "$hang_dir/err10")"
    elif [ -z "$hp" ]; then
      bad "⑩ の前提が崩れた: stub が 1 度も呼ばれていない"
    elif kill -0 "$hp" 2>/dev/null; then
      bad "⑩ 時間切れの client が残っている (pid=$hp)。bounded の後始末が実体へ届いていない"
      kill -KILL "$hp" 2>/dev/null || :
    else
      ok "⑩ ハングしたサーバは socket を残して rc≠0 (後始末自身は固まらず、待ち client も残さない)"
    fi
    rm -f -- "$sock10"
  fi

  rm -rf -- "$hang_dir"; hang_dir=""
fi

# --- ① 前提: kill-server だけでは socket が残る (この検査が守っている事実そのもの) ------------
#
# 🚨 これを canary として先に確かめる。もし tmux 側が将来 socket を消すようになったら、
# この検査は「何も守っていない」状態になるので、そのときに気づけるようにしておく。
tt_new_socket raw tt-cleanup-raw
tmux -L "$raw" -f /dev/null new-session -d 'sleep 30' >/dev/null 2>&1 || {
  # 🚨 ⑥ で既に違反を見つけているなら skip に畳まない (skip は緑に見える)
  [ "$fails" -eq 0 ] || { printf '✗ tmux は起動できないが、⑥ が %d 件の違反を出している\n' "$fails" >&2; exit 1; }
  echo "SKIP: tmux を起動できない"; exit 77
}
raw_path=$(tmux -L "$raw" display -p '#{socket_path}' 2>/dev/null)
tmux -L "$raw" kill-server 2>/dev/null || :
if [ -S "$raw_path" ]; then
  ok "前提: kill-server は socket ファイルを残す (この検査が要る理由)"
  rm -f -- "$raw_path"
else
  bad "前提が崩れた: kill-server が socket を消すようになった (この検査はもう何も守っていない。tt_tmux_kill_socket ごと見直すこと)"
fi

# --- ② ヘルパーは socket ファイルまで消す ------------------------------------------------------
tt_new_socket s1 tt-cleanup-a
tmux -L "$s1" -f /dev/null new-session -d 'sleep 30' >/dev/null 2>&1
p1=$(tmux -L "$s1" display -p '#{socket_path}' 2>/dev/null)
[ -S "$p1" ] || bad "前提: socket が作られていない ($p1)"
tt_tmux_kill_socket "$s1"
if [ -e "$p1" ]; then bad "tt_tmux_kill_socket が socket ファイルを残した: $p1"; rm -f -- "$p1"
else ok "tt_tmux_kill_socket は socket ファイルまで消す"; fi

# --- ③ 既に死んでいるサーバの socket も回収する ------------------------------------------------
tt_new_socket s2 tt-cleanup-b
tmux -L "$s2" -f /dev/null new-session -d 'sleep 30' >/dev/null 2>&1
p2=$(tmux -L "$s2" display -p '#{socket_path}' 2>/dev/null)
tmux -L "$s2" kill-server 2>/dev/null || :      # 先に殺す = display -p が失敗する状態を作る
[ -S "$p2" ] || bad "前提: 先に kill しても socket は残るはず"
tt_tmux_kill_socket "$s2"
if [ -e "$p2" ]; then bad "死んでいるサーバの socket を回収できていない: $p2"; rm -f -- "$p2"
else ok "サーバが既に死んでいても socket を回収する"; fi

# --- ④ 🚨 default (本番) は絶対に消さない ------------------------------------------------------
#
# 名前を組み立てる経路 (③) が誤っても本番へ届かないことを固定する。
# 🚨 **テンプレートを明示する**。macOS の `mktemp -d` は引数なしだと `$TMPDIR` を無視して
# Darwin のユーザ一時領域へ出るので、呼び出し側から隔離できない (3 周目 P3 の実測)
# 🚨 **mktemp の失敗で止まる**。`set -e` が無いので、失敗しても空文字のまま先へ進んでいた。
# 空の TMUX_TMPDIR は本番へフォールバックするので、この 1 行が本番 kill の引き金になった
# (2026-09-11 00:28。TMPDIR に存在しない dir を渡したサブエージェントの実行で発火)
guard_dir=$(mktemp -d "${TMPDIR:-/tmp}/tt-guard.XXXXXX" 2>/dev/null) || guard_dir=""
if [ -z "$guard_dir" ] || [ ! -d "$guard_dir" ]; then
  bad "④ の隔離 dir を作れない (TMPDIR=${TMPDIR:-/tmp})。空の TMUX_TMPDIR は本番へ届くので検査を中止する"
  printf '\n検査 %d 件: fail=%d\n' "$checks" "$fails" >&2
  exit 1
fi
mkdir -p "$guard_dir/tmux-$(id -u)"
guard="$guard_dir/tmux-$(id -u)/default"
python3 -c 'import socket,sys; s=socket.socket(socket.AF_UNIX); s.bind(sys.argv[1])' "$guard" 2>/dev/null || : > "$guard"
TMUX_TMPDIR="$guard_dir" tt_tmux_kill_socket default
if [ -e "$guard" ]; then ok "default という名前の socket は消さない"
else bad "🚨 default を消した (本番の socket を消しうる)"; fi
rm -rf -- "$guard_dir"; guard_dir=""

# --- ⑦ 🚨 停止要求が届かないサーバは KILL へ昇格し、死を確認してから socket を消す -----------
#
# issue 377 の本体。実サーバを**本当にハングさせる**と、このテスト自身が 377 を踏んで
# CPU 100% のプロセスを置き去りにする。そこで「実 tmux サーバ + 停止要求を握り潰す stub client」で、
# サーバ側から見た同じ状況 (コマンドが届かない) を作る。
#
# 🚨 **撃ったシグナルを記録して assert する**。結果 (サーバが死んだか) だけを見ると、
# `-KILL` を `-TERM` へ弱める退行が緑のまま通る (健全なサーバは TERM でも死ぬため。
# 敵対レビュー P2-1 の実測)。377 の定義的性質は「復旧は `kill -9` のみ」なので、種別が要点。
tt_new_socket s7 tt-cleanup-hang
tmux -L "$s7" -f /dev/null new-session -d 'sleep 300' >/dev/null 2>&1
p7=$(tmux -L "$s7" display -p '#{socket_path}' 2>/dev/null)
pid7=$(tmux -L "$s7" display -p '#{pid}' 2>/dev/null)
# 🚨 **サーバを SIGKILL すると pane の子は道連れにならない** (launchd へ里子化して残る。
# 実測 2026-09-15: この検査自身が `sleep 300` を 1 個置き去りにした)。掃除対象に登録する
pane7=$(tmux -L "$s7" display -p '#{pane_pid}' 2>/dev/null)
case "$pane7" in ''|*[!0123456789]*) pane7="" ;; *) TT_FAKE_PIDS+=("$pane7") ;; esac
shim_dir=$(mktemp -d "${TMPDIR:-/tmp}/tt-hang7.XXXXXX" 2>/dev/null) || shim_dir=""
if [ -z "$shim_dir" ] || [ ! -d "$shim_dir" ] || [ ! -S "$p7" ] || [ -z "$pid7" ] \
   || ! kill -0 "$pid7" 2>/dev/null; then
  bad "⑦ の前提が崩れている (socket=$p7 pid=$pid7 shim=$shim_dir)"
else
  cat > "$shim_dir/tmux" <<STUB
#!/bin/sh
case "\$*" in
  *display*) printf '%s\n' '$pid7 $p7' ;;
  *) : ;;                       # 🚨 停止要求は握り潰す (= 届かないサーバ)
esac
exit 0
STUB
  chmod +x "$shim_dir/tmux"
  : > "$shim_dir/killlog"
  (
    PATH="$shim_dir:$PATH"
    kill() { printf '%s\n' "$*" >> "$shim_dir/killlog"; builtin kill "$@"; }
    tt_tmux_kill_socket "$s7"
  ) >/dev/null 2>&1; rc7=$?
  if kill -0 "$pid7" 2>/dev/null; then
    bad "⑦ 停止要求が届かないサーバを KILL へ昇格していない (pid=$pid7 が CPU を握ったまま残る)"
  elif ! grep -qE '(^| )-KILL( |$)' "$shim_dir/killlog"; then
    bad "⑦ KILL 以外で殺している (377 の復旧は -9 のみ): $(tr '\n' ';' < "$shim_dir/killlog")"
  elif [ -e "$p7" ]; then
    bad "⑦ サーバは死んだのに socket が残っている: $p7"
  elif [ "$rc7" -ne 0 ]; then
    bad "⑦ 片付けは成功しているのに rc=$rc7 を返した"
  else
    ok "⑦ 停止要求が届かないサーバは KILL へ昇格し (種別も固定)、socket まで片付ける"
  fi
  [ -n "$pane7" ] && kill -KILL "$pane7" 2>/dev/null || :
  rm -rf -- "$shim_dir"; shim_dir=""
fi

# --- ⑪ 🚨 完全無応答のサーバを、socket の持ち主を外から引いて回収する -------------------------
#
# 377 の実症状では `display -p` すら返らないので、**pid を聞く相手がいない**。
# ⑦ の経路 (pid を聞けた) だけを持っていると、昇格は実症状では一度も発火しない死にコードになる。
# ここは実 tmux サーバ + **全コマンドが返らない** stub client で、`lsof -t -- <socket>` から
# 持ち主を引いて KILL し、死を確認して socket を消すまでを固定する。
tt_new_socket s11 tt-cleanup-noresp
tmux -L "$s11" -f /dev/null new-session -d 'sleep 300' >/dev/null 2>&1
p11=$(tmux -L "$s11" display -p '#{socket_path}' 2>/dev/null)
pid11=$(tmux -L "$s11" display -p '#{pid}' 2>/dev/null)
pane11=$(tmux -L "$s11" display -p '#{pane_pid}' 2>/dev/null)   # ⑦ と同じ理由で登録する
case "$pane11" in ''|*[!0123456789]*) pane11="" ;; *) TT_FAKE_PIDS+=("$pane11") ;; esac
shim_dir=$(mktemp -d "${TMPDIR:-/tmp}/tt-hang11.XXXXXX" 2>/dev/null) || shim_dir=""
if [ -z "$shim_dir" ] || [ ! -d "$shim_dir" ] || [ ! -S "$p11" ] || [ -z "$pid11" ] \
   || ! kill -0 "$pid11" 2>/dev/null; then
  bad "⑪ の前提が崩れている (socket=$p11 pid=$pid11 shim=$shim_dir)"
else
  printf '#!/bin/sh\nexec sleep 300\n' > "$shim_dir/tmux"   # 🚨 何を聞いても返らない
  chmod +x "$shim_dir/tmux"
  ( PATH="$shim_dir:$PATH"; tt_tmux_kill_socket "$s11" ) >/dev/null 2>&1; rc11=$?
  if kill -0 "$pid11" 2>/dev/null; then
    bad "⑪ 完全無応答のサーバを回収できていない (pid=$pid11 が残る。lsof 経路が効いていない)"
  elif [ -e "$p11" ]; then
    bad "⑪ サーバは死んだのに socket が残っている: $p11"
  elif [ "$rc11" -ne 0 ]; then
    bad "⑪ 回収できているのに rc=$rc11 を返した"
  else
    ok "⑪ 完全無応答でも socket の持ち主を外から引いて回収する (377 の実症状)"
  fi
  [ -n "$pane11" ] && kill -KILL "$pane11" 2>/dev/null || :
  rm -rf -- "$shim_dir"; shim_dir=""
fi

# --- ⑤ 実テストが漏らさないこと (配線の確認) ---------------------------------------------------
#
# 🚨 ヘルパー単体の検査だけでは「呼び出し側が使っている」を 1 mm も守らない
# (mutation-verify-new-tests.md の「計算が正しい ≠ 配線されている」)。
WIRED=(tests/tmux/test_ctrl_v_paste.sh tests/claude/test_tmux_pane_state_bell.sh)
for t in "${WIRED[@]}"; do
  if grep -q 'tt_tmux_kill_socket' "$ROOT_DIR/$t"; then
    ok "$(basename "$t"): tt_tmux_kill_socket を使っている"
  else
    bad "$(basename "$t") が socket を置き去りにする形へ戻っている (kill-server だけでは消えない)"
  fi
done

# 🚨 件数を assert する (「1 件も走らないまま 0 件 fail=0 で緑」を塞ぐ)。
# 🚨 **固定値を書かない**。旧版は dir の個数 (2 周目 P3-D) / ⑤ のファイル列の長さ (3 周目 P3) に
# 結合しており、「対象を 1 つ減らす正しい変更」が閾値割れで red になった。可変部から導出する。
want_checks=$(( 11 + ${#WIRED[@]} + 2 ))   # ⑥ が 8 (対象 2 件の可読性 + canary + 本走査 5) / ⑤ / ①〜④ のうち固定 4 のうち 2 は tmux 依存
[ "$checks" -ge "$want_checks" ] || bad "検査が $checks 件しか走っていない (${want_checks} 件以上のはず)"
printf '\n検査 %d 件: fail=%d\n' "$checks" "$fails"
[ "$fails" -eq 0 ] || exit 1
echo "OK tmux socket cleanup"
