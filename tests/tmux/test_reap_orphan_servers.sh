#!/usr/bin/env zsh
#
# scripts/tmux_reap_orphan_servers.sh の回帰テスト。
# 不変条件を 4 つ検査する:
#   A) socket ファイルが消えた孤児 tmux サーバ（プロセスだけ生存）を reap が kill する
#   B) 生存 socket を持つサーバ（実サーバ・接続中 client 相当）を reap が絶対に kill しない
#   C) 空白入り TMUX_TMPDIR の生存サーバを誤殺しない
#      (旧実装は lsof NAME 列を awk $NF で取りパスが truncate → [ -S ] 常に偽 → 誤殺)
#   D) socket ファイルが消えていても client が attach 中のサーバは kill しない
#      (unix socket は unlink 後も接続維持。accept 済み fd の有無で使用中を判定する)
#
# 隔離方針: 孤児役・生存役とも専用 TMUX_TMPDIR + 名前付き socket (-L) で起こす。reap は
# ユーザーの全 `^tmux` プロセスを走査するが、判定キーは「socket ファイルの実在」なので、
# このテストが起こした生存役と、(もし在れば)実本番サーバは生存 socket を持つため保護される。
# 孤児役だけが socket 消滅状態になり reap 対象になる。実サーバには一切触れない。

set -euo pipefail
unset CDPATH

TMUX_BIN_PATH=${TMUX_BIN:-tmux}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
REAP="$ROOT_DIR/scripts/tmux_reap_orphan_servers.sh"

UID_NUM=$(id -u)
# unix socket のパス長上限 (macOS は sun_path 104 byte) を超えないよう、TMUX_TMPDIR は
# /var/folders 配下の長い mktemp ではなく /tmp 直下の短い temp dir にし、socket 名も短くする。
ORPHAN_DIR=$(mktemp -d /tmp/reapo.XXXXXX)
LIVE_DIR=$(mktemp -d /tmp/reapl.XXXXXX)
SPACE_BASE=$(mktemp -d /tmp/reaps.XXXXXX)
SPACE_DIR="$SPACE_BASE/s p"   # 空白入り TMUX_TMPDIR (ケース C 用)
ATT_DIR=$(mktemp -d /tmp/reapa.XXXXXX)
ORPHAN_SOCK="ro$$"
LIVE_SOCK="rl$$"
SPACE_SOCK="rs$$"
ATT_SOCK="ra$$"
orphan_pid=""
live_pid=""
space_pid=""
att_pid=""
prot_pid=""
reap_log_probe_dir=""
# reap は実プロセステーブルを pgrep で走査する設計のため、このテストの reap 実行は「自分が作った
# 孤児」だけでなく、実環境に偶々存在する他の dead-socket 孤児も回収しうる（reap は生存 socket を
# 持つプロセスには絶対触れないので副作用は常に良性=ゴミ掃除）。ただし reap のログ書き込みは
# 実 ~/.cache を汚さないよう temp HOME に隔離する。
export HOME="$ORPHAN_DIR/home"
export TT_TRIGGER_LOG="$ORPHAN_DIR/trigger.log"
mkdir -p "$HOME"

fail() { print -u2 "[test-reap:zsh] FAIL: $1"; exit 1; }
ok()   { print "[test-reap:zsh] ok: $1"; }

cleanup() {
  # 生存役は明示的に PID で確実に落とす（socket 経由でなく PID 指定なので取りこぼさない）。
  [[ -n "$live_pid" ]]   && kill -KILL "$live_pid"   2>/dev/null || true
  [[ -n "$orphan_pid" ]] && kill -KILL "$orphan_pid" 2>/dev/null || true
  [[ -n "$space_pid" ]]  && kill -KILL "$space_pid"  2>/dev/null || true
  [[ -n "$att_pid" ]]    && kill -KILL "$att_pid"    2>/dev/null || true
  [[ -n "${prot_pid:-}" ]] && kill -KILL "$prot_pid" 2>/dev/null || true
  env TMUX_TMPDIR="$LIVE_DIR"   "$TMUX_BIN_PATH" -L "$LIVE_SOCK"   kill-server >/dev/null 2>&1 || true
  env TMUX_TMPDIR="$ORPHAN_DIR" "$TMUX_BIN_PATH" -L "$ORPHAN_SOCK" kill-server >/dev/null 2>&1 || true
  env TMUX_TMPDIR="$SPACE_DIR"  "$TMUX_BIN_PATH" -L "$SPACE_SOCK"  kill-server >/dev/null 2>&1 || true
  env TMUX_TMPDIR="$ATT_DIR"    "$TMUX_BIN_PATH" -L "$ATT_SOCK"    kill-server >/dev/null 2>&1 || true
  rm -rf "$ORPHAN_DIR" "$LIVE_DIR" "$SPACE_BASE" "$ATT_DIR"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

command -v "$TMUX_BIN_PATH" >/dev/null 2>&1 || { print -u2 "tmux not found (set \$TMUX_BIN)"; exit 1; }
[[ -x "$REAP" ]] || fail "reap script not found/executable: $REAP"
# ---- 静的検査 (実 tmux / lsof を要さないので gate より前に置く) ----------------------------
# ⚠️ この位置を gate の下へ動かさないこと。lsof が無い CI では gate で exit 0 するため、
# 下に置いた検査は**一度も走らない**。実測 2026-08-21: 保護の実装を revert しても CI は緑で、
# 退行を止める力が 0 だった (規範: _claude/rules/adversarial-review-own-safeguards.md の
# 「その機構が CI で実際に走るか、同じ commit で確認する」)。
# E-2) 既定の保護リストが本番の default socket を含むこと。
# 上の E は seam (TT_REAP_PROTECT_SOCKS) 経由なので「既定値が空になる退行」を検出できない
# (実測: 既定値を壊す変異で E は緑のまま通った)。既定値そのものを検査して閉じる。
grep -q 'TT_REAP_PROTECT_SOCKS' "$REAP" \
  || fail "E-2: 保護リストの seam (TT_REAP_PROTECT_SOCKS) が無い"
# ⚠️ '/tmp/tmux-$uid/default' で grep すると '/private/tmp/...' に部分一致して、/tmp 版が
# 消えても緑になる (実測)。引用符直後の形で厳密に見る。
grep -q '"/tmp/tmux-\$uid/default' "$REAP" \
  || fail "E-2: 既定の保護リストに本番の default socket (/tmp 表記) が含まれていない"
grep -q '/private/tmp/tmux-\$uid/default' "$REAP" \
  || fail "E-2: 既定の保護リストに /private/tmp 版が無い (macOS で lsof の表記と一致しない)"
ok "E-2: 既定の保護リストが本番の default socket (/tmp と /private/tmp 両表記) を含む"

# F-1) 相対パスの socket は [ -S ] で確認できないので alive 側 (= 停止しない) へ倒すこと。
# lsof が返すパスは対象プロセスの cwd 基準なので、reaper 自身の cwd で解決すると生きている
# socket を「無い」と読む。tmux は -S の相対パスを絶対化しないため live サーバが実在しうる
# (実測 2026-08-21: cwd を変えるだけで同じ生存サーバが would reap に入った / 消えた)。
grep -q 'case "\$s" in' "$REAP" \
  || fail "F-1: socket パスの絶対/相対を分けていない (相対パスの live サーバを誤殺する)"
grep -qE '^\s*\*\)\s*alive=1' <<< "$(awk '/case "\$s" in/,/esac/' "$REAP")" \
  || fail "F-1: 相対パスを alive 側へ倒していない (確認できないものを孤児と読む)"
ok "F-1: 相対パス socket は確認不能として保護側へ倒す"

# F-2) seam は「追加」であって「置換」ではないこと。置換形 (:-) だと空白 1 個や末尾スラッシュの
# ような空でない無意味な値で既定の本番保護が黙って全消えする (実測 2026-08-21)。
grep -q 'protect_socks=\${TT_REAP_PROTECT_SOCKS' "$REAP" \
  && fail "F-2: seam が置換形 (:-) のまま = 無意味な値で既定の保護が消える"
grep -q 'protect_socks="\$protect_socks' "$REAP" \
  || fail "F-2: seam の値を既定へ追加する形になっていない"
ok "F-2: 保護リストの seam は既定への追加 (置換ではない)"

# R1/R2) tmux 起動可否に左右されず、reap 本体の観測 writer を実行する。pgrep/lsof の観測結果
# だけをこのケース内で stub し、TERM 対象には現在生きていない PID を使う。writer 自体は
# scripts/tmux_reap_orphan_servers.sh の実装をそのまま通るため、属性だけの静的検査にはしない。
reap_log_probe_dir=$(mktemp -d /tmp/reaplog.XXXXXX)
reap_log_probe_home="$reap_log_probe_dir/home"
reap_log_probe_bin="$reap_log_probe_dir/bin"
reap_log_probe_seam="$reap_log_probe_dir/seam.log"
mkdir -p "$reap_log_probe_home" "$reap_log_probe_bin"
reap_log_probe_target=999999
while kill -0 "$reap_log_probe_target" 2>/dev/null; do
  reap_log_probe_target=$((reap_log_probe_target + 1))
done
cat >"$reap_log_probe_bin/pgrep" <<'EOF'
#!/bin/sh
printf '%s\n' "$REAP_LOG_PROBE_PID"
EOF
cat >"$reap_log_probe_bin/lsof" <<'EOF'
#!/bin/sh
printf 'n%s/tmux-%s/orphan\n' "$REAP_LOG_PROBE_ROOT" "$REAP_LOG_PROBE_UID"
EOF
chmod +x "$reap_log_probe_bin/pgrep" "$reap_log_probe_bin/lsof"
PATH="$reap_log_probe_bin:$PATH" \
  HOME="$reap_log_probe_home" \
  TT_TRIGGER_LOG="$reap_log_probe_seam" \
  REAP_LOG_PROBE_PID="$reap_log_probe_target" \
  REAP_LOG_PROBE_ROOT="$reap_log_probe_dir" \
  REAP_LOG_PROBE_UID="$UID_NUM" \
  "$REAP" >/dev/null 2>&1 || fail "R1: writer probe の reap 実行に失敗した"
probe_tab="$(printf '\t')"
if [[ -f "$reap_log_probe_seam" ]]; then
  probe_lines="$(wc -l <"$reap_log_probe_seam" | tr -d '[:space:]')"
else
  probe_lines=0
fi
if [[ -f "$reap_log_probe_seam" && "$probe_lines" == 1 ]] \
   && grep -Eq "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}${probe_tab}reaped-orphan-servers n=[1-9][0-9]* escalated=[0-9]+$" "$reap_log_probe_seam"; then
  ok "R1: reap writer probe が seam へ識別子付きの1行を書く"
else
  fail "R1: reap writer probe のログが ISO8601<TAB>本文の1行ではない"
fi
probe_default_log="$reap_log_probe_home/.cache/tt-restore-trigger.log"
[[ ! -e "$probe_default_log" ]] \
  || fail "R2: reap writer probe が HOME の既定ログを書いた: $probe_default_log"
ok "R2: reap writer probe は HOME の既定ログを汚さない"
rm -rf "$reap_log_probe_dir"
reap_log_probe_dir=""

command -v lsof >/dev/null 2>&1 || { print -u2 "[test-reap:zsh] skipped: lsof not available (静的検査は上で実施済み)"; exit 0; }

# ---- 生存役・孤児役のサーバを起こす ----
start_log="$ORPHAN_DIR/start.log"
if ! env TMUX_TMPDIR="$LIVE_DIR" "$TMUX_BIN_PATH" -L "$LIVE_SOCK" \
     new-session -d -s live "tail -f /dev/null" >"$start_log" 2>&1; then
  if grep -qiE "operation not permitted|permission denied" "$start_log"; then
    print -u2 "[test-reap:zsh] skipped: tmux cannot create sockets in this environment"
    exit 0
  fi
  cat "$start_log" >&2; fail "failed to start live server"
fi
env TMUX_TMPDIR="$ORPHAN_DIR" "$TMUX_BIN_PATH" -L "$ORPHAN_SOCK" \
  new-session -d -s orphan "tail -f /dev/null" >>"$start_log" 2>&1 || { cat "$start_log" >&2; fail "failed to start orphan server"; }

live_pid=$(env TMUX_TMPDIR="$LIVE_DIR"   "$TMUX_BIN_PATH" -L "$LIVE_SOCK"   display-message -p '#{pid}')
orphan_pid=$(env TMUX_TMPDIR="$ORPHAN_DIR" "$TMUX_BIN_PATH" -L "$ORPHAN_SOCK" display-message -p '#{pid}')
[[ -n "$live_pid" && -n "$orphan_pid" ]] || fail "server PID を取得できなかった (live=$live_pid orphan=$orphan_pid)"
kill -0 "$live_pid" 2>/dev/null   || fail "live server がすぐ死んだ (pid=$live_pid)"
kill -0 "$orphan_pid" 2>/dev/null || fail "orphan server がすぐ死んだ (pid=$orphan_pid)"

# ---- 孤児化: socket ファイルだけ消す（プロセスは残す） ----
orphan_socket_path="$ORPHAN_DIR/tmux-$UID_NUM/$ORPHAN_SOCK"
[[ -S "$orphan_socket_path" ]] || fail "orphan socket が想定パスに無い: $orphan_socket_path"
rm -f "$orphan_socket_path"
[[ -S "$orphan_socket_path" ]] && fail "orphan socket を消せていない"
kill -0 "$orphan_pid" 2>/dev/null || fail "socket を消したら orphan プロセスまで消えた (想定外)"
ok "準備: orphan は socket 消滅・プロセス生存 / live は socket 生存"

# ---- reap 実行 ----
"$REAP" >/dev/null 2>&1 || true

# A) 孤児が kill されること（TERM は非同期なので最大 ~3s 待つ）
i=0
while kill -0 "$orphan_pid" 2>/dev/null && [ "$i" -lt 30 ]; do sleep 0.1; i=$((i+1)); done
kill -0 "$orphan_pid" 2>/dev/null && fail "A: 孤児サーバ (pid=$orphan_pid) が reap 後も生存している"
ok "A: socket 消滅の孤児サーバを reap が回収した"

# R1/R2) reap の実 writer が seam へ、識別子付きの正しい1行を書くこと。HOME 側を同時に
# 見ることで、属性（ログファイルがどこかに存在するだけ）ではなく seam 経由を pin する。
reap_tab="$(printf '\t')"
if [[ -f "$TT_TRIGGER_LOG" ]]; then
  reap_lines="$(wc -l <"$TT_TRIGGER_LOG" | tr -d '[:space:]')"
else
  reap_lines=0
fi
if [[ -f "$TT_TRIGGER_LOG" && "$reap_lines" == 1 ]] \
   && grep -Eq "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}${reap_tab}reaped-orphan-servers n=[1-9][0-9]* escalated=[0-9]+$" "$TT_TRIGGER_LOG"; then
  ok "R1: reap の writer が seam へ reaped-orphan-servers を1行記録した"
else
  fail "R1: reap の seam ログが ISO8601<TAB>本文の1行ではない"
fi
default_reap_log="$HOME/.cache/tt-restore-trigger.log"
[[ ! -e "$default_reap_log" ]] \
  || fail "R2: reap が seam を迂回して HOME の既定ログを書いた: $default_reap_log"
ok "R2: reap は TT_TRIGGER_LOG を使い、HOME の既定ログを汚さない"

# B) 生存 socket のサーバは保護されること
kill -0 "$live_pid" 2>/dev/null || fail "B: 生存 socket のサーバ (pid=$live_pid) を reap が誤って kill した"
ok "B: 生存 socket のサーバは保護された"

# ---- C) 空白入り TMUX_TMPDIR の生存サーバを保護 ----
# 旧実装は lsof の NAME 列を awk '{print $NF}' で取っており、パスに空白があると最終
# フィールドだけに truncate → [ -S ] が常に偽 → 生存サーバを孤児と誤判定して kill した。
mkdir -p "$SPACE_DIR"
env TMUX_TMPDIR="$SPACE_DIR" "$TMUX_BIN_PATH" -L "$SPACE_SOCK" \
  new-session -d -s sp "tail -f /dev/null" >>"$start_log" 2>&1 || { cat "$start_log" >&2; fail "failed to start space server" }
space_pid=$(env TMUX_TMPDIR="$SPACE_DIR" "$TMUX_BIN_PATH" -L "$SPACE_SOCK" display-message -p '#{pid}')
[[ -n "$space_pid" ]] || fail "C: space server PID を取得できなかった"
[[ -S "$SPACE_DIR/tmux-$UID_NUM/$SPACE_SOCK" ]] || fail "C: 空白入りパスに socket が無い"
"$REAP" >/dev/null 2>&1 || true
kill -0 "$space_pid" 2>/dev/null || fail "C: 空白入りパスの生存サーバ (pid=$space_pid) を reap が誤殺した"
ok "C: 空白入り TMUX_TMPDIR の生存サーバは保護された"

# ---- D) socket 消滅 + client attach 中のサーバを保護 ----
# unix socket は unlink 後も接続が維持されるため「ファイル消滅 = 未使用」ではない。
# control-mode client (-C) は PTY 不要で headless に attach でき、stdin の sleep パイプが
# 切れる ~8s の間だけ接続が生きる (テストはその窓で reap を当てる)。
env TMUX_TMPDIR="$ATT_DIR" "$TMUX_BIN_PATH" -L "$ATT_SOCK" \
  new-session -d -s att "tail -f /dev/null" >>"$start_log" 2>&1 || { cat "$start_log" >&2; fail "failed to start att server" }
att_pid=$(env TMUX_TMPDIR="$ATT_DIR" "$TMUX_BIN_PATH" -L "$ATT_SOCK" display-message -p '#{pid}')
[[ -n "$att_pid" ]] || fail "D: att server PID を取得できなかった"
( sleep 8 | env TMUX_TMPDIR="$ATT_DIR" "$TMUX_BIN_PATH" -L "$ATT_SOCK" -C attach -t att >/dev/null 2>&1 & )
# client 接続の確立を待つ (socket 消滅前に確認しないと CLI が繋げない)
i=0
while [ "$(env TMUX_TMPDIR="$ATT_DIR" "$TMUX_BIN_PATH" -L "$ATT_SOCK" list-clients 2>/dev/null | wc -l | tr -d ' ')" -lt 1 ]; do
  [ "$i" -ge 30 ] && fail "D: control-mode client が attach しない"
  sleep 0.1; i=$((i+1))
done
rm -f "$ATT_DIR/tmux-$UID_NUM/$ATT_SOCK"
[[ -S "$ATT_DIR/tmux-$UID_NUM/$ATT_SOCK" ]] && fail "D: att socket を消せていない"
"$REAP" >/dev/null 2>&1 || true
kill -0 "$att_pid" 2>/dev/null || fail "D: attach 中 (socket 消滅) のサーバ (pid=$att_pid) を reap が誤殺した"
ok "D: socket 消滅でも attach 中のサーバは保護された"

# ---- E) 保護対象 socket (本番の default socket 相当) は socket 消滅でも保護 ----
# 実 default socket (/tmp/tmux-$uid/default) でサーバを起こすと本番と衝突するため、
# 保護リストの seam に「孤児役の socket パス」を渡して判定だけを検証する。
# なぜこの保護が必要か: 何かが default socket ファイルを削除し、その瞬間 client が全 detach
# していると、reap は「socket 全消滅 + 接続なし」= 孤児と結論して生きている本番サーバを
# TERM→KILL する (tmux は exit 時保存をしないため直前の保存以降が失われる)。
PROT_DIR=$(mktemp -d /tmp/reapp.XXXXXX)
PROT_SOCK="rp$$"
env TMUX_TMPDIR="$PROT_DIR" "$TMUX_BIN_PATH" -L "$PROT_SOCK" \
  new-session -d -s prot "tail -f /dev/null" >>"$start_log" 2>&1 \
  || { cat "$start_log" >&2; fail "failed to start protected-role server" }
prot_pid=$(env TMUX_TMPDIR="$PROT_DIR" "$TMUX_BIN_PATH" -L "$PROT_SOCK" display-message -p '#{pid}')
[[ -n "$prot_pid" ]] || fail "E: protected-role server の PID を取得できなかった"
prot_socket_path="$PROT_DIR/tmux-$UID_NUM/$PROT_SOCK"
rm -f "$prot_socket_path"
[[ -S "$prot_socket_path" ]] && fail "E: protected-role socket を消せていない"

# 保護リストに入れて実行 → kill されない
TT_REAP_PROTECT_SOCKS="$prot_socket_path" "$REAP" >/dev/null 2>&1 || true
kill -0 "$prot_pid" 2>/dev/null \
  || fail "E: 保護対象 socket のサーバ (pid=$prot_pid) を reap が kill した"
ok "E: 保護対象 socket は socket 消滅 + 接続なしでも kill されない"

# 対照: 保護リストから外すと同じプロセスが reap される (保護が効いていることの陽性対照。
# これが無いと「そもそも reap 対象になっていなかった」だけで緑になる)
TT_REAP_PROTECT_SOCKS="/nowhere/does-not-exist" "$REAP" >/dev/null 2>&1 || true
i=0
while kill -0 "$prot_pid" 2>/dev/null && [ "$i" -lt 30 ]; do sleep 0.1; i=$((i+1)); done
kill -0 "$prot_pid" 2>/dev/null \
  && fail "E(対照): 保護を外しても kill されない (= E は reap 対象外を見ていただけ)"
ok "E(対照): 保護を外すと同じプロセスが reap される (保護が実際に効いている)"

print "[test-reap:zsh] done"
