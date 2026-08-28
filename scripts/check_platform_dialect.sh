#!/usr/bin/env bash
# check_platform_dialect.sh — BSD (macOS) 専用の書き方が Linux (CI) で壊れる形を落とす。
#
# なぜ (2026-08-28 に 1 セッションで 3 件踏んだ。うち 2 件は「正しい形と注記が repo 内に
# 既にあったのに再発した」):
#   手元は BSD、CI は GNU なので、この種の誤りは **手元だけ緑**になる。しかも壊れ方が
#   「静かに空文字を返す」「stdout に別物を混ぜる」なので、失敗が実装の不具合に見える。
#
# 落とす形は実測で確定した 2 つだけ (実測 2026-08-28 / GNU coreutils 9 vs macOS 14):
#
#   A. 空白区切りの `stat -f %X` が `stat -c` より先にある / フォールバックが無い
#        GNU の -f は「ファイルシステム情報の表示」で書式指定ではない。空白で区切ると %X が
#        **ファイル名の位置**に来るため、%X が無いというエラーで rc=1 になる一方、
#        **引数のファイルの fs 情報を stdout に出す**。`||` のフォールバックが返す値と
#        連結され、数値でなくなる (取り残しの回収も schedkeys の prune も Linux で全滅した)。
#        密着形 (`stat -f%z`) は GNU では `%` が invalid option になり **stdout は空**なので
#        フォールバックが正しく効く。落とすのは空白区切りだけ。
#        正しい形: `stat -c '%Y' "$f" || stat -f '%m' "$f"` (GNU を先に試す。BSD の -c は
#        illegal option で**何も出さずに**失敗するため、逆順と違って stdout が汚れない)
#
#   B. フォールバックの無い `date -r <epoch>`
#        BSD の -r は epoch を読むが、GNU の -r は「参照ファイルの時刻」。epoch をファイル名
#        として探して rc=1・stdout 空になる。
#        正しい形: `date -r "$e" "$fmt" || date -d "@$e" "$fmt"`
#
# 意図的な例外は行内に `platform-dialect: allow` を書く (理由も添えること)。GNU の -f を
# 本来の意味 (ファイルシステム情報) で使う / 参照ファイル指定の date -r は正当な用法。
#
# 検査しないもの: コメント行 (行頭 #)。説明文で形を挙げる必要があるため。
#
# 使い方:
#   scripts/check_platform_dialect.sh              # repo 全体 (make test-lint 経由の既定)
#   scripts/check_platform_dialect.sh FILE...      # 指定したファイルだけ (テスト・部分確認用)
# ⚠️ 引数モードでは「発見の壊れ」を検出する下限チェックを外す。**このゲート自身が発火するか**を
#   テストできるようにするための入口で、CI が使うのは引数なしの経路 (下限チェックあり)。
# 本ファイルの説明文・メッセージには `$e` / `$fmt` が字として入る (直し方の提示そのもの)。
# shellcheck disable=SC2016
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || { printf '✗ repo root へ移動できない\n'; exit 1; }

for c in awk find sort; do
  command -v "$c" >/dev/null 2>&1 || { printf '✗ %s が無い。検査できないので緑にしない\n' "$c"; exit 1; }
done

files=()
if [ "$#" -gt 0 ]; then
  files=("$@")
else
  files_raw="$(
    { scripts/discover_shell_scripts.sh || printf '__DISCOVERY_FAILED__\n'
      find tests -type f \( -name '*.sh' -o -name '*.zsh' -o -name '*.bats' \)
    } | sort -u
  )"
  case "$files_raw" in
    *__DISCOVERY_FAILED__*) printf '✗ 対象ファイルの発見に失敗した。検査できないので緑にしない\n'; exit 1 ;;
  esac
  while IFS= read -r f; do
    [ -n "$f" ] && files+=("$f")
  done <<< "$files_raw"
  # 対象 0 件・極端に少ない = 発見の壊れ。緑にしない (沈黙で無検査になるのを防ぐ)
  if [ "${#files[@]}" -lt 20 ]; then
    printf '✗ 検査対象が %d 件しかない (発見の壊れ)。緑にしない\n' "${#files[@]}"
    exit 1
  fi
fi

offenders=""
checked=0
for f in "${files[@]}"; do
  [ -f "$f" ] || continue
  [ -r "$f" ] || { printf '✗ 読めないファイル: %s (検査できないので緑にしない)\n' "$f"; exit 1; }
  checked=$((checked + 1))
  out="$(
    awk '
      {
        line = $0
        s = line; gsub(/^[[:space:]]+/, "", s)
        if (s ~ /^#/) next
        if (line ~ /platform-dialect: allow/) next

        # --- A. stat の方言 -------------------------------------------------
        pf = match(line, /stat[[:space:]]+-f[[:space:]]+[\047\042]?%/)   # 空白区切りのみ
        if (pf > 0) {
          pc = match(line, /stat[[:space:]]+-c/)
          if (pc == 0)
            printf "%s:%d: [stat] GNU 側のフォールバックが無い: %s\n", FILENAME, FNR, s
          else if (pf < pc)
            printf "%s:%d: [stat] BSD (-f) を先に試している (GNU で stdout が汚れる): %s\n", FILENAME, FNR, s
        }

        # --- B. date の方言 -------------------------------------------------
        pr = match(line, /date[[:space:]]+-r[[:space:]]/)
        if (pr > 0 && line !~ /date[[:space:]]+-d[[:space:]]/)
          printf "%s:%d: [date] GNU 側のフォールバックが無い: %s\n", FILENAME, FNR, s
      }
    ' "$f"
  )" || { printf '✗ awk が失敗した (%s)。検査できないので緑にしない\n' "$f"; exit 1; }
  [ -n "$out" ] && offenders="${offenders}${out}"$'\n'
done

if [ -n "$offenders" ]; then
  printf '✗ BSD 専用の書き方がある (手元は緑・CI (Linux) で壊れる):\n'
  printf '%s' "$offenders" | sed 's/^/    /'
  printf "  直し方 [stat]: GNU を先に試す。stat -c '%%Y' \"\$f\" || stat -f '%%m' \"\$f\"\n"
  printf '  直し方 [date]: date -r "$e" "$fmt" || date -d "@$e" "$fmt"\n'
  printf '  意図的な例外は行内に `platform-dialect: allow` を書く (理由も添える)\n'
  exit 1
fi

printf '✓ BSD 専用の stat / date 方言: %d ファイルに該当なし\n' "$checked"
