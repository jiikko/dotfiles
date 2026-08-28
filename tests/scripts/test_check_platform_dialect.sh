#!/usr/bin/env bash
# scripts/check_platform_dialect.sh が「実際に発火する」ことを固定する。
#
# なぜ必要か: このゲートが守るのは **手元では絶対に再現しない退行** (BSD 専用の書き方が
# Linux でだけ壊れる)。ゲート自身が空振りしていても手元は緑のままなので、「対象 0 件で
# 該当なし」を見ても効いている証拠にならない (verify-execution-not-just-exit-code)。
# 違反 3 形を実際に作って exit 1 になること、許容形が exit 0 になることを両方見る。
#
# ⚠️ このファイル自身も検査対象 (tests/ 配下)。違反文字列を素で書くとゲートに食われるので、
#   トークンを変数から組み立てて素の形が source に現れないようにしている。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECK="$ROOT_DIR/scripts/check_platform_dialect.sh"

fails=0
ok() { printf '✓ %s\n' "$1"; }
ng() { printf '✗ %s\n' "$1" >&2; fails=$(( fails + 1 )); }

BASE="$(mktemp -d)"
trap 'rm -rf "$BASE"' EXIT

[ -x "$CHECK" ] || { printf '✗ 検査スクリプトが無い/実行できない: %s\n' "$CHECK" >&2; exit 1; }

# 違反形をトークン合成で書く (このファイル自身が食われないため)
SF="stat -f"; SC="stat -c"; DR="date -r"; DD="date -d"

mk() { printf '#!/usr/bin/env bash\n%s\n' "$2" > "$BASE/$1"; }

mk bad_order.sh        "m=\"\$($SF %m \"\$1\" 2>/dev/null || $SC %Y \"\$1\" 2>/dev/null)\""
mk bad_nofallback.sh   "m=\"\$($SF %m \"\$1\")\""
mk bad_date.sh         "s=\"\$($DR \"\$e\" +%Y%m%d)\""
mk good_order.sh       "m=\"\$($SC '%Y' \"\$1\" 2>/dev/null || $SF '%m' \"\$1\" 2>/dev/null)\""
mk good_attached.sh    "z=\"\$(stat -f%z -- \"\$1\" 2>/dev/null || stat -c%s -- \"\$1\" 2>/dev/null)\""
mk good_date.sh        "s=\"\$($DR \"\$e\" +%Y%m%d || $DD \"@\$e\" +%Y%m%d)\""
mk good_allow.sh       "m=\"\$($SF %m \"\$1\")\"   # platform-dialect: allow (テスト用の例外)"
mk good_comment.sh     "# $SF %m \"\$1\" のような説明コメントは検査しない"

run_check() { "$CHECK" "$BASE/$1" > "$BASE/out" 2>&1; printf '%s' "$?"; }

# --- 落とすべき形 ---------------------------------------------------------------
for case_name in bad_order bad_nofallback bad_date; do
  rc="$(run_check "$case_name.sh")"
  if [ "$rc" = 1 ]; then ok "$case_name を落とす (exit 1)"; else ng "$case_name を見逃した (exit $rc)"; fi
done

# 落とすだけでなく「どの形か」を言うこと (直せない指摘は指摘でない)
rc="$(run_check bad_order.sh)"; [ "$rc" = 1 ] || true
grep -q 'BSD (-f) を先に試している' "$BASE/out" \
  && ok "順序の誤りを順序の問題として報告する" || ng "順序の誤りの理由を報告していない"
rc="$(run_check bad_date.sh)"; [ "$rc" = 1 ] || true
grep -q '\[date\]' "$BASE/out" && ok "date の違反を date として報告する" || ng "date の違反を報告していない"

# --- 通すべき形 -----------------------------------------------------------------
# ⚠️ 密着形 (-f%z) を落とさないこと。GNU では `%` が invalid option になり **stdout は空**なので
#   フォールバックが正しく効く (実測 2026-08-28)。ここを落とすと動いている既存コードを壊す。
for case_name in good_order good_attached good_date good_allow good_comment; do
  rc="$(run_check "$case_name.sh")"
  if [ "$rc" = 0 ]; then ok "$case_name は通す (exit 0)"; else ng "$case_name を誤検出した (exit $rc): $(head -3 "$BASE/out" | tr '\n' ' ')"; fi
done

# --- 検査したことを出力に出すこと (沈黙を成功と区別する) -------------------------
rc="$(run_check good_order.sh)"; [ "$rc" = 0 ] || true
grep -q '該当なし' "$BASE/out" && ok "合格時も検査した件数を出す" || ng "合格時に何も出さない (沈黙と区別できない)"

printf '\n'
if (( fails > 0 )); then
  printf '[test-platform-dialect] %d 件失敗\n' "$fails" >&2
  exit 1
fi
printf '[test-platform-dialect] すべて成功\n'
