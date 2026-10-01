#!/usr/bin/env bash
# check_test_sleeps.sh — テストに、理由の印の無い sleep を足させない (issue 615)。
#   shell: tests/ の *.sh / *.zsh / *.bats の `sleep`。Go: src/ の *_test.go の `time.Sleep(`
#
# なぜ: 何かが起きるのを秒数で待つ sleep は、負荷の日にだけ落ち (速いマシンでは緑のまま)、待つぶん遅い。
# 規範 (_claude/rules/avoid-wall-clock-assertions.md) を文書だけで持っていたら、直した後も手書きの待ちが増えた (issue 613 / 262)。
# 待つなら条件で待つ: tests/lib/wait_until.sh の tt_wait_until。
#
# 規則: `sleep` という語 (`sleep 1` / `sleep "$x"` / `/bin/sleep` / `command sleep` / `sleep() { … }` を含む) が出る行は、
#   同じ行か直前の行に `sleep-ok: <分類>: <理由>` の印が無ければ落とす。分類は次のどれか (issue 613 の語彙):
#     dummy    kill される前提・生かすだけのプロセス (実時間を待たない)
#     window   競合の窓・遅い処理を演じる入力 (待ちではない。同期点に置き換えられないものだけ)
#     negative 起きないことの確認 (待つべき成立条件が無い)
#     stub     PATH 上の sleep の stub / sleep を潰す関数
#     tick     ポーリングの刻み (tt_wait_until を使えない場所。heredoc の stub の中など)
#     other    文字列・メッセージの中の sleep など、上のどれでもないもの
#   heredoc の本文 (生成する stub / Makefile) は、heredoc の開始行かその直前の行の印 1 つでまとめて許す
#   (本文の行末に `#` を書くと生成物の一部になる。継続行 `\` の末尾にも書けない)。
#   tests/lib/wait_until.sh (刻みの唯一の実装) は印なしで許す。
#   Go は `time.Sleep(` の行に、同じ行か直前の行の `// sleep-ok: <分類>: <理由>` を要求する (分類は同じ)。待つなら各 package の
#   待ちの helper (pro-con の waitUntil / dispatcher の pollUntil / lockman の waitForCondition) で条件を待ち、helper の中の刻みに印を付ける。
#   🚨 Go を forbidigo ではなくここで見るのは、規則と印を 1 つにするため。glogx / pro-con の .golangci.yml はテストで forbidigo を
#   丸ごと外しており、各 module の lint 設定を崩さずに済む (lint.yml は paths の絞り込み無しで毎 push この検査を回す)
#
# 脅威モデル (adversarial-review-own-safeguards.md §8):
#   止めるもの: テストを書く人が、秒数で待つ sleep を理由を書かずに足すこと。印の理由が妥当かは検査しない (review が読む)
#   止めないもの (検出しない形):
#     - 本番の定数 (猶予・lease・ticker) を実時間で過ぎさせる形。テストに sleep の語が出ない (issue 614 の主因)
#     - `read -t` / `timeout` / `perl -e 'select(undef,undef,undef,0.5)'` など、sleep の語を使わない待ち
#     - コメント行 (`#` で始まる行) の中の sleep
#     - Go の `time.After` / `time.NewTimer` / ticker で待つ形、`exec.Command("sleep", …)` で子を起こす形 (多くは kill される前提の子)
#
# 本ファイルの説明文には `sleep` が字として入る (検査の説明そのもの)。対象は tests/ だけなのでこのファイルは数えない。
# shellcheck disable=SC2016  # 案内の文言に `…` を字として入れる (展開しない)
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || { printf '✗ repo root へ移動できない\n'; exit 1; }
TARGET_DIR="${CHECK_TEST_SLEEPS_DIR:-tests}"   # 検査のテストが fixture の置き場を渡す
GO_DIR="${CHECK_TEST_SLEEPS_GO_DIR:-src}"
MIN_FILES="${CHECK_TEST_SLEEPS_MIN_FILES:-20}"

for c in find sort awk; do
  command -v "$c" >/dev/null 2>&1 || { printf '✗ %s が無い。検査できないので緑にしない\n' "$c"; exit 1; }
done
[ -d "$TARGET_DIR" ] || { printf '✗ 検査対象のディレクトリが無い: %s\n' "$TARGET_DIR"; exit 1; }

files_raw="$(find "$TARGET_DIR" -type f \( -name '*.sh' -o -name '*.zsh' -o -name '*.bats' \) | sort)" \
  || { printf '✗ 対象ファイルの列挙に失敗した。検査できないので緑にしない\n'; exit 1; }
files=()
while IFS= read -r f; do
  [ -n "$f" ] && files+=("$f")
done <<< "$files_raw"
if [ "${#files[@]}" -lt "$MIN_FILES" ]; then
  printf '✗ 検査対象が %d 件しかない (発見の壊れ)。緑にしない\n' "${#files[@]}"
  exit 1
fi

offenders=""
checked=0
marked=0
for f in "${files[@]}"; do
  [ -r "$f" ] || { printf '✗ 読めないファイル: %s (検査できないので緑にしない)\n' "$f"; exit 1; }
  checked=$((checked + 1))
  case "$f" in */tests/lib/wait_until.sh|tests/lib/wait_until.sh) continue ;; esac
  out="$(
    awk '
      function has_mark(s) { return s ~ /sleep-ok: (dummy|window|negative|stub|tick|other): [^[:space:]]/ }
      function has_sleep(s) { return s ~ /(^|[^A-Za-z0-9_.-])sleep([^A-Za-z0-9_-]|$)/ }
      FNR == 1 { hd = ""; hd_ok = 0; prev = "" }
      {
        line = $0
        if (hd != "") {
          s = line; gsub(/^[[:space:]]+|[[:space:]]+$/, "", s)
          if (s == hd) { hd = ""; prev = line; next }
          if (has_sleep(line) && !hd_ok && !has_mark(line)) printf "%s:%d: %s\n", FILENAME, FNR, line
          else if (has_sleep(line)) print "MARKED"
          next
        }
        s = line; gsub(/^[[:space:]]+/, "", s)
        if (s ~ /^#/) { prev = line; next }
        if (match(line, /<<-?[[:space:]]*[\047\042]?[A-Za-z_][A-Za-z0-9_]*/)) {
          tag = substr(line, RSTART, RLENGTH)
          sub(/^<<-?[[:space:]]*/, "", tag)
          gsub(/[\047\042]/, "", tag)
          hd = tag
          hd_ok = has_mark(line) || has_mark(prev)
        }
        if (has_sleep(line)) {
          if (has_mark(line) || has_mark(prev)) print "MARKED"
          else printf "%s:%d: %s\n", FILENAME, FNR, line
        }
        prev = line
      }
    ' "$f"
  )" || { printf '✗ awk が失敗した (%s)。検査できないので緑にしない\n' "$f"; exit 1; }
  if [ -n "$out" ]; then
    m="$(grep -c '^MARKED$' <<< "$out" || true)"
    marked=$((marked + m))
    bad="$(grep -v '^MARKED$' <<< "$out" || true)"
    [ -n "$bad" ] && offenders="${offenders}${bad}"$'\n'
  fi
done

# --- Go: *_test.go の time.Sleep( -----------------------------------------------------------------
go_checked=0
if [ "$GO_DIR" != none ]; then
  [ -d "$GO_DIR" ] || { printf '✗ Go の検査対象のディレクトリが無い: %s\n' "$GO_DIR"; exit 1; }
  go_raw="$(find "$GO_DIR" -type f -name '*_test.go' -not -path '*/vendor/*' | sort)" \
    || { printf '✗ Go の対象ファイルの列挙に失敗した。検査できないので緑にしない\n'; exit 1; }
  go_files=()
  while IFS= read -r f; do
    [ -n "$f" ] && go_files+=("$f")
  done <<< "$go_raw"
  if [ "${#go_files[@]}" -lt "${CHECK_TEST_SLEEPS_MIN_GO_FILES:-20}" ]; then
    printf '✗ Go の検査対象が %d 件しかない (発見の壊れ)。緑にしない\n' "${#go_files[@]}"
    exit 1
  fi
  for f in "${go_files[@]}"; do
    [ -r "$f" ] || { printf '✗ 読めないファイル: %s (検査できないので緑にしない)\n' "$f"; exit 1; }
    go_checked=$((go_checked + 1))
    out="$(
      awk '
        function has_mark(s) { return s ~ /sleep-ok: (dummy|window|negative|stub|tick|other): [^[:space:]]/ }
        FNR == 1 { prev = "" }
        {
          line = $0
          s = line; gsub(/^[[:space:]]+/, "", s)
          if (s ~ /^\/\//) { prev = line; next }
          if (line ~ /time\.Sleep\(/) {
            if (has_mark(line) || has_mark(prev)) print "MARKED"
            else printf "%s:%d: %s\n", FILENAME, FNR, s
          }
          prev = line
        }
      ' "$f"
    )" || { printf '✗ awk が失敗した (%s)。検査できないので緑にしない\n' "$f"; exit 1; }
    if [ -n "$out" ]; then
      m="$(grep -c '^MARKED$' <<< "$out" || true)"
      marked=$((marked + m))
      bad="$(grep -v '^MARKED$' <<< "$out" || true)"
      [ -n "$bad" ] && offenders="${offenders}${bad}"$'\n'
    fi
  done
fi

if [ -n "$offenders" ]; then
  printf '✗ テストに理由の印の無い sleep がある (秒数で待たない。issue 615):\n'
  printf '%s' "$offenders" | sed 's/^/    /'
  printf '  直し方: 何かが起きるのを待つなら tests/lib/wait_until.sh の tt_wait_until で条件を待つ\n'
  printf '  待ちではない sleep なら、同じ行か直前の行に `# sleep-ok: <dummy|window|negative|stub|tick|other>: <理由>` を書く (Go は // sleep-ok: …)\n'
  printf '  (heredoc の本文は、開始行かその直前の行の印でまとめて許す)\n'
  exit 1
fi

printf '✓ テストの sleep: shell %d ファイル・Go %d ファイルを検査、理由の印つき %d 行、印の無い行なし\n' "$checked" "$go_checked" "$marked"
