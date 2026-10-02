#!/usr/bin/env bash
# check_test_sleeps.sh — テストに、理由の印の無い sleep を足させない (issue 615)。
#   shell: tests/ の *.sh / *.zsh / *.bats の `sleep`。Go: src/ の *_test.go の `time.Sleep(`
#
# なぜ: 何かが起きるのを秒数で待つ sleep は、負荷の日にだけ落ち (速いマシンでは緑のまま)、待つぶん遅い。
# 規範 (_claude/rules/avoid-wall-clock-assertions.md) を文書だけで持っていたら、直した後も手書きの待ちが増えた (issue 613 / 262)。
# 待つなら条件で待つ: tests/lib/wait_until.sh の tt_wait_until。
#
# 規則: `sleep` という語 (`sleep 1` / `sleep "$x"` / `/bin/sleep` / `command sleep` / `sleep() { … }` を含む) が出る行は、
#   同じ行か直前の行 (コメントだけの行) に `sleep-ok: <分類>: <理由>` の印が無ければ落とす。印のある sleep の行の次の行には効かない。分類は次のどれか (issue 613 の語彙):
#     dummy    kill される前提・生かすだけのプロセス (実時間を待たない)
#     window   競合の窓・遅い処理を演じる入力 (待ちではない。同期点に置き換えられないものだけ)
#     negative 起きないことの確認 (待つべき成立条件が無い)
#     stub     PATH 上の sleep の stub / sleep を潰す関数
#     tick     ポーリングの刻み (tt_wait_until を使えない場所。heredoc の stub の中など)
#     realtime 本番の時間 (猶予・lease・ticker) を実時間で過ぎさせる / 観測の口が無い事象を秒数で待つ。固定待ちと同じ弱さを持つので、
#              理由に「なぜ置き換えられないか」を書く (例: issue 614 の 9)
#     other    文字列・メッセージの中の sleep など、上のどれでもないもの
#   heredoc の本文 (生成する stub / Makefile) は、heredoc の開始行かその直前のコメント行の印 1 つでまとめて許す
#   (本文の行末に `#` を書くと生成物の一部になる。継続行 `\` の末尾にも書けない)。
#   tests/lib/wait_until.sh (刻みの唯一の実装) は印なしで許す。
#   🚨 heredoc の開始は「引用符の外の `<<`」で判定する (引用符の中身を潰した写しで探す)。here-string (`<<<`) と算術
#   (`(( … ))` の中) は除く。タグは bash の規則で読む (`<<'A-B'` は引用符の中の任意の文字、素の `<<EOS-A` は区切り文字まで、`<<\EOS` も可)。
#   awk は LC_ALL=C (バイト単位) で走らせる。規則はすべて ASCII で、UTF-8 のロケールでは 1 文字ずつの切り出しが多バイト文字の途中で
#   「multibyte conversion failure」で落ちることがある (変異検証で実測)。
#   字句の近似を足していく形にしない (red team 2 周で、正規表現の継ぎ足しはタグのハイフンで素通りした)
#   終端の行が来ないまま末尾に達したら落とす (開始の判定を誤ると本文がファイルの最後まで続き、印 1 つで後半が素通りするため)
#   Go は `time.Sleep(` の行に、同じ行か直前の行の `// sleep-ok: <分類>: <理由>` を要求する (分類は同じ)。待つなら各 package の
#   待ちの helper (pro-con の waitUntil / dispatcher の pollUntil / lockman の waitForCondition) で条件を待ち、helper の中の刻みに印を付ける。
#   🚨 Go を forbidigo ではなくここで見るのは、規則と印を 1 つにするため。glogx / pro-con の .golangci.yml はテストで forbidigo を
#   丸ごと外しており、各 module の lint 設定を崩さずに済む (lint.yml は paths の絞り込み無しで毎 push この検査を回す)
#
# 脅威モデル (adversarial-review-own-safeguards.md §8):
#   止めるもの: テストを書く人が、秒数で待つ sleep を理由を書かずに**うっかり**足すこと。印の理由が妥当かは検査しない (review が読む)。
#   意図的な迂回 (印のある行のコメントに `<<TAG` を書いて後ろの同名の heredoc まで飲ませる、のように組み立てた形) は脅威に含めない。
#   red team 3 周で、素通りの形は周ごとに「組み立てないと起きない」側へ移った (3 周目の 3 形は直したが、ここで打ち切る。issue 615)
#   止めないもの (検出しない形):
#     - 本番の定数 (猶予・lease・ticker) を実時間で過ぎさせる形。テストに sleep の語が出ない (issue 614 の主因)
#     - `read -t` / `timeout` / `perl -e 'select(undef,undef,undef,0.5)'` など、sleep の語を使わない待ち
#     - コメント行 (`#` で始まる行) の中の sleep / 別の言語の sleep (`perl -e 'sleep 1'` は語として拾うが、`python3 -c 'time.sleep(1)'` は拾わない)
#     - 印が文字列の中にある形 (`echo "sleep-ok: …"; sleep 9`)。印の書き方は review が見る
#     - Go の `time.After` / `time.NewTimer` / ticker で待つ形、`exec.Command("sleep", …)` で子を起こす形 (多くは kill される前提の子)
#     - Go の別名 / dot import の time (`tm.Sleep` / `Sleep`)、`*_test.go` 以外のテスト用 helper の package。`/* … */` の中の time.Sleep は落とす (`//` の行だけを読み飛ばす)
#   既知の偽の red (安全側。`other` の印で逃がす): Go の文字列・行末のコメント・`/* */` の中の time.Sleep / 終端タグの後ろに空白がある行を
#   終端と数える / 複数行にまたがる文字列の 2 行目以降 (検査は引用符の状態を行をまたいで追わない)
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
    LC_ALL=C awk '
      function has_mark(s) { return s ~ /sleep-ok: (dummy|window|negative|stub|tick|realtime|other): [^[:space:]]/ }
      function has_sleep(s) { return s ~ /(^|[^A-Za-z0-9_.-])sleep([^A-Za-z0-9_-]|$)/ }
      # 直前の行の印は、その行がコメントだけのときに限る (印のある sleep の行が次の行まで許さないように)
      function prev_mark(p,   t) { t = p; gsub(/^[[:space:]]+/, "", t); return t ~ /^#/ && has_mark(t) }
      # heredoc のタグを返す (開始でなければ "")。引用符の中身を潰した写しで「引用符の外の <<」を探し、here-string (<<<) と
      # 算術 ((( … )) の中) を除き、タグは元の行のその位置から bash の規則で読む (引用符つきは閉じるまで任意の文字、素なら区切りまで)
      # 写しでは引用符の中身・引用符の外のエスケープ (\X)・語の先頭の # から行末 (コメント) を空白にする
      function heredoc_tag(l,   n, i, c, q, blank, depth, rest, tag, j, pc) {
        n = length(l); blank = ""; q = ""
        for (i = 1; i <= n; i++) {
          c = substr(l, i, 1)
          if (q == "") {
            if (c == "\\" && i < n) { blank = blank "  "; i++; continue }
            pc = (i == 1) ? " " : substr(l, i - 1, 1)
            if (c == "#" && pc ~ /[[:space:];&|(]/) { while (length(blank) < n) blank = blank " "; break }
            if (c == "\047" || c == "\042") q = c
            blank = blank c
          } else {
            if (q == "\042" && c == "\\" && i < n) { blank = blank "  "; i++; continue }
            if (c == q) { q = ""; blank = blank c } else blank = blank " "
          }
        }
        depth = 0
        for (i = 1; i < n; i++) {
          if (substr(blank, i, 2) == "((") { depth++; i++; continue }
          if (substr(blank, i, 2) == "))" && depth > 0) { depth--; i++; continue }
          if (substr(blank, i, 3) == "<<<") { i += 2; continue }
          if (substr(blank, i, 2) != "<<" || depth > 0) continue
          rest = substr(l, i + 2)
          sub(/^-/, "", rest); sub(/^[[:space:]]+/, "", rest)
          # タグは 1 語: 引用符で囲んだ部分 (中は任意の文字) と素の部分 (\X は X) を、区切り文字まで連結する (`<<"E" に引用符つきの S と x が続けば ESx`)
          tag = ""
          while (rest != "") {
            c = substr(rest, 1, 1)
            if (c == "\047" || c == "\042") {
              j = index(substr(rest, 2), c)
              if (j == 0) return ""
              tag = tag substr(rest, 2, j - 1); rest = substr(rest, j + 2)
            } else if (c == "\\" && length(rest) > 1) {
              tag = tag substr(rest, 2, 1); rest = substr(rest, 3)
            } else if (c ~ /[[:space:];|&<>()]/) {
              break
            } else {
              tag = tag c; rest = substr(rest, 2)
            }
          }
          return tag
        }
        return ""
      }
      FNR == 1 { hd = ""; hd_ok = 0; prev = "" }
      END { if (hd != "") printf "%s:%d: heredoc の終端 (%s) が見つからない (開始の判定を誤っている。本文がファイルの最後まで続く)\n", FILENAME, FNR, hd }
      {
        line = $0
        if (hd != "") {
          s = line; gsub(/^[[:space:]]+|[[:space:]]+$/, "", s)
          # 終端はタグだけの行。文字列の中に書いた heredoc (テストデータ) は「タグ + 閉じ引用符」で終わるので、それも終端に数える
          if (s == hd || index(s, hd "\"") == 1 || index(s, hd "\047") == 1) { hd = ""; prev = line; next }
          if (has_sleep(line) && !hd_ok && !has_mark(line)) printf "%s:%d: %s\n", FILENAME, FNR, line
          else if (has_sleep(line)) print "MARKED"
          next
        }
        s = line; gsub(/^[[:space:]]+/, "", s)
        if (s ~ /^#/) { prev = line; next }
        tag = heredoc_tag(line)
        if (tag != "") {
          hd = tag
          hd_ok = has_mark(line) || prev_mark(prev)
        }
        if (has_sleep(line)) {
          if (has_mark(line) || prev_mark(prev)) print "MARKED"
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
      LC_ALL=C awk '
        function has_mark(s) { return s ~ /sleep-ok: (dummy|window|negative|stub|tick|realtime|other): [^[:space:]]/ }
        function prev_mark(p,   t) { t = p; gsub(/^[[:space:]]+/, "", t); return t ~ /^\/\// && has_mark(t) }
        FNR == 1 { prev = "" }
        {
          line = $0
          s = line; gsub(/^[[:space:]]+/, "", s)
          if (s ~ /^\/\//) { prev = line; next }
          # 括弧の前の空白・括弧の無いメソッド値 (`f := time.Sleep`) も拾う
          if (line ~ /time\.Sleep([^A-Za-z0-9_]|$)/) {
            if (has_mark(line) || prev_mark(prev)) print "MARKED"
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
  printf '  待ちではない sleep なら、同じ行か直前の行に `# sleep-ok: <dummy|window|negative|stub|tick|realtime|other>: <理由>` を書く (Go は // sleep-ok: …)\n'
  printf '  (heredoc の本文は、開始行かその直前の行の印でまとめて許す)\n'
  exit 1
fi

printf '✓ テストの sleep: shell %d ファイル・Go %d ファイルを検査、理由の印つき %d 行、印の無い行なし\n' "$checked" "$go_checked" "$marked"
