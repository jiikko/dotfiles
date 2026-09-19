#!/usr/bin/env bash
# `check_unused_excluding_tests.sh` の staticcheck ガードが、**shim を掴んで素通りしない**ことを
# 実験で確かめる (issue 405)。
#
# なぜテストにするか: goenv / rbenv / nodenv の shim は「どの版にも実体が無い」ときでも PATH 上に
# 在り、`command -v` は rc=0 を返す。旧版のガードは `command -v` だったので**原理的に発火せず**、
# 用意した案内は一度も出ないまま shim のエラー文が下の parser へ流れ込んでいた
# (実測 2026-09-20: `make test` が「解釈できない staticcheck の出力」で毎回赤)。
# この壊れ方は「staticcheck が入っている環境」では観測できない = 緑の日には見えない。
#
# 手段: 本物の staticcheck は使わず、PATH を差し替えて 2 つの状態を作る
#   A) staticcheck が PATH に無い
#   B) **shim だけ在る** (rc=127 で goenv のエラー文を出すだけの偽 shim)
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR" || exit 1
# 🚨 repo 内の tmp/ に作らない (ignore が ~/.gitignore_global 由来で、新品チェックアウトと CI には無い)
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

fail=0
note() { printf '✓ %s\n' "$1"; }
bad() { printf '✗ %s\n' "$1"; fail=1; }

# 🚨 **staticcheck だけを PATH から落とす** (素朴に PATH を絞ると bash / env まで消えて、
# 「案内が出ない」の理由が別物 (env: bash: No such file or directory) にすり替わる。実測)。
# 実体の在るディレクトリだけを外し、他はそのまま残す。
BIN_DIR=""
IFS=':' read -r -a _path_dirs <<< "$PATH"
for d in "${_path_dirs[@]}"; do
  [ -n "$d" ] || continue
  [ -e "$d/staticcheck" ] && continue # staticcheck を持つ dir は外す
  BIN_DIR="${BIN_DIR:+$BIN_DIR:}$d"
done
if [ -z "$BIN_DIR" ]; then
  bad "前提が作れていない: staticcheck を除いた PATH が空"
  exit 1
fi
if PATH="$BIN_DIR" command -v staticcheck >/dev/null 2>&1; then
  bad "前提が作れていない: 絞った PATH にまだ staticcheck が居る"
  exit 1
fi

run_guard() { # $1=追加で PATH 先頭へ置く dir (空なら無し)
  local extra="$1" out rc
  out="$TMP_DIR/out.$$"
  if [ -n "$extra" ]; then
    PATH="$extra:$BIN_DIR" ./scripts/check_unused_excluding_tests.sh >"$out" 2>&1 && rc=0 || rc=$?
  else
    PATH="$BIN_DIR" ./scripts/check_unused_excluding_tests.sh >"$out" 2>&1 && rc=0 || rc=$?
  fi
  GUARD_RC="$rc"
  GUARD_OUT="$(cat "$out")"
}

# --- A: staticcheck が PATH に無い -------------------------------------------------
run_guard ""
if [ "$GUARD_RC" -eq 0 ]; then
  bad "A: staticcheck が無いのに rc=0 (検査できないのに緑)"
else
  note "A: staticcheck が無いと非 0 で止まる (rc=$GUARD_RC)"
fi
case "$GUARD_OUT" in
  *"staticcheck を実行できない"*) note "A: 案内が出る" ;;
  *) bad "A: 用意された案内が出ていない: $GUARD_OUT" ;;
esac

# --- B: shim だけ在る (goenv の壊れ方そのもの) --------------------------------------
SHIM_DIR="$TMP_DIR/shims"
mkdir -p "$SHIM_DIR"
cat > "$SHIM_DIR/staticcheck" <<'SHIM'
#!/usr/bin/env bash
# goenv の shim が「どの版にも実体が無い」ときに出す形 (実測 2026-09-20 の再現)
echo "goenv: 'staticcheck' command not found" >&2
echo "" >&2
echo "The 'staticcheck' command exists in these Go versions:" >&2
echo "  1.25.4" >&2
exit 127
SHIM
chmod +x "$SHIM_DIR/staticcheck"

# 前提: この偽 shim は `command -v` には当たる (= 旧ガードなら素通りする状態)
if ! PATH="$SHIM_DIR:$BIN_DIR" command -v staticcheck >/dev/null 2>&1; then
  bad "前提が作れていない: 偽 shim が command -v に当たらない"
fi

run_guard "$SHIM_DIR"
if [ "$GUARD_RC" -eq 0 ]; then
  bad "B: shim だけなのに rc=0 (素通りしている)"
else
  note "B: shim だけなら非 0 で止まる (rc=$GUARD_RC)"
fi
case "$GUARD_OUT" in
  *"staticcheck を実行できない"*) note "B: shim を掴まず案内が出る" ;;
  *) bad "B: 用意された案内が出ていない (shim を掴んで素通りした): $GUARD_OUT" ;;
esac
# 🚨 旧版の症状そのもの。shim のエラー文が parser へ流れ込むと、この語が出る
case "$GUARD_OUT" in
  *"解釈できない staticcheck の出力"*) bad "B: shim のエラー文が parser へ流れ込んでいる" ;;
  *) note "B: shim のエラー文が parser へ流れ込まない" ;;
esac

[ "$fail" -eq 0 ] || exit 1
echo "[staticcheck-guard] OK"
