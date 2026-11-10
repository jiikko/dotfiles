#!/usr/bin/env bash
# _claude/hooks/warn-invisible-chars-in-tool-input.sh (PreToolUse(Bash / Write / Edit): 入力の見えにくい字を知らせる) の unit テスト。
#
# なぜ: 知らせが出なくなる退行は無音 (道具はそのまま通り、字に変わった表記は diff でも見えない)。
# 🚨 見えにくい字は printf の 8 進で組む。このファイルを道具で書くので、表記で書くとこのファイルの中で字に変わる。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/warn-invisible-chars-in-tool-input.sh"
# 本番の配線と同じ上限 (timeout に殺されると無出力 = 知らせが消える)
HOOK_TIMEOUT=10
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1
command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い。hook は jq 前提なので検査できない" >&2; exit 1; }

# 配線: settings.json の PreToolUse の Bash と Write|Edit の両方から呼ばれていること
for m in Bash 'Write|Edit'; do
  jq -e --arg m "$m" '.hooks.PreToolUse[] | select(.matcher == $m) | .hooks[] | select(.command == "~/dotfiles/_claude/hooks/warn-invisible-chars-in-tool-input.sh")' \
    "$ROOT_DIR/_claude/settings.json" >/dev/null || { echo "✗ settings.json の PreToolUse($m) に hook が無い (配線が外れている)"; exit 1; }
done

IDEOSP=$(printf '\343\200\200')   # U+3000
NBSP=$(printf '\302\240')         # U+00A0
ZWSP=$(printf '\342\200\213')     # U+200B
BOM=$(printf '\357\273\277')      # U+FEFF

ok=0; fail=0
TMP_ROOT=$(mktemp -d); trap 'rm -rf "$TMP_ROOT"' EXIT

# 🚨 rc と stderr を捨てない: 知らせだけの hook は、どの入力でも rc=0・stderr は空でなければならない
run() { # stdin=hook の入力 → stdout
  local rc
  "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" > "$TMP_ROOT/out" 2> "$TMP_ROOT/err"
  rc=$?
  if [ "$rc" -ne 0 ] || [ -s "$TMP_ROOT/err" ]; then
    printf 'HOOK-FAILED rc=%s stderr=%s\n' "$rc" "$(head -c 200 "$TMP_ROOT/err")"
    return 0
  fi
  cat "$TMP_ROOT/out"
}
bash_in() { jq -n --arg c "$1" '{tool_name: "Bash", session_id: "s", tool_input: {command: $c}}'; }
# 内容はファイル経由で渡す (--arg は argv の上限 1MB を越える入力を渡せない)
write_in() { printf '%s' "$1" > "$TMP_ROOT/content"; jq -n --rawfile c "$TMP_ROOT/content" '{tool_name: "Write", session_id: "s", tool_input: {file_path: "/x/a.md", content: $c}}'; }
edit_in() { jq -n --arg o "$1" --arg n "$2" '{tool_name: "Edit", session_id: "s", tool_input: {file_path: "/x/a.go", old_string: $o, new_string: $n}}'; }

expect_notice() { # $1=説明 $2=stdout $3...=知らせに含むべき文字列
  local d="$1" out="$2"; shift 2
  case "$out" in HOOK-FAILED*) echo "✗ hook が rc≠0 か stderr を出した: $d: $out"; fail=$((fail+1)); return ;; esac
  local s
  for s in "$@"; do
    if ! printf '%s' "$out" | jq -e --arg l "$s" '.hookSpecificOutput.hookEventName == "PreToolUse" and (.hookSpecificOutput.additionalContext | contains($l))' >/dev/null 2>&1; then
      echo "✗ 知らせが出ない / 「${s}」を含まない: $d: $(printf '%s' "$out" | head -c 300)"; fail=$((fail+1)); return
    fi
  done
  ok=$((ok+1))
}
expect_silent() { # $1=説明 $2=stdout
  if [ -n "$2" ]; then echo "✗ 鳴るべきでないのに出力がある: $1: $(printf '%s' "$2" | head -c 300)"; fail=$((fail+1)); else ok=$((ok+1)); fi
}

expect_line() { # $1=説明 $2=stdout $3=「該当」の行として完全一致で在るべき行
  if printf '%s' "$2" | jq -e --arg l "$3" '.hookSpecificOutput.additionalContext | split("\n") | index([$l]) != null' >/dev/null 2>&1; then
    ok=$((ok+1))
  else
    echo "✗ 「該当」に行「$3」が無い: $1: $(printf '%s' "$2" | jq -r '.hookSpecificOutput.additionalContext' 2>/dev/null | tail -5)"; fail=$((fail+1))
  fi
}
rep() { local s=""; local i; for ((i = 0; i < $2; i++)); do s+="$1"; done; printf '%s' "$s"; }
ch_of() { jq -rn --argjson c "$1" '[$c] | implode'; }

LIST_CPS="160 173 847 4447 4448 6158 $(seq 8192 8207 | tr '\n' ' ') 8232 8233 $(seq 8234 8239 | tr '\n' ' ') 8287 $(seq 8288 8292 | tr '\n' ' ') $(seq 8294 8297 | tr '\n' ' ') 12288 12644 65279 65440"
ALL_CHARS=""; for cp in $LIST_CPS; do ALL_CHARS+=$(ch_of "$cp"); done
# 行ごとに全部の字を探す形 (変異 per-line-scan) は、この入力で 10 秒の上限の 2 倍を越える (約 23 秒)。今の形は約 0.5 秒 (2026-11-10 に jq 1.7.1 / 1.8.2 で測った)
MANY_LINES=$(head -c 1500000 /dev/zero | tr '\0' '\n'; printf x)

cases() {
# --- 鳴る ---
expect_notice "Bash の単一引用符の中の全角空白" "$(bash_in "printf '%s' 'X${IDEOSP}Y'" | run)" "U+3000 (全角空白) x1" "X<U+3000 (全角空白)>Y"
expect_notice "Bash の NBSP" "$(bash_in "echo a${NBSP}b" | run)" "U+00A0 (NBSP) x1"
expect_notice "Write の BOM (Go の rune のつもり)" "$(write_in "r == '${BOM}'" | run)" "U+FEFF (BOM) x1"
expect_notice "Write: 複数の字と行。該当の行を出す" \
  "$(write_in "$(printf 'line1\nA%sB%sC\nline3 %s\n' "$IDEOSP" "$IDEOSP" "$ZWSP")" | run)" \
  "U+3000 (全角空白) x2" "U+200B (ZWSP) x1" "A<U+3000 (全角空白)>B<U+3000 (全角空白)>C" "line3 <U+200B (ZWSP)>"
expect_notice "Edit: new_string で増えた字" "$(edit_in 'strings.TrimRight(s, " ")' "strings.TrimRight(s, \" ${IDEOSP}\")" | run)" "U+3000 (全角空白) x1"
expect_notice "Edit: 元からある字より数が増えた分だけ数える" "$(edit_in "a${IDEOSP}b" "a${IDEOSP}b${IDEOSP}c" | run)" "U+3000 (全角空白) x1"
# 長い行は字の前後だけを出す (行の頭が無いこと)
long=$(printf 'H%.0s' $(seq 300))
out=$(write_in "${long}Z${NBSP}W${long}" | run)
expect_notice "長い行: 字の周りを出す" "$out" "Z<U+00A0 (NBSP)>W"
case "$out" in *'HHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHH'*)
  echo "✗ 長い行を丸ごと出している"; fail=$((fail+1)) ;; *) ok=$((ok+1)) ;; esac
# 和文の長い行: 字の位置をバイトでなく字で数えていること (バイトで数えると字より前で切れて、字が出ない)
jp=$(printf '日%.0s' $(seq 200))
expect_notice "和文の長い行: 字の周りを出す" "$(write_in "${jp}${ZWSP}終" | run)" "日日<U+200B (ZWSP)>終"
# 窓は字の前 40 字・字・後ろ 39 字 (行の頭に近い字は行の頭から)
expect_line "窓の幅" "$(write_in "$(rep a 100)${ZWSP}$(rep b 100)" | run)" "  $(rep a 40)<U+200B (ZWSP)>$(rep b 39)"
expect_line "行の頭に近い字" "$(write_in "abc${ZWSP}$(rep x 200)" | run)" "  abc<U+200B (ZWSP)>$(rep x 39)"
# 1 行に別の種類の字が離れてあるときは、それぞれの字の周りを出す (窓は字ごとに切る)
expect_line "行の中で最初の字" "$(write_in "P${ZWSP}$(rep y 100)${NBSP}Q" | run)" "  P<U+200B (ZWSP)>$(rep y 39)"
# Edit: 要約は増えた分だけ数える (該当は元からある字の周りも出る。増えた字の場所は特定しない: hook の冒頭)
old5=$(printf 'o%sk\n' "$IDEOSP" "$IDEOSP" "$IDEOSP" "$IDEOSP" "$IDEOSP")
expect_notice "Edit: 元からある字が多くても増えた分だけ数える" "$(edit_in "$old5" "${old5}
NEW${IDEOSP}LINE" | run)" "U+3000 (全角空白) x1"
# 周りに入った別の種類の字も印にする (生の字のまま出さない)
expect_line "周りの別の種類の字" "$(write_in "x${NBSP}y${ZWSP}z" | run)" "  x<U+00A0 (NBSP)>y<U+200B (ZWSP)>z"
# 同じ字の 2 か所目・3 か所目も出し、4 か所目は出さない (字ごとに 3 か所まで)
out=$(write_in "$(printf 'p1%sx\np2%sx\np3%sx\np4%sx' "$NBSP" "$NBSP" "$NBSP" "$NBSP")" | run)
expect_line "同じ字の 2 か所目" "$out" "  p2<U+00A0 (NBSP)>x"
expect_line "同じ字の 3 か所目" "$out" "  p3<U+00A0 (NBSP)>x"
case "$out" in *'p4<U+00A0'*) echo "✗ 字ごとに 3 か所までのはずが 4 か所目を出した"; fail=$((fail+1)) ;; *) ok=$((ok+1)) ;; esac
# 同じ字が近くに続くとき、周りを同じ字の前後の出現で切らない (前と後ろの部分を繋いでから切る)。2 か所の周りは同じになり、1 回だけ出す
out=$(write_in "x1${ZWSP}x2${ZWSP}x3" | run)
expect_line "近くに続く同じ字の周り" "$out" "  x1<U+200B (ZWSP)>x2<U+200B (ZWSP)>x3"
n_x=$(printf '%s' "$out" | jq -r '.hookSpecificOutput.additionalContext' | grep -c '^  x')
if [ "$n_x" -eq 1 ]; then ok=$((ok+1)); else echo "✗ 近くに続く同じ字の周りを $n_x 行出した (1 行のはず)"; fail=$((fail+1)); fi
# 合わせて 5 か所まで (3 種 × 3 か所の離れた出現)
far=$(rep . 100)
out=$(write_in "a1${NBSP}${far}a2${NBSP}${far}a3${NBSP}${far}b1${ZWSP}${far}b2${ZWSP}${far}b3${ZWSP}${far}c1${IDEOSP}${far}c2${IDEOSP}${far}c3${IDEOSP}" | run)
n_all=$(printf '%s' "$out" | jq -r '.hookSpecificOutput.additionalContext' | sed -n '/^該当/,$p' | grep -c '^  ')
if [ "$n_all" -eq 5 ]; then ok=$((ok+1)); else echo "✗ 該当が 5 か所でない ($n_all か所)"; fail=$((fail+1)); fi
# 同じ周りは 1 回だけ出す。CR は出さない (CRLF の入力)
out=$(write_in "$(printf 'd%sd\r\nd%sd\r\n' "$ZWSP" "$ZWSP")" | run)
expect_line "CRLF の入力の周り" "$out" "  d<U+200B (ZWSP)>d"
n_same=$(printf '%s' "$out" | jq -r '.hookSpecificOutput.additionalContext' | grep -c 'd<U+200B (ZWSP)>d')
if [ "$n_same" -eq 1 ]; then ok=$((ok+1)); else echo "✗ 同じ周りを $n_same 回出した"; fail=$((fail+1)); fi
# 一覧の字は全部鳴る (仕様の一覧をテストの側にも持つ。hook の一覧から字が抜けると赤になる)
for cp in $LIST_CPS; do
  expect_notice "一覧の字 $cp" "$(write_in "a$(ch_of "$cp")b" | run)" "$(printf 'U+%04X (' "$cp")"
done

# --- 鳴らない ---
expect_silent "ASCII だけの Bash" "$(bash_in 'ls -la' | run)"
expect_silent "和文だけ (見えにくい字なし)" "$(write_in '日本語の文。全角の「括弧」と、句読点。' | run)"
expect_silent "表記のままの \\uXXXX (字でない)" "$(write_in 'a \uXXXX b \\u3000' | run)"
expect_silent "Edit: 元からある字を書き直しただけ" "$(edit_in "a${IDEOSP}b" "c${IDEOSP}d" | run)"
expect_silent "対象外の道具 (Read)" "$(jq -n --arg p "/x/${IDEOSP}" '{tool_name: "Read", tool_input: {file_path: $p}}' | run)"
expect_silent "壊れた入力" "$(printf 'not json' | run)"
expect_silent "空の入力" "$(printf '' | run)"
expect_silent "絵文字の異体字セレクタ U+FE0F は一覧に無い" "$(write_in "$(ch_of 9764)$(ch_of 65039)" | run)"

# --- 大きな入力でも timeout の内に返る ---
big=$(head -c 1000000 /dev/zero | tr '\0' 'a')
expect_notice "1 MB の 1 行の末尾に 1 字" "$(write_in "${big}${ZWSP}" | run)" "U+200B (ZWSP) x1"
# 字の無い行が多い入力の末尾に一覧の全部の字 (行ごとに字を探す形は「行数 × 字の種類」に比例し、10 秒を越えて知らせが出ない)
expect_notice "150 万行の末尾に全部の字" "$(write_in "${MANY_LINES}${ALL_CHARS}" | run)" "U+3000 (全角空白) x1" "U+FFA0 (ハングルの埋め字) x1"
}

# jq の版ごとに回す: jq 1.7 は文字列の index がバイトの位置を返すなど、版で結果が変わる関数がある。macOS の /usr/bin/jq は 1.7.1、
# 開発機の PATH の先頭は Homebrew の版。hook は PATH の jq を使う
jqs=(); seen=" "
for j in /usr/bin/jq "$(command -v jq)"; do
  [ -x "$j" ] || continue
  r=$(readlink -f "$j")
  case "$seen" in *" $r "*) continue ;; esac
  seen+="$r "; jqs+=("$j")
done
[ "${#jqs[@]}" -gt 0 ] || { echo "✗ 回せる jq が無い"; exit 1; }
k=0
for j in "${jqs[@]}"; do
  k=$((k+1)); mkdir -p "$TMP_ROOT/jq$k"; ln -s "$j" "$TMP_ROOT/jq$k/jq"
  echo "jq: $j ($("$j" --version))"
  PATH="$TMP_ROOT/jq$k:$PATH" cases
done

echo "warn-invisible-chars-in-tool-input: ok=$ok fail=$fail (jq ${#jqs[@]} 版)"
[ "$fail" -eq 0 ]
