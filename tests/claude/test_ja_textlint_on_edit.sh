#!/usr/bin/env bash
# _claude/hooks/ja-textlint-on-edit.sh (PostToolUse(Write / Edit): 書いた Markdown の行に出た自作の規則の指摘を知らせる) の unit テスト。
#
# なぜ: 知らせが出なくなる退行は無音 (書き込みはそのまま通る)。前からある指摘まで出す退行は、知らせが積もって読まれなくなる形で効いてくる。
# 依存が無いときに npm を走らせる退行は、編集のたびに数十秒止まる形で出る。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK="$ROOT_DIR/_claude/hooks/ja-textlint-on-edit.sh"
# 本番の配線と同じ上限 (timeout に殺されると無出力 = 知らせが消える)
HOOK_TIMEOUT=15
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
runtimeout_resolve "$ROOT_DIR" || exit 1
command -v jq >/dev/null 2>&1 || { echo "✗ jq が無い。hook は jq 前提なので検査できない" >&2; exit 1; }

# 配線: settings.json の PostToolUse の Write|Edit から呼ばれていること
jq -e '.hooks.PostToolUse[] | select(.matcher == "Write|Edit") | .hooks[] | select(.command == "~/dotfiles/_claude/hooks/ja-textlint-on-edit.sh")' \
  "$ROOT_DIR/_claude/settings.json" >/dev/null || { echo "✗ settings.json の PostToolUse(Write|Edit) に hook が無い (配線が外れている)"; exit 1; }

if ! command -v npm >/dev/null; then
  echo "skipped: npm が無い (ja-textlint は初回に npm ci で textlint を入れる)"
  exit 77
fi

ok=0; fail=0
# 実パスで持つ: /var は /private/var への symlink で、textlint は cwd を実パスで持つ。論理パスのファイルは cwd の
# .textlintignore の照合から外れ、「ignore に消されない」の検査が何も確かめなくなる
TMP_ROOT=$(cd "$(mktemp -d)" && pwd -P) || exit 1; trap 'rm -rf "$TMP_ROOT"' EXIT

# hook は依存を入れないので、先に手で 1 回打って入れておく (本番も同じ: 一度手で打てば入る)
printf 'x\n' > "$TMP_ROOT/warm.md"
"$ROOT_DIR/bin/ja-textlint" "$TMP_ROOT/warm.md" >/dev/null 2>&1 || true

# 🚨 rc と stderr を捨てない: 知らせだけの hook は、どの入力でも rc=0・stderr は空でなければならない
run() { # $1=hook への入力の JSON → stdout
  local rc
  printf '%s' "$1" | "$RUNTIMEOUT" "$HOOK_TIMEOUT" "$HOOK" > "$TMP_ROOT/out" 2> "$TMP_ROOT/err"
  rc=$?
  if [ "$rc" -ne 0 ] || [ -s "$TMP_ROOT/err" ]; then
    printf 'HOOK-FAILED rc=%s stderr=%s\n' "$rc" "$(head -c 200 "$TMP_ROOT/err")"
    return 0
  fi
  cat "$TMP_ROOT/out"
}
edit_input() { # $1=file $2=new_string
  jq -n --arg f "$1" --arg s "$2" '{tool_name: "Edit", tool_input: {file_path: $f, old_string: "x", new_string: $s}}'
}
write_input() { # $1=file
  jq -n --arg f "$1" '{tool_name: "Write", tool_input: {file_path: $f}}'
}
# 知らせの指摘の行番号を、並べた 1 行で返す ("3 5" のように)。知らせが無ければ空
lines_of() { # $1=stdout
  printf '%s' "$1" | jq -r '.hookSpecificOutput.additionalContext' 2>/dev/null |
    sed -n 's/^\([0-9][0-9]*\):[0-9][0-9]* .*/\1/p' | tr '\n' ' ' | sed 's/ $//'
}
expect_lines() { # $1=説明 $2=stdout $3=期待する行番号の並び (空なら知らせが無いこと)
  case "$2" in HOOK-FAILED*) echo "✗ hook が rc≠0 か stderr を出した: $1: $2"; fail=$((fail+1)); return ;; esac
  if [ -z "$3" ]; then
    if [ -z "$2" ]; then echo "✓ ${1}"; ok=$((ok+1)); else echo "✗ ${1}: 知らせが出た: $2"; fail=$((fail+1)); fi
    return
  fi
  if ! printf '%s' "$2" | jq -e '.hookSpecificOutput.hookEventName == "PostToolUse"' >/dev/null 2>&1; then
    echo "✗ ${1}: PostToolUse の知らせになっていない: $2"; fail=$((fail+1)); return
  fi
  local got; got=$(lines_of "$2")
  if [ "$got" = "$3" ]; then echo "✓ ${1}"; ok=$((ok+1)); else echo "✗ ${1}: 行が「${got}」(期待「${3}」)"; fail=$((fail+1)); fi
}

f="$TMP_ROOT/a.md"
printf '# 試験\n\n前から黙って書いてある。\n\nここは何も無い。\n\n新しく潰す行。\n' > "$f"

expect_lines "Write はファイル全体の指摘を知らせる" "$(run "$(write_input "$f")")" "3 7"
expect_lines "Edit は書いた行だけを知らせ、前からある指摘は出さない" "$(run "$(edit_input "$f" "新しく潰す行。")")" "7"
expect_lines "Edit の書いた行に指摘が無ければ何も出さない" "$(run "$(edit_input "$f" "ここは何も無い。")")" ""
expect_lines "Edit の new_string が空 (削除だけ) なら何も出さない" "$(run "$(edit_input "$f" "")")" ""

# 複数行の new_string は、その全部の行を見る (先頭の行だけで判定しない)
expect_lines "複数行の new_string の 2 行目の指摘も知らせる" "$(run "$(edit_input "$f" "ここは何も無い。

新しく潰す行。")")" "7"

# replace_all のように同じ文が複数の場所にあれば、全部の出現を見る。日本語の前置きでも行を数え違えない (位置はコードポイント)
g="$TMP_ROOT/b.md"
printf '日本語の前置き。\n同じ行で効く。\n間の行。\n同じ行で効く。\n' > "$g"
expect_lines "同じ new_string の全部の出現の行を知らせる" "$(run "$(edit_input "$g" "同じ行で効く。")")" "2 4"

# new_string の中の書き換えていない文脈の行 (old_string にある行) の指摘は出さない
edit_ctx_input() { # $1=file $2=old_string $3=new_string
  jq -n --arg f "$1" --arg o "$2" --arg s "$3" '{tool_name: "Edit", tool_input: {file_path: $f, old_string: $o, new_string: $s}}'
}
k="$TMP_ROOT/ctx.md"
printf '一行目。\n前から黙って書いてある。\n追加で潰す行。\n' > "$k"
expect_lines "文脈の行 (old_string にある行) の指摘は出さず、足した行だけを知らせる" \
  "$(run "$(edit_ctx_input "$k" "前から黙って書いてある。" "前から黙って書いてある。
追加で潰す行。")")" "3"

# ファイルの先頭 (位置 0) に書いた行も 1 行目として数える
top="$TMP_ROOT/top.md"
printf '先頭で潰す行。\n二行目。\n' > "$top"
expect_lines "ファイルの先頭に書いた行の指摘を知らせる" "$(run "$(edit_input "$top" "先頭で潰す行。")")" "1"

# old_string にある行を写して増やした分は、書いた行として知らせる (old_string にある元の行は出さない)
cp_f="$TMP_ROOT/copy.md"
printf '黙って捨てる。\n間。\n黙って捨てる。\n' > "$cp_f"
expect_lines "old_string の行を写して増やした行を知らせる" "$(run "$(edit_ctx_input "$cp_f" "黙って捨てる。
間。" "黙って捨てる。
間。
黙って捨てる。")")" "3"

# 出現の多い短い new_string (1 字の replace_all) でも上限の内に終わる (出現ごとに先頭から改行を数え直すと、出現の数の 2 乗で伸びる)。
# 大きさは 1000 行・3000 回: 実測で hook 全体は 2〜3 秒 (ほとんどが textlint)、数え直す形の jq だけで 800 行 17 秒・1200 行 55 秒
many="$TMP_ROOT/many.md"
{ for _ in $(seq 1000); do printf 'ここにあるのはの、の、の。\n'; done; printf '最後の行で潰す。\n'; } > "$many"
expect_lines "出現の多い 1 字の new_string でも上限の内に終わり、書いた行を知らせる" "$(run "$(edit_input "$many" "の")")" "1001"

# 知らせに文字列が含まれることを見る (件数の多いケース用)
expect_contains() { # $1=説明 $2=stdout $3=知らせに含むべき文字列
  case "$2" in HOOK-FAILED*) echo "✗ hook が rc≠0 か stderr を出した: $1: $2"; fail=$((fail+1)); return ;; esac
  if printf '%s' "$2" | jq -e --arg l "$3" '.hookSpecificOutput.additionalContext | contains($l)' >/dev/null 2>&1; then
    echo "✓ ${1}"; ok=$((ok+1))
  else
    echo "✗ ${1}: 知らせに「${3}」が無い: $(printf '%s' "$2" | head -c 200)"; fail=$((fail+1))
  fi
}

# 3000 行を超えるファイルは見ない (textlint の時間は行の数で伸びる。1 万行・150KB で 47 秒)
manyl="$TMP_ROOT/manyl.md"
{ printf '黙って捨てる。\n'; for _ in $(seq 4000); do printf -- '- 項目です\n'; done; } > "$manyl"
expect_lines "3000 行を超えるファイルは見ない" "$(run "$(write_input "$manyl")")" ""

# 行の多い new_string でも上限の内に終わる (行ごとに前を数え直すと、行の数の 2 乗で伸びる)。
# 1000 行にするのは、hook の中の textlint の上限 (10 秒) に負荷の高い日でも余裕を残すため (普段は約 2 秒)
biged="$TMP_ROOT/biged.md"
{ for n in $(seq 1000); do printf '行 %s で潰す。\n' "$n"; done; } > "$biged"
expect_contains "1000 行の new_string でも上限の内に終わり、知らせる" "$(run "$(edit_input "$biged" "$(cat "$biged")")")" "ほか 980 件"

# 同じ行の多い new_string で範囲が大きくなっても、知らせが消えない (範囲を引数で渡すと argument list too long で jq が起動できない)
same="$TMP_ROOT/same.md"
{ for _ in $(seq 1000); do printf '黙って捨てる。\n'; done; } > "$same"
block=$(for _ in $(seq 500); do printf '黙って捨てる。\n'; done)
expect_contains "同じ行の多い new_string でも知らせる" "$(run "$(edit_input "$same" "$block")")" "ほか 980 件"

# 行の区切りを textlint に揃える: 行の中の U+2028 (LS) で textlint は行を分ける。hook が分けないと行の番号が食い違って知らせが消える
ls_f="$TMP_ROOT/ls.md"
printf '一行目。\n二行目。\n前置き\xe2\x80\xa8後ろで潰す。\n' > "$ls_f"
ls_new=$(printf '前置き\xe2\x80\xa8後ろで潰す。')
expect_lines "行の中の U+2028 で textlint が行を分けても知らせる" "$(run "$(edit_input "$ls_f" "$ls_new")")" "4"

# CR だけで改行したファイルでも、textlint と同じ行の番号で知らせる
cr_f="$TMP_ROOT/cr.md"
printf '一。\r二。\r黙って捨てる。\r' > "$cr_f"
expect_lines "CR だけで改行したファイルでも正しい行で知らせる" "$(run "$(edit_input "$cr_f" "黙って捨てる。")")" "3"

# textlint が遅い入力でも、hook は上限 (15 秒) の内に黙って終わる (textlint は 10 秒で止める)。
# 入力は指摘の多い和文を空行なしで並べたもの。大きさ・行数の上限の内側 (159.5KB・2750 行) で、textlint 単独で 19 秒 (負荷の低いとき)
slow="$TMP_ROOT/slow.md"
{ for _ in $(seq 2750); do printf '黙って黙って黙って黙って黙って黙って。\n'; done; } > "$slow"
expect_lines "textlint が遅い入力でも上限の内に黙って終わる" "$(run "$(write_input "$slow")")" ""

# 読めないファイルでは何も出さず、stderr にも出さない
ro="$TMP_ROOT/ro.md"
printf '黙って捨てる。\n' > "$ro"
chmod 000 "$ro"
expect_lines "読めないファイルは見ず、stderr にも出さない" "$(run "$(write_input "$ro")")" ""
chmod 600 "$ro"

# new_string が改行で終わっても、次の行 (書いていない行) は見ない
m="$TMP_ROOT/nl.md"
printf '足した行。\n前から黙って書いてある。\n' > "$m"
expect_lines "new_string の末尾の改行で、次の行の指摘を出さない" "$(run "$(edit_input "$m" "足した行。
")")" ""

# CRLF のファイルでも、LF で届く複数行の new_string の位置を見つける
c="$TMP_ROOT/crlf.md"
printf '前置き\r\nここは何も無い。\r\n新しく潰す行。\r\n' > "$c"
expect_lines "CRLF のファイルで複数行の Edit の指摘を知らせる" "$(run "$(edit_input "$c" "ここは何も無い。
新しく潰す行。")")" "3"

# session の cwd の .textlintignore で知らせを消さない
ig="$TMP_ROOT/ig"
mkdir -p "$ig"
printf '*.md\n' > "$ig/.textlintignore"
printf '黙って捨てる。\n' > "$ig/x.md"
expect_lines "cwd の .textlintignore があっても知らせる" "$(cd "$ig" && run "$(write_input "$ig/x.md")")" "1"

# 160KB を超えるファイルは見ない (textlint はファイル全体を読むので、hook の上限を超える)
big="$TMP_ROOT/big.md"
{ printf '黙って捨てる。\n'; for _ in $(seq 6000); do printf 'これは三十字ほどの、指摘の出ない普通の文の行です。\n'; done; } > "$big"
expect_lines "160KB を超えるファイルは見ない" "$(run "$(write_input "$big")")" ""

# 表示されない太字も知らせる
h="$TMP_ROOT/c.md"
printf 'ただし**「x」**と書く。\n' > "$h"
expect_lines "表示されない太字を知らせる" "$(run "$(write_input "$h")")" "1 1"

# preset の指摘 (文の長さ) は知らせない
long="$TMP_ROOT/d.md"
{ printf 'これは'; for _ in $(seq 40); do printf 'とても長い文で'; done; printf 'ある。\n'; } > "$long"
expect_lines "preset の指摘 (文の長さ) は知らせない" "$(run "$(write_input "$long")")" ""

# Markdown 以外の拡張子は見ない
t="$TMP_ROOT/e.txt"
printf '黙って捨てる。\n' > "$t"
expect_lines ".txt は見ない" "$(run "$(write_input "$t")")" ""
expect_lines "相対パスは見ない" "$(run "$(write_input "e.md")")" ""
expect_lines "無いファイルは見ない" "$(run "$(write_input "$TMP_ROOT/none.md")")" ""

# 依存が入っていなければ、入れずに何も出さない (編集のたびに npm を走らせない)
empty_cache="$TMP_ROOT/cache"
mkdir -p "$empty_cache"
out=$(XDG_CACHE_HOME="$empty_cache" run "$(write_input "$f")")
expect_lines "依存が未導入なら何も出さない" "$out" ""
if [ -z "$(ls -A "$empty_cache")" ]; then
  echo "✓ 依存が未導入でも入れない (キャッシュの dir は空のまま)"; ok=$((ok+1))
else
  echo "✗ 依存が未導入のとき、hook が入れ始めた: $(ls -A "$empty_cache")"; fail=$((fail+1))
fi

echo "ja-textlint-on-edit: ok=$ok fail=$fail"
[ "$fail" -eq 0 ] || exit 1
