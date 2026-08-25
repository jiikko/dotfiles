#!/usr/bin/env bash
# scripts/lib/tmux_resurrect_guards.sh の owner 指紋 (起動時刻) の契約テスト (issue 078)
#
# なぜ必要か: 指紋の実装は以前 guards.sh と tmux_resurrect_save.sh に**二重にあり、書式まで
# 違っていた** (生の `ps -o lstart=` vs 空白を `_` に潰したトークン)。issue 068 の owner 書式
# drift はこの二重化が直接原因で、078 で guards.sh 側へ一本化した。
#
# ⚠️ 一本化しただけでは守られない。着手時の変異検証で、以下がすべて **green のまま通った**:
#   - tt_same_proc の起動時刻比較を `return 0` に潰す (= PID 再利用を検出しなくなる)
#   - 記録側の正規化 (移行ガード) を外す (= 旧書式の lock で生存 owner を奪う)
#   guards.sh 側の指紋比較を触るテストが 1 本も無かったため。ここで固定する。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$ROOT_DIR/scripts/lib/tmux_resurrect_guards.sh"

fails=0
ok()  { printf '✓ %s\n' "$1"; }
ng()  { printf '✗ %s\n' "$1" >&2; fails=$(( fails + 1 )); }
check() { if "$@"; then shift $(( $# - 1 )); ok "$1"; else shift $(( $# - 1 )); ng "$1"; fi; }

printf '\n=== owner 指紋 (tt_proc_starttime / tt_same_proc) ===\n\n'

# --- 1. 指紋は空白を含まない単一トークンであること -----------------------------
# 空白が残ると、owner 行を `read -r pid start` で読む書き手 (tmux_resurrect_save.sh) が
# 先頭語だけを拾い、比較が壊れる
fp="$(tt_proc_starttime "$$")"
if [ -z "$fp" ]; then
  printf '⚠ ps -o lstart= が使えない環境のため指紋テストを skip した (実行された証拠として明示)\n'
else
  case "$fp" in
    *[[:space:]]*) ng "指紋に空白が残っている: [$fp]" ;;
    *) ok "指紋は空白を含まない単一トークン" ;;
  esac
fi

# --- 2. 同一プロセスは同一と判定する -------------------------------------------
if tt_same_proc "$$" "$fp"; then ok "同じ pid + 同じ指紋 → 同一"; else ng "同じ pid + 同じ指紋を別物と判定した"; fi

# --- 3. 指紋が違えば別プロセス (PID 再利用の検出) ------------------------------
# ⚠️ ここが緩むと、再起動を跨いで残った lock の pid が無関係な長命プロセスに再利用された
#   ときに「先任が生きている」と誤認し、**全保存経路が exit 1 を繰り返して保存が止まる**
if tt_same_proc "$$" "SOME_OTHER_BOOT_2020"; then
  ng "指紋が違うのに同一と判定した (PID 再利用を検出できない)"
else
  ok "指紋が違えば別プロセス (PID 再利用を検出)"
fi

# --- 4. 移行ガード: 旧書式 (正規化前の生 ps 出力) の記録でも同一と判定する ------
# ⚠️ ここが緩むと、指紋の書式を変えた移行期に**生存 owner の lock を奪う** (誤奪)。
#   旧書式は空白を含むので、正規化せずに比較すると必ず不一致になる
raw="$(ps -o lstart= -p "$$" 2>/dev/null || true)"
if [ -z "$raw" ]; then
  printf '⚠ 生 ps 出力が取れないため移行ガードのテストを skip した\n'
else
  if tt_same_proc "$$" "$raw"; then
    ok "旧書式 (空白入りの生 ps 出力) の記録でも同一と判定する (移行時に奪わない)"
  else
    ng "旧書式の記録を別プロセスと判定した = 移行時に生存 owner を奪う"
  fi
fi

# --- 5. 死んだ pid は同一でない ------------------------------------------------
sleep 100 & dead=$!; kill "$dead" 2>/dev/null || true; wait "$dead" 2>/dev/null || true
if tt_same_proc "$dead" "$fp"; then ng "死んだ pid を生存扱いした"; else ok "死んだ pid は同一でない (解除可)"; fi

# --- 6. pid が数値でない / 空 は同一でない ------------------------------------
if tt_same_proc "notanumber" "$fp"; then ng "非数値 pid を受理した"; else ok "非数値 pid は同一でない"; fi
if tt_same_proc "" "$fp"; then ng "空 pid を受理した"; else ok "空 pid は同一でない"; fi

# --- 7. 記録が空 (旧形式 = pid のみ) なら pid 生存だけで判定する ---------------
if tt_same_proc "$$" ""; then ok "記録が空なら pid 生存のみで判定 (旧形式の lock を壊さない)"; else ng "記録が空のとき生存 pid を別物と判定した"; fi

# --- 8. lock ファイル経由の往復 (tt_lock_write_owner → tt_lock_owner_alive) ----
tmp_lock="$(mktemp -d)"
trap 'rm -rf "$tmp_lock"' EXIT
tt_lock_write_owner "$tmp_lock" "$$"
if tt_lock_owner_alive "$tmp_lock"; then ok "書いた owner を alive と読み戻せる"; else ng "書いた owner を alive と読めない"; fi
printf '%s\t%s\n' "$$" "SOME_OTHER_BOOT_2020" > "$tmp_lock/pid"
if tt_lock_owner_alive "$tmp_lock"; then ng "指紋違いの owner を alive と読んだ"; else ok "指紋違いの owner は alive でない"; fi

printf '\n'
if (( fails > 0 )); then
  printf '[test-owner-fingerprint] %d 件失敗\n' "$fails" >&2
  exit 1
fi
printf '[test-owner-fingerprint] すべて成功\n'
