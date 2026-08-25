#!/usr/bin/env bash
# scripts/lib/tmux_resurrect_guards.sh の lock 取得・掃除の契約テスト (issue 078)
#
# なぜ必要か: `tt_lock_acquire` / `tt_lock_sweep_stale` は 3 本のスクリプトに逐語コピーされて
# いた手順を集約したもので、**関数そのものを直接呼ぶテストが 1 本も無かった**。呼び出し元 3 本の
# 統合テスト経由で偶発的に踏まれているだけで、特に **rc=2 (取得に失敗した) はどの経路からも
# 到達しない** (敵対レビューの指摘、2026-08-25)。rc=2 を rc=0 に潰す変異が全 green で通る状態
# だったので、ここで 3 つの戻り値と掃除の条件を直接固定する。
#
# 契約: 0 = 取得した / 1 = 先任が同一プロセスとして生きている / 2 = 取得に失敗した
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$ROOT_DIR/scripts/lib/tmux_resurrect_guards.sh"

fails=0
ok() { printf '✓ %s\n' "$1"; }
ng() { printf '✗ %s\n' "$1" >&2; fails=$(( fails + 1 )); }

BASE="$(mktemp -d)"
FAKE_PIDS=()
cleanup() {
  local p
  for p in ${FAKE_PIDS+"${FAKE_PIDS[@]}"}; do kill "$p" 2>/dev/null || true; done
  rm -rf "$BASE"
}
trap cleanup EXIT

# 補助プロセスは `( trap - EXIT; exec ... ) &` で起こす (素の `cmd &` は fork 直後に kill されると
# 子が EXIT trap を継承して cleanup の rm -rf をテスト途中で走らせる)。
spawn_live() { ( trap - EXIT; exec sleep 300 ) & FAKE_PIDS+=("$!"); REPLY_PID="$!"; }
free_pid() { local p="${1:-999999}"; while kill -0 "$p" 2>/dev/null; do p=$(( p - 1 )); done; REPLY_PID="$p"; }

rc_of() { local rc=0; tt_lock_acquire "$1" || rc=$?; printf '%s' "$rc"; }

printf '\n=== tt_lock_acquire の戻り値 ===\n\n'

# --- rc=0: 空きなら取得し、owner を記録する ---------------------------------------
d="$BASE/a.lock"
rc="$(rc_of "$d")"
[ "$rc" = 0 ] && ok "空きなら取得できる (rc=0)" || ng "空きなのに rc=$rc"
[ -s "$d/pid" ] && ok "取得時に owner を記録する" || ng "owner が記録されていない"
# ⚠️ pid だけでなく起動時刻まで記録すること (pid 再利用で先任と誤認すると装置が張られない)
if [ "$(awk '{print NF}' "$d/pid" 2>/dev/null)" = 2 ]; then
  ok "owner は pid + 起動時刻の 2 列"
else
  # lstart が取れない環境では 1 列に縮退する。skip を沈黙させない
  printf '⚠ owner が 1 列 (ps -o lstart= が使えない環境と判断して skip)\n'
fi

# --- rc=1: 先任が生きているなら奪わない -------------------------------------------
d="$BASE/b.lock"; mkdir "$d"
spawn_live; tt_lock_write_owner "$d" "$REPLY_PID"
rc="$(rc_of "$d")"
[ "$rc" = 1 ] && ok "先任が生きているなら奪わない (rc=1)" || ng "生存 owner の lock を rc=$rc で扱った"
[ -s "$d/pid" ] && ok "rc=1 のとき owner を上書きしない" || ng "rc=1 なのに owner を書き換えた"

# --- rc=0 (奪う): owner 不在の取り残しは回収する ----------------------------------
# ⚠️ ここが緩むと、異常終了で残った lock が永久に残り **その経路が二度と走らない**
d="$BASE/c.lock"; mkdir "$d"
rc="$(rc_of "$d")"
[ "$rc" = 0 ] && ok "owner 不在の取り残しは奪って続行する (rc=0)" || ng "取り残しを rc=$rc で扱った"

# --- rc=0 (奪う): owner が死んだ pid でも回収する ---------------------------------
d="$BASE/d.lock"; mkdir "$d"; free_pid; printf '%s\n' "$REPLY_PID" > "$d/pid"
rc="$(rc_of "$d")"
[ "$rc" = 0 ] && ok "死んだ owner の lock は奪う (rc=0)" || ng "死んだ owner の lock を rc=$rc で扱った"

# --- rc=2: mkdir が通らないなら「取れなかった」を返す ------------------------------
# ⚠️ rc=2 を rc=0 に潰すと、呼び出し側は「取れた」と誤認して lock 無しで本処理へ進む
if [ "$(id -u)" = 0 ]; then
  printf '⚠ root では書き込み不可ディレクトリを作れないため rc=2 のテストを skip した\n'
else
  ro="$BASE/ro"; mkdir -p "$ro"; chmod 500 "$ro"
  rc="$(rc_of "$ro/x.lock")"
  chmod 700 "$ro"
  [ "$rc" = 2 ] && ok "取得に失敗したら rc=2" || ng "mkdir 不能なのに rc=$rc"
fi

printf '\n=== tt_lock_sweep_stale の掃除条件 ===\n\n'

S="$BASE/sweep"; mkdir -p "$S"

# 死んだサーバ + 死んだ owner → 掃除する
free_pid; dead_srv="$REPLY_PID"
mkdir "$S/$dead_srv.lock"; free_pid $(( dead_srv - 1 )); printf '%s\n' "$REPLY_PID" > "$S/$dead_srv.lock/pid"
# 生きているサーバ → 残す
spawn_live; live_srv="$REPLY_PID"; mkdir "$S/$live_srv.lock"
printf '%s\n' "$live_srv" > "$S/$live_srv.lock/pid"
# 死んだサーバ + 生きている owner → 残す (保存の実行中にサーバだけ落ちた形)
free_pid $(( dead_srv - 2 )); dead_srv2="$REPLY_PID"
mkdir "$S/$dead_srv2.lock"; spawn_live; tt_lock_write_owner "$S/$dead_srv2.lock" "$REPLY_PID"
# 生きているサーバ + 死んだ owner → 残す (掃除の条件は「両方死んでいる」の連言。
# ⚠️ ここを pin しないと、条件から `kill -0 "$spid"` を落とす変異が green のまま通る)
spawn_live; live_srv2="$REPLY_PID"; mkdir "$S/$live_srv2.lock"
free_pid $(( dead_srv - 3 )); printf '%s\n' "$REPLY_PID" > "$S/$live_srv2.lock/pid"
# ディレクトリでない `*.lock` は触らない (`[ -d "$d" ] || continue` を pin する)
: > "$S/notadir.lock"

tt_lock_sweep_stale "$S"

[ -d "$S/$dead_srv.lock" ]  && ng "死んだサーバの stale lock が残っている" || ok "死んだサーバ + 死んだ owner は掃除する"
[ -d "$S/$live_srv.lock" ]  && ok "生きているサーバの lock は残す" || ng "生きているサーバの lock を消した"
[ -d "$S/$dead_srv2.lock" ] && ok "owner が生きている lock は残す (実行中を奪わない)" || ng "実行中の owner の lock を消した"
[ -d "$S/$live_srv2.lock" ] && ok "サーバが生きていれば owner が死んでいても残す" || ng "サーバ生存中の lock を消した"
[ -f "$S/notadir.lock" ] && ok "ディレクトリでない *.lock は触らない" || ng "ディレクトリでない *.lock を消した"

# --- 空ディレクトリで落ちない (nullglob 無しのリテラル *.lock を掴む) --------------
E="$BASE/empty"; mkdir -p "$E"
if tt_lock_sweep_stale "$E"; then ok "対象 0 件でも落ちない"; else ng "対象 0 件で失敗した"; fi
[ -d "$E" ] && ok "対象 0 件で親ディレクトリを消さない" || ng "対象 0 件で親を消した"

printf '\n'
if (( fails > 0 )); then
  printf '[test-lock-acquire] %d 件失敗\n' "$fails" >&2
  exit 1
fi
printf '[test-lock-acquire] すべて成功\n'
