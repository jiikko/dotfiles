#!/usr/bin/env bash
# 隔離 tmux サーバの後始末が **socket ファイルまで** 消すことを固定する (issue 305 ①)。
#
# 🚨 なぜ: **`tmux kill-server` は socket ファイルを消さない** (実測 2026-09-10。SIGKILL でも同じ)。
# つまり中断時だけでなく**正常終了のたびに 1 個ずつ漏れる**。同日の実測で
# `/private/tmp/tmux-501/` に 536 ファイルあり、生きているのは `default` (本番) の 1 個だけだった
# (最古 2026-07-05)。大半は `ctrlv-test-*` / `pane-state-bell-*` = `-L <name>-$$` を使うテスト。
#
# 🚨 **脅威モデル**: 止めるのは「テストが自分の socket を置き去りにする」形だけ。
# 掃除機構 (母集合を走査して消す) は作らないので、他人の socket を消す経路は原理的に無い。
# 🚨 **検出しないと決めた形**:
#   - **中断 (SIGKILL) で trap が走らない場合**。次の run が同じ名前を作らない限り残る
#     (名前に `$$` が入るため)。回収は手動 (手順は issue 305 に実測つきで書いてある)
#   - `TMUX_TMPDIR` を差し替えるテスト。そちらは dir ごと消えるのでこの検査の対象外
#
# ⑥ だけは socket ではなく **その dir 自体**を見る。「dir ごと消える」は上の除外の前提なので、
# その前提が成り立っていること自体を固定する。
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


# --- ① 前提: kill-server だけでは socket が残る (この検査が守っている事実そのもの) ------------
#
# 🚨 これを canary として先に確かめる。もし tmux 側が将来 socket を消すようになったら、
# この検査は「何も守っていない」状態になるので、そのときに気づけるようにしておく。
raw="tt-cleanup-raw-$$"
tmux -L "$raw" -f /dev/null new-session -d 'sleep 30' >/dev/null 2>&1 || { echo "SKIP: tmux を起動できない"; exit 77; }
raw_path=$(tmux -L "$raw" display -p '#{socket_path}' 2>/dev/null)
tmux -L "$raw" kill-server 2>/dev/null || :
if [ -S "$raw_path" ]; then
  ok "前提: kill-server は socket ファイルを残す (この検査が要る理由)"
  rm -f -- "$raw_path"
else
  bad "前提が崩れた: kill-server が socket を消すようになった (この検査はもう何も守っていない。tt_tmux_kill_socket ごと見直すこと)"
fi

# --- ② ヘルパーは socket ファイルまで消す ------------------------------------------------------
s1="tt-cleanup-a-$$"
tmux -L "$s1" -f /dev/null new-session -d 'sleep 30' >/dev/null 2>&1
p1=$(tmux -L "$s1" display -p '#{socket_path}' 2>/dev/null)
[ -S "$p1" ] || bad "前提: socket が作られていない ($p1)"
tt_tmux_kill_socket "$s1"
if [ -e "$p1" ]; then bad "tt_tmux_kill_socket が socket ファイルを残した: $p1"; rm -f -- "$p1"
else ok "tt_tmux_kill_socket は socket ファイルまで消す"; fi

# --- ③ 既に死んでいるサーバの socket も回収する ------------------------------------------------
s2="tt-cleanup-b-$$"
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
guard_dir=$(mktemp -d); mkdir -p "$guard_dir/tmux-$(id -u)"
guard="$guard_dir/tmux-$(id -u)/default"
python3 -c 'import socket,sys; s=socket.socket(socket.AF_UNIX); s.bind(sys.argv[1])' "$guard" 2>/dev/null || : > "$guard"
TMUX_TMPDIR="$guard_dir" tt_tmux_kill_socket default
if [ -e "$guard" ]; then ok "default という名前の socket は消さない"
else bad "🚨 default を消した (本番の socket を消しうる)"; fi
rm -rf "$guard_dir"

# --- ⑤ 実テストが漏らさないこと (配線の確認) ---------------------------------------------------
#
# 🚨 ヘルパー単体の検査だけでは「呼び出し側が使っている」を 1 mm も守らない
# (mutation-verify-new-tests.md の「計算が正しい ≠ 配線されている」)。
for t in tests/tmux/test_ctrl_v_paste.sh tests/claude/test_tmux_pane_state_bell.sh; do
  if grep -q 'tt_tmux_kill_socket' "$ROOT_DIR/$t"; then
    ok "$(basename "$t"): tt_tmux_kill_socket を使っている"
  else
    bad "$(basename "$t") が socket を置き去りにする形へ戻っている (kill-server だけでは消えない)"
  fi
done

# --- ⑥ mktemp -d した dir が cleanup の rm -rf に載っていること ---------------------------
#
# 🚨 test_reap_orphan_servers.sh は `-L` ではなく **TMUX_TMPDIR ごと隔離する**形なので、
# 漏れるのは socket 1 個ではなく **dir** になる。実際にケース E の `PROT_DIR` が
# cleanup の `rm -rf` から漏れており、**正常終了のたびに 1 個ずつ** `/tmp/reapp.*` を
# 置いていた (実測 39 個)。止めるのは「dir を足して cleanup に足し忘れる」形だけ。
REAP="$ROOT_DIR/tests/tmux/test_reap_orphan_servers.sh"
# 🚨 **cleanup 全体でなく `rm -rf` の行だけを見る**。全体だと `kill-server` の
# `TMUX_TMPDIR="$LIVE_DIR"` が当たってしまい、`rm -rf` から外しても緑になる (false green)。
reap_cleanup=$(awk '
  /^cleanup\(\) \{$/ { inside = 1; next }
  inside && /^\}$/     { inside = 0 }
  inside && cont        { print; cont = (/\\$/); next }
  inside && /rm -rf/    { print; cont = (/\\$/) }
' "$REAP")
reap_vars=$(grep -oE '^[A-Za-z_][A-Za-z0-9_]*=\$\(mktemp -d' "$REAP" | sed 's/=\$(mktemp -d$//')
# canary: 抽出が壊れて 0 件になると「違反なし」で緑になる
reap_n=$(grep -c '[A-Za-z]' <<< "$reap_vars" || true)
if [ -z "$reap_cleanup" ] || [ "$reap_n" -lt 5 ]; then
  bad "test_reap_orphan_servers.sh の cleanup / mktemp -d を抽出できない (vars=${reap_n}。抽出が壊れると違反 0 件で緑になる)"
else
  ok "test_reap_orphan_servers.sh から cleanup と mktemp -d $reap_n 件を抽出できた"
  while IFS= read -r v; do
    [ -n "$v" ] || continue
    if grep -qF -- "\$$v" <<< "$reap_cleanup"; then
      ok "test_reap_orphan_servers.sh: \$$v は cleanup で消される"
    else
      bad "test_reap_orphan_servers.sh: \$$v が cleanup の rm -rf に無い (run のたびに /tmp へ 1 個ずつ残る)"
    fi
  done <<< "$reap_vars"
fi

printf '\n検査 %d 件: fail=%d\n' "$checks" "$fails"
[ "$fails" -eq 0 ] || exit 1
echo "OK tmux socket cleanup"
