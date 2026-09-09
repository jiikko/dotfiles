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


# --- ⑥ mktemp -d した dir が cleanup の rm -rf に載っていること -----------------------------
#
# 🚨 **tmux を起こす ① より前に置く**。①〜④ は tmux が無いと `exit 77` (skip) で終わるが、
# ⑥ は純粋なテキスト照合で tmux に依存しないので、後ろに置くと skip に巻き込まれる
# (敵対レビュー P2-1。実測: 偽 tmux を PATH 先頭に置くと ⑥ が 1 件も走らないまま rc=77)。
#
# 🚨 test_reap_orphan_servers.sh は `-L` ではなく **TMUX_TMPDIR ごと隔離する**形なので、
# 漏れるのは socket 1 個ではなく **dir**。ケース E の `PROT_DIR` が cleanup の `rm -rf` から
# 漏れて、**正常終了のたびに 1 個ずつ** `/tmp/reapp.*` を置いていた (実測 39 個)。
#
# 🚨 **脅威モデル**: 止めるのは「dir を足して cleanup に足し忘れる」形だけ。
# 🚨 **検出しないと決めた形** (敵対レビューの迂回指摘のうち、意図的な書き換えに当たるもの):
#   - `rm -fr` / `rm -r -f` のような綴り違い (うっかりではなく意図的な書き換え)
#   - 変数を経由した消し方 (`for d in $dirs; do rm -rf "$d"; done`)
#   - heredoc で書き出す子スクリプトの中の `mktemp -d` (外側の cleanup の責務ではないのに
#     ✗ になる = 偽陽性。現状この file に heredoc の mktemp は無い)
#   これらは review の責務。**判別力の本体は静的検査ではなく、実行前後の
#   `/tmp/reap*` の個数差** (A-B) で、ここは「素の書き忘れ」を早く止める事前フィルタ。

REAP="$ROOT_DIR/tests/tmux/test_reap_orphan_servers.sh"

# cleanup() の中の `rm -rf` の行 (継続行を含む)。
# 🚨 **cleanup 全体を見ない**。全体だと `kill-server` の `TMUX_TMPDIR="$LIVE_DIR"` に当たり、
#    `rm -rf` から外しても緑になる (false green)。
# 🚨 **コメント行を落とす**。落とさないと「`rm -rf "$FOO"` のようにここへ足すこと」という
#    注意書きが本物の `rm -rf` として通り、外した変数が緑になる (敵対レビュー P1-2)。
extract_rm_targets() { # extract_rm_targets <file>
  awk '
    /^cleanup\(\) \{$/ { inside = 1; next }
    inside && /^\}$/   { inside = 0 }
    !inside            { next }
    { line = $0; sub(/^[[:space:]]*/, "", line) }
    line ~ /^#/        { next }
    cont               { print; cont = (/\\$/); next }
    /rm -rf/           { print; cont = (/\\$/) }
  ' "$1"
}
# mktemp -d で作る dir の変数名。
# 🚨 素の `VAR=$(mktemp -d` だけを見る形は、**この repo の主流イディオム**
#    `VAR="$(mktemp -d` を丸ごと見逃す (敵対レビュー P1-1。実測 tests/ + scripts/:
#    クォート形 58 / 素の形 59 / インデント付き 12)。
extract_mktemp_vars() { # extract_mktemp_vars <file>
  grep -oE '^[[:space:]]*[A-Za-z_][A-Za-z0-9_]*="?\$\(mktemp[[:space:]]+-d' "$1" |
    sed -E 's/^[[:space:]]*//; s/="?\$\(mktemp[[:space:]]+-d$//'
}
# 🚨 部分一致で照合しない。`$PROT` は `$PROT_DIR` に当たってしまう (敵対レビュー P3)。
rm_covers() { # rm_covers <rm 本文> <変数名>
  grep -qE "\\\$\{?$2([^A-Za-z0-9_]|\$)" <<< "$1"
}

# canary — 本走査と**同じ関数**に既知の入力を通し、既知の答えが出ることを先に固定する。
# 抽出が壊れて 0 件になると「違反なし」で緑になるので、そこを塞ぐ。
canary_src=$(mktemp "${TMPDIR:-/tmp}/socket_cleanup_canary.XXXXXX")
cat > "$canary_src" <<'CANARY'
A_DIR=$(mktemp -d /tmp/a.XXXXXX)
  B_DIR="$(mktemp -d /tmp/b.XXXXXX)"
C_DIR=$(mktemp  -d /tmp/c.XXXXXX)
NOT_A_DIR=$(mktemp /tmp/d.XXXXXX)
cleanup() {
  # rm -rf "$C_DIR" のように足すこと (コメントなので拾わない)
  rm -rf "$A_DIR" \
    ${B_DIR:+"$B_DIR"}
}
CANARY
canary_vars=$(extract_mktemp_vars "$canary_src" | tr '\n' ' ')
canary_rm=$(extract_rm_targets "$canary_src")
canary_ok=1
[ "$canary_vars" = "A_DIR B_DIR C_DIR " ] || canary_ok=0
rm_covers "$canary_rm" A_DIR || canary_ok=0
rm_covers "$canary_rm" B_DIR || canary_ok=0
! rm_covers "$canary_rm" C_DIR || canary_ok=0   # コメント中の $C_DIR を拾ってはいけない
if [ "$canary_ok" -eq 1 ]; then
  ok "canary: 抽出 (クォート形・インデント・継続行・コメント除外) が既知の答えを返す"
else
  bad "canary: 抽出が壊れている (vars=[$canary_vars] / rm=[$canary_rm])。この状態の ⑥ は何も守らない"
fi
rm -f "$canary_src"

reap_rm=$(extract_rm_targets "$REAP")
reap_vars=$(extract_mktemp_vars "$REAP")
reap_n=$(grep -c '[A-Za-z]' <<< "$reap_vars" || true)
if [ -z "$reap_rm" ] || [ "$reap_n" -lt 1 ]; then
  bad "test_reap_orphan_servers.sh の cleanup / mktemp -d を抽出できない (vars=${reap_n})"
else
  ok "test_reap_orphan_servers.sh から cleanup の rm -rf と mktemp -d ${reap_n} 件を抽出できた"
  while IFS= read -r v; do
    [ -n "$v" ] || continue
    if rm_covers "$reap_rm" "$v"; then
      ok "test_reap_orphan_servers.sh: \$$v は cleanup の rm -rf に載っている"
    else
      bad "test_reap_orphan_servers.sh: \$$v が cleanup の rm -rf に無い (run のたびに /tmp へ 1 個ずつ残る)"
    fi
  done <<< "$reap_vars"
fi
# --- ① 前提: kill-server だけでは socket が残る (この検査が守っている事実そのもの) ------------
#
# 🚨 これを canary として先に確かめる。もし tmux 側が将来 socket を消すようになったら、
# この検査は「何も守っていない」状態になるので、そのときに気づけるようにしておく。
raw="tt-cleanup-raw-$$"
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

# 🚨 件数そのものを assert する。`checks` を出すだけでは「1 件も走らないまま緑」を塞げない
[ "$checks" -ge 13 ] || bad "検査が $checks 件しか走っていない (13 件以上のはず)"
printf '\n検査 %d 件: fail=%d\n' "$checks" "$fails"
[ "$fails" -eq 0 ] || exit 1
echo "OK tmux socket cleanup"
