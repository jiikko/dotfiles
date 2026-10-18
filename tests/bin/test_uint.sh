#!/usr/bin/env bash
# bin/lib/uint.sh の uint_ok を、bin の道具が実際に動くシェル (/bin/bash = 3.2・bash 5・zsh・sh) で固定する。
#
# なぜ: [!0-9] は /bin/bash (3.2) の ja_JP.UTF-8 で全角数字に一致せず「１２」を整数として通した (mutate-verify の --timeout が
# そうなっていた)。寄せた先の 1 か所が、どのシェルから source しても同じ答えを返すことを見る。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LIB="$ROOT_DIR/bin/lib/uint.sh"
fails=0
ok()   { printf '✓ %s\n' "$1"; }
fail() { printf '✗ %s\n' "$1" >&2; fails=$((fails + 1)); }

# 値 最大の桁数 期待する rc
cases=(
  "12 9 0" "0 9 0" "123456789 9 0" "1234567890 9 2" "12345678 7 2"
  "１２ 9 1" "1２ 9 1" "'' 9 1" "-1 9 1" "1.5 9 1" "' 1' 9 1" "0x1F 9 1"
)
shells=(/bin/bash /bin/zsh /bin/sh)
command -v bash >/dev/null && [ "$(command -v bash)" != /bin/bash ] && shells+=("$(command -v bash)")

seen=0
for sh in "${shells[@]}"; do
  [ -x "$sh" ] || { fail "$sh が無い"; continue; }
  for c in "${cases[@]}"; do
    eval "set -- $c"
    rc=0
    LC_ALL=ja_JP.UTF-8 "$sh" -c '. "$1"; uint_ok "$2" "$3"' _ "$LIB" "$1" "$2" || rc=$?
    seen=$((seen + 1))
    [ "$rc" -eq "$3" ] || fail "$sh: uint_ok '$1' $2 の rc=$rc (期待 $3)"
  done
done
[ "$seen" -ge $(( ${#cases[@]} * 3 )) ] || fail "試した組が少ない ($seen)"
ok "uint_ok: ${#shells[@]} つのシェル × ${#cases[@]} 件 (全角・空・符号・小数・空白・16 進・桁あふれ)"

# 寄せた先の道具が全角を拒む (前は mutate-verify の --timeout が /bin/bash で「１２」を通した)
rc=0
LC_ALL=ja_JP.UTF-8 /bin/bash "$ROOT_DIR/bin/mutate-verify" --file x --apply true --verify true --expect x --baseline-expect x \
  --timeout '１２' >/dev/null 2>"${TMPDIR:-/tmp}/uint-mv.$$" || rc=$?
if [ "$rc" -eq 2 ] && grep -qF -- '--timeout は 0 以上の整数' "${TMPDIR:-/tmp}/uint-mv.$$"; then
  ok "mutate-verify: 全角の --timeout を拒む"
else
  fail "mutate-verify: 全角の --timeout を拒まない (rc=$rc: $(cat "${TMPDIR:-/tmp}/uint-mv.$$"))"
fi
rm -f "${TMPDIR:-/tmp}/uint-mv.$$"

[ "$fails" -eq 0 ] || { printf '✗ %d 件失敗\n' "$fails" >&2; exit 1; }
echo "OK: uint_ok"
