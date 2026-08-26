#!/usr/bin/env bash
#
# 与えられたコマンドを「GNU grep が grep として見える」環境で実行する。
#
# なぜ: BSD grep (macOS) と GNU grep (Linux CI) は正規表現の方言が違い、テストが
# 手元だけ緑・CI だけ赤になる。実測 (2026-08-25):
#
#   printf 'a\tb\n' | grep -qE '\tb'    # BSD  → マッチする
#   printf 'a\tb\n' | ggrep -qE '\tb'   # GNU  → stray \ before t / マッチしない
#
# この差で 8 箇所の assert が手元では永遠に緑のまま CI を 1 日に 2 回赤にした (issue 108)。
# 手元で CI と同じ条件を再現するのが本スクリプトの役割。
#
# 使い方:
#   scripts/with_gnu_grep.sh <コマンド...>     # 例: scripts/with_gnu_grep.sh make test-discovered
#   make test-gnu                              # tests/ 全体を GNU grep 条件で回す
#
# 対象は grep 系のみ。sed / date / ps / stat も BSD と GNU で割れるが、それらまで
# shim すると「Linux エミュレータ」の自作になり保守が本体を超える。grep だけで
# issue 108 の 8 箇所は捕まっているので、必要になった時点で足す。
#
# 判定不能を緑にしない (adversarial-review-own-safeguards「沈黙 = 成功にしない」):
#   - システムの grep が既に GNU (Linux/CI) → shim 不要。そのまま実行する
#   - BSD かつ GNU grep が入っていない      → **失敗させる**。skip して緑を返さない

set -euo pipefail

[ "$#" -gt 0 ] || { echo "usage: $0 <command...>" >&2; exit 2; }

# システムの grep が既に GNU なら、それ自体が目的の条件なので何もしない。
# 判定は「一度変数に取ってから here-string」で行う。pipefail 下で `… | grep -q` を条件に使うと、
# grep -q が一致で即終了して上流が SIGPIPE を受け、**一致しているのに非 0** になる (issue 096)。
sys_grep_version="$(grep --version 2>/dev/null | head -1 || true)"
if grep -q 'GNU grep' <<<"$sys_grep_version"; then
  echo "[with-gnu-grep] システムの grep が既に GNU です。shim なしで実行します" >&2
  exec "$@"
fi

if ! command -v ggrep >/dev/null 2>&1; then
  cat >&2 <<'MSG'
[with-gnu-grep] FAIL: GNU grep が見つかりません。
  このホストの grep は BSD 系で、CI (Linux) と方言が違います。再現するには GNU grep が要ります:

      brew install grep

  「入っていないので skip」にはしません。skip を緑で返すと、CI だけで落ちる差を
  手元で潰せたと誤解するためです (issue 108)。
MSG
  exit 1
fi

shim_dir="$(mktemp -d "${TMPDIR:-/tmp}/gnugrep-shim.XXXXXX")"
trap 'rm -rf "$shim_dir"' EXIT

# 実体を絶対パスで解決してから貼る (path-shim-must-resolve-real-binary)。
# 相対名のまま exec すると PATH 先頭の自分自身に解決して無限再帰する。
for name in grep egrep fgrep; do
  gnu_path="$(command -v "g$name" 2>/dev/null)" || continue
  real="$(cd "$(dirname "$gnu_path")" && pwd)/$(basename "$gnu_path")"
  # symlink をたどって実体を得る (readlink -f は BSD に無い版があるため段階的に)
  while [ -L "$real" ]; do
    link="$(readlink "$real")"
    case "$link" in
      /*) real="$link" ;;
      *)  real="$(cd "$(dirname "$real")" && cd "$(dirname "$link")" && pwd)/$(basename "$link")" ;;
    esac
  done
  # 解決先が shim 自身の配下なら自己参照。無音で回り続けるので即座に落とす
  case "$real" in
    "$shim_dir"/*) echo "[with-gnu-grep] FAIL: $name の解決先が shim 自身です: $real" >&2; exit 70 ;;
  esac
  ln -sf "$real" "$shim_dir/$name"
done

[ -e "$shim_dir/grep" ] || { echo "[with-gnu-grep] FAIL: grep の shim を作れませんでした" >&2; exit 1; }

# shim が実際に GNU として効いているかを、使う前に確認する (貼れた = 効いている ではない)
shim_grep_version="$(PATH="$shim_dir:$PATH" grep --version 2>/dev/null | head -1 || true)"
if ! grep -q 'GNU grep' <<<"$shim_grep_version"; then
  echo "[with-gnu-grep] FAIL: shim を貼ったのに grep が GNU になっていません" >&2
  exit 1
fi

echo "[with-gnu-grep] $shim_grep_version で実行します" >&2
PATH="$shim_dir:$PATH" "$@"
