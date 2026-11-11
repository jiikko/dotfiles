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
TMP_ROOT=$(mktemp -d); trap 'rm -rf "$TMP_ROOT"' EXIT

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
